package core

import (
	"net/url"
	"strings"
	"testing"
)

const groupUsers = `{"data":[
	{"id":139,"username":"","first_name":"Alice","last_name":"Martin"},
	{"id":145,"username":"bob","first_name":"Bob","last_name":"Durand"},
	{"id":146,"username":"","first_name":"Alice","last_name":"Durand"}],"meta":{"total":3}}`

func TestListGroupsSelectsAKindAndMapsTheGroups(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[
		{"id":3,"role":"DBA","privilege":"F","description":" Database team "},
		{"id":7,"role":"Manager","privilege":"T","description":""},
		{"id":40,"role":"user_139","privilege":"F","description":""},
		{"id":41,"role":"Legacy","privilege":null,"description":""}],"meta":{"total":5}}`}
	groups, err := newTestService(collector).ListGroups(t.Context(), ListGroupsOptions{Name: "a", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	query := collector.requests[0].query
	if collector.requests[0].path != "/groups" || strings.Join(query["filter"], " ") != `role:~a privilege:!eq:T role:!~^user_([0-9]+)$` ||
		query.Get("orderby") != "role,id" || strings.Contains(query.Get("props"), "email") {
		t.Fatalf("request = %+v", collector.requests[0])
	}
	if groups.Total != 5 || !groups.Truncated || groups.NextCursor == "" || groups.Groups[0].Description != "Database team" ||
		groups.Groups[0].Kind != "organization" || groups.Groups[1].Kind != "privilege" ||
		groups.Groups[2].Kind != "personal" || groups.Groups[2].UserID != 139 || groups.Groups[3].Kind != "organization" {
		t.Fatalf("groups = %+v", groups)
	}
	for kind, want := range map[string]string{"privilege": "privilege:eq:T", "personal": `role:~^user_([0-9]+)$`, "all": ""} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListGroups(t.Context(), ListGroupsOptions{Kind: kind}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(collector.requests[0].query["filter"], " "); got != want {
			t.Fatalf("kind %s filters = %q", kind, got)
		}
	}
}

func TestListGroupsOfAUserFoundByItsName(t *testing.T) {
	// A full name is looked up as a username, then split at each space in a
	// first and a last name, with collector filters: no user list is read.
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/users" {
			if strings.Join(query["filter"], " ") == `first_name:~^[[:space:]]*bob[[:space:]]*$ last_name:~^[[:space:]]*durand[[:space:]]*$` {
				return `{"data":[{"id":145,"username":"bob","first_name":"Bob","last_name":"Durand"}],"meta":{"total":1}}`
			}
			return `{"data":[],"meta":{"total":0}}`
		}
		return `{"data":[{"id":3,"role":"DBA","privilege":"F"}],"meta":{"total":1}}`
	}}
	groups, err := newTestService(collector).ListGroups(t.Context(), ListGroupsOptions{User: "bob  durand", Kind: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(collector.requests) != 3 || collector.requests[0].query.Get("filter") != `username:~^[[:space:]]*bob  durand[[:space:]]*$` ||
		collector.requests[2].path != "/users/145/groups" || groups.User == nil || groups.User.ID != 145 || groups.User.Name != "Bob Durand" {
		t.Fatalf("groups = %+v, requests %+v", groups, collector.requests)
	}
	// A name two users hold is refused with their IDs.
	collector = &fakeCollector{body: `{"data":[{"id":145,"first_name":"Alice","last_name":"Durand"},{"id":146,"first_name":"Alice","last_name":"Durand"}],"meta":{"total":2}}`}
	if _, err := newTestService(collector).ListGroups(t.Context(), ListGroupsOptions{User: "Alice Durand"}); err == nil || !strings.Contains(err.Error(), "145, 146") {
		t.Fatalf("ambiguous user: %v", err)
	}
	collector = &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListGroups(t.Context(), ListGroupsOptions{User: "nobody here"}); err == nil || !strings.Contains(err.Error(), "list_users") {
		t.Fatalf("unknown user: %v", err)
	}
	collector = &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListGroups(t.Context(), ListGroupsOptions{User: "a b c d e f g"}); err == nil || len(collector.requests) != 0 {
		t.Fatalf("long name: %v, %d requests", err, len(collector.requests))
	}
	// An ID or an email address is read directly.
	collector = &fakeCollector{answer: func(path string, _ url.Values) string {
		return `{"data":[{"id":145,"first_name":"Bob","last_name":"Durand"}],"meta":{"total":1}}`
	}}
	if _, err := newTestService(collector).ListGroups(t.Context(), ListGroupsOptions{User: "bob@example.com"}); err != nil || collector.requests[0].path != "/users/bob@example.com" {
		t.Fatalf("by email: %v, %+v", err, collector.requests)
	}
}

func TestGetGroupCountsWhatItIsResponsibleFor(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/groups/Test%20Ops":
			return `{"data":[{"id":12,"role":"Test Ops","privilege":"F","description":"Test operations"}]}`
		case "/groups/12/apps":
			return `{"data":[{"app":"DEV","description":"development"},{"app":"QA"}],"meta":{"total":2}}`
		case "/groups/12/users":
			return `{"data":[{"id":139}],"meta":{"total":9}}`
		case "/groups/12/nodes":
			return `{"data":[],"meta":{"total":0}}`
		case "/groups/12/services":
			return `{"data":[{"svc_id":"s-1"}],"meta":{"total":92}}`
		}
		return `{"data":[]}`
	}}
	group, err := newTestService(collector).GetGroup(t.Context(), "Test Ops")
	if err != nil {
		t.Fatal(err)
	}
	if group.Group.Name != "Test Ops" || group.Group.Kind != "organization" || group.User != nil || group.AppsTotal != 2 || len(group.Apps) != 2 ||
		group.Apps[0].Name != "DEV" || group.Members != 9 || group.Nodes != 0 || group.Services != 92 || group.AppsTruncated {
		t.Fatalf("group = %+v", group)
	}
	if collector.requests[1].query.Get("orderby") != "app" || collector.requests[2].query.Get("limit") != "1" {
		t.Fatalf("requests = %+v", collector.requests)
	}
}

func TestGetGroupOfAPersonalGroupGivesItsUser(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, _ url.Values) string {
		switch path {
		case "/groups/user_145":
			return `{"data":[{"id":40,"role":"user_145","privilege":"F"}]}`
		case "/users/145":
			return `{"data":[{"id":145,"username":"bob","first_name":"Bob","last_name":"Durand"}]}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	group, err := newTestService(collector).GetGroup(t.Context(), "user_145")
	if err != nil || group.Group.Kind != "personal" || group.User == nil || group.User.Name != "Bob Durand" {
		t.Fatalf("group = %+v, %v", group, err)
	}
	// A personal group whose user is deleted, or not visible, is answered
	// without its user.
	collector = &fakeCollector{answer: func(path string, _ url.Values) string {
		if path == "/groups/user_999" {
			return `{"data":[{"id":41,"role":"user_999","privilege":"F"}]}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	group, err = newTestService(collector).GetGroup(t.Context(), "user_999")
	if err != nil || group.User != nil || group.Group.UserID != 999 {
		t.Fatalf("orphan group = %+v, %v", group, err)
	}
	collector = &fakeCollector{body: `{"data":[]}`}
	if _, err := newTestService(collector).GetGroup(t.Context(), "nosuch"); err == nil || !strings.Contains(err.Error(), "list_groups") {
		t.Fatalf("unknown group: %v", err)
	}
	// The collector answers an unknown group with a 404.
	collector = &fakeCollector{err: fakeNotFound{}}
	if _, err := newTestService(collector).GetGroup(t.Context(), "nosuch"); err == nil || !strings.Contains(err.Error(), "list_groups") {
		t.Fatalf("unknown group, 404: %v", err)
	}
	for _, group := range []string{"", "a/b", " DBA"} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).GetGroup(t.Context(), group); err == nil || len(collector.requests) != 0 {
			t.Fatalf("group %q accepted", group)
		}
	}
}

func TestListUsersPagesAndSearchesNames(t *testing.T) {
	collector := &fakeCollector{body: groupUsers}
	collector.body = strings.Replace(groupUsers, `"total":3`, `"total":5`, 1)
	users, err := newTestService(collector).ListUsers(t.Context(), ListUsersOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	query := collector.requests[0].query
	if query.Get("props") != "id,username,first_name,last_name" || query.Get("orderby") != "last_name,first_name,id" || query.Get("limit") != "3" {
		t.Fatalf("query = %v", query)
	}
	if users.Total != 5 || !users.Truncated || users.Users[1].Username != "bob" {
		t.Fatalf("users = %+v", users)
	}

	// A name is searched in the first name, the last name and the username,
	// among the members of a group too.
	collector = &fakeCollector{answer: func(path string, _ url.Values) string {
		if path == "/groups/DBA" {
			return `{"data":[{"id":3,"role":"DBA","privilege":"F"}]}`
		}
		return groupUsers
	}}
	users, err = newTestService(collector).ListUsers(t.Context(), ListUsersOptions{Group: "DBA", Name: "durand", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[1].path != "/groups/3/users" || collector.requests[1].query.Get("limit") != "1000" || users.Group.Name != "DBA" ||
		users.Total != 2 || users.Count != 1 || !users.Truncated || users.Users[0].ID != 145 {
		t.Fatalf("users = %+v", users)
	}
	next, err := newTestService(collector).ListUsers(t.Context(), ListUsersOptions{Group: "DBA", Name: "durand", Limit: 1, Cursor: users.NextCursor})
	if err != nil || next.Users[0].ID != 146 || next.Truncated {
		t.Fatalf("next = %+v, %v", next, err)
	}
	users, err = newTestService(&fakeCollector{body: groupUsers}).ListUsers(t.Context(), ListUsersOptions{Name: "BOB"})
	if err != nil || users.Total != 1 {
		t.Fatalf("username search = %+v, %v", users, err)
	}
}

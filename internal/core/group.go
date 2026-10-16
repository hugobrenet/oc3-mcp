package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultListGroupsLimit = 50
	maxListGroupsLimit     = 200
	defaultListUsersLimit  = 50
	maxListUsersLimit      = 200
	maxGroupApps           = 50
	// maxUserNameScan bounds the users a name search reads: the collector
	// filters one column at a time, and a name is in any of three.
	maxUserNameScan = 1000
	// maxUserNameWords bounds the words of a full name a user is looked up
	// by: each split of it in first and last name is one request.
	maxUserNameWords = 6
)

// groupKinds are the kinds of collector groups list_groups selects:
// organization groups gather people, privilege groups grant rights, and
// the personal group of a user, user_<id>, holds that user alone.
var groupKinds = []string{"organization", "privilege", "personal", "all"}

var personalGroupPattern = regexp.MustCompile(`^user_([0-9]+)$`)

var (
	groupProps = []string{"id", "role", "privilege", "description"}
	userProps  = []string{"id", "username", "first_name", "last_name"}
)

type ListGroupsOptions struct {
	Name   string
	Kind   string
	User   string
	Limit  int
	Cursor string
}

type GroupList struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	User       *UserReference `json:"user,omitempty" jsonschema:"the user whose groups are listed"`
	Total      int            `json:"total" jsonschema:"the number of groups matching, as the collector counts them"`
	Count      int            `json:"count" jsonschema:"the number of groups returned in this page"`
	Groups     []Group        `json:"groups" jsonschema:"the groups sorted by name"`
	NextCursor string         `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool           `json:"truncated" jsonschema:"whether matching groups remain after this page"`
}

type Group struct {
	ID          int    `json:"id" jsonschema:"the collector group ID"`
	Name        string `json:"name" jsonschema:"the group name, as the responsibles of apps, forms and nodes give it"`
	Description string `json:"description,omitempty" jsonschema:"the description of the group; untrusted text"`
	Kind        string `json:"kind" jsonschema:"organization for a team of people, privilege for a group granting collector rights, such as Manager or NodeManager, personal for the own group of one user"`
	UserID      int    `json:"user_id,omitempty" jsonschema:"for a personal group, the ID of its user, as list_users gives it"`
}

type UserReference struct {
	ID   int    `json:"id" jsonschema:"the collector user ID"`
	Name string `json:"name" jsonschema:"the first and last name of the user, or the username without one"`
}

type collectorGroup struct {
	ID          int    `json:"id"`
	Role        string `json:"role"`
	Privilege   string `json:"privilege"`
	Description string `json:"description"`
}

type collectorUser struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// ListGroups reads one page of the collector groups the caller sees, every
// group for a manager, of one kind, or the groups of one user.
func (s *Service) ListGroups(ctx context.Context, options ListGroupsOptions) (GroupList, error) {
	if err := validateFilterValue("name", options.Name); err != nil {
		return GroupList{}, err
	}
	kind := options.Kind
	if kind == "" {
		kind = "organization"
	}
	if !slices.Contains(groupKinds, kind) {
		return GroupList{}, fmt.Errorf("kind must be one of %s", strings.Join(groupKinds, ", "))
	}
	limit, err := pageLimit(options.Limit, defaultListGroupsLimit, maxListGroupsLimit)
	if err != nil {
		return GroupList{}, err
	}
	result := GroupList{}
	path := "/groups"
	if options.User != "" {
		user, err := s.lookupUser(ctx, options.User)
		if err != nil {
			return GroupList{}, err
		}
		result.User = &UserReference{ID: user.ID, Name: userName(user)}
		path = "/users/" + strconv.Itoa(user.ID) + "/groups"
	}
	var filters []string
	if options.Name != "" {
		filters = append(filters, filterContains("role", options.Name))
	}
	switch kind {
	case "organization":
		// The negation keeps the groups whose privilege is NULL, which
		// groupOf reads as organization groups, as privilege:eq:F would not.
		filters = append(filters, "privilege:!eq:T", "role:!~"+personalGroupPattern.String())
	case "privilege":
		filters = append(filters, filterEquals("privilege", "T"))
	case "personal":
		filters = append(filters, "role:~"+personalGroupPattern.String())
	}
	fingerprint := listFingerprint(url.Values{"path": {path}, "filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return GroupList{}, err
	}

	var response collectorList[collectorGroup]
	if err := s.client.GetJSON(ctx, path, pageQuery(groupProps, "role,id", filters, limit, offset), &response); err != nil {
		return GroupList{}, fmt.Errorf("list groups: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return GroupList{}, fmt.Errorf("list groups: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Groups = make([]Group, 0, len(response.Data))
	for _, row := range response.Data {
		result.Groups = append(result.Groups, groupOf(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

type GroupDetails struct {
	Provenance    Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Group         Group          `json:"group" jsonschema:"the group"`
	User          *UserReference `json:"user,omitempty" jsonschema:"for a personal group, its user; absent when the user is deleted or not visible to the caller"`
	Members       int            `json:"members" jsonschema:"the number of users in the group: list them with list_users and group"`
	Apps          []GroupApp     `json:"apps" jsonschema:"the apps the group is responsible for, sorted by code, at most 50"`
	AppsTotal     int            `json:"apps_total" jsonschema:"the number of apps the group is responsible for"`
	AppsTruncated bool           `json:"apps_truncated,omitempty" jsonschema:"whether apps were left out beyond 50"`
	Nodes         int            `json:"nodes" jsonschema:"the number of nodes whose responsible team is exactly this group: list them with list_nodes and team, whose filter is a text the team contains and may also list the nodes of a team whose name holds this one, such as DBA2 for DBA"`
	Services      int            `json:"services" jsonschema:"the number of services of the apps of the group: list them with list_services and app"`
}

type GroupApp struct {
	Name        string `json:"name" jsonschema:"the app code"`
	Description string `json:"description,omitempty" jsonschema:"the description of the app; untrusted text"`
}

// GetGroup reads one collector group by its name or ID, with what it is
// responsible for: its apps, the nodes of its team and the services of its
// apps, and the number of its members.
func (s *Service) GetGroup(ctx context.Context, group string) (GroupDetails, error) {
	if group == "" || strings.Contains(group, "/") {
		return GroupDetails{}, fmt.Errorf("group must be one exact group name or group ID")
	}
	if err := validateFilterValue("group", group); err != nil {
		return GroupDetails{}, err
	}
	found, err := s.lookupGroup(ctx, group)
	if err != nil {
		return GroupDetails{}, err
	}
	result := GroupDetails{Group: groupOf(found)}
	path := "/groups/" + strconv.Itoa(found.ID)
	if result.Group.UserID != 0 {
		// The group outlives a deleted user, and its user may not be
		// visible to the caller: the group is answered without it.
		if user, err := s.lookupUser(ctx, strconv.Itoa(result.Group.UserID)); err == nil {
			result.User = &UserReference{ID: user.ID, Name: userName(user)}
		}
	}

	var apps collectorList[collectorApp]
	if err := s.client.GetJSON(ctx, path+"/apps", url.Values{"props": {"app,description"}, "orderby": {"app"}, "limit": {strconv.Itoa(maxGroupApps)}}, &apps); err != nil {
		return GroupDetails{}, fmt.Errorf("get apps of group %s: %w", found.Role, err)
	}
	if apps.Meta.Total == nil || *apps.Meta.Total < 0 || len(apps.Data) > maxGroupApps {
		return GroupDetails{}, fmt.Errorf("get apps of group %s: unexpected collector response", found.Role)
	}
	result.AppsTotal, result.AppsTruncated = *apps.Meta.Total, *apps.Meta.Total > len(apps.Data)
	result.Apps = make([]GroupApp, 0, len(apps.Data))
	for _, app := range apps.Data {
		result.Apps = append(result.Apps, GroupApp{Name: app.App, Description: app.Description})
	}
	for _, count := range []struct {
		what, idProp string
		into         *int
	}{{"users", "id", &result.Members}, {"nodes", "node_id", &result.Nodes}, {"services", "svc_id", &result.Services}} {
		if *count.into, _, _, _, err = s.countRows(ctx, path+"/"+count.what, count.idProp, nil, "", 0); err != nil {
			return GroupDetails{}, fmt.Errorf("count %s of group %s: %w", count.what, found.Role, err)
		}
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// lookupGroup reads one group by its exact name or its ID.
func (s *Service) lookupGroup(ctx context.Context, group string) (collectorGroup, error) {
	var response collectorList[collectorGroup]
	err := s.client.GetJSON(ctx, "/groups/"+url.PathEscape(group), url.Values{"props": {strings.Join(groupProps, ",")}}, &response)
	if err != nil && !isNotFound(err) {
		return collectorGroup{}, fmt.Errorf("get group %s: %w", group, err)
	}
	if err != nil || len(response.Data) != 1 {
		return collectorGroup{}, fmt.Errorf("get group %s: not found in the collector, or not visible to the caller; list the groups with list_groups and name", group)
	}
	return response.Data[0], nil
}

type ListUsersOptions struct {
	Group  string
	Name   string
	Limit  int
	Cursor string
}

type UserList struct {
	Provenance    Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Group         *Group     `json:"group,omitempty" jsonschema:"the group whose members are listed"`
	Total         int        `json:"total" jsonschema:"the number of users matching, as the collector counts them, or with name as the MCP counts them among those it read"`
	ScanTruncated bool       `json:"scan_truncated,omitempty" jsonschema:"with name, whether the MCP read only the first 1000 users, so that other users may match the name"`
	Count         int        `json:"count" jsonschema:"the number of users returned in this page"`
	Users         []User     `json:"users" jsonschema:"the users sorted by last name, then first name"`
	NextCursor    string     `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated     bool       `json:"truncated" jsonschema:"whether matching users remain after this page"`
}

type User struct {
	ID        int    `json:"id" jsonschema:"the collector user ID; user_ followed by this ID names the personal group of the user"`
	Username  string `json:"username,omitempty" jsonschema:"the login name of the user, when set"`
	FirstName string `json:"first_name,omitempty" jsonschema:"the first name"`
	LastName  string `json:"last_name,omitempty" jsonschema:"the last name"`
}

// ListUsers reads one page of the collector users the caller sees, or of the
// members of one group, by their name. Their email address and phone number
// are not read.
func (s *Service) ListUsers(ctx context.Context, options ListUsersOptions) (UserList, error) {
	if err := validateFilterValue("name", options.Name); err != nil {
		return UserList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListUsersLimit, maxListUsersLimit)
	if err != nil {
		return UserList{}, err
	}
	result := UserList{}
	path := "/users"
	if options.Group != "" {
		if strings.Contains(options.Group, "/") {
			return UserList{}, fmt.Errorf("group must be one exact group name or group ID")
		}
		if err := validateFilterValue("group", options.Group); err != nil {
			return UserList{}, err
		}
		found, err := s.lookupGroup(ctx, options.Group)
		if err != nil {
			return UserList{}, err
		}
		group := groupOf(found)
		result.Group = &group
		path = "/groups/" + strconv.Itoa(found.ID) + "/users"
	}
	fingerprint := listFingerprint(url.Values{"path": {path}, "name": {strings.ToLower(options.Name)}})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return UserList{}, err
	}
	const order = "last_name,first_name,id"
	if options.Name != "" {
		return s.searchUsers(ctx, result, path, options.Name, limit, offset, fingerprint)
	}

	var response collectorList[collectorUser]
	if err := s.client.GetJSON(ctx, path, pageQuery(userProps, order, nil, limit, offset), &response); err != nil {
		return UserList{}, fmt.Errorf("list users: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return UserList{}, fmt.Errorf("list users: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Users = make([]User, 0, len(response.Data))
	for _, row := range response.Data {
		result.Users = append(result.Users, User(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// searchUsers reads the users of path and keeps those whose first name,
// last name or username holds the name: the collector filters one column at
// a time, and the group members not at all.
func (s *Service) searchUsers(ctx context.Context, result UserList, path, name string, limit, offset int, fingerprint string) (UserList, error) {
	var response collectorList[collectorUser]
	if err := s.client.GetJSON(ctx, path, pageQuery(userProps, "last_name,first_name,id", nil, maxUserNameScan, 0), &response); err != nil {
		return UserList{}, fmt.Errorf("list users: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > maxUserNameScan {
		return UserList{}, fmt.Errorf("list users: unexpected collector response")
	}
	var matching []User
	for _, row := range response.Data {
		if userMatches(row, name) {
			matching = append(matching, User(row))
		}
	}
	result.Provenance = s.newProvenance()
	result.Total, result.ScanTruncated = len(matching), *response.Meta.Total > len(response.Data)
	end := min(offset+limit, len(matching))
	result.Users = []User{}
	if offset < len(matching) {
		result.Users = matching[offset:end]
	}
	result.Count = len(result.Users)
	if end < len(matching) && result.Count > 0 {
		result.NextCursor = encodeListCursor(end, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// lookupUser reads one user by its ID, its email address, which the caller
// gives and no tool returns, its username, or its first and last name.
func (s *Service) lookupUser(ctx context.Context, user string) (collectorUser, error) {
	if err := validateFilterValue("user", user); err != nil {
		return collectorUser{}, err
	}
	if _, err := strconv.Atoi(user); err == nil || strings.Contains(user, "@") {
		var response collectorList[collectorUser]
		err := s.client.GetJSON(ctx, "/users/"+url.PathEscape(user), url.Values{"props": {strings.Join(userProps, ",")}}, &response)
		if err != nil && !isNotFound(err) {
			return collectorUser{}, fmt.Errorf("get user %s: %w", user, err)
		}
		if err != nil || len(response.Data) != 1 {
			return collectorUser{}, fmt.Errorf("get user %s: not found in the collector, or not visible to the caller; find the user with list_users and name", user)
		}
		return response.Data[0], nil
	}
	// A username, or a first and last name split at each space: the
	// collector filters each column, never the three of them together.
	words := strings.Fields(user)
	if len(words) > maxUserNameWords {
		return collectorUser{}, fmt.Errorf("user must be a user ID, a username, or a first and last name of at most %d words", maxUserNameWords)
	}
	candidates := [][]string{{filterEqualsTrimmed("username", user)}}
	for i := 1; i < len(words); i++ {
		candidates = append(candidates, []string{
			filterEqualsTrimmed("first_name", strings.Join(words[:i], " ")),
			filterEqualsTrimmed("last_name", strings.Join(words[i:], " ")),
		})
	}
	var found []collectorUser
	for _, filters := range candidates {
		var response collectorList[collectorUser]
		if err := s.client.GetJSON(ctx, "/users", pageQuery(userProps, "last_name,first_name,id", filters, maxListUsersLimit, 0), &response); err != nil {
			return collectorUser{}, fmt.Errorf("get user %s: %w", user, err)
		}
		for _, row := range response.Data {
			if !slices.ContainsFunc(found, func(other collectorUser) bool { return other.ID == row.ID }) {
				found = append(found, row)
			}
		}
	}
	switch len(found) {
	case 0:
		return collectorUser{}, fmt.Errorf("get user %s: no user has this username or this first and last name; find the user with list_users and name", user)
	case 1:
		return found[0], nil
	default:
		ids := make([]string, 0, len(found))
		for _, row := range found {
			ids = append(ids, strconv.Itoa(row.ID))
		}
		return collectorUser{}, fmt.Errorf("get user %s: the name matches %d users, pass one user ID: %s", user, len(ids), strings.Join(ids, ", "))
	}
}

func groupOf(row collectorGroup) Group {
	group := Group{ID: row.ID, Name: row.Role, Description: strings.TrimSpace(row.Description), Kind: "organization"}
	switch {
	case personalGroupPattern.MatchString(row.Role):
		group.Kind = "personal"
		group.UserID, _ = strconv.Atoi(personalGroupPattern.FindStringSubmatch(row.Role)[1])
	case row.Privilege == "T":
		group.Kind = "privilege"
	}
	return group
}

// userName is the first and last name of a user, or its username without
// one.
func userName(user collectorUser) string {
	if name := strings.Join(strings.Fields(user.FirstName+" "+user.LastName), " "); name != "" {
		return name
	}
	return strings.TrimSpace(user.Username)
}

// userMatches tells whether name is in the first name, the last name, the
// full name or the username of the user, case-insensitively.
func userMatches(user collectorUser, name string) bool {
	needle := strings.ToLower(strings.Join(strings.Fields(name), " "))
	for _, field := range []string{user.FirstName, user.LastName, userName(user), user.Username} {
		if strings.Contains(strings.ToLower(strings.TrimSpace(field)), needle) {
			return true
		}
	}
	return false
}

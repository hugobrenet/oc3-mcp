package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// appWriteCollector holds the app WEB, 4627, and answers the changes with
// writes, or the error of refusals.
type appWriteCollector struct {
	*fakeCollector
	refusals map[string]error
}

func newAppWriteCollector(after string, refusals map[string]error) appWriteCollector {
	read := 0
	return appWriteCollector{fakeCollector: &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/apps/4627/am_i_responsible":
			return `true`
		case "/apps/WEB", "/apps/4627", "/apps/WEB2":
			read++
			if read > 1 && after != "" {
				return after
			}
			return `{"app":"WEB","id":4627,"description":"web","app_domain":"","app_team_ops":""}`
		case "/apps/4627/responsibles":
			return `{"data":[{"role":"Test Web"}]}`
		case "/apps/4627/publications":
			return `{"data":[{"role":"Test Web"}]}`
		case "/nodes", "/services":
			return `{"data":[],"meta":{"total":0}}`
		}
		return `{"data":[]}`
	}}, refusals: refusals}
}

func (c appWriteCollector) PostJSON(ctx context.Context, path string, input any, output any) error {
	_ = c.fakeCollector.PostJSON(ctx, path, input, nil)
	if err := c.refusals[path]; err != nil {
		return err
	}
	return json.Unmarshal([]byte(`{"info":"ok"}`), output)
}

func (c appWriteCollector) DeleteJSON(ctx context.Context, path string, query url.Values, output any) error {
	_ = c.fakeCollector.DeleteJSON(ctx, path, query, nil)
	if err := c.refusals[path]; err != nil {
		return err
	}
	return json.Unmarshal([]byte(`{"info":"ok"}`), output)
}

func (c appWriteCollector) changes() []fakeRequest {
	var changes []fakeRequest
	for _, request := range c.requests {
		if request.method != "GET" {
			changes = append(changes, request)
		}
	}
	return changes
}

func TestCreateAndUpdateApp(t *testing.T) {
	collector := newAppWriteCollector("", nil)
	domain := "web"
	result, err := New(collector).CreateApp(t.Context(), "WEB", AppFields{Domain: &domain})
	changes := collector.changes()
	if err != nil || len(changes) != 1 || changes[0].path != "/apps" || changes[0].body["app"] != "WEB" || changes[0].body["app_domain"] != "web" || len(changes[0].body) != 2 ||
		result.Outcome != AppCreated || result.App == nil || len(result.App.Publications) != 1 {
		t.Fatalf("create: %+v, %v, %+v", result, err, changes)
	}
	// A new code: the app read back after the change, by its ID.
	collector = newAppWriteCollector(`{"app":"WEB2","id":4627,"description":"web"}`, nil)
	name := "WEB2"
	result, err = New(collector).UpdateApp(t.Context(), "WEB", AppFields{Name: &name})
	changes = collector.changes()
	if err != nil || len(changes) != 1 || changes[0].path != "/apps/4627" || changes[0].body["app"] != "WEB2" || result.Outcome != AppChanged || result.App.Name != "WEB2" {
		t.Fatalf("update: %+v, %v, %+v", result, err, changes)
	}
	collector = newAppWriteCollector("", nil)
	result, err = New(collector).UpdateApp(t.Context(), "WEB", AppFields{})
	if err != nil || result.Outcome != AppUnchanged {
		t.Fatalf("unchanged: %+v, %v", result, err)
	}
}

func TestDeleteAppTellsTheRefusal(t *testing.T) {
	collector := newAppWriteCollector("", map[string]error{"/apps/4627": statusError{http.StatusConflict, "this app code cannot be deleted. used by 3 nodes and 0 services"}})
	if _, err := New(collector).DeleteApp(t.Context(), "WEB"); err == nil || !strings.Contains(err.Error(), "used by 3 nodes") {
		t.Fatalf("delete used: %v", err)
	}
	collector = newAppWriteCollector("", nil)
	result, err := New(collector).DeleteApp(t.Context(), "WEB")
	if err != nil || result.Outcome != AppDeleted || result.App.Name != "WEB" || collector.changes()[0].method != "DELETE" {
		t.Fatalf("delete: %+v, %v", result, err)
	}
}

func TestChangeAppGroupsReadsTheOutcomes(t *testing.T) {
	collector := newAppWriteCollector("", map[string]error{
		"/apps/4627/responsibles/Test%20Web": statusError{http.StatusConflict, "group Test Web is already responsible for app WEB"},
		"/apps/4627/responsibles/Manager":    statusError{http.StatusForbidden, "operation not allowed on privileged group: Manager"},
	})
	result, err := New(collector).AttachAppGroups(t.Context(), AppGroupOptions{App: "WEB", Responsibles: []string{"Test Web", "Manager", "Ops"}, Publications: []string{"Ops"}})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := []string{}
	for _, group := range result.Groups {
		outcomes = append(outcomes, group.Role+":"+group.Group+":"+group.Outcome)
	}
	if strings.Join(outcomes, ",") != "responsible:Test Web:already_attached,responsible:Manager:refused,responsible:Ops:attached,publication:Ops:attached" ||
		result.Changed != 2 || result.Unchanged != 1 || result.Refused != 1 || !strings.Contains(result.Groups[1].Reason, "privileged group") {
		t.Fatalf("attach: %+v", result)
	}
	// A detachment of a group not attached is already_detached; the
	// responsibles left are read back.
	collector = newAppWriteCollector("", map[string]error{"/apps/4627/publications/Ops": statusError{http.StatusNotFound, "app WEB is not published to group Ops"}})
	result, err = New(collector).DetachAppGroups(t.Context(), AppGroupOptions{App: "WEB", Responsibles: []string{"Ops"}, Publications: []string{"Ops"}})
	if err != nil || result.Groups[0].Outcome != ChangeDetached || result.Groups[1].Outcome != ChangeAlreadyDetached || strings.Join(result.Responsibles, ",") != "Test Web" || result.Warning != "" {
		t.Fatalf("detach: %+v, %v", result, err)
	}
	if _, err := New(newAppWriteCollector("", nil)).AttachAppGroups(t.Context(), AppGroupOptions{App: "WEB"}); err == nil {
		t.Fatal("no group accepted")
	}
}

// The app is read back by the ID the collector answers with; a caller who
// withdrew their own group is told so.
func TestAppWritesReadBackAndSelfExclusion(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/apps/4627":
			return `{"app":"a/b","id":4627}`
		case "/apps/4627/responsibles", "/apps/4627/publications":
			return `{"data":[{"role":"Ops"}]}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	posting := appWriteCollector{fakeCollector: collector}
	// The fake answers the creation as the collector does, with the app.
	result, err := New(createAnswer{posting}).CreateApp(t.Context(), "a/b", AppFields{})
	if err != nil || result.App == nil || result.App.Name != "a/b" || result.Warning != "" {
		t.Fatalf("create: %+v, %v", result, err)
	}
	refusing := appWriteCollector{fakeCollector: &fakeCollector{answer: collector.answer}}
	change, err := New(selfExcluded{refusing}).DetachAppGroups(t.Context(), AppGroupOptions{App: "4627", Responsibles: []string{"Lab"}})
	if err != nil || !strings.Contains(change.Warning, "the caller is no longer responsible for app a/b") {
		t.Fatalf("detach: %+v, %v", change, err)
	}
}

type createAnswer struct{ appWriteCollector }

func (c createAnswer) PostJSON(ctx context.Context, path string, input any, output any) error {
	_ = c.fakeCollector.PostJSON(ctx, path, input, nil)
	return json.Unmarshal([]byte(`{"app":"a/b","id":4627}`), output)
}

type selfExcluded struct{ appWriteCollector }

func (c selfExcluded) GetJSON(ctx context.Context, path string, query url.Values, output any) error {
	if strings.HasSuffix(path, "/am_i_responsible") {
		return statusError{http.StatusForbidden, "you are not responsible for app 4627"}
	}
	return c.fakeCollector.GetJSON(ctx, path, query, output)
}

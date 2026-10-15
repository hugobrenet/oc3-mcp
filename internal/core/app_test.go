package core

import (
	"net/url"
	"testing"
)

func TestGetAppReadsItsResponsiblesAndCounts(t *testing.T) {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		switch path {
		case "/apps/web":
			return `{"app":"WEB","app_domain":"","app_team_ops":"","description":"WEB application code","id":4627,"updated":"2026-09-15 15:28:58"}`
		case "/apps/4627/responsibles":
			return `{"data":[{"role":"Test Web","description":"Test team responsible for the WEB app"},{"role":"user_1","description":"group uniquely assigned to user 1"}]}`
		case "/nodes":
			if query.Get("filter") == "app:eq:WEB" {
				return `{"data":[{}],"meta":{"total":6}}`
			}
		case "/services":
			if query.Get("filter") == "svc_app:eq:WEB" {
				return `{"data":[{}],"meta":{"total":4}}`
			}
		}
		return `{}`
	}
	app, err := newTestService(collector).GetApp(t.Context(), "web")
	if err != nil {
		t.Fatal(err)
	}
	if app.Name != "WEB" || app.ID != 4627 || app.Description != "WEB application code" || len(app.Responsibles) != 2 || app.Responsibles[0].Role != "Test Web" ||
		app.ResponsiblesTruncated || app.Nodes != 6 || app.Services != 4 || app.Provenance.Source != provenanceSourceOpenSVCCollector {
		t.Fatalf("app = %+v", app)
	}
	if collector.requests[1].query.Get("limit") != "101" {
		t.Fatalf("responsibles request = %+v", collector.requests[1])
	}
}

func TestGetAppRejectsInvalidInputs(t *testing.T) {
	for _, app := range []string{"", "a/b", " padded"} {
		collector := &fakeCollector{body: `{}`}
		if _, err := newTestService(collector).GetApp(t.Context(), app); err == nil || len(collector.requests) != 0 {
			t.Fatalf("app %q accepted", app)
		}
	}
	collector := &fakeCollector{body: `{}`}
	if _, err := newTestService(collector).GetApp(t.Context(), "WEB"); err == nil {
		t.Fatal("an empty collector answer was accepted")
	}
}

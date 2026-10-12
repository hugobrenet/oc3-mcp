package core

import (
	"net/url"
	"strings"
	"testing"
)

const testNodeUUID = "1c62e2f5-41a8-43ac-af36-60f3c8d1ffbc"
const testSvcUUID = "fbb73ffd-bb5b-11f1-b044-f25347bc8ed3"

func TestListCollectorEventsReadsTheLog(t *testing.T) {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		switch path {
		case "/logs":
			return `{"data":[
				{"id":2,"log_date":"2026-10-11 23:03:28","log_action":"service.status","log_level":"error","log_user":"scheduler",
				 "log_fmt":"service '%(svc)s' has zero live instance. Status flagged 'undef'","log_dict":"{\"svc\":\"test/svc/zfs30\"}","svc_id":"` + testSvcUUID + `"},
				{"id":1,"log_date":"2026-10-07 12:42:33","log_action":"node.change","log_level":"info","log_user":"admin@example.com","log_impersonator":"root@example.com",
				 "log_fmt":"change properties %(data)s on %(count)d nodes %(missing)s","log_dict":"{\"data\":{\"loc_building\":\"b1\"},\"count\":3}","node_id":"` + testNodeUUID + `"}],
				"meta":{"total":5}}`
		case "/nodes":
			return `{"data":[{"node_id":"` + testNodeUUID + `","nodename":"dev2n1"}]}`
		case "/services":
			return `{"data":[{"svc_id":"` + testSvcUUID + `","svcname":"test/svc/zfs30"}]}`
		}
		return `{}`
	}
	events, err := newTestService(collector).ListCollectorEvents(t.Context(), ListCollectorEventsOptions{Action: "service.status", Level: "error", User: "scheduler", Since: "2026-10-01", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	wantFilters := "log_action:!in:user.login,user.logout|log_action:eq:service.status|log_level:eq:error|log_user:eq:scheduler|log_date:gte:2026-10-01"
	if request.path != "/logs" || request.query.Get("orderby") != "-log_date,-id" || strings.Join(request.query["filter"], "|") != wantFilters ||
		strings.Contains(request.query.Get("props"), "log_entry_id") {
		t.Fatalf("request = %+v", request)
	}
	if names := collector.requests[1]; names.path != "/nodes" || names.query.Get("filter") != "node_id:in:"+testNodeUUID {
		t.Fatalf("names request = %+v", names)
	}
	status, change := events.Events[0], events.Events[1]
	if events.Total != 5 || !events.Truncated || events.NextCursor == "" ||
		status.Message != "service 'test/svc/zfs30' has zero live instance. Status flagged 'undef'" || status.Svcname != "test/svc/zfs30" || status.User != "scheduler" {
		t.Fatalf("status = %+v", status)
	}
	if change.Message != `change properties {"loc_building":"b1"} on 3 nodes %(missing)s` || change.Nodename != "dev2n1" || change.Impersonator != "root@example.com" ||
		change.Data["count"] != float64(3) {
		t.Fatalf("change = %+v", change)
	}
}

func TestListCollectorEventsResolvesTheNodeAndTheService(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": `{"data":[{"node_id":"` + testNodeUUID + `","nodename":"dev2n1"}]}`,
		"/services/web": `{"data":[{"svc_id":"` + testSvcUUID + `","svcname":"web"}]}`,
		"/logs":         `{"data":[],"meta":{"total":0}}`,
	}}
	events, err := newTestService(collector).ListCollectorEvents(t.Context(), ListCollectorEventsOptions{Node: "dev2n1", Service: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if filters := strings.Join(collector.requests[2].query["filter"], "|"); filters != "log_action:!in:user.login,user.logout|node_id:eq:"+testNodeUUID+"|svc_id:eq:"+testSvcUUID ||
		events.Node == nil || events.Service == nil || events.Events == nil || len(collector.requests) != 3 || collector.requests[2].query.Get("limit") != "50" {
		t.Fatalf("events = %+v, requests %+v", events, collector.requests)
	}
}

func TestRenderCollectorEvent(t *testing.T) {
	for _, test := range []struct{ template, data, want string }{
		{"run %(action)s", `{"action":"thaw"}`, "run thaw"},
		{"Alert %(id)s deleted", `{"id":42}`, "Alert 42 deleted"},
		{"signed out", `{}`, "signed out"},
		{"broken %(x)s", `not json`, "broken %(x)s"},
		{"literal 100%", ``, "literal 100%"},
	} {
		if got, _ := renderCollectorEvent(test.template, test.data); got != test.want {
			t.Fatalf("renderCollectorEvent(%q, %q) = %q, want %q", test.template, test.data, got, test.want)
		}
	}
	long, _ := renderCollectorEvent("%(x)s", `{"x":"`+strings.Repeat("é", maxCollectorEventMessageBytes)+`"}`)
	if len(long) > maxCollectorEventMessageBytes || !strings.HasSuffix(long, "é") {
		t.Fatalf("long message = %d bytes", len(long))
	}
}

func TestListCollectorEventsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListCollectorEventsOptions{
		{Level: "fatal"},
		{Since: "yesterday"},
		{Limit: 201},
		{Action: " service.status"},
		{Node: "a/b"},
		{Cursor: "garbage"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListCollectorEvents(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestListCollectorEventsFiltersAnActionFamily(t *testing.T) {
	for action, want := range map[string]string{
		"service.":        `log_action:~^service\.`,
		"compliance.rule": "log_action:eq:compliance.rule",
		"service.action":  "log_action:eq:service.action",
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListCollectorEvents(t.Context(), ListCollectorEventsOptions{Action: action}); err != nil {
			t.Fatal(err)
		}
		if got := collector.requests[0].query["filter"][1]; got != want {
			t.Fatalf("action %q filter = %q, want %q", action, got, want)
		}
	}
}

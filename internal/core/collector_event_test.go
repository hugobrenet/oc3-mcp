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
	if change.Message != `change properties {"loc_building":"b1"} on 3 nodes %(missing)s` || change.Nodename != "dev2n1" || change.Impersonator != maskedRequestValue || change.User != maskedRequestValue ||
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
		{Origin: "browser"},
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

// TestListCollectorEventsTellsTheOrigin reads the client an action was made
// through, and filters on it.
func TestListCollectorEventsTellsTheOrigin(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[
		{"id":3,"log_date":"2026-11-10 12:00:00","log_action":"service.action","log_level":"info","log_user":"scheduler",
		 "log_origin":"assistant","log_request_id":"0123456789abcdef0123456789abcdef","log_fmt":"restart","log_dict":"{}"}],
		"meta":{"total":1}}`}
	events, err := newTestService(collector).ListCollectorEvents(t.Context(), ListCollectorEventsOptions{Origin: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if got := request.query["filter"][1]; got != "log_origin:eq:assistant" {
		t.Fatalf("origin filter = %q", got)
	}
	if props := request.query.Get("props"); !strings.Contains(props, "log_origin") || !strings.Contains(props, "log_request_id") {
		t.Fatalf("props = %q", props)
	}
	if event := events.Events[0]; event.Origin != "assistant" || event.RequestID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("event = %+v", event)
	}
}

// TestListCollectorEventsMasksTheMessageAndTheData checks that the events
// mask what the other read tools mask: the email addresses the collector logs
// for the administration of users, the values of sensitive keys, and the
// output lines of submitted forms, which only get_form_result can mask.
func TestListCollectorEventsMasksTheMessageAndTheData(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{"/logs": `{"data":[
		{"id":3,"log_date":"2026-10-11 10:00:00","log_action":"user.group.attach","log_level":"info","log_user":"admin@example.com",
		 "log_fmt":"user %(u)s attached to group %(g)s","log_dict":"{\"u\":\"alice.dummy@example.com\",\"g\":\"DBA\"}"},
		{"id":2,"log_date":"2026-10-11 09:00:00","log_action":"users.groups.sync","log_level":"info","log_user":"scheduler",
		 "log_fmt":"aligned the teams of bob.dummy@example.com: %(detail)s","log_dict":"{\"detail\":{\"password\":\"DummyPass123\",\"joined\":[\"DBA\"]}}"},
		{"id":1,"log_date":"2026-10-11 08:00:00","log_action":"form.submit","log_level":"info","log_user":"carol@example.com",
		 "log_fmt":"created account svc_dummy with password DummyPass123\n","log_dict":"{}"}],
		"meta":{"total":3}}`}}
	events, err := newTestService(collector).ListCollectorEvents(t.Context(), ListCollectorEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attach, sync, form := events.Events[0], events.Events[1], events.Events[2]
	if attach.Message != "user (masked) attached to group DBA" || attach.Data["u"] != maskedRequestValue || attach.Data["g"] != "DBA" {
		t.Fatalf("attach = %+v", attach)
	}
	if strings.Contains(sync.Message, "@") || strings.Contains(sync.Message, "DummyPass123") || !strings.Contains(sync.Message, `"joined":["DBA"]`) {
		t.Fatalf("sync = %+v", sync)
	}
	if detail, _ := sync.Data["detail"].(map[string]any); detail["password"] != maskedRequestValue {
		t.Fatalf("sync data = %+v", sync.Data)
	}
	if strings.Contains(form.Message, "DummyPass123") || !strings.Contains(form.Message, "get_form_result") || form.Data != nil {
		t.Fatalf("form = %+v", form)
	}
}

// TestListCollectorEventsLeavesOutLargeData keeps the data of an event within
// the bound of its message.
func TestListCollectorEventsLeavesOutLargeData(t *testing.T) {
	large := strings.Repeat("x", maxCollectorEventMessageBytes)
	collector := &fakeCollector{bodies: map[string]string{"/logs": `{"data":[
		{"id":1,"log_date":"2026-10-11 08:00:00","log_action":"node.change","log_level":"info","log_user":"scheduler",
		 "log_fmt":"changed","log_dict":"{\"value\":\"` + large + `\"}"}],"meta":{"total":1}}`}}
	events, err := newTestService(collector).ListCollectorEvents(t.Context(), ListCollectorEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if events.Events[0].Data != nil || events.Events[0].Message != "changed" {
		t.Fatalf("event = %+v", events.Events[0])
	}
}

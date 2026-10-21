package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// ackCollector answers the acknowledgement of each action with the info the
// collector gives, refuses the action 4 as the collector does a service the
// caller is not responsible for, and gives the action back acknowledged.
type ackCollector struct{ *fakeCollector }

func (c ackCollector) PostJSON(_ context.Context, path string, input any, output any) error {
	c.record(fakeRequest{method: "POST", path: path})
	if path == "/services_actions/4" {
		return errString("HTTP 403 Forbidden: user is not responsible for the service of action 4")
	}
	return c.reply(path, nil, output)
}

func newAckCollector() ackCollector {
	return ackCollector{fakeCollector: &fakeCollector{answer: func(path string, query url.Values) string {
		if query != nil {
			return `{"data":[{"id":1,"status":"err","ack":1,"acked_by":"jane@example.com","acked_date":"2026-10-19 16:40:00","acked_comment":"known issue, see jane@example.com","services.svcname":"web"}]}`
		}
		return map[string]string{
			"/services_actions/1": `{"info":"action 1 acknowledged"}`,
			"/services_actions/2": `{"info":"skip acknowledge of action 2: already acknowledged"}`,
			"/services_actions/3": `{"info":"skip acknowledge of action 3 in status 'ok'"}`,
		}[path]
	}}}
}

func TestAckServiceActionsGivesTheOutcomeOnEachAction(t *testing.T) {
	collector := newAckCollector()
	result, err := New(collector).AckServiceActions(t.Context(), AckServiceActionsOptions{IDs: []int64{1, 2, 3, 4, 1}, Comment: "  known issue  "})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := []string{}
	for _, check := range result.Actions {
		outcomes = append(outcomes, check.Outcome)
	}
	if strings.Join(outcomes, ",") != "acknowledged,already_acknowledged,not_failed,refused" || result.Acknowledged != 1 || result.Unchanged != 2 || result.Refused != 1 ||
		!strings.Contains(result.Actions[3].Reason, "not responsible") {
		t.Fatalf("result = %+v", result)
	}
	// The acknowledgement as the collector recorded it, its emails masked.
	action := result.Actions[0].Action
	if action == nil || !action.Acknowledged || action.AckedBy != maskedRequestValue || strings.Contains(action.AckedComment, "@") || action.AckedAt != "2026-10-19 16:40:00" {
		t.Fatalf("action = %+v", action)
	}
	for _, test := range []struct {
		ids     []int64
		comment string
	}{{nil, "x"}, {make([]int64, 51), "x"}, {[]int64{0}, "x"}} {
		collector := newAckCollector()
		if _, err := New(collector).AckServiceActions(t.Context(), AckServiceActionsOptions{IDs: test.ids, Comment: test.comment}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%v %q accepted", test.ids, test.comment)
		}
	}
}

func TestListServiceActionsFiltersTheAcknowledgement(t *testing.T) {
	for acknowledged, want := range map[bool]string{true: "ack:eq:1", false: "ack:!eq:1"} {
		collector := &fakeCollector{body: `{"data":[{"id":7,"status":"err","ack":null}],"meta":{"total":1}}`}
		list, err := newTestService(collector).ListServiceActions(t.Context(), ListServiceActionsOptions{ServiceActionFilters: ServiceActionFilters{Status: "err", Acknowledged: &acknowledged}})
		if err != nil {
			t.Fatal(err)
		}
		filters := strings.Join(collector.requests[0].query["filter"], " ")
		if !strings.Contains(filters, want) || !strings.Contains(collector.requests[0].query.Get("props"), "acked_comment") || list.Actions[0].Acknowledged {
			t.Fatalf("acknowledged %v: filters %q, list %+v", acknowledged, filters, list)
		}
	}
}

// The three infos oc3 answers an acknowledgement with, each holding words of
// the others.
func TestAckOutcomeReadsTheInfosOfTheCollector(t *testing.T) {
	for info, want := range map[string]string{
		"skip acknowledge of action 2: already acknowledged": AckAlready,
		"skip acknowledge of action 3 in status 'ok'":        AckSkipped,
		"action 1 acknowledged":                              AckDone,
		"something else":                                     AckRefused,
	} {
		if got := ackOutcome(info); got != want {
			t.Errorf("ackOutcome(%q) = %s, want %s", info, got, want)
		}
	}
}

// With filters, the failed actions not acknowledged are selected, then
// acknowledged without being read back.
func TestAckServiceActionsByFilters(t *testing.T) {
	collector := ackCollector{fakeCollector: &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/dev2n1":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		case "/services_actions":
			if query.Get("offset") != "0" {
				return `{"data":[],"meta":{"total":2}}`
			}
			return `{"data":[{"id":1,"status":"err","action":"start","services.svcname":"web"},{"id":2,"status":"err","action":"stop"}],"meta":{"total":2}}`
		}
		return `{"info":"action 1 acknowledged"}`
	}}}
	result, err := New(collector).AckServiceActions(t.Context(), AckServiceActionsOptions{Node: "dev2n1", Since: "2026-10-01", Comment: "known issue"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 2 || result.Acknowledged != 2 || result.Node.Nodename != "dev2n1" || result.Actions[0].Action.Svcname != "web" {
		t.Fatalf("result = %+v", result)
	}
	var gets, posts int
	for _, request := range collector.requests {
		switch {
		case request.method == "POST":
			posts++
		case request.path == "/services_actions":
			gets++
			if filters := strings.Join(request.query["filter"], " "); filters != "log_type:eq:status status:eq:err ack:!eq:1 node_id:eq:n-1 begin:gte:2026-10-01" {
				t.Fatalf("selection filters = %q", filters)
			}
		case strings.HasPrefix(request.path, "/services_actions/"):
			t.Fatalf("an action was read back: %+v", request)
		}
	}
	if gets != 1 || posts != 2 {
		t.Fatalf("%d selection reads, %d acknowledgements", gets, posts)
	}
	for _, options := range []AckServiceActionsOptions{
		{Comment: "x"}, {IDs: []int64{1}, Node: "n", Comment: "x"}, {Node: "n", AllUnacknowledged: true, Comment: "x"},
	} {
		collector := newAckCollector()
		if _, err := New(collector).AckServiceActions(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	// A selection beyond the bound acknowledges nothing.
	collector = ackCollector{fakeCollector: &fakeCollector{body: `{"data":[{"id":1}],"meta":{"total":1001}}`}}
	if _, err := New(collector).AckServiceActions(t.Context(), AckServiceActionsOptions{AllUnacknowledged: true, Comment: "x"}); err == nil || !strings.Contains(err.Error(), "more than 1000") {
		t.Fatalf("too large: %v", err)
	}
	for _, request := range collector.requests {
		if request.method == "POST" {
			t.Fatalf("a selection too large sent %+v", request)
		}
	}
}

func TestCountServiceActionsGroupsByNode(t *testing.T) {
	acknowledged := false
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/nodes/dev2n1" {
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		}
		if query.Get("stats") == "true" {
			return `{"data":{"nodes.nodename":{"dev2n1":363}},"meta":{"total":363}}`
		}
		return `{"data":[],"meta":{"total":363}}`
	}}
	count, err := newTestService(collector).CountServiceActions(t.Context(), CountServiceActionsOptions{
		ServiceActionFilters: ServiceActionFilters{Node: "dev2n1", Status: "err", Acknowledged: &acknowledged}, GroupBy: "node",
	})
	if err != nil {
		t.Fatal(err)
	}
	last := collector.requests[len(collector.requests)-1]
	if filters := strings.Join(last.query["filter"], " "); filters != "log_type:eq:status node_id:eq:n-1 status:eq:err ack:!eq:1" || last.query.Get("props") != "nodes.nodename" || last.query.Get("stats") != "true" {
		t.Fatalf("request = %+v", last)
	}
	if count.Total != 363 || count.Node.Nodename != "dev2n1" || len(count.Groups) != 1 || count.Groups[0].Value != "dev2n1" || count.Groups[0].Count != 363 {
		t.Fatalf("count = %+v", count)
	}
	if _, err := newTestService(&fakeCollector{}).CountServiceActions(t.Context(), CountServiceActionsOptions{GroupBy: "date"}); err == nil {
		t.Fatal("group_by date accepted")
	}
}

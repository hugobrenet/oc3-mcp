package core

import (
	"net/url"
	"strings"
	"testing"
)

func TestListNodeActionsCleansTheQueue(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/dev2n2":
			return `{"data":[{"node_id":"n-2","nodename":"dev2n2","cluster_id":"c1"}]}`
		case "/actions":
			if strings.Join(query["filter"], " ") != "status:eq:T command:~freeze date_queued:gte:2026-10-10 node_id:eq:n-2" || query.Get("orderby") != "-id" ||
				!strings.HasSuffix(query.Get("props"), ",nodes.nodename,services.svcname") {
				t.Errorf("actions query = %v", query)
			}
			return `{"data":[
				{"id":"786","status":"T","command":"thaw --local","date_queued":"2026-10-16 10:44:38","date_dequeued":"2026-10-16 10:44:41","ret":"0","stderr":"\u001b[90m10:44:41.467\u001b[0m INF now unfrozen\n","action_type":"feed","user_id":"168","node_id":"n-2","nodes.nodename":"dev2n2"},
				{"id":787,"status":"W","command":"ssh -o ConnectTimeout=5 opensvc@10.0.0.1 -- sudo nodemgr pushasset","date_queued":"2026-10-16 11:00:00","date_dequeued":"0000-00-00 00:00:00","ret":0,"stdout":"never","action_type":"push","user_id":0,"node_id":"n-2","nodes.nodename":"dev2n2","svc_id":"s-1","services.svcname":"web"}],"meta":{"total":2}}`
		case "/users":
			return `{"data":[{"id":168,"first_name":"Jane","last_name":"jane@example.com"}]}`
		}
		return `{"data":[]}`
	}}
	list, err := newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{Node: "dev2n2", Status: "done", Command: "freeze", Since: "2026-10-10"})
	if err != nil {
		t.Fatal(err)
	}
	done, waiting := list.Actions[0], list.Actions[1]
	if done.ID != 786 || done.Status != "done" || done.Ret == nil || *done.Ret != 0 || done.Stderr != "10:44:41.467 INF now unfrozen\n" || done.Nodename != "dev2n2" ||
		strings.Contains(done.User, "@") || done.User == "" {
		t.Fatalf("done = %+v", done)
	}
	// An action not done has no exit code nor output, and the ssh invocation
	// of a push node is dropped from its command.
	if waiting.Status != "waiting" || waiting.Svcname != "web" || waiting.Ret != nil || waiting.Stdout != "" || waiting.Command != "pushasset" || waiting.Dequeued != "" || waiting.User != "" {
		t.Fatalf("waiting = %+v", waiting)
	}
	for _, request := range collector.requests {
		if request.path == "/nodes" {
			t.Fatalf("the node names were read apart: %+v", request)
		}
	}
	for _, options := range []ListNodeActionsOptions{{Status: "pending"}, {Since: "yesterday"}, {Limit: 101}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListNodeActions(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestQueueNodeActionPutsOneActionPerNode(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch {
		case path == "/nodes" && query != nil:
			return `{"data":[{"node_id":"n-1","nodename":"a"},{"node_id":"n-2","nodename":"b"}],"meta":{"total":2}}`
		case path == "/actions":
			return `{"data":[{"id":900,"status":"W","action_type":"feed"}]}`
		}
		return `{"data":[]}`
	}}
	queued, err := newTestService(collector).QueueNodeAction(t.Context(), NodeActionOptions{Action: "pushasset", NodeSelection: NodeSelection{Filters: NodeFilters{App: "WEB"}}})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Changed != 2 || queued.Nodes[0].Outcome != ChangeQueued || queued.Nodes[0].ActionID != 900 || queued.Nodes[0].QueuedFor != "feed" || queued.Nodes[0].Warning != "" {
		t.Fatalf("queued = %+v", queued)
	}
	var puts []string
	for _, request := range collector.requests {
		if request.method == "PUT" {
			if request.path != "/actions" || request.body["action"] != "pushasset" {
				t.Fatalf("put = %+v", request)
			}
			puts = append(puts, request.body["node_id"].(string))
		}
	}
	if len(puts) != 2 {
		t.Fatalf("puts = %v", puts)
	}
	// Only the actions that change nothing on the node but its reports.
	for _, action := range []string{"reboot", "pushpatch", "freeze", ""} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).QueueNodeAction(t.Context(), NodeActionOptions{Action: action, NodeSelection: NodeSelection{Nodes: []string{"a"}}}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("action %q accepted", action)
		}
	}
}

// A node read through the listener of the former agent, or over ssh, gets
// its action, with a warning: an om3 agent never runs it.
func TestQueueNodeActionWarnsOfANodeOm3DoesNotRead(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/actions" {
			return `{"data":[{"id":901,"status":"W","action_type":"pull"}]}`
		}
		return `{"data":[{"node_id":"n-1","nodename":"lab01"}],"meta":{"total":1}}`
	}}
	queued, err := newTestService(collector).QueueNodeAction(t.Context(), NodeActionOptions{Action: "checks", NodeSelection: NodeSelection{Nodes: []string{"lab01"}}})
	if err != nil || queued.Nodes[0].QueuedFor != "pull" || !strings.Contains(queued.Nodes[0].Warning, "an om3 agent never reads it") {
		t.Fatalf("queued = %+v, %v", queued, err)
	}
}

func TestListNodeActionsFiltersTheSentStatus(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"id":1,"status":"S","command":"checks"}],"meta":{"total":1}}`}
	list, err := newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{Status: "sent"})
	if err != nil || collector.requests[0].query.Get("filter") != "status:eq:S" || list.Actions[0].Status != "sent" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestRunNodeActionTakesOnlyTheChangingActions(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/actions" {
			return `{"data":[{"id":902,"status":"W","action_type":"feed"}]}`
		}
		return `{"data":[{"node_id":"n-1","nodename":"lab01"}],"meta":{"total":1}}`
	}}
	queued, err := newTestService(collector).RunNodeAction(t.Context(), NodeActionOptions{Action: "drain", NodeSelection: NodeSelection{Nodes: []string{"lab01"}}})
	if err != nil || queued.Changed != 1 || queued.Nodes[0].ActionID != 902 || queued.FollowUp != `list_node_actions with node "lab01", command "drain": an action is done when its status is done, with its exit code and output` {
		t.Fatalf("queued = %+v, %v", queued, err)
	}
	// reboot and shutdown are actions of the former agent: om3 answers them
	// with unknown command.
	for _, action := range []string{"reboot", "shutdown", "schedule_reboot", "pushasset"} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).RunNodeAction(t.Context(), NodeActionOptions{Action: action, NodeSelection: NodeSelection{Nodes: []string{"a"}}}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("action %q accepted", action)
		}
	}
}

func TestCancelNodeActionsCancelsOnlyWhatNoAgentTook(t *testing.T) {
	statuses := map[string]string{"/actions/1": "W", "/actions/2": "S", "/actions/3": "C", "/actions/4": "N"}
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if query == nil {
			return `{"id":1,"status":"C"}`
		}
		status, ok := statuses[path]
		if !ok {
			return `{"data":[]}`
		}
		return `{"data":[{"id":1,"status":"` + status + `","command":"freeze --local","nodes.nodename":"lab01"}]}`
	}}
	result, err := newTestService(collector).CancelNodeActions(t.Context(), []int{1, 2, 3, 9, 1})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := []string{}
	for _, action := range result.Actions {
		outcomes = append(outcomes, action.Outcome)
	}
	if strings.Join(outcomes, ",") != "cancelled,refused,already_cancelled,refused" || result.Cancelled != 1 || result.Refused != 2 ||
		!strings.Contains(result.Actions[1].Reason, "would not stop it") || result.Actions[0].Nodename != "lab01" {
		t.Fatalf("result = %+v", result)
	}
	var posts int
	for _, request := range collector.requests {
		if request.method == "POST" {
			posts++
			if request.path != "/actions" || request.body["status"] != "C" || request.body["id"] != float64(1) {
				t.Fatalf("post = %+v", request)
			}
		}
	}
	if posts != 1 {
		t.Fatalf("%d posts", posts)
	}
	// The collector answers a refusal with 200 and an error.
	collector = &fakeCollector{answer: func(path string, query url.Values) string {
		if query == nil {
			return `{"error":"Can not cancel action 4 in T state"}`
		}
		return `{"data":[{"id":4,"status":"N"}]}`
	}}
	result, err = newTestService(collector).CancelNodeActions(t.Context(), []int{4})
	if err != nil || result.Actions[0].Outcome != "refused" || !strings.Contains(result.Actions[0].Reason, "Can not cancel") {
		t.Fatalf("refusal = %+v, %v", result, err)
	}
	for _, ids := range [][]int{nil, {0}, make([]int, 51)} {
		if _, err := newTestService(&fakeCollector{}).CancelNodeActions(t.Context(), ids); err == nil {
			t.Fatalf("ids %v accepted", ids)
		}
	}
}

// The service filter keeps the actions on a service and its instances.
func TestListNodeActionsFiltersAService(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/services/foo":
			return `{"data":[{"svc_id":"s-1","svcname":"foo","cluster_id":"c1"}]}`
		case "/actions":
			return `{"data":[{"id":817,"status":"T","command":"freeze --local","svc_id":"s-1","services.svcname":"foo"}],"meta":{"total":1}}`
		}
		return `{"data":[]}`
	}}
	list, err := newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{Service: "foo"})
	if err != nil || list.Service == nil || list.Service.Svcname != "foo" || list.Actions[0].Svcname != "foo" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	for _, request := range collector.requests {
		if request.path == "/actions" && request.query.Get("filter") != "svc_id:eq:s-1" {
			t.Fatalf("actions query = %v", request.query)
		}
	}
}

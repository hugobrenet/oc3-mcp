package core

import (
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	if err != nil || queued.Changed != 1 || queued.Nodes[0].ActionID != 902 || !strings.HasPrefix(queued.FollowUp, "list_node_actions with action_ids [902] and wait") {
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

func TestCancelNodeActionsLeavesTheDecisionToTheCollector(t *testing.T) {
	// 1 waits and 2 runs: the collector cancels both; 3 is done and 9
	// unknown: it refuses them, with 200 and an error.
	answers := []string{
		`{"id":1,"status":"C","command":"freeze --local"}`,
		`{"id":2,"status":"C","command":"pushasset"}`,
		`{"error":"Can not cancel action 3 in T state"}`,
		`{"error":"Action 9 does not exist in action queue"}`,
	}
	var posts int
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		posts++
		return answers[posts-1]
	}}
	result, err := newTestService(collector).CancelNodeActions(t.Context(), []int{1, 2, 3, 9, 1})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := []string{}
	for _, action := range result.Actions {
		outcomes = append(outcomes, action.Outcome)
	}
	if strings.Join(outcomes, ",") != "cancelled,cancelled,refused,refused" || result.Cancelled != 2 || result.Refused != 2 ||
		result.Actions[1].Command != "pushasset" || !strings.Contains(result.Actions[2].Reason, "T state") {
		t.Fatalf("result = %+v", result)
	}
	// One request per action, a cancellation each, without a read first.
	for index, request := range collector.requests {
		if request.method != "POST" || request.path != "/actions" || request.body["status"] != "C" || request.body["id"] != float64([]int{1, 2, 3, 9}[index]) {
			t.Fatalf("request %d = %+v", index, request)
		}
	}
	if len(collector.requests) != 4 {
		t.Fatalf("%d requests", len(collector.requests))
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

func TestListNodeActionsWaitsForTheActionsNamed(t *testing.T) {
	interval := actionWaitInterval
	actionWaitInterval = 10 * time.Millisecond
	t.Cleanup(func() { actionWaitInterval = interval })
	// 7 waits then runs, 8 runs, then both are done; 9 is not found.
	var reads atomic.Int32
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path != "/actions" {
			return `{"data":[]}`
		}
		if strings.Join(query["filter"], " ") != "id:in:7,8,9" || query.Get("limit") != "100" {
			t.Errorf("actions query = %v", query)
		}
		switch reads.Add(1) {
		case 1:
			return `{"data":[{"id":8,"status":"R","action_type":"feed"},{"id":7,"status":"W","action_type":"feed"}],"meta":{"total":2}}`
		case 2:
			return `{"data":[{"id":8,"status":"T","ret":0,"action_type":"feed"},{"id":7,"status":"R","action_type":"feed"}],"meta":{"total":2}}`
		}
		return `{"data":[{"id":8,"status":"T","ret":0,"action_type":"feed"},{"id":7,"status":"T","ret":1,"stderr":"failed","action_type":"feed"}],"meta":{"total":2}}`
	}}
	list, err := newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{ActionIDs: []int{7, 8, 9, 7}, Wait: 30})
	if err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 3 || list.Pending != 0 || list.Waited > 1 || len(list.NotFound) != 1 || list.NotFound[0] != 9 ||
		list.Actions[1].Status != "done" || list.Actions[1].Ret == nil || *list.Actions[1].Ret != 1 {
		t.Fatalf("list = %+v, reads %d", list, reads.Load())
	}
	// Without wait, one read, which tells the pending actions.
	reads.Store(0)
	list, err = newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{ActionIDs: []int{7, 8, 9}})
	if err != nil || reads.Load() != 1 || list.Pending != 2 || list.Waited != 0 {
		t.Fatalf("list = %+v, %v, reads %d", list, err, reads.Load())
	}
}

func TestListNodeActionsEndsTheWait(t *testing.T) {
	interval := actionWaitInterval
	actionWaitInterval = 100 * time.Millisecond
	t.Cleanup(func() { actionWaitInterval = interval })
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		return `{"data":[{"id":7,"status":"W","action_type":"feed"}],"meta":{"total":1}}`
	}}
	started := time.Now()
	list, err := newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{ActionIDs: []int{7}, Wait: 1})
	if err != nil || list.Pending != 1 || list.Waited != 1 || time.Since(started) < time.Second || time.Since(started) > 4*time.Second {
		t.Fatalf("list = %+v, %v, after %v", list, err, time.Since(started))
	}
	for _, options := range []ListNodeActionsOptions{
		{Wait: maxActionWait + 1},
		{Wait: -1},
		{ActionIDs: []int{0}},
		{ActionIDs: make([]int, maxListNodeActionsLimit+1)},
	} {
		collector := &fakeCollector{answer: func(string, url.Values) string { return `{"data":[],"meta":{"total":0}}` }}
		if _, err := newTestService(collector).ListNodeActions(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	// More actions than a page are followed by their filter.
	if follow := followActions(make([]int, maxListNodeActionsLimit+1), true, `command "start"`); follow != `list_node_actions with command "start"` {
		t.Fatalf("follow = %s", follow)
	}
}

func TestListNodeActionsWaitsForTheAgentsOnly(t *testing.T) {
	interval := actionWaitInterval
	actionWaitInterval = 10 * time.Millisecond
	t.Cleanup(func() { actionWaitInterval = interval })
	// 7, queued for pull, waits until cancelled: the wait ends without it.
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		return `{"data":[{"id":7,"status":"W","action_type":"pull"}],"meta":{"total":1}}`
	}}
	list, err := newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{ActionIDs: []int{7}, Wait: 30})
	if err != nil || len(collector.requests) != 1 || list.Pending != 1 || list.PendingNotFeed != 1 {
		t.Fatalf("list = %+v, %v, requests %d", list, err, len(collector.requests))
	}
	// The node is resolved, and each user read, once for the whole wait.
	var reads atomic.Int32
	collector = &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/dev2n1":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`
		case "/users":
			return `{"data":[{"id":168,"first_name":"Jane","last_name":"Doe"}]}`
		}
		if reads.Add(1) < 3 {
			return `{"data":[{"id":8,"status":"R","action_type":"feed","user_id":168}],"meta":{"total":1}}`
		}
		return `{"data":[{"id":8,"status":"T","ret":0,"action_type":"feed","user_id":168}],"meta":{"total":1}}`
	}}
	list, err = newTestService(collector).ListNodeActions(t.Context(), ListNodeActionsOptions{Node: "dev2n1", Wait: 30})
	counts := map[string]int{}
	for _, request := range collector.requests {
		counts[request.path]++
	}
	if err != nil || list.Pending != 0 || list.Node == nil || list.Actions[0].User == "" || counts["/actions"] != 3 || counts["/nodes/dev2n1"] != 1 || counts["/users"] != 1 {
		t.Fatalf("list = %+v, %v, requests %v", list, err, counts)
	}
	// Actions no om3 agent reads are followed without wait.
	if follow := followActions([]int{9}, false, `command "pushasset"`); follow != "list_node_actions with action_ids [9]" {
		t.Fatalf("follow = %s", follow)
	}
}

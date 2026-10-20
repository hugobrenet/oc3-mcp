package core

import (
	"net/url"
	"strings"
	"sync"
	"testing"
)

// newServiceAgentCollector answers the services, their instances and the
// queueing of actions: web a failover service on n1, flex a flex service.
func newServiceAgentCollector() *fakeCollector {
	var mutex sync.Mutex
	return &fakeCollector{answer: func(path string, query url.Values) string {
		mutex.Lock()
		defer mutex.Unlock()
		filters := strings.Join(query["filter"], " ")
		switch {
		case path == "/nodes/n1":
			return `{"data":[{"node_id":"n-1","nodename":"n1"}]}`
		case path == "/nodes/n2":
			return `{"data":[{"node_id":"n-2","nodename":"n2"}]}`
		case path == "/services" && strings.Contains(filters, "svc_app:eq:WEB"):
			return `{"data":[{"svc_id":"s-1","svcname":"web","svc_topology":"failover"}],"meta":{"total":1}}`
		case path == "/services":
			return `{"data":[{"svc_id":"s-1","svcname":"web","svc_topology":"failover"},{"svc_id":"s-2","svcname":"flex","svc_topology":"flex"}],"meta":{"total":2}}`
		case path == "/services_instances" && strings.Contains(filters, "node_id:eq:n-2"):
			return `{"data":[{"svc_id":"s-1","node_id":"n-2","nodes.nodename":"n2","nodes.version":"","mon_availstatus":"up","services.svcname":"web","services.svc_topology":"failover"}],"meta":{"total":1}}`
		case path == "/services_instances":
			return `{"data":[{"svc_id":"s-1","node_id":"n-1","nodes.nodename":"n1","nodes.version":"v3.0.0-rc50","mon_availstatus":"up","services.svcname":"web","services.svc_topology":"failover"}],"meta":{"total":1}}`
		case path == "/actions":
			return `{"data":[{"id":901,"status":"W","action_type":"feed","command":"start","node_id":"n-2"}]}`
		}
		return `{"data":[]}`
	}}
}

func TestQueueServiceAgentActionOnTheServicesNamed(t *testing.T) {
	collector := newServiceAgentCollector()
	result, err := New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "switch", ServiceSelection: ServiceSelection{Services: []string{"web", "flex"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Sorted flex, web: a switch refuses the flex service before sending,
	// and queues the failover one, for the node the collector chose.
	flex, web := result.Actions[0], result.Actions[1]
	if result.Local || result.Selected != 2 || result.Queued != 1 || result.Refused != 1 || flex.Outcome != ChangeRefused || !strings.Contains(flex.Reason, "takeover_service") ||
		web.Outcome != ChangeQueued || web.ActionID != 901 || web.NodeID != "n-2" || web.QueuedFor != "feed" || !strings.Contains(result.FollowUp, `service "web"`) {
		t.Fatalf("result = %+v", result)
	}
	// A filter narrows the services named: the one it leaves out is told.
	collector = newServiceAgentCollector()
	result, err = New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "freeze", ServiceSelection: ServiceSelection{Services: []string{"web", "flex"}, Filters: ServiceFilters{App: "WEB"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queued != 1 || result.Refused != 1 || result.Actions[0].Svcname != "flex" || !strings.Contains(result.Actions[0].Reason, "does not match the filters") {
		t.Fatalf("result = %+v", result)
	}
	for _, request := range collector.requests {
		if request.path == "/services" && strings.Contains(strings.Join(request.query["filter"], " "), "svc_app") && !strings.Contains(strings.Join(request.query["filter"], " "), "svc_id:in:s-1,s-2") {
			t.Fatalf("selection = %+v", request)
		}
	}
	// With local, the instances of the services named on the node.
	collector = newServiceAgentCollector()
	result, err = New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "stop", Local: true, ServiceSelection: ServiceSelection{Services: []string{"web"}, Node: "n1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Local || result.Selected != 1 || result.Queued != 1 || result.Actions[0].Nodename != "n1" || result.Node == nil {
		t.Fatalf("result = %+v", result)
	}
	for _, request := range collector.requests {
		if request.method == "PUT" && (request.body["node_id"] != "n-1" || request.body["action"] != "stop") {
			t.Fatalf("queueing = %+v", request)
		}
	}
}

// A local action on a node without agent version would go without --local,
// a service named without instance there is told, and a takeover of a
// failover service already on its destination is left as it is.
func TestQueueServiceAgentActionChecksEachInstance(t *testing.T) {
	collector := newServiceAgentCollector()
	result, err := New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "stop", Local: true, ServiceSelection: ServiceSelection{Services: []string{"web"}, Node: "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Queued != 0 || result.Refused != 1 || !strings.Contains(result.Actions[0].Reason, "without --local") {
		t.Fatalf("result = %+v", result)
	}
	collector = newServiceAgentCollector()
	result, err = New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "takeover", Destination: "n1", ServiceSelection: ServiceSelection{Services: []string{"web", "flex"}}})
	if err != nil {
		t.Fatal(err)
	}
	flex, web := result.Actions[0], result.Actions[1]
	if result.Selected != 2 || result.Unchanged != 1 || result.Refused != 1 || result.To == nil || result.To.Nodename != "n1" || flex.Outcome != ChangeRefused || !strings.Contains(flex.Reason, "no instance on node n1, the destination") ||
		web.Outcome != ChangeUnchanged || !strings.Contains(web.Reason, "already runs") {
		t.Fatalf("result = %+v", result)
	}
	for _, request := range collector.requests {
		if request.method == "PUT" {
			t.Fatalf("nothing should be queued: %+v", request)
		}
	}
}

// A node alone selects every service of the node, the agent showing the
// call for the user to confirm; a call with neither services nor filters,
// or a scope an action does not take, is refused before anything is read.
func TestQueueServiceAgentActionScope(t *testing.T) {
	collector := newServiceAgentCollector()
	result, err := New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "restart", ServiceSelection: ServiceSelection{Node: "n1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Local || result.Selected != 1 || result.Queued != 1 || result.Actions[0].Nodename != "n1" {
		t.Fatalf("result = %+v", result)
	}
	for _, request := range collector.requests {
		if request.path == "/services_instances" && strings.Contains(strings.Join(request.query["filter"], " "), "svc_id:in:") {
			t.Fatalf("a node alone narrowed to services: %+v", request)
		}
	}
	for _, options := range []ServiceAgentActionOptions{
		{Action: "stop"},
		{Action: "freeze", Local: true},
		{Action: "push config", ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{Action: "giveback", Local: true, ServiceSelection: ServiceSelection{Services: []string{"web"}, Node: "n1"}},
		{Action: "abort", Local: true, ServiceSelection: ServiceSelection{Node: "n1"}},
		{Action: "takeover", ServiceSelection: ServiceSelection{Node: "n1"}},
		{Action: "takeover", ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{Action: "takeover", Destination: "n1"},
		{Action: "restart", Destination: "n2", ServiceSelection: ServiceSelection{Node: "n1"}},
		{Action: "restart", ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{Action: "stop", Local: true, ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{Action: "freeze", ServiceSelection: ServiceSelection{Services: make([]string, 151)}},
	} {
		collector := newServiceAgentCollector()
		if _, err := New(collector).QueueServiceAgentAction(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted, requests %v", options, collector.requests)
		}
	}
}

// A takeover moves the services the filters select, such as those of one
// node, to its destination.
func TestQueueServiceAgentActionTakesTheServicesOfANodeOver(t *testing.T) {
	collector := newServiceAgentCollector()
	result, err := New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "takeover", Destination: "n2", ServiceSelection: ServiceSelection{Node: "n1"}})
	if err != nil {
		t.Fatal(err)
	}
	// web runs on n2 already, as the instances answer: left as it is.
	if result.To == nil || result.To.Nodename != "n2" || result.Node == nil || result.Node.Nodename != "n1" || result.Selected != 1 ||
		result.Actions[0].Nodename != "n2" || result.Actions[0].Outcome != ChangeUnchanged {
		t.Fatalf("result = %+v", result)
	}
}

// A compliance run goes to the node of each instance without --local, so
// whatever the agent version, and tells a service without moduleset.
func TestQueueServiceComplianceRun(t *testing.T) {
	collector := newServiceAgentCollector()
	answer := collector.answer
	collector.answer = func(path string, query url.Values) string {
		switch path {
		case "/services/s-1/compliance/modulesets":
			return `{"data":[],"meta":{"total":0}}`
		case "/actions":
			return `{"data":[{"id":902,"status":"W","action_type":"feed","command":"compliance check","node_id":"n-2"}]}`
		}
		return answer(path, query)
	}
	result, err := New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "compliance_check", ServiceSelection: ServiceSelection{Node: "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	// n2 holds no agent version: no refusal, the collector adding no
	// --local to a compliance run, and no warning of it.
	queued := result.Actions[0]
	if !result.Local || result.Queued != 1 || queued.Outcome != ChangeQueued || queued.Nodename != "n2" || strings.Contains(queued.Warning, "--local") ||
		!strings.Contains(queued.Warning, "no moduleset attached") || !strings.Contains(result.FollowUp, "list_compliance_runs") {
		t.Fatalf("result = %+v", result)
	}
	for _, request := range collector.requests {
		if request.method == "PUT" && (request.body["action"] != "compliance_check" || request.body["node_id"] != "n-2" || request.body["svc_id"] != "s-1") {
			t.Fatalf("queueing = %+v", request)
		}
	}
	// Several services are followed by the command the collector queues.
	collector = newServiceAgentCollector()
	result, err = New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "compliance_fix", ServiceSelection: ServiceSelection{Services: []string{"web"}, Node: "n1"}})
	if err != nil || result.Queued != 1 || !strings.Contains(result.FollowUp, `service "web"`) {
		t.Fatalf("result = %+v, %v", result, err)
	}
	for _, options := range []ServiceAgentActionOptions{
		{Action: "compliance_check", ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{Action: "compliance_fix", ServiceSelection: ServiceSelection{Filters: ServiceFilters{App: "WEB"}}},
	} {
		collector := newServiceAgentCollector()
		if _, err := New(collector).QueueServiceAgentAction(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

// A selection over the bound queues nothing.
func TestQueueServiceAgentActionBoundsTheSelection(t *testing.T) {
	rows := make([]string, 201)
	for i := range rows {
		rows[i] = `{"svc_id":"s-` + strings.Repeat("1", i+1) + `","svcname":"svc` + strings.Repeat("a", i+1) + `","svc_topology":"failover"}`
	}
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/services" {
			page := rows[:200]
			if query.Get("offset") != "0" {
				page = rows[200:]
			}
			return `{"data":[` + strings.Join(page, ",") + `],"meta":{"total":201}}`
		}
		return `{"data":[]}`
	}}
	_, err := New(collector).QueueServiceAgentAction(t.Context(), ServiceAgentActionOptions{Action: "freeze", ServiceSelection: ServiceSelection{Filters: ServiceFilters{App: "WEB"}}})
	if err == nil || !strings.Contains(err.Error(), "more than 200") {
		t.Fatalf("err = %v", err)
	}
	for _, request := range collector.requests {
		if request.method == "PUT" {
			t.Fatalf("a selection over the bound queued %+v", request)
		}
	}
}

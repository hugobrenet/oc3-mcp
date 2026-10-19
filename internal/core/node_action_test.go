package core

import (
	"net/url"
	"strings"
	"testing"
)

func TestCreateNodeChecksTheNameAndTheReferences(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes":
			if query == nil {
				break
			}
			if query.Get("filter") != "nodename:eq:lab01" {
				t.Errorf("existence filter = %q", query.Get("filter"))
			}
			return `{"data":[],"meta":{"total":0}}`
		case "/apps/data":
			return `{"id":3,"app":"DATA"}`
		case "/groups/infra%20unix":
			return `{"data":[{"id":7,"role":"Infra Unix"}]}`
		}
		// The POST answers the created node, whose status the collector did
		// not keep.
		return `{"node_id":"n-9","nodename":"lab01","app":"DATA","team_responsible":"Infra Unix","status":"","warranty_end":"2027-12-31 00:00:00.000"}`
	}}
	created, err := newTestService(collector).CreateNode(t.Context(), "lab01", map[string]string{
		"app": "data", "team_responsible": "infra unix", "status": "Commissioning", "warranty_end": "2027-12-31", "role": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	post := collector.requests[len(collector.requests)-1]
	if post.method != "POST" || post.path != "/nodes" || post.body["nodename"] != "lab01" || post.body["app"] != "DATA" || post.body["team_responsible"] != "Infra Unix" ||
		post.body["node_id"] != nil || post.body["role"] != nil {
		t.Fatalf("post = %+v", post)
	}
	if created.Node.NodeID != "n-9" || created.Properties["warranty_end"] != "2027-12-31 00:00:00.000" || len(created.NotApplied) != 1 ||
		created.NotApplied[0].Property != "status" || created.NotApplied[0].Requested != "Commissioning" {
		t.Fatalf("created = %+v", created)
	}
}

func TestCreateNodeRefusesAnExistingName(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"node_id":"n-1","nodename":"lab01","cluster_id":"c1"}],"meta":{"total":1}}`}
	_, err := newTestService(collector).CreateNode(t.Context(), "lab01", nil)
	if err == nil || !strings.Contains(err.Error(), "node lab01 already exists: lab01 n-1 (cluster c1)") {
		t.Fatalf("existing node: %v", err)
	}
	for _, request := range collector.requests {
		if request.method != "GET" {
			t.Fatalf("an existing name sent %+v", request)
		}
	}
	for _, test := range []struct {
		nodename   string
		properties map[string]string
	}{
		{"", nil}, {"lab 01", nil}, {"a/b", nil}, {"lab01", map[string]string{"warranty_end": "31/12/2027"}},
		{"lab01", map[string]string{"cpu_cores": "4"}}, {"lab01", map[string]string{"status": " x"}},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).CreateNode(t.Context(), test.nodename, test.properties); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%q %v accepted", test.nodename, test.properties)
		}
	}
	collector = &fakeCollector{err: fakeNotFound{}}
	if _, err := newTestService(collector).CreateNode(t.Context(), "lab01", map[string]string{"app": "NOSUCH"}); err == nil || !strings.Contains(err.Error(), "app NOSUCH is not an app") {
		t.Fatalf("unknown app: %v", err)
	}
}

func TestUpdateNodeGivesTheValuesBeforeAndAfter(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch {
		case path == "/nodes/lab01":
			return `{"data":[{"node_id":"n-9","nodename":"lab01"}]}`
		case path == "/nodes/n-9" && query != nil:
			if props := query.Get("props"); props != "node_id,nodename,cluster_id,status,loc_city" {
				t.Errorf("props = %q", props)
			}
			return `{"data":[{"node_id":"n-9","nodename":"lab01","status":"Commissioning","loc_city":"Lyon"}]}`
		}
		return `{"node_id":"n-9","nodename":"lab01","status":"In service","loc_city":""}`
	}}
	update, err := newTestService(collector).UpdateNode(t.Context(), "lab01", map[string]string{"status": "In service", "loc_city": ""})
	if err != nil {
		t.Fatal(err)
	}
	post := collector.requests[len(collector.requests)-1]
	if post.method != "POST" || post.path != "/nodes/n-9" || len(post.body) != 2 || post.body["loc_city"] != "" {
		t.Fatalf("post = %+v", post)
	}
	if len(update.Changes) != 2 || update.Changes[0] != (NodeChange{Property: "status", Before: "Commissioning", After: "In service"}) ||
		update.Changes[1] != (NodeChange{Property: "loc_city", Before: "Lyon"}) || update.NotApplied != nil {
		t.Fatalf("update = %+v", update)
	}
	for _, properties := range []map[string]string{nil, {"app": ""}, {"team_responsible": ""}, {"maintenance_end": "2028"}} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).UpdateNode(t.Context(), "lab01", properties); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%v accepted", properties)
		}
	}
}

func TestDeleteNodeDeletesTheExactNode(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/lab01":
			return `{"data":[{"node_id":"n-9","nodename":"lab01"}]}`
		case "/nodes/n-9":
			if query == nil {
				return `{"info":"delete node lab01"}`
			}
			return `{"data":[{"node_id":"n-9","nodename":"lab01","cluster_id":""}]}`
		}
		return `{}`
	}}
	deletion, err := newTestService(collector).DeleteNode(t.Context(), "lab01")
	if err != nil {
		t.Fatal(err)
	}
	last := collector.requests[len(collector.requests)-1]
	if last.method != "DELETE" || last.path != "/nodes/n-9" || !deletion.Deleted || deletion.Node.Nodename != "lab01" {
		t.Fatalf("deletion = %+v, last request %+v", deletion, last)
	}
	collector = &fakeCollector{body: `{"data":[{"node_id":"n-1","cluster_id":"c1"},{"node_id":"n-2","cluster_id":"c2"}]}`}
	if _, err := newTestService(collector).DeleteNode(t.Context(), "lab01"); err == nil || len(collector.requests) != 1 {
		t.Fatalf("an ambiguous name: %v, %d requests", err, len(collector.requests))
	}
}

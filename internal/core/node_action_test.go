package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/client"
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

// updateCollector answers the selection of the nodes, a page of 200 at a
// time, their values before the update, by node ID, and an update with the
// answer of after.
func updateCollector(nodes []string, before, after map[string]string) *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch {
		case path == "/nodes" && query != nil:
			offset, _ := strconv.Atoi(query.Get("offset"))
			page := nodes[min(offset, len(nodes)):min(offset+200, len(nodes))]
			return `{"data":[` + strings.Join(page, ",") + `],"meta":{"total":` + itoa(len(nodes)) + `}}`
		case strings.HasPrefix(path, "/apps/"):
			return `{"id":3,"app":"DATA"}`
		case strings.HasPrefix(path, "/nodes/") && query != nil:
			return `{"data":[` + before[strings.TrimPrefix(path, "/nodes/")] + `]}`
		case strings.HasPrefix(path, "/nodes/"):
			return after[strings.TrimPrefix(path, "/nodes/")]
		}
		return `{"data":[]}`
	}}
}

// An update of the nodes a filter selects sets each, one request per node:
// a node holding the values already is left as it is, nothing sent for it; a
// refusal of the collector on one leaves the others updated; an update the
// collector answers out of its API, or not at all, has an outcome unknown,
// never counted as refused.
func TestUpdateNodesSetsEachSelectedNode(t *testing.T) {
	nodes := []string{
		`{"node_id":"n-3","nodename":"c"}`, `{"node_id":"n-1","nodename":"a"}`, `{"node_id":"n-2","nodename":"b"}`,
		`{"node_id":"n-4","nodename":"d"}`, `{"node_id":"n-5","nodename":"e"}`,
	}
	collector := updateCollector(nodes, map[string]string{
		"n-1": `{"node_id":"n-1","status":"Commissioning","loc_city":"Lyon"}`,
		"n-2": `{"node_id":"n-2","status":"In service","loc_city":""}`,
		"n-3": `{"node_id":"n-3","status":"Commissioning","loc_city":"Paris"}`,
		"n-4": `{"node_id":"n-4","status":"Commissioning","loc_city":""}`,
		"n-5": `{"node_id":"n-5","status":"Commissioning","loc_city":""}`,
	}, map[string]string{
		"n-1": `{"node_id":"n-1","status":"In service","loc_city":""}`,
		"n-3": `{"node_id":"other"}`,
	})
	collector.failures = map[string]error{
		"POST /nodes/n-4": statusError{http.StatusForbidden, "you are not responsible for node d"},
		"POST /nodes/n-5": statusError{http.StatusBadGateway, "the collector is restarting"},
	}
	update, err := newTestService(collector).UpdateNodes(t.Context(), NodeUpdateOptions{
		NodeSelection: NodeSelection{Filters: NodeFilters{Status: "Commissioning"}},
		Properties:    map[string]string{"status": "In service", "loc_city": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if update.Selected != 5 || update.Changed != 1 || update.Unchanged != 1 || update.Refused != 1 || update.Unknown != 2 ||
		update.Properties["status"] != "In service" || update.Properties["loc_city"] != "" {
		t.Fatalf("update = %+v", update)
	}
	a, b, c, d, e := update.Nodes[0], update.Nodes[1], update.Nodes[2], update.Nodes[3], update.Nodes[4]
	if a.Nodename != "a" || a.Outcome != ChangeSet || len(a.Changes) != 2 ||
		a.Changes[0] != (NodeChange{Property: "status", Before: "Commissioning"}) ||
		a.Changes[1] != (NodeChange{Property: "loc_city", Before: "Lyon"}) || a.NotApplied != nil {
		t.Fatalf("a = %+v", a)
	}
	if b.Outcome != ChangeUnchanged || b.Changes != nil {
		t.Fatalf("b = %+v", b)
	}
	if c.Outcome != ChangeUnknown || !strings.Contains(c.Reason, "out of its API; get_node tells") {
		t.Fatalf("c = %+v", c)
	}
	if d.Outcome != ChangeRefused || !strings.Contains(d.Reason, "not responsible") {
		t.Fatalf("d = %+v", d)
	}
	if e.Outcome != ChangeUnknown || !strings.Contains(e.Reason, "restarting; get_node tells") {
		t.Fatalf("e = %+v", e)
	}
	posted := map[string]bool{}
	for _, request := range collector.requests {
		switch {
		case request.method == "POST":
			posted[request.path] = true
			if len(request.body) != 2 || request.body["status"] != "In service" || request.body["loc_city"] != "" {
				t.Fatalf("post = %+v", request)
			}
		case request.path == "/nodes" && request.query.Get("filter") != "status:eq:Commissioning":
			t.Fatalf("selection request = %+v", request)
		case request.path == "/nodes/n-1" && request.query.Get("props") != "node_id,status,loc_city":
			t.Fatalf("read before = %+v", request)
		}
	}
	if len(posted) != 4 || posted["/nodes/n-2"] {
		t.Fatalf("posted %v", posted)
	}
}

// A value equal to the one asked, by the rules of the collector, leaves the
// node unchanged; a value the collector holds otherwise after the update is
// told not applied.
func TestUpdateNodesComparesAsTheCollectorStores(t *testing.T) {
	nodes := []string{`{"node_id":"n-1","nodename":"a"}`, `{"node_id":"n-2","nodename":"b"}`}
	collector := updateCollector(nodes, map[string]string{
		"n-1": `{"node_id":"n-1","app":"data","maintenance_end":"2027-12-31 00:00:00"}`,
		"n-2": `{"node_id":"n-2","app":"OTHER","maintenance_end":""}`,
	}, map[string]string{
		"n-2": `{"node_id":"n-2","app":"DATA","maintenance_end":"2028-01-01 00:00:00"}`,
	})
	update, err := newTestService(collector).UpdateNodes(t.Context(), NodeUpdateOptions{
		NodeSelection: NodeSelection{Filters: NodeFilters{Env: "DEV"}},
		Properties:    map[string]string{"app": "DATA", "maintenance_end": "2027-12-31"},
	})
	if err != nil {
		t.Fatal(err)
	}
	a, b := update.Nodes[0], update.Nodes[1]
	if a.Outcome != ChangeUnchanged {
		t.Fatalf("a = %+v", a)
	}
	if b.Outcome != ChangeSet || len(b.NotApplied) != 1 ||
		b.NotApplied[0] != (NodePropertyMismatch{Property: "maintenance_end", Requested: "2027-12-31", Applied: "2028-01-01 00:00:00"}) {
		t.Fatalf("b = %+v", b)
	}
}

// A read answered out of the collector API before any update fails the call
// as incompatible; a read the collector refuses refuses its node only.
func TestUpdateNodesTellsAReadItCannotUse(t *testing.T) {
	nodes := []string{`{"node_id":"n-1","nodename":"a"}`}
	collector := updateCollector(nodes, map[string]string{"n-1": `{"node_id":"other"}`}, nil)
	_, err := newTestService(collector).UpdateNodes(t.Context(), NodeUpdateOptions{
		NodeSelection: NodeSelection{Nodes: []string{"a"}}, Properties: map[string]string{"status": "x"},
	})
	if !errors.Is(err, client.ErrIncompatible) {
		t.Fatalf("err = %v", err)
	}
	for _, request := range collector.requests {
		if request.method == "POST" {
			t.Fatalf("an update was sent: %+v", request)
		}
	}
	collector = updateCollector(nodes, nil, nil)
	collector.failures = map[string]error{"GET /nodes/n-1": statusError{http.StatusForbidden, "not visible"}}
	update, err := newTestService(collector).UpdateNodes(t.Context(), NodeUpdateOptions{
		NodeSelection: NodeSelection{Nodes: []string{"a"}}, Properties: map[string]string{"status": "x"},
	})
	if err != nil || update.Refused != 1 || update.Nodes[0].Outcome != ChangeRefused {
		t.Fatalf("update = %+v, %v", update, err)
	}
}

// The values before an update go up to their bound: on 1000 nodes, each of
// 19 properties of 255 bytes, the result stays under the 1 MiB of a tool
// result the agent reads, every node keeping its outcome.
func TestUpdateNodesBoundsItsDetails(t *testing.T) {
	long := strings.Repeat("v", 255)
	properties := map[string]string{}
	for _, name := range NodeProperties {
		switch name {
		case "app", "team_responsible":
		case "warranty_end", "maintenance_end":
			properties[name] = "2030-01-01"
		default:
			properties[name] = "x"
		}
	}
	var row strings.Builder
	for name := range properties {
		row.WriteString(`,"` + name + `":"` + long + `"`)
	}
	nodes := make([]string, 1000)
	before, after := map[string]string{}, map[string]string{}
	for index := range nodes {
		id := "n-" + itoa(index)
		nodes[index] = `{"node_id":"` + id + `","nodename":"node` + itoa(index) + `"}`
		before[id] = `{"node_id":"` + id + `"` + row.String() + `}`
		after[id] = `{"node_id":"` + id + `"}`
	}
	collector := updateCollector(nodes, before, after)
	update, err := newTestService(collector).UpdateNodes(t.Context(), NodeUpdateOptions{
		NodeSelection: NodeSelection{Filters: NodeFilters{Env: "DEV"}}, Properties: properties,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(update)
	t.Logf("1000 nodes, %d details omitted, %d bytes", update.DetailsOmitted, len(data))
	if update.Selected != 1000 || len(update.Nodes) != 1000 || update.DetailsOmitted == 0 || len(data) >= 1<<20 {
		t.Fatalf("selected %d, omitted %d, %d bytes", update.Selected, update.DetailsOmitted, len(data))
	}
}

// One node, named, goes as a selection of one: its name is resolved first.
func TestUpdateNodesOfNamedNodes(t *testing.T) {
	collector := updateCollector([]string{`{"node_id":"n-9","nodename":"lab01"}`},
		map[string]string{"n-9": `{"node_id":"n-9","status":""}`},
		map[string]string{"n-9": `{"node_id":"n-9","status":"In service"}`})
	update, err := newTestService(collector).UpdateNodes(t.Context(), NodeUpdateOptions{
		NodeSelection: NodeSelection{Nodes: []string{"lab01"}}, Properties: map[string]string{"status": "In service"},
	})
	if err != nil || update.Changed != 1 || update.Nodes[0].Changes[0] != (NodeChange{Property: "status"}) {
		t.Fatalf("update = %+v, %v", update, err)
	}
	if request := collector.requests[0]; request.path != "/nodes" || request.query.Get("filter") != "nodename:in:lab01" {
		t.Fatalf("resolution = %+v", request)
	}
}

// A call with no property, a value refused, or no selection, or two, sends
// nothing, an app to check included.
func TestUpdateNodesRefusesBeforeAnyRequest(t *testing.T) {
	for _, options := range []NodeUpdateOptions{
		{NodeSelection: NodeSelection{Nodes: []string{"lab01"}}},
		{NodeSelection: NodeSelection{Nodes: []string{"lab01"}}, Properties: map[string]string{"app": ""}},
		{NodeSelection: NodeSelection{Nodes: []string{"lab01"}}, Properties: map[string]string{"team_responsible": ""}},
		{NodeSelection: NodeSelection{Nodes: []string{"lab01"}}, Properties: map[string]string{"maintenance_end": "2028"}},
		{Properties: map[string]string{"status": "x"}},
		{Properties: map[string]string{"app": "DATA"}},
		{NodeSelection: NodeSelection{Nodes: []string{"a"}, Filters: NodeFilters{Status: "x"}}, Properties: map[string]string{"app": "DATA"}},
		{NodeSelection: NodeSelection{Nodes: make([]string, 51)}, Properties: map[string]string{"status": "y"}},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).UpdateNodes(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%+v accepted, %d requests", options, len(collector.requests))
		}
	}
}

// The nodes a filter selects are deleted, one request each: the selection
// is read first, as for the tags.
func TestDeleteNodesSelectedByAFilter(t *testing.T) {
	nodes := []string{`{"node_id":"n-1","nodename":"a"}`, `{"node_id":"n-2","nodename":"b"}`}
	collector := updateCollector(nodes, nil, map[string]string{"n-1": `{"info":"delete node a"}`, "n-2": `{"info":"delete node b"}`})
	deletion, err := newTestService(collector).DeleteNodes(t.Context(), NodeSelection{Filters: NodeFilters{Status: "TOTO"}})
	if err != nil || deletion.Selected != 2 || deletion.Changed != 2 {
		t.Fatalf("deletion = %+v, %v", deletion, err)
	}
	for _, request := range collector.requests {
		if request.path == "/nodes" && request.query.Get("filter") != "status:eq:TOTO" {
			t.Fatalf("selection request = %+v", request)
		}
	}
}

// The nodes named are deleted one request each: a refusal of the collector
// on one leaves the others deleted, and a deletion answered not at all has an
// outcome unknown, never counted as refused.
func TestDeleteNodesDeletesTheNamedNodes(t *testing.T) {
	nodes := []string{`{"node_id":"n-1","nodename":"a"}`, `{"node_id":"n-2","nodename":"b"}`, `{"node_id":"n-3","nodename":"c"}`}
	collector := updateCollector(nodes, nil, map[string]string{"n-1": `{"info":"delete node a"}`})
	collector.failures = map[string]error{
		"DELETE /nodes/n-2": statusError{http.StatusForbidden, "you are not responsible for node b"},
		"DELETE /nodes/n-3": statusError{http.StatusBadGateway, "the collector is restarting"},
	}
	deletion, err := newTestService(collector).DeleteNodes(t.Context(), NodeSelection{Nodes: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if deletion.Selected != 3 || deletion.Changed != 1 || deletion.Refused != 1 || deletion.Unknown != 1 {
		t.Fatalf("deletion = %+v", deletion)
	}
	a, b, c := deletion.Nodes[0], deletion.Nodes[1], deletion.Nodes[2]
	if a.Outcome != ChangeDeleted || b.Outcome != ChangeRefused || !strings.Contains(b.Reason, "not responsible") ||
		c.Outcome != ChangeUnknown || !strings.Contains(c.Reason, "get_node tells whether it is still there") {
		t.Fatalf("nodes = %+v", deletion.Nodes)
	}
	deleted := 0
	for _, request := range collector.requests {
		if request.method == "DELETE" {
			deleted++
		}
	}
	if deleted != 3 {
		t.Fatalf("%d deletions sent", deleted)
	}
	// An ambiguous name, or none, or more than 50, deletes nothing.
	ambiguous := updateCollector([]string{`{"node_id":"n-1","nodename":"a","cluster_id":"c1"}`, `{"node_id":"n-2","nodename":"a","cluster_id":"c2"}`}, nil, nil)
	for _, names := range [][]string{{"a"}, nil, make([]string, 51)} {
		collector := ambiguous
		if names == nil || len(names) == 51 {
			collector = &fakeCollector{body: `{"data":[]}`}
		}
		if _, err := newTestService(collector).DeleteNodes(t.Context(), NodeSelection{Nodes: names}); err == nil {
			t.Fatalf("%d names accepted", len(names))
		}
		for _, request := range collector.requests {
			if request.method == "DELETE" {
				t.Fatalf("a refused call deleted: %+v", request)
			}
		}
	}
}

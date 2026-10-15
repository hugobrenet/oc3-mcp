package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const testNodeListBody = `{"data":[
 {"node_id":"603ec1ff-7fb0-40ab-8cec-93b593e42f37","nodename":"ams-dev-app01","cluster_id":"1a85097c-b382-4864-93f3-3b1ba42dd34f","app":"WEB","node_env":"DEV","status":"up","os_concat":"linux Ubuntu 22.04","version":"3.0.4","last_comm":"2026-09-15 15:10:00.000","node_frozen":"T"},
 {"node_id":"9e194817-4e7c-4af6-8ba8-b4c61193611d","nodename":"ams-dev-app02","node_frozen":"F"}
],"meta":{"count":2,"limit":2,"offset":0,"total":5}}`

func TestListNodesQueriesOnePageAndMapsNodes(t *testing.T) {
	collector := &fakeCollector{body: testNodeListBody}
	nodes, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{
		NodeFilters: NodeFilters{Name: "app.0", ClusterID: "1a85097c-b382-4864-93f3-3b1ba42dd34f", App: "WEB", Env: "DEV"}, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/nodes" || request.query.Get("orderby") != "nodename,node_id" || request.query.Get("limit") != "2" || request.query.Get("offset") != "0" ||
		request.query.Get("props") != strings.Join(nodeSummaryProps, ",") {
		t.Fatalf("request = %+v", request)
	}
	wantFilters := []string{`nodename:~app\.0`, "cluster_id:eq:1a85097c-b382-4864-93f3-3b1ba42dd34f", "app:eq:WEB", "node_env:eq:DEV"}
	if strings.Join(request.query["filter"], "|") != strings.Join(wantFilters, "|") {
		t.Fatalf("filters = %q", request.query["filter"])
	}
	if nodes.Provenance.Source != "opensvc_collector" || nodes.Total != 5 || nodes.Count != 2 || !nodes.Truncated || nodes.NextCursor == "" {
		t.Fatalf("page = %+v", nodes)
	}
	first := nodes.Nodes[0]
	if first.Nodename != "ams-dev-app01" || first.Env != "DEV" || first.Status != "up" || first.OS != "linux Ubuntu 22.04" ||
		first.AgentVersion != "3.0.4" || first.LastComm != "2026-09-15 15:10:00.000" || !first.Frozen || nodes.Nodes[1].Frozen {
		t.Fatalf("nodes = %+v", nodes.Nodes)
	}

	// The cursor continues the same listing, and only that one.
	next, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{
		NodeFilters: NodeFilters{Name: "app.0", ClusterID: "1a85097c-b382-4864-93f3-3b1ba42dd34f", App: "WEB", Env: "DEV"}, Limit: 2, Cursor: nodes.NextCursor,
	})
	if err != nil || collector.requests[1].query.Get("offset") != "2" || next.NextCursor == "" {
		t.Fatalf("next page = %+v, %v, request %+v", next, err, collector.requests[1])
	}
	if _, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{Name: "other"}, Limit: 2, Cursor: nodes.NextCursor}); err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("cursor with other filters error = %v", err)
	}
}

func TestListNodesEndsOnTheLastPage(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"node_id":"a","nodename":"n1"}],"meta":{"total":1}}`}
	nodes, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{})
	if err != nil || nodes.Truncated || nodes.NextCursor != "" || collector.requests[0].query.Get("limit") != "50" || len(collector.requests[0].query["filter"]) != 0 {
		t.Fatalf("page = %+v, %v, request %+v", nodes, err, collector.requests[0])
	}
}

func TestListNodesRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListNodesOptions{
		{Limit: -1},
		{Limit: 201},
		{NodeFilters: NodeFilters{ClusterID: "dev5"}},
		{NodeFilters: NodeFilters{Name: " padded"}},
		{NodeFilters: NodeFilters{Name: "line\nbreak"}},
		{NodeFilters: NodeFilters{App: strings.Repeat("a", 256)}},
		{Cursor: "not-a-cursor"},
		{Cursor: strings.Repeat("a", 300)},
	} {
		collector := &fakeCollector{body: testNodeListBody}
		if _, err := newTestService(collector).ListNodes(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestListNodesRejectsUnexpectedResponses(t *testing.T) {
	for _, body := range []string{
		`{"data":[]}`,
		`{"data":[],"meta":{"total":-1}}`,
		`{"data":[{},{},{}],"meta":{"total":3}}`,
	} {
		if _, err := newTestService(&fakeCollector{body: body}).ListNodes(t.Context(), ListNodesOptions{Limit: 2}); err == nil {
			t.Fatalf("response %s accepted", body)
		}
	}
	upstream := errors.New("collector unavailable")
	if _, err := newTestService(&fakeCollector{err: upstream}).ListNodes(t.Context(), ListNodesOptions{}); !errors.Is(err, upstream) {
		t.Fatalf("upstream error = %v", err)
	}
}

func TestGetNodeMapsTheNode(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"node_id":"1c62e2f5-41a8-43ac-af36-60f3c8d1ffbc","nodename":"dev2n1","fqdn":"dev2n1.example.com","cluster_id":"82e47bfb-182f-413b-b5dd-9de8164095d5",
		"node_env":"REC","status":"","node_frozen":"F","version":"v3.0.0-rc40","last_comm":"2026-10-12 11:00:07.000","os_name":"Linux","os_release":"24.04.5 LTS","tz":"+02:00",
		"cpu_cores":16,"cpu_threads":32,"cpu_freq":"2000","mem_bytes":12288,"team_responsible":"DBA","loc_city":"Paris","os_obs_alert_date":"2026-10-03 00:00:00.000"}]}`}
	node, err := newTestService(collector).GetNode(t.Context(), "dev2n1")
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/nodes/dev2n1" || request.query.Get("meta") != "false" || request.query.Get("props") != strings.Join(nodeDetailProps, ",") {
		t.Fatalf("request = %+v", request)
	}
	if node.Node.Nodename != "dev2n1" || node.Node.Env != "REC" || node.Node.Frozen || node.Agent.Version != "v3.0.0-rc40" || node.OS.Timezone != "+02:00" ||
		node.Hardware.CPUCores != 16 || node.Hardware.MemMB != 12288 || node.Teams.Responsible != "DBA" || node.Location.City != "Paris" ||
		node.Lifecycle.OSObsAlertDate != "2026-10-03 00:00:00.000" || node.Provenance.Source != "opensvc_collector" {
		t.Fatalf("node = %+v", node)
	}
}

func TestGetNodeRefusesAnAmbiguousName(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"node_id":"id-a","nodename":"n1","cluster_id":"c-a"},{"node_id":"id-b","nodename":"n1","cluster_id":"c-b"}]}`}
	_, err := newTestService(collector).GetNode(t.Context(), "n1")
	if err == nil || !strings.Contains(err.Error(), "id-a (cluster c-a)") || !strings.Contains(err.Error(), "id-b (cluster c-b)") {
		t.Fatalf("ambiguous name error = %v", err)
	}
	if _, err := newTestService(&fakeCollector{body: `{"data":[]}`}).GetNode(t.Context(), "n1"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("empty answer error = %v", err)
	}
}

func TestGetNodeRejectsInvalidNodes(t *testing.T) {
	for _, node := range []string{"", "a/b", "two words", "line\nbreak", strings.Repeat("a", 256)} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).GetNode(t.Context(), node); err == nil || len(collector.requests) != 0 {
			t.Fatalf("node %q accepted", node)
		}
	}
}

func TestListNodesFiltersFrozenNodes(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"node_id":"n-1","nodename":"dev2n1","node_frozen":"T","node_frozen_at":""}],"meta":{"total":1}}`}
	nodes, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{Frozen: true}})
	if err != nil {
		t.Fatal(err)
	}
	if filters := strings.Join(collector.requests[0].query["filter"], "|"); filters != "node_frozen:eq:T" ||
		!strings.Contains(collector.requests[0].query.Get("props"), "node_frozen_at") {
		t.Fatalf("request = %+v", collector.requests[0])
	}
	if !nodes.Nodes[0].Frozen || nodes.Nodes[0].FrozenAt != "" {
		t.Fatalf("nodes = %+v", nodes.Nodes)
	}
	collector = &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{FrozenBefore: "2026-10-01 12:00"}}); err != nil {
		t.Fatal(err)
	}
	if filters := strings.Join(collector.requests[0].query["filter"], "|"); filters != "node_frozen:eq:T|node_frozen_at:lte:2026-10-01 12:00" {
		t.Fatalf("frozen_before filters = %q", filters)
	}
	for _, date := range []string{"yesterday", "2026-10-01T12:00:00", "2026-10-01 12"} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{FrozenBefore: date}}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("frozen_before %q accepted", date)
		}
	}
}

func TestNodeSummaryAlwaysCarriesFrozenAt(t *testing.T) {
	data, err := json.Marshal(NodeSummary{NodeID: "n-1", Nodename: "dev2n1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"frozen_at":""`) {
		t.Fatalf("summary = %s", data)
	}
}

func TestListNodesFiltersInventory(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	_, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{
		OSName: "linux", OSVendor: "red hat", OSRelease: "9.", AgentVersion: "3.0.4", Manufacturer: "Dell", Model: "R740",
		Team: "Network", Country: "France", City: "Paris", SilentFor: "7d",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `os_name:~linux|os_vendor:~red hat|os_release:~^9\.|version:eq:3.0.4|manufacturer:~Dell|model:~R740|team_responsible:~Network|loc_country:~France|loc_city:~Paris|last_comm:lt:-7d`
	if filters := strings.Join(collector.requests[0].query["filter"], "|"); filters != want {
		t.Fatalf("filters = %q", filters)
	}
	for _, options := range []NodeFilters{{SilentFor: "7"}, {SilentFor: "1w"}, {SilentFor: "0d"}, {SilentFor: "-7d"}, {OSName: " linux"}, {Team: "a\nb"}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: options}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("filters %+v accepted", options)
		}
	}
}

func TestCountNodes(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"node_id":"n-1"}],"meta":{"total":484}}`}
	count, err := newTestService(collector).CountNodes(t.Context(), CountNodesOptions{NodeFilters: NodeFilters{Env: "PRD"}})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if count.Total != 484 || count.Groups != nil || count.Truncated || request.path != "/nodes" || request.query.Get("limit") != "1" ||
		request.query.Get("stats") != "" || strings.Join(request.query["filter"], "|") != "node_env:eq:PRD" {
		t.Fatalf("count = %+v, request %+v", count, request)
	}

	collector = &fakeCollector{body: `{"data":{"os_name":{"windows":33,"Linux":181,"empty":2,"sunos":7}},"meta":{"distinct":{"os_name":5},"other":{"os_name":3},"total":226}}`}
	count, err = newTestService(collector).CountNodes(t.Context(), CountNodesOptions{NodeFilters: NodeFilters{Env: "PRD"}, GroupBy: "os_name", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	request = collector.requests[0]
	if request.query.Get("stats") != "true" || request.query.Get("props") != "os_name" || request.query.Get("limit") != "4" {
		t.Fatalf("request = %+v", request)
	}
	want := []CountGroup{{"Linux", 181}, {"windows", 33}, {"sunos", 7}, {"", 2}}
	if count.Total != 226 || count.GroupBy != "os_name" || count.Distinct != 5 || count.Other != 3 || !count.Truncated || fmt.Sprint(count.Groups) != fmt.Sprint(want) {
		t.Fatalf("count = %+v", count)
	}

	collector = &fakeCollector{body: `{"data":{"version":{"3.0.4":2}},"meta":{"total":2}}`}
	if _, err := newTestService(collector).CountNodes(t.Context(), CountNodesOptions{GroupBy: "agent_version"}); err != nil || collector.requests[0].query.Get("props") != "version" || collector.requests[0].query.Get("limit") != "20" {
		t.Fatalf("agent_version grouping: %v, %+v", err, collector.requests)
	}
	for _, options := range []CountNodesOptions{{GroupBy: "os"}, {GroupBy: "nodename"}, {Limit: 101}, {NodeFilters: NodeFilters{SilentFor: "x"}}} {
		collector := &fakeCollector{}
		if _, err := newTestService(collector).CountNodes(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	for _, body := range []string{`{"data":{"other":{"a":1}},"meta":{"total":1}}`, `{"data":{"os_name":{"a":1}},"meta":{}}`, `{"data":[],"meta":{}}`} {
		if _, err := newTestService(&fakeCollector{body: body}).CountNodes(t.Context(), CountNodesOptions{GroupBy: "os_name"}); err == nil {
			t.Fatalf("response %s accepted", body)
		}
	}
}

// The status of a node is the one of the collector Nodes page: filtered,
// counted and grouped as is, and returned as status.
func TestNodeStatusIsFilteredGroupedAndReturned(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"node_id":"n-1","nodename":"dev2n1","status":"down"}],"meta":{"total":1}}`}
	nodes, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{OSName: "linux", Status: "down"}})
	if err != nil {
		t.Fatal(err)
	}
	if filters := strings.Join(collector.requests[0].query["filter"], "|"); filters != "status:eq:down|os_name:~linux" {
		t.Fatalf("filters = %q", filters)
	}
	data, _ := json.Marshal(nodes.Nodes[0])
	if nodes.Nodes[0].Status != "down" || !strings.Contains(string(data), `"status":"down"`) || strings.Contains(string(data), "asset_status") {
		t.Fatalf("node = %s", data)
	}
	collector = &fakeCollector{body: `{"data":{"status":{"up":287,"down":98,"warn":94,"empty":5}},"meta":{"distinct":{"status":4},"other":{"status":0},"total":484}}`}
	count, err := newTestService(collector).CountNodes(t.Context(), CountNodesOptions{GroupBy: "status"})
	if err != nil || collector.requests[0].query.Get("props") != "status" || count.Groups[1] != (CountGroup{"down", 98}) || count.Groups[3] != (CountGroup{"", 5}) {
		t.Fatalf("count = %+v, %v", count, err)
	}
	if _, err := newTestService(&fakeCollector{}).CountNodes(t.Context(), CountNodesOptions{GroupBy: "asset_status"}); err == nil {
		t.Fatal("the former asset_status grouping is accepted")
	}
	if _, err := newTestService(&fakeCollector{}).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{Status: " down"}}); err == nil {
		t.Fatal("a padded status is accepted")
	}
}

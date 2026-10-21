package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/oc3-mcp/internal/testutil"
)

type toolCaller func(name string, arguments map[string]any) (*mcp.CallToolResult, map[string]any)

// newToolCaller serves the MCP over a fake collector answering with
// collectorHandler, and returns a function calling its tools.
func newToolCaller(t *testing.T, collectorHandler http.HandlerFunc) toolCaller {
	t.Helper()
	return newToolCallerWithActions(t, false, collectorHandler)
}

// newToolCallerWithActions serves the tools, with the action tools when
// actions is set.
func newToolCallerWithActions(t *testing.T, actions bool, collectorHandler http.HandlerFunc) toolCaller {
	t.Helper()
	collector := testutil.NewCollector(t, collectorHandler)
	api, err := newCollectorClient(config.Config{CollectorURL: collector.URL, CollectorCAFile: collector.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newHandler(api, testDelegator(t), actions)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: testutil.Token, requestID: testRequestID}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return func(name string, arguments map[string]any) (*mcp.CallToolResult, map[string]any) {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatalf("call %s: %v", name, err)
		}
		var structured map[string]any
		if result.StructuredContent != nil {
			data, _ := json.Marshal(result.StructuredContent)
			_ = json.Unmarshal(data, &structured)
		}
		return result, structured
	}
}

func notFound(w http.ResponseWriter, text string) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"text":"` + text + `"}`))
}

func TestNodeToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/nodes":
			if r.URL.Query().Get("filter") != "node_env:eq:PRD" {
				t.Errorf("filters = %q", r.URL.Query()["filter"])
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1","node_env":"PRD","node_frozen":"F"}],"meta":{"total":1}}`))
		case "/nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1","node_env":"PRD","mem_bytes":4096}]}`))
		case "/filtersets/prd-linux":
			_, _ = w.Write([]byte(`{"data":[{"id":282,"fset_name":"prd-linux"}]}`))
		case "/filtersets/282/nodes":
			if r.URL.Query().Get("filter") != "node_env:eq:PRD" {
				t.Errorf("filterset filters = %q", r.URL.Query()["filter"])
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1","node_env":"PRD","node_frozen":"F"}],"meta":{"total":187}}`))
		default:
			notFound(w, "node missing not found")
		}
	})

	result, members := call("list_nodes", map[string]any{"env": "PRD", "filterset": "prd-linux"})
	if result.IsError || members["total"] != float64(187) || members["filterset"].(map[string]any)["name"] != "prd-linux" {
		t.Fatalf("list_nodes filterset = %+v", members)
	}
	if result, _ := call("list_nodes", map[string]any{"tag": "pci-dss", "filterset": "prd-linux"}); !result.IsError {
		t.Fatal("list_nodes accepted tag and filterset")
	}
	result, list := call("list_nodes", map[string]any{"env": "PRD"})
	if result.IsError || list["total"] != float64(1) || len(list["nodes"].([]any)) != 1 {
		t.Fatalf("list_nodes = %+v", list)
	}
	result, node := call("get_node", map[string]any{"node": "n1"})
	hardware, _ := node["hardware"].(map[string]any)
	if result.IsError || hardware["mem_mb"] != float64(4096) {
		t.Fatalf("get_node = %+v", node)
	}
	result, _ = call("get_node", map[string]any{"node": "missing"})
	text := result.Content[0].(*mcp.TextContent).Text
	if !result.IsError || !strings.Contains(text, "HTTP 404") || !strings.Contains(text, "node missing not found") {
		t.Fatalf("missing node = %v %q", result.IsError, text)
	}
}

func TestServiceToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1"}]}`))
		case "/services_instances":
			if strings.Join(r.URL.Query()["filter"], "|") != "node_id:eq:id-1|services.svc_availstatus:eq:down" {
				t.Errorf("filters = %q", r.URL.Query()["filter"])
			}
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s1","services.svcname":"ns1/svc/web","services.svc_availstatus":"down"}],"meta":{"total":1}}`))
		case "/services/ns1%2Fsvc%2Fweb":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s1","svcname":"ns1/svc/web","svc_availstatus":"down","svc_availability":42.5}]}`))
		case "/services/s1/nodes":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodes.nodename":"n1","mon_availstatus":"down","mon_frozen":"0"}],"meta":{"total":1}}`))
		case "/resources":
			if strings.Join(r.URL.Query()["filter"], "|") != "svc_id:eq:s1|res_status:eq:down" {
				t.Errorf("filters = %q", r.URL.Query()["filter"])
			}
			_, _ = w.Write([]byte(`{"data":[{"rid":"disk#1","res_type":"disk.disk","res_status":"down","nodes.nodename":"n1","res_disable":"T"}],"meta":{"total":1}}`))
		case "/services/s1/status_log":
			if r.URL.Query().Get("days") != "30" {
				t.Errorf("days = %q", r.URL.Query().Get("days"))
			}
			_, _ = w.Write([]byte(`{"availability":{"rate":12.5,"from":"a","to":"b"},"data":[{"begin":"a","end":"b","status":"down"}]}`))
		default:
			notFound(w, "service missing not found")
		}
	})

	result, list := call("list_services", map[string]any{"node": "n1", "availstatus": "down"})
	node, _ := list["node"].(map[string]any)
	if result.IsError || list["total"] != float64(1) || node["node_id"] != "id-1" {
		t.Fatalf("list_services = %+v", list)
	}
	result, service := call("get_service", map[string]any{"service": "ns1/svc/web"})
	instances, _ := service["instances"].(map[string]any)
	availability, _ := service["availability"].(map[string]any)
	if result.IsError || instances["total"] != float64(1) || availability["rate_percent"] != 42.5 {
		t.Fatalf("get_service = %+v", service)
	}
	result, resources := call("list_service_resources", map[string]any{"service": "ns1/svc/web", "status": "down"})
	items, _ := resources["resources"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["disabled"] != true || items[0].(map[string]any)["nodename"] != "n1" {
		t.Fatalf("list_service_resources = %+v", resources)
	}
	result, history := call("get_service_status_history", map[string]any{"service": "ns1/svc/web", "days": 30})
	rate, _ := history["availability"].(map[string]any)
	if result.IsError || history["total"] != float64(1) || rate["rate_percent"] != 12.5 {
		t.Fatalf("get_service_status_history = %+v", history)
	}
	result, _ = call("get_service", map[string]any{"service": "missing"})
	if !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "service missing not found") {
		t.Fatalf("missing service = %+v", result)
	}
}

func TestAlertToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/alerts":
			if strings.Join(r.URL.Query()["filter"], "|") != "dash_severity:gte:3" {
				t.Errorf("filters = %q", r.URL.Query()["filter"])
			}
			_, _ = w.Write([]byte(`{"data":[{"id":1,"dash_type":"service unavailable","dash_severity":3,"alert":"current availability status: down","svc_id":"s1","services.svcname":"web"}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, alerts := call("list_alerts", map[string]any{"min_severity": 3})
	items, _ := alerts["alerts"].([]any)
	if result.IsError || alerts["total"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["severity"] != float64(3) {
		t.Fatalf("list_alerts = %+v", alerts)
	}
	result, _ = call("list_alerts", map[string]any{"min_severity": 9})
	if !result.IsError {
		t.Fatal("min_severity 9 was accepted")
	}
}

func TestServiceActionToolsReadTheCollector(t *testing.T) {
	const session = "4e54c743-29a2-46fa-b05f-9828136f008c"
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.EscapedPath() != "/services_actions" {
			notFound(w, "missing")
			return
		}
		if r.URL.Query().Get("filter") == "sid:eq:"+session {
			_, _ = w.Write([]byte(`{"data":[{"id":1,"log_type":"status","action":"start","status":"err"},{"id":2,"log_type":"","status":"err","status_log":"virsh exit code 1"}],"meta":{"total":2}}`))
			return
		}
		if strings.Join(r.URL.Query()["filter"], "|") != "log_type:eq:status|status:eq:err" {
			t.Errorf("filters = %q", r.URL.Query()["filter"])
		}
		_, _ = w.Write([]byte(`{"data":[{"id":1,"sid":"` + session + `","action":"start","status":"err"}],"meta":{"total":1}}`))
	})
	result, actions := call("list_service_actions", map[string]any{"status": "err"})
	items, _ := actions["actions"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["session_id"] != session {
		t.Fatalf("list_service_actions = %+v", actions)
	}
	result, logs := call("get_service_action_logs", map[string]any{"session_id": session})
	entries, _ := logs["logs"].([]any)
	if result.IsError || len(entries) != 1 || entries[0].(map[string]any)["text"] != "virsh exit code 1" {
		t.Fatalf("get_service_action_logs = %+v", logs)
	}
}

func TestCheckToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1"}]}`))
		case "/services/web":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s1","svcname":"web"}]}`))
		case "/nodes/id-1/checks", "/services/s1/checks":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"node_id":"id-1","svc_id":"s1","chk_type":"fs_u","chk_instance":"/srv/web.free","chk_value":40,"chk_low":1500,"chk_high":null,"chk_err":1},
				{"id":2,"node_id":"id-1","chk_type":"eth","chk_instance":"eth0.link","chk_value":1,"chk_low":1,"chk_high":1,"chk_err":0}],"meta":{"total":2}}`))
		case "/nodes/id-1/services":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s1","services.svcname":"web"}]}`))
		case "/services/s1/nodes":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodes.nodename":"n1"}]}`))
		default:
			notFound(w, "missing")
		}
	})
	result, checks := call("list_node_checks", map[string]any{"node": "n1", "out_of_bounds_only": true})
	items, _ := checks["checks"].([]any)
	if result.IsError || checks["total"] != float64(2) || len(items) != 1 || items[0].(map[string]any)["svcname"] != "web" || items[0].(map[string]any)["high"] != nil {
		t.Fatalf("list_node_checks = %+v", checks)
	}
	result, checks = call("list_service_checks", map[string]any{"service": "web"})
	items, _ = checks["checks"].([]any)
	if result.IsError || len(items) != 2 || items[0].(map[string]any)["nodename"] != "n1" {
		t.Fatalf("list_service_checks = %+v", checks)
	}
	if result, _ := call("list_node_checks", map[string]any{}); !result.IsError {
		t.Fatal("list_node_checks without node succeeded")
	}
}

func TestClusterToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.EscapedPath() != "/clusters" {
			notFound(w, "missing")
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"cluster_id":"82e47bfb-182f-413b-b5dd-9de8164095d5","cluster_name":"reliable-leopard","cluster_nodes":"n1, n2","quorum":1}],"meta":{"total":1}}`))
	})
	result, clusters := call("list_clusters", map[string]any{"name": "reliable"})
	items, _ := clusters["clusters"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["name"] != "reliable-leopard" || len(items[0].(map[string]any)["nodes"].([]any)) != 2 {
		t.Fatalf("list_clusters = %+v", clusters)
	}
}

func TestPackageToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1"}]}`))
		case "/nodes/n2":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-2","nodename":"n2"}]}`))
		case "/packages":
			_, _ = w.Write([]byte(`{"data":[{"pkg_name":"gh","pkg_version":"2"}],"meta":{"total":1}}`))
		case "/packages/diff":
			if r.URL.Query().Get("node_ids") != "id-1,id-2" {
				t.Errorf("node_ids = %q", r.URL.Query().Get("node_ids"))
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","pkg_name":"gh","pkg_version":"2"}],"meta":{"nodes":[{"node_id":"id-1","nodename":"n1"},{"node_id":"id-2","nodename":"n2"}]}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, differences := call("list_package_differences", map[string]any{"nodes": []string{"n1", "n2"}})
	items, _ := differences["packages"].([]any)
	if result.IsError || differences["missing"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["missing_on"].([]any)[0] != "n2" {
		t.Fatalf("list_package_differences = %+v", differences)
	}
	result, packages := call("list_node_packages", map[string]any{"node": "n1", "name": "gh"})
	items, _ = packages["packages"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["version"] != "2" {
		t.Fatalf("list_node_packages = %+v", packages)
	}
	if result, _ := call("list_package_differences", map[string]any{"nodes": []string{"n1"}}); !result.IsError {
		t.Fatal("a single node was accepted")
	}
}

func TestFrozenToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/nodes":
			if strings.Join(r.URL.Query()["filter"], "|") != "node_frozen:eq:T|node_frozen_at:lte:2026-10-01" {
				t.Errorf("node filters = %q", r.URL.Query()["filter"])
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1","node_frozen":"T","node_frozen_at":"2026-09-30 10:00:00"}],"meta":{"total":1}}`))
		case "/services_instances":
			if strings.Join(r.URL.Query()["filter"], "|") != "mon_frozen:ne:0" {
				t.Errorf("instance filters = %q", r.URL.Query()["filter"])
			}
			if r.URL.Query().Get("stats") == "true" {
				_, _ = w.Write([]byte(`{"data":{"nodes.nodename":{"n1":1}},"meta":{"total":1,"distinct":{"nodes.nodename":1},"other":{"nodes.nodename":0}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s1","services.svcname":"web","node_id":"id-1","nodes.nodename":"n1","mon_frozen":"1","mon_frozen_at":"2026-09-12 00:22:06"}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, nodes := call("list_nodes", map[string]any{"frozen_before": "2026-10-01"})
	items, _ := nodes["nodes"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["frozen_at"] != "2026-09-30 10:00:00" {
		t.Fatalf("list_nodes = %+v", nodes)
	}
	result, instances := call("list_instances", map[string]any{"frozen": true})
	items, _ = instances["instances"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["frozen_at"] != "2026-09-12 00:22:06" || items[0].(map[string]any)["frozen"] != true {
		t.Fatalf("list_instances = %+v", instances)
	}
	result, count := call("count_instances", map[string]any{"frozen": true, "group_by": "node"})
	groups, _ := count["groups"].([]any)
	if result.IsError || count["total"] != float64(1) || len(groups) != 1 || groups[0].(map[string]any)["value"] != "n1" {
		t.Fatalf("count_instances = %+v", count)
	}
}

func TestSanToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1"}]}`))
		case "/nodes/id-1/san":
			_, _ = w.Write([]byte(`{"data":{"nodes":[{"id":"server:id-1","kind":"server","label":"n1","ports":["hba"]},
				{"id":"switch:a","kind":"switch","label":"sw-a","fabric":"fab-a"},{"id":"array:x","kind":"array","label":"x"}],
				"links":[{"tail":"server:id-1","tail_port":"hba","head":"switch:a","head_port":"1","speeds":[16]},
				{"tail":"switch:a","tail_port":"2","head":"array:x","head_port":"t1","speeds":[32]}]}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, topology := call("get_node_san_topology", map[string]any{"node": "n1"})
	arrays, _ := topology["arrays"].([]any)
	if result.IsError || topology["wired"] != true || len(arrays) != 1 || arrays[0].(map[string]any)["paths"] != float64(1) {
		t.Fatalf("get_node_san_topology = %+v", topology)
	}
}

func TestDiskToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.EscapedPath() == "/nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1"}]}`))
		case r.URL.EscapedPath() == "/disks" && strings.HasPrefix(r.URL.Query().Get("filter"), "disk_id:in:"):
			_, _ = w.Write([]byte(`{"data":[{"disk_id":"360014050afbff271f0a4e019f692be55","node_id":"id-2","nodename":"n2"}]}`))
		case r.URL.EscapedPath() == "/disks":
			_, _ = w.Write([]byte(`{"data":[{"disk_id":"360014050afbff271f0a4e019f692be55","disk_size":1024}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, disks := call("list_node_disks", map[string]any{"node": "n1"})
	items, _ := disks["disks"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["shared_with"].([]any)[0] != "n2" {
		t.Fatalf("list_node_disks = %+v", disks)
	}
}

func TestStatsToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1"}]}`))
		case "/nodes/id-1/stats":
			if r.URL.Query().Get("group") != "mem" {
				t.Errorf("group = %q", r.URL.Query().Get("group"))
			}
			_, _ = w.Write([]byte(`{"data":[{"metric":"pct_memused","points":[[1791792000,70.5],[1791792600,77.6]]}]}`))
		default:
			notFound(w, "missing")
		}
	})
	result, stats := call("get_node_stats", map[string]any{"node": "n1", "group": "mem"})
	series, _ := stats["series"].([]any)
	if result.IsError || len(series) != 1 || series[0].(map[string]any)["max"] != 77.6 {
		t.Fatalf("get_node_stats = %+v", stats)
	}
	if result, _ := call("get_node_stats", map[string]any{"node": "n1", "group": "gpu"}); !result.IsError {
		t.Fatal("an unknown group was accepted")
	}
}

func TestResourceHistoryToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/services/web":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s1","svcname":"web"}]}`))
		case "/services/s1/resources/logs":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","rid":"fs#1","res_status":"down","res_begin":"2026-10-11 23:00:00","res_end":"2026-10-12 10:00:00"}],"meta":{"total":1}}`))
		case "/services/s1/nodes":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodes.nodename":"n1"}]}`))
		default:
			notFound(w, "missing")
		}
	})
	result, history := call("get_service_resource_history", map[string]any{"service": "web"})
	resources, _ := history["resources"].([]any)
	if result.IsError || len(resources) != 1 || resources[0].(map[string]any)["current_status"] != "down" || resources[0].(map[string]any)["nodename"] != "n1" {
		t.Fatalf("get_service_resource_history = %+v", history)
	}
}

func TestCollectorEventToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/logs":
			if r.URL.Query()["filter"][0] != "log_action:!in:user.login,user.logout" {
				t.Errorf("filters = %q", r.URL.Query()["filter"])
			}
			_, _ = w.Write([]byte(`{"data":[{"id":1,"log_date":"2026-10-07 12:42:33","log_action":"service.action","log_level":"info","log_user":"admin@example.com","log_fmt":"run %(action)s","log_dict":"{\"action\":\"thaw\"}"}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, events := call("list_collector_events", map[string]any{})
	items, _ := events["events"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["message"] != "run thaw" {
		t.Fatalf("list_collector_events = %+v", events)
	}
}

func TestComplianceToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/compliance/status":
			if filters := strings.Join(r.URL.Query()["filter"], "|"); filters != `run_module:~^sec\.|run_status:eq:1` {
				t.Errorf("filters = %q", filters)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":1102,"run_module":"sec.ssh","node_id":"e1d22de4-07ea-47d1-ac46-8bf014d0cbec","nodes.nodename":"n1","svc_id":"","services.svcname":null,
				"run_action":"check","run_status":1,"run_date":"2026-10-11 10:48:10","run_log":"PermitRootLogin is yes\nERR: PermitRootLogin should be no"}],"meta":{"total":1}}`))
		case "/compliance/modulesets":
			_, _ = w.Write([]byte(`{"data":[{"id":3,"modset_name":"base.linux"}],"meta":{"total":1}}`))
		case "/compliance/modulesets_modules":
			_, _ = w.Write([]byte(`{"data":[{"id":10,"modset_id":3,"modset_mod_name":"linux.files","autofix":"T"}],"meta":{"total":1}}`))
		case "/compliance/rulesets":
			_, _ = w.Write([]byte(`{"data":[{"id":22,"ruleset_name":"os.linux.base","ruleset_type":"explicit","ruleset_public":"T"}],"meta":{"total":1}}`))
		case "/compliance/rulesets/os.linux.base":
			_, _ = w.Write([]byte(`{"data":[{"id":22,"ruleset_name":"os.linux.base","ruleset_type":"explicit","ruleset_public":"T"}]}`))
		case "/compliance/rulesets/22/export":
			_, _ = w.Write([]byte(`{"filtersets":[],"rulesets":[{"id":22,"ruleset_name":"os.linux.base","ruleset_type":"explicit","fset_name":null,"rulesets":[],
				"variables":[{"var_name":"root_password","var_class":"raw","var_value":"s3cret"},{"var_name":"motd","var_class":"file","var_value":"{\"path\":\"/etc/motd\"}"}]}]}`))
		case "/compliance/rulesets/22/usage":
			_, _ = w.Write([]byte(`{"data":{"modulesets":[],"rulesets":[{"id":23,"ruleset_name":"os.linux.security"}],"nodes":[],"services":[]}}`))
		case "/filtersets":
			_, _ = w.Write([]byte(`{"data":[{"id":283,"fset_name":"big-memory"}],"meta":{"total":1}}`))
		case "/filtersets/big-memory":
			_, _ = w.Write([]byte(`{"data":[{"id":283,"fset_name":"big-memory"}]}`))
		case "/filtersets/283/export":
			_, _ = w.Write([]byte(`{"filtersets":[{"id":283,"fset_name":"big-memory","filters":[{"f_log_op":"AND","f_order":1,"filter":{"f_table":"nodes","f_field":"mem_bytes","f_op":">=","f_value":"131072"}}]}]}`))
		case "/filtersets/283/nodes", "/filtersets/283/services":
			_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0}}`))
		case "/filtersets/283/usage":
			_, _ = w.Write([]byte(`{"data":{"rulesets":[{"id":1030,"ruleset_name":"ctx.big-memory"}],"users":[{"id":1,"email":"jane@example.com","name":"Jane Doe"}]}}`))
		case "/compliance/modulesets/base.linux":
			_, _ = w.Write([]byte(`{"data":[{"id":3,"modset_name":"base.linux"}]}`))
		case "/compliance/modulesets/3/export":
			_, _ = w.Write([]byte(`{"filtersets":[],"rulesets":[],"modulesets":[{"id":3,"modset_name":"base.linux","modules":[{"modset_mod_name":"linux.files","autofix":"T"}],"modulesets":[],"rulesets":[],"publications":["Everybody"],"responsibles":["Infra Unix"]}]}`))
		case "/compliance/modulesets/3/usage":
			_, _ = w.Write([]byte(`{"data":{"modulesets":[{"id":1,"modset_name":"app.web"}]}}`))
		case "/compliance/modulesets/3/nodes", "/compliance/modulesets/3/services", "/compliance/rulesets/22/nodes", "/compliance/rulesets/22/services":
			_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0}}`))
		case "/compliance/logs":
			if filters := strings.Join(r.URL.Query()["filter"], "|"); filters != `run_module:eq:linux.files|run_action:eq:fix|run_date:lte:2026-09-30 23:59:59` {
				t.Errorf("filters = %q", filters)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":37,"run_module":"linux.files","node_id":"e1d22de4-07ea-47d1-ac46-8bf014d0cbec","nodes.nodename":"n1","svc_id":"","services.svcname":null,
				"run_action":"fix","run_status":0,"run_date":"2026-09-28 16:28:10","run_log":"/etc/motd created"}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, history := call("list_compliance_runs", map[string]any{"module": "linux.files", "action": "fix", "until": "2026-09-30"})
	runs, _ := history["runs"].([]any)
	if result.IsError || len(runs) != 1 || runs[0].(map[string]any)["action"] != "fix" || runs[0].(map[string]any)["status"] != "ok" {
		t.Fatalf("list_compliance_runs = %+v", history)
	}
	if result, _ := call("list_compliance_runs", map[string]any{"action": "repair"}); !result.IsError {
		t.Fatal("list_compliance_runs accepted action repair")
	}
	if result, _ := call("count_compliance_runs", map[string]any{"group_by": "date"}); !result.IsError {
		t.Fatal("count_compliance_runs accepted group_by date")
	}
	result, detail := call("get_compliance_moduleset", map[string]any{"moduleset": "base.linux"})
	if result.IsError || detail["included_by"].([]any)[0] != "app.web" || detail["responsibles"].([]any)[0] != "Infra Unix" ||
		detail["moduleset"].(map[string]any)["modules"].([]any)[0].(map[string]any)["name"] != "linux.files" {
		t.Fatalf("get_compliance_moduleset = %+v", detail)
	}
	if result, _ := call("get_compliance_moduleset", map[string]any{}); !result.IsError {
		t.Fatal("get_compliance_moduleset accepted no moduleset")
	}
	result, rulesets := call("list_compliance_rulesets", map[string]any{"type": "explicit"})
	if items, _ := rulesets["rulesets"].([]any); result.IsError || len(items) != 1 || items[0].(map[string]any)["name"] != "os.linux.base" {
		t.Fatalf("list_compliance_rulesets = %+v", rulesets)
	}
	result, ruleset := call("get_compliance_ruleset", map[string]any{"ruleset": "os.linux.base"})
	variables, _ := ruleset["variables"].([]any)
	if result.IsError || len(variables) != 2 || variables[1].(map[string]any)["value"] != "(masked)" || ruleset["included_by"].([]any)[0] != "os.linux.security" {
		t.Fatalf("get_compliance_ruleset = %+v", ruleset)
	}
	result, filtersets := call("list_filtersets", map[string]any{})
	if items, _ := filtersets["filtersets"].([]any); result.IsError || len(items) != 1 {
		t.Fatalf("list_filtersets = %+v", filtersets)
	}
	result, filterset := call("get_filterset", map[string]any{"filterset": "big-memory"})
	if result.IsError || filterset["criteria"].([]any)[0].(map[string]any)["filter"] != "nodes.mem_bytes >= 131072" ||
		filterset["rulesets"].([]any)[0] != "ctx.big-memory" || strings.Contains(fmt.Sprint(filterset), "Jane") || filterset["session_filter_users"] != float64(1) {
		t.Fatalf("get_filterset = %+v", filterset)
	}
	result, modulesets := call("list_compliance_modulesets", map[string]any{})
	items, _ := modulesets["modulesets"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["modules"].([]any)[0].(map[string]any)["autofix"] != true {
		t.Fatalf("list_compliance_modulesets = %+v", modulesets)
	}
	result, list := call("list_compliance_status", map[string]any{"module": "sec.", "status": "nok"})
	statuses, _ := list["statuses"].([]any)
	if result.IsError || len(statuses) != 1 || statuses[0].(map[string]any)["status"] != "nok" || statuses[0].(map[string]any)["nodename"] != "n1" {
		t.Fatalf("list_compliance_status = %+v", list)
	}
}

func TestIPToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/ips":
			if filters := strings.Join(r.URL.Query()["filter"], "|"); filters != "addr:eq:10.29.0.21" {
				t.Errorf("ips filters = %q", filters)
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"id-1","nodename":"n1","intf":"br-prd","type":"ipv4","addr":"10.29.0.21","mask":"24"}],"meta":{"total":1}}`))
		case "/resources":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","services.svcname":"testnfs","node_id":"id-1","nodes.nodename":"n1","rid":"ip#1","res_type":"ip.host","res_status":"up",
				"res_desc":"host 10.29.0.21/24 br-prd"}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, ips := call("list_ips", map[string]any{"ip": "10.29.0.21"})
	nodes, _ := ips["node_addresses"].([]any)
	services, _ := ips["service_addresses"].([]any)
	if result.IsError || len(nodes) != 1 || nodes[0].(map[string]any)["interface"] != "br-prd" || len(services) != 1 || services[0].(map[string]any)["svcname"] != "testnfs" {
		t.Fatalf("list_ips = %+v", ips)
	}
	if result, _ := call("list_ips", map[string]any{}); !result.IsError {
		t.Fatal("a call without criterion was accepted")
	}
}

func TestTagAndAppToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/tags":
			_, _ = w.Write([]byte(`{"data":[{"tag_id":"t-1","tag_name":"pci-dss","tag_data":{"scope":"compliance"}}],"meta":{"total":1}}`))
		case "/apps/WEB":
			_, _ = w.Write([]byte(`{"app":"WEB","id":4627,"description":"WEB application code"}`))
		case "/apps/4627/responsibles":
			_, _ = w.Write([]byte(`{"data":[{"role":"Test Web","description":"Test team"}]}`))
		case "/nodes", "/services":
			_, _ = w.Write([]byte(`{"data":[{}],"meta":{"total":2}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, tags := call("list_tags", map[string]any{})
	items, _ := tags["tags"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["data"] != `{"scope":"compliance"}` {
		t.Fatalf("list_tags = %+v", tags)
	}
	result, app := call("get_app", map[string]any{"app": "WEB"})
	groups, _ := app["responsibles"].([]any)
	if result.IsError || app["nodes"] != float64(2) || len(groups) != 1 || groups[0].(map[string]any)["role"] != "Test Web" {
		t.Fatalf("get_app = %+v", app)
	}
}

func TestFindObjectsReadsTheCollectorSearch(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/search":
			if r.URL.Query().Get("kinds") != "node,service,instance,app,network,disk,tag" {
				t.Errorf("kinds = %q", r.URL.Query().Get("kinds"))
			}
			_, _ = w.Write([]byte(`{"data":[{"kind":"node","items":[{"node_id":"n-1","nodename":"lyn-prd-app01","app":"INFRA"}],"more":true}]}`))
		default:
			notFound(w, "missing")
		}
	})
	result, found := call("find_objects", map[string]any{"text": "lyn-prd"})
	groups, _ := found["groups"].([]any)
	if result.IsError || len(groups) != 1 {
		t.Fatalf("find_objects = %+v", found)
	}
	item := groups[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["matched_by"] != "name" || item["next_tool"] != "get_node" || groups[0].(map[string]any)["more"] != true {
		t.Fatalf("find_objects item = %+v", item)
	}
}

func TestRequestToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/workflows":
			if filters := strings.Join(r.URL.Query()["filter"], "|"); filters != "status:eq:pending" && filters != "id:eq:1354" {
				t.Errorf("filters = %q", filters)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":1354,"form_head_id":1411,"form_name":"sample-vm-request","status":"pending","steps":1,"creator":" Manager","last_assignee":"Infra Unix"}],"meta":{"total":1}}`))
		case "/forms_store":
			_, _ = w.Write([]byte(`{"data":[{"form_name":"sample-vm-request","form_submitter":" Manager","form_assignee":"Infra Unix","form_data":"{\"hostname\": \"web-12\"}",
				"form_definition":{"Label":"Virtual machine request","Inputs":[{"Id":"hostname","Label":"Hostname"}]}}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, details := call("get_request", map[string]any{"id": 1354})
	steps, _ := details["steps"].([]any)
	if result.IsError || len(steps) != 1 || steps[0].(map[string]any)["values"].([]any)[0].(map[string]any)["value"] != "web-12" {
		t.Fatalf("get_request = %+v", details)
	}
	result, list := call("list_requests", map[string]any{"status": "pending"})
	items, _ := list["requests"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["assignee"] != "Infra Unix" || items[0].(map[string]any)["creator"] != "Manager" {
		t.Fatalf("list_requests = %+v", list)
	}
}

func TestFormToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/forms":
			_, _ = w.Write([]byte(`{"data":[{"id":198,"form_name":"sample-vm-request","form_type":"custo","form_yaml":"Label: Virtual machine request\n",
				"form_definition":{"Label":"Virtual machine request","Inputs":[{"Id":"hostname","Mandatory":true}],"Outputs":[{"Dest":"workflow"}]}}],"meta":{"total":1}}`))
		case "/forms/198/publications", "/forms/198/responsibles":
			_, _ = w.Write([]byte(`{"data":[{"role":"Infra Unix"}]}`))
		default:
			notFound(w, "missing")
		}
	})
	result, form := call("get_form", map[string]any{"form": "sample-vm-request"})
	inputs, _ := form["inputs"].([]any)
	if result.IsError || len(inputs) != 1 || inputs[0].(map[string]any)["mandatory"] != true || form["yaml"] != "Label: Virtual machine request\n" {
		t.Fatalf("get_form = %+v", form)
	}
	result, list := call("list_forms", map[string]any{"type": "custo"})
	items, _ := list["forms"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["label"] != "Virtual machine request" {
		t.Fatalf("list_forms = %+v", list)
	}
}

func TestStorageToolsReadTheCollector(t *testing.T) {
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/arrays":
			_, _ = w.Write([]byte(`{"data":[{"id":7,"array_name":"lyn-pmax-01","array_model":"PowerMax_8500","array_cache":2097152}],"meta":{"total":1}}`))
		case "/arrays/7/diskgroups":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"dg_name":"SRP_1","dg_size":1000,"dg_used":400,"dg_free":600}]}`))
		case "/apps/DATA/quotas":
			_, _ = w.Write([]byte(`{"data":[{"app":"DATA","array_name":"lyn-pmax-01","dg_name":"SRP_1","quota":300,"quota_used":100}]}`))
		case "/san-switches":
			if filters := strings.Join(r.URL.Query()["filter"], "|"); filters != "sw_portstate:eq:No_Light" {
				t.Errorf("san-switches filters = %q", filters)
			}
			_, _ = w.Write([]byte(`{"data":[{"sw_fabric":"a","sw_name":"edge01","sw_port":5,"sw_porttype":"U-Port","sw_portstate":"No_Light"}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, arrays := call("list_storage_arrays", map[string]any{})
	items, _ := arrays["arrays"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["free_mb"] != float64(600) || items[0].(map[string]any)["cache"] != "2097152" {
		t.Fatalf("list_storage_arrays = %+v", arrays)
	}
	result, quotas := call("list_storage_quotas", map[string]any{"app": "DATA"})
	items, _ = quotas["quotas"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["quota_mb"] != float64(300) {
		t.Fatalf("list_storage_quotas = %+v", quotas)
	}
	result, ports := call("list_san_switch_ports", map[string]any{"state": "No_Light"})
	items, _ = ports["ports"].([]any)
	if result.IsError || len(items) != 1 || items[0].(map[string]any)["switch"] != "edge01" {
		t.Fatalf("list_san_switch_ports = %+v", ports)
	}
	if result, _ := call("list_san_switch_ports", map[string]any{"type": "X-Port"}); !result.IsError {
		t.Fatal("list_san_switch_ports accepted type X-Port")
	}
}

func TestNodeActionToolsChangeTheCollector(t *testing.T) {
	var posted map[string]any
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.EscapedPath() {
		case "GET /nodes":
			_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0}}`))
		case "GET /apps/DATA":
			_, _ = w.Write([]byte(`{"id":3,"app":"DATA"}`))
		case "POST /nodes":
			_ = json.NewDecoder(r.Body).Decode(&posted)
			_, _ = w.Write([]byte(`{"node_id":"n-9","nodename":"lab01","app":"DATA","team_responsible":"user_170"}`))
		case "GET /nodes/lab01":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-9","nodename":"lab01"}]}`))
		case "GET /nodes/n-9":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-9","nodename":"lab01","status":""}]}`))
		case "POST /nodes/n-9":
			_, _ = w.Write([]byte(`{"node_id":"n-9","nodename":"lab01","status":"In service"}`))
		case "DELETE /nodes/n-9":
			_, _ = w.Write([]byte(`{"info":"delete node lab01"}`))
		default:
			notFound(w, "missing")
		}
	})
	result, created := call("create_node", map[string]any{"nodename": "lab01", "app": "DATA"})
	if result.IsError || created["node"].(map[string]any)["node_id"] != "n-9" || posted["nodename"] != "lab01" || posted["node_id"] != nil {
		t.Fatalf("create_node = %+v, posted %v", created, posted)
	}
	result, update := call("update_node", map[string]any{"node": "lab01", "status": "In service"})
	changes, _ := update["changes"].([]any)
	if result.IsError || len(changes) != 1 || changes[0].(map[string]any)["after"] != "In service" {
		t.Fatalf("update_node = %+v", update)
	}
	result, deletion := call("delete_node", map[string]any{"node": "lab01"})
	if result.IsError || deletion["deleted"] != true {
		t.Fatalf("delete_node = %+v", deletion)
	}
}

func TestNodeActionToolsNeedActionsEnabled(t *testing.T) {
	for _, actions := range []bool{false, true} {
		handler, err := newHandler(nil, testDelegator(t), actions)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		t.Cleanup(cancel)
		session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint: server.URL + "/mcp", HTTPClient: bearerClient(testutil.Token),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		tools, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]*mcp.Tool{}
		for _, tool := range tools.Tools {
			if !tool.Annotations.ReadOnlyHint {
				found[tool.Name] = tool
			}
		}
		if !actions {
			if len(found) != 0 {
				t.Fatalf("without actions, action tools %v", found)
			}
			continue
		}
		// The agent asks to confirm a destructive tool: the update, the
		// deletion, the detachments and the compliance attachments, not the
		// creation or the attachment of a tag.
		for name, destructive := range map[string]bool{"create_node": false, "update_node": true, "delete_node": true, "attach_node_tag": false, "detach_node_tag": true,
			"attach_node_ruleset": true, "detach_node_ruleset": true, "attach_node_moduleset": true, "detach_node_moduleset": true, "queue_node_action": false,
			"freeze_node": false, "unfreeze_node": true, "drain_node": true, "fix_node_compliance": true, "cancel_node_action": false,
			"attach_service_tag": false, "detach_service_tag": true,
			"attach_service_ruleset": true, "detach_service_ruleset": true, "attach_service_moduleset": true, "detach_service_moduleset": true,
			"justify_service_downtime": false, "unjustify_service_downtime": false, "set_service_sla": false,
			"freeze_service": false, "thaw_service": true, "start_service": false, "stop_service": true, "restart_service": true,
			"switch_service": true, "giveback_service": true, "takeover_service": true, "abort_service_action": true,
			"check_service_compliance": false, "fix_service_compliance": true,
			"delete_service": true, "delete_service_instance": true, "ack_service_action": true} {
			tool := found[name]
			if tool == nil || *tool.Annotations.DestructiveHint != destructive || *tool.Annotations.OpenWorldHint {
				t.Fatalf("%s = %+v", name, tool)
			}
			if keywords, ok := tool.Meta["opensvc/keywords"].([]any); !ok || len(keywords) == 0 {
				t.Fatalf("tool %s has no search keywords", name)
			}
		}
		// properties are the inputs of a tool, by name, and their
		// description: an input named twice keeps one of them only.
		properties := func(name string) map[string]string {
			data, _ := json.Marshal(found[name].InputSchema)
			var schema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			}
			_ = json.Unmarshal(data, &schema)
			inputs := map[string]string{}
			for input, property := range schema.Properties {
				inputs[input] = property.Description
			}
			return inputs
		}
		// The tag changed and the tag filter have their own names.
		for _, name := range []string{"attach_node_tag", "detach_node_tag", "attach_service_tag", "detach_service_tag"} {
			inputs := properties(name)
			if !strings.HasPrefix(inputs["tag"], "one exact tag") || inputs["filter_tag"] == "" || inputs["status"] == "" {
				t.Fatalf("%s inputs = %v", name, inputs)
			}
		}
		// The selections of services take the filters of their instances,
		// and those of list_nodes as an object.
		for _, name := range []string{"attach_service_tag", "detach_service_tag", "attach_service_ruleset", "detach_service_moduleset", "set_service_sla"} {
			inputs := properties(name)
			if inputs["node"] == "" || inputs["on_nodes"] == "" || inputs["instance_availability"] == "" || inputs["instance_frozen"] == "" {
				t.Fatalf("%s inputs = %v", name, inputs)
			}
		}
		// So have the ruleset or moduleset changed and its filter.
		for name, kinds := range map[string][2]string{
			"attach_node_ruleset": {"ruleset", "moduleset"}, "detach_node_ruleset": {"ruleset", "moduleset"},
			"attach_node_moduleset": {"moduleset", "ruleset"}, "detach_node_moduleset": {"moduleset", "ruleset"},
			"attach_service_ruleset": {"ruleset", "moduleset"}, "detach_service_ruleset": {"ruleset", "moduleset"},
			"attach_service_moduleset": {"moduleset", "ruleset"}, "detach_service_moduleset": {"moduleset", "ruleset"},
		} {
			inputs := properties(name)
			if !strings.HasPrefix(inputs[kinds[0]], "one exact compliance") || inputs["filter_"+kinds[0]] == "" || !strings.HasPrefix(inputs[kinds[1]], "optional") {
				t.Fatalf("%s inputs = %v", name, inputs)
			}
		}
		schema, _ := json.Marshal(found["update_node"].InputSchema)
		if !strings.Contains(string(schema), `"loc_city"`) || !strings.Contains(string(schema), `"required":["node"]`) {
			t.Fatalf("update_node schema = %s", schema)
		}
		// The justification of a downtime requires its comment and whether
		// the period counts: the model passes them, it does not default them.
		schema, _ = json.Marshal(found["justify_service_downtime"].InputSchema)
		if !strings.Contains(string(schema), `"required":["service","begin","comment","counted"]`) {
			t.Fatalf("justify_service_downtime schema = %s", schema)
		}
		// A takeover takes its destination; every action selects by name
		// or by filters.
		data, _ := json.Marshal(found["takeover_service"].InputSchema)
		if !strings.Contains(string(data), `"required":["to"]`) {
			t.Fatalf("takeover_service schema = %s", data)
		}
		for _, name := range []string{"freeze_service", "stop_service", "restart_service", "switch_service"} {
			data, _ := json.Marshal(found[name].InputSchema)
			if strings.Contains(string(data), `"required"`) || strings.Contains(string(data), "dry_run") || strings.Contains(string(data), "all_matching") {
				t.Fatalf("%s schema = %s", name, data)
			}
		}
		// A restart and a switch act again at each call; the others not.
		for name, idempotent := range map[string]bool{"restart_service": false, "switch_service": false, "takeover_service": true, "giveback_service": true, "freeze_service": true, "stop_service": true} {
			if found[name].Annotations.IdempotentHint != idempotent {
				t.Fatalf("%s idempotent = %v", name, found[name].Annotations.IdempotentHint)
			}
		}
		// local is an input of the actions on services or instances only.
		for name, local := range map[string]bool{"freeze_service": true, "stop_service": true, "switch_service": false, "abort_service_action": false, "restart_service": false, "takeover_service": false} {
			if _, ok := properties(name)["local"]; ok != local {
				t.Fatalf("%s local input = %v", name, ok)
			}
		}
		// resources is an input of start, stop and restart only.
		for name, resources := range map[string]bool{"start_service": true, "stop_service": true, "restart_service": true, "freeze_service": false, "thaw_service": false, "switch_service": false, "check_service_compliance": false} {
			if _, ok := properties(name)["resources"]; ok != resources {
				t.Fatalf("%s resources input = %v", name, ok)
			}
		}
		if len(found) != 38 {
			t.Fatalf("action tools = %v", found)
		}
	}
}

// The tag changed and the tag filter of the selection are two inputs: a
// tag is detached from the nodes holding it, read from the tag members.
func TestDetachNodeTagFromTheNodesHoldingIt(t *testing.T) {
	var mutex sync.Mutex
	var deleted []string
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch path := r.Method + " " + r.URL.EscapedPath(); {
		case path == "GET /tags":
			if filter := r.URL.Query().Get("filter"); filter != "tag_name:eq:tag-mcp" {
				t.Errorf("tag filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"tag_id":"t-1","tag_name":"tag-mcp"}]}`))
		case path == "GET /tags/t-1/nodes":
			if filter := r.URL.Query().Get("filter"); filter != "status:eq:In service" {
				t.Errorf("selection filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"a"},{"node_id":"n-2","nodename":"b"}],"meta":{"total":2}}`))
		case strings.HasPrefix(path, "DELETE /tags/t-1/nodes/"):
			mutex.Lock()
			deleted = append(deleted, strings.TrimPrefix(path, "DELETE /tags/t-1/nodes/"))
			mutex.Unlock()
			_, _ = w.Write([]byte(`{"info":"tag detached"}`))
		default:
			notFound(w, "missing")
		}
	})
	result, change := call("detach_node_tag", map[string]any{"tag": "tag-mcp", "filter_tag": "tag-mcp", "status": "In service"})
	if result.IsError || change["changed"] != float64(2) || change["filter_tag"].(map[string]any)["name"] != "tag-mcp" || len(deleted) != 2 {
		t.Fatalf("detach_node_tag = %+v, deleted %v", change, deleted)
	}
	if result, _ := call("detach_node_tag", map[string]any{"tag": "tag-mcp"}); !result.IsError || len(deleted) != 2 {
		t.Fatalf("a call selecting no node was accepted, deleted %v", deleted)
	}
}

func TestNodeComplianceToolsChangeTheSelectedNodes(t *testing.T) {
	var mutex sync.Mutex
	var changes []string
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch path := r.Method + " " + r.URL.EscapedPath(); {
		case path == "GET /compliance/rulesets/os.linux.base":
			_, _ = w.Write([]byte(`{"data":[{"id":12,"ruleset_name":"os.linux.base","ruleset_type":"explicit"}]}`))
		case path == "GET /compliance/modulesets/base.linux":
			_, _ = w.Write([]byte(`{"data":[{"id":7,"modset_name":"base.linux"}]}`))
		case path == "GET /tags":
			_, _ = w.Write([]byte(`{"data":[{"tag_id":"t-1","tag_name":"pci-dss"}]}`))
		case path == "GET /tags/t-1/nodes":
			if filter := r.URL.Query().Get("filter"); filter != "os_name:~linux" {
				t.Errorf("selection filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"a"}],"meta":{"total":1}}`))
		case path == "GET /nodes":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-2","nodename":"b"}],"meta":{"total":1}}`))
		case strings.HasPrefix(path, "POST /nodes/"), strings.HasPrefix(path, "DELETE /nodes/"):
			mutex.Lock()
			changes = append(changes, path)
			mutex.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"info":"done"}`))
		default:
			notFound(w, "missing")
		}
	})
	// The tag filter of list_nodes selects here: no tag is changed.
	result, ruleset := call("attach_node_ruleset", map[string]any{"ruleset": "os.linux.base", "tag": "pci-dss", "os_name": "linux"})
	if result.IsError || ruleset["changed"] != float64(1) || ruleset["filter_tag"].(map[string]any)["name"] != "pci-dss" {
		t.Fatalf("attach_node_ruleset = %+v", ruleset)
	}
	result, moduleset := call("detach_node_moduleset", map[string]any{"moduleset": "base.linux", "nodes": []string{"b"}})
	if result.IsError || moduleset["changed"] != float64(1) || moduleset["moduleset"].(map[string]any)["id"] != float64(7) {
		t.Fatalf("detach_node_moduleset = %+v", moduleset)
	}
	if strings.Join(changes, ",") != "POST /nodes/n-1/compliance/rulesets/12,DELETE /nodes/n-2/compliance/modulesets/7" {
		t.Fatalf("changes = %v", changes)
	}
}

// The ruleset and moduleset filters read the nodes they are attached to,
// with the other filters: to list, count and change them.
func TestNodeFiltersByRulesetAndModuleset(t *testing.T) {
	var mutex sync.Mutex
	var deleted []string
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		query := r.URL.Query()
		switch path := r.Method + " " + r.URL.EscapedPath(); {
		case path == "GET /compliance/rulesets/os.linux.base":
			_, _ = w.Write([]byte(`{"data":[{"id":12,"ruleset_name":"os.linux.base","ruleset_type":"explicit"}]}`))
		case path == "GET /compliance/rulesets/ctx.dev2":
			_, _ = w.Write([]byte(`{"data":[{"id":13,"ruleset_name":"ctx.dev2","ruleset_type":"contextual"}]}`))
		case path == "GET /compliance/modulesets/base.linux":
			_, _ = w.Write([]byte(`{"data":[{"id":7,"modset_name":"base.linux"}]}`))
		case path == "GET /compliance/rulesets/12/nodes":
			if filter := query.Get("filter"); filter != "node_env:eq:PRD" {
				t.Errorf("list filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"a"}],"meta":{"total":1}}`))
		case path == "GET /compliance/modulesets/7/nodes" && query.Get("stats") == "true":
			if query.Get("props") != "status" {
				t.Errorf("count props = %q", query.Get("props"))
			}
			_, _ = w.Write([]byte(`{"data":{"status":{"In service":2,"Commissioning":1}},"meta":{"total":3,"distinct":{"status":2},"other":{"status":0}}}`))
		case path == "GET /compliance/modulesets/7/nodes":
			if filter := query.Get("filter"); filter != "status:eq:In service" {
				t.Errorf("selection filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"a"},{"node_id":"n-2","nodename":"b"}],"meta":{"total":2}}`))
		case strings.HasPrefix(path, "DELETE /nodes/"):
			mutex.Lock()
			deleted = append(deleted, path)
			mutex.Unlock()
			_, _ = w.Write([]byte(`{"info":"moduleset detached"}`))
		default:
			notFound(w, "missing")
		}
	})
	result, list := call("list_nodes", map[string]any{"ruleset": "os.linux.base", "env": "PRD"})
	if result.IsError || list["total"] != float64(1) || list["ruleset"].(map[string]any)["id"] != float64(12) {
		t.Fatalf("list_nodes = %+v", list)
	}
	result, count := call("count_nodes", map[string]any{"moduleset": "base.linux", "group_by": "status"})
	if result.IsError || count["total"] != float64(3) || count["moduleset"].(map[string]any)["name"] != "base.linux" {
		t.Fatalf("count_nodes = %+v", count)
	}
	result, change := call("detach_node_moduleset", map[string]any{"moduleset": "base.linux", "filter_moduleset": "base.linux", "status": "In service"})
	if result.IsError || change["changed"] != float64(2) || change["filter_moduleset"].(map[string]any)["id"] != float64(7) || len(deleted) != 2 {
		t.Fatalf("detach_node_moduleset = %+v, deleted %v", change, deleted)
	}
	// A contextual ruleset is attached to no node, and two membership
	// filters are refused, before any change.
	for _, arguments := range []map[string]any{
		{"ruleset": "ctx.dev2"},
		{"ruleset": "os.linux.base", "moduleset": "base.linux"},
		{"tag": "pci-dss", "moduleset": "base.linux"},
	} {
		if result, _ := call("list_nodes", arguments); !result.IsError {
			t.Fatalf("list_nodes %v was accepted", arguments)
		}
	}
	if result, _ := call("detach_node_ruleset", map[string]any{"ruleset": "os.linux.base", "filter_ruleset": "ctx.dev2"}); !result.IsError || len(deleted) != 2 {
		t.Fatalf("a contextual ruleset filter was accepted, deleted %v", deleted)
	}
}

// The ruleset and moduleset filters of the services read those they are
// attached to: to list, count and change them.
func TestServiceFiltersByRulesetAndModuleset(t *testing.T) {
	var mutex sync.Mutex
	var deleted []string
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		query := r.URL.Query()
		switch path := r.Method + " " + r.URL.EscapedPath(); {
		case path == "GET /compliance/rulesets/os.linux.base":
			_, _ = w.Write([]byte(`{"data":[{"id":12,"ruleset_name":"os.linux.base","ruleset_type":"explicit"}]}`))
		case path == "GET /compliance/modulesets/base.linux":
			_, _ = w.Write([]byte(`{"data":[{"id":7,"modset_name":"base.linux"}]}`))
		case path == "GET /compliance/modulesets/7/services":
			if filter := query.Get("filter"); filter != "svc_env:eq:PRD" {
				t.Errorf("list filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","svcname":"web"}],"meta":{"total":1}}`))
		case path == "GET /compliance/rulesets/12/services" && query.Get("stats") == "true":
			_, _ = w.Write([]byte(`{"data":{"svc_app":{"WEB":2}},"meta":{"total":2,"distinct":{"svc_app":1},"other":{"svc_app":0}}}`))
		case path == "GET /compliance/rulesets/12/services":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","svcname":"web"},{"svc_id":"s-2","svcname":"db"}],"meta":{"total":2}}`))
		case strings.HasPrefix(path, "DELETE /services/"):
			mutex.Lock()
			deleted = append(deleted, path)
			mutex.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"info":"ruleset 12 detached"}`))
		default:
			notFound(w, "missing")
		}
	})
	result, list := call("list_services", map[string]any{"moduleset": "base.linux", "env": "PRD"})
	if result.IsError || list["total"] != float64(1) || list["moduleset"].(map[string]any)["id"] != float64(7) {
		t.Fatalf("list_services = %+v", list)
	}
	result, count := call("count_services", map[string]any{"ruleset": "os.linux.base", "group_by": "app"})
	if result.IsError || count["total"] != float64(2) || count["ruleset"].(map[string]any)["name"] != "os.linux.base" {
		t.Fatalf("count_services = %+v", count)
	}
	result, change := call("detach_service_ruleset", map[string]any{"ruleset": "os.linux.base", "filter_ruleset": "os.linux.base"})
	if result.IsError || change["changed"] != float64(2) || change["filter_ruleset"].(map[string]any)["id"] != float64(12) ||
		strings.Join(deleted, ",") == "" || !strings.Contains(strings.Join(deleted, ","), "/services/s-2/compliance/rulesets/12") {
		t.Fatalf("detach_service_ruleset = %+v, deleted %v", change, deleted)
	}
	// The node filter reads the instances: it goes with no member filter.
	if result, _ := call("list_services", map[string]any{"node": "n-1", "ruleset": "os.linux.base"}); !result.IsError {
		t.Fatal("node with ruleset was accepted")
	}
	if result, _ := call("detach_service_moduleset", map[string]any{"moduleset": "base.linux"}); !result.IsError || len(deleted) != 2 {
		t.Fatalf("a call selecting no service was accepted, deleted %v", deleted)
	}
}

func TestNodeActionQueueTools(t *testing.T) {
	var put map[string]any
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.EscapedPath() {
		case "GET /nodes":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"lab01"}],"meta":{"total":1}}`))
		case "PUT /actions":
			_ = json.NewDecoder(r.Body).Decode(&put)
			_, _ = w.Write([]byte(`{"data":[{"id":901,"status":"W","action_type":"feed"}],"meta":{"count":1}}`))
		case "GET /actions":
			_, _ = w.Write([]byte(`{"data":[{"id":901,"status":"W","command":"pushasset","node_id":"n-1","user_id":0,"nodes.nodename":"lab01"}],"meta":{"total":1}}`))
		default:
			notFound(w, "missing")
		}
	})
	result, queued := call("queue_node_action", map[string]any{"action": "pushasset", "nodes": []string{"lab01"}})
	if node := queued["nodes"].([]any)[0].(map[string]any); result.IsError || queued["changed"] != float64(1) || node["action_id"] != float64(901) || node["queued_for"] != "feed" || put["node_id"] != "n-1" {
		t.Fatalf("queue_node_action = %+v, put %v", queued, put)
	}
	result, list := call("list_node_actions", map[string]any{"status": "waiting"})
	actions, _ := list["actions"].([]any)
	if result.IsError || len(actions) != 1 || actions[0].(map[string]any)["nodename"] != "lab01" || actions[0].(map[string]any)["status"] != "waiting" {
		t.Fatalf("list_node_actions = %+v", list)
	}
	// The follow-up waits for the action queued; a wait on it, still
	// waiting, ends with the wait, and an unknown ID is told.
	if !strings.Contains(fmt.Sprint(queued["follow_up"]), "action_ids [901] and wait") {
		t.Fatalf("queue_node_action follow_up = %v", queued["follow_up"])
	}
	result, list = call("list_node_actions", map[string]any{"action_ids": []int{901, 902}, "wait": 1})
	if result.IsError || list["pending"] != float64(1) || list["waited_seconds"] != float64(1) || fmt.Sprint(list["not_found"]) != "[902]" || list["pending_not_feed"] != nil {
		t.Fatalf("list_node_actions with wait = %+v", list)
	}
	if result, _ := call("list_node_actions", map[string]any{"action_ids": []int{901}, "wait": 121}); !result.IsError {
		t.Fatal("list_node_actions accepted wait 121")
	}
	if result, _ := call("queue_node_action", map[string]any{"action": "reboot", "nodes": []string{"lab01"}}); !result.IsError {
		t.Fatal("queue_node_action accepted reboot")
	}
}

func TestNodeChangingActionTools(t *testing.T) {
	var put map[string]any
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.EscapedPath() {
		case "GET /nodes":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"lab01"}],"meta":{"total":1}}`))
		case "PUT /actions":
			_ = json.NewDecoder(r.Body).Decode(&put)
			_, _ = w.Write([]byte(`{"data":[{"id":903,"status":"W","action_type":"pull"}]}`))
		case "POST /actions":
			_, _ = w.Write([]byte(`{"id":903,"status":"C","command":"thaw --local"}`))
		default:
			notFound(w, "missing")
		}
	})
	result, queued := call("unfreeze_node", map[string]any{"nodes": []string{"lab01"}})
	node := queued["nodes"].([]any)[0].(map[string]any)
	if result.IsError || put["action"] != "thaw" || node["queued_for"] != "pull" || node["warning"] == nil {
		t.Fatalf("unfreeze_node = %+v, put %v", queued, put)
	}
	result, cancelled := call("cancel_node_action", map[string]any{"action_ids": []int{903}})
	if result.IsError || cancelled["cancelled"] != float64(1) || cancelled["actions"].([]any)[0].(map[string]any)["command"] != "thaw --local" {
		t.Fatalf("cancel_node_action = %+v", cancelled)
	}
}

func TestServiceTagTools(t *testing.T) {
	var mutex sync.Mutex
	var changed []string
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch path := r.Method + " " + r.URL.EscapedPath(); {
		case path == "GET /tags":
			_, _ = w.Write([]byte(`{"data":[{"tag_id":"t-1","tag_name":"tag-mcp"}]}`))
		case path == "GET /tags/t-1/services":
			if filter := r.URL.Query().Get("filter"); filter != "svc_app:eq:DATA" {
				t.Errorf("selection filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","svcname":"prd/svc/ora-hr"}],"meta":{"total":1}}`))
		case strings.HasPrefix(path, "POST /tags/t-1/services/"), strings.HasPrefix(path, "DELETE /tags/t-1/services/"):
			mutex.Lock()
			changed = append(changed, path)
			mutex.Unlock()
			_, _ = w.Write([]byte(`{"info":"tag detached"}`))
		default:
			notFound(w, "missing")
		}
	})
	result, change := call("detach_service_tag", map[string]any{"tag": "tag-mcp", "filter_tag": "tag-mcp", "app": "DATA"})
	if result.IsError || change["changed"] != float64(1) || change["services"].([]any)[0].(map[string]any)["svcname"] != "prd/svc/ora-hr" ||
		strings.Join(changed, ",") != "DELETE /tags/t-1/services/s-1" {
		t.Fatalf("detach_service_tag = %+v, changed %v", change, changed)
	}
	if result, _ := call("attach_service_tag", map[string]any{"tag": "tag-mcp"}); !result.IsError {
		t.Fatal("attach_service_tag accepted a call selecting no service")
	}
}

func TestServiceDeleteTools(t *testing.T) {
	var mutex sync.Mutex
	var deleted []string
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		filters := strings.Join(r.URL.Query()["filter"], " ")
		switch path := r.Method + " " + r.URL.EscapedPath(); {
		case path == "GET /services/web":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","svcname":"web","cluster_id":"c1","svc_app":"WEB"}]}`))
		case path == "GET /services/lone":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-3","svcname":"lone","cluster_id":"c1"}]}`))
		case path == "GET /services/dup":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","svcname":"dup","cluster_id":"c1"},{"svc_id":"s-2","svcname":"dup","cluster_id":"c2"}]}`))
		case path == "GET /nodes/n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"n1","cluster_id":"c1"}]}`))
		case path == "GET /services_instances" && strings.Contains(filters, "svc_id:eq:s-3"):
			_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0}}`))
		case path == "GET /services_instances" && strings.Contains(filters, "mon_updated:gte:-1h"):
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1"}],"meta":{"total":1}}`))
		case path == "GET /services_instances":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1"}],"meta":{"total":2}}`))
		case strings.HasPrefix(path, "DELETE /services/"):
			mutex.Lock()
			deleted = append(deleted, path)
			mutex.Unlock()
			_, _ = w.Write([]byte(`{"info":"service web in app WEB deleted"}`))
		default:
			notFound(w, "missing")
		}
	})
	// An instance reported within the hour: the service is deleted, with a
	// warning that it comes back.
	result, deletion := call("delete_service", map[string]any{"service": "web"})
	if result.IsError || deletion["deleted"] != true || deletion["instances"] != float64(2) || deletion["recently_reported"] != float64(1) || deletion["warning"] == nil ||
		strings.Join(deleted, ",") != "DELETE /services/s-1" {
		t.Fatalf("delete_service = %+v, deleted %v", deletion, deleted)
	}
	// No instance of the service on the node: nothing is deleted.
	result, _ = call("delete_service_instance", map[string]any{"service": "lone", "node": "n1"})
	if !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "has no instance on node n1") || len(deleted) != 1 {
		t.Fatalf("delete_service_instance without instance = %+v, deleted %v", result.Content[0], deleted)
	}
	// A name two clusters hold: refused with both IDs, nothing is deleted.
	result, _ = call("delete_service", map[string]any{"service": "dup"})
	if text := result.Content[0].(*mcp.TextContent).Text; !result.IsError || !strings.Contains(text, "s-1 (cluster c1)") || !strings.Contains(text, "s-2 (cluster c2)") || len(deleted) != 1 {
		t.Fatalf("delete_service of homonyms = %q, deleted %v", text, deleted)
	}
}

func TestServiceDowntimeTools(t *testing.T) {
	var mutex sync.Mutex
	var requests []string
	var body map[string]any
	justified := false
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mutex.Lock()
		defer mutex.Unlock()
		switch path := r.Method + " " + r.URL.EscapedPath(); path {
		case "GET /services/web":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","svcname":"web","cluster_id":"c-1"}]}`))
		case "GET /services/s-1/status_log":
			ack := `null`
			if justified {
				ack = `{"comment":"maintenance","account":false,"acked_by":"ops@example.com","acked_on":"2026-10-20 10:00:00"}`
			}
			_, _ = w.Write([]byte(`{"data":[{"begin":"2026-10-03 00:46:03","end":"2026-10-03 00:46:18","status":"down","ack":` + ack + `},
				{"begin":"2026-10-03 00:46:18","end":"2026-10-03 01:06:18","status":"up"}],
				"availability":{"from":"2026-10-03 00:46:03","to":"2026-10-20 10:28:07","rate":0.08,"available_s":1200,"excluded_s":15,"counted_s":1500000}}`))
		case "PUT /services/s-1/status_log/ack":
			requests = append(requests, path)
			_ = json.NewDecoder(r.Body).Decode(&body)
			justified = true
			_, _ = w.Write([]byte(`{"info":"period justified"}`))
		case "DELETE /services/s-1/status_log/ack":
			requests = append(requests, path+"?"+r.URL.RawQuery)
			justified = false
			_, _ = w.Write([]byte(`{"info":"justification removed"}`))
		default:
			notFound(w, "missing")
		}
	})
	result, change := call("justify_service_downtime", map[string]any{"service": "web", "begin": "2026-10-03 00:46:03", "comment": "maintenance", "counted": false})
	ack, _ := change["period"].(map[string]any)["ack"].(map[string]any)
	if result.IsError || change["outcome"] != "justified" || body["end"] != "2026-10-03 00:46:18" || body["account"] != false ||
		ack == nil || strings.Contains(fmt.Sprint(ack["acked_by"]), "@") {
		t.Fatalf("justify_service_downtime = %+v, body %v", change, body)
	}
	result, change = call("unjustify_service_downtime", map[string]any{"service": "web", "begin": "2026-10-03 00:46:03"})
	if result.IsError || change["outcome"] != "removed" || len(requests) != 2 || !strings.Contains(requests[1], "begin=2026-10-03+00%3A46%3A03") {
		t.Fatalf("unjustify_service_downtime = %+v, requests %v", change, requests)
	}
	result, change = call("unjustify_service_downtime", map[string]any{"service": "web", "begin": "2026-10-03 00:46:03"})
	if result.IsError || change["outcome"] != "not_justified" || len(requests) != 2 {
		t.Fatalf("unjustify_service_downtime of a period not justified = %+v, requests %v", change, requests)
	}
}

func TestSetServiceSLATool(t *testing.T) {
	var mutex sync.Mutex
	var bodies []map[string]any
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch path := r.Method + " " + r.URL.EscapedPath(); path {
		case "GET /nodes":
			if filter := r.URL.Query().Get("filter"); filter != "status:eq:Maintenance" {
				t.Errorf("node filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"a"}],"meta":{"total":1}}`))
		case "GET /services_instances":
			if filter := strings.Join(r.URL.Query()["filter"], " "); filter != "node_id:eq:n-1 mon_availstatus:eq:down services.svc_env:eq:PRD" {
				t.Errorf("instance filter = %q", filter)
			}
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","node_id":"n-1","services.svcname":"web"},{"svc_id":"s-2","node_id":"n-1","services.svcname":"db"},
				{"svc_id":"s-3","node_id":"n-1","services.svcname":"lab","services.svc_sla":99.95,"services.svc_availability":98.5}],"meta":{"total":3}}`))
		case "POST /services/s-2":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"text":"user is not responsible for service s-2"}`))
		case "POST /services/s-1", "POST /services/s-3":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mutex.Lock()
			bodies = append(bodies, body)
			mutex.Unlock()
			if path == "POST /services/s-3" {
				_, _ = w.Write([]byte(`{"info":"changed","data":[{"svc_sla":99.95,"svc_availability":98.5}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"info":"changed","data":[{"svc_sla":99.95}]}`))
		default:
			notFound(w, "missing")
		}
	})
	result, change := call("set_service_sla", map[string]any{"sla": 99.95, "on_nodes": map[string]any{"status": "Maintenance"}, "instance_availability": "down", "env": "PRD"})
	// Sorted db, lab, web: db refused, lab already at 99.95, set all the
	// same and told unchanged, web set.
	services, _ := change["services"].([]any)
	if result.IsError || change["changed"] != float64(1) || change["refused"] != float64(1) || change["unchanged"] != float64(1) ||
		len(bodies) != 2 || bodies[0]["svc_sla"] != "99.95" || bodies[1]["svc_sla"] != "99.95" || len(services) != 3 || change["on_nodes_selected"] != float64(1) {
		t.Fatalf("set_service_sla = %+v, bodies %v", change, bodies)
	}
	db, lab, web := services[0].(map[string]any), services[1].(map[string]any), services[2].(map[string]any)
	if db["outcome"] != "refused" || !strings.Contains(fmt.Sprint(db["reason"]), "not responsible") || lab["outcome"] != "unchanged" ||
		lab["availability_percent"] != 98.5 || web["outcome"] != "set" || web["sla"] != 99.95 {
		t.Fatalf("set_service_sla services = %v", services)
	}
	for _, arguments := range []map[string]any{
		{"sla": 120, "on_nodes": map[string]any{"status": "Maintenance"}},
		{"sla": 99, "remove": true, "on_nodes": map[string]any{"status": "Maintenance"}},
		{"sla": 99},
		{"sla": 99, "on_nodes": map[string]any{}},
	} {
		if result, _ := call("set_service_sla", arguments); !result.IsError || len(bodies) != 2 {
			t.Fatalf("set_service_sla %v was accepted, bodies %v", arguments, bodies)
		}
	}
}

func TestServiceAgentActionTools(t *testing.T) {
	var mutex sync.Mutex
	var bodies []map[string]any
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch path := r.Method + " " + r.URL.EscapedPath(); path {
		case "GET /nodes/dev2n1":
			_, _ = w.Write([]byte(`{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`))
		case "GET /services":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","svcname":"foo","svc_topology":"failover"},{"svc_id":"s-2","svcname":"bug1","svc_topology":"flex"}],"meta":{"total":2}}`))
		case "GET /services_instances":
			_, _ = w.Write([]byte(`{"data":[{"svc_id":"s-1","node_id":"n-1","nodes.nodename":"dev2n1","nodes.version":"v3.0.0-rc50-8","services.svcname":"foo","services.svc_topology":"failover"},
				{"svc_id":"s-2","node_id":"n-1","nodes.nodename":"dev2n1","nodes.version":"v3.0.0-rc50-8","services.svcname":"bug1","services.svc_topology":"flex"}],"meta":{"total":2}}`))
		case "GET /resources":
			_, _ = w.Write([]byte(`{"data":[{"rid":"fs#1","res_disable":"F"}],"meta":{"total":1}}`))
		case "PUT /actions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mutex.Lock()
			bodies = append(bodies, body)
			mutex.Unlock()
			if body["svc_id"] == "s-2" && body["action"] == "freeze" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"text":"user has no NodeExec privilege"}`))
				return
			}
			if body["svc_id"] == "s-2" && body["action"] == "compliance_fix" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"text":"user has no CompExec privilege"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":817,"status":"W","action_type":"feed","command":"freeze --local","node_id":"n-1"}]}`))
		default:
			notFound(w, "missing")
		}
	})
	// The instances of foo and bug1 on dev2n1, each run by dev2n1: bug1
	// refused by the collector, foo queued.
	result, queued := call("freeze_service", map[string]any{"services": []string{"foo", "bug1"}, "node": "dev2n1", "local": true})
	actions, _ := queued["actions"].([]any)
	if result.IsError || queued["queued"] != float64(1) || queued["refused"] != float64(1) || len(actions) != 2 ||
		actions[1].(map[string]any)["command"] != "freeze --local" || !strings.Contains(fmt.Sprint(actions[0].(map[string]any)["reason"]), "NodeExec") ||
		bodies[0]["node_id"] != "n-1" || queued["follow_up"] == nil {
		t.Fatalf("freeze_service = %+v, bodies %v", queued, bodies)
	}
	// A switch of foo and bug1: the flex bug1 refused before sending, the
	// failover foo queued on the whole service.
	result, queued = call("switch_service", map[string]any{"services": []string{"foo", "bug1"}})
	if result.IsError || queued["queued"] != float64(1) || queued["refused"] != float64(1) || len(bodies) != 3 || bodies[2]["node_id"] != nil || bodies[2]["svc_id"] != "s-1" {
		t.Fatalf("switch_service = %+v, bodies %v", queued, bodies)
	}
	if result, _ := call("takeover_service", map[string]any{"services": []string{"foo"}}); !result.IsError || len(bodies) != 3 {
		t.Fatalf("a takeover without destination was accepted, bodies %v", bodies)
	}
	// A node alone selects every service of the node, the agent showing
	// the call to confirm; a call selecting nothing is refused.
	result, queued = call("restart_service", map[string]any{"node": "dev2n1"})
	if result.IsError || queued["selected"] != float64(2) || len(bodies) != 5 || bodies[3]["node_id"] != "n-1" || bodies[3]["action"] != "restart" {
		t.Fatalf("restart_service = %+v, bodies %v", queued, bodies)
	}
	if result, _ := call("stop_service", map[string]any{}); !result.IsError || len(bodies) != 5 {
		t.Fatalf("a stop selecting nothing was accepted, bodies %v", bodies)
	}
	// A compliance check of the instances of dev2n1, each run by dev2n1.
	result, queued = call("check_service_compliance", map[string]any{"node": "dev2n1"})
	if result.IsError || queued["selected"] != float64(2) || len(bodies) != 7 || bodies[5]["action"] != "compliance_check" || bodies[5]["node_id"] != "n-1" ||
		!strings.Contains(fmt.Sprint(queued["follow_up"]), "list_compliance_runs") {
		t.Fatalf("check_service_compliance = %+v, bodies %v", queued, bodies)
	}
	if result, _ := call("fix_service_compliance", map[string]any{"services": []string{"foo"}}); !result.IsError || len(bodies) != 7 {
		t.Fatalf("a fix without node was accepted, bodies %v", bodies)
	}
	// A compliance fix of the instances of dev2n1: bug1 refused by the
	// collector for want of CompExec, foo queued.
	result, queued = call("fix_service_compliance", map[string]any{"node": "dev2n1"})
	actions, _ = queued["actions"].([]any)
	if result.IsError || queued["queued"] != float64(1) || queued["refused"] != float64(1) || len(actions) != 2 || len(bodies) != 9 ||
		bodies[7]["action"] != "compliance_fix" || bodies[7]["node_id"] != "n-1" || !strings.Contains(fmt.Sprint(actions[0].(map[string]any)["reason"]), "CompExec") {
		t.Fatalf("fix_service_compliance = %+v, bodies %v", queued, bodies)
	}
	// A restart of the resource fs#1 of the services of dev2n1, with --rid.
	result, queued = call("restart_service", map[string]any{"node": "dev2n1", "resources": []string{"fs#1"}})
	if result.IsError || queued["queued"] != float64(2) || len(bodies) != 11 || bodies[9]["rid"] != "fs#1" || bodies[10]["rid"] != "fs#1" || bodies[9]["node_id"] != "n-1" {
		t.Fatalf("restart_service with resources = %+v, bodies %v", queued, bodies)
	}
	if result, _ := call("stop_service", map[string]any{"node": "dev2n1", "resources": []string{"fs#9"}}); result.IsError || len(bodies) != 11 {
		t.Fatalf("a stop of a resource the instances lack was queued, bodies %v", bodies)
	}
}

func TestAckServiceActionTool(t *testing.T) {
	var body map[string]any
	call := newToolCallerWithActions(t, true, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.EscapedPath() {
		case "POST /services_actions/7":
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"info":"action 7 acknowledged"}`))
		case "GET /services_actions/7":
			_, _ = w.Write([]byte(`{"data":[{"id":7,"status":"err","ack":1,"acked_comment":"known issue"}]}`))
		default:
			notFound(w, "missing")
		}
	})
	result, ack := call("ack_service_action", map[string]any{"action_ids": []int{7}, "comment": "known issue"})
	if result.IsError || ack["acknowledged"] != float64(1) || body["ack"] != float64(1) || body["acked_comment"] != "known issue" ||
		ack["actions"].([]any)[0].(map[string]any)["action"].(map[string]any)["acknowledged"] != true {
		t.Fatalf("ack_service_action = %+v, body %v", ack, body)
	}
	// The collector takes an acknowledgement without comment.
	if result, _ := call("ack_service_action", map[string]any{"action_ids": []int{7}}); result.IsError || body["acked_comment"] != "" {
		t.Fatalf("ack_service_action without comment = %+v, body %v", result, body)
	}
	if result, _ := call("ack_service_action", map[string]any{"comment": "known issue"}); !result.IsError {
		t.Fatal("ack_service_action accepted a call selecting no action")
	}
}

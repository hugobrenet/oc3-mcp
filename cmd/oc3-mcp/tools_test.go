package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	collector := testutil.NewCollector(t, collectorHandler)
	api, err := newCollectorClient(config.Config{CollectorURL: collector.URL, CollectorCAFile: collector.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newHandler(api, testDelegator(t))
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
		default:
			notFound(w, "node missing not found")
		}
	})

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
		case "/compliance/modulesets/base.linux":
			_, _ = w.Write([]byte(`{"data":[{"id":3,"modset_name":"base.linux"}]}`))
		case "/compliance/modulesets/3/export":
			_, _ = w.Write([]byte(`{"filtersets":[],"rulesets":[],"modulesets":[{"id":3,"modset_name":"base.linux","modules":[{"modset_mod_name":"linux.files","autofix":"T"}],"modulesets":[],"rulesets":[],"publications":["Everybody"],"responsibles":["Infra Unix"]}]}`))
		case "/compliance/modulesets/3/usage":
			_, _ = w.Write([]byte(`{"data":{"modulesets":[{"id":1,"modset_name":"app.web"}]}}`))
		case "/compliance/modulesets/3/nodes", "/compliance/modulesets/3/services":
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

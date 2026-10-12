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
	"github.com/opensvc/oc3-mcp/internal/core"
	"github.com/opensvc/oc3-mcp/internal/testutil"
)

type toolCaller func(name string, arguments map[string]any) (*mcp.CallToolResult, map[string]any)

// newToolCaller serves the MCP over a fake collector answering with
// collectorHandler, and returns a function calling its tools.
func newToolCaller(t *testing.T, collectorHandler http.HandlerFunc) toolCaller {
	t.Helper()
	collector := testutil.NewCollector(t, collectorHandler)
	api, err := newCollectorClient(config.Config{
		CollectorURL:             collector.URL,
		CollectorCAFile:          collector.CAFile,
		CollectorCredentialsFile: testutil.WriteNetrc(t, "127.0.0.1", testutil.CollectorLogin, testutil.CollectorPassword, 0o600),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newHandler(core.New(api))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: server.Client(),
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

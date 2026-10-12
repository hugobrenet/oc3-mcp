package main

import (
	"context"
	"io"
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

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, err := newHandler(core.New(nil))
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestHealth(t *testing.T) {
	server := httptest.NewTLSServer(newTestHandler(t))
	defer server.Close()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), method, server.URL+"/health", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if method == http.MethodPost {
				if response.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("POST status=%d, want 405", response.StatusCode)
				}
				return
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			want := "{\"status\":\"ok\"}\n"
			if method == http.MethodHead {
				want = ""
			}
			if response.StatusCode != http.StatusOK || string(body) != want || response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected health response: status=%d headers=%v body=%q", response.StatusCode, response.Header, body)
			}
		})
	}
}

func TestMCPInitializeAndListTools(t *testing.T) {
	server := httptest.NewTLSServer(newTestHandler(t))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: server.Client(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if info := session.InitializeResult().ServerInfo; info.Name != serverName || info.Version != serverVersion {
		t.Fatalf("unexpected server info: %+v", info)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %s is not read-only", tool.Name)
		}
	}
	if strings.Join(names, ",") != "get_node,get_node_san_topology,get_node_stats,get_service,get_service_action_logs,get_service_status_history,list_alerts,list_clusters,list_frozen_instances,list_node_checks,list_node_disks,list_node_packages,list_nodes,list_package_differences,list_service_actions,list_service_checks,list_service_resources,list_services" {
		t.Fatalf("tools = %v", names)
	}
}

func TestNewCollectorClientReadsCredentialsWithoutContactingTheCollector(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("startup contacted the collector")
	}))
	cfg := config.Config{
		CollectorURL:             collector.URL,
		CollectorCAFile:          collector.CAFile,
		CollectorCredentialsFile: testutil.WriteNetrc(t, "127.0.0.1", testutil.CollectorLogin, testutil.CollectorPassword, 0o600),
	}
	if _, err := newCollectorClient(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.CollectorCredentialsFile = testutil.WriteNetrc(t, "127.0.0.1", testutil.CollectorLogin, testutil.CollectorPassword, 0o644)
	if _, err := newCollectorClient(cfg); err == nil {
		t.Fatal("a credentials file readable by others was accepted")
	}
	cfg.CollectorCredentialsFile = testutil.WriteNetrc(t, "collector.example.com", testutil.CollectorLogin, testutil.CollectorPassword, 0o600)
	if _, err := newCollectorClient(cfg); err == nil {
		t.Fatal("credentials for another machine were accepted")
	}
}

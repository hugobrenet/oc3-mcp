package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/auth"
	"github.com/opensvc/oc3-mcp/internal/client"
	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/oc3-mcp/internal/testutil"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

// bearerTransport adds the caller's access token, as the AI agent does.
type bearerTransport struct {
	token     string
	requestID string
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	if t.requestID != "" {
		clone.Header.Set(requestIDHeader, t.requestID)
	}
	return http.DefaultTransport.RoundTrip(clone)
}

func bearerClient(token string) *http.Client {
	return &http.Client{Transport: bearerTransport{token: token}}
}

// acceptingVerifier stands for the OpenID provider of the collector in the
// tests of the tools, whose tokens have no genuine signature: the
// verification of the socket has its own tests.
type acceptingVerifier struct{}

func (acceptingVerifier) Verify(_ context.Context, _, issuer, _ string) (mcpauth.VerifiedClaims, error) {
	return mcpauth.VerifiedClaims{Issuer: issuer}, nil
}

func testDelegator(t *testing.T) *auth.Delegator {
	t.Helper()
	d, err := auth.NewDelegator(testutil.TokenIssuer, "collector", acceptingVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newTestServer(t *testing.T, collector *client.Client) *httptest.Server {
	t.Helper()
	handler, err := newHandler(collector, testDelegator(t))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestHealth(t *testing.T) {
	server := newTestServer(t, nil)
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
	server := newTestServer(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: bearerClient(testutil.Token),
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
	if strings.Join(names, ",") != "count_nodes,count_services,get_node,get_node_san_topology,get_node_stats,get_service,get_service_action_logs,get_service_resource_history,get_service_status_history,list_alerts,list_clusters,list_collector_events,list_compliance_status,list_frozen_instances,list_node_checks,list_node_disks,list_node_packages,list_nodes,list_package_differences,list_service_actions,list_service_checks,list_service_resources,list_services" {
		t.Fatalf("tools = %v", names)
	}
	// The model learns that a service is up or down by its status, and that
	// the status of a node is a property its users set with their own values;
	// it finds the status filter in the schemas.
	byName := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		byName[tool.Name] = tool
	}
	for name, rule := range map[string]string{
		"list_nodes": "values each organization chooses", "count_nodes": "values each organization chooses", "get_node": "values each organization chooses",
		"list_services": "A service is up or down by its status", "count_services": "A service is up or down by its status",
	} {
		tool := byName[name]
		if !strings.Contains(tool.Description, rule) || strings.Contains(tool.Description, "not a live state") {
			t.Errorf("%s description = %q", name, tool.Description)
		}
		if strings.HasSuffix(name, "nodes") || name == "get_node" {
			if strings.Contains(tool.Description, "up or down by its status") {
				t.Errorf("%s tells that a node is up or down by its status", name)
			}
		}
		if name == "get_node" {
			continue
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(schema), `"status":{`) || strings.Contains(string(schema), "asset_status") || strings.Contains(string(schema), "overall_status") {
			t.Errorf("%s schema lacks the status filter: %s", name, schema)
		}
		if strings.HasSuffix(name, "nodes") && !strings.Contains(string(schema), "Not Ready") {
			t.Errorf("%s status filter does not present free values: %s", name, schema)
		}
	}
}

func TestMCPRequiresAnAccessToken(t *testing.T) {
	server := newTestServer(t, nil)
	expired := testutil.NewToken(testutil.TokenIssuer, testutil.TokenSubject, time.Now().Add(-time.Minute))
	for name, httpClient := range map[string]*http.Client{
		"no token":      server.Client(),
		"expired token": bearerClient(expired),
	} {
		for _, path := range []string{"/mcp", "/mcp/auth/whoami"} {
			method := http.MethodPost
			if path == "/mcp/auth/whoami" {
				method = http.MethodGet
			}
			request, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(""))
			if err != nil {
				t.Fatal(err)
			}
			response, err := httpClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("%s %s: status=%d", name, path, response.StatusCode)
			}
		}
	}
}

func TestWhoAmIProvesTheTokenWithTheCollector(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		identity string
		want     int
		audit    string
	}{
		{"confirmed", http.StatusOK, `{"auth_source":"bearer","issuer":"` + testutil.TokenIssuer + `","subject":"` + testutil.TokenSubject + `","email":"user@example.com"}`, http.StatusOK, "outcome=ok status=200"},
		{"session", http.StatusOK, `{"auth_source":"session","issuer":"` + testutil.TokenIssuer + `","subject":"` + testutil.TokenSubject + `"}`, http.StatusUnauthorized, "outcome=identity_mismatch status=401"},
		{"other subject", http.StatusOK, `{"auth_source":"bearer","issuer":"` + testutil.TokenIssuer + `","subject":"other"}`, http.StatusUnauthorized, "outcome=identity_mismatch status=401"},
		{"other issuer", http.StatusOK, `{"auth_source":"bearer","issuer":"https://other.example.com/","subject":"` + testutil.TokenSubject + `"}`, http.StatusUnauthorized, "outcome=identity_mismatch status=401"},
		{"forbidden", http.StatusForbidden, `{"text":"denied"}`, http.StatusForbidden, "outcome=collector_refused status=403"},
		{"collector failure", http.StatusInternalServerError, `{"text":"boom"}`, http.StatusBadGateway, "outcome=collector_failed status=502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var audit syncBuffer
			defer func(logger *slog.Logger) { auditLogger = logger }(auditLogger)
			auditLogger = slog.New(slog.NewTextHandler(&audit, nil))
			collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/auth/whoami" {
					t.Errorf("path = %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.identity))
			}))
			api, err := newCollectorClient(config.Config{CollectorURL: collector.URL, CollectorCAFile: collector.CAFile})
			if err != nil {
				t.Fatal(err)
			}
			server := newTestServer(t, api)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/mcp/auth/whoami", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := bearerClient(testutil.Token).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			if response.StatusCode != tc.want || strings.Contains(string(body), "boom") || strings.Contains(string(body), "denied") {
				t.Fatalf("status=%d body=%s", response.StatusCode, body)
			}
			line := strings.TrimSpace(audit.String())
			if !strings.HasSuffix(line, `request_id=""`) {
				t.Fatalf("whoami without a request ID logged one:\n%s", line)
			}
			if strings.Count(line, "\n") != 0 || !strings.Contains(line, `msg="mcp whoami" issuer=`+testutil.TokenIssuer+` subject=`+testutil.TokenSubject+` `+tc.audit) || strings.Contains(line, testutil.Token) {
				t.Fatalf("unexpected whoami audit line:\n%s", line)
			}
			if tc.status != http.StatusOK && !strings.Contains(line, "collector_status="+strconv.Itoa(tc.status)) {
				t.Fatalf("whoami audit line misses the collector status:\n%s", line)
			}
			if tc.want != http.StatusOK {
				return
			}
			var identity struct {
				Issuer    string    `json:"issuer"`
				Subject   string    `json:"subject"`
				ExpiresAt time.Time `json:"expires_at"`
			}
			if err := json.Unmarshal(body, &identity); err != nil || identity.Issuer != testutil.TokenIssuer || identity.Subject != testutil.TokenSubject || identity.ExpiresAt.IsZero() {
				t.Fatalf("identity = %s", body)
			}
		})
	}
}

func TestNewCollectorClientDoesNotContactTheCollector(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("startup contacted the collector")
	}))
	if _, err := newCollectorClient(config.Config{CollectorURL: collector.URL, CollectorCAFile: collector.CAFile}); err != nil {
		t.Fatal(err)
	}
	if _, err := newCollectorClient(config.Config{CollectorURL: collector.URL, CollectorCAFile: "/nonexistent/ca.pem"}); err == nil {
		t.Fatal("a missing CA file was accepted")
	}
}

func TestListenDelegated(t *testing.T) {
	// Unix socket paths are short: keep the directory under the system one.
	dir, err := os.MkdirTemp("", "oc3-mcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "delegated.sock")
	listener, err := listenDelegated(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode = %v, %v", info.Mode(), err)
	}
	if _, err := listenDelegated(path); err == nil {
		t.Fatal("a live socket was replaced")
	}
	// A stopped process leaves its socket file behind.
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	listener.Close()
	listener, err = listenDelegated(path)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	listener.Close()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := listenDelegated(file); err == nil {
		t.Fatal("a regular file was replaced")
	}
}

func TestMCPRefusesCrossOriginBrowserRequests(t *testing.T) {
	server := newTestServer(t, nil)
	post := func(client *http.Client, fetchSite string) int {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if fetchSite != "" {
			request.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status := post(bearerClient(testutil.Token), "cross-site"); status != http.StatusForbidden {
		t.Fatalf("cross-site request status = %d, want 403", status)
	}
	if status := post(bearerClient(testutil.Token), "same-origin"); status != http.StatusOK {
		t.Fatalf("same-origin request status = %d, want 200", status)
	}
	// The token is checked first: a cross-site request without one gets 401.
	if status := post(server.Client(), "cross-site"); status != http.StatusUnauthorized {
		t.Fatalf("cross-site request without token status = %d, want 401", status)
	}
}

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/oc3-mcp/internal/testutil"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
	"github.com/opensvc/osvc-auth-mcp/mcpauthtest"
)

const (
	testResourceURL       = "https://mcp.example.test:8444/mcp"
	testCollectorAudience = "collector-client-id"
)

// httpsTest serves the HTTPS handler with a fake OAuth provider, which also
// answers the token exchange, and a fake collector accepting the exchanged
// token only.
type httpsTest struct {
	provider  *mcpauthtest.OAuthProvider
	server    *httptest.Server
	incoming  string
	exchanged string
	exchanges atomic.Int32
	collector atomic.Int32
}

func newHTTPSTest(t *testing.T, exchange bool) *httpsTest {
	t.Helper()
	h := &httpsTest{provider: mcpauthtest.NewOAuthProvider(t)}
	h.provider.Metadata["token_endpoint"] = h.provider.Server.URL + "/token"
	h.incoming = h.provider.Token(t, testResourceURL, "alice", func(claims jwt.MapClaims) {
		claims["azp"] = "external-client"
		claims["iat"] = time.Now().Unix()
	})
	h.exchanged = h.provider.Token(t, testCollectorAudience, "alice-collector", func(claims jwt.MapClaims) {
		claims["iat"] = time.Now().Unix()
	})
	h.provider.TokenHandler = func(w http.ResponseWriter, r *http.Request) {
		h.exchanges.Add(1)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:token-exchange" ||
			r.PostForm.Get("subject_token") != h.incoming || r.PostForm.Get("audience") != testCollectorAudience {
			t.Errorf("unexpected token exchange request: %v", r.PostForm)
		}
		if client, _, ok := r.BasicAuth(); !ok || client != "test-exchange" {
			t.Errorf("exchange client not authenticated: %q", client)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": h.exchanged, "token_type": "Bearer", "expires_in": 300,
			"issued_token_type": "urn:ietf:params:oauth:token-type:access_token",
		})
	}
	collector := testutil.NewCollectorWithToken(t, h.exchanged, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.collector.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0}}`))
	}))
	cfg := config.Config{
		OAuth:        mcpauth.OAuthConfig{ResourceURL: testResourceURL, Issuer: h.provider.Issuer, CAFile: h.provider.CAFile},
		CollectorURL: collector.URL, CollectorCAFile: collector.CAFile,
	}
	if exchange {
		profiles, err := mcpauth.LoadExchangeProfiles(mcpauthtest.WriteExchangeProfiles(t, h.provider, "client_secret_basic"))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Exchange, cfg.ExchangeProfile, cfg.CollectorAudience = profiles, "test-sso", testCollectorAudience
	}
	api, err := newCollectorClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newMCPHandler(cfg, api)
	if err != nil {
		t.Fatal(err)
	}
	h.server = httptest.NewServer(handler)
	t.Cleanup(h.server.Close)
	return h
}

func (h *httpsTest) call(t *testing.T, token string, name string) *mcp.CallToolResult {
	t.Helper()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "external-client", Version: "test"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: h.server.URL + "/mcp", HTTPClient: bearerClient(token), DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHTTPSPublishesItsOAuthMetadataAndChallenge(t *testing.T) {
	h := newHTTPSTest(t, true)
	response, err := http.Get(h.server.URL + "/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Resource             string   `json:"resource"`
		ResourceName         string   `json:"resource_name"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	err = json.NewDecoder(response.Body).Decode(&metadata)
	response.Body.Close()
	if err != nil || metadata.Resource != testResourceURL || metadata.ResourceName != "OpenSVC Collector MCP" || len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != h.provider.Issuer {
		t.Fatalf("metadata = %+v, %v", metadata, err)
	}
	response, err = http.Post(h.server.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || !strings.Contains(response.Header.Get("WWW-Authenticate"), `resource_metadata="https://mcp.example.test:8444/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("status=%d challenge=%q", response.StatusCode, response.Header.Get("WWW-Authenticate"))
	}
	if response, err := http.Get(h.server.URL + "/health"); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("health: %v %v", response, err)
	}
}

func TestHTTPSToolCallsReachTheCollectorWithTheExchangedToken(t *testing.T) {
	var audit syncBuffer
	defer func(logger *slog.Logger) { auditLogger = logger }(auditLogger)
	auditLogger = slog.New(slog.NewTextHandler(&audit, nil))
	h := newHTTPSTest(t, true)
	if result := h.call(t, h.incoming, "list_clusters"); result.IsError {
		t.Fatalf("list_clusters failed: %+v", result.Content[0])
	}
	if h.exchanges.Load() != 1 || h.collector.Load() == 0 {
		t.Fatalf("exchanges=%d collector requests=%d", h.exchanges.Load(), h.collector.Load())
	}
	line := strings.TrimSpace(audit.String())
	want := `tool=list_clusters issuer=` + h.provider.Issuer + ` subject=alice client_id=external-client exchange=ok collector_subject=alice-collector outcome=ok`
	if !strings.Contains(line, want) || !strings.HasSuffix(line, `request_id=""`) || strings.Contains(line, h.incoming) || strings.Contains(line, h.exchanged) {
		t.Fatalf("unexpected audit line:\n%s", line)
	}
}

func TestHTTPSRefusesATokenIntendedForTheCollector(t *testing.T) {
	h := newHTTPSTest(t, true)
	request, err := http.NewRequest(http.MethodPost, h.server.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+h.exchanged)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || strings.Contains(string(body), h.exchanged) || h.exchanges.Load() != 0 {
		t.Fatalf("collector token accepted by the MCP: %d %s", response.StatusCode, body)
	}
}

func TestHTTPSToolCallsFailWhenTheExchangeIsRefused(t *testing.T) {
	h := newHTTPSTest(t, true)
	h.provider.TokenHandler = func(w http.ResponseWriter, r *http.Request) {
		h.exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"secret upstream detail"}`))
	}
	result := h.call(t, h.incoming, "list_clusters")
	text, _ := result.Content[0].(*mcp.TextContent)
	if !result.IsError || text == nil || text.Text != "token exchange refused by SSO (invalid_grant)" || h.collector.Load() != 0 {
		t.Fatalf("result=%+v collector requests=%d", result, h.collector.Load())
	}
}

func TestHTTPSBlocksToolCallsWithoutTokenExchange(t *testing.T) {
	h := newHTTPSTest(t, false)
	result := h.call(t, h.incoming, "list_clusters")
	text, _ := result.Content[0].(*mcp.TextContent)
	if !result.IsError || text == nil || !strings.Contains(text.Text, "token exchange configuration") || h.collector.Load() != 0 || h.exchanges.Load() != 0 {
		t.Fatalf("result=%+v", result)
	}
}

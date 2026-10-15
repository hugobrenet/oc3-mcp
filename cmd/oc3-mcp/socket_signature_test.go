package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/osvc-auth-mcp/mcpauthtest"
)

// TestSocketVerifiesTokensWithTheProviderOfTheCollector: a token the
// configured provider of the collector did not sign for the collector never
// reaches the collector.
func TestSocketVerifiesTokensWithTheProviderOfTheCollector(t *testing.T) {
	provider := mcpauthtest.NewOAuthProvider(t)
	var whoamiCalls atomic.Int32
	collector := mcpauthtest.NewTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/whoami":
			whoamiCalls.Add(1)
			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			var claims jwt.RegisteredClaims
			if _, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return &provider.Key.PublicKey, nil }, jwt.WithAudience("collector")); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"auth_source": "bearer", "issuer": claims.Issuer, "subject": claims.Subject})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	// A socket-only configuration: the provider is trusted with the delegated
	// CA bundle, the OAuth one of the HTTPS listener being unavailable.
	cfg := config.Config{CollectorURL: collector.Server.URL + "/api", CollectorCAFile: collector.CAFile, DelegatedCAFile: provider.CAFile,
		DelegatedIssuer: provider.Issuer, DelegatedAudience: "collector"}
	api, err := newCollectorClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	delegator, err := newDelegator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newHandler(api, delegator)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	whoami := func(token string) int {
		t.Helper()
		response, err := bearerClient(token).Get(server.URL + "/mcp/auth/whoami")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status := whoami(provider.Token(t, "collector", "alice", nil)); status != http.StatusOK || whoamiCalls.Load() != 1 {
		t.Fatalf("valid token: %d, %d collector whoami", status, whoamiCalls.Load())
	}
	forged := mcpauthtest.NewOAuthProvider(t)
	for _, token := range []string{
		forged.Token(t, "collector", "alice", func(c jwt.MapClaims) { c["iss"] = provider.Issuer }),
		forged.Token(t, "collector", "mallory", nil),
		provider.Token(t, "another-client", "alice", nil),
	} {
		if status := whoami(token); status != http.StatusUnauthorized {
			t.Fatalf("forged token: %d", status)
		}
	}
	if whoamiCalls.Load() != 1 {
		t.Fatalf("refused tokens reached the collector: %d whoami", whoamiCalls.Load())
	}
}

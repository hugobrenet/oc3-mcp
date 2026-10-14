package testutil

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	TokenIssuer  = "https://sso.example.com/application/o/collector/"
	TokenSubject = "user-subject"
)

// Token is an access token of the shape the MCP accepts, valid for a day. Its
// signature is not genuine: the fake collector compares the whole token.
var Token = NewToken(TokenIssuer, TokenSubject, time.Now().Add(24*time.Hour))

// NewToken returns an access token with the given claims and a fake
// signature.
func NewToken(issuer string, subject string, expires time.Time) string {
	segment := func(value any) string {
		data, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	return segment(map[string]string{"alg": "RS256", "kid": "example-key", "typ": "JWT"}) + "." +
		segment(map[string]any{"iss": issuer, "sub": subject, "aud": "collector", "exp": expires.Unix()}) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("not-a-signature"))
}

type Collector struct {
	Server *httptest.Server
	CAFile string
	// URL is the base URL of the fake collector API.
	URL string
}

// NewCollector serves handler over TLS under /api, trusted through a
// temporary CA file. Requests without Token get 401.
func NewCollector(t testing.TB, handler http.Handler) Collector {
	t.Helper()
	return NewCollectorWithToken(t, Token, handler)
}

// NewCollectorWithToken serves handler like NewCollector, for requests
// carrying token, such as the token exchanged for the collector.
func NewCollectorWithToken(t testing.TB, token string, handler http.Handler) Collector {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"text":"strategies/union: [invalid credentials]"}`))
			return
		}
		handler.ServeHTTP(w, r)
	})))
	server := NewTLSServer(t, mux)
	return Collector{Server: server.Server, CAFile: server.CAFile, URL: server.Server.URL + "/api"}
}

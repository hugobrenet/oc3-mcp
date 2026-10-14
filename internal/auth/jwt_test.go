package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
	"github.com/opensvc/osvc-auth-mcp/mcpauthtest"
)

func token(header map[string]any, claims map[string]any) string {
	segment := func(value any) string {
		data, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	return segment(header) + "." + segment(claims) + ".c2lnbmF0dXJl"
}

func validHeader() map[string]any {
	return map[string]any{"alg": "RS256", "kid": "key-1", "typ": "JWT"}
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{"iss": "https://sso.example.com/o/collector/", "sub": "user-1", "aud": "collector", "exp": now.Add(time.Hour).Unix()}
}

func TestCheckPredictsTheIdentityOfAnAccessToken(t *testing.T) {
	now := time.Now()
	for name, claims := range map[string]map[string]any{
		"audience string": validClaims(now),
		"audience list":   func() map[string]any { c := validClaims(now); c["aud"] = []string{"collector", "other"}; return c }(),
	} {
		delegation, err := Check(token(validHeader(), claims))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if delegation.Issuer != "https://sso.example.com/o/collector/" || delegation.Subject != "user-1" || !delegation.ExpiresAt.Equal(time.Unix(now.Add(time.Hour).Unix(), 0)) {
			t.Fatalf("%s: delegation = %+v", name, delegation)
		}
	}
}

func TestCheckRejectsInvalidTokens(t *testing.T) {
	now := time.Now()
	with := func(key string, value any) string {
		claims := validClaims(now)
		if value == nil {
			delete(claims, key)
		} else {
			claims[key] = value
		}
		return token(validHeader(), claims)
	}
	header := func(key string, value any) string {
		h := validHeader()
		if value == nil {
			delete(h, key)
		} else {
			h[key] = value
		}
		return token(h, validClaims(now))
	}
	for name, raw := range map[string]string{
		"empty":           "",
		"two segments":    "a.b",
		"not base64":      "!!!.!!!.sig",
		"symmetric":       header("alg", "HS256"),
		"none":            header("alg", "none"),
		"no kid":          header("kid", nil),
		"no issuer":       with("iss", nil),
		"no subject":      with("sub", nil),
		"spaced subject":  with("sub", " user-1"),
		"control subject": with("sub", "user\n1"),
		"no audience":     with("aud", nil),
		"empty audience":  with("aud", []string{}),
		"no expiry":       with("exp", nil),
		"expired":         with("exp", now.Add(-time.Second).Unix()),
		"string expiry":   with("exp", "soon"),
		"not yet valid":   with("nbf", now.Add(time.Hour).Unix()),
		"oversized":       strings.Repeat("a", mcpauth.MaxTokenBytes+1),
	} {
		if _, err := Check(raw); err == nil {
			t.Fatalf("%s token accepted", name)
		}
	}
}

func TestMiddlewareCarriesTheCallerUntilTheTokenExpires(t *testing.T) {
	now := time.Now()
	raw := token(validHeader(), validClaims(now))
	var seen Delegation
	var forwarded, header string
	handler := acceptingDelegator(t).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		seen, forwarded, ok = DelegationFromContext(r.Context())
		if !ok {
			t.Error("no caller in the request context")
		}
		if deadline, ok := r.Context().Deadline(); !ok || deadline.After(seen.ExpiresAt) {
			t.Errorf("deadline = %v, want the token expiry %v", deadline, seen.ExpiresAt)
		}
		header = r.Header.Get("Authorization")
	}))
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer "+raw)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || seen.Subject != "user-1" || forwarded != raw || header != "" {
		t.Fatalf("status=%d caller=%+v forwarded=%v header=%q", recorder.Code, seen, forwarded == raw, header)
	}
}

func TestMiddlewareRefusesRequestsWithoutOneValidBearerToken(t *testing.T) {
	raw := token(validHeader(), validClaims(time.Now()))
	handler := acceptingDelegator(t).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an unauthenticated request reached the MCP")
	}))
	for name, set := range map[string]func(*http.Request){
		"no header": func(r *http.Request) {},
		"basic":     func(r *http.Request) { r.SetBasicAuth("user", "password") },
		"two headers": func(r *http.Request) {
			r.Header.Add("Authorization", "Bearer "+raw)
			r.Header.Add("Authorization", "Bearer "+raw)
		},
		"invalid token": func(r *http.Request) { r.Header.Set("Authorization", "Bearer abc.def.ghi") },
		"token in query": func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+raw)
			r.URL.RawQuery = "access_token=" + raw
		},
	} {
		request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		set(request)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") != "Bearer" || strings.Contains(recorder.Body.String(), raw) {
			t.Fatalf("%s: status=%d headers=%v", name, recorder.Code, recorder.Header())
		}
	}
}

func TestDelegationFromContextIgnoresOtherContexts(t *testing.T) {
	if _, _, ok := DelegationFromContext(t.Context()); ok {
		t.Fatal("a caller was found in a context without one")
	}
}

type acceptingVerifier struct{}

func (acceptingVerifier) Verify(_ context.Context, _, issuer, _ string) (mcpauth.VerifiedClaims, error) {
	return mcpauth.VerifiedClaims{Issuer: issuer}, nil
}

// acceptingDelegator accepts the tokens of validClaims, whose signature is not
// genuine, to test the request handling apart from the verification.
func acceptingDelegator(t *testing.T) *Delegator {
	t.Helper()
	d, err := NewDelegator(validClaims(time.Now())["iss"].(string), "collector", acceptingVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestDelegatorVerifiesBeforeTheCollector: a token the OpenID provider of the
// collector did not sign for the collector never reaches the handler, which
// relays to the collector.
func TestDelegatorVerifiesBeforeTheCollector(t *testing.T) {
	p := mcpauthtest.NewOAuthProvider(t)
	verifier, err := mcpauth.NewJWTVerifier(p.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDelegator(p.Issuer, "collector", verifier)
	if err != nil {
		t.Fatal(err)
	}
	serve := func(raw string) (int, bool) {
		called := false
		handler := d.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}))
		request := httptest.NewRequest(http.MethodGet, "/mcp/auth/whoami", nil)
		request.Header.Set("Authorization", "Bearer "+raw)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code, called
	}
	if code, called := serve(p.Token(t, "collector", "alice", nil)); code != http.StatusNoContent || !called {
		t.Fatalf("valid token refused: %d", code)
	}
	other := mcpauthtest.NewOAuthProvider(t)
	for name, raw := range map[string]string{
		"signed by another key": other.Token(t, "collector", "alice", func(c jwt.MapClaims) { c["iss"] = p.Issuer }),
		"another issuer":        other.Token(t, "collector", "alice", nil),
		"another audience":      p.Token(t, "another-client", "alice", nil),
		"audience of the MCP":   p.Token(t, "https://mcp.example.test/mcp", "alice", nil),
		"expired":               p.Token(t, "collector", "alice", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Second).Unix() }),
	} {
		t.Run(name, func(t *testing.T) {
			if code, called := serve(raw); code != http.StatusUnauthorized || called {
				t.Fatalf("refused token reached the collector: %d %v", code, called)
			}
		})
	}
	if other.KeyCalls.Load() != 0 {
		t.Fatal("the keys of an untrusted issuer were fetched")
	}
	if _, err := NewDelegator("", "collector", verifier); err == nil {
		t.Fatal("delegator without issuer")
	}
	t.Run("provider keys unavailable", func(t *testing.T) {
		p := mcpauthtest.NewOAuthProvider(t)
		verifier, err := mcpauth.NewJWTVerifier(p.CAFile)
		if err != nil {
			t.Fatal(err)
		}
		d, err = NewDelegator(p.Issuer, "collector", verifier)
		if err != nil {
			t.Fatal(err)
		}
		p.Status = http.StatusServiceUnavailable
		if code, called := serve(p.Token(t, "collector", "alice", nil)); code != http.StatusServiceUnavailable || called {
			t.Fatalf("unverifiable token admitted: %d", code)
		}
	})
}

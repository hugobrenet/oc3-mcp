// Package auth takes the delegated access token of each MCP caller. On the
// local Unix socket, OpenID Connect access tokens are checked for shape only
// and verified by the collector, which receives them unchanged.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
)

const (
	maxTokenBytes   = 32 << 10
	maxMCPBodyBytes = 1 << 20
)

var errUnauthorized = errors.New("invalid OpenID Connect access token")

// Delegation describes UNVERIFIED claims of a delegated access token. They
// only predict the identity that the collector must confirm; they never
// establish an identity by themselves.
type Delegation struct {
	Issuer    string
	Subject   string
	ExpiresAt time.Time
}

// Check predicts the collector identity of an OpenID Connect access token.
func Check(raw string) (Delegation, error) {
	if raw == "" || len(raw) > maxTokenBytes {
		return Delegation{}, errUnauthorized
	}
	var parsed jwt.RegisteredClaims
	token, _, err := jwt.NewParser().ParseUnverified(raw, &parsed)
	// Keeping the token shape does not prove the signature is genuine.
	if err != nil || token == nil {
		return Delegation{}, errUnauthorized
	}
	if err := jwt.NewValidator(jwt.WithExpirationRequired()).Validate(&parsed); err != nil {
		return Delegation{}, errUnauthorized
	}
	if !validClaimText(parsed.Subject) || !validClaimText(parsed.Issuer) || parsed.ExpiresAt == nil {
		return Delegation{}, errUnauthorized
	}
	if len(parsed.Audience) == 0 || !openIDSigningMethod(token.Method.Alg()) {
		return Delegation{}, errUnauthorized
	}
	kid, ok := token.Header["kid"].(string)
	if !ok || !validClaimText(kid) {
		return Delegation{}, errUnauthorized
	}
	for _, audience := range parsed.Audience {
		if !validClaimText(audience) {
			return Delegation{}, errUnauthorized
		}
	}
	return Delegation{Issuer: parsed.Issuer, Subject: parsed.Subject, ExpiresAt: parsed.ExpiresAt.Time}, nil
}

func openIDSigningMethod(algorithm string) bool {
	switch algorithm {
	case "RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512":
		return true
	default:
		return false
	}
}

func validClaimText(s string) bool {
	return len(s) > 0 && len(s) <= 256 && strings.TrimSpace(s) == s && utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) })
}

type delegationContextKey struct{}
type delegatedCredential struct {
	delegation Delegation
	token      string
}

// NewContext checks raw and returns ctx carrying its delegation, with a
// deadline at the token expiry.
func NewContext(ctx context.Context, raw string) (context.Context, context.CancelFunc, error) {
	delegation, err := Check(raw)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithDeadline(ctx, delegation.ExpiresAt)
	return context.WithValue(ctx, delegationContextKey{}, delegatedCredential{delegation: delegation, token: raw}), cancel, nil
}

// DelegationFromContext never returns credentials for an expired request.
// Only NewContext populates this request-scoped context. The claims remain
// unverified: the collector alone authenticates the bearer.
func DelegationFromContext(ctx context.Context) (Delegation, string, bool) {
	c, ok := ctx.Value(delegationContextKey{}).(delegatedCredential)
	if !ok || ctx.Err() != nil || !time.Now().Before(c.delegation.ExpiresAt) {
		return Delegation{}, "", false
	}
	return c.delegation, c.token, true
}

// Middleware requires one Bearer token. It is mounted on the local Unix
// socket, the only listener of the MCP.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		var raw string
		if len(values) == 1 {
			parts := strings.Fields(values[0])
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				raw = parts[1]
			}
		}
		ctx, cancel, err := NewContext(r.Context(), raw)
		// Tokens in query strings are never an alternative authentication path.
		if err != nil || len(values) != 1 || r.URL.Query().Has("access_token") {
			if cancel != nil {
				cancel()
			}
			WriteProblem(w, http.StatusUnauthorized, "An OpenID Connect access token is required.")
			return
		}
		defer cancel()
		request := r.Clone(ctx)
		request.Header.Del("Authorization")
		request.Body = http.MaxBytesReader(w, request.Body, maxMCPBodyBytes)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, request)
	})
}

// WriteProblem answers an RFC 9457 problem without upstream details.
func WriteProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail,omitempty"`
	}{"about:blank", http.StatusText(status), status, detail})
}

// CollectorToken returns the token the collector receives for the request:
// the token exchanged for the collector audience on the HTTPS listener, or the
// delegated token forwarded unchanged on the Unix socket. The MCP access token
// of an external client is never returned.
func CollectorToken(ctx context.Context) (string, bool) {
	if token, ok := ExchangedFromContext(ctx); ok {
		return token, true
	}
	_, token, ok := DelegationFromContext(ctx)
	return token, ok
}

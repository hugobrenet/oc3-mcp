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

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

const (
	maxMCPBodyBytes = 1 << 20
)

var errUnauthorized = errors.New("invalid OpenID Connect access token")

// TokenVerifier verifies an access token for an issuer and an audience, as
// mcpauth.JWTVerifier does.
type TokenVerifier interface {
	Verify(ctx context.Context, raw, issuer, audience string) (mcpauth.VerifiedClaims, error)
}

// Delegation describes the claims of a delegated access token. On the socket,
// the middleware of a Delegator verified its signature, issuer, audience and
// lifetime; the collector still decides the access of that identity.
type Delegation struct {
	Issuer    string
	Subject   string
	ExpiresAt time.Time
}

// Check predicts the collector identity of an OpenID Connect access token.
func Check(raw string) (Delegation, error) {
	if raw == "" || len(raw) > mcpauth.MaxTokenBytes {
		return Delegation{}, errUnauthorized
	}
	var parsed jwt.RegisteredClaims
	token, _, err := jwt.NewParser().ParseUnverified(raw, &parsed)
	// Keeping the token shape does not prove the signature is genuine.
	if err != nil || token == nil {
		return Delegation{}, errUnauthorized
	}
	if err := mcpauth.ValidateLifetime(&parsed); err != nil {
		return Delegation{}, errUnauthorized
	}
	if !mcpauth.ValidClaimText(parsed.Subject) || !mcpauth.ValidClaimText(parsed.Issuer) || parsed.ExpiresAt == nil {
		return Delegation{}, errUnauthorized
	}
	if len(parsed.Audience) == 0 || !mcpauth.OpenIDSigningMethod(token.Method.Alg()) {
		return Delegation{}, errUnauthorized
	}
	kid, ok := token.Header["kid"].(string)
	if !ok || !mcpauth.ValidClaimText(kid) {
		return Delegation{}, errUnauthorized
	}
	for _, audience := range parsed.Audience {
		if !mcpauth.ValidClaimText(audience) {
			return Delegation{}, errUnauthorized
		}
	}
	return Delegation{Issuer: parsed.Issuer, Subject: parsed.Subject, ExpiresAt: parsed.ExpiresAt.Time}, nil
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
// Only NewContext populates this request-scoped context: on the socket, after
// the Delegator verified the token.
func DelegationFromContext(ctx context.Context) (Delegation, string, bool) {
	c, ok := ctx.Value(delegationContextKey{}).(delegatedCredential)
	if !ok || ctx.Err() != nil || !time.Now().Before(c.delegation.ExpiresAt) {
		return Delegation{}, "", false
	}
	return c.delegation, c.token, true
}

// Delegator verifies the tokens of the socket with the OpenID provider of the
// collector, configured, before they reach the collector.
type Delegator struct {
	issuer, audience string
	verifier         TokenVerifier
}

// NewDelegator verifies the tokens for issuer and audience, the issuer and
// the client ID of the OpenID provider the collector trusts.
func NewDelegator(issuer, audience string, verifier TokenVerifier) (*Delegator, error) {
	if issuer == "" || audience == "" || verifier == nil {
		return nil, errors.New("token delegation requires the OpenID provider of the collector")
	}
	return &Delegator{issuer: issuer, audience: audience, verifier: verifier}, nil
}

// Middleware requires one Bearer token, verified with the provider of the
// collector. It is mounted on the local Unix socket only.
func (d *Delegator) Middleware(next http.Handler) http.Handler {
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
		if err == nil && len(values) == 1 && !r.URL.Query().Has("access_token") {
			err = d.verify(ctx, raw)
		}
		if errors.Is(err, mcpauth.ErrVerificationUnavailable) {
			if cancel != nil {
				cancel()
			}
			w.Header().Set("Retry-After", "30")
			WriteProblem(w, http.StatusServiceUnavailable, "The keys of the OpenID provider of the collector could not be loaded; retry later.")
			return
		}
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
	if _, token, ok := mcpauth.ExchangedFromContext(ctx); ok {
		return token, true
	}
	_, token, ok := DelegationFromContext(ctx)
	return token, ok
}

// verify proves the token with the OpenID provider of the collector:
// signature by a key of its JWKS, issuer, audience (its client ID) and
// lifetime. A refused token never reaches the collector.
func (d *Delegator) verify(ctx context.Context, raw string) error {
	// A token of another issuer is refused without loading any key.
	if delegation, _, _ := DelegationFromContext(ctx); delegation.Issuer != d.issuer {
		return errUnauthorized
	}
	_, err := d.verifier.Verify(ctx, raw, d.issuer, d.audience)
	return err
}

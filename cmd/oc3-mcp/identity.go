package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/opensvc/oc3-mcp/internal/auth"
	"github.com/opensvc/oc3-mcp/internal/client"
)

// collectorIdentity is the part of the collector GET /auth/whoami answer the
// MCP checks.
type collectorIdentity struct {
	AuthSource string `json:"auth_source"`
	Issuer     string `json:"issuer"`
	Subject    string `json:"subject"`
}

// serveWhoAmI is a narrow identity bridge, not an MCP tool or token issuer.
// It proves the caller's access token by asking the collector who it
// authenticates, and answers the identity only when the collector confirms
// the token's own issuer and subject.
func serveWhoAmI(api *client.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		delegation, _, ok := auth.DelegationFromContext(r.Context())
		// refuse answers a problem and records one audit line for it.
		refuse := func(status int, outcome string, extra ...slog.Attr) {
			auth.WriteProblem(w, status, "")
			auditWhoAmI(delegation, outcome, status, started, extra...)
		}
		if !ok {
			refuse(http.StatusUnauthorized, "expired")
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			refuse(http.StatusBadRequest, "bad_request")
			return
		}
		var identity collectorIdentity
		if err := api.GetJSON(r.Context(), "/auth/whoami", nil, &identity); err != nil {
			if !time.Now().Before(delegation.ExpiresAt) {
				refuse(http.StatusUnauthorized, "expired")
				return
			}
			var apiError *client.APIError
			if errors.As(err, &apiError) && (apiError.StatusCode == http.StatusUnauthorized || apiError.StatusCode == http.StatusForbidden) {
				refuse(apiError.StatusCode, "collector_refused", slog.Int("collector_status", apiError.StatusCode))
				return
			}
			extra := []slog.Attr{slog.String("detail", auditValue(err.Error()))}
			if errors.As(err, &apiError) {
				extra = append(extra, slog.Int("collector_status", apiError.StatusCode))
			}
			refuse(http.StatusBadGateway, "collector_failed", extra...)
			return
		}
		// A session or a password, or the identity of another token, cannot
		// prove this one.
		if identity.AuthSource != "bearer" || identity.Issuer != delegation.Issuer || identity.Subject != delegation.Subject {
			refuse(http.StatusUnauthorized, "identity_mismatch", slog.String("auth_source", auditValue(identity.AuthSource)))
			return
		}
		if _, _, ok := auth.DelegationFromContext(r.Context()); !ok {
			refuse(http.StatusUnauthorized, "expired")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Issuer    string    `json:"issuer"`
			Subject   string    `json:"subject"`
			ExpiresAt time.Time `json:"expires_at"`
		}{delegation.Issuer, delegation.Subject, delegation.ExpiresAt.UTC()})
		auditWhoAmI(delegation, "ok", http.StatusOK, started)
	}
}

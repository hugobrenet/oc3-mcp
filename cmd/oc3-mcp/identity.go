package main

import (
	"encoding/json"
	"errors"
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
		delegation, _, ok := auth.DelegationFromContext(r.Context())
		if !ok {
			auth.WriteProblem(w, http.StatusUnauthorized, "")
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			auth.WriteProblem(w, http.StatusBadRequest, "")
			return
		}
		var identity collectorIdentity
		if err := api.GetJSON(r.Context(), "/auth/whoami", nil, &identity); err != nil {
			if !time.Now().Before(delegation.ExpiresAt) {
				auth.WriteProblem(w, http.StatusUnauthorized, "")
				return
			}
			var apiError *client.APIError
			if errors.As(err, &apiError) && (apiError.StatusCode == http.StatusUnauthorized || apiError.StatusCode == http.StatusForbidden) {
				auth.WriteProblem(w, apiError.StatusCode, "")
				return
			}
			auth.WriteProblem(w, http.StatusBadGateway, "")
			return
		}
		// A session or a password, or the identity of another token, cannot
		// prove this one.
		if identity.AuthSource != "bearer" || identity.Issuer != delegation.Issuer || identity.Subject != delegation.Subject {
			auth.WriteProblem(w, http.StatusUnauthorized, "")
			return
		}
		if _, _, ok := auth.DelegationFromContext(r.Context()); !ok {
			auth.WriteProblem(w, http.StatusUnauthorized, "")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Issuer    string    `json:"issuer"`
			Subject   string    `json:"subject"`
			ExpiresAt time.Time `json:"expires_at"`
		}{delegation.Issuer, delegation.Subject, delegation.ExpiresAt.UTC()})
	}
}

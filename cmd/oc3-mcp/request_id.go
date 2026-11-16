package main

import (
	"context"
	"net/http"
	"regexp"

	"github.com/opensvc/oc3-mcp/internal/requestid"
)

// requestIDHeader carries the request ID of the AI agent, to join its audit
// lines with those of the MCP. It is not an identity: any process reaching the
// socket may set it, so only the agent format is kept.
const requestIDHeader = requestid.Header

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// withRequestID keeps one well-formed X-Request-ID in the request context, for
// the audit lines and the requests to the collector. Any other value is
// ignored.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if values := r.Header.Values(requestIDHeader); len(values) == 1 && requestIDPattern.MatchString(values[0]) {
			r = r.WithContext(requestid.With(r.Context(), values[0]))
		}
		next.ServeHTTP(w, r)
	})
}

func requestIDFromContext(ctx context.Context) string {
	return requestid.FromContext(ctx)
}

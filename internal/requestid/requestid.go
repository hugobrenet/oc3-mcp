// Package requestid carries the request ID of the AI agent, which the MCP
// writes on its audit lines and forwards to the collector, to join the audit
// of the three.
package requestid

import "context"

// Header names the request ID, on the requests of the agent and on those the
// MCP sends to the collector.
const Header = "X-Request-ID"

type contextKey struct{}

// With returns ctx carrying the request ID.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the request ID of ctx, empty when it has none.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/auth"
)

const maxAuditValueBytes = 256

// auditLogger receives one credential-free line per tool call. Tests replace it.
var auditLogger = slog.Default()

// auditToolCalls traces who called which tool, through which listener and
// client application, and the exchange and call outcomes. It never logs
// tokens, raw arguments or tool results.
func auditToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		call, ok := request.(*mcp.CallToolRequest)
		if method != "tools/call" || !ok || call.Params == nil {
			return next(ctx, method, request)
		}
		identity, _, _ := auth.OAuthFromContext(ctx)
		ctx, trace := auth.WithCallTrace(ctx)
		start := time.Now()
		result, err := next(ctx, method, request)

		issuer, subject, clientID := identity.Issuer, identity.Subject, identity.ClientID
		exchange, collectorSubject := trace.Exchange, trace.CollectorSubject
		if exchange == "" {
			exchange = "none"
		}
		// On the Unix socket, the collector receives the caller's token
		// unchanged and verifies it on each call: a refused call may log
		// unverified claims.
		if delegation, _, ok := auth.DelegationFromContext(ctx); ok {
			issuer, subject = delegation.Issuer, delegation.Subject
			exchange, collectorSubject = "delegated", delegation.Subject
		}
		outcome, detail := "ok", ""
		if err != nil {
			outcome, detail = "error", err.Error()
		} else if r, ok := result.(*mcp.CallToolResult); ok && r.IsError {
			outcome = "tool_error"
			for _, content := range r.Content {
				if text, ok := content.(*mcp.TextContent); ok {
					detail = text.Text
					break
				}
			}
		}
		attrs := []slog.Attr{
			slog.String("tool", auditValue(call.Params.Name)),
			slog.String("issuer", issuer),
			slog.String("subject", subject),
			slog.String("client_id", clientID),
			slog.String("exchange", auditValue(exchange)),
			slog.String("collector_subject", collectorSubject),
			slog.String("outcome", outcome),
			slog.Duration("duration", time.Since(start).Round(time.Millisecond)),
		}
		if detail != "" {
			attrs = append(attrs, slog.String("detail", auditValue(detail)))
		}
		attrs = append(attrs, slog.String("request_id", requestIDFromContext(ctx)))
		auditLogger.LogAttrs(context.Background(), slog.LevelInfo, "mcp tool call", attrs...)
		return result, err
	}
}

// auditWhoAmI records one identity check of the whoami bridge: the claims of
// the token, which the collector verifies, what the bridge answered and why.
func auditWhoAmI(ctx context.Context, delegation auth.Delegation, outcome string, status int, started time.Time, extra ...slog.Attr) {
	attrs := []slog.Attr{
		slog.String("issuer", delegation.Issuer),
		slog.String("subject", delegation.Subject),
		slog.String("outcome", outcome),
		slog.Int("status", status),
		slog.Duration("duration", time.Since(started).Round(time.Millisecond)),
	}
	attrs = append(attrs, extra...)
	attrs = append(attrs, slog.String("request_id", requestIDFromContext(ctx)))
	auditLogger.LogAttrs(context.Background(), slog.LevelInfo, "mcp whoami", attrs...)
}

func auditValue(s string) string {
	if len(s) > maxAuditValueBytes {
		return s[:maxAuditValueBytes] + "..."
	}
	return s
}

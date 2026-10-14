package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

// exchangeToolCalls exchanges, for each tool call on the HTTPS listener, the
// MCP access token of the external client for a token intended for the
// collector. The profile and the audience come from the configuration.
func exchangeToolCalls(profiles *mcpauth.ExchangeProfiles, profile, audience string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, request)
			}
			exchanged, cancel, err := profiles.Prepare(ctx, profile, audience, "")
			if err != nil {
				return toolError(err.Error()), nil
			}
			defer cancel()
			return next(exchanged, method, request)
		}
	}
}

// blockToolCalls keeps the tools discoverable on HTTPS while no token exchange
// is configured: no external token can reach the collector.
func blockToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method == "tools/call" {
			return toolError("collector calls require the token exchange configuration"), nil
		}
		return next(ctx, method, request)
	}
}

func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

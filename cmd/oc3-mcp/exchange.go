package main

import (
	"context"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/content"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

// exchangeToolCalls exchanges, for each tool call on the HTTPS listener, and
// each read of the resources and the skills of the collector, the MCP access
// token of the external client for a token intended for the collector. The
// profile and the audience come from the configuration.
func exchangeToolCalls(profiles *mcpauth.ExchangeProfiles, profile, audience string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method != "tools/call" && !slices.Contains(content.Methods, method) {
				return next(ctx, method, request)
			}
			exchanged, cancel, err := profiles.Prepare(ctx, profile, audience, "")
			if err != nil {
				if method != "tools/call" {
					return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: err.Error()}
				}
				return toolError(err.Error()), nil
			}
			defer cancel()
			return next(exchanged, method, request)
		}
	}
}

// codeCallLimited is the JSON-RPC error of a content read refused by the
// limits of its identity: a server error, not an internal one, so that a
// client reads a limit to retry after the delay its message gives, not a
// failure of the server. om3-mcp answers with the same code.
const codeCallLimited = -32029

// limitToolCalls refuses the tool calls of an identity beyond its limits,
// before any token exchange or collector request. The audit records them.
// The reads of the resources and the skills reach the collector too: they
// count against the same limits, so that no method, alone or in a batch,
// sends the collector more than the limits of its identity allow.
func limitToolCalls(limiter *mcpauth.CallLimiter) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method != "tools/call" && !slices.Contains(content.Methods, method) {
				return next(ctx, method, request)
			}
			release, err := limiter.Acquire(ctx)
			if err != nil {
				if method != "tools/call" {
					return nil, &jsonrpc.Error{Code: codeCallLimited, Message: err.Error()}
				}
				return toolError(err.Error()), nil
			}
			defer release()
			return next(ctx, method, request)
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
		if slices.Contains(content.Methods, method) {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "collector calls require the token exchange configuration"}
		}
		return next(ctx, method, request)
	}
}

func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

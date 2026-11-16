package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A tool the collector can no longer serve is audited as such, an
// outcome of its own, which the audit is searched by; another refusal stays a
// tool error.
func TestAuditNamesAnIncompatibleCollector(t *testing.T) {
	defer func(logger *slog.Logger) { auditLogger = logger }(auditLogger)
	for text, outcome := range map[string]string{
		"collector_incompatible: the tool list_nodes cannot be used with this collector": "collector_incompatible",
		"OpenSVC collector GET /nodes returned HTTP 403 Forbidden":                       "tool_error",
	} {
		var audit syncBuffer
		auditLogger = slog.New(slog.NewTextHandler(&audit, nil))
		handler := auditToolCalls(func(context.Context, string, mcp.Request) (mcp.Result, error) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
		})
		request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "list_nodes", Arguments: json.RawMessage(`{}`)}}
		if _, err := handler(context.Background(), "tools/call", request); err != nil {
			t.Fatal(err)
		}
		if line := audit.String(); !strings.Contains(line, " outcome="+outcome+" ") {
			t.Errorf("got %q, want outcome %s", line, outcome)
		}
	}
}

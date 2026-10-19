package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

func readOnlyClosedWorldAnnotations() *mcp.ToolAnnotations {
	destructive := false
	openWorld := false
	return &mcp.ToolAnnotations{
		DestructiveHint: &destructive,
		OpenWorldHint:   &openWorld,
		ReadOnlyHint:    true,
	}
}

// actionAnnotations describe a tool that changes the collector state.
// Clients identify these tools by readOnlyHint false; the OpenSVC AI agent
// asks the user to confirm a destructive one before it runs.
func actionAnnotations(destructive, idempotent bool) *mcp.ToolAnnotations {
	openWorld := false
	return &mcp.ToolAnnotations{
		DestructiveHint: &destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &openWorld,
		ReadOnlyHint:    false,
	}
}

package tools

import (
	"errors"
	"fmt"

	"github.com/opensvc/oc3-mcp/internal/client"
)

// serverVersion is the version of this MCP, which a refusal of a collector it
// cannot use names: the one to upgrade.
var serverVersion = "unknown"

// SetServerVersion sets the version the refusals of an incompatible collector name.
func SetServerVersion(version string) {
	if version != "" {
		serverVersion = version
	}
}

// explainIncompatible tells the model, in the result of a tool, that the
// collector answered in a way this MCP cannot use: its API moved on, a route
// gone or a response of another shape. Without it, the model reads a decode
// error as a passing failure, and answers from other tools as if there were
// nothing, as om3-mcp saw on 2026-11-13.
func explainIncompatible(tool string, err error) error {
	if err == nil || !errors.Is(err, client.ErrIncompatible) {
		return err
	}
	return &incompatibleToolError{tool: tool, version: serverVersion, err: err}
}

// incompatibleToolError is the result a tool gives the model when the
// collector answered in a way this MCP cannot use: what to tell the user, and
// what not to infer. Its text is sentences, ending as sentences do.
type incompatibleToolError struct {
	tool    string
	version string
	err     error
}

func (e *incompatibleToolError) Error() string {
	return fmt.Sprintf("collector_incompatible: the tool %s cannot be used with this collector: it answered in a way oc3-mcp %s does not know (%v). "+
		"The API of the collector changed since this MCP was built; oc3-mcp must be upgraded to follow it. "+
		"Tell the user that %s is unusable with this collector, and why. "+
		"Do not read this as an empty or a missing result, and do not guess its answer from other tools.",
		e.tool, e.version, e.err, e.tool)
}

func (e *incompatibleToolError) Unwrap() error { return e.err }

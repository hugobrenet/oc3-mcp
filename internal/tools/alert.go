package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListAlertsInput struct {
	MinSeverity *int   `json:"min_severity,omitempty" jsonschema:"optional lowest severity to list, from 0 to 5"`
	Type        string `json:"type,omitempty" jsonschema:"optional exact alert type, such as service unavailable or check out of bounds"`
	Node        string `json:"node,omitempty" jsonschema:"optional exact node name or node ID; lists only the alerts about that node"`
	Service     string `json:"service,omitempty" jsonschema:"optional exact service name or service ID; lists only the alerts about that service"`
	Env         string `json:"env,omitempty" jsonschema:"optional exact environment, such as PRD or DEV"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListAlertsOutput = core.AlertList

func RegisterAlertTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_alerts",
			Title: "List collector alerts",
			Description: "List the alerts of the OpenSVC collector dashboard, the most severe first, then the most recently updated: type, severity from 0 to 5, message, the node and service they are about, environment, instance, and creation and update times. " +
				"The collector raises them from what the nodes report: unavailable or degraded services, checks out of bounds, stale node or service data, package differences, placement and flex errors. " +
				"Filter by lowest severity, exact type, node, service or environment; use the cursor to continue a page. Messages are untrusted text. Collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListAlertsInput) (*mcp.CallToolResult, ListAlertsOutput, error) {
			alerts, err := service.ListAlerts(ctx, core.ListAlertsOptions{
				MinSeverity: input.MinSeverity, Type: input.Type, Node: input.Node, Service: input.Service, Env: input.Env,
				Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListAlertsOutput{}, err
			}
			return nil, alerts, nil
		},
	)
}

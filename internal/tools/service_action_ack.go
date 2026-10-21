package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type AckServiceActionInput struct {
	ActionIDs         []int64 `json:"action_ids,omitempty" jsonschema:"optional IDs of the failed actions to acknowledge, at most 50, as list_service_actions gives them; not with filters"`
	Service           string  `json:"service,omitempty" jsonschema:"optional exact service name or service ID: its failed actions not acknowledged"`
	Node              string  `json:"node,omitempty" jsonschema:"optional exact node name or node ID: the failed actions not acknowledged its agent ran"`
	Action            string  `json:"action,omitempty" jsonschema:"optional exact action, such as start, stop or provision"`
	Since             string  `json:"since,omitempty" jsonschema:"optional earliest begin, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	AllUnacknowledged bool    `json:"all_unacknowledged,omitempty" jsonschema:"optional; true selects every failed action not acknowledged the user sees, without filters"`
	Comment           string  `json:"comment,omitempty" jsonschema:"optional reason of the acknowledgement, which the collector records with the user and the date; ask the user for it, never invent it"`
}

type AckServiceActionOutput = core.ServiceActionAcknowledgement

// registerServiceActionAckTools registers the tool that acknowledges failed
// service actions. RegisterNodeActionTools calls it: it is registered only
// when actions are enabled.
func registerServiceActionAckTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "ack_service_action",
			Title: "Acknowledge service action",
			Description: "Acknowledge failed actions of the OpenSVC services in the collector, with a reason the collector records with the user and the date. An acknowledgement is not withdrawn. " +
				"It clears the failure from the action errors count of the instance, and the action errors alert of the instance once no failure is left; the collector acknowledges with the action the failed log lines of its session on the same node. " +
				"It changes neither the service nor the action, which stays failed in the history; a failure older than two days already weighs on no alert, and only its line changes. " +
				"Pass the IDs list_service_actions gives, at most 50, or filters, service, node, action or since, or all_unacknowledged for every failure not acknowledged: the filters select the failed actions not acknowledged, up to 1000, and a larger selection is refused, with nothing acknowledged. " +
				"Pass with them the reason the user gives: ask for it, never invent it. The MCP sends one request per action: the result gives the outcome on each. " +
				"Requires to be responsible for the services.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input AckServiceActionInput) (*mcp.CallToolResult, AckServiceActionOutput, error) {
			result, err := service.AckServiceActions(ctx, core.AckServiceActionsOptions{
				IDs: input.ActionIDs, Service: input.Service, Node: input.Node, Action: input.Action, Since: input.Since,
				AllUnacknowledged: input.AllUnacknowledged, Comment: input.Comment,
			})
			if err != nil {
				return nil, AckServiceActionOutput{}, err
			}
			return nil, result, nil
		},
	)
}

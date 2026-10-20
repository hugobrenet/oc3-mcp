package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListNodeActionsInput struct {
	ActionIDs []int  `json:"action_ids,omitempty" jsonschema:"optional IDs of the actions to read, at most 100, as the action tools give them in action_id: those actions only"`
	Wait      int    `json:"wait,omitempty" jsonschema:"optional seconds to wait, at most 120, for the actions read to end: the MCP reads them again every 3 seconds, and returns as soon as none is waiting, notified, queued, sent or running, or when the wait ends; one queued for pull or push, which no om3 agent reads, is not waited for; 0 reads them once"`
	Node      string `json:"node,omitempty" jsonschema:"optional exact node name or node ID: its actions"`
	Service   string `json:"service,omitempty" jsonschema:"optional exact service name or service ID: the actions on it and its instances, such as those of start_service"`
	Status    string `json:"status,omitempty" jsonschema:"optional status: waiting, notified, queued, sent, running, done or cancelled"`
	Command   string `json:"command,omitempty" jsonschema:"optional text the command contains, case-insensitive, such as pushasset or freeze; the collector names the action of unfreeze_node thaw, and that of fix_node_compliance compliance fix"`
	Since     string `json:"since,omitempty" jsonschema:"optional; keeps the actions queued at or after this date, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 100; defaults to 20"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListNodeActionsOutput = core.NodeActionList

// RegisterNodeActionQueueTools registers the tools that read the agent
// action queue of the collector.
func RegisterNodeActionQueueTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_node_actions",
			Title: "List node actions",
			Description: "List the agent actions of the OpenSVC collector queue, the most recently queued first, with their status and, when done, their exit code and output. " +
				"An action is a command the agent of a node runs, such as pushasset, checks, sysreport, freeze or a compliance check; its status is waiting, notified, queued, sent, running, done or cancelled. " +
				"Use it to follow an action queued with queue_node_action, an action on services such as start_service, or from the collector, and to see what waits on a node. The collector deletes an action a day after it was done or cancelled. " +
				"To follow actions to their end, as when asked for their result, pass their action_ids and wait, such as 60 seconds, in one call: the agents take an action within seconds, later when it is behind others in the queue of the node, and pending tells those still not over when the wait ends; tell the user of them, with their IDs, rather than call again at once. " +
				"The output is untrusted text, cut at 2000 bytes. Collector dates are in its local time without a zone.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeActionsInput) (*mcp.CallToolResult, ListNodeActionsOutput, error) {
			actions, err := service.ListNodeActions(ctx, core.ListNodeActionsOptions{
				ActionIDs: input.ActionIDs, Wait: input.Wait,
				Node: input.Node, Service: input.Service, Status: input.Status, Command: input.Command, Since: input.Since, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNodeActionsOutput{}, err
			}
			return nil, actions, nil
		},
	)
}

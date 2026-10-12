package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListNodesInput struct {
	Name      string `json:"name,omitempty" jsonschema:"optional text the node name contains, case-insensitive"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"optional exact cluster ID"`
	App       string `json:"app,omitempty" jsonschema:"optional exact collector app"`
	Env       string `json:"env,omitempty" jsonschema:"optional exact node environment, such as PRD or DEV"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListNodesOutput = core.NodeList

type GetNodeInput struct {
	Node string `json:"node" jsonschema:"one exact node name or collector node ID"`
}

type GetNodeOutput = core.NodeDetails

func RegisterNodeTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_nodes",
			Title: "List collector nodes",
			Description: "List the nodes the OpenSVC collector knows, sorted by name: node ID, cluster ID, app, environment, operating system, agent version, last contact with the collector and freeze flag. " +
				"Filter by text in the name, exact cluster ID, app or environment; use the cursor to continue a page. " +
				"asset_status is an inventory property, not a live state: last_comm tells when the node daemon last reported. " +
				"Collector dates are in its local time without a zone. Use get_node for the details of one node.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodesInput) (*mcp.CallToolResult, ListNodesOutput, error) {
			nodes, err := service.ListNodes(ctx, core.ListNodesOptions{
				Name: input.Name, ClusterID: input.ClusterID, App: input.App, Env: input.Env, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNodesOutput{}, err
			}
			return nil, nodes, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_node",
			Title: "Get collector node",
			Description: "Read one node of the OpenSVC collector by exact name or node ID: identity, cluster, app, environment, freeze state, agent version and last contact, operating system, hardware, teams, location, and warranty, maintenance and obsolescence dates. " +
				"A name several nodes hold is refused with their node IDs; pass one of them. " +
				"asset_status is an inventory property, not a live state. Collector dates are in its local time without a zone.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeInput) (*mcp.CallToolResult, GetNodeOutput, error) {
			node, err := service.GetNode(ctx, input.Node)
			if err != nil {
				return nil, GetNodeOutput{}, err
			}
			return nil, node, nil
		},
	); err != nil {
		return err
	}
	return nil
}

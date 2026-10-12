package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListNodesInput struct {
	Name         string `json:"name,omitempty" jsonschema:"optional text the node name contains, case-insensitive"`
	ClusterID    string `json:"cluster_id,omitempty" jsonschema:"optional exact cluster ID"`
	App          string `json:"app,omitempty" jsonschema:"optional exact collector app"`
	Env          string `json:"env,omitempty" jsonschema:"optional exact node environment, such as PRD or DEV"`
	Frozen       bool   `json:"frozen,omitempty" jsonschema:"optional; true lists only the frozen nodes"`
	FrozenBefore string `json:"frozen_before,omitempty" jsonschema:"optional; lists only the nodes frozen at or before this date, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	Limit        int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor       string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListNodesOutput = core.NodeList

type GetNodeInput struct {
	Node string `json:"node" jsonschema:"one exact node name or collector node ID"`
}

type GetNodeOutput = core.NodeDetails

type GetNodeSanTopologyInput struct {
	Node string `json:"node" jsonschema:"one exact node name or collector node ID"`
}

type GetNodeSanTopologyOutput = core.NodeSanTopology

func RegisterNodeTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_nodes",
			Title: "List collector nodes",
			Description: "List the nodes the OpenSVC collector knows, sorted by name: node ID, cluster ID, app, environment, operating system, agent version, last contact with the collector, freeze flag and freeze date. " +
				"Filter by text in the name, exact cluster ID, app or environment, by frozen state, or by freeze date with frozen_before to list the nodes frozen since a date; use the cursor to continue a page. " +
				"frozen_at is empty when the collector does not store the freeze date of the node. " +
				"asset_status is an inventory property, not a live state: last_comm tells when the node daemon last reported. " +
				"Collector dates are in its local time without a zone. Use get_node for the details of one node.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodesInput) (*mcp.CallToolResult, ListNodesOutput, error) {
			nodes, err := service.ListNodes(ctx, core.ListNodesOptions{
				Name: input.Name, ClusterID: input.ClusterID, App: input.App, Env: input.Env, Frozen: input.Frozen, FrozenBefore: input.FrozenBefore,
				Limit: input.Limit, Cursor: input.Cursor,
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
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_node_san_topology",
			Title: "Get node SAN topology",
			Description: "Read the fibre channel SAN wiring of one node, as the OpenSVC collector computes it from the node adapters, the SAN switch ports and the zoning: " +
				"for each adapter port, the paths through the switches of each fabric to the storage array target ports, with the ports and the cable speeds of each hop; " +
				"for each storage array, the fabrics, adapters, target ports and number of paths reaching it; and the devices and cables of the wiring. " +
				"Use it to check the redundancy of the SAN paths of a node, such as an array reached through one fabric only, or a slower hop. " +
				"wired is false for a node without a fibre channel wiring known to the collector, such as an iSCSI node.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeSanTopologyInput) (*mcp.CallToolResult, GetNodeSanTopologyOutput, error) {
			topology, err := service.GetNodeSanTopology(ctx, input.Node)
			if err != nil {
				return nil, GetNodeSanTopologyOutput{}, err
			}
			return nil, topology, nil
		},
	); err != nil {
		return err
	}
	return nil
}

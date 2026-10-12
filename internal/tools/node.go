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

type ListNodeDisksInput struct {
	Node   string `json:"node" jsonschema:"one exact node name or collector node ID"`
	DiskID string `json:"disk_id,omitempty" jsonschema:"optional text the disk ID contains, case-insensitive, such as the end of a world wide identifier"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListNodeDisksOutput = core.NodeDiskList

type GetNodeStatsInput struct {
	Node    string `json:"node" jsonschema:"one exact node name or collector node ID"`
	Group   string `json:"group" jsonschema:"the statistics group: cpu, mem, swap, proc, block, netdev, netdev_err or blockdev"`
	Days    int    `json:"days,omitempty" jsonschema:"optional days of history; defaults to 1; at most 1095, or 30 for netdev, netdev_err and blockdev"`
	Metric  string `json:"metric,omitempty" jsonschema:"optional exact metric, such as usr, pct_memused, ldavg_15 or pct_util"`
	Device  string `json:"device,omitempty" jsonschema:"optional exact device, for the netdev, netdev_err and blockdev groups"`
	Samples int    `json:"samples,omitempty" jsonschema:"optional number of equal time slices, up to 48, whose means are returned per series to show the trend; 0 by default"`
	At      string `json:"at,omitempty" jsonschema:"optional UTC time, RFC 3339 such as 2026-10-12T14:30:00Z, to read the value of each series at that time; the days read are extended to cover it"`
}

type GetNodeStatsOutput = core.NodeStats

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
			Name:  "list_node_disks",
			Title: "List node disks",
			Description: "List the disks one node sees, as the OpenSVC collector knows them from its agent and the storage arrays: disk ID, size and used size in MiB, vendor, model, storage array and its disk group, the disk group the node reports, the service using the disk when the collector knows it, " +
				"and the other nodes seeing the same disk. Use it to tell which LUNs a node sees, on which array, and whether a disk is visible from the other nodes a service may fail over to. " +
				"A disk used by several services is one row per service. Local disks carry their device name, such as vda, and are never shared. Collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeDisksInput) (*mcp.CallToolResult, ListNodeDisksOutput, error) {
			disks, err := service.ListNodeDisks(ctx, core.ListNodeDisksOptions{Node: input.Node, DiskID: input.DiskID, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListNodeDisksOutput{}, err
			}
			return nil, disks, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_node_stats",
			Title: "Get node statistics",
			Description: "Sum up the performance statistics the OpenSVC collector keeps for one node over the last days, for one group: cpu (usr, sys, iowait, idle, steal… in %), mem (pct_memused, pct_commit, kb…), swap, proc (load averages, process counts), block (I/O), netdev and netdev_err per interface, or blockdev per disk. " +
				"Each series gives its last, lowest, mean and highest value with the time of the highest, and with samples the means of equal time slices to show the trend. " +
				"Use it to tell whether a node is or was loaded, saturated or swapping, and when. Filter by metric, or by device for the device groups, which hold one series per device. " +
				"With at, each series also gives its value at that time, from the closest point. " +
				"The resolution is a minute over half an hour, ten minutes over three days, an hour over ninety days, a day beyond: the older the time, the coarser the point. Times are UTC.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeStatsInput) (*mcp.CallToolResult, GetNodeStatsOutput, error) {
			stats, err := service.GetNodeStats(ctx, core.GetNodeStatsOptions{
				Node: input.Node, Group: input.Group, Days: input.Days, Metric: input.Metric, Device: input.Device, Samples: input.Samples, At: input.At,
			})
			if err != nil {
				return nil, GetNodeStatsOutput{}, err
			}
			return nil, stats, nil
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

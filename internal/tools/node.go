package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// NodeFiltersInput are the node filters shared by list_nodes and count_nodes.
type NodeFiltersInput struct {
	Name         string `json:"name,omitempty" jsonschema:"optional text the node name contains, case-insensitive"`
	ClusterID    string `json:"cluster_id,omitempty" jsonschema:"optional exact cluster ID"`
	App          string `json:"app,omitempty" jsonschema:"optional exact collector app"`
	Env          string `json:"env,omitempty" jsonschema:"optional exact node environment, such as PRD or DEV"`
	Status       string `json:"status,omitempty" jsonschema:"optional exact node status, a property the collector users set on the node, whose values each organization chooses, such as Ready, Not Ready, Operating, up or down; use group_by status to see the values the collector holds"`
	Frozen       bool   `json:"frozen,omitempty" jsonschema:"optional; true keeps only the frozen nodes"`
	FrozenBefore string `json:"frozen_before,omitempty" jsonschema:"optional; keeps only the nodes frozen at or before this date, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	OSName       string `json:"os_name,omitempty" jsonschema:"optional text the operating system name contains, case-insensitive, such as linux, windows, sunos or freebsd"`
	OSVendor     string `json:"os_vendor,omitempty" jsonschema:"optional text the operating system vendor or distribution contains, case-insensitive, such as red hat, ubuntu or debian"`
	OSRelease    string `json:"os_release,omitempty" jsonschema:"optional exact operating system release, such as 9.4; a release ending with a dot, such as 9., selects every release starting with it"`
	AgentVersion string `json:"agent_version,omitempty" jsonschema:"optional exact OpenSVC agent version, such as 3.0.4; a version ending with a dot, such as 2., selects every version starting with it"`
	Manufacturer string `json:"manufacturer,omitempty" jsonschema:"optional text the hardware manufacturer contains, case-insensitive, such as dell or hpe"`
	Model        string `json:"model,omitempty" jsonschema:"optional text the hardware model contains, case-insensitive"`
	Team         string `json:"team,omitempty" jsonschema:"optional text the name of the team responsible for the node contains, case-insensitive"`
	Country      string `json:"country,omitempty" jsonschema:"optional text the country of the node location contains, case-insensitive, such as France"`
	City         string `json:"city,omitempty" jsonschema:"optional text the city of the node location contains, case-insensitive"`
	SilentFor    string `json:"silent_for,omitempty" jsonschema:"optional; keeps only the nodes whose last contact with the collector is older than this duration, in days, hours or minutes, such as 7d, 12h or 30m; nodes that never reported are left out"`
}

func (f NodeFiltersInput) core() core.NodeFilters {
	return core.NodeFilters{
		Name: f.Name, ClusterID: f.ClusterID, App: f.App, Env: f.Env, Status: f.Status, Frozen: f.Frozen, FrozenBefore: f.FrozenBefore,
		OSName: f.OSName, OSVendor: f.OSVendor, OSRelease: f.OSRelease, AgentVersion: f.AgentVersion,
		Manufacturer: f.Manufacturer, Model: f.Model, Team: f.Team, Country: f.Country, City: f.City, SilentFor: f.SilentFor,
	}
}

type ListNodesInput struct {
	NodeFiltersInput
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type CountNodesInput struct {
	NodeFiltersInput
	GroupBy string `json:"group_by,omitempty" jsonschema:"optional grouping: cluster_id, app, env, status, type, os_name, os_vendor, os_release, os_arch, agent_version, manufacturer, model, team, country or city"`
	Limit   int    `json:"limit,omitempty" jsonschema:"optional number of values returned with group_by, the most frequent first, between 1 and 100; defaults to 20"`
}

type CountNodesOutput = core.NodeCount

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
			Description: "List the nodes the OpenSVC collector knows, sorted by name: node ID, cluster ID, app, environment, status, operating system, agent version, last contact with the collector, freeze flag and freeze date. " +
				"status is the node status the collector Nodes page shows, a property set by the collector users with values each organization chooses, not reported by the node daemon. " +
				"Filter by name, cluster, app, environment, status, operating system name, vendor and release, agent version, hardware manufacturer and model, responsible team, country, city, frozen state or freeze date, and by silent_for to find the nodes that stopped reporting; use the cursor to continue a page. " +
				"To count nodes, or to break them down by a property, use count_nodes instead of reading every page. " +
				"frozen_at is empty when the collector does not store the freeze date of the node. " +
				"last_comm tells when the node daemon last reported: use silent_for for the nodes that stopped reporting. " +
				"Collector dates are in its local time without a zone. Use get_node for the details of one node.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodesInput) (*mcp.CallToolResult, ListNodesOutput, error) {
			nodes, err := service.ListNodes(ctx, core.ListNodesOptions{NodeFilters: input.core(), Limit: input.Limit, Cursor: input.Cursor})
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
			Name:  "count_nodes",
			Title: "Count collector nodes",
			Description: "Count the nodes the OpenSVC collector knows that match the filters of list_nodes, as the collector computes it, and with group_by break them down by one property, such as os_name, os_release, env, app, agent_version, manufacturer, team or country, the most frequent values first. " +
				"Use it for any question of how many nodes, or of the distribution of the fleet, instead of reading the pages of list_nodes. " +
				"status is a property set by the collector users, with values each organization chooses, such as Ready, Not Ready or Operating, not reported by the node daemon: to filter on it, check its values with group_by status first. " +
				"silent_for tells the nodes whose daemon stopped reporting. " +
				"When a filter value is unsure, group_by that property first to see the values the collector holds. " +
				"The collector groups the values case-insensitively. Combine the filters to narrow the count, such as os_name linux and env PRD grouped by os_release.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CountNodesInput) (*mcp.CallToolResult, CountNodesOutput, error) {
			count, err := service.CountNodes(ctx, core.CountNodesOptions{NodeFilters: input.core(), GroupBy: input.GroupBy, Limit: input.Limit})
			if err != nil {
				return nil, CountNodesOutput{}, err
			}
			return nil, count, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_node",
			Title: "Get collector node",
			Description: "Read one node of the OpenSVC collector by exact name or node ID: identity, cluster, app, environment, status, freeze state, agent version and last contact, operating system, hardware, teams, location, and warranty, maintenance and obsolescence dates. " +
				"A name several nodes hold is refused with their node IDs; pass one of them. " +
				"status is the node status the collector Nodes page shows, a property set by the collector users with values each organization chooses, not reported by the node daemon; last_comm tells when the node daemon last reported. " +
				"Its tags are read with list_tags, the groups responsible for its app with get_app. Collector dates are in its local time without a zone.",
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

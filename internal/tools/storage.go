package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListStorageArraysInput struct {
	Name   string `json:"name,omitempty" jsonschema:"optional text the array name contains, case-insensitive"`
	Model  string `json:"model,omitempty" jsonschema:"optional text the array model contains, case-insensitive, such as powermax, vsp or flasharray"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 50; defaults to 20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListStorageArraysOutput = core.StorageArrayList

type ListStorageQuotasInput struct {
	App    string `json:"app,omitempty" jsonschema:"optional exact app code or app ID: its quotas"`
	Array  string `json:"array,omitempty" jsonschema:"optional exact array name: the quotas on its disk groups"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListStorageQuotasOutput = core.StorageQuotaList

type ListSANSwitchPortsInput struct {
	Fabric      string `json:"fabric,omitempty" jsonschema:"optional exact fabric name, such as lyn-fabric-a"`
	Switch      string `json:"switch,omitempty" jsonschema:"optional exact switch name"`
	State       string `json:"state,omitempty" jsonschema:"optional exact port state, such as Online or No_Light for a port without signal"`
	Type        string `json:"type,omitempty" jsonschema:"optional port type: F-Port for a device such as a node adapter or an array port, E-Port for a link to another switch, U-Port, N-Port, L-Port or G-Port"`
	ConnectedTo string `json:"connected_to,omitempty" jsonschema:"optional exact name of what is plugged on the other end: a node, a storage array or a switch"`
	Node        string `json:"node,omitempty" jsonschema:"optional exact node name or node ID: the ports its adapters are plugged in; for a storage array or a switch, pass connected_to"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 500; defaults to 100"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListSANSwitchPortsOutput = core.SANSwitchPortList

func RegisterStorageTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_storage_arrays",
			Title: "List storage arrays",
			Description: "List the storage arrays the OpenSVC collector knows, sorted by name: model, firmware and cache, and their disk groups, or pools, with their size, used, free and reserved space in MB, and the totals of each array. " +
				"Use it for the capacity left on the arrays and which one is the fullest. list_storage_quotas gives the storage quotas of the apps on the disk groups. " +
				"Collector dates are in its local time without a zone.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListStorageArraysInput) (*mcp.CallToolResult, ListStorageArraysOutput, error) {
			arrays, err := service.ListStorageArrays(ctx, core.ListStorageArraysOptions{Name: input.Name, Model: input.Model, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListStorageArraysOutput{}, err
			}
			return nil, arrays, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_storage_quotas",
			Title: "List storage quotas",
			Description: "List the storage quotas the apps hold on the disk groups of the storage arrays, of one app or of every app, optionally on one array: the quota and its used space in MB, and the size, free and still reservable space of the disk group. " +
				"Use it for how much storage an app may allocate, and which apps hold quotas on an array. list_storage_arrays gives the capacity of the arrays. " +
				"A used space of zero means unknown as often as unused: the collector links the disks of an app to a disk group only when it knows the disk group of the disks, which the agents of the nodes do not report. " +
				"Without app, the MCP reads the quotas of each app, at most 100, one request each, again on every page: pass app when you can.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListStorageQuotasInput) (*mcp.CallToolResult, ListStorageQuotasOutput, error) {
			quotas, err := service.ListStorageQuotas(ctx, core.ListStorageQuotasOptions{App: input.App, Array: input.Array, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListStorageQuotasOutput{}, err
			}
			return nil, quotas, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_san_switch_ports",
			Title: "List SAN switch ports",
			Description: "List the ports of the SAN switches the OpenSVC collector knows, sorted by fabric, switch, slot and port: speed, type, state, world wide names, and what is plugged on the other end, a node, a storage array or another switch. " +
				"Use it for the ports without signal, which switches an array or a node is plugged in, and the links between switches. get_node_san_topology gives the SAN paths of one node, from its adapters to the arrays. " +
				"Every authenticated user sees every port. Collector dates are in its local time without a zone.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListSANSwitchPortsInput) (*mcp.CallToolResult, ListSANSwitchPortsOutput, error) {
			ports, err := service.ListSANSwitchPorts(ctx, core.ListSANSwitchPortsOptions{
				Fabric: input.Fabric, Switch: input.Switch, State: input.State, Type: input.Type, ConnectedTo: input.ConnectedTo, Node: input.Node, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListSANSwitchPortsOutput{}, err
			}
			return nil, ports, nil
		},
	)
}

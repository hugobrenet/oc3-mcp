package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListIPsInput struct {
	IP             string `json:"ip,omitempty" jsonschema:"optional exact IPv4 or IPv6 address, without prefix length, such as 10.0.3.12 or 2001:db8::1; any IPv6 spelling is accepted"`
	Node           string `json:"node,omitempty" jsonschema:"optional exact node name or node ID whose addresses to list"`
	Network        string `json:"network,omitempty" jsonschema:"optional exact name or ID of a network declared in the collector, such as production; lists the node addresses it contains"`
	IncludeSpecial bool   `json:"include_special,omitempty" jsonschema:"optional; without ip, true also lists the loopback, link-local, multicast and unspecified addresses, left out by default"`
	Limit          int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200, node and service addresses together; defaults to 50"`
	Cursor         string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListIPsOutput = core.IPList

func RegisterIPTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_ips",
			Title: "List IP addresses",
			Description: "Find which node or service holds an IP address, or list the IP addresses of one node or of a declared network, from the network inventory the OpenSVC agents report to the collector. " +
				"node_addresses lists each address with its prefix length, interface, MAC, node, declared network and when the node last reported it. " +
				"With ip, service_addresses follows with the ip.host and ip.netns resources of the services whose description names the address, such as a service virtual IP, with the node, the container and the resource status on each node: up where the address is plumbed. Container addresses of ip.netns resources appear there only. The page size counts both lists, and the cursor continues them. " +
				"The same private address, such as a docker0, virbr0 or lxcbr0 bridge address, is often listed on several nodes. Without ip, loopback, link-local and multicast addresses are left out unless include_special is set. " +
				"Pass at least one of ip, node or network; they combine, for both lists: with a network, a service address is listed only when the network contains it. It is an inventory, not a reachability check: an empty result means no visible inventory or resource description names the address, not that it is free; the other IP drivers, such as ip.cni without a fixed address or ip.route naming its gateway, are not searched. Collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListIPsInput) (*mcp.CallToolResult, ListIPsOutput, error) {
			ips, err := service.ListIPs(ctx, core.ListIPsOptions{
				IP: input.IP, Node: input.Node, Network: input.Network, IncludeSpecial: input.IncludeSpecial, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListIPsOutput{}, err
			}
			return nil, ips, nil
		},
	)
}

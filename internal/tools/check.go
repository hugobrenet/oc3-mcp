package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListNodeChecksInput struct {
	Node            string `json:"node" jsonschema:"one exact node name or node ID"`
	Type            string `json:"type,omitempty" jsonschema:"optional exact check type, such as fs_u, fs_i, mpath, vg_u, eth or zpool"`
	OutOfBoundsOnly bool   `json:"out_of_bounds_only,omitempty" jsonschema:"optional; true lists only the checks out of their thresholds"`
}

type ListServiceChecksInput struct {
	Service         string `json:"service" jsonschema:"one exact service name, such as ns/svc/name, or service ID"`
	Type            string `json:"type,omitempty" jsonschema:"optional exact check type, such as fs_u, fs_i, mpath, vg_u, eth or zpool"`
	OutOfBoundsOnly bool   `json:"out_of_bounds_only,omitempty" jsonschema:"optional; true lists only the checks out of their thresholds"`
}

type ListChecksOutput = core.CheckList

const checksDescription = "Each check holds its type, the instance it measures, the last value the agent reported, the low and high thresholds, whether the collector flags it out of bounds, where the thresholds come from (settings of the check on the node, a filterset, or the defaults of the type), and when it was last reported. " +
	"Check types include fs_u (filesystem usage and free space), fs_i (inode usage), mpath (active multipath paths), vg_u (volume group usage), eth (link state) and zpool. " +
	"Values are in the unit of the check type as the agent reports it. Checks out of bounds come first. At most 2000 checks are read. Collector dates are as it gives them."

func RegisterCheckTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "list_node_checks",
			Title:       "List node checks",
			Description: "List the checks the OpenSVC agent of one node reports to the collector: filesystems, inodes, multipath, volume groups, links and pools, those of the resources of its services included, with the service they belong to. " + checksDescription,
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeChecksInput) (*mcp.CallToolResult, ListChecksOutput, error) {
			checks, err := service.ListNodeChecks(ctx, input.Node, core.ListChecksOptions{Type: input.Type, OutOfBoundsOnly: input.OutOfBoundsOnly})
			if err != nil {
				return nil, ListChecksOutput{}, err
			}
			return nil, checks, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "list_service_checks",
			Title:       "List service checks",
			Description: "List the checks the OpenSVC agents report to the collector for the resources of one service, such as its volumes and filesystems, on all its nodes, with the node they run on. " + checksDescription,
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListServiceChecksInput) (*mcp.CallToolResult, ListChecksOutput, error) {
			checks, err := service.ListServiceChecks(ctx, input.Service, core.ListChecksOptions{Type: input.Type, OutOfBoundsOnly: input.OutOfBoundsOnly})
			if err != nil {
				return nil, ListChecksOutput{}, err
			}
			return nil, checks, nil
		},
	); err != nil {
		return err
	}
	return nil
}

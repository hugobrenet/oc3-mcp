package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// filtersetDescription tells what a filterset is, for the filterset tools.
const filtersetDescription = "A filterset selects nodes and services by criteria on their properties, such as nodes.loc_city = Paris, combined with AND, OR and NOT and possibly including other filtersets; contextual compliance rulesets, check thresholds, sysreport grants and the session filters of the users rely on them. "

type ListFiltersetsInput struct {
	Name   string `json:"name,omitempty" jsonschema:"optional text the filterset name contains, case-insensitive"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListFiltersetsOutput = core.FiltersetList

type GetFiltersetInput struct {
	Filterset string `json:"filterset" jsonschema:"one exact filterset name, such as prd-linux, or collector filterset ID"`
}

type GetFiltersetOutput = core.FiltersetDetails

func RegisterFiltersetTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_filtersets",
			Title: "List filtersets",
			Description: "List the filtersets of the OpenSVC collector, sorted by name: ID, name, author and update time. " +
				filtersetDescription +
				"Use get_filterset for the criteria of one, the nodes and services it selects and what uses it.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListFiltersetsInput) (*mcp.CallToolResult, ListFiltersetsOutput, error) {
			filtersets, err := service.ListFiltersets(ctx, core.ListFiltersetsOptions{Name: input.Name, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListFiltersetsOutput{}, err
			}
			return nil, filtersets, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_filterset",
			Title: "Get filterset",
			Description: "Read one filterset of the OpenSVC collector by its name or ID: its criteria in their order, the filtersets it includes, the nodes and services it selects, and what uses it. " +
				filtersetDescription +
				"It names at most 50 of the nodes and services it selects, among those the caller sees, and counts the users whose session filter it is. " +
				"The collector does not apply the logic of an included filterset: it chains the included criteria as if written in place, so a NOT before an included filterset is ignored and its criteria are not grouped; the nodes and services it gives, and what relies on the filterset, follow that, not the definition.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetFiltersetInput) (*mcp.CallToolResult, GetFiltersetOutput, error) {
			filterset, err := service.GetFilterset(ctx, input.Filterset)
			if err != nil {
				return nil, GetFiltersetOutput{}, err
			}
			return nil, filterset, nil
		},
	)
}

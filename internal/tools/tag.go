package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListTagsInput struct {
	Node    string `json:"node,omitempty" jsonschema:"optional exact node name or node ID whose tags to list"`
	Service string `json:"service,omitempty" jsonschema:"optional exact service name, such as ns/svc/name, or service ID whose tags to list"`
	Name    string `json:"name,omitempty" jsonschema:"optional text the tag name contains, case-insensitive; without node or service only"`
	Limit   int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListTagsOutput = core.TagList

func RegisterTagTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_tags",
			Title: "List tags",
			Description: "List the tags of the OpenSVC collector, the labels its users attach to nodes and services, or the tags of one node or one service. Sorted by name, each holds its name, creation date, the tags it excludes and its data, with node or service the data of the tag on it. " +
				"Tags are set by the collector users, with names and data each organization chooses; data is often JSON and untrusted text, cut to 1 KiB. Collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListTagsInput) (*mcp.CallToolResult, ListTagsOutput, error) {
			tags, err := service.ListTags(ctx, core.ListTagsOptions{Node: input.Node, Service: input.Service, Name: input.Name, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListTagsOutput{}, err
			}
			return nil, tags, nil
		},
	)
}

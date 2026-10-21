package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type CreateTagInput struct {
	Name    string  `json:"name" jsonschema:"the tag name, unique, case-insensitively"`
	Exclude *string `json:"exclude,omitempty" jsonschema:"optional regular expression of the tag names a node or service holding this tag cannot hold"`
	Data    *string `json:"data,omitempty" jsonschema:"optional data of the tag, as the collector stores it, often JSON text"`
}

type UpdateTagInput struct {
	Tag     string  `json:"tag" jsonschema:"one exact tag name, case-insensitive, or tag ID"`
	Name    *string `json:"name,omitempty" jsonschema:"optional new name"`
	Exclude *string `json:"exclude,omitempty" jsonschema:"optional regular expression of the tag names a node or service holding this tag cannot hold; an empty string clears it"`
	Data    *string `json:"data,omitempty" jsonschema:"optional data of the tag, as the collector stores it, often JSON text; an empty string clears it"`
}

type DeleteTagInput struct {
	Tag string `json:"tag" jsonschema:"one exact tag name, case-insensitive, or tag ID"`
}

type TagWriteOutput = core.TagWrite

// registerTagWriteTools registers the tools that create, change and delete
// tags. RegisterNodeActionTools calls it: they are registered only when
// actions are enabled.
func registerTagWriteTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "create_tag",
			Title: "Create tag",
			Description: "Create a tag in the OpenSVC collector, such as pci-dss, which filters, filtersets and contextual compliance rulesets may rely on, with optionally the regular expression of the tag names it cannot be attached along with, and data. " +
				"The collector gives it its tag ID; attach_node_tag and attach_service_tag attach it. A name another tag holds, whatever its case, is refused. Requires the TagManager or the Manager privilege.",
			Annotations: actionAnnotations(false, false),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CreateTagInput) (*mcp.CallToolResult, TagWriteOutput, error) {
			result, err := service.CreateTag(ctx, input.Name, input.Exclude, input.Data)
			if err != nil {
				return nil, TagWriteOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "update_tag",
			Title: "Update tag",
			Description: "Change the name, the exclusion pattern or the data of one tag of the OpenSVC collector, the fields left out kept: the nodes and services holding it keep it under its new name. " +
				"The exclusion pattern is the regular expression of the tag names a node or service holding this tag cannot hold; it applies to the attachments to come. A change leaving the tag as it was is told unchanged. Requires the TagManager or the Manager privilege.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateTagInput) (*mcp.CallToolResult, TagWriteOutput, error) {
			result, err := service.UpdateTag(ctx, input.Tag, core.TagFields{Name: input.Name, Exclude: input.Exclude, Data: input.Data})
			if err != nil {
				return nil, TagWriteOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:        "delete_tag",
			Title:       "Delete tag",
			Description: "Delete one tag of the OpenSVC collector, and detach it from every node and service holding it; the result gives the number of each. list_nodes and list_services with tag list them beforehand. Requires the TagManager or the Manager privilege.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteTagInput) (*mcp.CallToolResult, TagWriteOutput, error) {
			result, err := service.DeleteTag(ctx, input.Tag)
			if err != nil {
				return nil, TagWriteOutput{}, err
			}
			return nil, result, nil
		},
	)
}

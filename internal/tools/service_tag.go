package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// ServiceTagInput selects the services of a tag change: services, or the
// filters of list_services and count_services.
type ServiceTagInput struct {
	Tag      string   `json:"tag" jsonschema:"one exact tag name, case-insensitive, or tag ID"`
	Services []string `json:"services,omitempty" jsonschema:"optional exact service names or service IDs, at most 50; not with filters"`
	ServiceFiltersInput
	// FilterTag is the tag filter of list_services, whose name tag is the
	// tag changed here.
	FilterTag string `json:"filter_tag,omitempty" jsonschema:"optional exact tag name or tag ID; keeps only the services holding this tag, the tag filter of list_services; to detach a tag from every service holding it, pass the same tag; not with filterset, ruleset or moduleset"`
	ServiceInstanceScopeInput
}

func (input ServiceTagInput) options() core.ServiceTagOptions {
	filters := input.ServiceFiltersInput.core()
	filters.Tag = input.FilterTag
	selection := core.ServiceSelection{Services: input.Services, Filters: filters}
	input.ServiceInstanceScopeInput.apply(&selection)
	return core.ServiceTagOptions{Tag: input.Tag, ServiceSelection: selection}
}

type AttachServiceTagInput struct {
	ServiceTagInput
	AttachData *string `json:"attach_data,omitempty" jsonschema:"optional data of the attachment, as the collector stores it, often JSON text such as the fields of the form of a tag; an empty string clears the data of an attachment already there, which leaving it out keeps"`
}

type ServiceTagOutput = core.ServiceTagChange

// serviceTagSelection tells how a tag change selects its services.
var serviceTagSelection = serviceSelection("tag") +
	"A refusal may be a tag incompatible with the other tags of the service. Requires to be responsible for the services."

// registerServiceTagTools registers the tools that attach tags to services
// and detach them. RegisterNodeActionTools calls it: they are registered only
// when actions are enabled.
func registerServiceTagTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "attach_service_tag",
			Title: "Attach service tag",
			Description: "Attach one tag of the OpenSVC collector to one or several services, such as backup-nightly, which filters, filtersets and contextual compliance rulesets may rely on. A service already holding the tag is left as it is, its attach data updated when given. " +
				serviceTagSelection,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input AttachServiceTagInput) (*mcp.CallToolResult, ServiceTagOutput, error) {
			options := input.options()
			options.AttachData = input.AttachData
			result, err := service.AttachServiceTag(ctx, options)
			if err != nil {
				return nil, ServiceTagOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "detach_service_tag",
			Title: "Detach service tag",
			Description: "Detach one tag of the OpenSVC collector from one or several services, with its attach data: the filters, filtersets and contextual compliance rulesets relying on the tag no longer select them. A service not holding the tag is left as it is. " +
				serviceTagSelection,
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ServiceTagInput) (*mcp.CallToolResult, ServiceTagOutput, error) {
			result, err := service.DetachServiceTag(ctx, input.options())
			if err != nil {
				return nil, ServiceTagOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	return nil
}

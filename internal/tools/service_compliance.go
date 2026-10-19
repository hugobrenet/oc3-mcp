package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// ServiceSelectionInput selects the services of a compliance change:
// services, or the filters of list_services and count_services.
type ServiceSelectionInput struct {
	Services []string `json:"services,omitempty" jsonschema:"optional exact service names or service IDs, at most 50; not with filters"`
	ServiceFiltersInput
	Node string `json:"node,omitempty" jsonschema:"optional exact node name or node ID; keeps only the services with an instance on that node; not with tag, filterset, ruleset or moduleset"`
}

func (input ServiceSelectionInput) selection() core.ServiceSelection {
	return core.ServiceSelection{Services: input.Services, Filters: input.ServiceFiltersInput.core(), Node: input.Node}
}

type ServiceRulesetInput struct {
	Ruleset string `json:"ruleset" jsonschema:"one exact compliance ruleset name or ruleset ID"`
	ServiceSelectionInput
	// FilterRuleset is the ruleset filter of list_services, whose name
	// ruleset is the ruleset changed here.
	FilterRuleset string `json:"filter_ruleset,omitempty" jsonschema:"optional exact explicit ruleset name or ruleset ID; keeps only the services it is attached to, the ruleset filter of list_services and count_services; to detach a ruleset from every service holding it, pass the same ruleset; a contextual one is refused here: pass its services; not with tag, filterset, moduleset or node"`
}

func (input ServiceRulesetInput) selection() core.ServiceSelection {
	selection := input.ServiceSelectionInput.selection()
	selection.Filters.Ruleset = input.FilterRuleset
	return selection
}

type ServiceModulesetInput struct {
	Moduleset string `json:"moduleset" jsonschema:"one exact compliance moduleset name or moduleset ID"`
	ServiceSelectionInput
	// FilterModuleset is the moduleset filter of list_services, whose name
	// moduleset is the moduleset changed here.
	FilterModuleset string `json:"filter_moduleset,omitempty" jsonschema:"optional exact moduleset name or moduleset ID; keeps only the services it is attached to directly, the moduleset filter of list_services and count_services; to detach a moduleset from every service holding it, pass the same moduleset; not with tag, filterset, ruleset or node"`
}

func (input ServiceModulesetInput) selection() core.ServiceSelection {
	selection := input.ServiceSelectionInput.selection()
	selection.Filters.Moduleset = input.FilterModuleset
	return selection
}

type ServiceRulesetOutput = core.ServiceRulesetChange

type ServiceModulesetOutput = core.ServiceModulesetChange

// serviceComplianceSelection tells how a change of a ruleset or a moduleset,
// the kind, selects its services.
func serviceComplianceSelection(kind string) string {
	return "Pass the services, at most 50 exact names or IDs, or at least one of the filters of list_services and count_services, the " + kind + " filter given here as filter_" + kind + ", or node, to select up to 1000 services; a larger selection is refused, and nothing is changed. " +
		"An unknown name, or one several clusters hold, refuses the whole call, before anything is changed. The MCP sends one request per service: the change is not atomic, and the result gives the outcome on each service, with the reason of each refusal. " +
		"The change is that of the service itself, not of its encapsulated services. " +
		"Requires to be responsible for the services, by their app; the collector attaches only what is published to a group responsible for the app of the service, or to Everybody."
}

// registerServiceComplianceTools registers the tools that attach compliance
// rulesets and modulesets to services and detach them. RegisterNodeActionTools
// calls it: they are registered only when actions are enabled.
func registerServiceComplianceTools(registrar *Registrar, service *core.Service) error {
	for _, declared := range []struct {
		name, title, description string
		attach                   bool
	}{
		{"attach_service_ruleset", "Attach service ruleset", "Attach one explicit compliance ruleset to one or several services: its variables, the expected values the compliance modules check, apply to them, and a compliance fix would enforce them. A contextual ruleset applies through its filterset, and is refused. A service already holding the ruleset is left as it is. ", true},
		{"detach_service_ruleset", "Detach service ruleset", "Detach one compliance ruleset from one or several services: its variables no longer apply to them, unless a filterset or another ruleset brings them. A service not holding the ruleset is left as it is. ", false},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name: declared.name, Title: declared.title, Description: declared.description + serviceComplianceSelection("ruleset"),
				// An attachment is destructive too: it makes the agents
				// run modules, which may change the services, as an
				// autofix.
				Annotations: actionAnnotations(true, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input ServiceRulesetInput) (*mcp.CallToolResult, ServiceRulesetOutput, error) {
				options := core.ServiceRulesetOptions{Ruleset: input.Ruleset, ServiceSelection: input.selection()}
				change := service.DetachServiceRuleset
				if declared.attach {
					change = service.AttachServiceRuleset
				}
				result, err := change(ctx, options)
				if err != nil {
					return nil, ServiceRulesetOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	for _, declared := range []struct {
		name, title, description string
		attach                   bool
	}{
		{"attach_service_moduleset", "Attach service moduleset", "Attach one compliance moduleset to one or several services: the agent runs its modules, and those of the modulesets it includes, at each compliance check of the service, and a compliance fix lets them change it. A service already holding the moduleset is left as it is. ", true},
		{"detach_service_moduleset", "Detach service moduleset", "Detach one compliance moduleset from one or several services: the agent no longer runs its modules for them, unless another moduleset brings them. A service not holding the moduleset is left as it is. ", false},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name: declared.name, Title: declared.title, Description: declared.description + serviceComplianceSelection("moduleset"),
				// An attachment is destructive too: it makes the agents
				// run modules, which may change the services, as an
				// autofix.
				Annotations: actionAnnotations(true, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input ServiceModulesetInput) (*mcp.CallToolResult, ServiceModulesetOutput, error) {
				options := core.ServiceModulesetOptions{Moduleset: input.Moduleset, ServiceSelection: input.selection()}
				change := service.DetachServiceModuleset
				if declared.attach {
					change = service.AttachServiceModuleset
				}
				result, err := change(ctx, options)
				if err != nil {
					return nil, ServiceModulesetOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	return nil
}

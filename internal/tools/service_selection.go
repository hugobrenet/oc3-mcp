package tools

import (
	"github.com/opensvc/oc3-mcp/internal/core"
)

// ServiceInstanceScopeInput keeps the services of a change by their
// instances: on a node, on the nodes of the filters of list_nodes, or in a
// state.
type ServiceInstanceScopeInput struct {
	Node                 string            `json:"node,omitempty" jsonschema:"optional exact node name or node ID; keeps only the services with an instance on that node; not with on_nodes"`
	OnNodes              *NodeFiltersInput `json:"on_nodes,omitempty" jsonschema:"optional object of filters of list_nodes, such as status In service and team DBA; keeps only the services with an instance on the nodes they select, up to 1000 nodes; not with node"`
	InstanceAvailability string            `json:"instance_availability,omitempty" jsonschema:"optional exact availability status of an instance, such as up, down, warn or n/a; keeps only the services with an instance in it, on the node or the nodes of on_nodes when given"`
	InstanceOverall      string            `json:"instance_overall,omitempty" jsonschema:"optional exact overall status of an instance, such as up, down or warn; keeps only the services with an instance in it"`
	InstanceFrozen       bool              `json:"instance_frozen,omitempty" jsonschema:"optional; true keeps only the services with a frozen instance"`
}

// apply sets the instance filters of selection.
func (input ServiceInstanceScopeInput) apply(selection *core.ServiceSelection) {
	selection.Node = input.Node
	if input.OnNodes != nil {
		filters := input.OnNodes.core()
		selection.OnNodes = &filters
	}
	selection.Instance = core.InstanceState{Availability: input.InstanceAvailability, Overall: input.InstanceOverall, Frozen: input.InstanceFrozen}
}

// ServiceSelectionInput selects the services of a change: services, or the
// filters of list_services and count_services and those of the instances.
type ServiceSelectionInput struct {
	Services []string `json:"services,omitempty" jsonschema:"optional exact service names or service IDs, at most 50; not with filters"`
	ServiceFiltersInput
	ServiceInstanceScopeInput
}

func (input ServiceSelectionInput) selection() core.ServiceSelection {
	selection := core.ServiceSelection{Services: input.Services, Filters: input.ServiceFiltersInput.core()}
	input.ServiceInstanceScopeInput.apply(&selection)
	return selection
}

// serviceSelection tells how a change selects its services; filter names
// the filter of list_services given under another name, when one is.
func serviceSelection(filter string) string {
	renamed := ""
	if filter != "" {
		renamed = " the " + filter + " filter given here as filter_" + filter + ","
	}
	return "Select the services in this call, without listing them first: pass the services, at most 50 exact names or IDs, or at least one filter: those of list_services and count_services," + renamed +
		" node, or on_nodes, an object of filters of list_nodes, for the services with an instance on the nodes they select, and instance_availability, instance_overall or instance_frozen for the services with an instance in that state; they combine. " +
		"For example, the services hosted on the nodes in maintenance: on_nodes {status: Maintenance}; the production services with an instance down on the nodes of team DBA: env PRD, instance_availability down and on_nodes {team: DBA}. " +
		"A service is selected when one of its instances matches, active or not. Up to 1000 services are selected; a larger selection is refused, and nothing is changed. " +
		"An unknown name, or one several clusters hold, refuses the whole call, before anything is changed. The MCP sends one request per service: the change is not atomic, and the result gives the outcome on each service, with the reason of each refusal. "
}

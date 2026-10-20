package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// The actions on services take the services named, or those the filters
// select, node alone selecting every service of the node: the agent asks the
// user to confirm the call, its destructive annotation telling it to, and
// the user decides.

// ActionFiltersInput are the filters of the services of an action: those of
// list_services, and of their instances.
type ActionFiltersInput struct {
	ServiceFiltersInput
	OnNodes              *NodeFiltersInput `json:"on_nodes,omitempty" jsonschema:"optional object of filters of list_nodes, such as status Maintenance: the services with an instance on the nodes they select, up to 1000 nodes; not with node"`
	InstanceAvailability string            `json:"instance_availability,omitempty" jsonschema:"optional exact availability status of an instance, such as up or down: the services with an instance in it, on node or the nodes of on_nodes when given"`
	InstanceOverall      string            `json:"instance_overall,omitempty" jsonschema:"optional exact overall status of an instance, such as up, down or warn: the services with an instance in it"`
	InstanceFrozen       bool              `json:"instance_frozen,omitempty" jsonschema:"optional; true keeps the services with a frozen instance"`
}

func (input ActionFiltersInput) selection(services []string, node string) core.ServiceSelection {
	selection := core.ServiceSelection{Services: services, Filters: input.ServiceFiltersInput.core(), Node: node}
	if input.OnNodes != nil {
		filters := input.OnNodes.core()
		selection.OnNodes = &filters
	}
	selection.Instance = core.InstanceState{Availability: input.InstanceAvailability, Overall: input.InstanceOverall, Frozen: input.InstanceFrozen}
	return selection
}

// ServiceActionTargetsInput selects the services of an action: the services
// named, or those the filters select.
type ServiceActionTargetsInput struct {
	Services []string `json:"services,omitempty" jsonschema:"optional exact service names or service IDs, at most 150: those services only, which the filters narrow; without, the services the filters, node or on_nodes select"`
	Node     string   `json:"node,omitempty" jsonschema:"optional exact node name or node ID: the services with an instance on that node, every one of them without services; with services, those services on that node"`
	ActionFiltersInput
}

func (input ServiceActionTargetsInput) targets() core.ServiceSelection {
	return input.ActionFiltersInput.selection(input.Services, input.Node)
}

// ServiceOrInstanceActionInput selects the services of an action on whole
// services, or with local on their instances.
type ServiceOrInstanceActionInput struct {
	ServiceActionTargetsInput
	Local bool `json:"local,omitempty" jsonschema:"optional; true acts on the instances on node or on the nodes of on_nodes, each run by its node, instead of on whole services"`
}

// TakeoverActionInput selects the services a takeover moves to the node
// to.
type TakeoverActionInput struct {
	To string `json:"to" jsonschema:"exact node name or node ID: the destination, where the services have a provisioned instance"`
	ServiceActionTargetsInput
}

type ServiceAgentActionOutput = core.ServiceAgentActionQueueing

// serviceAgentSelection tells how an action selects its services.
const serviceAgentSelection = "Select the services by services, exact names or IDs, at most 150, or by the filters of list_services, node, on_nodes, an object of filters of list_nodes, and the instance states; they combine. " +
	"node alone selects every service with an instance on the node, and services with node those services on it: for one service on one node, pass both. A call never selects every service: pass services or at least one filter. " +
	"A service named that the filters leave out is refused with the reason. Up to 200 services or instances are selected; a larger selection is refused, and nothing is queued. " +
	"An unknown name, or one several clusters hold, refuses the whole call, before anything is queued. The MCP sends one request per service or instance: the queueing is not atomic, and the result gives the outcome on each, with the reason of each refusal. "

// serviceAgentActionFollowUp tells how an action queued on services runs.
const serviceAgentActionFollowUp = "The collector queues it for a node of each service seen in the last 15 minutes, or with local for the node of each instance; the agent runs it as om svc -s <service> <action> when it reads its queue, within seconds. " +
	"With local, an instance on a node for which the collector holds no agent version of 1.9 or later is refused: the collector would queue the action without --local, on the whole service. " +
	"Returns as soon as each action is queued, with its action_id: follow it with list_node_actions and its service filter. Done with exit code 0 means the agent accepted the action, an orchestration then going on; get_service tells the state of the instances, the collector receiving it 10 to 20 seconds later. Cancel an action before an agent takes it with cancel_node_action. "

const serviceAgentActionPrivilege = "Requires the NodeExec privilege and to be responsible for the services, by their app."

// startAfterStop warns of the om3 daemons choosing the node of a start right
// after a stop of the whole service on the view they have of their peers.
const startAfterStop = "Right after a stop of the whole service, the daemons may choose another node than the preferred one, or two: check the placement with get_service. "

// registerServiceAgentActionTools registers the tools that queue agent
// actions on services. RegisterNodeActionTools calls it: they are
// registered only when actions are enabled.
func registerServiceAgentActionTools(registrar *Registrar, service *core.Service) error {
	queueTo := func(ctx context.Context, action string, local bool, destination string, selection core.ServiceSelection) (*mcp.CallToolResult, ServiceAgentActionOutput, error) {
		result, err := service.QueueServiceAgentAction(ctx, core.ServiceAgentActionOptions{Action: action, Local: local, Destination: destination, ServiceSelection: selection})
		if err != nil {
			return nil, ServiceAgentActionOutput{}, err
		}
		return nil, result, nil
	}
	queue := func(ctx context.Context, action string, local bool, selection core.ServiceSelection) (*mcp.CallToolResult, ServiceAgentActionOutput, error) {
		return queueTo(ctx, action, local, "", selection)
	}
	for _, declared := range []struct {
		name, title, action, description string
		destructive                      bool
	}{
		{"freeze_service", "Freeze service", "freeze", "Freeze OpenSVC services through their agents: the daemons no longer start, stop, move or restart them on their own, until thawed; running instances keep running. With local, only their instances on the nodes given. ", false},
		{"thaw_service", "Thaw service", "thaw", "Thaw OpenSVC services through their agents: the daemons resume their automatic actions, and may then start, move or restart instances at once to reach their placement. With local, only their instances on the nodes given. ", true},
		{"start_service", "Start service", "start", "Start OpenSVC services through their agents: the daemons start each where its placement says. With local, their instances on the nodes given, each started by its node. " + startAfterStop, false},
		{"stop_service", "Stop service", "stop", "Stop OpenSVC services through their agents: the daemons stop every instance and keep them down, starting none on their own until a start. With local, only their instances on the nodes given. ", true},
	} {
		declared := declared
		if err := addTool(registrar, &mcp.Tool{
			Name: declared.name, Title: declared.title,
			Description: declared.description + serviceAgentSelection + serviceAgentActionFollowUp + serviceAgentActionPrivilege,
			Annotations: actionAnnotations(declared.destructive, true),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input ServiceOrInstanceActionInput) (*mcp.CallToolResult, ServiceAgentActionOutput, error) {
			return queue(ctx, declared.action, input.Local, input.targets())
		}); err != nil {
			return err
		}
	}
	for _, declared := range []struct {
		name, title, action, description string
		idempotent                       bool
	}{
		{"switch_service", "Switch service", "switch", "Switch failover OpenSVC services to the next preferred node, through their agents: the daemons stop the running instance and start the service on the next node whose instance is provisioned. A flex service is refused, its agents choosing no destination: use takeover_service with the destination node. Each call moves the services again. ", false},
		{"giveback_service", "Give service back", "giveback", "Give OpenSVC services back to their preferred nodes, through their agents: the daemons stop the instances running elsewhere and start the services where their placement prefers. " + startAfterStop, true},
		{"abort_service_action", "Abort service action", "abort", "Abort the orchestration going on for OpenSVC services, through their agents, such as a start, a stop or a move stuck in progress: the daemons stop pursuing it, and leave the instances as they are. ", true},
	} {
		declared := declared
		if err := addTool(registrar, &mcp.Tool{
			Name: declared.name, Title: declared.title,
			Description: declared.description + "It acts on whole services. " + serviceAgentSelection + serviceAgentActionFollowUp + serviceAgentActionPrivilege,
			Annotations: actionAnnotations(true, declared.idempotent),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input ServiceActionTargetsInput) (*mcp.CallToolResult, ServiceAgentActionOutput, error) {
			return queue(ctx, declared.action, false, input.targets())
		}); err != nil {
			return err
		}
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "restart_service", Title: "Restart service",
		Description: "Restart the instances of OpenSVC services on the nodes given, through their agents: each node stops and starts its instances, again at each call. Pass node or on_nodes, and services for some of their services only. " +
			serviceAgentSelection + serviceAgentActionFollowUp + serviceAgentActionPrivilege,
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ServiceActionTargetsInput) (*mcp.CallToolResult, ServiceAgentActionOutput, error) {
		return queue(ctx, "restart", true, input.targets())
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "takeover_service", Title: "Take service over",
		Description: "Move OpenSVC services to one node, through the agent of that node: it stops the instances running elsewhere and starts its own, as om svc switch --to that node. " +
			"Pass to, the destination, and the services to move: services, or the filters, such as node for the services with an instance on a node, and instance_availability up for those running there; node dev2n1 and to dev2n2 move the services of dev2n1 to dev2n2. " +
			"A service without instance on the destination is refused before anything is queued, a failover service already running there is left as it is, and the agent refuses a destination whose instance is not provisioned, its action then done with exit code 1. " +
			serviceAgentSelection + serviceAgentActionFollowUp + serviceAgentActionPrivilege,
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input TakeoverActionInput) (*mcp.CallToolResult, ServiceAgentActionOutput, error) {
		return queueTo(ctx, "takeover", true, input.To, input.targets())
	})
}

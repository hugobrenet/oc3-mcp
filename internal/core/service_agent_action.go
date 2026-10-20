package core

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const (
	// maxServiceAgentActions bounds the actions one call queues for the
	// services named: they stop, start or move services.
	maxServiceAgentActions = 200
	// maxServiceAgentNamed bounds the services one call names.
	maxServiceAgentNamed = 150
)

// serviceAgentAction is what an agent action on services acts on.
type serviceAgentAction struct {
	// service and instance tell whether the action acts on whole services,
	// run by a node of each the collector chooses, and on instances, run by
	// their node with --local.
	service, instance bool
	// failover keeps the action to the failover services.
	failover bool
	// destination is an action on the instance of the services selected
	// on one node, their destination.
	destination bool
	// local is an action the collector queues with --local on an
	// instance, when the agent version of its node is 1.9 or later, and
	// bare else: the agent then acts on the whole service.
	local bool
}

// serviceAgentActions are the agent actions on services the MCP queues, as
// an om3 agent runs them: om svc -s <service> <action>, with --local on an
// instance. The collector also takes push resinfo, push config, syncall,
// syncnodes and syncdrp, which om3 does not know, and adds --local to abort
// and giveback on an instance, which om3 refuses: those are left out.
var serviceAgentActions = map[string]serviceAgentAction{
	"freeze":   {service: true, instance: true, local: true},
	"thaw":     {service: true, instance: true, local: true},
	"start":    {service: true, instance: true, local: true},
	"stop":     {service: true, instance: true, local: true},
	"restart":  {instance: true, local: true},
	"switch":   {service: true, failover: true},
	"giveback": {service: true},
	"takeover": {instance: true, destination: true},
	"abort":    {service: true},
}

// ServiceAgentActionOptions queue an agent action on the selected services,
// or with Local on their selected instances.
type ServiceAgentActionOptions struct {
	Action string
	Local  bool
	// Destination is the node a takeover moves the services selected to.
	Destination string
	ServiceSelection
}

type ServiceAgentActionQueueing struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Action     string         `json:"action" jsonschema:"the agent action queued"`
	Local      bool           `json:"local" jsonschema:"true when the action acts on instances, each run by its node, false on whole services"`
	To         *NodeReference `json:"to,omitempty" jsonschema:"with takeover_service, the destination node"`
	ServiceSelectionScope
	Selected  int                         `json:"selected" jsonschema:"the number of services, or of instances with local, selected"`
	Queued    int                         `json:"queued" jsonschema:"the number of actions queued"`
	Unchanged int                         `json:"unchanged" jsonschema:"the number left as they were, such as a takeover of a failover service already running on its destination"`
	Refused   int                         `json:"refused" jsonschema:"the number not queued: refused by the MCP or the collector, or not attempted when the call ended before"`
	Actions   []ServiceAgentActionOutcome `json:"actions" jsonschema:"the outcome on each service or instance, sorted by service name then node name"`
	FollowUp  string                      `json:"follow_up,omitempty" jsonschema:"how to tell whether the queued actions ran, and their result"`
}

type ServiceAgentActionOutcome struct {
	SvcID     string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname   string `json:"svcname" jsonschema:"the service name"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
	NodeID    string `json:"node_id,omitempty" jsonschema:"with local, the node of the instance; else the node the collector queued the action for"`
	Nodename  string `json:"nodename,omitempty" jsonschema:"the name of that node"`
	Outcome   string `json:"outcome" jsonschema:"queued, unchanged when already as asked, refused, or not_attempted when the call ended before"`
	Reason    string `json:"reason,omitempty" jsonschema:"with refused, why"`
	ActionID  int    `json:"action_id,omitempty" jsonschema:"with queued, the ID of the action in the queue, which list_node_actions follows"`
	Command   string `json:"command,omitempty" jsonschema:"with queued, the command the agent runs after om svc -s <service>"`
	QueuedFor string `json:"queued_for,omitempty" jsonschema:"with queued, how the node reads its queue: feed, read by an om3 agent; pull or push, the means of the former agent"`
	Warning   string `json:"warning,omitempty" jsonschema:"with queued, why the action may never run; tell the user"`
}

// serviceAgentTarget is a service, or with local an instance, an action is
// queued for.
type serviceAgentTarget struct {
	service        selectedService
	node, nodename string
	// availability and agentVersion are those of the instance and its node.
	availability, agentVersion string
	// missing is a service named with no instance selected: refused.
	missing string
}

// QueueServiceAgentAction queues an agent action on the selected services,
// or with Local on their instances on the nodes selected, one request each:
// the collector has no bulk route, and the queueing is not atomic. Each
// agent runs it when it reads its queue: the result tells what was queued,
// list_node_actions what ran.
func (s *Service) QueueServiceAgentAction(ctx context.Context, options ServiceAgentActionOptions) (ServiceAgentActionQueueing, error) {
	spec, ok := serviceAgentActions[options.Action]
	if !ok {
		return ServiceAgentActionQueueing{}, fmt.Errorf("unsupported service action %q", options.Action)
	}
	local := options.Local || !spec.service
	selection := options.ServiceSelection
	switch {
	case local && !spec.instance:
		return ServiceAgentActionQueueing{}, fmt.Errorf("%s acts on whole services: pass local false", options.Action)
	case spec.destination && options.Destination == "":
		return ServiceAgentActionQueueing{}, fmt.Errorf("%s moves the services selected to one node: pass to, the destination", options.Action)
	case !spec.destination && options.Destination != "":
		return ServiceAgentActionQueueing{}, fmt.Errorf("to is the destination of takeover_service only")
	case local && !spec.destination && selection.Node == "" && selection.OnNodes == nil:
		return ServiceAgentActionQueueing{}, fmt.Errorf("%s acts on instances: pass node or on_nodes, the nodes of the instances", options.Action)
	}
	// The services named, or those the filters select, a node alone
	// selecting every service of the node: the confirmation of the agent
	// shows the call, and the user decides. Never every service.
	named := selection.Services
	selection.Services = nil
	narrowed := selection.byInstance() || !reflect.DeepEqual(selection.Filters, ServiceFilters{})
	switch {
	case len(named) == 0 && !narrowed:
		return ServiceAgentActionQueueing{}, fmt.Errorf("pass services, the exact names or IDs, or at least one filter, node or on_nodes to select them; a call never selects every service")
	case len(named) > maxServiceAgentNamed:
		return ServiceAgentActionQueueing{}, fmt.Errorf("pass at most %d services; select more services with filters", maxServiceAgentNamed)
	}
	if narrowed {
		if err := checkServiceSelection(selection); err != nil {
			return ServiceAgentActionQueueing{}, err
		}
	}
	var resolved []selectedService
	var only []string
	if len(named) > 0 {
		var err error
		if resolved, err = s.resolveServices(ctx, named); err != nil {
			return ServiceAgentActionQueueing{}, err
		}
		ids := make([]string, 0, len(resolved))
		for _, service := range resolved {
			ids = append(ids, service.SvcID)
		}
		only = []string{"svc_id:in:" + strings.Join(ids, ",")}
	}
	result := ServiceAgentActionQueueing{Action: options.Action, Local: local, Actions: []ServiceAgentActionOutcome{}}
	var targets []serviceAgentTarget
	found := map[string]bool{}
	switch {
	case local && !spec.destination:
		instances, err := s.readSelectionInstances(ctx, selection, &result.ServiceSelectionScope, only)
		if err != nil {
			return ServiceAgentActionQueueing{}, err
		}
		for _, instance := range instances {
			found[instance.SvcID] = true
			targets = append(targets, serviceAgentTarget{
				service: instance.selectedService, node: instance.NodeID, nodename: instance.Nodename,
				availability: instance.Availability, agentVersion: instance.AgentVersion,
			})
		}
	case narrowed:
		services, err := s.readSelection(ctx, selection, &result.ServiceSelectionScope, only)
		if err != nil {
			return ServiceAgentActionQueueing{}, err
		}
		for _, service := range services {
			found[service.SvcID] = true
			targets = append(targets, serviceAgentTarget{service: service})
		}
	default:
		for _, service := range resolved {
			found[service.SvcID] = true
			targets = append(targets, serviceAgentTarget{service: service})
		}
	}
	// A service named that the rest of the selection leaves out is told,
	// not dropped.
	for _, service := range resolved {
		if found[service.SvcID] {
			continue
		}
		reason := fmt.Sprintf("service %s does not match the filters given: nothing was queued for it", service.Svcname)
		if local && !spec.destination {
			where := "on the nodes on_nodes selected"
			if result.Node != nil {
				where = "on node " + result.Node.Nodename
			}
			reason = fmt.Sprintf("service %s has no instance %s, with the filters given: nothing was queued for it", service.Svcname, where)
		}
		targets = append(targets, serviceAgentTarget{service: service, missing: reason})
	}
	// A takeover acts on the instances of the services selected on the
	// destination: a service without instance there is told, the
	// destination being wrong for it.
	if spec.destination {
		var err error
		if targets, err = s.takeoverTargets(ctx, options.Destination, targets, &result); err != nil {
			return ServiceAgentActionQueueing{}, err
		}
	}
	if len(targets) > maxServiceAgentActions {
		return ServiceAgentActionQueueing{}, fmt.Errorf("the selection holds %d %s, more than %d: nothing was queued; narrow it", len(targets), map[bool]string{false: "services", true: "instances"}[local], maxServiceAgentActions)
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].service.Svcname != targets[j].service.Svcname {
			return targets[i].service.Svcname < targets[j].service.Svcname
		}
		return targets[i].nodename < targets[j].nodename
	})

	outcomes := make([]ServiceAgentActionOutcome, len(targets))
	forEachSelected(ctx, len(targets), func(index int) {
		outcomes[index] = s.queueServiceAgentAction(ctx, options.Action, spec, targets[index])
	}, func(index int) {
		target := targets[index]
		outcomes[index] = ServiceAgentActionOutcome{SvcID: target.service.SvcID, Svcname: target.service.Svcname, ClusterID: target.service.ClusterID, NodeID: target.node, Nodename: target.nodename, Outcome: ChangeNotAttempted}
	})
	services := map[string]string{}
	for _, outcome := range outcomes {
		switch outcome.Outcome {
		case ChangeQueued:
			result.Queued++
			services[outcome.SvcID] = outcome.Svcname
		case ChangeUnchanged:
			result.Unchanged++
		default:
			result.Refused++
		}
	}
	result.Selected, result.Actions = len(targets), outcomes
	if result.Queued > 0 {
		// The actions of one service are followed by its name, those of
		// several by the command, which a restart also holds.
		filter := fmt.Sprintf("command %q", options.Action)
		if len(services) == 1 {
			for _, name := range services {
				filter = fmt.Sprintf("service %q", name)
			}
		}
		result.FollowUp = fmt.Sprintf("list_node_actions with %s: an agent takes the action within seconds; done with exit code 0 means the agent accepted it, an orchestration of start, stop, switch, giveback or takeover then going on; get_service gives the state of the instances, which the collector receives 10 to 20 seconds later", filter)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// takeoverTargets gives, for the services of targets, their instances on the
// destination node, and a refusal for those without.
func (s *Service) takeoverTargets(ctx context.Context, destination string, targets []serviceAgentTarget, result *ServiceAgentActionQueueing) ([]serviceAgentTarget, error) {
	node, err := s.lookupNode(ctx, destination, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return nil, err
	}
	result.To = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
	var ids []string
	for _, target := range targets {
		if target.missing == "" {
			ids = append(ids, target.service.SvcID)
		}
	}
	there := map[string]selectedInstance{}
	if len(ids) > 0 {
		instances, err := s.selectInstances(ctx, []string{filterEquals("node_id", node.NodeID), "svc_id:in:" + strings.Join(ids, ",")}, maxSelectionInstances)
		if err != nil {
			return nil, err
		}
		for _, instance := range instances {
			there[instance.SvcID] = instance
		}
	}
	moved := make([]serviceAgentTarget, 0, len(targets))
	for _, target := range targets {
		instance, ok := there[target.service.SvcID]
		switch {
		case target.missing != "":
			moved = append(moved, target)
		case !ok:
			moved = append(moved, serviceAgentTarget{service: target.service, node: node.NodeID, nodename: node.Nodename,
				missing: fmt.Sprintf("service %s has no instance on node %s, the destination: nothing was queued for it", target.service.Svcname, node.Nodename)})
		default:
			moved = append(moved, serviceAgentTarget{service: instance.selectedService, node: instance.NodeID, nodename: instance.Nodename,
				availability: instance.Availability, agentVersion: instance.AgentVersion})
		}
	}
	return moved, nil
}

// queueServiceAgentAction queues the action for one service or instance.
func (s *Service) queueServiceAgentAction(ctx context.Context, action string, spec serviceAgentAction, target serviceAgentTarget) ServiceAgentActionOutcome {
	outcome := ServiceAgentActionOutcome{SvcID: target.service.SvcID, Svcname: target.service.Svcname, ClusterID: target.service.ClusterID, NodeID: target.node, Nodename: target.nodename, Outcome: ChangeRefused}
	if target.missing != "" {
		outcome.Reason = target.missing
		return outcome
	}
	// The collector adds --local for an agent of version 1.9 or later, as
	// pushasset reports it: without, the bare action would act on the whole
	// service.
	if target.node != "" && spec.local && !agentAtLeast19(target.agentVersion) {
		outcome.Reason = fmt.Sprintf("the collector holds no agent version 1.9 or later for node %s, which pushasset reports, and would queue the action without --local, on the whole service: nothing was queued for it; queue_node_action pushasset on the node first", target.nodename)
		return outcome
	}
	// A failover service running on the destination already stays there.
	if spec.destination && target.service.Topology == "failover" && target.availability == "up" {
		outcome.Outcome, outcome.Reason = ChangeUnchanged, fmt.Sprintf("service %s already runs on node %s", target.service.Svcname, target.nodename)
		return outcome
	}
	// The agent switches a failover service to the next node it chooses: it
	// chooses none for a flex service, whose destination the collector does
	// not pass.
	if spec.failover && target.service.Topology != "failover" {
		outcome.Reason = fmt.Sprintf("switch moves a failover service to the next preferred node, and service %s is %s: use takeover_service with the destination node", target.service.Svcname, target.service.Topology)
		return outcome
	}
	body := map[string]any{"svc_id": target.service.SvcID, "action": action}
	if target.node != "" {
		body["node_id"] = target.node
	}
	var response struct {
		Data []struct {
			ID      any    `json:"id"`
			Type    string `json:"action_type"`
			Command string `json:"command"`
			NodeID  string `json:"node_id"`
		} `json:"data"`
	}
	if err := s.client.PutJSON(ctx, "/actions", body, &response); err != nil {
		outcome.Reason = err.Error()
		return outcome
	}
	outcome.Outcome = ChangeQueued
	if len(response.Data) == 1 {
		queued := response.Data[0]
		outcome.ActionID, _ = strconv.Atoi(anyText(queued.ID))
		outcome.QueuedFor, outcome.Command = queued.Type, queued.Command
		if outcome.NodeID == "" {
			outcome.NodeID = queued.NodeID
		}
	}
	outcome.Warning = queueWarning(outcome.QueuedFor)
	if target.node != "" && spec.local && outcome.Command != "" && !strings.Contains(outcome.Command, "--local") {
		outcome.Warning = strings.TrimSpace(outcome.Warning + " queued without --local: the agent acts on the whole service, not only its instance on this node")
	}
	return outcome
}

// agentAtLeast19 tells an agent version of 1.9 or later, as the collector
// reads it to add --local: the leading digits, a leading v skipped, so that
// v3.0.0 reads 3.0.
func agentAtLeast19(version string) bool {
	version = strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V")
	if len(version) < 3 {
		return false
	}
	number, err := strconv.ParseFloat(version[:3], 64)
	return err == nil && number >= 1.9
}

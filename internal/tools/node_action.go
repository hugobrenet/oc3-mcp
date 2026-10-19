package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// NodePropertiesInput are the properties of a node the action tools set. A
// property left out is not changed.
type NodePropertiesInput struct {
	App             *string `json:"app,omitempty" jsonschema:"the exact app code of the node; it must be an app of the collector; never empty"`
	TeamResponsible *string `json:"team_responsible,omitempty" jsonschema:"the exact name of the group responsible for the node, which may manage it; it must be a group of the collector; never empty"`
	Status          *string `json:"status,omitempty" jsonschema:"the status of the node, a free text the organization defines, such as In service or Commissioning"`
	Role            *string `json:"role,omitempty" jsonschema:"the role of the node, a free text"`
	Assetname       *string `json:"assetname,omitempty" jsonschema:"the asset name of the node in the organization inventory"`
	WarrantyEnd     *string `json:"warranty_end,omitempty" jsonschema:"the end date of the hardware warranty, YYYY-MM-DD"`
	MaintenanceEnd  *string `json:"maintenance_end,omitempty" jsonschema:"the end date of the maintenance contract, YYYY-MM-DD"`
	AssetEnv        *string `json:"asset_env,omitempty" jsonschema:"the asset environment, such as PRD or DEV; the agent rewrites it when the configuration of its node sets it"`
	SecZone         *string `json:"sec_zone,omitempty" jsonschema:"the security zone; the agent rewrites it when the configuration of its node sets it"`
	TeamInteg       *string `json:"team_integ,omitempty" jsonschema:"the integration team; the agent rewrites it when the configuration of its node sets it"`
	TeamSupport     *string `json:"team_support,omitempty" jsonschema:"the support team; the agent rewrites it when the configuration of its node sets it"`
	LocCountry      *string `json:"loc_country,omitempty" jsonschema:"the country of the node; the agent rewrites the location when the configuration of its node sets it"`
	LocCity         *string `json:"loc_city,omitempty" jsonschema:"the city"`
	LocAddr         *string `json:"loc_addr,omitempty" jsonschema:"the street address"`
	LocZip          *string `json:"loc_zip,omitempty" jsonschema:"the postal code"`
	LocBuilding     *string `json:"loc_building,omitempty" jsonschema:"the building"`
	LocFloor        *string `json:"loc_floor,omitempty" jsonschema:"the floor"`
	LocRoom         *string `json:"loc_room,omitempty" jsonschema:"the room"`
	LocRack         *string `json:"loc_rack,omitempty" jsonschema:"the rack"`
}

func (input NodePropertiesInput) properties() map[string]string {
	properties := map[string]string{}
	for name, value := range map[string]*string{
		"app": input.App, "team_responsible": input.TeamResponsible, "status": input.Status, "role": input.Role, "assetname": input.Assetname,
		"warranty_end": input.WarrantyEnd, "maintenance_end": input.MaintenanceEnd, "asset_env": input.AssetEnv, "sec_zone": input.SecZone,
		"team_integ": input.TeamInteg, "team_support": input.TeamSupport, "loc_country": input.LocCountry, "loc_city": input.LocCity,
		"loc_addr": input.LocAddr, "loc_zip": input.LocZip, "loc_building": input.LocBuilding, "loc_floor": input.LocFloor,
		"loc_room": input.LocRoom, "loc_rack": input.LocRack,
	} {
		if value != nil {
			properties[name] = *value
		}
	}
	return properties
}

type CreateNodeInput struct {
	Nodename string `json:"nodename" jsonschema:"the name of the node to create, without spaces or slashes; a name a node already has is refused"`
	NodePropertiesInput
}

type CreateNodeOutput = core.NodeCreation

type UpdateNodeInput struct {
	Node string `json:"node" jsonschema:"one exact node name or collector node ID"`
	NodePropertiesInput
}

type UpdateNodeOutput = core.NodeUpdate

type DeleteNodeInput struct {
	Node string `json:"node" jsonschema:"one exact node name or collector node ID"`
}

type DeleteNodeOutput = core.NodeDeletion

// NodeTagInput selects the nodes of a tag change: nodes, or the filters of
// list_nodes and count_nodes.
type NodeTagInput struct {
	Tag   string   `json:"tag" jsonschema:"one exact tag name, case-insensitive, or tag ID"`
	Nodes []string `json:"nodes,omitempty" jsonschema:"optional exact node names or node IDs, at most 50; not with filters"`
	NodeFiltersInput
	// FilterTag is the tag filter of list_nodes, whose name tag is the tag
	// changed here.
	FilterTag string `json:"filter_tag,omitempty" jsonschema:"optional exact tag name or tag ID; keeps only the nodes holding this tag, the tag filter of list_nodes and count_nodes; to detach a tag from every node holding it, pass the same tag; not with filterset"`
}

func (input NodeTagInput) options() core.NodeTagOptions {
	filters := input.NodeFiltersInput.core()
	filters.Tag = input.FilterTag
	return core.NodeTagOptions{Tag: input.Tag, NodeSelection: core.NodeSelection{Nodes: input.Nodes, Filters: filters}}
}

type AttachNodeTagInput struct {
	NodeTagInput
	AttachData *string `json:"attach_data,omitempty" jsonschema:"optional data of the attachment, such as the fields of the form of a tag, as JSON text"`
}

type NodeTagOutput = core.NodeTagChange

// NodeSelectionInput selects the nodes of a compliance change: nodes, or
// the filters of list_nodes and count_nodes.
type NodeSelectionInput struct {
	Nodes []string `json:"nodes,omitempty" jsonschema:"optional exact node names or node IDs, at most 50; not with filters"`
	NodeFiltersInput
}

func (input NodeSelectionInput) selection() core.NodeSelection {
	return core.NodeSelection{Nodes: input.Nodes, Filters: input.NodeFiltersInput.core()}
}

type NodeRulesetInput struct {
	Ruleset string `json:"ruleset" jsonschema:"one exact compliance ruleset name or ruleset ID"`
	NodeSelectionInput
}

type NodeModulesetInput struct {
	Moduleset string `json:"moduleset" jsonschema:"one exact compliance moduleset name or moduleset ID"`
	NodeSelectionInput
}

type NodeRulesetOutput = core.NodeRulesetChange

type QueueNodeActionInput struct {
	Action string `json:"action" jsonschema:"the agent action: pushasset, pushdisks, pushpkg, pushstats, checks, sysreport or compliance_check"`
	NodeSelectionInput
}

type QueueNodeActionOutput = core.NodeActionQueueing

type CancelNodeActionInput struct {
	ActionIDs []int `json:"action_ids" jsonschema:"the IDs of the actions to cancel, 1 to 50, as list_node_actions and the action tools give them"`
}

type CancelNodeActionOutput = core.NodeActionCancellation

// nodeActionFollowUp tells how an action queued on nodes runs and how to
// follow it.
const nodeActionFollowUp = "The collector queues it on each node; the agent runs it when it reads its queue. Returns as soon as each action is queued, with its action_id: follow it with list_node_actions, and cancel it before an agent takes it with cancel_node_action. " +
	"Only an action queued for feed reaches an om3 agent: a warning tells the nodes queued for pull or push, whose action may never run. " +
	"Pass the nodes, at most 50 exact names or IDs, or at least one of the filters of list_nodes and count_nodes, to select up to 1000 nodes; a larger selection is refused, and nothing is queued. An unknown or ambiguous node name refuses the whole call, before anything is queued. "

type NodeModulesetOutput = core.NodeModulesetChange

// nodeComplianceSelection tells how a compliance change selects its nodes.
const nodeComplianceSelection = "Pass the nodes, at most 50 exact names or IDs, or at least one of the filters of list_nodes and count_nodes, to select up to 1000 nodes; a larger selection is refused, and nothing is changed. " +
	"An unknown or ambiguous node name refuses the whole call, before anything is changed. The MCP sends one request per node: the change is not atomic, and the result gives the outcome on each node, with the reason of each refusal. " +
	"Requires to be responsible for the nodes; the collector attaches only what is published to the group responsible for the node, or to Everybody."

// nodeTagSelection tells how a tag change selects its nodes.
const nodeTagSelection = "Pass the nodes, at most 50 exact names or IDs, or at least one of the filters of list_nodes and count_nodes, the tag filter given here as filter_tag, to select up to 1000 nodes; a larger selection is refused, and nothing is changed." +
	"An unknown or ambiguous node name refuses the whole call, before anything is changed. The MCP sends one request per node: the change is not atomic, and the result gives the outcome on each node, with the reason of each refusal, such as a tag the collector holds incompatible with the other tags of the node. " +
	"Requires to be responsible for the nodes."

// RegisterNodeActionTools registers the tools that create, update and
// delete nodes in the collector. They are registered only when actions are
// enabled.
func RegisterNodeActionTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "create_node",
			Title: "Create node",
			Description: "Create one node in the OpenSVC collector inventory, by its name, with optional properties: its app, the group responsible for it, its status, role, asset data and location. " +
				"A name a node already has is refused, and nothing is changed. The collector gives the node ID; without app or team_responsible, it gives the default app and the default group of the caller. " +
				"Requires the Manager privilege. The node holds the properties only until an agent registers and pushes it. not_applied lists the properties the collector holds with another value than asked for.",
			Annotations: actionAnnotations(false, false),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CreateNodeInput) (*mcp.CallToolResult, CreateNodeOutput, error) {
			result, err := service.CreateNode(ctx, input.Nodename, input.properties())
			if err != nil {
				return nil, CreateNodeOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "update_node",
			Title: "Update node",
			Description: "Set properties of one node in the OpenSVC collector: its app, the group responsible for it, its status, role, asset data and location. A property left out is not changed; an empty value clears it, except app and team_responsible. " +
				"Returns each property set with its value before and after the update. Changing team_responsible changes who may manage the node. " +
				"The agent of the node rewrites asset_env, sec_zone, team_integ, team_support and the location at each push when the configuration of its node sets them. " +
				"Requires the NodeManager privilege and to be responsible for the node.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateNodeInput) (*mcp.CallToolResult, UpdateNodeOutput, error) {
			result, err := service.UpdateNode(ctx, input.Node, input.properties())
			if err != nil {
				return nil, UpdateNodeOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "attach_node_tag",
			Title: "Attach node tag",
			Description: "Attach one tag of the OpenSVC collector to one or several nodes, such as pci-dss, which filters, filtersets and contextual compliance rulesets may rely on. A node already holding the tag is left as it is, its attach data updated when given. " +
				nodeTagSelection,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input AttachNodeTagInput) (*mcp.CallToolResult, NodeTagOutput, error) {
			options := input.options()
			options.AttachData = input.AttachData
			result, err := service.AttachNodeTag(ctx, options)
			if err != nil {
				return nil, NodeTagOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "detach_node_tag",
			Title: "Detach node tag",
			Description: "Detach one tag of the OpenSVC collector from one or several nodes, with its attach data: the filters, filtersets and contextual compliance rulesets relying on the tag no longer select them. A node not holding the tag is left as it is. " +
				nodeTagSelection,
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input NodeTagInput) (*mcp.CallToolResult, NodeTagOutput, error) {
			result, err := service.DetachNodeTag(ctx, input.options())
			if err != nil {
				return nil, NodeTagOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	for _, declared := range []struct {
		name, title, description string
		attach                   bool
	}{
		{"attach_node_ruleset", "Attach node ruleset", "Attach one explicit compliance ruleset to one or several nodes: its variables, the expected values the compliance modules check, apply to them, and a compliance fix would enforce them. A contextual ruleset applies through its filterset, and is refused. A node already holding the ruleset is left as it is. ", true},
		{"detach_node_ruleset", "Detach node ruleset", "Detach one compliance ruleset from one or several nodes: its variables no longer apply to them, unless a filterset or another ruleset brings them. A node not holding the ruleset is left as it is. ", false},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name: declared.name, Title: declared.title, Description: declared.description + nodeComplianceSelection,
				// An attachment is destructive too: it makes the agents
				// run modules, which may change the nodes, as an autofix.
				Annotations: actionAnnotations(true, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input NodeRulesetInput) (*mcp.CallToolResult, NodeRulesetOutput, error) {
				options := core.NodeRulesetOptions{Ruleset: input.Ruleset, NodeSelection: input.selection()}
				change := service.DetachNodeRuleset
				if declared.attach {
					change = service.AttachNodeRuleset
				}
				result, err := change(ctx, options)
				if err != nil {
					return nil, NodeRulesetOutput{}, err
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
		{"attach_node_moduleset", "Attach node moduleset", "Attach one compliance moduleset to one or several nodes: the agent runs its modules, and those of the modulesets it includes, at each compliance check, and a compliance fix lets them change the node. A node already holding the moduleset is left as it is. ", true},
		{"detach_node_moduleset", "Detach node moduleset", "Detach one compliance moduleset from one or several nodes: the agent no longer runs its modules there, unless another moduleset brings them. A node not holding the moduleset is left as it is. ", false},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name: declared.name, Title: declared.title, Description: declared.description + nodeComplianceSelection,
				// An attachment is destructive too: it makes the agents
				// run modules, which may change the nodes, as an autofix.
				Annotations: actionAnnotations(true, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input NodeModulesetInput) (*mcp.CallToolResult, NodeModulesetOutput, error) {
				options := core.NodeModulesetOptions{Moduleset: input.Moduleset, NodeSelection: input.selection()}
				change := service.DetachNodeModuleset
				if declared.attach {
					change = service.AttachNodeModuleset
				}
				result, err := change(ctx, options)
				if err != nil {
					return nil, NodeModulesetOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "queue_node_action",
			Title: "Queue node action",
			Description: "Queue one agent action on one or several nodes in the OpenSVC collector: the agent of each node runs it, as om node <action>, when it reads its queue. " +
				"The actions refresh what the collector knows of the node, and change nothing else on it: pushasset, its hardware and system; pushdisks, its disks; pushpkg, its packages; pushstats, its performance statistics; checks, its health checks; sysreport, its sysreport; compliance_check, a compliance check on every module attached to it, without fixing. " +
				"Returns as soon as each action is queued, with its action_id: follow it with list_node_actions. Queuing again queues another run. Only an action queued for feed reaches an om3 agent: a warning tells the nodes queued for pull or push, whose action may never run. " +
				"Pass the nodes, at most 50 exact names or IDs, or at least one of the filters of list_nodes and count_nodes, to select up to 1000 nodes; a larger selection is refused, and nothing is queued. " +
				"An unknown or ambiguous node name refuses the whole call, before anything is queued. The result gives the outcome on each node, with the reason of each refusal. " +
				"Requires the NodeExec privilege, CompExec for compliance_check, and to be responsible for the nodes.",
			Annotations: actionAnnotations(false, false),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input QueueNodeActionInput) (*mcp.CallToolResult, QueueNodeActionOutput, error) {
			result, err := service.QueueNodeAction(ctx, core.NodeActionOptions{Action: input.Action, NodeSelection: input.selection()})
			if err != nil {
				return nil, QueueNodeActionOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	for _, declared := range []struct {
		name, title, action, description string
		destructive                      bool
	}{
		{"freeze_node", "Freeze node", "freeze", "Freeze one or several nodes: the agent of each runs om node freeze, and the daemon stops the automatic starts and failovers of the instances of the node, until it is unfrozen. Running instances keep running. ", false},
		{"unfreeze_node", "Unfreeze node", "thaw", "Unfreeze one or several nodes: the collector queues the action thaw, the agent of each runs om node unfreeze, and the daemon resumes the automatic actions of the node, which may then start, fail over or restart instances there to reach their placement. ", true},
		{"drain_node", "Drain node", "drain", "Drain one or several nodes: the agent of each runs om node drain, which freezes the node and stops every instance running on it, so that its services stop or fail over to other nodes. " +
			"om node drain hands the drain to the daemon and returns: an action done with exit code 0 means the drain was accepted, not that the node is empty; get_node tells whether the node is frozen, and list_instances which instances stopped or moved. " +
			"The node stays frozen after the drain: unfreeze_node gives it back to the automatic actions, to end a maintenance. ", true},
		{"fix_node_compliance", "Fix node compliance", "compliance_fix", "Fix the compliance of one or several nodes: the agent of each runs om node compliance fix, and every module attached to the node changes the node to the values of its rulesets, such as files, packages or settings. ", true},
	} {
		declared := declared
		privilege := "Requires the NodeExec privilege and to be responsible for the nodes."
		if declared.action == "compliance_fix" {
			privilege = "Requires the CompExec privilege and to be responsible for the nodes."
		}
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name: declared.name, Title: declared.title, Description: declared.description + nodeActionFollowUp + privilege,
				Annotations: actionAnnotations(declared.destructive, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input NodeSelectionInput) (*mcp.CallToolResult, QueueNodeActionOutput, error) {
				result, err := service.RunNodeAction(ctx, core.NodeActionOptions{Action: declared.action, NodeSelection: input.selection()})
				if err != nil {
					return nil, QueueNodeActionOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "cancel_node_action",
			Title: "Cancel node action",
			Description: "Cancel actions of the OpenSVC collector queue that no agent has taken yet: waiting, notified or queued. An action sent to its agent, running or done is refused: a cancellation would not stop it. " +
				"An agent may take an action between the check and the cancellation, and run it though the result says cancelled: check list_node_actions. " +
				"Use it for an action queued by mistake, or one queued for pull or push that an om3 agent never reads. Requires the NodeExec or CompExec privilege and to be responsible for the node of each action.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CancelNodeActionInput) (*mcp.CallToolResult, CancelNodeActionOutput, error) {
			result, err := service.CancelNodeActions(ctx, input.ActionIDs)
			if err != nil {
				return nil, CancelNodeActionOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := registerServiceTagTools(registrar, service); err != nil {
		return err
	}
	if err := registerServiceDeleteTools(registrar, service); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "delete_node",
			Title: "Delete node",
			Description: "Delete one node from the OpenSVC collector, irreversibly, with its related entries. They are its addresses, adapters, tags, packages, patches, service instance states and their history, resource history, live checks, alerts and SAN zones, and the credentials of its agent, which must register again to push. " +
				"It does not stop or change the node itself. Requires the NodeManager privilege and to be responsible for the node.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteNodeInput) (*mcp.CallToolResult, DeleteNodeOutput, error) {
			result, err := service.DeleteNode(ctx, input.Node)
			if err != nil {
				return nil, DeleteNodeOutput{}, err
			}
			return nil, result, nil
		},
	)
}

package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type CheckKeyInput struct {
	Node     string `json:"node" jsonschema:"one exact node name or node ID: the node the check runs on"`
	Type     string `json:"type" jsonschema:"the exact check type, such as fs_u, fs_i, mpath, vg_u, eth or zpool, as list_node_checks gives it"`
	Instance string `json:"instance" jsonschema:"the exact instance of the check, such as the mount point /var or /var.free, as list_node_checks gives it"`
	Service  string `json:"service,omitempty" jsonschema:"for a check of a resource of a service, one exact service name or service ID, as list_node_checks gives it; none for a check of the node"`
}

func (input CheckKeyInput) key() core.CheckKeyOptions {
	return core.CheckKeyOptions{Node: input.Node, Type: input.Type, Instance: input.Instance, Service: input.Service}
}

type GetCheckThresholdOutput = core.CheckThresholdDetails

type ListCheckThresholdsInput struct {
	Scope     string `json:"scope" jsonschema:"settings: the thresholds set on checks; defaults: the defaults of the check types; contextual: the thresholds of filtersets"`
	Type      string `json:"type,omitempty" jsonschema:"optional exact check type, such as fs_u"`
	Node      string `json:"node,omitempty" jsonschema:"with scope settings, optional exact node name or node ID"`
	Filterset string `json:"filterset,omitempty" jsonschema:"with scope contextual, optional exact filterset name or filterset ID"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional maximum number of rows, 50 by default, 500 at most"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"optional next_cursor of the previous page, with the same filters"`
}

type ListCheckThresholdsOutput = core.CheckThresholdList

// thresholdOrder tells how the collector picks the thresholds of a check.
const thresholdOrder = "The collector takes the thresholds of a check from its settings, else from the most recently created contextual thresholds of its type whose filterset holds its node, and its service, and whose pattern, an unanchored regular expression, matches its instance, whatever the precision of the pattern; " +
	"else from the defaults of its type whose pattern, anchored, or empty, matches its instance, of the highest priority, then of the longest pattern; else it has none. "

// registerCheckThresholdTools registers the tools that read the thresholds
// of the checks. RegisterCheckTools calls it.
func registerCheckThresholdTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_check_threshold",
			Title: "Get check threshold",
			Description: "Explain where the thresholds of one check of the OpenSVC collector come from, and which it would take without its settings. " +
				"The result gives the check, its value and the thresholds it takes, its settings, with who set them and when, and the contextual thresholds and the defaults of its type that match its instance, the one that wins marked. " +
				thresholdOrder +
				"Thresholds are integers, in the unit of the check type. Use list_node_checks for the checks of a node and their instances.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CheckKeyInput) (*mcp.CallToolResult, GetCheckThresholdOutput, error) {
			result, err := service.GetCheckThreshold(ctx, input.key())
			if err != nil {
				return nil, GetCheckThresholdOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_check_thresholds",
			Title: "List check thresholds",
			Description: "List one page of the thresholds of the checks of the OpenSVC collector, by scope: the settings of checks, the defaults of the check types, or the contextual thresholds of filtersets, with their IDs. " +
				thresholdOrder +
				"Use get_check_threshold for where the thresholds of one check come from. The settings are those of the nodes the caller is responsible for.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListCheckThresholdsInput) (*mcp.CallToolResult, ListCheckThresholdsOutput, error) {
			result, err := service.ListCheckThresholds(ctx, core.ListCheckThresholdsOptions{Scope: input.Scope, Type: input.Type, Node: input.Node, Filterset: input.Filterset, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListCheckThresholdsOutput{}, err
			}
			return nil, result, nil
		},
	)
}

type SetCheckThresholdInput struct {
	CheckKeyInput
	Low  *int64 `json:"low,omitempty" jsonschema:"the low threshold, an integer in the unit of the check type; not given, the check keeps its low threshold"`
	High *int64 `json:"high,omitempty" jsonschema:"the high threshold, an integer in the unit of the check type; not given, the check keeps its high threshold"`
}

type CheckThresholdChangeOutput = core.CheckThresholdChange

// registerCheckThresholdWriteTools registers the tools that set the
// thresholds of a check. RegisterNodeActionTools calls it: they are
// registered only when actions are enabled.
func registerCheckThresholdWriteTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "set_check_threshold",
			Title: "Set check threshold",
			Description: "Set the thresholds of one check of the OpenSVC collector on its node, which take precedence over the contextual and the default ones, such as the high threshold of the filesystem usage of /var on a node. " +
				"A threshold not given keeps the value the check has now; both must then be known, and low at most high; with neither, the current thresholds are recorded as settings, which the defaults no longer change. Thresholds are integers, in the unit of the check type. " +
				"The collector applies them to the check and its alert at once: the check comes back read, with its thresholds and whether it is out of bounds. unset_check_threshold gives the check its contextual or default thresholds again. " +
				"Requires the CheckExec or the Manager privilege, and to be responsible for the node, by its app.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input SetCheckThresholdInput) (*mcp.CallToolResult, CheckThresholdChangeOutput, error) {
			result, err := service.SetCheckThreshold(ctx, input.key(), input.Low, input.High)
			if err != nil {
				return nil, CheckThresholdChangeOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "unset_check_threshold",
			Title: "Unset check threshold",
			Description: "Remove the thresholds set on one check of the OpenSVC collector: it takes the contextual or the default thresholds again, which get_check_threshold tells beforehand. A check without settings is told unchanged. " +
				"The collector applies it to the check and its alert at once: the check comes back read. " +
				"Requires the CheckExec or the Manager privilege, and to be responsible for the node, by its app.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CheckKeyInput) (*mcp.CallToolResult, CheckThresholdChangeOutput, error) {
			result, err := service.UnsetCheckThreshold(ctx, input.key())
			if err != nil {
				return nil, CheckThresholdChangeOutput{}, err
			}
			return nil, result, nil
		},
	)
}

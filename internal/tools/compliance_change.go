package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// ComplianceOperationInput is one operation of a compliance change.
type ComplianceOperationInput struct {
	Op            string  `json:"op" jsonschema:"the operation"`
	Kind          string  `json:"kind" jsonschema:"the kind of the object the operation changes, or creates"`
	Ref           string  `json:"ref,omitempty" jsonschema:"the object, by its exact name, its ID, or the tmp: reference an earlier operation gave it; every op but create"`
	As            string  `json:"as,omitempty" jsonschema:"create and clone: the tmp: reference the next operations name the new object by, such as tmp:web"`
	Name          *string `json:"name,omitempty" jsonschema:"create, rename, add_module, rename_module: the name; clone: the name of the copy, <name>_clone when not given"`
	RulesetType   string  `json:"ruleset_type,omitempty" jsonschema:"set_type: explicit, applied where it is attached, or contextual, applied to the nodes and services of its filterset"`
	RulesetPublic *bool   `json:"ruleset_public,omitempty" jsonschema:"set_public: whether the ruleset may be attached to nodes and services"`
	Filterset     *string `json:"filterset,omitempty" jsonschema:"set_filterset: the filterset, by exact name or ID, which makes the ruleset contextual; empty removes it"`
	Variable      string  `json:"variable,omitempty" jsonschema:"update_variable, delete_variable, copy_variable: the variable of the ruleset, by name or ID"`
	VarName       *string `json:"var_name,omitempty" jsonschema:"add_variable: the name of the new variable, which the modules read; update_variable: its new name"`
	VarClass      *string `json:"var_class,omitempty" jsonschema:"add_variable, update_variable: the class of the variable, the form telling how the modules read the value"`
	VarValue      *string `json:"var_value,omitempty" jsonschema:"add_variable, update_variable: the value, a text, often JSON serialized in it"`
	Dst           string  `json:"dst,omitempty" jsonschema:"copy_variable: the ruleset the variable goes to, by name, ID or tmp: reference"`
	Move          bool    `json:"move,omitempty" jsonschema:"copy_variable: true to move the variable instead of copying it"`
	ChildKind     string  `json:"child_kind,omitempty" jsonschema:"include, detach: the kind of the child; a ruleset includes rulesets, a moduleset includes modulesets and holds rulesets"`
	Child         string  `json:"child,omitempty" jsonschema:"include, detach: the child, by name, ID or tmp: reference"`
	Module        string  `json:"module,omitempty" jsonschema:"rename_module, set_autofix, delete_module: the module of the moduleset, by name or ID"`
	Autofix       *bool   `json:"autofix,omitempty" jsonschema:"set_autofix: whether a compliance check also fixes what the module finds"`
	Role          string  `json:"role,omitempty" jsonschema:"add_team, remove_team, move_team: the role of the group"`
	ToRole        string  `json:"to_role,omitempty" jsonschema:"move_team: the role the group takes instead"`
	Team          string  `json:"team,omitempty" jsonschema:"add_team, remove_team, move_team: the group, by its name (role) or ID"`
}

func (input ComplianceOperationInput) operation() core.ComplianceOperation {
	return core.ComplianceOperation{
		Op: input.Op, Kind: input.Kind, Ref: input.Ref, As: input.As, Name: input.Name,
		RulesetType: input.RulesetType, RulesetPublic: input.RulesetPublic, Filterset: input.Filterset,
		Variable: input.Variable, VarName: input.VarName, VarClass: input.VarClass, VarValue: input.VarValue,
		Dst: input.Dst, Move: input.Move, ChildKind: input.ChildKind, Child: input.Child,
		Module: input.Module, Autofix: input.Autofix, Role: input.Role, ToRole: input.ToRole, Team: input.Team,
	}
}

type ApplyComplianceChangesInput struct {
	Message    string                     `json:"message" jsonschema:"what the change does, its first line a summary: the message of the version of the compliance history recording it"`
	Operations []ComplianceOperationInput `json:"operations" jsonschema:"the operations, 1 to 100, applied in their order, all of them or none"`
}

type ApplyComplianceChangesOutput = core.ComplianceChanges

// complianceOperationsSchema sets the lists of the collector on the
// properties of an operation.
func complianceOperationsSchema() (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[ApplyComplianceChangesInput](&jsonschema.ForOptions{})
	if err != nil {
		return nil, err
	}
	operations, ok := schema.Properties["operations"]
	if !ok || operations.Items == nil {
		return nil, fmt.Errorf("input property operations is not a list of operations")
	}
	for name, values := range map[string][]string{
		"op": core.ComplianceOperations, "kind": core.ComplianceKinds, "child_kind": core.ComplianceKinds,
		"role": core.ComplianceTeamRoles, "to_role": core.ComplianceTeamRoles, "ruleset_type": core.ComplianceRulesetTypes,
	} {
		property, ok := operations.Items.Properties[name]
		if !ok || property.Type != "string" {
			return nil, fmt.Errorf("operation property %q is not a text", name)
		}
		for _, value := range values {
			property.Enum = append(property.Enum, value)
		}
	}
	minItems, maxItems := 1, 100
	operations.MinItems, operations.MaxItems = &minItems, &maxItems
	return schema, nil
}

// registerComplianceChangeTools registers the tool that changes the
// compliance rulesets and modulesets. RegisterNodeActionTools calls it: it
// is registered only when actions are enabled.
func registerComplianceChangeTools(registrar *Registrar, service *core.Service) error {
	schema, err := complianceOperationsSchema()
	if err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "apply_compliance_changes", Title: "Apply compliance changes", InputSchema: schema,
		Description: "Change the compliance rulesets and modulesets of the OpenSVC collector: a list of operations, applied in order, all of them or none, recorded as one version of the compliance history with message. " +
			"Operations, by op (kind ruleset R, moduleset M): create (name, as), rename (name), clone (as, name), delete; R: set_type (ruleset_type; explicit keeps the filterset, set_filterset empty removes it), set_public (ruleset_public), set_filterset (filterset, empty removes it; it makes the ruleset contextual), add_variable (var_name, var_class, var_value), update_variable (variable, then var_name, var_class or var_value), delete_variable (variable), copy_variable (variable, dst, move); include and detach (child_kind, child): a ruleset in a ruleset, a moduleset or a ruleset in a moduleset; M: add_module (name, autofix off), rename_module (module, name), set_autofix (module, autofix), delete_module (module); add_team, remove_team (role publication or responsible, team), move_team (role, to_role, team). " +
			"Name an object by exact name, ID, or the tmp: reference that its create or clone gave it with as. A variable value is a text: serialize JSON in it; read the form named by var_class with get_form for its format, and a similar ruleset with get_compliance_ruleset. " +
			"Publishing a ruleset or moduleset to a group lets its members see and attach it; a responsible group may change it. A change to a ruleset or moduleset attached applies at the next compliance check of its nodes and services, a module in autofix changing them. " +
			"Requires the CompManager or the Manager privilege, and to be responsible for each object changed. The objects named are first read among those the caller sees. A refusal names the operation, and nothing is applied. The objects changed come back read, variables masked; the values passed in are not masked in the confirmation nor the audit. list_compliance_history gives the versions.",
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ApplyComplianceChangesInput) (*mcp.CallToolResult, ApplyComplianceChangesOutput, error) {
		operations := make([]core.ComplianceOperation, 0, len(input.Operations))
		for _, operation := range input.Operations {
			operations = append(operations, operation.operation())
		}
		result, err := service.ApplyComplianceChanges(ctx, input.Message, operations)
		if err != nil {
			return nil, ApplyComplianceChangesOutput{}, err
		}
		return nil, result, nil
	})
}

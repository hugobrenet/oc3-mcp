package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// FiltersetCriterionInput is a criterion: a filter, table, field, op and
// value, or an included filterset.
type FiltersetCriterionInput struct {
	Logic     string `json:"logic" jsonschema:"how the criterion combines with those before it: AND, OR, AND NOT or OR NOT; NOT negates the criterion"`
	Table     string `json:"table,omitempty" jsonschema:"for a filter, the table of the column, such as nodes, services, apps or v_tags"`
	Field     string `json:"field,omitempty" jsonschema:"for a filter, the column of the table, a database column name, such as nodename, os_name or loc_city of nodes"`
	Op        string `json:"op,omitempty" jsonschema:"for a filter, the comparison: =, LIKE with % as a wildcard, >, >=, <, <=, or IN with values separated by commas without spaces"`
	Value     string `json:"value,omitempty" jsonschema:"for a filter, the value compared, as the column stores it, at most 256 characters"`
	Filterset string `json:"filterset,omitempty" jsonschema:"instead of a filter, one exact filterset name or ID to include, whose criteria apply in place"`
}

func (input FiltersetCriterionInput) criterion() core.FiltersetCriterionInput {
	return core.FiltersetCriterionInput{Logic: input.Logic, Table: input.Table, Field: input.Field, Op: input.Op, Value: input.Value, Filterset: input.Filterset}
}

type CreateFiltersetInput struct {
	Name     string                    `json:"name" jsonschema:"the filterset name, unique, at most 100 characters"`
	Criteria []FiltersetCriterionInput `json:"criteria" jsonschema:"the criteria, in their order"`
}

type RenameFiltersetInput struct {
	Filterset string `json:"filterset" jsonschema:"one exact filterset name or filterset ID"`
	Name      string `json:"name" jsonschema:"the new name"`
}

type AddFiltersetCriterionInput struct {
	Filterset string `json:"filterset" jsonschema:"one exact filterset name or filterset ID: the filterset to change"`
	Logic     string `json:"logic" jsonschema:"how the criterion combines with those before it: AND, OR, AND NOT or OR NOT; NOT negates the criterion"`
	Table     string `json:"table,omitempty" jsonschema:"for a filter, the table of the column, such as nodes, services, apps or v_tags"`
	Field     string `json:"field,omitempty" jsonschema:"for a filter, the column of the table, a database column name, such as nodename, os_name or loc_city of nodes"`
	Op        string `json:"op,omitempty" jsonschema:"for a filter, the comparison: =, LIKE with % as a wildcard, >, >=, <, <=, or IN with values separated by commas without spaces"`
	Value     string `json:"value,omitempty" jsonschema:"for a filter, the value compared, as the column stores it, at most 256 characters"`
	Included  string `json:"included_filterset,omitempty" jsonschema:"instead of a filter, one exact filterset name or ID to include, whose criteria apply in place"`
	Order     *int   `json:"order,omitempty" jsonschema:"optional rank of the criterion; a new criterion goes last when not given, one attached already keeps its rank"`
}

func (input AddFiltersetCriterionInput) criterion() core.FiltersetCriterionInput {
	return core.FiltersetCriterionInput{Logic: input.Logic, Table: input.Table, Field: input.Field, Op: input.Op, Value: input.Value, Filterset: input.Included}
}

type RemoveFiltersetCriterionInput struct {
	Filterset string `json:"filterset" jsonschema:"one exact filterset name or filterset ID: the filterset to change"`
	Table     string `json:"table,omitempty" jsonschema:"for a filter, its table"`
	Field     string `json:"field,omitempty" jsonschema:"for a filter, its column"`
	Op        string `json:"op,omitempty" jsonschema:"for a filter, its comparison"`
	Value     string `json:"value,omitempty" jsonschema:"for a filter, its value"`
	Included  string `json:"included_filterset,omitempty" jsonschema:"instead of a filter, the included filterset to remove, by its exact name or ID"`
}

type DeleteFiltersetInput struct {
	Filterset string `json:"filterset" jsonschema:"one exact filterset name or filterset ID"`
}

type FiltersetWriteOutput = core.FiltersetWrite

// filtersetEvaluation tells how the collector reads the criteria.
const filtersetEvaluation = "The collector evaluates the criteria from left to right, without precedence nor parentheses: AND intersects what the criteria before select with what the criterion selects, OR adds it, NOT negates the criterion; the logic of the first criterion is ignored, its NOT kept. " +
	"An included filterset is read in place, its own criteria with their own logic, the logic of the inclusion ignored. " +
	"A filter compares a database column, such as nodes.nodename, os_name, os_vendor, os_release, app, loc_city, loc_country, host_type, node_env or mem_bytes (in MB), services.svcname, svc_app or svc_env, apps.app, v_tags.tag_name; get_filterset gives the criteria of a similar filterset. Negate with AND NOT or OR NOT. "

// criterionSchema sets the lists of the collector on the properties of a
// criterion.
func criterionSchema(properties map[string]*jsonschema.Schema) error {
	for name, values := range map[string][]string{"logic": core.FiltersetLogics, "table": core.FilterTables, "op": core.FilterOperators} {
		property, ok := properties[name]
		if !ok {
			continue
		}
		if property.Type != "string" {
			return fmt.Errorf("criterion property %q is not a text", name)
		}
		for _, value := range values {
			property.Enum = append(property.Enum, value)
		}
	}
	return nil
}

func filtersetInputSchema[In any](criteria string) (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[In](&jsonschema.ForOptions{})
	if err != nil {
		return nil, err
	}
	properties := schema.Properties
	if criteria != "" {
		property, ok := schema.Properties[criteria]
		if !ok || property.Items == nil {
			return nil, fmt.Errorf("input property %q is not a list of criteria", criteria)
		}
		properties = property.Items.Properties
	}
	return schema, criterionSchema(properties)
}

// registerFiltersetWriteTools registers the tools that create, change and
// delete filtersets. RegisterNodeActionTools calls it: they are registered
// only when actions are enabled.
func registerFiltersetWriteTools(registrar *Registrar, service *core.Service) error {
	createSchema, err := filtersetInputSchema[CreateFiltersetInput]("criteria")
	if err != nil {
		return err
	}
	addSchema, err := filtersetInputSchema[AddFiltersetCriterionInput]("")
	if err != nil {
		return err
	}
	removeSchema, err := filtersetInputSchema[RemoveFiltersetCriterionInput]("")
	if err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "create_filterset", Title: "Create filterset", InputSchema: createSchema,
		Description: "Create a filterset in the OpenSVC collector, a selection of nodes and services by criteria, which contextual compliance rulesets, check thresholds and sysreport grants apply to; each criterion is a filter on a column or an included filterset. " +
			filtersetEvaluation +
			"The collector creates the filterset and its criteria at once, reusing a filter of the same definition. The filterset comes back read as get_filterset reads it, with the nodes and services it selects: check them. Requires the CompManager or the Manager privilege.",
		Annotations: actionAnnotations(false, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateFiltersetInput) (*mcp.CallToolResult, FiltersetWriteOutput, error) {
		criteria := make([]core.FiltersetCriterionInput, 0, len(input.Criteria))
		for _, criterion := range input.Criteria {
			criteria = append(criteria, criterion.criterion())
		}
		result, err := service.CreateFilterset(ctx, input.Name, criteria)
		if err != nil {
			return nil, FiltersetWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "rename_filterset", Title: "Rename filterset",
		Description: "Rename one filterset of the OpenSVC collector: the rulesets, thresholds and filtersets using it refer to its ID and keep it. Requires the CompManager or the Manager privilege.",
		Annotations: actionAnnotations(false, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RenameFiltersetInput) (*mcp.CallToolResult, FiltersetWriteOutput, error) {
		result, err := service.RenameFilterset(ctx, input.Filterset, input.Name)
		if err != nil {
			return nil, FiltersetWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "add_filterset_criterion", Title: "Add filterset criterion", InputSchema: addSchema,
		Description: "Add a criterion to one filterset of the OpenSVC collector, a filter on a column or an included filterset, or change the logic or the rank of one it holds: what the filterset selects changes for the rulesets, thresholds and grants using it, which get_filterset names. " +
			filtersetEvaluation +
			"A new criterion goes last unless order is given; the ranks are not renumbered. The filterset comes back read with the nodes and services it selects now. Requires the CompManager or the Manager privilege.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddFiltersetCriterionInput) (*mcp.CallToolResult, FiltersetWriteOutput, error) {
		result, err := service.AddFiltersetCriterion(ctx, input.Filterset, input.criterion(), input.Order)
		if err != nil {
			return nil, FiltersetWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "remove_filterset_criterion", Title: "Remove filterset criterion", InputSchema: removeSchema,
		Description: "Remove a criterion from one filterset of the OpenSVC collector, a filter by its table, field, op and value, or an included filterset, as get_filterset gives them: what the filterset selects changes for the rulesets, thresholds and grants using it. " +
			"The filter itself stays, for the other filtersets. A filterset without criteria selects nothing. The filterset comes back read with the nodes and services it selects now. Requires the CompManager or the Manager privilege.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RemoveFiltersetCriterionInput) (*mcp.CallToolResult, FiltersetWriteOutput, error) {
		criterion := core.FiltersetCriterionInput{Table: input.Table, Field: input.Field, Op: input.Op, Value: input.Value, Filterset: input.Included}
		result, err := service.RemoveFiltersetCriterion(ctx, input.Filterset, criterion)
		if err != nil {
			return nil, FiltersetWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "delete_filterset", Title: "Delete filterset",
		Description: "Delete one filterset of the OpenSVC collector, with what refers to it: its inclusions in other filtersets, the attachments of contextual rulesets, the contextual check thresholds, the session filters of users, the statistics comparisons and the sysreport grants using it, which get_filterset names beforehand. " +
			"The filters it held stay. The result gives the filterset as it was, with what used it. Requires the CompManager or the Manager privilege.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteFiltersetInput) (*mcp.CallToolResult, FiltersetWriteOutput, error) {
		result, err := service.DeleteFilterset(ctx, input.Filterset)
		if err != nil {
			return nil, FiltersetWriteOutput{}, err
		}
		return nil, result, nil
	})
}

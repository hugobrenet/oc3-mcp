package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type CreateFormInput struct {
	Name   string `json:"name" jsonschema:"the name of the form, unique in the collector, such as sample-vm-request"`
	YAML   string `json:"yaml" jsonschema:"the definition of the form, YAML of capitalized keys as the forms of the collector write them: Label, Desc, Inputs (Id, Label, Type, Mandatory, Candidates, StrictCandidates…), Outputs (Dest, and the keys of its Dest)"`
	Folder string `json:"folder,omitempty" jsonschema:"optional folder of the form, such as /samples"`
	Type   string `json:"type,omitempty" jsonschema:"optional form type: custo or generic for a form starting a request or running an action, obj for a compliance variable form; the collector only stores it"`
}

type UpdateFormInput struct {
	Form   string  `json:"form" jsonschema:"one exact form name or form ID"`
	Name   *string `json:"name,omitempty" jsonschema:"optional new name of the form"`
	YAML   *string `json:"yaml,omitempty" jsonschema:"optional new definition, which replaces the whole definition: the one get_form gives, changed"`
	Folder *string `json:"folder,omitempty" jsonschema:"optional new folder"`
	Type   *string `json:"type,omitempty" jsonschema:"optional new form type: custo, generic or obj"`
}

type DeleteFormInput struct {
	Form string `json:"form" jsonschema:"one exact form name or form ID"`
}

type FormGroupsInput struct {
	Form         string   `json:"form" jsonschema:"one exact form name or form ID"`
	Publications []string `json:"publications,omitempty" jsonschema:"optional groups, by name or ID, whose members may submit the form"`
	Responsibles []string `json:"responsibles,omitempty" jsonschema:"optional groups, by name or ID, whose members may change the form"`
}

type FormWriteOutput = core.FormWrite
type FormGroupsOutput = core.FormGroupChange

// formDefinitionWriting tells how the collector takes a definition.
const formDefinitionWriting = "The collector checks only that the definition is YAML: a key it does not know, or an output without a Dest it runs (workflow, rest, script, mail or db), is ignored, and the form then does nothing on submission. " +
	"Start from the definition of a similar form, which get_form gives, rather than from memory. " +
	"The form comes back read as get_form reads it: check its inputs and outputs, and tell the user of an output missing. "

// registerFormWriteTools registers the tools that create, change and
// delete forms and their groups. RegisterNodeActionTools calls it: they are
// registered only when actions are enabled.
func registerFormWriteTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "create_form", Title: "Create form",
		Description: "Create a form of the OpenSVC collector from its name and its definition, the YAML of what a submission asks and runs, such as starting a request, calling an API or running a script. " +
			formDefinitionWriting +
			"The collector publishes the new form to the default group of the caller and makes that group responsible for it; attach_form_groups adds groups. A name already taken is refused. Requires the FormsManager or the Manager privilege.",
		Annotations: actionAnnotations(false, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateFormInput) (*mcp.CallToolResult, FormWriteOutput, error) {
		result, err := service.CreateForm(ctx, core.CreateFormOptions{Name: input.Name, YAML: input.YAML, Folder: input.Folder, Type: input.Type})
		if err != nil {
			return nil, FormWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "update_form", Title: "Update form",
		Description: "Change a form of the OpenSVC collector: its definition, the YAML of its inputs and outputs, its name, folder or type; the fields not given are kept. " +
			"A definition given replaces the whole definition: read it with get_form, and send it back changed. " + formDefinitionWriting +
			"Each change of the definition is kept in the history of the collector. Requires the FormsManager or the Manager privilege.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateFormInput) (*mcp.CallToolResult, FormWriteOutput, error) {
		result, err := service.UpdateForm(ctx, core.UpdateFormOptions{Form: input.Form, Name: input.Name, YAML: input.YAML, Folder: input.Folder, Type: input.Type})
		if err != nil {
			return nil, FormWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "delete_form", Title: "Delete form",
		Description: "Delete a form of the OpenSVC collector, with its publications and responsibles: it can no longer be submitted, and the requests it started keep their steps. " +
			"Requires the FormsManager or the Manager privilege, and to be responsible for the form.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteFormInput) (*mcp.CallToolResult, FormWriteOutput, error) {
		result, err := service.DeleteForm(ctx, input.Form)
		if err != nil {
			return nil, FormWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	groups := "Pass publications, the groups whose members may submit the form, responsibles, those whose members may change it, or both, at most 50 groups in all, by name or ID; list_groups gives them. " +
		"The MCP sends one request per group, and the result gives the outcome on each. The collector refuses a privilege group, such as Manager, and to a caller not a Manager a group they are not a member of. " +
		"Requires the FormsManager or the Manager privilege, and to be responsible for the form."
	if err := addTool(registrar, &mcp.Tool{
		Name: "attach_form_groups", Title: "Attach form groups",
		Description: "Publish a form of the OpenSVC collector to groups, whose members may then submit it, and make groups responsible for it, whose members may then change it. " + groups,
		Annotations: actionAnnotations(false, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input FormGroupsInput) (*mcp.CallToolResult, FormGroupsOutput, error) {
		result, err := service.AttachFormGroups(ctx, core.FormGroupOptions{Form: input.Form, Publications: input.Publications, Responsibles: input.Responsibles})
		if err != nil {
			return nil, FormGroupsOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "detach_form_groups", Title: "Detach form groups",
		Description: "Unpublish a form of the OpenSVC collector from groups, whose members may then no longer submit it, and withdraw groups from its responsibles, whose members may then no longer change it. " + groups,
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input FormGroupsInput) (*mcp.CallToolResult, FormGroupsOutput, error) {
		result, err := service.DetachFormGroups(ctx, core.FormGroupOptions{Form: input.Form, Publications: input.Publications, Responsibles: input.Responsibles})
		if err != nil {
			return nil, FormGroupsOutput{}, err
		}
		return nil, result, nil
	})
}

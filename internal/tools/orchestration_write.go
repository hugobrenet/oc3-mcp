package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type CreateOrchestrationInput struct {
	Name        string                   `json:"name" jsonschema:"the name of the orchestration, unique"`
	Description *string                  `json:"description,omitempty" jsonschema:"optional description"`
	Steps       []core.OrchestrationStep `json:"steps" jsonschema:"the steps, in order, numbered from 0, at most 50"`
}

type UpdateOrchestrationInput struct {
	Orchestration string                   `json:"orchestration" jsonschema:"one exact orchestration name or ID"`
	Name          *string                  `json:"name,omitempty" jsonschema:"optional new name"`
	Description   *string                  `json:"description,omitempty" jsonschema:"optional description; an empty string clears it"`
	Steps         []core.OrchestrationStep `json:"steps,omitempty" jsonschema:"optional new steps, which replace the whole list: read them with get_orchestration, and pass every step to keep, changed or not"`
}

type DeleteOrchestrationInput struct {
	Orchestration string `json:"orchestration" jsonschema:"one exact orchestration name or ID"`
}

type OrchestrationWriteOutput = core.OrchestrationWrite

// orchestrationWriteCheck tells the agent what the check of the result is.
const orchestrationWriteCheck = "The collector refuses a step it does not accept, such as an action the kind of its targets does not take or a same_as naming no earlier step of that kind, with the step numbered from 1, and a name taken with 409; it saves targets that do not exist or cannot be reached: the MCP reads the orchestration back and returns its check, the steps and targets a run starting now would refuse. Nothing runs: a run is started apart. " +
	"When the collector does not accept the action for the kind asked, tell the user and ask, rather than changing the kind or widening the targets. "

// registerOrchestrationWriteTools registers the tools that create, change
// and delete orchestrations. RegisterNodeActionTools calls it: they are
// registered only when actions are enabled.
func registerOrchestrationWriteTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "create_orchestration", Title: "Create orchestration",
		Description: "Create an orchestration in the OpenSVC collector: an ordered list of steps, each one agent action on a set of targets of one kind, picked by ID, matched by a filterset when a run starts, or the same as an earlier step. " +
			"A step holds its action, such as freeze, stop, drain, pushasset or checks, the kind of its targets, service, instance, node or cluster, its targets, a filterset given by its name or ID, its pace, all, one or batch with batch_size, and an optional timeout in seconds and rid for an instance; check_orchestration checks a draft before. " +
			orchestrationWriteCheck +
			"Requires the Manager privilege.",
		Annotations: actionAnnotations(false, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateOrchestrationInput) (*mcp.CallToolResult, OrchestrationWriteOutput, error) {
		result, err := service.CreateOrchestration(ctx, input.Name, input.Description, input.Steps)
		if err != nil {
			return nil, OrchestrationWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "update_orchestration", Title: "Update orchestration",
		Description: "Change the name, the description or the steps of an orchestration of the OpenSVC collector; the values not given are kept. Steps replace the whole list: read them with get_orchestration first, and pass every step to keep, changed or not. " +
			"The runs, past or running, keep the plan they started with; the next runs take the new steps. " +
			orchestrationWriteCheck +
			"Returns outcome unchanged when the values given were those held. Requires the Manager privilege.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateOrchestrationInput) (*mcp.CallToolResult, OrchestrationWriteOutput, error) {
		result, err := service.UpdateOrchestration(ctx, input.Orchestration, core.OrchestrationFields{Name: input.Name, Description: input.Description, Steps: input.Steps})
		if err != nil {
			return nil, OrchestrationWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "delete_orchestration", Title: "Delete orchestration",
		Description: "Delete an orchestration of the OpenSVC collector. Its runs stay, with the plan they ran, listed by list_orchestration_runs under its name; a run running goes on, which the result warns of. " +
			"Returns the orchestration as it was. Requires the Manager privilege.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteOrchestrationInput) (*mcp.CallToolResult, OrchestrationWriteOutput, error) {
		result, err := service.DeleteOrchestration(ctx, input.Orchestration)
		if err != nil {
			return nil, OrchestrationWriteOutput{}, err
		}
		return nil, result, nil
	})
}

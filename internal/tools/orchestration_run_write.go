package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type StartOrchestrationRunInput struct {
	Orchestration string `json:"orchestration" jsonschema:"one exact orchestration name or ID"`
	Wait          int    `json:"wait,omitempty" jsonschema:"optional seconds to wait, at most 120, for the run to end: the MCP reads it again every 3 seconds, and returns as soon as it is no longer running, or when the wait ends; 0 reads it once"`
}

type OrchestrationRunInput struct {
	ID int `json:"id" jsonschema:"one run ID, as list_orchestration_runs or the last_run_id of an orchestration gives it"`
}

type ContinueOrchestrationRunInput struct {
	ID      int    `json:"id" jsonschema:"one run ID, as list_orchestration_runs or the last_run_id of an orchestration gives it"`
	Comment string `json:"comment" jsonschema:"why the failure of the step is accepted, recorded with the step, at most 2000 bytes"`
}

type OrchestrationRunChangeOutput = core.OrchestrationRunChange

// orchestrationRunResult tells the agent what the result of a run action
// holds and how to follow it.
const orchestrationRunResult = "Returns the run as the collector answers the request, with the first 20 targets; the actions of the targets run in the agents later: follow the run with get_orchestration_run. Requires the Manager privilege."

// registerOrchestrationRunTools registers the tools that start, stop,
// resume and continue the runs of the orchestrations.
// RegisterNodeActionTools calls it: they are registered only when actions
// are enabled.
func registerOrchestrationRunTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "start_orchestration_run", Title: "Start orchestration run",
		Description: "Start a run of an orchestration of the OpenSVC collector: the agents act on its targets, step after step, freezing, stopping or draining nodes and services as its steps say. " +
			"The collector freezes the steps in the run, resolves the filtersets now, and queues the actions of the first step at once; each next step starts when every target of the previous one succeeded, and the run stops at the first target that fails. " +
			"Before the start, read the steps with get_orchestration and the targets with check_orchestration, so the confirmation names what will act; the collector refuses with 400 a step that matches no target. " +
			"To start it and wait for its end, as when asked for its result, pass wait, such as 60 seconds, in the same call; a run still running when the wait ends is followed with get_orchestration_run and wait. " +
			orchestrationRunResult,
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input StartOrchestrationRunInput) (*mcp.CallToolResult, OrchestrationRunChangeOutput, error) {
		result, err := service.StartOrchestrationRun(ctx, input.Orchestration, input.Wait)
		if err != nil {
			return nil, OrchestrationRunChangeOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	for _, declared := range []struct {
		action      core.OrchestrationRunAction
		name, title string
		description string
	}{
		{
			action: core.OrchestrationRunStop, name: "stop_orchestration_run", title: "Stop orchestration run",
			description: "Stop a running run of an orchestration of the OpenSVC collector: the targets not started do not run, nor the next steps, and the actions still waiting in the queue are cancelled. An action an agent already runs cannot be interrupted: it goes on, and its result is recorded. " +
				"The run ends stopped; resume_orchestration_run or continue_orchestration_run goes on with it. The collector refuses with 409 a run not running. ",
		},
		{
			action: core.OrchestrationRunResume, name: "resume_orchestration_run", title: "Resume orchestration run",
			description: "Resume a failed or stopped run of an orchestration of the OpenSVC collector from the step it stopped at: the actions of the targets of that step that did not succeed are queued again, then the next steps run, with the targets resolved when the run started. " +
				"The collector refuses with 409 a run neither failed nor stopped, or one an action of which still runs. ",
		},
	} {
		declared := declared
		if err := addTool(registrar, &mcp.Tool{
			Name: declared.name, Title: declared.title,
			Description: declared.description + orchestrationRunResult,
			Annotations: actionAnnotations(true, false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrchestrationRunInput) (*mcp.CallToolResult, OrchestrationRunChangeOutput, error) {
			result, err := service.ChangeOrchestrationRun(ctx, input.ID, declared.action, "")
			if err != nil {
				return nil, OrchestrationRunChangeOutput{}, err
			}
			return nil, result, nil
		}); err != nil {
			return err
		}
	}
	return addTool(registrar, &mcp.Tool{
		Name: "continue_orchestration_run", Title: "Continue orchestration run",
		Description: "Accept the failed or stopped step of a failed or stopped run of an orchestration of the OpenSVC collector, and go on with the next step: the step becomes acknowledged, with the comment, which is required, the caller and the date; its targets keep their outcome and do not run again, unlike resume_orchestration_run. " +
			"The next steps run with the targets resolved when the run started; when the step was the last, the run ends succeeded, though the targets of the step did not succeed, even those a stop cancelled. " +
			"The collector refuses with 409 a run neither failed nor stopped, or one an action of which still runs. " +
			orchestrationRunResult,
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ContinueOrchestrationRunInput) (*mcp.CallToolResult, OrchestrationRunChangeOutput, error) {
		result, err := service.ChangeOrchestrationRun(ctx, input.ID, core.OrchestrationRunContinue, input.Comment)
		if err != nil {
			return nil, OrchestrationRunChangeOutput{}, err
		}
		return nil, result, nil
	})
}

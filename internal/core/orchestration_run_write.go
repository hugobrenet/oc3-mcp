package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// OrchestrationRunAction is an action on a run: its start, its stop, the
// resume of a failed or stopped run, or the acknowledgement of its failed
// step for it to go on.
type OrchestrationRunAction string

const (
	OrchestrationRunStart    OrchestrationRunAction = "start"
	OrchestrationRunStop     OrchestrationRunAction = "stop"
	OrchestrationRunResume   OrchestrationRunAction = "resume"
	OrchestrationRunContinue OrchestrationRunAction = "continue"
)

type OrchestrationRunChange struct {
	Action string `json:"action" jsonschema:"start, stop, resume or continue"`
	OrchestrationRunDetails
}

type collectorRunContinue struct {
	Comment string `json:"comment"`
}

// StartOrchestrationRun starts a run of an orchestration, by its name or
// ID: the collector freezes its steps in the run, resolves the filtersets
// and starts the first step at once.
func (s *Service) StartOrchestrationRun(ctx context.Context, orchestration string, wait int) (OrchestrationRunChange, error) {
	if wait < 0 || wait > maxActionWait {
		return OrchestrationRunChange{}, fmt.Errorf("wait must be between 0 and %d seconds", maxActionWait)
	}
	row, _, err := s.lookupOrchestration(ctx, orchestration)
	if err != nil {
		return OrchestrationRunChange{}, err
	}
	var response collectorOrchestrationRunDetails
	if err := s.client.PostJSON(ctx, "/orchestrations/"+strconv.Itoa(row.ID)+"/runs", nil, &response); err != nil {
		return OrchestrationRunChange{}, fmt.Errorf("start a run of orchestration %s: %w", row.Name, err)
	}
	if response.ID < 1 {
		return OrchestrationRunChange{}, fmt.Errorf("start a run of orchestration %s: the collector returned no run ID", row.Name)
	}
	// The run is started: a wait that fails returns it as it was answered.
	response, waited, err := s.waitRun(ctx, response, wait)
	if err != nil {
		return OrchestrationRunChange{}, err
	}
	result, err := s.runChanged(OrchestrationRunStart, response)
	result.Waited = waited
	return result, err
}

// ChangeOrchestrationRun stops a running run, resumes a failed or stopped
// one, or acknowledges its failed or stopped step, with a comment, for it to
// go on. The collector refuses with 409 a run in another status.
func (s *Service) ChangeOrchestrationRun(ctx context.Context, id int, action OrchestrationRunAction, comment string) (OrchestrationRunChange, error) {
	if id < 1 {
		return OrchestrationRunChange{}, fmt.Errorf("id must be one run ID, as list_orchestration_runs gives it")
	}
	var body any
	switch action {
	case OrchestrationRunStop, OrchestrationRunResume:
		if comment != "" {
			return OrchestrationRunChange{}, fmt.Errorf("a comment applies to the continue action only")
		}
	case OrchestrationRunContinue:
		// The collector requires the comment: an acknowledgement says why
		// the failure is accepted.
		if strings.TrimSpace(comment) == "" || len(comment) > maxOrchestrationTextBytes {
			return OrchestrationRunChange{}, fmt.Errorf("comment must say why the failure of the step is accepted, at most %d bytes", maxOrchestrationTextBytes)
		}
		body = collectorRunContinue{Comment: comment}
	default:
		return OrchestrationRunChange{}, fmt.Errorf("unsupported run action %q", action)
	}
	var response collectorOrchestrationRunDetails
	if err := s.client.PostJSON(ctx, "/orchestration_runs/"+strconv.Itoa(id)+"/"+string(action), body, &response); err != nil {
		if isNotFound(err) {
			return OrchestrationRunChange{}, fmt.Errorf("orchestration run %d: not found in the collector; list the runs with list_orchestration_runs", id)
		}
		return OrchestrationRunChange{}, fmt.Errorf("%s orchestration run %d: %w", action, id, err)
	}
	if response.ID != id {
		return OrchestrationRunChange{}, fmt.Errorf("%s orchestration run %d: unexpected collector response", action, id)
	}
	return s.runChanged(action, response)
}

// runChanged gives the run as the collector answers it after the action,
// with the first page of its targets.
func (s *Service) runChanged(action OrchestrationRunAction, response collectorOrchestrationRunDetails) (OrchestrationRunChange, error) {
	options := GetOrchestrationRunOptions{ID: response.ID}
	fingerprint := listFingerprint(url.Values{"id": {strconv.Itoa(response.ID)}, "status": {""}, "all_output": {"false"}})
	details, err := s.runDetailsOf(response, options, 0, defaultRunTargetsLimit, fingerprint)
	if err != nil {
		return OrchestrationRunChange{}, err
	}
	return OrchestrationRunChange{Action: string(action), OrchestrationRunDetails: details}, nil
}

package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"
)

const (
	defaultListOrchestrationRunsLimit = 50
	maxListOrchestrationRunsLimit     = 200
	defaultRunTargetsLimit            = 20
	maxRunTargetsLimit                = 100
)

var orchestrationRunProps = []string{
	"id", "orchestration_id", "name", "status", "current_step", "step_count", "steps_done", "message", "started", "ended", "started_by", "duration",
}

var (
	orchestrationRunStatuses = []string{"running", "succeeded", "failed", "stopped"}
	runTargetStatuses        = []string{"waiting", "queued", "running", "ok", "failed", "refused", "timed_out", "cancelled", "not_run"}
)

type OrchestrationRun struct {
	ID              int    `json:"id" jsonschema:"the collector run ID"`
	OrchestrationID *int   `json:"orchestration_id,omitempty" jsonschema:"the ID of the orchestration it ran; absent once the orchestration is deleted"`
	Name            string `json:"name" jsonschema:"the name of the orchestration when the run started; untrusted text"`
	Status          string `json:"status" jsonschema:"running, succeeded, failed or stopped; a run whose failed or stopped steps were acknowledged ends succeeded"`
	CurrentStep     int    `json:"current_step" jsonschema:"the index, from 0, of the step running, or where the run ended"`
	StepCount       int    `json:"step_count" jsonschema:"the number of its steps"`
	StepsDone       int    `json:"steps_done" jsonschema:"its steps succeeded, or failed and acknowledged"`
	Message         string `json:"message,omitempty" jsonschema:"why the run failed or was stopped; the collector numbers steps from 1 in its messages; untrusted text"`
	Started         string `json:"started,omitempty" jsonschema:"when it started, as the collector gives it"`
	Ended           string `json:"ended,omitempty" jsonschema:"when it ended, absent while it runs"`
	StartedBy       string `json:"started_by,omitempty" jsonschema:"who started it; an email address is masked"`
	DurationSeconds int    `json:"duration_seconds" jsonschema:"seconds from its start to its end, or to now while it runs"`
}

type collectorOrchestrationRun struct {
	ID              int     `json:"id"`
	OrchestrationID *int    `json:"orchestration_id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	CurrentStep     int     `json:"current_step"`
	StepCount       int     `json:"step_count"`
	StepsDone       int     `json:"steps_done"`
	Message         string  `json:"message"`
	Started         string  `json:"started"`
	Ended           *string `json:"ended"`
	StartedBy       string  `json:"started_by"`
	Duration        int     `json:"duration"`
}

type ListOrchestrationRunsOptions struct {
	// Orchestration keeps the runs of one orchestration, by ID, or by the
	// name it had, which holds for a deleted one too.
	Orchestration string
	Status        string
	StartedBy     string
	StartedSince  string
	StartedUntil  string
	Limit         int
	Cursor        string
}

type OrchestrationRunList struct {
	Provenance Provenance         `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total      int                `json:"total" jsonschema:"the number of runs matching the filters"`
	Count      int                `json:"count" jsonschema:"the number of runs returned in this page"`
	Runs       []OrchestrationRun `json:"runs" jsonschema:"the runs, the latest first"`
	NextCursor string             `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page; a run started between two pages shifts them"`
	Truncated  bool               `json:"truncated" jsonschema:"whether matching runs remain after this page"`
}

// ListOrchestrationRuns reads one page of the runs, the latest first.
func (s *Service) ListOrchestrationRuns(ctx context.Context, options ListOrchestrationRunsOptions) (OrchestrationRunList, error) {
	for name, value := range map[string]string{"orchestration": options.Orchestration, "status": options.Status, "started_by": options.StartedBy} {
		if err := validateFilterValue(name, value); err != nil {
			return OrchestrationRunList{}, err
		}
	}
	for name, value := range map[string]string{"started_since": options.StartedSince, "started_until": options.StartedUntil} {
		if err := validateCollectorDate(name, value); err != nil {
			return OrchestrationRunList{}, err
		}
	}
	if options.Status != "" && !slices.Contains(orchestrationRunStatuses, options.Status) {
		return OrchestrationRunList{}, fmt.Errorf("status must be running, succeeded, failed or stopped")
	}
	limit, err := pageLimit(options.Limit, defaultListOrchestrationRunsLimit, maxListOrchestrationRunsLimit)
	if err != nil {
		return OrchestrationRunList{}, err
	}
	var filters []string
	if options.Orchestration != "" {
		if id, err := strconv.Atoi(options.Orchestration); err == nil && id > 0 {
			filters = append(filters, filterEquals("orchestration_id", strconv.Itoa(id)))
		} else {
			filters = append(filters, filterEquals("name", options.Orchestration))
		}
	}
	if options.Status != "" {
		filters = append(filters, filterEquals("status", options.Status))
	}
	if options.StartedBy != "" {
		filters = append(filters, filterContains("started_by", options.StartedBy))
	}
	if options.StartedSince != "" {
		filters = append(filters, "started:gte:"+options.StartedSince)
	}
	if options.StartedUntil != "" {
		filters = append(filters, "started:lt:"+options.StartedUntil)
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return OrchestrationRunList{}, err
	}
	var response collectorList[collectorOrchestrationRun]
	if err := s.client.GetJSON(ctx, "/orchestration_runs", pageQuery(orchestrationRunProps, "-started,-id", filters, limit, offset), &response); err != nil {
		return OrchestrationRunList{}, fmt.Errorf("list orchestration runs: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return OrchestrationRunList{}, fmt.Errorf("list orchestration runs: unexpected collector response")
	}
	result := OrchestrationRunList{Provenance: s.newProvenance(), Total: *response.Meta.Total, Count: len(response.Data), Runs: make([]OrchestrationRun, 0, len(response.Data))}
	for _, row := range response.Data {
		result.Runs = append(result.Runs, orchestrationRunOf(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

func orchestrationRunOf(row collectorOrchestrationRun) OrchestrationRun {
	message, _ := truncateUTF8(row.Message, maxOrchestrationTextBytes)
	run := OrchestrationRun{
		ID: row.ID, OrchestrationID: row.OrchestrationID, Name: row.Name, Status: row.Status, CurrentStep: row.CurrentStep,
		StepCount: row.StepCount, StepsDone: row.StepsDone, Message: message, Started: row.Started, StartedBy: personName(row.StartedBy), DurationSeconds: row.Duration,
	}
	if row.Ended != nil {
		run.Ended = *row.Ended
	}
	return run
}

type GetOrchestrationRunOptions struct {
	ID int
	// Step keeps the targets of one step, by its index from 0.
	Step *int
	// TargetStatus keeps the targets of one status.
	TargetStatus string
	// AllOutput gives the output of the targets that succeeded too.
	AllOutput bool
	Limit     int
	Cursor    string
}

type OrchestrationRunDetails struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	OrchestrationRun
	Plan  []OrchestrationStep `json:"plan" jsonschema:"the steps of the orchestration when the run started, numbered from 0"`
	Steps []RunStep           `json:"steps" jsonschema:"the steps the run reached, numbered from 0, with the count of their targets by status"`
	// The targets of the page.
	TargetTotal int         `json:"target_total" jsonschema:"the number of targets matching step and target_status"`
	Targets     []RunTarget `json:"targets" jsonschema:"the targets of this page, by step, then position"`
	NextCursor  string      `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same id and filters for the next targets"`
	Truncated   bool        `json:"truncated" jsonschema:"whether matching targets remain after this page"`
}

type RunStep struct {
	Step       int            `json:"step" jsonschema:"the index of the step, from 0"`
	Status     string         `json:"status" jsonschema:"waiting, running, succeeded, failed, stopped, the run stopped during it, not_run, the run ended before it, or acknowledged, its failure accepted for the run to go on"`
	Counts     map[string]int `json:"counts" jsonschema:"the number of its targets by status"`
	Started    string         `json:"started,omitempty" jsonschema:"when it started"`
	Ended      string         `json:"ended,omitempty" jsonschema:"when it ended"`
	AckedBy    string         `json:"acked_by,omitempty" jsonschema:"who acknowledged its failure; an email address is masked"`
	AckedAt    string         `json:"acked_at,omitempty" jsonschema:"when its failure was acknowledged"`
	AckComment string         `json:"ack_comment,omitempty" jsonschema:"why its failure was accepted; untrusted text"`
}

type RunTarget struct {
	Step            int    `json:"step" jsonschema:"the index of its step, from 0"`
	Position        int    `json:"position" jsonschema:"its position in the step"`
	Label           string `json:"label" jsonschema:"the name of the target"`
	Status          string `json:"status" jsonschema:"waiting, queued, running, ok, failed, it returned non-zero, refused by the action queue, timed_out, cancelled, or not_run, the run stopped before it"`
	Message         string `json:"message,omitempty" jsonschema:"why it did not succeed, as the action queue or the run says; untrusted text"`
	NodeID          string `json:"node_id,omitempty" jsonschema:"the node ID of the target"`
	SvcID           string `json:"svc_id,omitempty" jsonschema:"the service ID of the target"`
	ClusterID       string `json:"cluster_id,omitempty" jsonschema:"the cluster ID of the target"`
	ActionID        *int   `json:"action_id,omitempty" jsonschema:"the ID of its action in the queue, as list_node_actions reads it"`
	Command         string `json:"command,omitempty" jsonschema:"the command of its action"`
	Queued          string `json:"queued,omitempty" jsonschema:"when its action was queued"`
	Started         string `json:"started,omitempty" jsonschema:"when it started"`
	Ended           string `json:"ended,omitempty" jsonschema:"when it ended"`
	Ret             *int   `json:"ret,omitempty" jsonschema:"the exit code of its action; 0 is a success"`
	Stdout          string `json:"stdout,omitempty" jsonschema:"the standard output of its action, at most 2000 bytes, without terminal colors, given for a target that did not succeed unless all_output; untrusted text"`
	Stderr          string `json:"stderr,omitempty" jsonschema:"the standard error, where the agent writes its log lines, at most 2000 bytes, same rule; untrusted text"`
	OutputTruncated bool   `json:"output_truncated,omitempty" jsonschema:"whether stdout or stderr was cut"`
}

type collectorOrchestrationRunDetails struct {
	collectorOrchestrationRun
	Plan  []OrchestrationStep `json:"plan"`
	Steps []struct {
		Step       int            `json:"step"`
		Status     string         `json:"status"`
		Counts     map[string]int `json:"counts"`
		Started    *string        `json:"started"`
		Ended      *string        `json:"ended"`
		AckedBy    string         `json:"acked_by"`
		AckedAt    *string        `json:"acked_at"`
		AckComment string         `json:"ack_comment"`
		Targets    []struct {
			Step      int     `json:"step"`
			Position  int     `json:"position"`
			Label     string  `json:"label"`
			Status    string  `json:"status"`
			Message   string  `json:"message"`
			NodeID    string  `json:"node_id"`
			SvcID     string  `json:"svc_id"`
			ClusterID string  `json:"cluster_id"`
			ActionID  *int    `json:"action_id"`
			Command   string  `json:"command"`
			Queued    *string `json:"queued"`
			Started   *string `json:"started"`
			Ended     *string `json:"ended"`
			Ret       *int    `json:"ret"`
			Stdout    string  `json:"stdout"`
			Stderr    string  `json:"stderr"`
		} `json:"targets"`
	} `json:"steps"`
}

// GetOrchestrationRun reads one run: its plan, its steps, and one page of
// its targets, with the output of their actions.
func (s *Service) GetOrchestrationRun(ctx context.Context, options GetOrchestrationRunOptions) (OrchestrationRunDetails, error) {
	if options.ID < 1 {
		return OrchestrationRunDetails{}, fmt.Errorf("id must be one run ID, as list_orchestration_runs gives it")
	}
	if options.Step != nil && *options.Step < 0 {
		return OrchestrationRunDetails{}, fmt.Errorf("step must be a step index, from 0")
	}
	if options.TargetStatus != "" && !slices.Contains(runTargetStatuses, options.TargetStatus) {
		return OrchestrationRunDetails{}, fmt.Errorf("target_status must be one of %v", runTargetStatuses)
	}
	limit, err := pageLimit(options.Limit, defaultRunTargetsLimit, maxRunTargetsLimit)
	if err != nil {
		return OrchestrationRunDetails{}, err
	}
	filters := url.Values{"id": {strconv.Itoa(options.ID)}, "status": {options.TargetStatus}, "all_output": {strconv.FormatBool(options.AllOutput)}}
	if options.Step != nil {
		filters.Set("step", strconv.Itoa(*options.Step))
	}
	fingerprint := listFingerprint(filters)
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return OrchestrationRunDetails{}, err
	}
	var response collectorOrchestrationRunDetails
	if err := s.client.GetJSON(ctx, "/orchestration_runs/"+strconv.Itoa(options.ID), nil, &response); err != nil {
		if isNotFound(err) {
			return OrchestrationRunDetails{}, fmt.Errorf("orchestration run %d: not found in the collector; list the runs with list_orchestration_runs", options.ID)
		}
		return OrchestrationRunDetails{}, fmt.Errorf("get orchestration run %d: %w", options.ID, err)
	}
	if response.ID != options.ID {
		return OrchestrationRunDetails{}, fmt.Errorf("get orchestration run %d: unexpected collector response", options.ID)
	}
	return s.runDetailsOf(response, options, offset, limit, fingerprint)
}

// runDetailsOf turns the detail of a run into its result, with one page of
// its targets.
func (s *Service) runDetailsOf(response collectorOrchestrationRunDetails, options GetOrchestrationRunOptions, offset int, limit int, fingerprint string) (OrchestrationRunDetails, error) {
	run := orchestrationRunOf(response.collectorOrchestrationRun)
	// The detail holds the steps of the run, not their count nor its
	// duration.
	run.StepCount = len(response.Plan)
	run.DurationSeconds = s.runDuration(response.Started, run.Ended)
	result := OrchestrationRunDetails{OrchestrationRun: run, Plan: boundSteps(response.Plan), Steps: make([]RunStep, 0, len(response.Steps)), Targets: []RunTarget{}}
	var matching []RunTarget
	for _, step := range response.Steps {
		comment, _ := truncateUTF8(step.AckComment, maxOrchestrationTextBytes)
		item := RunStep{Step: step.Step, Status: step.Status, Counts: step.Counts, Started: optionalText(step.Started), Ended: optionalText(step.Ended), AckedBy: personName(step.AckedBy), AckedAt: optionalText(step.AckedAt), AckComment: comment}
		if item.Counts == nil {
			item.Counts = map[string]int{}
		}
		if item.Status == "succeeded" || item.Status == "acknowledged" {
			result.StepsDone++
		}
		result.Steps = append(result.Steps, item)
		if options.Step != nil && step.Step != *options.Step {
			continue
		}
		for _, target := range step.Targets {
			if options.TargetStatus != "" && target.Status != options.TargetStatus {
				continue
			}
			message, _ := truncateUTF8(target.Message, maxOrchestrationTextBytes)
			item := RunTarget{
				Step: target.Step, Position: target.Position, Label: target.Label, Status: target.Status, Message: message, NodeID: target.NodeID, SvcID: target.SvcID, ClusterID: target.ClusterID,
				ActionID: target.ActionID, Command: target.Command, Queued: optionalText(target.Queued), Started: optionalText(target.Started), Ended: optionalText(target.Ended), Ret: target.Ret,
			}
			if options.AllOutput || target.Status != "ok" {
				var cutOut, cutErr bool
				item.Stdout, cutOut = truncateUTF8(ansiEscape.ReplaceAllString(target.Stdout, ""), maxActionOutputBytes)
				item.Stderr, cutErr = truncateUTF8(ansiEscape.ReplaceAllString(target.Stderr, ""), maxActionOutputBytes)
				item.OutputTruncated = cutOut || cutErr
			}
			matching = append(matching, item)
		}
	}
	slices.SortStableFunc(matching, func(a, b RunTarget) int {
		if a.Step != b.Step {
			return a.Step - b.Step
		}
		return a.Position - b.Position
	})
	result.TargetTotal = len(matching)
	if offset > len(matching) {
		return OrchestrationRunDetails{}, fmt.Errorf("cursor is invalid: pass the next_cursor of a previous call with the same id and filters")
	}
	end := min(offset+limit, len(matching))
	result.Targets = append(result.Targets, matching[offset:end]...)
	if end < len(matching) {
		result.NextCursor = encodeListCursor(end, fingerprint)
		result.Truncated = true
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// runDuration is the seconds from the start of a run to its end, or to now
// while it runs; 0 when a time cannot be read.
func (s *Service) runDuration(started string, ended string) int {
	start, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return 0
	}
	end := s.now()
	if ended != "" {
		if end, err = time.Parse(time.RFC3339, ended); err != nil {
			return 0
		}
	}
	return max(0, int(end.Sub(start).Seconds()))
}

// optionalText is the value of an optional collector text.
func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

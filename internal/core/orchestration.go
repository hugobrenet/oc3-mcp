package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultListOrchestrationsLimit = 50
	maxListOrchestrationsLimit     = 200
	// maxOrchestrationSteps bounds the steps of a draft the MCP checks.
	maxOrchestrationSteps = 50
	// maxOrchestrationTargetIDs bounds the ids a step picks, given or
	// returned.
	maxOrchestrationTargetIDs = 200
	// maxCheckTargets bounds the targets returned for each checked step.
	maxCheckTargets = 100
	// maxOrchestrationTextBytes bounds a description, a message or a
	// comment, untrusted text.
	maxOrchestrationTextBytes = 2000
)

var orchestrationProps = []string{
	"id", "name", "description", "step_count", "run_count", "last_run_id", "last_run_status", "last_run_started", "created", "updated", "updated_by",
}

// orchestrationAction is the form of an action name: the collector judges
// which ones a kind of target accepts.
var orchestrationAction = regexp.MustCompile(`^[a-z_ ]{1,64}$`)

// OrchestrationStep is one step of an orchestration: one agent action on a
// set of targets.
type OrchestrationStep struct {
	Name      string              `json:"name" jsonschema:"the name of the step; untrusted text"`
	Action    string              `json:"action" jsonschema:"the agent action, one the action queue accepts for the kind of target, such as start, stop, freeze, thaw, switch, drain, pushasset, checks or compliance_fix"`
	Kind      string              `json:"kind" jsonschema:"what the action acts on: service, a service as a whole; instance, the instance of a service on a node; node; or cluster"`
	Targets   OrchestrationTarget `json:"targets" jsonschema:"how the step names its targets"`
	Pace      string              `json:"pace" jsonschema:"how many targets run at a time: all, one, one after the other, or batch, batch_size at a time"`
	BatchSize int                 `json:"batch_size,omitempty" jsonschema:"with pace batch, the number of targets run at a time, 1 or more"`
	Timeout   int                 `json:"timeout,omitempty" jsonschema:"the seconds after which a target whose action has not ended fails, its action cancelled; 0 or omitted, no limit"`
	RID       string              `json:"rid,omitempty" jsonschema:"for kind instance only, the resources of the instance the action applies to, such as fs#1"`
}

// OrchestrationTarget is how a step names its targets.
type OrchestrationTarget struct {
	Mode         string   `json:"mode" jsonschema:"pick, the ids given; filterset, what the filterset matches when the run starts; same_as, the targets of an earlier step, of the same kind"`
	IDs          []string `json:"ids,omitempty" jsonschema:"with pick, the ids: svc_id for a service, svc_id@node_id for an instance, node_id for a node, cluster_id for a cluster; at most 200"`
	FiltersetID  int      `json:"filterset_id,omitempty" jsonschema:"with filterset, the filterset ID: its services, its nodes, the instances of its services on its nodes, or the clusters of its nodes"`
	Filterset    string   `json:"filterset,omitempty" jsonschema:"with filterset, in place of filterset_id, the exact filterset name, which the MCP resolves to its ID before sending the steps"`
	Step         *int     `json:"step,omitempty" jsonschema:"with same_as, the index of the earlier step, from 0"`
	IDsTruncated bool     `json:"ids_truncated,omitempty" jsonschema:"in a result, whether ids were left out after the first 200"`
}

type Orchestration struct {
	ID             int    `json:"id" jsonschema:"the collector orchestration ID"`
	Name           string `json:"name" jsonschema:"the name of the orchestration, unique; untrusted text"`
	Description    string `json:"description,omitempty" jsonschema:"its description, at most 2000 bytes; untrusted text"`
	StepCount      int    `json:"step_count" jsonschema:"the number of its steps"`
	RunCount       int    `json:"run_count" jsonschema:"the number of its runs"`
	LastRunID      *int   `json:"last_run_id,omitempty" jsonschema:"the ID of its last run, for get_orchestration_run; absent when never run"`
	LastRunStatus  string `json:"last_run_status,omitempty" jsonschema:"the status of its last run: running, succeeded, failed or stopped"`
	LastRunStarted string `json:"last_run_started,omitempty" jsonschema:"when its last run started, as the collector gives it"`
	Created        string `json:"created,omitempty" jsonschema:"when it was created, as the collector gives it"`
	Updated        string `json:"updated,omitempty" jsonschema:"when it was last changed, as the collector gives it"`
	UpdatedBy      string `json:"updated_by,omitempty" jsonschema:"who changed it last; an email address is masked"`
}

type collectorOrchestration struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	StepCount      int     `json:"step_count"`
	RunCount       int     `json:"run_count"`
	LastRunID      *int    `json:"last_run_id"`
	LastRunStatus  *string `json:"last_run_status"`
	LastRunStarted *string `json:"last_run_started"`
	Created        string  `json:"created"`
	Updated        string  `json:"updated"`
	UpdatedBy      string  `json:"updated_by"`
	// Steps is the JSON text of the steps the listing gives.
	Steps string `json:"steps"`
}

type ListOrchestrationsOptions struct {
	Name          string
	Action        string
	LastRunStatus string
	Limit         int
	Cursor        string
}

type OrchestrationList struct {
	Provenance     Provenance      `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total          int             `json:"total" jsonschema:"the number of orchestrations matching the filters"`
	Count          int             `json:"count" jsonschema:"the number of orchestrations returned in this page"`
	Orchestrations []Orchestration `json:"orchestrations" jsonschema:"the orchestrations, by name"`
	NextCursor     string          `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated      bool            `json:"truncated" jsonschema:"whether matching orchestrations remain after this page"`
}

type OrchestrationDetails struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Orchestration
	Steps []OrchestrationStep `json:"steps" jsonschema:"its steps, in order; a step is numbered from 0, as same_as and the runs number it"`
}

// ListOrchestrations reads one page of the orchestrations, by name.
func (s *Service) ListOrchestrations(ctx context.Context, options ListOrchestrationsOptions) (OrchestrationList, error) {
	for name, value := range map[string]string{"name": options.Name, "action": options.Action, "last_run_status": options.LastRunStatus} {
		if err := validateFilterValue(name, value); err != nil {
			return OrchestrationList{}, err
		}
	}
	if options.Action != "" && !orchestrationAction.MatchString(options.Action) {
		return OrchestrationList{}, fmt.Errorf("action must be one agent action name, such as freeze or pushasset")
	}
	limit, err := pageLimit(options.Limit, defaultListOrchestrationsLimit, maxListOrchestrationsLimit)
	if err != nil {
		return OrchestrationList{}, err
	}
	var filters []string
	if options.Name != "" {
		filters = append(filters, filterEquals("name", options.Name))
	}
	if options.Action != "" {
		// The collector reads a filter on steps in the actions of the steps,
		// not in their JSON: the action is matched as a word.
		filters = append(filters, `steps:~\b`+regexp.QuoteMeta(options.Action)+`\b`)
	}
	if options.LastRunStatus != "" {
		filters = append(filters, filterEquals("last_run_status", options.LastRunStatus))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return OrchestrationList{}, err
	}
	var response collectorList[collectorOrchestration]
	if err := s.client.GetJSON(ctx, "/orchestrations", pageQuery(orchestrationProps, "name,id", filters, limit, offset), &response); err != nil {
		return OrchestrationList{}, fmt.Errorf("list orchestrations: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return OrchestrationList{}, fmt.Errorf("list orchestrations: unexpected collector response")
	}
	result := OrchestrationList{Provenance: s.newProvenance(), Total: *response.Meta.Total, Count: len(response.Data), Orchestrations: make([]Orchestration, 0, len(response.Data))}
	for _, row := range response.Data {
		result.Orchestrations = append(result.Orchestrations, orchestrationOf(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// GetOrchestration reads one orchestration by its name or ID, with its
// steps.
func (s *Service) GetOrchestration(ctx context.Context, orchestration string) (OrchestrationDetails, error) {
	row, steps, err := s.lookupOrchestration(ctx, orchestration)
	if err != nil {
		return OrchestrationDetails{}, err
	}
	return OrchestrationDetails{Provenance: s.newProvenance(), Orchestration: orchestrationOf(row), Steps: boundSteps(steps)}, nil
}

// lookupOrchestration reads one orchestration, with its steps, by its ID or
// its name, which the collector keeps unique, in one listing call: the
// listing holds its steps, its counts and its last run.
func (s *Service) lookupOrchestration(ctx context.Context, orchestration string) (collectorOrchestration, []OrchestrationStep, error) {
	orchestration = strings.TrimSpace(orchestration)
	if orchestration == "" {
		return collectorOrchestration{}, nil, fmt.Errorf("orchestration must be one exact orchestration name or ID")
	}
	if err := validateFilterValue("orchestration", orchestration); err != nil {
		return collectorOrchestration{}, nil, err
	}
	filter := filterEquals("name", orchestration)
	if id, err := strconv.Atoi(orchestration); err == nil && id > 0 {
		filter = filterEquals("id", strconv.Itoa(id))
	}
	var found collectorList[collectorOrchestration]
	if err := s.client.GetJSON(ctx, "/orchestrations", pageQuery(append(slices.Clone(orchestrationProps), "steps"), "id", []string{filter}, 2, 0), &found); err != nil {
		return collectorOrchestration{}, nil, fmt.Errorf("get orchestration %s: %w", orchestration, err)
	}
	switch len(found.Data) {
	case 0:
		return collectorOrchestration{}, nil, fmt.Errorf("orchestration %s: not found in the collector; list the orchestrations with list_orchestrations", orchestration)
	case 1:
	default:
		ids := make([]string, 0, len(found.Data))
		for _, row := range found.Data {
			ids = append(ids, strconv.Itoa(row.ID))
		}
		return collectorOrchestration{}, nil, fmt.Errorf("orchestration %s matches several orchestrations, IDs %s: pass the ID", orchestration, strings.Join(ids, ", "))
	}
	row := found.Data[0]
	var steps []OrchestrationStep
	if err := json.Unmarshal([]byte(row.Steps), &steps); err != nil {
		return collectorOrchestration{}, nil, fmt.Errorf("get orchestration %s: unexpected collector steps", orchestration)
	}
	return row, steps, nil
}

func orchestrationOf(row collectorOrchestration) Orchestration {
	description, _ := truncateUTF8(row.Description, maxOrchestrationTextBytes)
	result := Orchestration{
		ID: row.ID, Name: row.Name, Description: description, StepCount: row.StepCount, RunCount: row.RunCount, LastRunID: row.LastRunID,
		Created: row.Created, Updated: row.Updated, UpdatedBy: personName(row.UpdatedBy),
	}
	if row.LastRunStatus != nil {
		result.LastRunStatus = *row.LastRunStatus
	}
	if row.LastRunStarted != nil {
		result.LastRunStarted = *row.LastRunStarted
	}
	return result
}

// boundSteps bounds the ids each step picks.
func boundSteps(steps []OrchestrationStep) []OrchestrationStep {
	result := make([]OrchestrationStep, 0, len(steps))
	for _, step := range steps {
		if len(step.Targets.IDs) > maxOrchestrationTargetIDs {
			step.Targets.IDs = slices.Clone(step.Targets.IDs[:maxOrchestrationTargetIDs])
			step.Targets.IDsTruncated = true
		}
		result = append(result, step)
	}
	return result
}

type CheckOrchestrationOptions struct {
	// Orchestration names a saved orchestration, by name or ID; Steps a
	// draft. One of the two.
	Orchestration string
	Steps         []OrchestrationStep
}

type OrchestrationCheck struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	// Orchestration is the saved orchestration checked, absent for a draft.
	Orchestration *Orchestration `json:"orchestration,omitempty" jsonschema:"the saved orchestration checked; absent for a draft"`
	Valid         bool           `json:"valid" jsonschema:"whether every step is valid and every target would be accepted, as a run starting now would find them"`
	Steps         []CheckedStep  `json:"steps" jsonschema:"each step, numbered from 0, with what the collector finds"`
}

type CheckedStep struct {
	Step             int             `json:"step" jsonschema:"the index of the step, from 0"`
	Name             string          `json:"name,omitempty" jsonschema:"the name of the step; untrusted text"`
	Action           string          `json:"action" jsonschema:"the action of the step"`
	Kind             string          `json:"kind" jsonschema:"the kind of its targets"`
	Message          string          `json:"message,omitempty" jsonschema:"what makes the step itself invalid, such as an action the kind does not accept; empty when valid. The collector numbers steps from 1 in its messages"`
	TargetCount      int             `json:"target_count" jsonschema:"the number of targets the step resolves to"`
	RefusedCount     int             `json:"refused_count" jsonschema:"the number of those the action queue would refuse"`
	Targets          []CheckedTarget `json:"targets" jsonschema:"the targets, those refused first, at most 100"`
	TargetsTruncated bool            `json:"targets_truncated,omitempty" jsonschema:"whether targets were left out after the first 100"`
}

type CheckedTarget struct {
	Label     string `json:"label" jsonschema:"the name of the target, as a run shows it"`
	OK        bool   `json:"ok" jsonschema:"whether the action queue would accept the target: the action fits its kind, the target exists and an agent can be reached for it"`
	Message   string `json:"message,omitempty" jsonschema:"why the action queue would refuse the target"`
	NodeID    string `json:"node_id,omitempty" jsonschema:"the node ID of the target, for a node or an instance"`
	SvcID     string `json:"svc_id,omitempty" jsonschema:"the service ID of the target, for a service or an instance"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"the cluster ID of the target, for a cluster"`
}

type collectorOrchestrationInput struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Steps       []OrchestrationStep `json:"steps"`
}

type collectorOrchestrationCheck struct {
	Data []struct {
		Step    int    `json:"step"`
		Message string `json:"message"`
		Targets []struct {
			Label     string `json:"label"`
			OK        bool   `json:"ok"`
			Message   string `json:"message"`
			NodeID    string `json:"node_id"`
			SvcID     string `json:"svc_id"`
			ClusterID string `json:"cluster_id"`
		} `json:"targets"`
	} `json:"data"`
}

// CheckOrchestration resolves the targets of each step of a saved
// orchestration, or of a draft, as a run starting now would, and checks
// each one as the action queue would. The collector stores nothing.
func (s *Service) CheckOrchestration(ctx context.Context, options CheckOrchestrationOptions) (OrchestrationCheck, error) {
	if (options.Orchestration == "") == (len(options.Steps) == 0) {
		return OrchestrationCheck{}, fmt.Errorf("pass either orchestration, a saved one, or steps, a draft")
	}
	result := OrchestrationCheck{}
	input := collectorOrchestrationInput{Name: "draft", Steps: options.Steps}
	if options.Orchestration != "" {
		row, steps, err := s.lookupOrchestration(ctx, options.Orchestration)
		if err != nil {
			return OrchestrationCheck{}, err
		}
		saved := orchestrationOf(row)
		result.Orchestration = &saved
		input = collectorOrchestrationInput{Name: row.Name, Description: row.Description, Steps: steps}
	} else {
		if err := validateDraftSteps(options.Steps); err != nil {
			return OrchestrationCheck{}, err
		}
		steps, err := s.resolveStepFiltersets(ctx, options.Steps)
		if err != nil {
			return OrchestrationCheck{}, err
		}
		input.Steps = steps
	}
	var response collectorOrchestrationCheck
	if err := s.client.PostJSON(ctx, "/orchestrations/check", input, &response); err != nil {
		return OrchestrationCheck{}, fmt.Errorf("check orchestration: %w", err)
	}
	if len(response.Data) > len(input.Steps) {
		return OrchestrationCheck{}, fmt.Errorf("check orchestration: unexpected collector response")
	}
	result.Valid = len(response.Data) == len(input.Steps)
	result.Steps = make([]CheckedStep, 0, len(response.Data))
	for _, checked := range response.Data {
		if checked.Step < 0 || checked.Step >= len(input.Steps) {
			return OrchestrationCheck{}, fmt.Errorf("check orchestration: unexpected collector response")
		}
		step := input.Steps[checked.Step]
		message, _ := truncateUTF8(checked.Message, maxOrchestrationTextBytes)
		item := CheckedStep{Step: checked.Step, Name: step.Name, Action: step.Action, Kind: step.Kind, Message: message, TargetCount: len(checked.Targets), Targets: []CheckedTarget{}}
		targets := make([]CheckedTarget, 0, len(checked.Targets))
		for _, target := range checked.Targets {
			targetMessage, _ := truncateUTF8(target.Message, maxOrchestrationTextBytes)
			targets = append(targets, CheckedTarget{Label: target.Label, OK: target.OK, Message: targetMessage, NodeID: target.NodeID, SvcID: target.SvcID, ClusterID: target.ClusterID})
			if !target.OK {
				item.RefusedCount++
			}
		}
		// The refused targets first: they are what the check is read for.
		slices.SortStableFunc(targets, func(a, b CheckedTarget) int {
			switch {
			case a.OK == b.OK:
				return 0
			case !a.OK:
				return -1
			default:
				return 1
			}
		})
		if len(targets) > maxCheckTargets {
			targets = targets[:maxCheckTargets]
			item.TargetsTruncated = true
		}
		item.Targets = targets
		if item.Message != "" || item.RefusedCount > 0 {
			result.Valid = false
		}
		result.Steps = append(result.Steps, item)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// resolveStepFiltersets gives the steps that name a filterset by name its
// ID, the only form the collector takes, each name read once.
func (s *Service) resolveStepFiltersets(ctx context.Context, steps []OrchestrationStep) ([]OrchestrationStep, error) {
	resolved := slices.Clone(steps)
	ids := map[string]int{}
	for index := range resolved {
		targets := &resolved[index].Targets
		if targets.Filterset == "" {
			continue
		}
		id, ok := ids[targets.Filterset]
		if !ok {
			if err := validateFilterValue("filterset", targets.Filterset); err != nil {
				return nil, fmt.Errorf("step %d: %w", index, err)
			}
			row, err := s.lookupFilterset(ctx, targets.Filterset)
			if err != nil {
				return nil, fmt.Errorf("step %d: %w", index, err)
			}
			id = row.ID
			ids[targets.Filterset] = id
		}
		if targets.FiltersetID != 0 && targets.FiltersetID != id {
			return nil, fmt.Errorf("step %d: filterset %s is ID %d, not filterset_id %d: pass one of the two", index, targets.Filterset, id, targets.FiltersetID)
		}
		targets.FiltersetID, targets.Filterset = id, ""
	}
	return resolved, nil
}

// validateDraftSteps bounds a draft; the collector judges its steps.
func validateDraftSteps(steps []OrchestrationStep) error {
	if len(steps) > maxOrchestrationSteps {
		return fmt.Errorf("at most %d steps are accepted", maxOrchestrationSteps)
	}
	for index, step := range steps {
		if len(step.Targets.IDs) > maxOrchestrationTargetIDs {
			return fmt.Errorf("step %d: at most %d ids are accepted", index, maxOrchestrationTargetIDs)
		}
		if step.Targets.IDsTruncated {
			return fmt.Errorf("step %d: ids_truncated is a result field", index)
		}
		for _, text := range []string{step.Name, step.Action, step.Kind, step.Pace, step.RID, step.Targets.Mode} {
			if len(text) > maxFilterValueBytes {
				return fmt.Errorf("step %d: a field exceeds %d bytes", index, maxFilterValueBytes)
			}
		}
	}
	return nil
}

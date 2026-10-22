package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	OrchestrationCreated   = "created"
	OrchestrationChanged   = "changed"
	OrchestrationUnchanged = "unchanged"
	OrchestrationDeleted   = "deleted"
)

// OrchestrationFields are the fields of an orchestration a change sets; a
// nil field keeps its value, and steps replace the whole list.
type OrchestrationFields struct {
	Name        *string
	Description *string
	Steps       []OrchestrationStep
}

type OrchestrationWrite struct {
	Provenance    Provenance            `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome       string                `json:"outcome" jsonschema:"created, changed, unchanged, when the values given were those held, or deleted"`
	Orchestration *OrchestrationDetails `json:"orchestration,omitempty" jsonschema:"the orchestration as the collector holds it after the change, or as it was before a deletion"`
	// Check is what a run starting now would find, read after a creation or
	// a change: the collector saves an orchestration whose targets do not
	// exist or cannot be reached.
	Check   *OrchestrationCheck `json:"check,omitempty" jsonschema:"after a creation or a change, the check of the orchestration as saved: the steps and targets a run starting now would refuse, which the collector saves all the same"`
	Warning string              `json:"warning,omitempty" jsonschema:"what could not be read back after the change, which is made, or a run that goes on after a deletion"`
}

type collectorOrchestrationSaved struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// CreateOrchestration creates an orchestration, then reads it back and
// checks it. The collector checks the steps as a whole, and refuses a name
// taken with 409.
func (s *Service) CreateOrchestration(ctx context.Context, name string, description *string, steps []OrchestrationStep) (OrchestrationWrite, error) {
	if strings.TrimSpace(name) == "" || len(name) > maxFilterValueBytes {
		return OrchestrationWrite{}, fmt.Errorf("name must be the name of the orchestration, at most %d bytes", maxFilterValueBytes)
	}
	if len(steps) == 0 {
		return OrchestrationWrite{}, fmt.Errorf("steps must hold at least one step")
	}
	if err := validateDraftSteps(steps); err != nil {
		return OrchestrationWrite{}, err
	}
	input := collectorOrchestrationInput{Name: name, Steps: steps}
	if description != nil {
		input.Description = *description
	}
	var saved collectorOrchestrationSaved
	if err := s.client.PostJSON(ctx, "/orchestrations", input, &saved); err != nil {
		return OrchestrationWrite{}, fmt.Errorf("create orchestration %s: %w", name, err)
	}
	if saved.ID < 1 {
		return OrchestrationWrite{}, fmt.Errorf("create orchestration %s: the collector returned no orchestration ID", name)
	}
	return s.orchestrationWritten(ctx, OrchestrationCreated, saved.ID), nil
}

// UpdateOrchestration changes an orchestration. The collector replaces the
// name, the description and the steps together: the MCP reads them first
// and sends the values not given as they are. The runs keep the plan they
// started with.
func (s *Service) UpdateOrchestration(ctx context.Context, orchestration string, fields OrchestrationFields) (OrchestrationWrite, error) {
	if fields.Name == nil && fields.Description == nil && fields.Steps == nil {
		return OrchestrationWrite{}, fmt.Errorf("pass at least one of name, description and steps")
	}
	if fields.Name != nil && (strings.TrimSpace(*fields.Name) == "" || len(*fields.Name) > maxFilterValueBytes) {
		return OrchestrationWrite{}, fmt.Errorf("name must be the new name of the orchestration, at most %d bytes", maxFilterValueBytes)
	}
	if fields.Steps != nil {
		if len(fields.Steps) == 0 {
			return OrchestrationWrite{}, fmt.Errorf("steps must hold at least one step")
		}
		if err := validateDraftSteps(fields.Steps); err != nil {
			return OrchestrationWrite{}, err
		}
	}
	before, steps, err := s.lookupOrchestration(ctx, orchestration)
	if err != nil {
		return OrchestrationWrite{}, err
	}
	input := collectorOrchestrationInput{Name: before.Name, Description: before.Description, Steps: steps}
	if fields.Name != nil {
		input.Name = *fields.Name
	}
	if fields.Description != nil {
		input.Description = *fields.Description
	}
	if fields.Steps != nil {
		input.Steps = fields.Steps
	}
	unchanged := input.Name == before.Name && input.Description == before.Description && sameSteps(input.Steps, steps)
	var saved collectorOrchestrationSaved
	if err := s.client.PutJSON(ctx, "/orchestrations/"+strconv.Itoa(before.ID), input, &saved); err != nil {
		return OrchestrationWrite{}, fmt.Errorf("update orchestration %s: %w", before.Name, err)
	}
	outcome := OrchestrationChanged
	if unchanged {
		outcome = OrchestrationUnchanged
	}
	return s.orchestrationWritten(ctx, outcome, before.ID), nil
}

// DeleteOrchestration deletes an orchestration. Its runs stay, with the plan
// they ran; a run running goes on. It is read first, for the result to tell
// what it was.
func (s *Service) DeleteOrchestration(ctx context.Context, orchestration string) (OrchestrationWrite, error) {
	before, steps, err := s.lookupOrchestration(ctx, orchestration)
	if err != nil {
		return OrchestrationWrite{}, err
	}
	var response any
	if err := s.client.DeleteJSON(ctx, "/orchestrations/"+strconv.Itoa(before.ID), nil, &response); err != nil {
		return OrchestrationWrite{}, fmt.Errorf("delete orchestration %s: %w", before.Name, err)
	}
	details := OrchestrationDetails{Orchestration: orchestrationOf(before), Steps: boundSteps(steps)}
	result := OrchestrationWrite{Provenance: s.newProvenance(), Outcome: OrchestrationDeleted, Orchestration: &details}
	if details.LastRunStatus == "running" && details.LastRunID != nil {
		result.Warning = fmt.Sprintf("its run %d is running: it goes on with the plan it started with", *details.LastRunID)
	}
	return result, nil
}

// orchestrationWritten reads an orchestration back after a change, which is
// made, and checks it: a read that fails is told, not returned as an error.
func (s *Service) orchestrationWritten(ctx context.Context, outcome string, id int) OrchestrationWrite {
	result := OrchestrationWrite{Outcome: outcome}
	details, err := s.GetOrchestration(ctx, strconv.Itoa(id))
	if err != nil {
		result.Warning = fmt.Sprintf("the orchestration could not be read back: %v", err)
		result.Provenance = s.newProvenance()
		return result
	}
	result.Orchestration = &details
	check, err := s.CheckOrchestration(ctx, CheckOrchestrationOptions{Orchestration: strconv.Itoa(id)})
	if err != nil {
		result.Warning = fmt.Sprintf("the orchestration could not be checked: %v", err)
	} else {
		check.Orchestration = nil
		result.Check = &check
	}
	result.Provenance = s.newProvenance()
	return result
}

// sameSteps says whether two lists of steps hold the same values.
func sameSteps(a, b []OrchestrationStep) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

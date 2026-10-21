package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// maxFormGroupChanges bounds the groups one call attaches to or detaches
// from a form.
const maxFormGroupChanges = 50

// Outcomes of a change of a form.
const (
	FormCreated   = "created"
	FormChanged   = "changed"
	FormUnchanged = "unchanged"
	FormDeleted   = "deleted"
	// FormRolledBack is a definition restored at an earlier revision.
	FormRolledBack = "rolled_back"
)

type CreateFormOptions struct {
	Name   string
	YAML   string
	Folder string
	Type   string
}

// UpdateFormOptions change the fields given of a form, the others kept.
type UpdateFormOptions struct {
	Form   string
	Name   *string
	YAML   *string
	Folder *string
	Type   *string
}

type FormWrite struct {
	Provenance Provenance   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome    string       `json:"outcome" jsonschema:"created, changed, unchanged when the collector found nothing to change, rolled_back, or deleted"`
	ID         int          `json:"id" jsonschema:"the collector form ID"`
	Name       string       `json:"name" jsonschema:"the form name"`
	Form       *FormDetails `json:"read_back,omitempty" jsonschema:"with create_form and update_form, the form read back as get_form gives it: its inputs and outputs as the collector reads the definition, its groups and its definition text"`
	Warning    string       `json:"warning,omitempty" jsonschema:"why the form read back is not what was asked, the change being made: not read back, or a definition in which the collector reads no form; tell the user"`
}

type FormGroupOptions struct {
	Form         string
	Publications []string
	Responsibles []string
}

type FormGroupChange struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Form       FormReference     `json:"form" jsonschema:"the form"`
	Changed    int               `json:"changed" jsonschema:"the number of groups attached or detached"`
	Unchanged  int               `json:"unchanged" jsonschema:"the number of groups already as asked"`
	Refused    int               `json:"refused" jsonschema:"the number of groups refused by the collector or not found"`
	Groups     []FormGroupResult `json:"groups" jsonschema:"the outcome on each group, publications first, in the order given"`
	// Responsibles are read back after a detachment of responsibles.
	Responsibles []string `json:"responsibles,omitempty" jsonschema:"with detach_form_groups of responsibles, the groups still responsible for the form, read back"`
	Warning      string   `json:"warning,omitempty" jsonschema:"with detach_form_groups, when no group is responsible for the form any more: only a Manager may then change, publish or delete it; tell the user"`
}

type FormGroupResult struct {
	Group   string `json:"group" jsonschema:"the group, as given"`
	Role    string `json:"role" jsonschema:"publication: its members may submit the form; responsible: its members may change it"`
	GroupID int    `json:"group_id,omitempty" jsonschema:"the collector group ID"`
	Outcome string `json:"outcome" jsonschema:"attached, already_attached, detached, already_detached or refused"`
	Reason  string `json:"reason,omitempty" jsonschema:"with refused, why"`
}

// CreateForm creates a form as the collector takes it: its name, its
// definition text, which the collector only checks to be YAML, its folder
// and type. The collector publishes it to the default group of the caller
// and makes that group responsible for it. The form is read back as
// get_form gives it.
func (s *Service) CreateForm(ctx context.Context, options CreateFormOptions) (FormWrite, error) {
	if options.Name == "" {
		return FormWrite{}, fmt.Errorf("name must be the name of the form")
	}
	body := map[string]any{"form_name": options.Name, "form_yaml": options.YAML}
	for key, value := range map[string]string{"form_folder": options.Folder, "form_type": options.Type} {
		if value != "" {
			body[key] = value
		}
	}
	var response collectorList[collectorForm]
	if err := s.client.PostJSON(ctx, "/forms", body, &response); err != nil {
		return FormWrite{}, fmt.Errorf("create form %s: %w", options.Name, err)
	}
	if len(response.Data) != 1 || response.Data[0].ID <= 0 {
		return FormWrite{}, fmt.Errorf("create form %s: unexpected collector response; list_forms tells whether it was created", options.Name)
	}
	return s.formWritten(ctx, FormCreated, response.Data[0].ID, options.Name), nil
}

// UpdateForm changes the fields given of a form, the others kept, and reads
// it back as get_form gives it.
func (s *Service) UpdateForm(ctx context.Context, options UpdateFormOptions) (FormWrite, error) {
	body := map[string]any{}
	for key, value := range map[string]*string{"form_name": options.Name, "form_yaml": options.YAML, "form_folder": options.Folder, "form_type": options.Type} {
		if value != nil {
			body[key] = *value
		}
	}
	if len(body) == 0 {
		return FormWrite{}, fmt.Errorf("pass at least one of name, yaml, folder or type to change")
	}
	form, err := s.findForm(ctx, options.Form, []string{"id", "form_name"})
	if err != nil {
		return FormWrite{}, err
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.PostJSON(ctx, "/forms/"+strconv.Itoa(form.ID), body, &response); err != nil {
		return FormWrite{}, fmt.Errorf("update form %s: %w", form.FormName, err)
	}
	// The collector lists the changes after the colon of its message: none
	// when the values given were those of the form.
	outcome := FormChanged
	if _, changes, ok := strings.Cut(response.Info, " change: "); ok && strings.TrimSpace(changes) == "" {
		outcome = FormUnchanged
	}
	name := form.FormName
	if options.Name != nil {
		name = *options.Name
	}
	return s.formWritten(ctx, outcome, form.ID, name), nil
}

// RollbackForm restores the definition of a form at an earlier revision:
// the collector records a new revision for it, and the form is read back as
// get_form gives it.
func (s *Service) RollbackForm(ctx context.Context, form, revision string) (FormWrite, error) {
	if !formRevisionPattern.MatchString(revision) {
		return FormWrite{}, fmt.Errorf("revision must be a revision, as list_form_revisions gives it")
	}
	found, err := s.findForm(ctx, form, []string{"id", "form_name"})
	if err != nil {
		return FormWrite{}, err
	}
	// The collector answers with nothing.
	var response any
	if err := s.client.PostJSON(ctx, "/forms/"+strconv.Itoa(found.ID)+"/rollback/"+revision, nil, &response); err != nil {
		return FormWrite{}, fmt.Errorf("roll form %s back to %s: %w", found.FormName, revision, err)
	}
	return s.formWritten(ctx, FormRolledBack, found.ID, found.FormName), nil
}

// DeleteForm deletes a form, with its publications and responsibles.
func (s *Service) DeleteForm(ctx context.Context, form string) (FormWrite, error) {
	found, err := s.findForm(ctx, form, []string{"id", "form_name"})
	if err != nil {
		return FormWrite{}, err
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, "/forms/"+strconv.Itoa(found.ID), nil, &response); err != nil {
		return FormWrite{}, fmt.Errorf("delete form %s: %w", found.FormName, err)
	}
	return FormWrite{Provenance: s.newProvenance(), Outcome: FormDeleted, ID: found.ID, Name: found.FormName}, nil
}

// formWritten reads a form back after a change, which is made: a read back
// that fails is told, not returned as an error.
func (s *Service) formWritten(ctx context.Context, outcome string, id int, name string) FormWrite {
	result := FormWrite{Outcome: outcome, ID: id, Name: name}
	form, err := s.GetForm(ctx, strconv.Itoa(id))
	if err != nil {
		result.Warning = fmt.Sprintf("the form was %s, but could not be read back: %v; get_form reads it", outcome, err)
	} else {
		result.Form = &form
		result.Name = form.Form.Name
		// YAML that is no form, such as a scalar or a list, is stored all
		// the same: the form then has no inputs nor outputs.
		if form.Form.DefinitionUnavailable {
			result.Warning = "the collector stored the definition, but reads no form in it: the form has no inputs nor outputs, and does nothing on submission; check the YAML"
		}
	}
	result.Provenance = s.newProvenance()
	return result
}

// AttachFormGroups publishes a form to groups, and makes groups responsible
// for it, one request per group, as the collector takes them.
func (s *Service) AttachFormGroups(ctx context.Context, options FormGroupOptions) (FormGroupChange, error) {
	return s.changeFormGroups(ctx, options, true)
}

// DetachFormGroups unpublishes a form from groups, and withdraws groups from
// its responsibles, one request per group.
func (s *Service) DetachFormGroups(ctx context.Context, options FormGroupOptions) (FormGroupChange, error) {
	return s.changeFormGroups(ctx, options, false)
}

func (s *Service) changeFormGroups(ctx context.Context, options FormGroupOptions, attach bool) (FormGroupChange, error) {
	total := len(options.Publications) + len(options.Responsibles)
	if total == 0 {
		return FormGroupChange{}, fmt.Errorf("pass publications or responsibles, the groups to change")
	}
	if total > maxFormGroupChanges {
		return FormGroupChange{}, fmt.Errorf("pass at most %d groups", maxFormGroupChanges)
	}
	form, err := s.findForm(ctx, options.Form, []string{"id", "form_name"})
	if err != nil {
		return FormGroupChange{}, err
	}
	result := FormGroupChange{Form: FormReference{ID: form.ID, Name: form.FormName}, Groups: []FormGroupResult{}}
	for _, team := range []struct {
		role, path string
		groups     []string
	}{{"publication", "publications", options.Publications}, {"responsible", "responsibles", options.Responsibles}} {
		for _, name := range team.groups {
			change := FormGroupResult{Group: name, Role: team.role}
			group, err := s.lookupGroup(ctx, name)
			if err != nil {
				change.Outcome, change.Reason = ChangeRefused, err.Error()
			} else {
				change.GroupID = group.ID
				path := "/forms/" + strconv.Itoa(form.ID) + "/" + team.path + "/" + url.PathEscape(strconv.Itoa(group.ID))
				var response struct {
					Info string `json:"info"`
				}
				if attach {
					err = s.client.PostJSON(ctx, path, nil, &response)
				} else {
					err = s.client.DeleteJSON(ctx, path, nil, &response)
				}
				change.Outcome, change.Reason = formGroupOutcome(err, response.Info, attach)
			}
			switch change.Outcome {
			case ChangeAttached, ChangeDetached:
				result.Changed++
			case ChangeAlreadyAttached, ChangeAlreadyDetached:
				result.Unchanged++
			default:
				result.Refused++
			}
			result.Groups = append(result.Groups, change)
		}
	}
	// A form without responsible group is changed by a Manager only: the
	// groups left are read back after a detachment of responsibles.
	if !attach && len(options.Responsibles) > 0 {
		var groups collectorList[struct {
			Role string `json:"role"`
		}]
		path := "/forms/" + strconv.Itoa(form.ID) + "/responsibles"
		if err := s.client.GetJSON(ctx, path, url.Values{"props": {"role"}, "limit": {strconv.Itoa(maxFormGroups + 1)}}, &groups); err == nil {
			for _, group := range groups.Data {
				result.Responsibles = append(result.Responsibles, group.Role)
			}
			if len(groups.Data) == 0 {
				result.Warning = fmt.Sprintf("no group is responsible for form %s any more: only a Manager may change, publish or delete it", form.FormName)
			}
		}
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// formGroupOutcome reads the answer of the collector to a change of the
// groups of a form, which answers a group already as asked with 200 and a
// message saying so: "already published", "already removed".
func formGroupOutcome(err error, info string, attach bool) (outcome, reason string) {
	already := strings.Contains(strings.ToLower(info), " already ")
	switch {
	case err != nil:
		return ChangeRefused, err.Error()
	case attach && already:
		return ChangeAlreadyAttached, ""
	case attach:
		return ChangeAttached, ""
	case already:
		return ChangeAlreadyDetached, ""
	}
	return ChangeDetached, ""
}

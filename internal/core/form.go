package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const (
	defaultListFormsLimit = 50
	maxListFormsLimit     = 200
	maxFormDescription    = 512
)

var formProps = []string{"id", "form_name", "form_type", "form_folder", "form_author", "form_created", "form_definition"}

// formTypes are the form types the collector holds: obj forms set compliance
// variables, the others start requests or run scripts, and folder ones group
// forms.
var formTypes = []string{"obj", "custo", "generic", "folder"}

type ListFormsOptions struct {
	Name   string
	Type   string
	Folder string
	Limit  int
	Cursor string
}

type FormList struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total      int        `json:"total" jsonschema:"the number of forms matching the filters, as the collector counts them"`
	Count      int        `json:"count" jsonschema:"the number of forms returned in this page"`
	Forms      []Form     `json:"forms" jsonschema:"the forms sorted by folder then name"`
	NextCursor string     `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool       `json:"truncated" jsonschema:"whether matching forms remain after this page"`
}

type Form struct {
	ID          int        `json:"id" jsonschema:"the collector form ID"`
	Name        string     `json:"name" jsonschema:"the form name, as requests give it"`
	Label       string     `json:"label,omitempty" jsonschema:"the title of the form; untrusted text"`
	Description string     `json:"description,omitempty" jsonschema:"what the form is for, cut to 512 bytes; untrusted text"`
	Type        string     `json:"type,omitempty" jsonschema:"the form type: obj for a compliance variable form, custo or generic for a form starting a request or running a script, folder for a folder"`
	Folder      string     `json:"folder,omitempty" jsonschema:"the folder of the form, such as /compliance or /samples"`
	Author      string     `json:"author,omitempty" jsonschema:"who wrote the form"`
	Created     string     `json:"created,omitempty" jsonschema:"when the form was created, as the collector gives it"`
	Inputs      *int       `json:"inputs,omitempty" jsonschema:"the number of inputs of the form; absent when its definition is unavailable"`
	Outputs     []string   `json:"outputs,omitempty" jsonschema:"what a submission does, by output: workflow starts or goes on with a request, compliance variable sets a compliance variable, script, rest, mail or db run an action"`
	NextSteps   []FormNext `json:"next_steps,omitempty" jsonschema:"the steps a workflow output asks for next, each with its condition"`
	// DefinitionUnavailable tells a form whose definition the collector could
	// not read from apart from a form without inputs.
	DefinitionUnavailable bool `json:"definition_unavailable,omitempty" jsonschema:"whether the collector could not read the definition of the form: its inputs, outputs and next steps are then unknown, not empty"`
}

// FormNext is a step a workflow output asks for after a submission.
type FormNext struct {
	Condition string   `json:"condition" jsonschema:"when this step comes: always, on_success or on_error of the script the output runs"`
	Forms     []string `json:"forms,omitempty" jsonschema:"the forms to submit next; none closes the request"`
	Assignee  string   `json:"assignee,omitempty" jsonschema:"the team or user the next step is assigned to"`
}

type collectorForm struct {
	ID             int             `json:"id"`
	FormName       string          `json:"form_name"`
	FormType       string          `json:"form_type"`
	FormFolder     string          `json:"form_folder"`
	FormAuthor     string          `json:"form_author"`
	FormCreated    string          `json:"form_created"`
	FormDefinition json.RawMessage `json:"form_definition"`
}

type collectorFormOutputs struct {
	Label   string           `json:"Label"`
	Desc    string           `json:"Desc"`
	Inputs  []map[string]any `json:"Inputs"`
	Outputs []struct {
		Dest string `json:"Dest"`
		collectorFormNext
		// Scripts, when present, replace the direct next step: the collector
		// takes the Success or the Error one by the return code of the script.
		Scripts *struct {
			Success *collectorFormNext `json:"Success"`
			Error   *collectorFormNext `json:"Error"`
		} `json:"Scripts"`
	} `json:"Outputs"`
}

type collectorFormNext struct {
	NextForms    []any  `json:"NextForms"`
	NextAssignee string `json:"NextAssignee"`
}

// next is the next step of a branch; a branch the definition leaves out, n
// nil, has no form: it closes the request.
func (n *collectorFormNext) next(condition string) FormNext {
	step := FormNext{Condition: condition}
	if n == nil {
		return step
	}
	for _, form := range n.NextForms {
		if name, ok := form.(string); ok && name != "" && !slices.Contains(step.Forms, name) {
			step.Forms = append(step.Forms, name)
		}
	}
	step.Assignee = strings.TrimSpace(n.NextAssignee)
	return step
}

// ListForms reads one page of the forms the caller may use or see: those
// published to one of its groups, every form for a manager.
func (s *Service) ListForms(ctx context.Context, options ListFormsOptions) (FormList, error) {
	for name, value := range map[string]string{"name": options.Name, "folder": options.Folder} {
		if err := validateFilterValue(name, value); err != nil {
			return FormList{}, err
		}
	}
	if options.Type != "" && !slices.Contains(formTypes, options.Type) {
		return FormList{}, fmt.Errorf("type must be one of %s", strings.Join(formTypes, ", "))
	}
	limit, err := pageLimit(options.Limit, defaultListFormsLimit, maxListFormsLimit)
	if err != nil {
		return FormList{}, err
	}
	var filters []string
	if options.Name != "" {
		filters = append(filters, filterContains("form_name", options.Name))
	}
	if options.Type != "" {
		filters = append(filters, filterEquals("form_type", options.Type))
	}
	if options.Folder != "" {
		filters = append(filters, filterEquals("form_folder", options.Folder))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return FormList{}, err
	}

	var response collectorList[collectorForm]
	if err := s.client.GetJSON(ctx, "/forms", pageQuery(formProps, "form_folder,form_name,id", filters, limit, offset), &response); err != nil {
		return FormList{}, fmt.Errorf("list forms: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return FormList{}, fmt.Errorf("list forms: unexpected collector response")
	}
	result := FormList{Provenance: s.newProvenance(), Total: *response.Meta.Total, Count: len(response.Data), Forms: make([]Form, 0, len(response.Data))}
	for _, row := range response.Data {
		form := Form{
			ID: row.ID, Name: row.FormName, Type: row.FormType, Folder: row.FormFolder,
			Author: strings.TrimSpace(row.FormAuthor), Created: row.FormCreated,
		}
		// The collector leaves form_definition out when it cannot read the
		// YAML: the form is then unknown, not empty.
		var definition collectorFormOutputs
		if len(row.FormDefinition) == 0 || string(row.FormDefinition) == "null" || json.Unmarshal(row.FormDefinition, &definition) != nil {
			form.DefinitionUnavailable = true
			result.Forms = append(result.Forms, form)
			continue
		}
		form.Label, _ = truncateUTF8(definition.Label, maxRequestLabel)
		form.Description, _ = truncateUTF8(strings.TrimSpace(definition.Desc), maxFormDescription)
		inputs := len(definition.Inputs)
		form.Inputs = &inputs
		for _, output := range definition.Outputs {
			if output.Dest != "" && !slices.Contains(form.Outputs, output.Dest) {
				form.Outputs = append(form.Outputs, output.Dest)
			}
			if output.Dest != "workflow" {
				continue
			}
			var steps []FormNext
			if output.Scripts != nil {
				steps = []FormNext{output.Scripts.Success.next("on_success"), output.Scripts.Error.next("on_error")}
			} else {
				steps = []FormNext{output.collectorFormNext.next("always")}
			}
			for _, step := range steps {
				if !slices.ContainsFunc(form.NextSteps, func(other FormNext) bool { return sameFormNext(other, step) }) {
					form.NextSteps = append(form.NextSteps, step)
				}
			}
		}
		result.Forms = append(result.Forms, form)
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

func sameFormNext(a, b FormNext) bool {
	return a.Condition == b.Condition && a.Assignee == b.Assignee && slices.Equal(a.Forms, b.Forms)
}

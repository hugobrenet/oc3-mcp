package core

import (
	"bytes"
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
	Text   string
	Name   string
	Type   string
	Folder string
	Limit  int
	Cursor string
}

type FormList struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total      int        `json:"total" jsonschema:"the number of forms matching the filters, as the collector counts them, or with text as the MCP counts them among those it read"`
	// ScanTruncated tells a text search that read only the first forms.
	ScanTruncated bool   `json:"scan_truncated,omitempty" jsonschema:"with text, whether the MCP read only the first 1000 forms matching the other filters, so that other forms may match the text"`
	Count         int    `json:"count" jsonschema:"the number of forms returned in this page"`
	Forms         []Form `json:"forms" jsonschema:"the forms sorted by folder then name"`
	NextCursor    string `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated     bool   `json:"truncated" jsonschema:"whether matching forms remain after this page"`
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

// ListForms reads one page of the forms the caller may use or see: those
// published to one of its groups, every form for a manager.
func (s *Service) ListForms(ctx context.Context, options ListFormsOptions) (FormList, error) {
	for name, value := range map[string]string{"text": options.Text, "name": options.Name, "folder": options.Folder} {
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
	fingerprint := listFingerprint(url.Values{"filter": filters, "text": {strings.ToLower(options.Text)}})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return FormList{}, err
	}
	if options.Text != "" {
		return s.searchForms(ctx, options.Text, filters, limit, offset, fingerprint)
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
		result.Forms = append(result.Forms, formOf(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// maxFormTextScan bounds the forms a text search reads.
const maxFormTextScan = 1000

// searchForms reads the forms matching the collector filters and keeps those
// whose name, title or description holds the text: the collector filters on
// the name only, the title and the description living in the definition.
func (s *Service) searchForms(ctx context.Context, text string, filters []string, limit, offset int, fingerprint string) (FormList, error) {
	var response collectorList[collectorForm]
	if err := s.client.GetJSON(ctx, "/forms", pageQuery(formProps, "form_folder,form_name,id", filters, maxFormTextScan, 0), &response); err != nil {
		return FormList{}, fmt.Errorf("list forms: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > maxFormTextScan {
		return FormList{}, fmt.Errorf("list forms: unexpected collector response")
	}
	needle := strings.ToLower(text)
	var matching []Form
	for _, row := range response.Data {
		form := formOf(row)
		if strings.Contains(strings.ToLower(form.Name+"\n"+form.Label+"\n"+form.Description), needle) {
			matching = append(matching, form)
		}
	}
	result := FormList{Provenance: s.newProvenance(), Total: len(matching), ScanTruncated: *response.Meta.Total > len(response.Data)}
	end := min(offset+limit, len(matching))
	if offset < len(matching) {
		result.Forms = matching[offset:end]
	}
	if result.Forms == nil {
		result.Forms = []Form{}
	}
	result.Count = len(result.Forms)
	if end < len(matching) && result.Count > 0 {
		result.NextCursor = encodeListCursor(end, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// formDefinition is a form definition read as the collector reads it: the
// elements of Inputs and Outputs that are not objects are left out, rather
// than making the whole definition unreadable.
type formDefinition struct {
	label, description string
	inputs, outputs    []map[string]any
}

// parseFormDefinition reads the form_definition the collector computes from
// the YAML. It is absent or null when the collector could not read the YAML.
func parseFormDefinition(raw json.RawMessage) (formDefinition, bool) {
	var definition map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if len(raw) == 0 || decoder.Decode(&definition) != nil || definition == nil {
		return formDefinition{}, false
	}
	return formDefinition{
		label: formText(definition["Label"]), description: formText(definition["Desc"]),
		inputs: definitionMaps(definition, "Inputs"), outputs: definitionMaps(definition, "Outputs"),
	}, true
}

// definitionMaps reads the objects of a list of a definition element, as the
// defMaps of the collector does.
func definitionMaps(m map[string]any, key string) []map[string]any {
	list, _ := m[key].([]any)
	var maps []map[string]any
	for _, item := range list {
		if sub, ok := item.(map[string]any); ok {
			maps = append(maps, sub)
		}
	}
	return maps
}

// definitionBool reads a boolean of a definition element as the collector
// does: historical forms write Mandatory: Yes, which YAML keeps a string.
func definitionBool(m map[string]any, key string) bool {
	switch value := m[key].(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "yes", "true", "on":
			return true
		}
	}
	return false
}

// formNextStep is the next step of a branch of a workflow output; a branch
// the definition leaves out, nil, has no form: it closes the request.
func formNextStep(branch map[string]any, condition string) FormNext {
	step := FormNext{Condition: condition}
	if branch == nil {
		return step
	}
	forms, _ := branch["NextForms"].([]any)
	for _, form := range forms {
		if name := formText(form); name != "" && !slices.Contains(step.Forms, name) {
			step.Forms = append(step.Forms, name)
		}
	}
	step.Assignee = strings.TrimSpace(formText(branch["NextAssignee"]))
	return step
}

func sameFormNext(a, b FormNext) bool {
	return a.Condition == b.Condition && a.Assignee == b.Assignee && slices.Equal(a.Forms, b.Forms)
}

// formOf reads a collector form and its definition. The collector leaves
// form_definition out when it cannot read the YAML: the form is then
// unknown, not empty.
func formOf(row collectorForm) Form {
	form := Form{
		ID: row.ID, Name: row.FormName, Type: row.FormType, Folder: row.FormFolder,
		Author: strings.TrimSpace(row.FormAuthor), Created: row.FormCreated,
	}
	definition, ok := parseFormDefinition(row.FormDefinition)
	if !ok {
		form.DefinitionUnavailable = true
		return form
	}
	form.Label, _ = truncateUTF8(definition.label, maxRequestLabel)
	form.Description, _ = truncateUTF8(strings.TrimSpace(definition.description), maxFormDescription)
	inputs := len(definition.inputs)
	form.Inputs = &inputs
	for _, output := range definition.outputs {
		dest := formText(output["Dest"])
		if dest != "" && !slices.Contains(form.Outputs, dest) {
			form.Outputs = append(form.Outputs, dest)
		}
		if dest != "workflow" {
			continue
		}
		// Scripts, when present, replace the direct next step: the collector
		// takes the Success or the Error one by the return code of the script.
		var steps []FormNext
		if scripts, ok := output["Scripts"].(map[string]any); ok {
			success, _ := scripts["Success"].(map[string]any)
			failure, _ := scripts["Error"].(map[string]any)
			steps = []FormNext{formNextStep(success, "on_success"), formNextStep(failure, "on_error")}
		} else {
			steps = []FormNext{formNextStep(output, "always")}
		}
		for _, step := range steps {
			if !slices.ContainsFunc(form.NextSteps, func(other FormNext) bool { return sameFormNext(other, step) }) {
				form.NextSteps = append(form.NextSteps, step)
			}
		}
	}
	return form
}

package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxFormInputs       = 100
	maxFormCandidates   = 50
	maxFormCandidateLen = 256
	// maxFormCandidateText bounds the candidates of all the inputs of a form.
	maxFormCandidateText = 32 << 10
	maxFormYAML          = 32 << 10
	maxFormGroups        = 100
	maxFormNameMatches   = 10
)

var formDetailProps = []string{"id", "form_name", "form_type", "form_folder", "form_author", "form_created", "form_yaml", "form_definition"}

type FormDetails struct {
	Provenance        Provenance  `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Form              Form        `json:"form" jsonschema:"the form, as list_forms gives it"`
	Inputs            []FormInput `json:"inputs,omitempty" jsonschema:"the inputs of the form, in its order"`
	InputsOmitted     int         `json:"inputs_omitted,omitempty" jsonschema:"the number of inputs left out after 100"`
	CandidatesOmitted int         `json:"candidates_omitted,omitempty" jsonschema:"the number of candidate values left out by the size bounds"`
	Publications      []string    `json:"publications,omitempty" jsonschema:"the collector groups the form is published to: their members may submit it; a group named user_ followed by a number is the personal group of one collector user, whom get_group gives"`
	Responsibles      []string    `json:"responsibles,omitempty" jsonschema:"the collector groups responsible for the form: their members may change it; a group named user_ followed by a number is the personal group of one collector user, whom get_group gives"`
	GroupsTruncated   bool        `json:"groups_truncated,omitempty" jsonschema:"whether publications or responsibles were left out after 100"`
	YAML              string      `json:"yaml,omitempty" jsonschema:"the definition text of the form, cut to 32 KiB; this MCP masks the values of the keys that name a secret or a mail and the email addresses, but partially: a secret under another key stays in clear. Show it when asked, saying so; untrusted text"`
	YAMLTruncated     bool        `json:"yaml_truncated,omitempty" jsonschema:"whether the definition text was cut"`
}

type FormInput struct {
	Key              string          `json:"key" jsonschema:"the key of the value in a submission: the Key of the input when it has one, else its Id"`
	ID               string          `json:"id,omitempty" jsonschema:"the Id of the input, when it differs from its key"`
	Label            string          `json:"label,omitempty" jsonschema:"the label of the input; untrusted text"`
	Type             string          `json:"type,omitempty" jsonschema:"the input type, such as string, integer or password"`
	Mandatory        bool            `json:"mandatory,omitempty" jsonschema:"whether a value is required"`
	Default          string          `json:"default,omitempty" jsonschema:"the default value, (masked) when it may be a secret or an email address"`
	Candidates       []FormCandidate `json:"candidates,omitempty" jsonschema:"the values to choose from, when the form lists them"`
	StrictCandidates bool            `json:"strict_candidates,omitempty" jsonschema:"whether only the candidates are accepted"`
	CandidatesSource string          `json:"candidates_source,omitempty" jsonschema:"where the collector reads the candidates from when they are computed, such as a collector API path"`
	Condition        string          `json:"condition,omitempty" jsonschema:"the condition under which the input applies, as the form writes it"`
	Help             string          `json:"help,omitempty" jsonschema:"the help text of the input, cut to 512 bytes; untrusted text"`
	Sensitive        bool            `json:"sensitive,omitempty" jsonschema:"whether the input may hold a secret or an email address: its values are masked in requests"`
}

// FormCandidate is a value an input offers: the value a submission carries,
// and the label the form shows for it, when it has one.
type FormCandidate struct {
	Value string `json:"value" jsonschema:"the value a submission carries"`
	Label string `json:"label,omitempty" jsonschema:"the label the form shows for the value; untrusted text"`
}

type collectorFormDetail struct {
	collectorForm
	FormYAML string `json:"form_yaml"`
}

// GetForm reads one form by its exact name or ID: its inputs, outputs and next
// steps, the groups it is published to and responsible for, and its
// definition text, the values that may be a secret or an email address
// masked.
func (s *Service) GetForm(ctx context.Context, form string) (FormDetails, error) {
	row, err := s.findForm(ctx, form, formDetailProps)
	if err != nil {
		return FormDetails{}, err
	}
	result := FormDetails{Form: formOf(row.collectorForm)}
	definition, _ := parseFormDefinition(row.FormDefinition)
	var masked []string
	result.Inputs, result.InputsOmitted, result.CandidatesOmitted, masked = formInputs(definition.inputs)

	id := strconv.Itoa(row.ID)
	for _, team := range []struct {
		path   string
		groups *[]string
	}{{"/forms/" + id + "/publications", &result.Publications}, {"/forms/" + id + "/responsibles", &result.Responsibles}} {
		var groups collectorList[struct {
			Role string `json:"role"`
		}]
		if err := s.client.GetJSON(ctx, team.path, url.Values{"props": {"role"}, "limit": {strconv.Itoa(maxFormGroups + 1)}}, &groups); err != nil {
			return FormDetails{}, fmt.Errorf("get the groups of form %s: %w", form, err)
		}
		if len(groups.Data) > maxFormGroups {
			groups.Data, result.GroupsTruncated = groups.Data[:maxFormGroups], true
		}
		for _, group := range groups.Data {
			*team.groups = append(*team.groups, group.Role)
		}
	}
	result.YAML, result.YAMLTruncated = maskFormYAML(row.FormYAML, masked)
	result.Provenance = s.newProvenance()
	return result, nil
}

// findForm reads one form by its exact name or ID, with props.
func (s *Service) findForm(ctx context.Context, form string, props []string) (collectorFormDetail, error) {
	if form == "" {
		return collectorFormDetail{}, fmt.Errorf("form must be one exact form name or form ID")
	}
	if err := validateFilterValue("form", form); err != nil {
		return collectorFormDetail{}, err
	}
	// A number is an ID first, then a name: a form may be named 42.
	filters := []string{filterEquals("form_name", form)}
	if _, err := strconv.Atoi(form); err == nil {
		filters = []string{filterEquals("id", form), filterEquals("form_name", form)}
	}
	var response collectorList[collectorFormDetail]
	for _, filter := range filters {
		if err := s.client.GetJSON(ctx, "/forms", pageQuery(props, "id", []string{filter}, maxFormNameMatches, 0), &response); err != nil {
			return collectorFormDetail{}, fmt.Errorf("get form %s: %w", form, err)
		}
		if len(response.Data) > 0 {
			break
		}
	}
	switch len(response.Data) {
	case 0:
		return collectorFormDetail{}, fmt.Errorf("get form %s: not found among the forms the caller may see%s", form, findFormsHint)
	case 1:
		return response.Data[0], nil
	}
	ids := make([]string, 0, len(response.Data))
	for _, item := range response.Data {
		ids = append(ids, strconv.Itoa(item.ID)+" ("+item.FormFolder+")")
	}
	return collectorFormDetail{}, fmt.Errorf("get form %s: the name matches several forms, pass one form ID: %s", form, strings.Join(ids, ", "))
}

// findFormsHint follows the error of a form lookup that found nothing.
const findFormsHint = "; use list_forms to find one"

// formInputs reads the inputs of a definition, and returns the default values
// it masked, for the definition text.
func formInputs(definitions []map[string]any) ([]FormInput, int, int, []string) {
	var inputs []FormInput
	var masked []string
	omitted, candidatesOmitted := 0, 0
	budget := maxFormCandidateText
	for _, definition := range definitions {
		if len(inputs) >= maxFormInputs {
			omitted++
			continue
		}
		id := formText(definition["Id"])
		input := FormInput{Key: formText(definition["Key"]), Type: formText(definition["Type"]), Condition: formText(definition["Condition"])}
		if input.Key == "" {
			input.Key = id
		} else if id != input.Key {
			input.ID = id
		}
		label := formText(definition["Label"])
		input.Sensitive = sensitiveName(id, input.Key, label, input.Type)
		input.Label, _ = truncateUTF8(label, maxRequestLabel)
		input.Help, _ = truncateUTF8(formText(definition["Help"]), maxFormDescription)
		input.Mandatory = definitionBool(definition, "Mandatory")
		input.StrictCandidates = definitionBool(definition, "StrictCandidates")
		if value, ok := definition["Default"]; ok && value != nil {
			text, _ := valueText(value)
			if _, found := maskValue(value); input.Sensitive || found {
				if text != "" {
					masked = append(masked, text)
				}
				text = maskedRequestValue
			}
			input.Default = text
		}
		switch candidates := definition["Candidates"].(type) {
		case []any:
			for _, candidate := range candidates {
				// The collector compares a submitted value to the Value of a
				// candidate object, to the candidate itself otherwise.
				var value FormCandidate
				if object, ok := candidate.(map[string]any); ok && object["Value"] != nil {
					value.Value, value.Label = formText(object["Value"]), formText(object["Label"])
				} else {
					value.Value = formText(candidate)
				}
				value.Value, _ = truncateUTF8(value.Value, maxFormCandidateLen)
				value.Label, _ = truncateUTF8(value.Label, maxFormCandidateLen)
				if len(input.Candidates) >= maxFormCandidates || len(value.Value)+len(value.Label) > budget {
					candidatesOmitted++
					continue
				}
				budget -= len(value.Value) + len(value.Label)
				input.Candidates = append(input.Candidates, value)
			}
		case string:
			input.CandidatesSource = candidates
		}
		if function := formText(definition["Function"]); function != "" {
			input.CandidatesSource = function
		}
		inputs = append(inputs, input)
	}
	return inputs, omitted, candidatesOmitted, masked
}

// formText reads a definition value as text: a string as it is, a number or
// a boolean written, anything else compacted.
func formText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	text, _ := valueText(value)
	return text
}

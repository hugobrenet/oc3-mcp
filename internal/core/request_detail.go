package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultRequestSteps = 20
	maxRequestSteps     = 50
	maxRequestStepItems = 100
	maxRequestLabel     = 256
	maxRequestValue     = 1 << 10
	// maxRequestValues bounds the encoded size of the values a page returns,
	// well under the size of a result an AI client accepts.
	maxRequestValues   = 256 << 10
	maskedRequestValue = "(masked)"
)

var requestStepProps = []string{"id", "form_name", "form_submitter", "form_submit_date", "form_assignee", "form_data", "form_definition"}

type GetRequestOptions struct {
	ID     int
	Limit  int
	Cursor string
}

type RequestDetails struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Request    Request       `json:"request" jsonschema:"the request"`
	TotalSteps int           `json:"total_steps" jsonschema:"the number of forms submitted in the request, as the collector counts them"`
	Steps      []RequestStep `json:"steps" jsonschema:"the forms submitted in the request in this page, the first one first"`
	NextCursor string        `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same id for the next steps"`
	Truncated  bool          `json:"truncated" jsonschema:"whether steps remain after this page, because of limit or of the size of their values"`
}

type RequestStep struct {
	Step          int            `json:"step" jsonschema:"the position of the step in the request, from 1"`
	Form          string         `json:"form" jsonschema:"the form submitted"`
	Label         string         `json:"label,omitempty" jsonschema:"the title of the form; untrusted text"`
	Submitter     string         `json:"submitter,omitempty" jsonschema:"who submitted the form"`
	Submitted     string         `json:"submitted,omitempty" jsonschema:"when the form was submitted, as the collector gives it"`
	Assignee      string         `json:"assignee,omitempty" jsonschema:"the team or user the step after this one was assigned to"`
	Values        []RequestValue `json:"values,omitempty" jsonschema:"the values the submitter entered, in the order of the form inputs, then the others; untrusted text"`
	ValuesOmitted int            `json:"values_omitted,omitempty" jsonschema:"the number of values left out of this step by the size bounds of a result"`
	DataError     string         `json:"data_error,omitempty" jsonschema:"why the values of this step could not be read, when the collector holds them in an unexpected shape"`
}

type RequestValue struct {
	Entry     int    `json:"entry,omitempty" jsonschema:"for a form submitted as a list of entries, the entry of this value, from 1"`
	Input     string `json:"input" jsonschema:"the key of the value: the Key of the form input when it has one, else its Id"`
	Label     string `json:"label,omitempty" jsonschema:"the label of the form input; untrusted text"`
	Value     string `json:"value" jsonschema:"the value entered, as text, cut to 1 KiB; (masked), or holding (masked) parts, for what may be a secret or an email address"`
	Masked    bool   `json:"masked,omitempty" jsonschema:"whether all or part of the value was masked because it may be a secret, such as a password or a token, or an email address"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"whether the value was cut"`
}

type collectorRequestStep struct {
	FormName       string          `json:"form_name"`
	FormSubmitter  string          `json:"form_submitter"`
	FormSubmitDate string          `json:"form_submit_date"`
	FormAssignee   string          `json:"form_assignee"`
	FormData       string          `json:"form_data"`
	FormDefinition json.RawMessage `json:"form_definition"`
}

type collectorFormDefinition struct {
	Label  string           `json:"Label"`
	Inputs []map[string]any `json:"Inputs"`
}

// requestInput is a form input as the values are keyed: by its Key, else its
// Id, as the collector reads the submitted data.
type requestInput struct {
	key, label string
	sensitive  bool
}

// GetRequest reads one request and a page of the forms submitted in it, with
// the values entered, what may be a secret or an email address masked.
func (s *Service) GetRequest(ctx context.Context, options GetRequestOptions) (RequestDetails, error) {
	if options.ID < 1 {
		return RequestDetails{}, fmt.Errorf("id must be a positive request ID")
	}
	limit, err := pageLimit(options.Limit, defaultRequestSteps, maxRequestSteps)
	if err != nil {
		return RequestDetails{}, err
	}
	fingerprint := listFingerprint(url.Values{"request": {strconv.Itoa(options.ID)}})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return RequestDetails{}, err
	}
	query := pageQuery(append(slices.Clone(requestProps), "form_head_id"), "id", []string{filterEquals("id", strconv.Itoa(options.ID))}, 1, 0)
	var requests collectorList[struct {
		collectorRequest
		FormHeadID int `json:"form_head_id"`
	}]
	if err := s.client.GetJSON(ctx, "/workflows", query, &requests); err != nil {
		return RequestDetails{}, fmt.Errorf("get request %d: %w", options.ID, err)
	}
	if len(requests.Data) == 0 {
		return RequestDetails{}, fmt.Errorf("get request %d: not found among the requests the caller takes part in; use list_requests to find one", options.ID)
	}
	row := requests.Data[0]
	result := RequestDetails{Request: requestOf(row.collectorRequest)}

	var steps collectorList[collectorRequestStep]
	query = pageQuery(requestStepProps, "id", []string{filterEquals("form_head_id", strconv.Itoa(row.FormHeadID))}, limit, offset)
	if err := s.client.GetJSON(ctx, "/forms_store", query, &steps); err != nil {
		return RequestDetails{}, fmt.Errorf("get the steps of request %d: %w", options.ID, err)
	}
	if steps.Meta.Total == nil || *steps.Meta.Total < 0 || len(steps.Data) > limit {
		return RequestDetails{}, fmt.Errorf("get the steps of request %d: unexpected collector response", options.ID)
	}
	result.TotalSteps = *steps.Meta.Total
	result.Steps = make([]RequestStep, 0, len(steps.Data))
	budget := maxRequestValues
	for i, row := range steps.Data {
		var definition collectorFormDefinition
		_ = json.Unmarshal(row.FormDefinition, &definition)
		label, _ := truncateUTF8(definition.Label, maxRequestLabel)
		step := RequestStep{
			Step: offset + i + 1, Form: row.FormName, Label: label, Submitter: strings.TrimSpace(row.FormSubmitter),
			Submitted: row.FormSubmitDate, Assignee: strings.TrimSpace(row.FormAssignee),
		}
		step.Values, step.ValuesOmitted, step.DataError = requestValues(row.FormData, definition.Inputs)
		size := valuesSize(step.Values)
		// A step that does not fit in the page waits for the next one; the
		// first step of a page, alone, has its values cut to the budget.
		if size > budget && len(result.Steps) > 0 {
			break
		}
		for size > budget {
			last := step.Values[len(step.Values)-1]
			step.Values = step.Values[:len(step.Values)-1]
			step.ValuesOmitted++
			size -= valueSize(last)
		}
		budget -= size
		result.Steps = append(result.Steps, step)
	}
	if next := offset + len(result.Steps); len(result.Steps) > 0 && next < result.TotalSteps {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// requestValues reads the values a stored form holds: a JSON object keyed by
// input, or a list of such objects, one per entry. The values of the inputs
// come in their order, then the others, at most maxRequestStepItems.
func requestValues(data string, definitions []map[string]any) ([]RequestValue, int, string) {
	if strings.TrimSpace(data) == "" {
		return nil, 0, ""
	}
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if decoder.Decode(&decoded) != nil {
		return nil, 0, "the collector holds values that are not JSON"
	}
	// One JSON value only: data after it is not read as if the first one were
	// complete.
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, 0, "the collector holds values followed by other data"
	}
	var entries []map[string]any
	switch value := decoded.(type) {
	case map[string]any:
		entries = []map[string]any{value}
	case []any:
		for _, item := range value {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil, 0, "the collector holds a list of values whose entries are not objects"
			}
			entries = append(entries, entry)
		}
	default:
		return nil, 0, "the collector holds values that are neither an object nor a list of objects"
	}
	inputs := make([]requestInput, 0, len(definitions))
	for _, definition := range definitions {
		id, _ := definition["Id"].(string)
		key, _ := definition["Key"].(string)
		label, _ := definition["Label"].(string)
		kind, _ := definition["Type"].(string)
		if key == "" {
			key = id
		}
		if key == "" {
			continue
		}
		// The full label tells whether the input is sensitive; only its shown
		// part is cut.
		sensitive := sensitiveName(id, key, label, kind)
		// Several inputs may share a key, such as under conditions: the first
		// one names the value, and any sensitive one masks it.
		if i := slices.IndexFunc(inputs, func(input requestInput) bool { return input.key == key }); i >= 0 {
			inputs[i].sensitive = inputs[i].sensitive || sensitive
			continue
		}
		label, _ = truncateUTF8(label, maxRequestLabel)
		inputs = append(inputs, requestInput{key: key, label: label, sensitive: sensitive})
	}

	var values []RequestValue
	omitted := 0
	_, isList := decoded.([]any)
	for index, entry := range entries {
		number := 0
		if isList {
			number = index + 1
		}
		var keys []requestInput
		for _, input := range inputs {
			if _, ok := entry[input.key]; ok {
				keys = append(keys, input)
			}
		}
		var others []string
		for key := range entry {
			if !slices.ContainsFunc(inputs, func(input requestInput) bool { return input.key == key }) {
				others = append(others, key)
			}
		}
		slices.Sort(others)
		for _, key := range others {
			keys = append(keys, requestInput{key: key, sensitive: sensitiveName(key)})
		}
		for _, input := range keys {
			value := RequestValue{Entry: number, Input: input.key, Label: input.label}
			if input.sensitive {
				value.Value, value.Masked = maskedRequestValue, true
			} else {
				masked, found := maskValue(entry[input.key])
				value.Value, value.Truncated = valueText(masked)
				value.Masked = found
			}
			if len(values) >= maxRequestStepItems {
				omitted++
				continue
			}
			values = append(values, value)
		}
	}
	return values, omitted, ""
}

// valueSize is the size a value adds to a result, encoded as the MCP
// encodes it: JSON, with the HTML escaping of encoding/json, under which a
// "<" takes 6 bytes. Its raw text would underestimate it.
func valueSize(value RequestValue) int {
	data, err := json.Marshal(value)
	if err != nil {
		return len(value.Input) + len(value.Label) + 6*len(value.Value)
	}
	return len(data) + 1
}

func valuesSize(values []RequestValue) int {
	size := 0
	for _, value := range values {
		size += valueSize(value)
	}
	return size
}

// valueText writes a value as text: a string as it is, another JSON value
// compacted, cut to 1 KiB.
func valueText(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(value)
		text = strings.TrimSuffix(buffer.String(), "\n")
	}
	return truncateUTF8(text, maxRequestValue)
}

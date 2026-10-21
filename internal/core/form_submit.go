package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"
)

const (
	// maxFormWait bounds how long submit_form and get_form_result wait for
	// the outputs of an Async form to end, in seconds, within the write
	// timeout of the MCP.
	maxFormWait = 120
	// formSubmitTimeout bounds a submission: the collector answers a form
	// that is not Async once its outputs ran, the calls of its rest outputs
	// and their WaitResult included.
	formSubmitTimeout = 2 * time.Minute
	// maxFormOutputLines bounds the log lines given for one output.
	maxFormOutputLines = 50
	// maxFormLogLine bounds one log line.
	maxFormLogLine = 1 << 10
)

// formRunStatuses are the statuses of a submission, as the MCP names them,
// and as the collector stores them.
var formRunStatuses = map[string]string{"QUEUED": "queued", "RUNNING": "running", "COMPLETED": "completed"}

var formRunProps = []string{"id", "form_name", "form_definition"}

// formSubmissionOutput names the lines the collector logs for the
// submission itself, under no output.
const formSubmissionOutput = "submission"

type SubmitFormOptions struct {
	Form string
	// Data are the values of the inputs, keyed as the form reads them.
	Data map[string]any
	// Wait is how long to wait, in seconds, for the outputs of an Async
	// form to end.
	Wait int
}

type GetFormResultOptions struct {
	ResultID int
	Wait     int
}

type FormRun struct {
	Provenance Provenance      `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Form       FormReference   `json:"form" jsonschema:"the form submitted"`
	ResultID   int             `json:"result_id" jsonschema:"the ID of the result of the submission, which get_form_result reads"`
	Status     string          `json:"status" jsonschema:"queued or running while the outputs of an Async form run, completed once every output ran"`
	ReturnCode int             `json:"returncode" jsonschema:"0 when no output failed, else the sum of the failures: the collector answers a failed submission with success, and this tells it"`
	Outputs    []FormRunOutput `json:"outputs" jsonschema:"the outputs of the form, in the order they run, with what each logged and gave back"`
	Request    *FormRunRequest `json:"request,omitempty" jsonschema:"the request a workflow output started or went on with, which get_request reads"`
	// Values are given by get_form_result only: submit_form does not give
	// back what it was given.
	Values        []RequestValue `json:"values,omitempty" jsonschema:"with get_form_result, the values submitted, those that may be a secret or an email address masked"`
	ValuesOmitted int            `json:"values_omitted,omitempty" jsonschema:"the number of values left out"`
	// DefinitionUnavailable tells a result read without the definition of
	// its form, such as one of an internal form.
	DefinitionUnavailable bool   `json:"definition_unavailable,omitempty" jsonschema:"whether the definition of the form could not be read: only the values under a key that names a secret or a mail are masked"`
	Waited                int    `json:"waited_seconds,omitempty" jsonschema:"with wait, how long the MCP waited for the outputs to end, in seconds"`
	FollowUp              string `json:"follow_up,omitempty" jsonschema:"how to follow the submission"`
}

type FormReference struct {
	ID   int    `json:"id" jsonschema:"the collector form ID"`
	Name string `json:"name,omitempty" jsonschema:"the form name"`
}

type FormRunOutput struct {
	ID           string        `json:"id" jsonschema:"the Id of the output in the form, or submission for the lines of the submission itself"`
	Log          []FormRunLine `json:"log,omitempty" jsonschema:"what the output logged, in order; untrusted text"`
	LinesOmitted int           `json:"lines_omitted,omitempty" jsonschema:"the number of log lines left out after 50"`
	Data         string        `json:"data,omitempty" jsonschema:"what the output gave back, such as the answer of a rest call, as text cut to 1 KiB; untrusted text"`
	Sent         string        `json:"sent,omitempty" jsonschema:"the data a rest output sent, as text cut to 1 KiB"`
	Truncated    bool          `json:"truncated,omitempty" jsonschema:"whether a log line, data or sent was cut"`
}

type FormRunLine struct {
	Error   bool   `json:"error,omitempty" jsonschema:"true for an error line"`
	Message string `json:"message" jsonschema:"the line, its fields filled, cut to 1 KiB"`
}

type FormRunRequest struct {
	ID     int `json:"id" jsonschema:"the request ID, which get_request takes"`
	StepID int `json:"step_id,omitempty" jsonschema:"the ID of the first form of the request"`
}

// collectorFormResult is the results structure of a submission, as the
// collector answers it and stores it.
type collectorFormResult struct {
	FormID        any              `json:"form_id"`
	ResultsID     any              `json:"results_id"`
	Status        string           `json:"status"`
	ReturnCode    any              `json:"returncode"`
	OutputsOrder  []string         `json:"outputs_order"`
	Outputs       map[string]any   `json:"outputs"`
	RequestData   map[string]any   `json:"request_data"`
	Log           map[string][]any `json:"log"`
	SubmittedData any              `json:"submitted_data"`
}

// SubmitForm submits a form with the values given, as the collector takes
// them: it validates them and runs the outputs of the form, its rest calls
// with the credentials of the caller. A form that is not Async is answered
// once its outputs ran; with Wait, the MCP reads the result of an Async
// one until its outputs ran.
func (s *Service) SubmitForm(ctx context.Context, options SubmitFormOptions) (FormRun, error) {
	if options.Data == nil {
		return FormRun{}, fmt.Errorf("data must be an object of the values of the inputs, keyed as get_form gives them")
	}
	if options.Wait < 0 || options.Wait > maxFormWait {
		return FormRun{}, fmt.Errorf("wait must be between 0 and %d seconds", maxFormWait)
	}
	form, err := s.findForm(ctx, options.Form, formRunProps)
	if err != nil {
		return FormRun{}, err
	}
	definition, _ := parseFormDefinition(form.FormDefinition)
	started := time.Now()
	var result collectorFormResult
	path := "/forms/" + strconv.Itoa(form.ID)
	if err := s.putLong(ctx, path, map[string]any{"data": options.Data}, &result, formSubmitTimeout); err != nil {
		return FormRun{}, fmt.Errorf("submit form %s: %w", form.FormName, err)
	}
	result, err = s.waitFormResult(ctx, result, started, options.Wait)
	if err != nil {
		return FormRun{}, err
	}
	run := formRunOf(result, definition.inputs, false)
	run.Form, run.Provenance = FormReference{ID: form.ID, Name: form.FormName}, s.newProvenance()
	if options.Wait > 0 {
		run.Waited = int(time.Since(started).Round(time.Second) / time.Second)
	}
	return run, nil
}

// GetFormResult reads the result of a submission, its values masked by the
// definition of its form; with Wait, until its outputs ran.
func (s *Service) GetFormResult(ctx context.Context, options GetFormResultOptions) (FormRun, error) {
	if options.ResultID <= 0 {
		return FormRun{}, fmt.Errorf("result_id must be the result ID submit_form gives")
	}
	if options.Wait < 0 || options.Wait > maxFormWait {
		return FormRun{}, fmt.Errorf("wait must be between 0 and %d seconds", maxFormWait)
	}
	started := time.Now()
	result, err := s.readFormResult(ctx, options.ResultID)
	if err != nil {
		return FormRun{}, err
	}
	if result, err = s.waitFormResult(ctx, result, started, options.Wait); err != nil {
		return FormRun{}, err
	}
	formID, _ := strconv.Atoi(anyText(result.FormID))
	// An internal form, of a negative ID, has no definition to read.
	var inputs []map[string]any
	unavailable := true
	reference := FormReference{ID: formID}
	if formID > 0 {
		if form, err := s.findForm(ctx, strconv.Itoa(formID), formRunProps); err == nil {
			definition, ok := parseFormDefinition(form.FormDefinition)
			inputs, unavailable = definition.inputs, !ok
			reference.Name = form.FormName
		}
	}
	run := formRunOf(result, inputs, true)
	run.Form, run.DefinitionUnavailable, run.Provenance = reference, unavailable, s.newProvenance()
	if options.Wait > 0 {
		run.Waited = int(time.Since(started).Round(time.Second) / time.Second)
	}
	return run, nil
}

// longPutClient is a collector client that bounds a request otherwise than
// by its default.
type longPutClient interface {
	PutJSONWithin(ctx context.Context, path string, input any, output any, timeout time.Duration) error
}

// putLong sends a PUT bounded by timeout, when the client takes a bound.
func (s *Service) putLong(ctx context.Context, path string, input any, output any, timeout time.Duration) error {
	if client, ok := s.client.(longPutClient); ok {
		return client.PutJSONWithin(ctx, path, input, output, timeout)
	}
	return s.client.PutJSON(ctx, path, input, output)
}

func (s *Service) readFormResult(ctx context.Context, id int) (collectorFormResult, error) {
	var result collectorFormResult
	if err := s.client.GetJSON(ctx, "/form_output_results/"+url.PathEscape(strconv.Itoa(id)), nil, &result); err != nil {
		return collectorFormResult{}, fmt.Errorf("get form result %d: %w", id, err)
	}
	return result, nil
}

// waitFormResult reads the result again every actionWaitInterval until its
// outputs ran, or wait seconds after started.
func (s *Service) waitFormResult(ctx context.Context, result collectorFormResult, started time.Time, wait int) (collectorFormResult, error) {
	deadline := started.Add(time.Duration(wait) * time.Second)
	for result.Status != "COMPLETED" {
		remaining := time.Until(deadline)
		if wait == 0 || remaining <= 0 {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return collectorFormResult{}, ctx.Err()
		case <-time.After(min(actionWaitInterval, remaining)):
		}
		id, err := strconv.Atoi(anyText(result.ResultsID))
		if err != nil {
			return result, nil
		}
		if result, err = s.readFormResult(ctx, id); err != nil {
			return collectorFormResult{}, err
		}
	}
	return result, nil
}

// formRunOf gives a result its outputs in order, their logs filled, and
// masks what may be a secret: the values of the inputs the definition or
// their key tells sensitive, wherever they show, and the email addresses.
func formRunOf(result collectorFormResult, inputs []map[string]any, withValues bool) FormRun {
	run := FormRun{Status: formRunStatuses[result.Status], Outputs: []FormRunOutput{}}
	if run.Status == "" {
		run.Status = result.Status
	}
	run.ResultID, _ = strconv.Atoi(anyText(result.ResultsID))
	run.ReturnCode, _ = strconv.Atoi(anyText(result.ReturnCode))
	secrets := formSecrets(result.SubmittedData, inputs)
	if withValues && result.SubmittedData != nil {
		if data, err := json.Marshal(result.SubmittedData); err == nil {
			run.Values, run.ValuesOmitted, _ = requestValues(string(data), inputs)
		}
	}
	order := slices.Clone(result.OutputsOrder)
	var others []string
	for id := range result.Log {
		if !slices.Contains(order, id) {
			others = append(others, id)
		}
	}
	slices.Sort(others)
	for _, id := range append(order, others...) {
		output := FormRunOutput{ID: id}
		// The collector logs the lines of the submission itself, such as
		// a step already completed, under no output.
		if id == "" {
			output.ID = formSubmissionOutput
		}
		for index, line := range result.Log[id] {
			if index >= maxFormOutputLines {
				output.LinesOmitted++
				continue
			}
			entry, cut := formRunLine(line, secrets)
			output.Log = append(output.Log, entry)
			output.Truncated = output.Truncated || cut
		}
		var cut bool
		if value, ok := result.Outputs[id]; ok {
			if request := formRunRequestOf(value); request != nil {
				run.Request = request
			}
			output.Data, cut = formRunText(value, secrets)
			output.Truncated = output.Truncated || cut
		}
		if value, ok := result.RequestData[id]; ok {
			output.Sent, cut = formRunText(value, secrets)
			output.Truncated = output.Truncated || cut
		}
		run.Outputs = append(run.Outputs, output)
	}
	switch {
	case run.Status != "completed" && run.ResultID > 0:
		run.FollowUp = fmt.Sprintf("get_form_result with result_id %d and wait, such as 60 seconds, at most %d, reads the result once the outputs ran, in one call", run.ResultID, maxFormWait)
	case run.Request != nil:
		run.FollowUp = fmt.Sprintf("get_request with id %d gives the request, its steps and who the next step is assigned to", run.Request.ID)
	}
	return run
}

// formRunLine fills the fields of a log line, [return code, template,
// fields], as the collector renders it, its sensitive fields masked.
func formRunLine(line any, secrets []string) (FormRunLine, bool) {
	parts, _ := line.([]any)
	var entry FormRunLine
	if len(parts) > 0 {
		code, err := strconv.Atoi(anyText(parts[0]))
		entry.Error = err == nil && code != 0
	}
	template := ""
	if len(parts) > 1 {
		template, _ = parts[1].(string)
	}
	fields := ""
	if len(parts) > 2 {
		masked, _ := maskValue(parts[2])
		if data, err := json.Marshal(masked); err == nil {
			fields = string(data)
		}
	}
	message, _ := renderCollectorEvent(template, fields)
	var cut bool
	entry.Message, cut = truncateUTF8(maskFormSecrets(message, secrets), maxFormLogLine)
	return entry, cut
}

// formRunText writes what an output gave back or sent as text, masked and
// cut to 1 KiB.
func formRunText(value any, secrets []string) (string, bool) {
	masked, _ := maskValue(value)
	text, cut := valueText(masked)
	return maskFormSecrets(text, secrets), cut
}

// formRunRequestOf reads the request a workflow output gives back.
func formRunRequestOf(value any) *FormRunRequest {
	output, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	id, err := strconv.Atoi(anyText(output["workflow_id"]))
	if err != nil || id <= 0 {
		return nil
	}
	step, _ := strconv.Atoi(anyText(output["head_form_id"]))
	return &FormRunRequest{ID: id, StepID: step}
}

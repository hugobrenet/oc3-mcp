package core

import (
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newFormRunCollector answers form 7, whose pwd input is a password, and its
// submission: a workflow output, and a rest output whose log, data and sent
// data hold the password and an email address.
func newFormRunCollector(status string, reads *atomic.Int32) *fakeCollector {
	definition := `{"Inputs":[{"Id":"hostname"},{"Id":"pwd","Type":"password"}],"Outputs":[{"Dest":"workflow","NextAssignee":"Infra Unix","NextForms":["vm-approval"]}]}`
	result := func(status string) string {
		return `{"form_id":7,"results_id":9,"status":"` + status + `","returncode":1,"outputs_order":["workflow","rest"],
			"submitted_data":{"hostname":"web1","pwd":"s3cretpw"},
			"outputs":{"workflow":{"workflow_id":1376,"head_form_id":1447},"rest":{"answer":"user created with s3cretpw"}},
			"request_data":{"rest":{"login":"web1","pwd":"s3cretpw"}},
			"log":{"workflow":[[0,"New workflow %(form_name)s created with id %(id)d",{"form_name":"vm","id":1447}]],
				"rest":[[1,"failed for %(who)s: %(why)s",{"who":"jane@example.com","why":"bad password s3cretpw"}]],
				"":[[1,"This step is already completed (id=%(id)d)",{"id":5}]]}}`
	}
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch {
		case path == "/forms":
			return `{"data":[{"id":7,"form_name":"vm","form_definition":` + definition + `}]}`
		case path == "/forms/7":
			return result(status)
		case path == "/workflows" && query.Get("filter") == "id:eq:1376":
			return `{"data":[{"id":1376,"last_form_id":1447}]}`
		case path == "/form_output_results/9":
			if reads != nil && reads.Add(1) < 2 {
				return result("RUNNING")
			}
			return result("COMPLETED")
		}
		return `{"data":[]}`
	}}
}

func TestSubmitFormGivesTheOutputsMasked(t *testing.T) {
	collector := newFormRunCollector("COMPLETED", nil)
	run, err := New(collector).SubmitForm(t.Context(), SubmitFormOptions{Form: "vm", Data: map[string]any{"hostname": "web1", "pwd": "s3cretpw"}})
	if err != nil {
		t.Fatal(err)
	}
	put := collector.requests[1]
	data, _ := put.body["data"].(map[string]any)
	if put.method != "PUT" || put.path != "/forms/7" || data["pwd"] != "s3cretpw" || len(put.body) != 1 {
		t.Fatalf("submission = %+v", put)
	}
	workflow, rest := run.Outputs[0], run.Outputs[1]
	if run.Form.Name != "vm" || run.ResultID != 9 || run.Status != "completed" || run.ReturnCode != 1 || run.Request == nil || run.Request.ID != 1376 || run.Request.StepID != 1447 ||
		run.NextStep == nil || strings.Join(run.NextStep.Forms, ",") != "vm-approval" || run.NextStep.Assignee != "Infra Unix" ||
		!strings.Contains(run.FollowUp, "request 1376 now waits for vm-approval, assigned to Infra Unix: submit_form with one of these forms and request 1376") || run.Values != nil || len(run.Outputs) != 3 {
		t.Fatalf("run = %+v", run)
	}
	if workflow.ID != "workflow" || workflow.Log[0].Message != "New workflow vm created with id 1447" || workflow.Log[0].Error {
		t.Fatalf("workflow = %+v", workflow)
	}
	// The lines of the submission itself come last, under submission.
	if submission := run.Outputs[2]; submission.ID != "submission" || submission.Log[0].Message != "This step is already completed (id=5)" {
		t.Fatalf("submission = %+v", submission)
	}
	// The password submitted and the email address are masked wherever they
	// show: in the log, the data given back and the data sent, which the
	// masks shorten without cutting.
	if !rest.Log[0].Error || rest.Log[0].Message != "failed for (masked): bad password (masked)" || rest.Truncated ||
		strings.Contains(rest.Data+rest.Sent, "s3cretpw") || !strings.Contains(rest.Data, "user created with (masked)") || !strings.Contains(rest.Sent, `"login":"web1"`) {
		t.Fatalf("rest = %+v", rest)
	}
	for _, options := range []SubmitFormOptions{
		{Form: "vm"},
		{Form: "vm", Data: map[string]any{}, Wait: maxFormWait + 1},
		{Data: map[string]any{}},
	} {
		collector := newFormRunCollector("COMPLETED", nil)
		if _, err := New(collector).SubmitForm(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestSubmitFormWaitsForAnAsyncForm(t *testing.T) {
	interval := actionWaitInterval
	actionWaitInterval = 10 * time.Millisecond
	t.Cleanup(func() { actionWaitInterval = interval })
	var reads atomic.Int32
	run, err := New(newFormRunCollector("QUEUED", &reads)).SubmitForm(t.Context(), SubmitFormOptions{Form: "vm", Data: map[string]any{"hostname": "web1"}, Wait: 30})
	if err != nil || run.Status != "completed" || reads.Load() != 2 || run.FollowUp == "" || strings.Contains(run.FollowUp, "get_form_result") {
		t.Fatalf("run = %+v, %v, reads %d", run, err, reads.Load())
	}
	// Without wait, the queued submission tells how to follow it.
	run, err = New(newFormRunCollector("QUEUED", nil)).SubmitForm(t.Context(), SubmitFormOptions{Form: "vm", Data: map[string]any{"hostname": "web1"}})
	if err != nil || run.Status != "queued" || !strings.Contains(run.FollowUp, "get_form_result with result_id 9 and wait") {
		t.Fatalf("run = %+v, %v", run, err)
	}
}

func TestGetFormResultMasksTheValuesSubmitted(t *testing.T) {
	run, err := New(newFormRunCollector("COMPLETED", nil)).GetFormResult(t.Context(), GetFormResultOptions{ResultID: 9})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, value := range run.Values {
		values[value.Input] = value.Value
	}
	if run.Form.Name != "vm" || run.DefinitionUnavailable || values["hostname"] != "web1" || values["pwd"] != maskedRequestValue {
		t.Fatalf("run = %+v", run)
	}
	// The result of an internal form has no definition to read: only the
	// keys naming a secret are masked.
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		return `{"form_id":-6,"results_id":10,"status":"COMPLETED","returncode":0,"submitted_data":{"username":"bob","password":"hunter22"}}`
	}}
	run, err = New(collector).GetFormResult(t.Context(), GetFormResultOptions{ResultID: 10})
	if err != nil || !run.DefinitionUnavailable || len(collector.requests) != 1 {
		t.Fatalf("run = %+v, %v, requests %d", run, err, len(collector.requests))
	}
	for _, value := range run.Values {
		if value.Input == "password" && value.Value != maskedRequestValue {
			t.Fatalf("values = %+v", run.Values)
		}
	}
	if _, err := New(collector).GetFormResult(t.Context(), GetFormResultOptions{}); err == nil {
		t.Fatal("a result without ID was read")
	}
}

// With request, the submission decides the pending step of the request:
// its last one, which the collector checks the form and the caller against.
func TestSubmitFormDecidesTheStepOfARequest(t *testing.T) {
	collector := newFormRunCollector("COMPLETED", nil)
	if _, err := New(collector).SubmitForm(t.Context(), SubmitFormOptions{Form: "vm", Data: map[string]any{"decision": "approved"}, Request: 1376}); err != nil {
		t.Fatal(err)
	}
	put := collector.requests[len(collector.requests)-1]
	if put.method != "PUT" || put.path != "/forms/7" || put.body["prev_wfid"] != float64(1447) {
		t.Fatalf("submission = %+v", put)
	}
	// A request the caller does not see is refused before any submission.
	collector = newFormRunCollector("COMPLETED", nil)
	if _, err := New(collector).SubmitForm(t.Context(), SubmitFormOptions{Form: "vm", Data: map[string]any{}, Request: 9}); err == nil || !strings.Contains(err.Error(), "list_requests") {
		t.Fatalf("an unknown request = %v", err)
	}
	for _, request := range collector.requests {
		if request.method == "PUT" {
			t.Fatalf("submission = %+v", request)
		}
	}
}

// The next step follows the Success or the Error branch of the scripts of the
// workflow output, by the return code; a branch without forms closes the
// request.
func TestRequestNextStep(t *testing.T) {
	outputs := []map[string]any{{"Dest": "rest"}, {"Dest": "workflow", "Scripts": map[string]any{
		"Success": map[string]any{"NextForms": []any{"deliver"}, "NextAssignee": "Storage"},
		"Error":   map[string]any{},
	}}}
	if next := requestNextStep(outputs, 0); next == nil || strings.Join(next.Forms, ",") != "deliver" || next.Assignee != "Storage" {
		t.Fatalf("on success = %+v", next)
	}
	next := requestNextStep(outputs, 1)
	if next == nil || len(next.Forms) != 0 || !strings.HasPrefix(requestFollowUp(9, *next), "this step closed request 9") {
		t.Fatalf("on error = %+v", next)
	}
	if requestNextStep([]map[string]any{{"Dest": "mail"}}, 0) != nil {
		t.Fatal("a form without workflow output gave a next step")
	}
}

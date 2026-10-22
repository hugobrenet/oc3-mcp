package core

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func runWriteCollector() *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/orchestrations" && query != nil {
			return `{"data":[` + orchestrationRow + `],"meta":{"total":1}}`
		}
		return strings.Replace(orchestrationRunDetail, "STDERR_PLACEHOLDER", "", 1)
	}}
}

func TestStartOrchestrationRunByName(t *testing.T) {
	collector := runWriteCollector()
	result, err := newTestService(collector).StartOrchestrationRun(t.Context(), "Test failure", 0)
	if err != nil {
		t.Fatal(err)
	}
	started := collector.requests[1]
	if started.method != "POST" || started.path != "/orchestrations/2/runs" || started.body != nil {
		t.Fatalf("started = %+v", started)
	}
	if result.Action != "start" || result.ID != 12 || result.StepCount != 2 || result.TargetTotal != 3 || len(result.Targets) != 3 || result.Targets[2].Stdout != "red" {
		t.Fatalf("result = %+v", result)
	}
	collector = &fakeCollector{answer: func(path string, query url.Values) string { return `{"data":[],"meta":{"total":0}}` }}
	if _, err := newTestService(collector).StartOrchestrationRun(t.Context(), "nope", 0); err == nil || len(collector.requests) != 1 {
		t.Fatalf("unknown orchestration: %v", err)
	}
	collector = &fakeCollector{err: statusError{http.StatusBadRequest, "step 1 matches no target"}}
	if _, err := newTestService(collector).StartOrchestrationRun(t.Context(), "2", 0); err == nil || !strings.Contains(err.Error(), "matches no target") {
		t.Fatalf("refused: %v", err)
	}
}

func TestChangeOrchestrationRun(t *testing.T) {
	for _, tc := range []struct {
		action  OrchestrationRunAction
		comment string
		path    string
	}{
		{OrchestrationRunStop, "", "/orchestration_runs/12/stop"},
		{OrchestrationRunResume, "", "/orchestration_runs/12/resume"},
		{OrchestrationRunContinue, "Known down target, planned.", "/orchestration_runs/12/continue"},
	} {
		collector := runWriteCollector()
		result, err := newTestService(collector).ChangeOrchestrationRun(t.Context(), 12, tc.action, tc.comment)
		if err != nil {
			t.Fatal(err)
		}
		request := collector.requests[0]
		if len(collector.requests) != 1 || request.method != "POST" || request.path != tc.path || result.Action != string(tc.action) || result.ID != 12 {
			t.Fatalf("%s: %+v, %+v", tc.action, collector.requests, result.OrchestrationRun)
		}
		if tc.comment == "" && request.body != nil || tc.comment != "" && request.body["comment"] != tc.comment {
			t.Fatalf("%s body = %+v", tc.action, request.body)
		}
	}
	for label, tc := range map[string]struct {
		id      int
		action  OrchestrationRunAction
		comment string
	}{
		"no id":              {0, OrchestrationRunStop, ""},
		"continue no reason": {12, OrchestrationRunContinue, " "},
		"comment on stop":    {12, OrchestrationRunStop, "why"},
		"unknown action":     {12, "start", ""},
	} {
		collector := runWriteCollector()
		if _, err := newTestService(collector).ChangeOrchestrationRun(t.Context(), tc.id, tc.action, tc.comment); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%s accepted", label)
		}
	}
	collector := &fakeCollector{err: statusError{http.StatusConflict, "run 13 is stopped, not running"}}
	if _, err := newTestService(collector).ChangeOrchestrationRun(t.Context(), 13, OrchestrationRunStop, ""); err == nil || !strings.Contains(err.Error(), "is stopped, not running") {
		t.Fatalf("conflict: %v", err)
	}
	collector = &fakeCollector{err: statusError{http.StatusNotFound, "run 99 not found"}}
	if _, err := newTestService(collector).ChangeOrchestrationRun(t.Context(), 99, OrchestrationRunResume, ""); err == nil || !strings.Contains(err.Error(), "list_orchestration_runs") {
		t.Fatalf("not found: %v", err)
	}
}

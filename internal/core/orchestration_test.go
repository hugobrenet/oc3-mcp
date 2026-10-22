package core

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Shaped after dev2: the listing gives the steps as a JSON text.
const orchestrationRow = `{"id":2,"name":"Test failure","description":"","step_count":2,"run_count":3,"last_run_id":12,"last_run_status":"succeeded",
	"last_run_started":"2026-10-21T12:12:49+02:00","created":"2026-10-20T15:08:13+02:00","updated":"2026-10-20T15:08:13+02:00","updated_by":"t-manager@test.local",
	"steps":"[{\"action\":\"freeze\",\"kind\":\"service\",\"name\":\"Freeze an unknown service\",\"pace\":\"all\",\"targets\":{\"ids\":[\"00000000-0000-0000-0000-000000000000\"],\"mode\":\"pick\"}},{\"action\":\"checks\",\"kind\":\"node\",\"name\":\"Run the checks\",\"pace\":\"one\",\"timeout\":600,\"targets\":{\"mode\":\"same_as\",\"step\":0}}]"}`

func TestListOrchestrationsTranslatesTheFilters(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[` + orchestrationRow + `,{"id":5,"name":"never run","step_count":1,"run_count":0,"last_run_id":null,"last_run_status":null,"updated_by":"Jane Doe"}],"meta":{"total":3}}`}
	result, err := newTestService(collector).ListOrchestrations(t.Context(), ListOrchestrationsOptions{Name: "Test failure", Action: "freeze", LastRunStatus: "succeeded", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/orchestrations" || request.query.Get("orderby") != "name,id" || strings.Contains(request.query.Get("props"), "steps") ||
		strings.Join(request.query["filter"], "|") != `name:eq:Test failure|steps:~\bfreeze\b|last_run_status:eq:succeeded` {
		t.Fatalf("request = %+v", request)
	}
	got := result.Orchestrations[0]
	if result.Total != 3 || !result.Truncated || result.NextCursor == "" || got.ID != 2 || got.StepCount != 2 || got.RunCount != 3 || *got.LastRunID != 12 ||
		got.LastRunStatus != "succeeded" || strings.Contains(got.UpdatedBy, "@") || result.Orchestrations[1].LastRunID != nil || result.Orchestrations[1].LastRunStatus != "" {
		t.Fatalf("result = %+v", result)
	}
	for _, options := range []ListOrchestrationsOptions{{Action: "rm -rf"}, {Limit: 201}, {Cursor: "garbage"}, {Name: " x"}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListOrchestrations(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestGetOrchestrationByNameOrID(t *testing.T) {
	for _, tc := range []struct{ orchestration, filter string }{{"Test failure", "name:eq:Test failure"}, {"2", "id:eq:2"}} {
		collector := &fakeCollector{body: `{"data":[` + orchestrationRow + `],"meta":{"total":1}}`}
		result, err := newTestService(collector).GetOrchestration(t.Context(), tc.orchestration)
		if err != nil {
			t.Fatal(err)
		}
		if query := collector.requests[0].query; query.Get("filter") != tc.filter || !strings.Contains(query.Get("props"), "steps") || len(collector.requests) != 1 {
			t.Fatalf("%s: query = %v", tc.orchestration, query)
		}
		if result.ID != 2 || result.RunCount != 3 || len(result.Steps) != 2 || result.Steps[0].Targets.Mode != "pick" || result.Steps[1].Targets.Step == nil ||
			*result.Steps[1].Targets.Step != 0 || result.Steps[1].Timeout != 600 || result.Steps[1].Pace != "one" {
			t.Fatalf("%s: result = %+v", tc.orchestration, result)
		}
	}
	// A pick of many ids is bounded.
	ids := make([]string, 0, 201)
	for i := range 201 {
		ids = append(ids, fmt.Sprintf(`\"%d\"`, i))
	}
	many := strings.Replace(orchestrationRow, `[\"00000000-0000-0000-0000-000000000000\"]`, "["+strings.Join(ids, ",")+"]", 1)
	result, err := newTestService(&fakeCollector{body: `{"data":[` + many + `],"meta":{"total":1}}`}).GetOrchestration(t.Context(), "2")
	if err != nil || len(result.Steps[0].Targets.IDs) != 200 || !result.Steps[0].Targets.IDsTruncated {
		t.Fatalf("bounded ids: %v %+v", err, result.Steps[0].Targets)
	}
	for body, want := range map[string]string{
		`{"data":[],"meta":{"total":0}}`: "not found in the collector",
		`{"data":[{"id":2,"name":"a","steps":"[]"},{"id":7,"name":"A","steps":"[]"}],"meta":{"total":2}}`: "IDs 2, 7: pass the ID",
	} {
		if _, err := newTestService(&fakeCollector{body: body}).GetOrchestration(t.Context(), "a"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", want, err)
		}
	}
}

func TestCheckOrchestrationSavedOrDraft(t *testing.T) {
	check := `{"data":[{"step":0,"message":"","targets":[{"label":"00000000-0000-0000-0000-000000000000","ok":false,"message":"service 00000000-0000-0000-0000-000000000000 not found","svc_id":"00000000-0000-0000-0000-000000000000"}]},
		{"step":1,"message":"same_as names step 1, whose targets are not nodes","targets":[]}]}`
	collector := &fakeCollector{bodies: map[string]string{"/orchestrations": `{"data":[` + orchestrationRow + `],"meta":{"total":1}}`, "/orchestrations/check": check}}
	result, err := newTestService(collector).CheckOrchestration(t.Context(), CheckOrchestrationOptions{Orchestration: "Test failure"})
	if err != nil {
		t.Fatal(err)
	}
	posted := collector.requests[1]
	if posted.method != "POST" || posted.path != "/orchestrations/check" || posted.body["name"] != "Test failure" || len(posted.body["steps"].([]any)) != 2 {
		t.Fatalf("posted = %+v", posted)
	}
	if result.Valid || result.Orchestration == nil || result.Orchestration.ID != 2 || result.Steps[0].RefusedCount != 1 || result.Steps[0].Action != "freeze" ||
		result.Steps[1].Message == "" || result.Steps[1].Kind != "node" {
		t.Fatalf("result = %+v", result)
	}

	// A draft: the refused targets first, bounded.
	var targets []string
	for i := range 120 {
		targets = append(targets, fmt.Sprintf(`{"label":"node%d","ok":true,"node_id":"n%d"}`, i, i))
	}
	targets = append(targets, `{"label":"ghost","ok":false,"message":"node ghost not found","node_id":"ghost"}`)
	collector = &fakeCollector{bodies: map[string]string{"/orchestrations/check": `{"data":[{"step":0,"message":"","targets":[` + strings.Join(targets, ",") + `]}]}`}}
	step := OrchestrationStep{Name: "push", Action: "pushasset", Kind: "node", Pace: "all", Targets: OrchestrationTarget{Mode: "filterset", FiltersetID: 290}}
	result, err = newTestService(collector).CheckOrchestration(t.Context(), CheckOrchestrationOptions{Steps: []OrchestrationStep{step}})
	if err != nil {
		t.Fatal(err)
	}
	if posted := collector.requests[0]; len(collector.requests) != 1 || posted.body["name"] != "draft" {
		t.Fatalf("posted = %+v", collector.requests)
	}
	if result.Valid || result.Orchestration != nil || result.Steps[0].TargetCount != 121 || result.Steps[0].RefusedCount != 1 || len(result.Steps[0].Targets) != 100 ||
		!result.Steps[0].TargetsTruncated || result.Steps[0].Targets[0].Label != "ghost" {
		t.Fatalf("draft = %+v", result.Steps[0].Targets[:2])
	}
	collector = &fakeCollector{bodies: map[string]string{"/orchestrations/check": `{"data":[{"step":0,"message":"","targets":[{"label":"n1","ok":true}]}]}`}}
	if result, err := newTestService(collector).CheckOrchestration(t.Context(), CheckOrchestrationOptions{Steps: []OrchestrationStep{step}}); err != nil || !result.Valid {
		t.Fatalf("valid draft: %+v %v", result, err)
	}

	tooMany := make([]OrchestrationStep, 51)
	for _, options := range []CheckOrchestrationOptions{{}, {Orchestration: "a", Steps: []OrchestrationStep{step}}, {Steps: tooMany}} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).CheckOrchestration(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestListOrchestrationRunsTranslatesTheFilters(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"id":13,"orchestration_id":null,"name":"mcp-test","status":"failed","current_step":0,"step_count":1,"steps_done":0,
		"message":"step 1: ams-dev-app02 timed out","started":"2026-10-22T16:09:10+02:00","ended":"2026-10-22T16:09:40+02:00","started_by":"oc3automation@opensvc.com","duration":30}],"meta":{"total":1}}`}
	result, err := newTestService(collector).ListOrchestrationRuns(t.Context(), ListOrchestrationRunsOptions{Orchestration: "5", Status: "failed", StartedBy: "seb", StartedSince: "2026-10-21"})
	if err != nil {
		t.Fatal(err)
	}
	if request := collector.requests[0]; request.path != "/orchestration_runs" || request.query.Get("orderby") != "-started,-id" ||
		strings.Join(request.query["filter"], "|") != `orchestration_id:eq:5|status:eq:failed|started_by:~seb|started:gte:2026-10-21` {
		t.Fatalf("request = %+v", request)
	}
	run := result.Runs[0]
	if run.OrchestrationID != nil || run.DurationSeconds != 30 || run.Ended == "" || strings.Contains(run.StartedBy, "@") || run.Message == "" {
		t.Fatalf("run = %+v", run)
	}
	collector = &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListOrchestrationRuns(t.Context(), ListOrchestrationRunsOptions{Orchestration: "UI test"}); err != nil || collector.requests[0].query.Get("filter") != "name:eq:UI test" {
		t.Fatalf("by name: %v %v", err, collector.requests)
	}
	for _, options := range []ListOrchestrationRunsOptions{{Status: "done"}, {StartedSince: "yesterday"}, {Limit: 201}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListOrchestrationRuns(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

// Shaped after the run 12 of dev2: the failure of its first step was
// acknowledged.
const orchestrationRunDetail = `{"id":12,"name":"Test failure","orchestration_id":2,"status":"running","current_step":1,"message":"",
	"started":"2026-10-12T10:59:00+02:00","started_by":"t-manager@test.local",
	"plan":[{"action":"freeze","kind":"service","name":"Freeze","pace":"all","targets":{"ids":["00000000-0000-0000-0000-000000000000"],"mode":"pick"}},
		{"action":"pushasset","kind":"node","name":"Push","pace":"all","targets":{"ids":["n1","n2"],"mode":"pick"}}],
	"steps":[
		{"step":0,"status":"acknowledged","counts":{"refused":1},"ack_comment":"Known down target, planned.","acked_at":"2026-10-21T14:26:37+02:00","acked_by":"t-manager@test.local",
			"targets":[{"id":35,"step":0,"position":0,"label":"00000000-0000-0000-0000-000000000000","status":"refused","message":"service not found","svc_id":"00000000-0000-0000-0000-000000000000","node_id":"","cluster_id":""}]},
		{"step":1,"status":"running","counts":{"ok":1,"failed":1},
			"targets":[
				{"id":37,"step":1,"position":1,"label":"dev2n2","status":"failed","action_id":1408,"command":"pushasset","ret":1,"stdout":"\u001b[31mred\u001b[0m","stderr":"STDERR_PLACEHOLDER","node_id":"n2"},
				{"id":36,"step":1,"position":0,"label":"dev2n1","status":"ok","action_id":1407,"command":"pushasset","ret":0,"stdout":"asset dump","stderr":"","node_id":"n1"}]}]}`

func TestGetOrchestrationRunPagesItsTargets(t *testing.T) {
	body := strings.Replace(orchestrationRunDetail, "STDERR_PLACEHOLDER", strings.Repeat("e", 3000), 1)
	collector := &fakeCollector{bodies: map[string]string{"/orchestration_runs/12": body}}
	service := newTestService(collector)
	result, err := service.GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 12, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	// From 08:59 UTC to the 09:00 UTC of the test clock, while it runs.
	if result.StepCount != 2 || result.StepsDone != 1 || result.DurationSeconds != 60 || len(result.Plan) != 2 || len(result.Steps) != 2 ||
		result.Steps[0].AckComment == "" || strings.Contains(result.Steps[0].AckedBy, "@") || strings.Contains(result.StartedBy, "@") {
		t.Fatalf("run = %+v", result.OrchestrationRun)
	}
	// By step, then position; the output of a target that succeeded is left
	// out, the other cut and without colors.
	if result.TargetTotal != 3 || len(result.Targets) != 2 || !result.Truncated || result.Targets[0].Status != "refused" || result.Targets[1].Label != "dev2n1" ||
		result.Targets[1].Stdout != "" || *result.Targets[1].ActionID != 1407 {
		t.Fatalf("targets = %+v", result.Targets)
	}
	next, err := service.GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 12, Limit: 2, Cursor: result.NextCursor})
	if err != nil || len(next.Targets) != 1 || next.Targets[0].Stdout != "red" || len(next.Targets[0].Stderr) != 2000 || !next.Targets[0].OutputTruncated || *next.Targets[0].Ret != 1 {
		t.Fatalf("next = %+v %v", next.Targets, err)
	}
	if _, err := service.GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 12, TargetStatus: "ok", Cursor: result.NextCursor}); err == nil {
		t.Fatal("a cursor was taken with other filters")
	}
	step := 1
	filtered, err := service.GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 12, Step: &step, TargetStatus: "ok", AllOutput: true})
	if err != nil || filtered.TargetTotal != 1 || filtered.Targets[0].Stdout != "asset dump" {
		t.Fatalf("filtered = %+v %v", filtered.Targets, err)
	}

	collector = &fakeCollector{err: statusError{http.StatusNotFound, "run 9999 not found"}}
	if _, err := newTestService(collector).GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 9999}); err == nil || !strings.Contains(err.Error(), "list_orchestration_runs") {
		t.Fatalf("not found: %v", err)
	}
	negative := -1
	for _, options := range []GetOrchestrationRunOptions{{}, {ID: 12, Step: &negative}, {ID: 12, TargetStatus: "done"}, {ID: 12, Limit: 101}} {
		collector := &fakeCollector{body: `{}`}
		if _, err := newTestService(collector).GetOrchestrationRun(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

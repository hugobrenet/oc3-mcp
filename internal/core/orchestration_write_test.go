package core

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// orchestrationWriteCollector answers the listing with row, a write with the
// saved orchestration, and a check with check.
func orchestrationWriteCollector(row string, check string) *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch {
		case path == "/orchestrations" && query != nil:
			if row == "" {
				return `{"data":[],"meta":{"total":0}}`
			}
			return `{"data":[` + row + `],"meta":{"total":1}}`
		case path == "/orchestrations/check":
			return check
		case path == "/orchestrations" || strings.HasPrefix(path, "/orchestrations/"):
			return `{"id":2,"name":"Test failure"}`
		}
		return `{}`
	}}
}

const orchestrationCheckOK = `{"data":[{"step":0,"message":"","targets":[{"label":"n1","ok":true}]},{"step":1,"message":"","targets":[{"label":"n1","ok":true}]}]}`

func TestCreateOrchestrationReadsItBackAndChecksIt(t *testing.T) {
	collector := orchestrationWriteCollector(orchestrationRow, `{"data":[{"step":0,"message":"","targets":[{"label":"ghost","ok":false,"message":"service ghost not found"}]},{"step":1,"message":"","targets":[]}]}`)
	description := "freeze then checks"
	step := OrchestrationStep{Name: "freeze", Action: "freeze", Kind: "service", Pace: "all", Targets: OrchestrationTarget{Mode: "pick", IDs: []string{"ghost"}}}
	result, err := newTestService(collector).CreateOrchestration(t.Context(), "Test failure", &description, []OrchestrationStep{step})
	if err != nil {
		t.Fatal(err)
	}
	posted := collector.requests[0]
	if posted.method != "POST" || posted.path != "/orchestrations" || posted.body["name"] != "Test failure" || posted.body["description"] != description || len(posted.body["steps"].([]any)) != 1 {
		t.Fatalf("posted = %+v", posted)
	}
	if read := collector.requests[1]; read.method != "GET" || read.query.Get("filter") != "id:eq:2" {
		t.Fatalf("read back = %+v", read)
	}
	if result.Outcome != OrchestrationCreated || result.Orchestration == nil || result.Orchestration.ID != 2 || result.Check == nil || result.Check.Valid ||
		result.Check.Orchestration != nil || result.Check.Steps[0].RefusedCount != 1 || result.Warning != "" {
		t.Fatalf("result = %+v", result)
	}

	// A read back that fails is told, the creation made.
	collector = orchestrationWriteCollector("", orchestrationCheckOK)
	result, err = newTestService(collector).CreateOrchestration(t.Context(), "Test failure", nil, []OrchestrationStep{step})
	if err != nil || result.Outcome != OrchestrationCreated || result.Orchestration != nil || !strings.Contains(result.Warning, "could not be read back") {
		t.Fatalf("read back failed: %+v %v", result, err)
	}
	if _, ok := collector.requests[0].body["description"]; ok {
		t.Fatalf("an absent description was sent: %+v", collector.requests[0].body)
	}

	// The refusal of the collector is given as it says it.
	collector = &fakeCollector{err: statusError{http.StatusConflict, "an orchestration is named Test failure already"}}
	if _, err := newTestService(collector).CreateOrchestration(t.Context(), "Test failure", nil, []OrchestrationStep{step}); err == nil || !strings.Contains(err.Error(), "named Test failure already") {
		t.Fatalf("conflict: %v", err)
	}
	for name, steps := range map[string][]OrchestrationStep{" ": {step}, "no steps": nil, "too many": make([]OrchestrationStep, 51)} {
		collector := orchestrationWriteCollector(orchestrationRow, orchestrationCheckOK)
		if _, err := newTestService(collector).CreateOrchestration(t.Context(), name, nil, steps); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%q accepted", name)
		}
	}
}

func TestUpdateOrchestrationKeepsWhatIsNotGiven(t *testing.T) {
	collector := orchestrationWriteCollector(orchestrationRow, orchestrationCheckOK)
	description := "now described"
	result, err := newTestService(collector).UpdateOrchestration(t.Context(), "Test failure", OrchestrationFields{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	put := collector.requests[1]
	if put.method != "PUT" || put.path != "/orchestrations/2" || put.body["name"] != "Test failure" || put.body["description"] != description {
		t.Fatalf("put = %+v", put)
	}
	// The steps sent are those held, unchanged.
	steps := put.body["steps"].([]any)
	if len(steps) != 2 || steps[1].(map[string]any)["timeout"] != float64(600) || steps[1].(map[string]any)["targets"].(map[string]any)["step"] != float64(0) {
		t.Fatalf("steps = %+v", steps)
	}
	if result.Outcome != OrchestrationChanged || result.Check == nil || !result.Check.Valid {
		t.Fatalf("result = %+v", result)
	}

	// The values held, given again, leave it unchanged.
	collector = orchestrationWriteCollector(orchestrationRow, orchestrationCheckOK)
	name := "Test failure"
	if result, err := newTestService(collector).UpdateOrchestration(t.Context(), "2", OrchestrationFields{Name: &name}); err != nil || result.Outcome != OrchestrationUnchanged {
		t.Fatalf("unchanged: %+v %v", result, err)
	}
	empty := ""
	for label, fields := range map[string]OrchestrationFields{"nothing": {}, "empty name": {Name: &empty}, "empty steps": {Steps: []OrchestrationStep{}}} {
		collector := orchestrationWriteCollector(orchestrationRow, orchestrationCheckOK)
		if _, err := newTestService(collector).UpdateOrchestration(t.Context(), "2", fields); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%s accepted", label)
		}
	}
}

func TestDeleteOrchestrationTellsWhatItWas(t *testing.T) {
	running := strings.Replace(orchestrationRow, `"last_run_status":"succeeded"`, `"last_run_status":"running"`, 1)
	collector := orchestrationWriteCollector(running, orchestrationCheckOK)
	result, err := newTestService(collector).DeleteOrchestration(t.Context(), "Test failure")
	if err != nil {
		t.Fatal(err)
	}
	if deleted := collector.requests[1]; deleted.method != "DELETE" || deleted.path != "/orchestrations/2" {
		t.Fatalf("delete = %+v", deleted)
	}
	if result.Outcome != OrchestrationDeleted || result.Orchestration.ID != 2 || len(result.Orchestration.Steps) != 2 || !strings.Contains(result.Warning, "its run 12 is running") {
		t.Fatalf("result = %+v", result)
	}
	collector = orchestrationWriteCollector("", orchestrationCheckOK)
	if _, err := newTestService(collector).DeleteOrchestration(t.Context(), "nope"); err == nil || len(collector.requests) != 1 {
		t.Fatalf("unknown: %v %+v", err, collector.requests)
	}
}

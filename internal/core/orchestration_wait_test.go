package core

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOrchestrationStepsNameAFiltersetByName(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/filtersets/dev2-nodes":
			return `{"data":[{"id":290,"fset_name":"dev2-nodes"}]}`
		case "/filtersets/nope":
			return `{"data":[]}`
		case "/orchestrations/check":
			return `{"data":[{"step":0,"message":"","targets":[{"label":"dev2n1","ok":true}]},{"step":1,"message":"","targets":[{"label":"dev2n1","ok":true}]}]}`
		}
		return `{}`
	}}
	byName := OrchestrationStep{Name: "freeze", Action: "freeze", Kind: "node", Pace: "all", Targets: OrchestrationTarget{Mode: "filterset", Filterset: "dev2-nodes"}}
	thaw := byName
	thaw.Action = "thaw"
	result, err := newTestService(collector).CheckOrchestration(t.Context(), CheckOrchestrationOptions{Steps: []OrchestrationStep{byName, thaw}})
	if err != nil || !result.Valid {
		t.Fatalf("%+v %v", result, err)
	}
	// The name is read once, and the collector receives its ID only.
	if len(collector.requests) != 2 || collector.requests[0].path != "/filtersets/dev2-nodes" {
		t.Fatalf("requests = %+v", collector.requests)
	}
	for _, step := range collector.requests[1].body["steps"].([]any) {
		targets := step.(map[string]any)["targets"].(map[string]any)
		if targets["filterset_id"] != float64(290) || targets["filterset"] != nil {
			t.Fatalf("targets sent = %+v", targets)
		}
	}
	for label, target := range map[string]OrchestrationTarget{
		"unknown":  {Mode: "filterset", Filterset: "nope"},
		"mismatch": {Mode: "filterset", Filterset: "dev2-nodes", FiltersetID: 7},
	} {
		collector.requests = nil
		step := byName
		step.Targets = target
		if _, err := newTestService(collector).CheckOrchestration(t.Context(), CheckOrchestrationOptions{Steps: []OrchestrationStep{step}}); err == nil || slicesHasPost(collector.requests) {
			t.Fatalf("%s accepted: %v", label, err)
		}
	}
}

func slicesHasPost(requests []fakeRequest) bool {
	for _, request := range requests {
		if request.method == "POST" {
			return true
		}
	}
	return false
}

func TestOrchestrationRunWaitsForItsEnd(t *testing.T) {
	interval := actionWaitInterval
	actionWaitInterval = 10 * time.Millisecond
	defer func() { actionWaitInterval = interval }()
	running := strings.Replace(orchestrationRunDetail, "STDERR_PLACEHOLDER", "", 1)
	done := strings.Replace(running, `"status":"running","current_step"`, `"status":"failed","current_step"`, 1)
	reads := 0
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/orchestrations" && query != nil {
			return `{"data":[` + orchestrationRow + `],"meta":{"total":1}}`
		}
		if path == "/orchestration_runs/12" {
			reads++
			if reads < 3 {
				return running
			}
			return done
		}
		return running
	}}
	result, err := newTestService(collector).GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 12, Wait: 5})
	if err != nil || result.Status != "failed" || reads != 3 {
		t.Fatalf("get: %s after %d reads, %v", result.Status, reads, err)
	}

	// A start answered running is read again until it ends.
	reads = 0
	started, err := newTestService(collector).StartOrchestrationRun(t.Context(), "Test failure", 5)
	if err != nil || started.Status != "failed" || started.Action != "start" || reads != 3 {
		t.Fatalf("start: %s after %d reads, %v", started.Status, reads, err)
	}
	// Without wait, one read.
	reads = 0
	once, err := newTestService(collector).GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 12})
	if err != nil || once.Status != "running" || reads != 1 || once.Waited != 0 {
		t.Fatalf("once: %+v %v", once.OrchestrationRun, err)
	}
	for _, wait := range []int{-1, 121} {
		if _, err := newTestService(collector).GetOrchestrationRun(t.Context(), GetOrchestrationRunOptions{ID: 12, Wait: wait}); err == nil {
			t.Fatalf("wait %d accepted", wait)
		}
		if _, err := newTestService(collector).StartOrchestrationRun(t.Context(), "2", wait); err == nil {
			t.Fatalf("start wait %d accepted", wait)
		}
	}
}

package core

import (
	"net/url"
	"strings"
	"testing"
)

// newServiceComplianceCollector answers the ruleset and moduleset lookups
// and the service reads, and refuses the changes as the collector does: 409
// for an attachment already there or a detachment of what is not attached,
// 403 for what is not published to a group responsible for the app.
func newServiceComplianceCollector(refusals map[string]string) complianceCollector {
	return complianceCollector{fakeCollector: &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/compliance/rulesets/os.linux.base":
			return `{"data":[{"id":12,"ruleset_name":"os.linux.base","ruleset_type":"explicit"}]}`
		case "/compliance/rulesets/ctx.dev2":
			return `{"data":[{"id":1,"ruleset_name":"ctx.dev2","ruleset_type":"contextual"}]}`
		case "/compliance/modulesets/base.linux":
			return `{"data":[{"id":7,"modset_name":"base.linux"}]}`
		case "/services":
			if strings.HasPrefix(query.Get("filter"), "svcname:in:") {
				return `{"data":[{"svc_id":"s-1","svcname":"web"}],"meta":{"total":1}}`
			}
			return `{"data":[{"svc_id":"s-1","svcname":"web"},{"svc_id":"s-2","svcname":"db"},{"svc_id":"s-3","svcname":"lab"}],"meta":{"total":3}}`
		case "/compliance/modulesets/7/services":
			return `{"data":[{"svc_id":"s-1","svcname":"web"},{"svc_id":"s-2","svcname":"db"}],"meta":{"total":2}}`
		}
		return `{"data":[]}`
	}}, refusals: refusals}
}

func TestAttachServiceRulesetGivesTheOutcomeOnEachService(t *testing.T) {
	collector := newServiceComplianceCollector(map[string]string{
		"/services/s-2/compliance/rulesets/12": "HTTP 409 Conflict: ruleset 12 is already attached to this service",
		"/services/s-3/compliance/rulesets/12": "HTTP 403 Forbidden: ruleset 12 is not attachable to this service",
	})
	change, err := New(collector).AttachServiceRuleset(t.Context(), ServiceRulesetOptions{Ruleset: "os.linux.base", ServiceSelection: ServiceSelection{Filters: ServiceFilters{App: "WEB"}}})
	if err != nil {
		t.Fatal(err)
	}
	// The outcomes are sorted by service name: db, lab, web.
	if change.Ruleset.ID != 12 || change.Selected != 3 || change.Changed != 1 || change.Unchanged != 1 || change.Refused != 1 ||
		change.Services[0].Outcome != ChangeAlreadyAttached || !strings.Contains(change.Services[1].Reason, "not attachable") || change.Services[2].Outcome != ChangeAttached {
		t.Fatalf("change = %+v", change)
	}
	// A contextual ruleset is refused before any change, and detached.
	collector = newServiceComplianceCollector(nil)
	if _, err := New(collector).AttachServiceRuleset(t.Context(), ServiceRulesetOptions{Ruleset: "ctx.dev2", ServiceSelection: ServiceSelection{Services: []string{"web"}}}); err == nil || !strings.Contains(err.Error(), "is contextual") {
		t.Fatalf("contextual ruleset: %v", err)
	}
	for _, request := range collector.requests {
		if request.method == "CHANGE" {
			t.Fatalf("a contextual ruleset sent %+v", request)
		}
	}
	detach, err := New(collector).DetachServiceRuleset(t.Context(), ServiceRulesetOptions{Ruleset: "ctx.dev2", ServiceSelection: ServiceSelection{Services: []string{"web"}}})
	if err != nil || detach.Changed != 1 || collector.requests[len(collector.requests)-1].path != "/services/s-1/compliance/rulesets/1" {
		t.Fatalf("detach of a contextual ruleset = %+v, %v", detach, err)
	}
	// As a filter, a contextual ruleset is attached to no service.
	if _, err := New(collector).DetachServiceRuleset(t.Context(), ServiceRulesetOptions{Ruleset: "ctx.dev2", ServiceSelection: ServiceSelection{Filters: ServiceFilters{Ruleset: "ctx.dev2"}}}); err == nil || !strings.Contains(err.Error(), "attached to none") {
		t.Fatalf("contextual ruleset filter: %v", err)
	}
	for _, selection := range []ServiceSelection{{}, {Node: "n-1", Filters: ServiceFilters{Moduleset: "base.linux"}}, {Filters: ServiceFilters{Tag: "a", Ruleset: "os.linux.base"}}} {
		collector := newServiceComplianceCollector(nil)
		if _, err := New(collector).AttachServiceRuleset(t.Context(), ServiceRulesetOptions{Ruleset: "os.linux.base", ServiceSelection: selection}); err == nil {
			t.Fatalf("selection %+v accepted", selection)
		}
		for _, request := range collector.requests {
			if request.method == "CHANGE" {
				t.Fatalf("selection %+v sent %+v", selection, request)
			}
		}
	}
}

// A moduleset is detached from every service holding it, read from the
// services the collector lists for it.
func TestDetachServiceModulesetFromTheServicesHoldingIt(t *testing.T) {
	collector := newServiceComplianceCollector(map[string]string{
		"/services/s-2/compliance/modulesets/7": "HTTP 409 Conflict: moduleset 7 is not attached to this service",
	})
	change, err := New(collector).DetachServiceModuleset(t.Context(), ServiceModulesetOptions{Moduleset: "base.linux", ServiceSelection: ServiceSelection{Filters: ServiceFilters{Moduleset: "base.linux", Env: "PRD"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.FilterModuleset == nil || change.FilterModuleset.ID != 7 || change.Selected != 2 || change.Changed != 1 || change.Unchanged != 1 {
		t.Fatalf("change = %+v", change)
	}
	read := 0
	for _, request := range collector.requests {
		if request.path == "/compliance/modulesets/7/services" {
			read++
			if request.query.Get("filter") != "svc_env:eq:PRD" {
				t.Fatalf("selection query = %v", request.query)
			}
		}
	}
	if read != 1 {
		t.Fatalf("the services of the moduleset were read %d times", read)
	}
}

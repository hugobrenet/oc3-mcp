package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// complianceCollector answers the ruleset and moduleset lookups and the node
// reads, and refuses the changes as the collector does on dev2: 409 for an
// attachment already there or a detachment of what is not attached, 403
// for what is not published to the group responsible for the node.
type complianceCollector struct {
	*fakeCollector
	refusals map[string]string
}

func (c complianceCollector) change(path string) error {
	c.record(fakeRequest{method: "CHANGE", path: path})
	if text, ok := c.refusals[path]; ok {
		return errString(text)
	}
	return nil
}

func (c complianceCollector) PostJSON(_ context.Context, path string, _ any, _ any) error {
	return c.change(path)
}

func (c complianceCollector) DeleteJSON(_ context.Context, path string, _ url.Values, _ any) error {
	return c.change(path)
}

func newComplianceCollector(refusals map[string]string) complianceCollector {
	return complianceCollector{fakeCollector: &fakeCollector{answer: func(path string, _ url.Values) string {
		switch path {
		case "/compliance/rulesets/os.linux.base":
			return `{"data":[{"id":12,"ruleset_name":"os.linux.base","ruleset_type":"explicit"}]}`
		case "/compliance/rulesets/ctx.dev2":
			return `{"data":[{"id":1,"ruleset_name":"ctx.dev2","ruleset_type":"contextual"}]}`
		case "/compliance/modulesets/base.linux":
			return `{"data":[{"id":7,"modset_name":"base.linux"}]}`
		case "/nodes":
			return `{"data":[{"node_id":"n-1","nodename":"a"},{"node_id":"n-2","nodename":"b"},{"node_id":"n-3","nodename":"c"}],"meta":{"total":3}}`
		}
		return `{"data":[]}`
	}}, refusals: refusals}
}

func TestAttachNodeRulesetGivesTheOutcomeOnEachNode(t *testing.T) {
	collector := newComplianceCollector(map[string]string{
		"/nodes/n-2/compliance/rulesets/12": "HTTP 409 Conflict: ruleset 12 is already attached to this node",
		"/nodes/n-3/compliance/rulesets/12": "HTTP 403 Forbidden: ruleset 12 is not attachable to this node",
	})
	change, err := New(collector).AttachNodeRuleset(t.Context(), NodeRulesetOptions{Ruleset: "os.linux.base", NodeSelection: NodeSelection{Filters: NodeFilters{OSName: "linux"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Ruleset.ID != 12 || change.Selected != 3 || change.Changed != 1 || change.Unchanged != 1 || change.Refused != 1 ||
		change.Nodes[0].Outcome != ChangeAttached || change.Nodes[1].Outcome != ChangeAlreadyAttached || !strings.Contains(change.Nodes[2].Reason, "not attachable") {
		t.Fatalf("change = %+v", change)
	}
	// A contextual ruleset applies through its filterset: it is not attached.
	collector = newComplianceCollector(nil)
	if _, err := New(collector).AttachNodeRuleset(t.Context(), NodeRulesetOptions{Ruleset: "ctx.dev2", NodeSelection: NodeSelection{Nodes: []string{"a"}}}); err == nil || !strings.Contains(err.Error(), "is contextual") {
		t.Fatalf("contextual ruleset: %v", err)
	}
	for _, request := range collector.requests {
		if request.method == "CHANGE" {
			t.Fatalf("a contextual ruleset sent %+v", request)
		}
	}
	for _, options := range []NodeRulesetOptions{{Ruleset: "a/b", NodeSelection: NodeSelection{Nodes: []string{"a"}}}, {Ruleset: "os.linux.base"}} {
		collector := newComplianceCollector(nil)
		if _, err := New(collector).AttachNodeRuleset(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestDetachNodeModulesetTakesANotAttachedOneAsDone(t *testing.T) {
	collector := newComplianceCollector(map[string]string{"/nodes/n-1/compliance/modulesets/7": "HTTP 409 Conflict: moduleset 7 is not attached to this node"})
	change, err := New(collector).DetachNodeModuleset(t.Context(), NodeModulesetOptions{Moduleset: "base.linux", NodeSelection: NodeSelection{Filters: NodeFilters{App: "WEB"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Moduleset.Name != "base.linux" || change.Changed != 2 || change.Unchanged != 1 || change.Nodes[0].Outcome != ChangeAlreadyDetached || change.Nodes[1].Outcome != ChangeDetached {
		t.Fatalf("change = %+v", change)
	}
	// A contextual ruleset may be detached: the collector lets one published
	// to Everybody be attached.
	collector = newComplianceCollector(nil)
	if change, err := New(collector).DetachNodeRuleset(t.Context(), NodeRulesetOptions{Ruleset: "ctx.dev2", NodeSelection: NodeSelection{Nodes: []string{"a"}}}); err != nil || change.Changed != 1 {
		t.Fatalf("contextual detachment = %+v, %v", change, err)
	}
	collector = newComplianceCollector(nil)
	if _, err := New(collector).AttachNodeModuleset(t.Context(), NodeModulesetOptions{Moduleset: "nosuch", NodeSelection: NodeSelection{Nodes: []string{"a"}}}); err == nil || !strings.Contains(err.Error(), "list_compliance_modulesets") {
		t.Fatalf("unknown moduleset: %v", err)
	}
}

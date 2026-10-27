package core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/client"
)

// complianceChangeCollector holds the ruleset os.base, 10, including
// os.child, published to and the responsibility of Ops, the ruleset
// os.other, 12, and the moduleset base.linux, 20; the ruleset secret.hidden
// is not published to the caller. A designer commit creates tmp:web as 31.
func complianceChangeCollector() *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/compliance/rulesets/os.base", "/compliance/rulesets/10":
			return `{"data":[{"id":10,"ruleset_name":"os.base","ruleset_type":"explicit"}]}`
		case "/compliance/rulesets/os.child", "/compliance/rulesets/11":
			return `{"data":[{"id":11,"ruleset_name":"os.child","ruleset_type":"explicit"}]}`
		case "/compliance/rulesets/os.other", "/compliance/rulesets/12":
			return `{"data":[{"id":12,"ruleset_name":"os.other","ruleset_type":"explicit"}]}`
		case "/compliance/modulesets/base.linux":
			return `{"data":[{"id":20,"modset_name":"base.linux"}]}`
		case "/compliance/rulesets/10/export":
			return `{"rulesets":[{"id":10,"ruleset_name":"os.base","rulesets":["os.child"],"publications":["Ops"],"responsibles":["Ops"]},{"id":11,"ruleset_name":"os.child"}]}`
		case "/compliance/modulesets/20/export":
			return `{"modulesets":[{"id":20,"modset_name":"base.linux","modulesets":[],"rulesets":[],"publications":["Ops"],"responsibles":["Ops"]}],"rulesets":[]}`
		case "/compliance/designer/commits":
			return `{"applied":3,"ids":{"tmp:web":31},"version":{"commit":"abc1234","changed":true}}`
		}
		return `{"data":[]}`
	}}
}

func text(value string) *string { return &value }

func designerCommits(collector *fakeCollector) []fakeRequest {
	var posts []fakeRequest
	for _, request := range collector.requests {
		if request.method == "POST" && request.path == "/compliance/designer/commits" {
			posts = append(posts, request)
		}
	}
	return posts
}

func TestApplyComplianceChangesSendsTheListAtOnce(t *testing.T) {
	collector := complianceChangeCollector()
	result, err := New(collector).ApplyComplianceChanges(t.Context(), "Add the web ruleset", []ComplianceOperation{
		{Op: "create", Kind: "ruleset", Name: text("web"), As: "tmp:web"},
		{Op: "add_variable", Kind: "ruleset", Ref: "tmp:web", VarName: text("port"), VarClass: text("raw"), VarValue: text("8080")},
		{Op: "include", Kind: "moduleset", Ref: "base.linux", ChildKind: "ruleset", Child: "tmp:web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	posts := designerCommits(collector)
	if len(posts) != 1 {
		t.Fatalf("designer commits = %d", len(posts))
	}
	body := posts[0].body
	operations, _ := body["operations"].([]any)
	if body["message"] != "[AI assistant] Add the web ruleset" || len(operations) != 3 {
		t.Fatalf("body = %v", body)
	}
	// Each operation carries the fields of its op only, an object resolved
	// by its ID.
	include := operations[2].(map[string]any)
	if include["op"] != "include" || include["child_kind"] != "ruleset" || include["child"] != "tmp:web" || include["ref"] != "20" || include["var_value"] != nil {
		t.Fatalf("include = %v", include)
	}
	variable := operations[1].(map[string]any)
	if variable["var_name"] != "port" || variable["var_value"] != "8080" || variable["child_kind"] != nil {
		t.Fatalf("add_variable = %v", variable)
	}
	// The objects changed are read back by ID; this collector gives none.
	if !strings.Contains(result.Warning, "ruleset:31") || !strings.Contains(result.Warning, "moduleset:20") {
		t.Fatalf("warning = %q", result.Warning)
	}
	if result.Outcome != ComplianceChangeApplied || result.Applied != 3 || result.Version == nil || result.Version.Commit != "abc1234" || !result.Version.Recorded ||
		len(result.Created) != 1 || result.Created[0] != (ComplianceObjectCreated{Ref: "tmp:web", Kind: "ruleset", ID: 31}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestApplyComplianceChangesChecksTheListBeforeSending(t *testing.T) {
	many := make([]ComplianceOperation, maxComplianceOperations+1)
	for index := range many {
		many[index] = ComplianceOperation{Op: "delete", Kind: "ruleset", Ref: "os.base"}
	}
	for name, test := range map[string]struct {
		message    string
		operations []ComplianceOperation
		want       string
	}{
		"no message":         {"", []ComplianceOperation{{Op: "delete", Kind: "ruleset", Ref: "os.base"}}, "message is required"},
		"too many":           {"m", many, "1 to 100 operations"},
		"create without as":  {"m", []ComplianceOperation{{Op: "create", Kind: "ruleset", Name: text("web")}}, "as is required"},
		"as without tmp":     {"m", []ComplianceOperation{{Op: "create", Kind: "ruleset", Name: text("web"), As: "web"}}, "as must be tmp:"},
		"as twice":           {"m", []ComplianceOperation{{Op: "create", Kind: "ruleset", Name: text("a"), As: "tmp:x"}, {Op: "create", Kind: "moduleset", Name: text("b"), As: "tmp:x"}}, "given by an earlier operation"},
		"reference unknown":  {"m", []ComplianceOperation{{Op: "delete", Kind: "ruleset", Ref: "tmp:x"}}, "not created by an earlier operation"},
		"reference of kind":  {"m", []ComplianceOperation{{Op: "create", Kind: "moduleset", Name: text("m"), As: "tmp:m"}, {Op: "delete", Kind: "ruleset", Ref: "tmp:m"}}, "is a moduleset, not a ruleset"},
		"op of the kind":     {"m", []ComplianceOperation{{Op: "set_type", Kind: "moduleset", Ref: "base.linux", RulesetType: "explicit"}}, "applies to a ruleset only"},
		"moduleset in rset":  {"m", []ComplianceOperation{{Op: "include", Kind: "ruleset", Ref: "os.base", ChildKind: "moduleset", Child: "base.linux"}}, "a ruleset includes rulesets only"},
		"update of nothing":  {"m", []ComplianceOperation{{Op: "update_variable", Kind: "ruleset", Ref: "os.base", Variable: "port"}}, "var_name, var_class or var_value is required"},
		"move to same role":  {"m", []ComplianceOperation{{Op: "move_team", Kind: "ruleset", Ref: "os.base", Team: "Ops", Role: "responsible", ToRole: "responsible"}}, "to_role must be the other role"},
		"caller sees no obj": {"m", []ComplianceOperation{{Op: "clone", Kind: "ruleset", Ref: "secret.hidden", As: "tmp:copy"}}, "not published to the caller"},
	} {
		t.Run(name, func(t *testing.T) {
			collector := complianceChangeCollector()
			_, err := New(collector).ApplyComplianceChanges(t.Context(), test.message, test.operations)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if len(designerCommits(collector)) != 0 {
				t.Fatal("the list was sent")
			}
		})
	}
}

// A name an earlier operation gives an object is left to the collector.
func TestApplyComplianceChangesLeavesTheNamesTheListGives(t *testing.T) {
	collector := complianceChangeCollector()
	_, err := New(collector).ApplyComplianceChanges(t.Context(), "Rename", []ComplianceOperation{
		{Op: "rename", Kind: "ruleset", Ref: "os.base", Name: text("os.common")},
		{Op: "set_public", Kind: "ruleset", Ref: "os.common", RulesetPublic: new(bool)},
	})
	if err != nil || len(designerCommits(collector)) != 1 {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyComplianceChangesTellsTheOperationsWithNothingToDo(t *testing.T) {
	collector := complianceChangeCollector()
	result, err := New(collector).ApplyComplianceChanges(t.Context(), "Clean up", []ComplianceOperation{
		{Op: "detach", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.other"},
		{Op: "remove_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing to do at all: nothing is sent, no version is recorded.
	if result.Outcome != ComplianceChangeUnchanged || len(result.Unchanged) != 2 || len(designerCommits(collector)) != 0 ||
		!strings.Contains(result.Unchanged[0].Reason, "os.other is not linked") || !strings.Contains(result.Unchanged[1].Reason, "Lab is not a publication") {
		t.Fatalf("result = %+v", result)
	}
	// A detach of a child linked has something to do: the list is sent,
	// without the remove_team of a group absent, before or after it.
	collector = complianceChangeCollector()
	result, err = New(collector).ApplyComplianceChanges(t.Context(), "Detach", []ComplianceOperation{
		{Op: "remove_team", Kind: "ruleset", Ref: "os.base", Role: "responsible", Team: "Lab"},
		{Op: "detach", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.child"},
		{Op: "remove_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
	})
	if err != nil || result.Outcome != ComplianceChangeApplied || len(result.Unchanged) != 2 || result.Unchanged[0].Index != 0 || result.Unchanged[1].Index != 2 || len(designerCommits(collector)) != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	// The operations with nothing to do are not sent: a group the collector
	// does not know would make it refuse the whole list.
	if sent, _ := designerCommits(collector)[0].body["operations"].([]any); len(sent) != 1 || sent[0].(map[string]any)["op"] != "detach" {
		t.Fatalf("operations sent = %v", sent)
	}
}

// Each operation on the links and the groups is judged on what the ones
// before it leave, in any order.
func TestApplyComplianceChangesJudgesEachLinkOnTheListBefore(t *testing.T) {
	for name, test := range map[string]struct {
		operations []ComplianceOperation
		noops      []int
	}{
		// The object changed, then its groups: the removal of a group
		// absent is still told.
		"rename then remove an absent group": {[]ComplianceOperation{
			{Op: "rename", Kind: "ruleset", Ref: "os.base", Name: text("os.common")},
			{Op: "remove_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
		}, []int{1}},
		"include a child linked already": {[]ComplianceOperation{
			{Op: "include", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.child"},
			{Op: "include", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.other"},
		}, []int{0}},
		"add a group there already": {[]ComplianceOperation{
			{Op: "add_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Ops"},
			{Op: "add_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
		}, []int{0}},
		// An include, then the detach of the same child: both have
		// something to do, as a detach then an include.
		"include then detach": {[]ComplianceOperation{
			{Op: "include", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.other"},
			{Op: "detach", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.other"},
			{Op: "detach", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.other"},
		}, []int{2}},
		"move a group in the other role already": {[]ComplianceOperation{
			{Op: "add_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
			{Op: "move_team", Kind: "ruleset", Ref: "os.base", Role: "responsible", ToRole: "publication", Team: "Lab"},
		}, []int{1}},
		// A child deleted earlier has lost its links.
		"detach a child deleted": {[]ComplianceOperation{
			{Op: "delete", Kind: "ruleset", Ref: "os.child"},
			{Op: "detach", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "os.child"},
		}, []int{1}},
		// A group by ID, a child the list creates: left to the collector.
		"what the export cannot tell": {[]ComplianceOperation{
			{Op: "remove_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "17"},
			{Op: "create", Kind: "ruleset", Name: text("new"), As: "tmp:new"},
			{Op: "detach", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "tmp:new"},
		}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := New(complianceChangeCollector()).ApplyComplianceChanges(t.Context(), "m", test.operations)
			if err != nil {
				t.Fatal(err)
			}
			var noops []int
			for _, noop := range result.Unchanged {
				noops = append(noops, noop.Index)
			}
			if !slices.Equal(noops, test.noops) {
				t.Fatalf("noops = %v, want %v: %+v", noops, test.noops, result.Unchanged)
			}
		})
	}
}

// designerRefusingCollector refuses the designer commits as the collector does.
type designerRefusingCollector struct {
	*fakeCollector
	refusal error
}

func (c designerRefusingCollector) PostJSON(ctx context.Context, path string, input any, output any) error {
	_ = c.fakeCollector.PostJSON(ctx, path, input, nil)
	return c.refusal
}

// The index the collector gives is that of the operations sent: the
// operation named is that of the list passed.
func TestApplyComplianceChangesNamesTheRefusedOperationOfTheListPassed(t *testing.T) {
	index := 0
	collector := designerRefusingCollector{fakeCollector: complianceChangeCollector(), refusal: &client.APIError{StatusCode: http.StatusNotFound, Text: "variable nope not found", Index: &index}}
	_, err := New(collector).ApplyComplianceChanges(t.Context(), "Clean", []ComplianceOperation{
		{Op: "remove_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
		{Op: "delete_variable", Kind: "ruleset", Ref: "os.base", Variable: "nope"},
	})
	if err == nil || !strings.Contains(err.Error(), "operation 1 (delete_variable ruleset os.base variable nope)") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyComplianceChangesNamesTheOperationRefused(t *testing.T) {
	index := 1
	collector := designerRefusingCollector{fakeCollector: complianceChangeCollector(), refusal: &client.APIError{
		Method: "POST", Path: "/compliance/designer/commits", StatusCode: http.StatusConflict, Text: "the ruleset already has a variable named port", Index: &index,
	}}
	_, err := New(collector).ApplyComplianceChanges(t.Context(), "Port", []ComplianceOperation{
		{Op: "set_public", Kind: "ruleset", Ref: "os.base", RulesetPublic: new(bool)},
		{Op: "add_variable", Kind: "ruleset", Ref: "os.base", VarName: text("port"), VarValue: text("80")},
	})
	if err == nil || !strings.Contains(err.Error(), "operation 1 (add_variable ruleset os.base var_name port)") || !strings.Contains(err.Error(), "HTTP 409") ||
		!strings.Contains(err.Error(), "already has a variable named port") || !strings.Contains(err.Error(), "nothing was applied") {
		t.Fatalf("error = %v", err)
	}
}

// An export that cannot be read leaves the operations on the links to the
// collector, the change going on.
func TestApplyComplianceChangesSendsWhatAnExportCannotTell(t *testing.T) {
	collector := complianceChangeCollector()
	answer := collector.answer
	collector.answer = func(path string, query url.Values) string {
		if strings.HasSuffix(path, "/export") {
			return `{}`
		}
		return answer(path, query)
	}
	result, err := New(collector).ApplyComplianceChanges(t.Context(), "m", []ComplianceOperation{
		{Op: "remove_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
	})
	if err != nil || result.Outcome != ComplianceChangeApplied || len(result.Unchanged) != 0 || len(designerCommits(collector)) != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

// An object resolved goes by its ID, which a rename earlier in the list
// leaves valid; a name the list gives goes as it is, the collector
// resolving it in the change.
func TestApplyComplianceChangesSendsTheObjectsResolvedByID(t *testing.T) {
	collector := complianceChangeCollector()
	_, err := New(collector).ApplyComplianceChanges(t.Context(), "Rename", []ComplianceOperation{
		{Op: "rename", Kind: "ruleset", Ref: "os.base", Name: text("os.common")},
		{Op: "add_team", Kind: "ruleset", Ref: "os.base", Role: "publication", Team: "Lab"},
		{Op: "delete", Kind: "ruleset", Ref: "os.other"},
		{Op: "create", Kind: "ruleset", Name: text("os.other"), As: "tmp:other"},
		{Op: "set_public", Kind: "ruleset", Ref: "os.other", RulesetPublic: new(bool)},
		{Op: "copy_variable", Kind: "ruleset", Ref: "os.common", Variable: "port", Dst: "os.child"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := designerCommits(collector)[0].body["operations"].([]any)
	var refs []string
	for _, operation := range sent {
		refs = append(refs, fmt.Sprint(operation.(map[string]any)["ref"]))
	}
	if !slices.Equal(refs, []string{"10", "10", "12", "<nil>", "os.other", "os.common"}) || sent[5].(map[string]any)["dst"] != "11" {
		t.Fatalf("refs = %v, operations = %v", refs, sent)
	}
}

// An operation naming an object deleted earlier in the list is refused
// before anything is sent, but the detach of a child deleted, which has
// nothing to do.
func TestApplyComplianceChangesRefusesWhatTheListDeletedBefore(t *testing.T) {
	for name, operations := range map[string][]ComplianceOperation{
		"change of an object deleted": {
			{Op: "delete", Kind: "ruleset", Ref: "os.other"},
			{Op: "set_public", Kind: "ruleset", Ref: "os.other", RulesetPublic: new(bool)},
		},
		"include of a child deleted": {
			{Op: "delete", Kind: "ruleset", Ref: "os.other"},
			{Op: "include", Kind: "ruleset", Ref: "os.base", ChildKind: "ruleset", Child: "12"},
		},
		"copy to a ruleset deleted": {
			{Op: "delete", Kind: "ruleset", Ref: "os.child"},
			{Op: "copy_variable", Kind: "ruleset", Ref: "os.base", Variable: "port", Dst: "os.child"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			collector := complianceChangeCollector()
			_, err := New(collector).ApplyComplianceChanges(t.Context(), "m", operations)
			if err == nil || !strings.Contains(err.Error(), "operation 1 (") || !strings.Contains(err.Error(), "is deleted earlier in the list") || len(designerCommits(collector)) != 0 {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

package core

import (
	"net/url"
	"strings"
	"testing"
)

func TestListComplianceHistoryReadsTheVersionsOfOneObject(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/compliance/rulesets/os.base":
			return `{"data":[{"id":10,"ruleset_name":"os.base","ruleset_type":"explicit"}]}`
		case "/compliance/history":
			return `{"data":[
				{"id":"abc1234","date":"2026-10-27T10:00:00+01:00","author":"Jane Doe <jane@example.com>","subject":"[AI assistant] Add the port","body":"for jane@example.com","source":"designer"},
				{"id":"def5678","date":"2026-10-26T10:00:00+01:00","author":"Ops <ops@example.com>","subject":"Changes made outside the designer","body":"","source":"elsewhere"}]}`
		}
		return `{"data":[]}`
	}}
	result, err := New(collector).ListComplianceHistory(t.Context(), "os.base", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[len(collector.requests)-1]
	if request.path != "/compliance/history" || request.query.Get("object") != "ruleset:10" || request.query.Get("limit") != "20" {
		t.Fatalf("request = %+v", request)
	}
	first := result.Versions[0]
	if result.Object != "ruleset:os.base" || len(result.Versions) != 2 || !first.ByAssistant || result.Versions[1].ByAssistant ||
		strings.Contains(first.Author, "@") || strings.Contains(first.Body, "@") || first.Source != "designer" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := New(collector).ListComplianceHistory(t.Context(), "os.base", "base.linux", "", 0); err == nil {
		t.Fatal("two objects accepted")
	}
}

func TestGetComplianceVersionGivesTheChangesAndAMaskedDiff(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path != "/compliance/history/abc1234" {
			return `{"data":[]}`
		}
		return `{
			"version":{"id":"abc1234","date":"2026-10-27T10:00:00+01:00","author":"Jane","subject":"[AI assistant] Secrets","body":"","source":"designer"},
			"previous_id":"def5678",
			"previous":{"filtersets":[],"modulesets":[{"id":20,"modset_name":"base.linux"}],"rulesets":[
				{"id":10,"ruleset_name":"os.base","variables":[]},
				{"id":12,"ruleset_name":"os.old","variables":[]}]},
			"export":{"filtersets":[{"id":5,"fset_name":"paris"}],"modulesets":[{"id":20,"modset_name":"base.linux"}],"rulesets":[
				{"id":10,"ruleset_name":"os.base","variables":[
					{"var_name":"db_password","var_class":"raw","var_value":"s3cret"},
					{"var_name":"app","var_class":"raw","var_value":"{\"api_key\":\"k-123\",\"port\":80}"}]}]},
			"diff":"--- a\n+++ b\n+      \"var_name\": \"db_password\",\n+      \"var_value\": \"s3cret\",\n+      \"var_value\": \"{\\\"api_key\\\":\\\"k-123\\\",\\\"port\\\":80}\",\n+      \"var_author\": \"jane@example.com\"\n"
		}`
	}}
	result, err := New(collector).GetComplianceVersion(t.Context(), "ABC1234")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Diff, "s3cret") || strings.Contains(result.Diff, "k-123") || strings.Contains(result.Diff, "jane@example.com") ||
		!strings.Contains(result.Diff, `"var_value": "(masked)"`) || !strings.Contains(result.Diff, `\"port\":80`) {
		t.Fatalf("diff = %s", result.Diff)
	}
	want := []ComplianceObjectChange{
		{Kind: "filterset", Name: "paris", ID: 5, Change: "added"},
		{Kind: "ruleset", Name: "os.base", ID: 10, Change: "changed"},
		{Kind: "ruleset", Name: "os.old", ID: 12, Change: "removed"},
	}
	if result.Previous != "def5678" || !result.Version.ByAssistant || len(result.Changes) != len(want) {
		t.Fatalf("result = %+v", result)
	}
	for index := range want {
		if result.Changes[index] != want[index] {
			t.Fatalf("change %d = %+v, want %+v", index, result.Changes[index], want[index])
		}
	}
	if _, err := New(collector).GetComplianceVersion(t.Context(), "HEAD~1"); err == nil {
		t.Fatal("a revision expression accepted")
	}
}

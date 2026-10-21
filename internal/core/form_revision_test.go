package core

import (
	"net/url"
	"strings"
	"testing"
)

func newFormRevisionCollector() *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/forms":
			return `{"data":[{"id":211,"form_name":"test-mcp","form_definition":{"Inputs":[]}}],"meta":{"total":1}}`
		case "/forms/211/revisions":
			return `{"data":[
				{"id":"d923","cid":"d92332af8b678dc6","start":"2026-10-20T13:53:45","content":"Author: Sebastien Guiheneuf <sg@example.com><br>    rollback to 2026-10-20 13:52:30"},
				{"id":"416a","cid":"416abe30e080cafb","start":"2026-10-20T13:52:57","content":"Author: jane@example.com <jane@example.com>"},
				{"id":"b5fe","cid":"b5fe254c86ff4efb","start":"2026-10-20T13:52:30","content":"Author: Sebastien Guiheneuf <sg@example.com>"}]}`
		case "/forms/211/revisions/416abe30e080cafb":
			return `{"data":{"oid":"0c3c","content":"Label: probe\nInputs:\n  - Id: token\n    Type: password\n    Default: hunter2\nOutputs:\n  - Dest: rest\n    Password: s3cret\n    To: ops@example.com\n"}}`
		case "/forms/211/revisions/b5fe254c86ff4efb":
			return `{"data":{"oid":"0c3b","content":"Label: probe\nInputs:\n  - Id: token\n    Type: password\n    Default: oldpass\n"}}`
		case "/forms/211/revisions/0000beef":
			// A form whose creation the collector failed to commit.
			return `{"data":""}`
		case "/forms/211/diff/416abe30e080cafb":
			return `{"data":"2026-10-20 13:52:57 +0200\n\ndiff --git a/forms b/forms\n--- a/forms\n+++ b/forms\n@@ -1,3 +1,3 @@\n-    Password: old\n+    Password: s3cret\n-    Default: oldpass\n+    Default: hunter2\n     Label: probe\n"}`
		case "/forms/211/rollback/b5fe254c86ff4efb":
			return `null`
		}
		return `{"data":[]}`
	}}
}

func TestListFormRevisionsMasksTheAuthors(t *testing.T) {
	collector := newFormRevisionCollector()
	list, err := New(collector).ListFormRevisions(t.Context(), ListFormRevisionsOptions{Form: "test-mcp", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	first, second := list.Revisions[0], list.Revisions[1]
	if list.Total != 3 || list.Count != 2 || !list.Truncated || first.Revision != "d92332af8b678dc6" || first.Date != "2026-10-20 13:53:45" ||
		first.Author != "Sebastien Guiheneuf <"+maskedRequestValue+">" || first.Rollback != "rollback to 2026-10-20 13:52:30" || strings.Contains(second.Author, "@") || second.Rollback != "" {
		t.Fatalf("list = %+v", list)
	}
}

func TestGetFormRevisionMasksTheDefinitionAndTheChange(t *testing.T) {
	collector := newFormRevisionCollector()
	revision, err := New(collector).GetFormRevision(t.Context(), GetFormRevisionOptions{Form: "test-mcp", Revision: "416abe30e080cafb"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(revision.YAML+revision.Change, "s3cret") || strings.Contains(revision.YAML+revision.Change, "hunter2") || strings.Contains(revision.YAML, "@") || !strings.Contains(revision.YAML, "Label: probe") ||
		!strings.Contains(revision.Change, "+    Password: "+maskedRequestValue) || !strings.Contains(revision.Change, "-    Password: "+maskedRequestValue) {
		t.Fatalf("revision = %+v", revision)
	}
	// compare_to goes to the collector as other.
	collector = newFormRevisionCollector()
	compared, err := New(collector).GetFormRevision(t.Context(), GetFormRevisionOptions{Form: "test-mcp", Revision: "416abe30e080cafb", CompareTo: "b5fe254c86ff4efb"})
	if err != nil || strings.Contains(compared.Change, "oldpass") || strings.Contains(compared.Change, "hunter2") {
		t.Fatalf("compared = %+v, %v", compared, err)
	}
	if last := collector.requests[len(collector.requests)-1]; last.path != "/forms/211/diff/416abe30e080cafb" || last.query.Get("other") != "b5fe254c86ff4efb" {
		t.Fatalf("diff request = %+v", last)
	}
	if _, err := New(newFormRevisionCollector()).GetFormRevision(t.Context(), GetFormRevisionOptions{Form: "test-mcp", Revision: "0000beef"}); err == nil || err.Error() != "no history recorded for form test-mcp" {
		t.Fatalf("form without history: %v", err)
	}
	for _, options := range []GetFormRevisionOptions{{Form: "test-mcp"}, {Form: "test-mcp", Revision: "../x"}, {Form: "test-mcp", Revision: "416abe30", CompareTo: "HEAD~1"}} {
		collector := newFormRevisionCollector()
		if _, err := New(collector).GetFormRevision(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestRollbackFormReadsTheFormBack(t *testing.T) {
	collector := newFormRevisionCollector()
	result, err := New(collector).RollbackForm(t.Context(), "test-mcp", "b5fe254c86ff4efb")
	if err != nil {
		t.Fatal(err)
	}
	var post fakeRequest
	for _, request := range collector.requests {
		if request.method == "POST" {
			post = request
		}
	}
	if post.path != "/forms/211/rollback/b5fe254c86ff4efb" || result.Outcome != FormRolledBack || result.Form == nil {
		t.Fatalf("post = %+v, result = %+v", post, result)
	}
	if _, err := New(newFormRevisionCollector()).RollbackForm(t.Context(), "test-mcp", "main"); err == nil {
		t.Fatal("a revision out of pattern was accepted")
	}
}

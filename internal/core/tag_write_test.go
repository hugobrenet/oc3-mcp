package core

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
)

// tagWriteCollector reads the tag mcp-test-tag, and answers its changes
// with after.
type tagWriteCollector struct {
	*fakeCollector
	after string
}

func newTagWriteCollector(after string) tagWriteCollector {
	return tagWriteCollector{fakeCollector: &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/tags":
			return `{"data":[{"tag_id":"t-1","tag_name":"mcp-test-tag"}]}`
		case "/tags/t-1":
			return `{"data":[{"tag_id":"t-1","tag_name":"mcp-test-tag","tag_created":"2026-10-21 16:00:00","tag_exclude":"","tag_data":null}]}`
		}
		return `{"data":[]}`
	}}, after: after}
}

func (c tagWriteCollector) PostJSON(ctx context.Context, path string, input any, output any) error {
	_ = c.fakeCollector.PostJSON(ctx, path, input, nil)
	return json.Unmarshal([]byte(c.after), output)
}

func (c tagWriteCollector) DeleteJSON(ctx context.Context, path string, query url.Values, output any) error {
	_ = c.fakeCollector.DeleteJSON(ctx, path, query, nil)
	return json.Unmarshal([]byte(c.after), output)
}

func TestCreateTagSendsTheFieldsGiven(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"tag_id":"t-1","tag_name":"mcp-test-tag","tag_created":"2026-10-21 16:00:00","tag_exclude":"^mcp-other$","tag_data":"{\"k\":1}"}]}`}
	exclude := "^mcp-other$"
	result, err := newTestService(collector).CreateTag(t.Context(), "mcp-test-tag", &exclude, nil)
	if err != nil {
		t.Fatal(err)
	}
	post := collector.requests[0]
	if post.method != "POST" || post.path != "/tags" || post.body["tag_name"] != "mcp-test-tag" || post.body["tag_exclude"] != exclude || len(post.body) != 2 {
		t.Fatalf("post = %+v", post)
	}
	if result.Outcome != TagCreated || result.Tag.TagID != "t-1" || result.Tag.Exclude != exclude || result.Tag.Data != `{"k":1}` {
		t.Fatalf("result = %+v", result)
	}
}

func TestUpdateTagTellsAChangeAndNone(t *testing.T) {
	collector := newTagWriteCollector(`{"data":[{"tag_id":"t-1","tag_name":"mcp-test-tag2","tag_created":"2026-10-21 16:00:00","tag_exclude":"","tag_data":null}]}`)
	name, data := "mcp-test-tag2", ""
	result, err := New(collector).UpdateTag(t.Context(), "mcp-test-tag", TagFields{Name: &name, Data: &data})
	if err != nil {
		t.Fatal(err)
	}
	post := collector.requests[len(collector.requests)-1]
	if post.method != "POST" || post.path != "/tags/t-1" || post.body["tag_name"] != name || post.body["tag_data"] != "" || len(post.body) != 2 {
		t.Fatalf("post = %+v", post)
	}
	if result.Outcome != TagChanged || result.Tag.Name != name {
		t.Fatalf("result = %+v", result)
	}
	// The collector takes a change of nothing, and answers with the tag.
	collector = newTagWriteCollector(`{"data":[{"tag_id":"t-1","tag_name":"mcp-test-tag","tag_created":"2026-10-21 16:00:00","tag_exclude":"","tag_data":null}]}`)
	result, err = New(collector).UpdateTag(t.Context(), "mcp-test-tag", TagFields{})
	if err != nil || result.Outcome != TagUnchanged {
		t.Fatalf("unchanged = %+v, %v", result, err)
	}
}

func TestDeleteTagCountsTheAttachments(t *testing.T) {
	collector := newTagWriteCollector(`{"info":"3 node attachments deleted, 1 service attachments deleted, tag 'mcp-test-tag' deleted"}`)
	result, err := New(collector).DeleteTag(t.Context(), "mcp-test-tag")
	if err != nil {
		t.Fatal(err)
	}
	last := collector.requests[len(collector.requests)-1]
	if last.method != "DELETE" || last.path != "/tags/t-1" || result.Outcome != TagDeleted || *result.NodeAttachments != 3 || *result.ServiceAttachments != 1 || result.Tag.Name != "mcp-test-tag" || result.Warning != "" {
		t.Fatalf("delete = %+v, result = %+v", last, result)
	}
	collector = newTagWriteCollector(`{"info":"0 node attachments deleted, 0 service attachments deleted, tag 'mcp-test-tag' not deleted"}`)
	result, err = New(collector).DeleteTag(t.Context(), "mcp-test-tag")
	if err != nil || result.Outcome != TagUnchanged || result.Warning == "" {
		t.Fatalf("not deleted = %+v, %v", result, err)
	}
}

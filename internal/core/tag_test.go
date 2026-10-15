package core

import (
	"strings"
	"testing"
)

func TestListTagsOfTheCollector(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/tags": `{"data":[
			{"tag_id":"t-1","tag_name":"keyval::test","tag_created":"2026-10-12T14:36:56+02:00","tag_exclude":"","tag_data":[{"key":"shared","op":"=","value":"from the tag"}]},
			{"tag_id":"t-2","tag_name":"pci-dss","tag_exclude":"","tag_data":{"scope":"compliance"}},
			{"tag_id":"t-3","tag_name":"decommission-q4","tag_exclude":"^pci-dss$","tag_data":null}],"meta":{"total":4}}`,
	}}
	tags, err := newTestService(collector).ListTags(t.Context(), ListTagsOptions{Name: "a.b", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/tags" || request.query.Get("filter") != `tag_name:~a\.b` || request.query.Get("orderby") != "tag_name,tag_id" || strings.Contains(request.query.Get("props"), "tag_attach_data") {
		t.Fatalf("request = %+v", request)
	}
	if tags.Total != 4 || tags.Count != 3 || !tags.Truncated || tags.NextCursor == "" || tags.Node != nil ||
		tags.Tags[0].Data != `[{"key":"shared","op":"=","value":"from the tag"}]` || tags.Tags[1].Data != `{"scope":"compliance"}` ||
		tags.Tags[2].Data != "" || tags.Tags[2].Exclude != "^pci-dss$" || tags.Tags[0].Created != "2026-10-12T14:36:56+02:00" {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestListTagsOfANodeOrAService(t *testing.T) {
	long := strings.Repeat("é", maxTagDataBytes)
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/nodes/n-1/tags": `{"data":[{"tag_id":"t-1","tag_name":"keyval::test","tag_data":"[{\"key\": \"shared\"}]",
			"tag_attach_data":"` + long + `"}],"meta":{"total":1}}`,
		"/services/foo":      `{"data":[{"svc_id":"s-1","svcname":"foo","cluster_id":"c1"}]}`,
		"/services/s-1/tags": `{"data":[{"tag_id":"t-2","tag_name":"backup-nightly","tag_attach_data":""}],"meta":{"total":1}}`,
	}}
	tags, err := newTestService(collector).ListTags(t.Context(), ListTagsOptions{Node: "dev2n1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(collector.requests[1].query.Get("props"), "tag_attach_data") || collector.requests[1].query.Get("limit") != "50" {
		t.Fatalf("request = %+v", collector.requests[1])
	}
	got := tags.Tags[0]
	if tags.Node.Nodename != "dev2n1" || got.Data != `[{"key": "shared"}]` || !got.DataTruncated || len(got.AttachData) > maxTagDataBytes || tags.Truncated {
		t.Fatalf("node tags = %+v", tags)
	}
	tags, err = newTestService(collector).ListTags(t.Context(), ListTagsOptions{Service: "foo"})
	if err != nil || tags.Service.Svcname != "foo" || tags.Tags[0].Name != "backup-nightly" || tags.Tags[0].DataTruncated {
		t.Fatalf("service tags = %+v, %v", tags, err)
	}
}

func TestListTagsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListTagsOptions{
		{Node: "dev2n1", Service: "foo"},
		{Node: "dev2n1", Name: "pci"},
		{Name: " padded"},
		{Limit: 201},
		{Cursor: "garbage"},
		{Node: "a/b"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListTags(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

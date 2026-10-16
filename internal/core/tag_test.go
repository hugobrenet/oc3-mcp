package core

import (
	"net/url"
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

func TestTagFilterReadsTheNodesAndServicesHoldingTheTag(t *testing.T) {
	const tagID = "8c4a1d2e-0f3b-4c5d-9e6f-7a8b9c0d1e2f"
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/tags":
			return `{"data":[{"tag_id":"` + tagID + `","tag_name":"pci-dss"}],"meta":{"total":1}}`
		case "/tags/" + tagID + "/nodes":
			if query.Get("stats") == "true" {
				return `{"data":{"app":{"WEB":1,"DATA":1}},"meta":{"total":2,"distinct":{"app":2}}}`
			}
			return `{"data":[{"node_id":"n-1","nodename":"ams-dev-app01"}],"meta":{"total":2}}`
		case "/tags/" + tagID + "/services":
			if query.Get("stats") == "true" {
				return `{"data":{"svc_status":{"down":1}},"meta":{"total":1,"distinct":{"svc_status":1}}}`
			}
			return `{"data":[{"svc_id":"s-1","svcname":"foo"}],"meta":{"total":1}}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	service := newTestService(collector)
	nodes, err := service.ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{OSName: "linux", Tag: "PCI-DSS"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[0].query.Get("filter") != "tag_name:eq:PCI-DSS" ||
		collector.requests[1].query.Get("filter") != "os_name:~linux" || collector.requests[1].query.Get("orderby") != "nodename,node_id" {
		t.Fatalf("requests = %+v", collector.requests)
	}
	if nodes.Tag == nil || nodes.Tag.TagID != tagID || nodes.Tag.Name != "pci-dss" || nodes.Total != 2 || !nodes.Truncated {
		t.Fatalf("nodes = %+v", nodes)
	}
	// A cursor is bound to the tag: it does not continue the list of all nodes.
	if _, err := service.ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{OSName: "linux"}, Cursor: nodes.NextCursor}); err == nil {
		t.Fatal("cursor of a tagged list accepted without the tag")
	}
	count, err := service.CountNodes(t.Context(), CountNodesOptions{NodeFilters: NodeFilters{Tag: tagID}, GroupBy: "app"})
	if err != nil || count.Tag == nil || count.Total != 2 || len(count.Groups) != 2 {
		t.Fatalf("count = %+v, %v", count, err)
	}
	if request := collector.requests[len(collector.requests)-2]; request.query.Get("filter") != "tag_id:eq:"+tagID {
		t.Fatalf("tag ID lookup = %+v", request)
	}
	services, err := service.ListServices(t.Context(), ListServicesOptions{ServiceFilters: ServiceFilters{Status: "down", Tag: "pci-dss"}})
	if err != nil || services.Tag == nil || services.Total != 1 || services.Services[0].Svcname != "foo" {
		t.Fatalf("services = %+v, %v", services, err)
	}
	if request := collector.requests[len(collector.requests)-1]; request.path != "/tags/"+tagID+"/services" || request.query.Get("filter") != "svc_status:eq:down" {
		t.Fatalf("services request = %+v", request)
	}
	serviceCount, err := service.CountServices(t.Context(), CountServicesOptions{ServiceFilters: ServiceFilters{Tag: "pci-dss"}, GroupBy: "status"})
	if err != nil || serviceCount.Tag == nil || serviceCount.Total != 1 || serviceCount.Groups[0].Value != "down" {
		t.Fatalf("service count = %+v, %v", serviceCount, err)
	}
}

func TestTagFilterRefusesUnknownAndAmbiguousTags(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{Tag: "nosuch"}}); err == nil ||
		!strings.Contains(err.Error(), "list_tags") || len(collector.requests) != 1 {
		t.Fatalf("unknown tag: %v, %d requests", err, len(collector.requests))
	}
	collector = &fakeCollector{body: `{"data":[{"tag_id":"t-1","tag_name":"Test"},{"tag_id":"t-2","tag_name":"TEST"}],"meta":{"total":2}}`}
	if _, err := newTestService(collector).CountServices(t.Context(), CountServicesOptions{ServiceFilters: ServiceFilters{Tag: "test"}}); err == nil ||
		!strings.Contains(err.Error(), "t-1 (Test)") || !strings.Contains(err.Error(), "t-2 (TEST)") {
		t.Fatalf("ambiguous tag: %v", err)
	}
	// The exact spelling picks one of the tags the collector matches case-insensitively.
	collector = &fakeCollector{answer: func(path string, _ url.Values) string {
		if path == "/tags" {
			return `{"data":[{"tag_id":"t-1","tag_name":"Test"},{"tag_id":"t-2","tag_name":"TEST"}],"meta":{"total":2}}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	if count, err := newTestService(collector).CountNodes(t.Context(), CountNodesOptions{NodeFilters: NodeFilters{Tag: "TEST"}}); err != nil ||
		count.Tag.TagID != "t-2" || collector.requests[1].path != "/tags/t-2/nodes" {
		t.Fatalf("exact spelling: %+v, %v", count, err)
	}
	for _, options := range []ListServicesOptions{
		{Node: "dev2n1", ServiceFilters: ServiceFilters{Tag: "pci-dss"}},
		{ServiceFilters: ServiceFilters{Tag: " pci-dss"}},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListServices(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

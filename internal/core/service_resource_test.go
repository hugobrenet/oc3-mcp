package core

import (
	"strings"
	"testing"
)

func TestListServiceResourcesQueriesResources(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/kvm%2Fsvc%2Fvm1": `{"data":[{"svc_id":"s-1","svcname":"kvm/svc/vm1","cluster_id":"c1"}]}`,
		"/nodes/dev2n1":             `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/resources": `{"data":[{"id":292851215,"rid":"disk#caca1","node_id":"n-1","nodes.nodename":"dev2n1","res_type":"disk.disk","res_status":"down",
			"res_desc":"disk 600140000000000000000000000cc331","res_log":"","res_monitor":"T","res_disable":"F","res_optional":"T","vmname":"",
			"changed":"2026-10-11 23:35:43","updated":"2026-10-12 14:29:10"}],"meta":{"total":14}}`,
	}}
	resources, err := newTestService(collector).ListServiceResources(t.Context(), ListServiceResourcesOptions{
		Service: "kvm/svc/vm1", Node: "dev2n1", Status: "down", Type: "disk.disk", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[2]
	if request.path != "/resources" || request.query.Get("orderby") != "nodes.nodename,rid,id" ||
		strings.Join(request.query["filter"], "|") != "svc_id:eq:s-1|node_id:eq:n-1|res_status:eq:down|res_type:eq:disk.disk" {
		t.Fatalf("request = %+v", request)
	}
	if resources.Service.Svcname != "kvm/svc/vm1" || resources.Node == nil || resources.Total != 14 || !resources.Truncated || resources.NextCursor == "" {
		t.Fatalf("page = %+v", resources)
	}
	got := resources.Resources[0]
	if got.RID != "disk#caca1" || got.Type != "disk.disk" || got.Status != "down" || got.Description != "disk 600140000000000000000000000cc331" ||
		got.Nodename != "dev2n1" || !got.Monitored || got.Disabled || !got.Optional || got.Changed != "2026-10-11 23:35:43" || got.Log != "" {
		t.Fatalf("resource = %+v", got)
	}
}

func TestListServiceResourcesBoundsTheLog(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/web": `{"data":[{"svc_id":"s-1","svcname":"web"}]}`,
		"/resources":    `{"data":[{"rid":"fs#1","res_log":"` + strings.Repeat("a", maxResourceLogBytes+10) + `"}],"meta":{"total":1}}`,
	}}
	resources, err := newTestService(collector).ListServiceResources(t.Context(), ListServiceResourcesOptions{Service: "web"})
	if err != nil {
		t.Fatal(err)
	}
	got := resources.Resources[0]
	if len(got.Log) != maxResourceLogBytes || !got.LogTruncated || resources.Truncated || collector.requests[1].query.Get("limit") != "100" {
		t.Fatalf("resource log = %d bytes, cut %v, page %+v", len(got.Log), got.LogTruncated, resources)
	}
}

func TestListServiceResourcesRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListServiceResourcesOptions{
		{},
		{Service: "two words"},
		{Service: "web", Status: " down"},
		{Service: "web", Limit: 201},
		{Service: "web", Type: strings.Repeat("a", 256)},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).ListServiceResources(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{body: `{"data":[{"svc_id":"s-1","svcname":"web"}],"meta":{"total":1}}`}
	if _, err := newTestService(collector).ListServiceResources(t.Context(), ListServiceResourcesOptions{Service: "web", Cursor: "garbage"}); err == nil {
		t.Fatal("an invalid cursor was accepted")
	}
}

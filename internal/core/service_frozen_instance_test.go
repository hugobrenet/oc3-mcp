package core

import (
	"strings"
	"testing"
)

func TestListFrozenInstancesQueriesFrozenInstances(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":    `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/services/toto13": `{"data":[{"svc_id":"s-1","svcname":"toto13","cluster_id":"c1"}]}`,
		"/services_instances": `{"data":[{"svc_id":"s-1","services.svcname":"toto13","services.cluster_id":"c1","services.svc_frozen":"frozen",
			"node_id":"n-1","nodes.nodename":"dev2n1","mon_frozen_at":"2026-09-22 11:47:43","mon_availstatus":"down","mon_overallstatus":"down"}],
			"meta":{"total":3}}`,
	}}
	instances, err := newTestService(collector).ListFrozenInstances(t.Context(), ListFrozenInstancesOptions{
		FrozenBefore: "2026-09-25", Service: "toto13", Node: "dev2n1", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[2]
	if request.path != "/services_instances" || request.query.Get("orderby") != "mon_frozen_at,svc_id,node_id" ||
		strings.Join(request.query["filter"], "|") != "mon_frozen:ne:0|svc_id:eq:s-1|node_id:eq:n-1|mon_frozen_at:lte:2026-09-25" {
		t.Fatalf("request = %+v", request)
	}
	got := instances.Instances[0]
	if instances.Service.Svcname != "toto13" || instances.Node.Nodename != "dev2n1" || instances.Total != 3 || !instances.Truncated ||
		got.Svcname != "toto13" || got.Nodename != "dev2n1" || got.FrozenAt != "2026-09-22 11:47:43" || got.ServiceFrozen != "frozen" || got.Avail != "down" {
		t.Fatalf("instances = %+v", instances)
	}
}

func TestListFrozenInstancesRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListFrozenInstancesOptions{
		{FrozenBefore: "last week"},
		{Limit: 201},
		{Service: "two words"},
		{Node: "a/b"},
		{Cursor: "garbage"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListFrozenInstances(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	instances, err := newTestService(collector).ListFrozenInstances(t.Context(), ListFrozenInstancesOptions{})
	if err != nil || collector.requests[0].query.Get("filter") != "mon_frozen:ne:0" || instances.Instances == nil || collector.requests[0].query.Get("limit") != "100" {
		t.Fatalf("no filter = %+v, %v, %+v", instances, err, collector.requests)
	}
}

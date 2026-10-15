package core

import (
	"strings"
	"testing"
)

func TestListInstancesTranslatesTheFilters(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/services_instances": `{"data":[{"svc_id":"s-1","services.svcname":"web","services.cluster_id":"c1","services.svc_topology":"failover",
			"services.svc_availstatus":"up","services.svc_frozen":"unfrozen","node_id":"n-1","nodes.nodename":"dev2n1","mon_availstatus":"down",
			"mon_overallstatus":"down","mon_frozen":"0","mon_frozen_at":"0000-00-00 00:00:00","mon_smon_status":"idle","mon_vmname":"web1","mon_vmtype":"docker",
			"mon_updated":"2026-10-15 22:46:16"}],"meta":{"total":3}}`,
	}}
	instances, err := newTestService(collector).ListInstances(t.Context(), ListInstancesOptions{
		InstanceFilters: InstanceFilters{Node: "dev2n1", App: "shop", Availability: "down", Overall: "down", ServiceAvailability: "up", Topology: "failover"},
		Limit:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[1]
	if request.path != "/services_instances" || request.query.Get("orderby") != "nodes.nodename,services.svcname,svc_id,node_id,mon_vmname" ||
		strings.Join(request.query["filter"], "|") != "node_id:eq:n-1|services.svc_app:eq:shop|mon_availstatus:eq:down|mon_overallstatus:eq:down|services.svc_availstatus:eq:up|services.svc_topology:eq:failover" {
		t.Fatalf("request = %+v", request)
	}
	got := instances.Instances[0]
	if instances.Node.Nodename != "dev2n1" || instances.Total != 3 || !instances.Truncated || instances.NextCursor == "" ||
		got.Svcname != "web" || got.Topology != "failover" || got.ServiceAvailability != "up" || got.Availability != "down" || got.Frozen || got.FrozenAt != "" ||
		got.MonitorStatus != "idle" || got.VMName != "web1" || got.VMType != "docker" || got.LastReportedAt != "2026-10-15 22:46:16" {
		t.Fatalf("instances = %+v", instances)
	}
}

func TestListInstancesFrozenComeTheLongestFrozenFirst(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/toto13": `{"data":[{"svc_id":"s-1","svcname":"toto13","cluster_id":"c1"}]}`,
		"/services_instances": `{"data":[{"svc_id":"s-1","services.svcname":"toto13","services.svc_frozen":"frozen","node_id":"n-1",
			"nodes.nodename":"dev2n1","mon_frozen":"1","mon_frozen_at":"2026-09-22 11:47:43","mon_availstatus":"down"}],"meta":{"total":1}}`,
	}}
	instances, err := newTestService(collector).ListInstances(t.Context(), ListInstancesOptions{InstanceFilters: InstanceFilters{Service: "toto13", FrozenBefore: "2026-09-25"}})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[1]
	if request.query.Get("orderby") != "mon_frozen_at,svc_id,node_id,mon_vmname" || request.query.Get("limit") != "100" ||
		strings.Join(request.query["filter"], "|") != "mon_frozen:ne:0|svc_id:eq:s-1|mon_frozen_at:lte:2026-09-25" {
		t.Fatalf("request = %+v", request)
	}
	got := instances.Instances[0]
	if instances.Service.Svcname != "toto13" || !got.Frozen || got.FrozenAt != "2026-09-22 11:47:43" || got.ServiceFrozen != "frozen" {
		t.Fatalf("instances = %+v", instances)
	}
	collector.requests = nil
	if _, err := newTestService(collector).ListInstances(t.Context(), ListInstancesOptions{InstanceFilters: InstanceFilters{Frozen: true}}); err != nil ||
		collector.requests[0].query.Get("filter") != "mon_frozen:ne:0" || collector.requests[0].query.Get("orderby") != "mon_frozen_at,svc_id,node_id,mon_vmname" {
		t.Fatalf("frozen = %v, %+v", err, collector.requests)
	}
}

func TestCountInstancesGroupsAndNamesTheFreezeFlags(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services_instances": `{"data":{"nodes.nodename":{"dev2n1":84,"dev2n3":84,"dev2n2":62}},"meta":{"total":230,"distinct":{"nodes.nodename":3},"other":{"nodes.nodename":0}}}`,
	}}
	count, err := newTestService(collector).CountInstances(t.Context(), CountInstancesOptions{InstanceFilters: InstanceFilters{Availability: "down"}, GroupBy: "node"})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.query.Get("stats") != "true" || request.query.Get("props") != "nodes.nodename" || request.query.Get("filter") != "mon_availstatus:eq:down" {
		t.Fatalf("request = %+v", request)
	}
	if count.Total != 230 || count.Distinct != 3 || len(count.Groups) != 3 || count.Groups[0] != (CountGroup{Value: "dev2n1", Count: 84}) || count.Groups[2].Value != "dev2n2" {
		t.Fatalf("count = %+v", count)
	}
	collector = &fakeCollector{bodies: map[string]string{
		"/services_instances": `{"data":{"mon_frozen":{"0":336,"1":9}},"meta":{"total":345,"distinct":{"mon_frozen":2},"other":{"mon_frozen":0}}}`,
	}}
	count, err = newTestService(collector).CountInstances(t.Context(), CountInstancesOptions{GroupBy: "frozen"})
	if err != nil || count.Distinct != 2 || count.Groups[0] != (CountGroup{Value: "unfrozen", Count: 336}) || count.Groups[1] != (CountGroup{Value: "frozen", Count: 9}) {
		t.Fatalf("frozen count = %+v, %v", count, err)
	}
}

func TestInstanceToolsRejectInvalidInputs(t *testing.T) {
	for _, filters := range []InstanceFilters{
		{FrozenBefore: "last week"},
		{Topology: "cluster"},
		{Availability: " up"},
		{Service: "two words"},
		{Node: "a/b"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListInstances(t.Context(), ListInstancesOptions{InstanceFilters: filters}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("list filters %+v accepted", filters)
		}
		if _, err := newTestService(collector).CountInstances(t.Context(), CountInstancesOptions{InstanceFilters: filters}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("count filters %+v accepted", filters)
		}
	}
	collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	for _, options := range []ListInstancesOptions{{Limit: 201}, {Cursor: "garbage"}} {
		if _, err := newTestService(collector).ListInstances(t.Context(), options); err == nil {
			t.Fatalf("options %+v accepted", options)
		}
	}
	if _, err := newTestService(collector).CountInstances(t.Context(), CountInstancesOptions{GroupBy: "color"}); err == nil || !strings.Contains(err.Error(), "service_availability") {
		t.Fatalf("group_by color: %v", err)
	}
}

package core

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestListStorageArraysSumsTheirDiskGroups(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, _ url.Values) string {
		switch path {
		case "/arrays":
			return `{"data":[{"id":7,"array_name":"lyn-pmax-01","array_model":"PowerMax_8500 ","array_cache":2097152,"array_updated":"2026-10-19 08:00:00"}],"meta":{"total":3}}`
		case "/arrays/7/diskgroups":
			return `{"data":[{"id":1,"dg_name":"SRP_1","dg_size":1000,"dg_used":250,"dg_free":750,"dg_reserved":400},
				{"id":2,"dg_name":"SRP_2","dg_size":0,"dg_used":0,"dg_free":0}]}`
		}
		return `{"data":[]}`
	}}
	list, err := newTestService(collector).ListStorageArrays(t.Context(), ListStorageArraysOptions{Model: "powermax", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[0].query.Get("filter") != "array_model:~powermax" || collector.requests[0].query.Get("orderby") != "array_name,id" {
		t.Fatalf("request = %+v", collector.requests[0])
	}
	array := list.Arrays[0]
	if list.Total != 3 || !list.Truncated || list.NextCursor == "" || array.Model != "PowerMax_8500" || array.Cache != "2097152" ||
		array.SizeMB != 1000 || array.UsedMB != 250 || array.FreeMB != 750 || len(array.DiskGroups) != 2 ||
		array.DiskGroups[0].UsedPct != 25 || array.DiskGroups[0].ReservedMB != 400 || array.DiskGroups[1].UsedPct != 0 {
		t.Fatalf("list = %+v", list)
	}
	for _, options := range []ListStorageArraysOptions{{Name: " a"}, {Limit: 51}, {Cursor: "bad"}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListStorageArrays(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestListStorageQuotasOfEveryApp(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/apps":
			return `{"data":[{"id":1,"app":"DATA"},{"id":2,"app":"WEB"}],"meta":{"total":3}}`
		case "/apps/1/quotas":
			return `{"data":[{"app":"DATA","array_name":"lyn-pmax-01","dg_name":"SRP_2","quota":100},{"app":"DATA","array_name":"lyn-pmax-01","dg_name":"SRP_1","quota":200,"quota_used":50}]}`
		case "/apps/2/quotas":
			return `{"data":[{"app":"WEB","array_name":"lyn-pmax-01","dg_name":"SRP_1","quota":10}]}`
		}
		return `{"data":[]}`
	}}
	list, err := newTestService(collector).ListStorageQuotas(t.Context(), ListStorageQuotasOptions{Array: "lyn-pmax-01", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[1].query.Get("filter") != "array_name:eq:lyn-pmax-01" {
		t.Fatalf("request = %+v", collector.requests[1])
	}
	if list.Total != 3 || !list.ScanTruncated || list.Count != 2 || !list.Truncated || list.Quotas[0].DiskGroup != "SRP_1" || list.Quotas[0].UsedMB != 50 || list.Quotas[1].DiskGroup != "SRP_2" {
		t.Fatalf("list = %+v", list)
	}
	next, err := newTestService(collector).ListStorageQuotas(t.Context(), ListStorageQuotasOptions{Array: "lyn-pmax-01", Limit: 2, Cursor: list.NextCursor})
	if err != nil || next.Count != 1 || next.Quotas[0].App != "WEB" || next.Truncated {
		t.Fatalf("next = %+v, %v", next, err)
	}
	if _, err := newTestService(collector).ListStorageQuotas(t.Context(), ListStorageQuotasOptions{Array: "other", Cursor: list.NextCursor}); err == nil {
		t.Fatal("a cursor of other filters accepted")
	}
}

func TestListStorageQuotasOfAnUnknownApp(t *testing.T) {
	collector := &fakeCollector{err: fakeNotFound{}}
	if _, err := newTestService(collector).ListStorageQuotas(t.Context(), ListStorageQuotasOptions{App: "nosuch"}); err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("unknown app: %v", err)
	}
	if collector.requests[0].path != "/apps/nosuch/quotas" {
		t.Fatalf("requests = %+v", collector.requests)
	}
}

func TestListSANSwitchPortsOfANode(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, _ url.Values) string {
		switch path {
		case "/nodes/kvm1":
			return `{"data":[{"node_id":"n-1","nodename":"kvm1","cluster_id":"c1"}]}`
		case "/san-switches":
			return `{"data":[{"sw_fabric":"a","sw_name":"edge01","sw_slot":0,"sw_port":3,"sw_portspeed":32,"sw_porttype":"F-Port","sw_portstate":"Online",
				"sw_portname":"20:03","sw_rportname":"10:00","sw_rname":"kvm1","node_id":"n-1"}],"meta":{"total":1}}`
		}
		return `{"data":[]}`
	}}
	list, err := newTestService(collector).ListSANSwitchPorts(t.Context(), ListSANSwitchPortsOptions{Node: "kvm1", Fabric: "a", Type: "F-Port"})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[1]
	if request.path != "/san-switches" || strings.Join(request.query["filter"], " ") != "node_id:eq:n-1 sw_fabric:eq:a sw_porttype:eq:F-Port" ||
		request.query.Get("orderby") != "sw_fabric,sw_name,sw_slot,sw_port,id" {
		t.Fatalf("request = %+v", request)
	}
	port := list.Ports[0]
	if list.Node.Nodename != "kvm1" || list.Total != 1 || list.Truncated || port.SpeedGbps != 32 || port.ConnectedTo != "kvm1" || port.RemotePort != "10:00" || port.Port != 3 {
		t.Fatalf("list = %+v", list)
	}
	collector = &fakeCollector{answer: func(path string, query url.Values) string {
		switch query.Get("groupby") + " " + query.Get("filter") {
		case "sw_fabric sw_fabric:~a", "sw_fabric sw_fabric:~lyn-fabric-a":
			return `{"data":[{"sw_fabric":"lyn-fabric-a"},{"sw_fabric":"lyn-fabric-b"}]}`
		case "sw_name sw_name:~edge01":
			return `{"data":[{"sw_name":"lyn-sana-edge01"}]}`
		case "sw_rname ":
			return `{"data":[{"sw_rname":"kvm1"},{"sw_rname":"lyn-vsp-01"}]}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	for options, want := range map[ListSANSwitchPortsOptions]string{
		{Fabric: "a"}:                              "map[fabric:[lyn-fabric-a lyn-fabric-b]]",
		{Switch: "edge01"}:                         "map[switch:[lyn-sana-edge01]]",
		{ConnectedTo: "nosuch"}:                    "map[connected_to:[kvm1 lyn-vsp-01]]",
		{Fabric: "lyn-fabric-a", Switch: "edge01"}: "map[switch:[lyn-sana-edge01]]",
	} {
		list, err := newTestService(collector).ListSANSwitchPorts(t.Context(), options)
		if err != nil || fmt.Sprint(list.NamesContaining) != want || list.Total != 0 {
			t.Fatalf("options %+v: %+v, %v", options, list, err)
		}
	}
	collector = &fakeCollector{answer: func(path string, query url.Values) string {
		if query.Get("groupby") == "sw_fabric" {
			return `{"data":[{"sw_fabric":"lyn-fabric-b"}]}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	if list, err := newTestService(collector).ListSANSwitchPorts(t.Context(), ListSANSwitchPortsOptions{Fabric: "lyn-fabric-b", State: "No_Light"}); err != nil || list.Total != 0 || list.NamesContaining != nil {
		t.Fatalf("known fabric without match: %+v, %v", list, err)
	}
	for _, options := range []ListSANSwitchPortsOptions{{Type: "f-port"}, {State: " Online"}, {Limit: 501}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListSANSwitchPorts(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

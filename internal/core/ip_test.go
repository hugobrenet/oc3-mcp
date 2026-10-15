package core

import (
	"net/url"
	"strings"
	"testing"
)

func TestListIPsByAddressReadsNodeAndServiceAddresses(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/ips": `{"data":[{"node_id":"n-1","nodename":"dev2n1","intf":"br-prd","mac":"52:54:00:00:00:01","type":"ipv6","addr":"2001:db8::21","mask":"64",
			"updated":"2026-10-15 17:44:40","flag_deprecated":0,"net_id":null,"net_name":""}],"meta":{"total":1}}`,
		"/resources": `{"data":[
			{"svc_id":"s-1","services.svcname":"testnfs","node_id":"n-1","nodes.nodename":"dev2n1","rid":"ip#1","res_type":"ip.host","res_status":"up","res_desc":"host 2001:db8::21/64 br-prd","updated":"2026-10-15 21:52:01"},
			{"svc_id":"s-1","services.svcname":"testnfs","node_id":"n-2","nodes.nodename":"dev2n2","rid":"ip#1","res_type":"ip.host","res_status":"down","res_desc":"host 2001:db8::21/64 br-prd","updated":"2026-10-15 21:52:01"}],"meta":{"total":3}}`,
	}}
	ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{IP: "2001:0DB8:0:0::0021"})
	if err != nil {
		t.Fatal(err)
	}
	if len(collector.requests) != 2 {
		t.Fatalf("requests = %+v", collector.requests)
	}
	page, resources := collector.requests[0], collector.requests[1]
	if page.path != "/ips" || strings.Join(page.query["filter"], "|") != "addr:eq:2001:db8::21" || page.query.Get("orderby") != "addr,nodename,intf,id" || page.query.Get("limit") != "50" {
		t.Fatalf("ips request = %+v", page)
	}
	if resources.path != "/resources" || strings.Join(resources.query["filter"], "|") != `res_type:~^ip(\.|$)|res_desc:~(^|[[:space:]])2001:db8::21(/|[[:space:]]|$)` || resources.query.Get("limit") != "200" {
		t.Fatalf("resources request = %+v", resources)
	}
	address := ips.NodeAddresses[0]
	if ips.IP != "2001:db8::21" || ips.SpecialExcluded || ips.Total != 1 || ips.Truncated || address.Nodename != "dev2n1" || address.Interface != "br-prd" ||
		address.PrefixLength != "64" || address.NetworkID != 0 || address.LastReportedAt != "2026-10-15 17:44:40" {
		t.Fatalf("ips = %+v", ips)
	}
	up := ips.ServiceAddresses[0]
	if len(ips.ServiceAddresses) != 2 || !ips.ServiceAddressesTruncated || up.Svcname != "testnfs" || up.Nodename != "dev2n1" || up.Status != "up" || up.RID != "ip#1" ||
		up.LastReportedAt != "2026-10-15 21:52:01" || ips.Provenance.Source != provenanceSourceOpenSVCCollector {
		t.Fatalf("service addresses = %+v", ips)
	}
}

func TestListIPsOfANodeLeavesOutSpecialAddressesAndPaginates(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/ips":          `{"data":[{"node_id":"n-1","nodename":"dev2n1","addr":"10.29.0.11","mask":"24","net_id":7,"net_name":"prd"}],"meta":{"total":3}}`,
	}}
	ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{Node: "dev2n1", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(collector.requests) != 2 || strings.Join(collector.requests[1].query["filter"], "|") != specialAddressesFilter+"|node_id:eq:n-1" {
		t.Fatalf("requests = %+v", collector.requests)
	}
	if !ips.SpecialExcluded || ips.Node.Nodename != "dev2n1" || !ips.Truncated || ips.NextCursor == "" || ips.ServiceAddresses != nil ||
		ips.NodeAddresses[0].NetworkID != 7 || ips.NodeAddresses[0].NetworkName != "prd" {
		t.Fatalf("ips = %+v", ips)
	}
	next, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{Node: "dev2n1", Limit: 1, Cursor: ips.NextCursor})
	if err != nil || collector.requests[len(collector.requests)-1].query.Get("offset") != "1" || next.Count != 1 {
		t.Fatalf("next = %+v, %v", next, err)
	}
	if _, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{Node: "dev2n1", IncludeSpecial: true, Cursor: ips.NextCursor}); err == nil {
		t.Fatal("a cursor was accepted with other filters")
	}
}

func TestListIPsByAddressSkipsServiceAddressesAfterTheFirstPage(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{"/ips": `{"data":[{"addr":"10.0.3.1","nodename":"dev2n2"}],"meta":{"total":3}}`}}
	cursor := encodeListCursor(1, listFingerprint(url.Values{"filter": {"addr:eq:10.0.3.1"}}))
	ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{IP: "10.0.3.1", Limit: 1, Cursor: cursor})
	if err != nil || len(collector.requests) != 1 || ips.ServiceAddresses != nil || !ips.Truncated {
		t.Fatalf("ips = %+v, %v, %d requests", ips, err, len(collector.requests))
	}
}

func TestListIPsOfADeclaredNetwork(t *testing.T) {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		switch path {
		case "/networks":
			if query.Get("filter") == "id:eq:2191" || query.Get("filter") == "name:eq:Test network" {
				return `{"data":[{"id":2191,"name":"Test network","network":"192.168.122.0","netmask":24}]}`
			}
			if query.Get("filter") == "name:eq:dup" {
				return `{"data":[{"id":1,"name":"dup","network":"10.0.0.0","netmask":8},{"id":2,"name":"dup","network":"10.1.0.0","netmask":16}]}`
			}
			return `{"data":[]}`
		case "/ips":
			return `{"data":[{"addr":"192.168.122.1","nodename":"dev2n1","net_id":2191,"net_name":"Test network"}],"meta":{"total":1}}`
		}
		return `{}`
	}
	for _, network := range []string{"2191", "Test network"} {
		ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{Network: network})
		last := collector.requests[len(collector.requests)-1]
		if err != nil || ips.Network.ID != 2191 || ips.Network.Netmask != 24 || strings.Join(last.query["filter"], "|") != specialAddressesFilter+"|net_id:eq:2191" {
			t.Fatalf("network %q: ips = %+v, %v, %+v", network, ips, err, last)
		}
	}
	if _, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{Network: "missing"}); err == nil || !strings.Contains(err.Error(), "no declared network") {
		t.Fatalf("missing network: %v", err)
	}
	if _, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{Network: "dup"}); err == nil || !strings.Contains(err.Error(), "1 (10.0.0.0/8), 2 (10.1.0.0/16)") {
		t.Fatalf("ambiguous network: %v", err)
	}
}

func TestListIPsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListIPsOptions{
		{},
		{IncludeSpecial: true},
		{IP: "10.0.3.0/24"},
		{IP: "10.0.3"},
		{IP: "fe80::1%eth0"},
		{IP: "host"},
		{Node: "a/b"},
		{Network: " padded"},
		{IP: "10.0.3.1", Limit: 201},
		{IP: "10.0.3.1", Cursor: "bogus"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListIPs(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

package core

import (
	"net/url"
	"strings"
	"testing"
)

const serviceAddressFilters = `res_type:in:ip.host,ip.netns|res_desc:~(^|[[:space:]])2001:db8::21/`

func TestListIPsByAddressReadsNodeThenServiceAddresses(t *testing.T) {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		switch {
		case path == "/ips" && query.Get("offset") == "0":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","intf":"br-prd","mac":"52:54:00:00:00:01","type":"ipv6","addr":"2001:db8::21","mask":"64",
				"updated":"2026-10-15 17:44:40","flag_deprecated":0,"net_id":null,"net_name":""}],"meta":{"total":1}}`
		case path == "/ips":
			return `{"data":[],"meta":{"total":1}}`
		case path == "/resources" && query.Get("offset") == "0":
			return `{"data":[
				{"svc_id":"s-1","services.svcname":"testnfs","node_id":"n-1","nodes.nodename":"dev2n1","rid":"ip#1","res_type":"ip.host","res_status":"up","res_desc":"host 2001:db8::21/64 br-prd","updated":"2026-10-15 21:52:01"}],"meta":{"total":2}}`
		case path == "/resources":
			return `{"data":[
				{"svc_id":"s-2","services.svcname":"web","node_id":"n-1","nodes.nodename":"dev2n1","vmname":"web1","rid":"ip#0","res_type":"ip.netns","res_status":"up","res_desc":"netns 2001:db8::21/64 in container#0"}],"meta":{"total":2}}`
		}
		return `{}`
	}
	ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{IP: "2001:0DB8:0:0::0021", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(collector.requests) != 2 {
		t.Fatalf("requests = %+v", collector.requests)
	}
	page, resources := collector.requests[0], collector.requests[1]
	if page.path != "/ips" || strings.Join(page.query["filter"], "|") != "addr:eq:2001:db8::21" || page.query.Get("orderby") != "addr,nodename,intf,id" || page.query.Get("limit") != "2" {
		t.Fatalf("ips request = %+v", page)
	}
	// The service addresses fill the room the node addresses left.
	if resources.path != "/resources" || strings.Join(resources.query["filter"], "|") != serviceAddressFilters || resources.query.Get("limit") != "1" ||
		resources.query.Get("orderby") != "services.svcname,nodes.nodename,vmname,rid,id" || !strings.Contains(resources.query.Get("props"), "vmname") {
		t.Fatalf("resources request = %+v", resources)
	}
	address := ips.NodeAddresses[0]
	if ips.IP != "2001:db8::21" || ips.SpecialExcluded || ips.Total != 1 || ips.Count != 2 || address.Nodename != "dev2n1" || address.Interface != "br-prd" ||
		address.PrefixLength != "64" || address.NetworkID != 0 || address.LastReportedAt != "2026-10-15 17:44:40" {
		t.Fatalf("ips = %+v", ips)
	}
	up := ips.ServiceAddresses[0]
	if len(ips.ServiceAddresses) != 1 || *ips.ServiceTotal != 2 || !ips.Truncated || ips.NextCursor == "" || up.Svcname != "testnfs" || up.Status != "up" ||
		up.RID != "ip#1" || up.LastReportedAt != "2026-10-15 21:52:01" || ips.Provenance.Source != provenanceSourceOpenSVCCollector {
		t.Fatalf("service addresses = %+v", ips)
	}

	next, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{IP: "2001:db8::21", Limit: 2, Cursor: ips.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	nodes, services := collector.requests[2], collector.requests[3]
	if nodes.query.Get("offset") != "1" || services.query.Get("offset") != "1" || services.query.Get("limit") != "2" {
		t.Fatalf("next requests = %+v, %+v", nodes, services)
	}
	if next.Count != 1 || len(next.NodeAddresses) != 0 || next.ServiceAddresses[0].VMName != "web1" || next.ServiceAddresses[0].Type != "ip.netns" || next.Truncated || next.NextCursor != "" {
		t.Fatalf("next = %+v", next)
	}
}

func TestListIPsByAddressCountsServiceAddressesWhenNodesFillThePage(t *testing.T) {
	for _, total := range []string{"0", "3"} {
		collector := &fakeCollector{bodies: map[string]string{
			"/ips":       `{"data":[{"addr":"10.0.3.1","nodename":"dev2n1"}],"meta":{"total":1}}`,
			"/resources": `{"data":[{"id":1}],"meta":{"total":` + total + `}}`,
		}}
		ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{IP: "10.0.3.1", Limit: 1})
		if err != nil || len(collector.requests) != 2 || collector.requests[1].query.Get("props") != "id" || collector.requests[1].query.Get("limit") != "1" {
			t.Fatalf("total %s: ips = %+v, %v, %+v", total, ips, err, collector.requests)
		}
		if ips.ServiceAddresses != nil || ips.Count != 1 || ips.Truncated != (total != "0") || (ips.NextCursor != "") != (total != "0") {
			t.Fatalf("total %s: ips = %+v", total, ips)
		}
	}
	// While node addresses remain, the service addresses are not read.
	collector := &fakeCollector{bodies: map[string]string{"/ips": `{"data":[{"addr":"10.0.3.1","nodename":"dev2n1"}],"meta":{"total":3}}`}}
	ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{IP: "10.0.3.1", Limit: 1})
	if err != nil || len(collector.requests) != 1 || ips.ServiceTotal != nil || !ips.Truncated {
		t.Fatalf("ips = %+v, %v, %d requests", ips, err, len(collector.requests))
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
	if !ips.SpecialExcluded || ips.Node.Nodename != "dev2n1" || !ips.Truncated || ips.NextCursor == "" || ips.ServiceAddresses != nil || ips.ServiceTotal != nil ||
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
		case "/resources":
			return `{"data":[],"meta":{"total":0}}`
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
	// The network applies to the service addresses too: an address outside
	// it has none, one inside has those naming it.
	for ip, read := range map[string]bool{"10.29.0.21": false, "192.168.122.1": true} {
		collector.requests = nil
		ips, err := newTestService(collector).ListIPs(t.Context(), ListIPsOptions{IP: ip, Network: "Test network"})
		if err != nil || (collector.requests[len(collector.requests)-1].path == "/resources") != read || (ips.ServiceTotal != nil) != read {
			t.Fatalf("ip %s in the network: ips = %+v, %v, %+v", ip, ips, err, collector.requests)
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
		{IP: "10.0.3.1", Cursor: encodeIPCursor(ipCursor{})},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListIPs(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

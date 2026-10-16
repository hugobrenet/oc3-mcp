package core

import (
	"net/url"
	"strings"
	"testing"
)

func TestListFiltersetsFiltersTheName(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"id":283,"fset_name":"big-memory","fset_author":" root ","fset_updated":"2026-09-15 15:51:56"}],"meta":{"total":3}}`}
	list, err := newTestService(collector).ListFiltersets(t.Context(), ListFiltersetsOptions{Name: "mem", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[0].path != "/filtersets" || collector.requests[0].query.Get("filter") != "fset_name:~mem" || collector.requests[0].query.Get("orderby") != "fset_name,id" {
		t.Fatalf("request = %+v", collector.requests[0])
	}
	if list.Total != 3 || !list.Truncated || list.Filtersets[0].Author != "root" || list.Filtersets[0].Name != "big-memory" {
		t.Fatalf("list = %+v", list)
	}
}

func TestGetFiltersetReadsItsCriteriaMembersAndUsage(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/filtersets/demo":
			return `{"data":[{"id":295,"fset_name":"demo"}]}`
		case "/filtersets/295/export":
			return `{"filtersets":[
				{"id":295,"fset_name":"demo","filters":[
					{"f_log_op":"AND NOT","f_order":2,"filter":null,"filterset":"big-memory"},
					{"f_log_op":"AND","f_order":1,"filter":{"f_table":"nodes","f_field":"loc_city","f_op":"=","f_value":"Paris"},"filterset":null}]},
				{"id":283,"fset_name":"big-memory","filters":[{"f_log_op":"AND","f_order":1,"filter":{"f_table":"nodes","f_field":"mem_bytes","f_op":">=","f_value":"131072"},"filterset":null}]}]}`
		case "/filtersets/295/nodes":
			return `{"data":[{"nodename":"par1"},{"nodename":"par2"}],"meta":{"total":23}}`
		case "/filtersets/295/services":
			return `{"data":[],"meta":{"total":0}}`
		case "/filtersets/295/usage":
			return `{"data":{"comparisons":[],"filtersets":[{"id":300,"fset_name":"all-paris","f_log_op":"OR","entries":2}],"rulesets":[{"id":1030,"ruleset_name":"ctx.paris"}],
				"sysreport_grants":[{"id":1}],"thresholds":[{"id":1},{"id":2}],"users":[{"id":161,"email":"jane@example.com","name":"Jane Doe"}]}}`
		}
		return `{"data":[]}`
	}}
	filterset, err := newTestService(collector).GetFilterset(t.Context(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	// The criteria follow their order, the included filterset is given with
	// its own, and the users are named without their email address.
	if len(filterset.Criteria) != 2 || filterset.Criteria[0].Filter != "nodes.loc_city = Paris" || filterset.Criteria[1].Logic != "AND NOT" ||
		filterset.Criteria[1].Filterset != "big-memory" || len(filterset.Included) != 1 || filterset.Included[0].Criteria[0].Filter != "nodes.mem_bytes >= 131072" ||
		filterset.NodesCount != 23 || filterset.Nodes[1] != "par2" || filterset.Rulesets[0] != "ctx.paris" || filterset.IncludedBy[0] != "all-paris" ||
		filterset.Thresholds != 2 || filterset.SysreportGrants != 1 || filterset.SessionFilterOf != 1 {
		t.Fatalf("filterset = %+v", filterset)
	}
	collector = &fakeCollector{err: fakeNotFound{}}
	if _, err := newTestService(collector).GetFilterset(t.Context(), "nosuch"); err == nil || !strings.Contains(err.Error(), "list_filtersets") {
		t.Fatalf("unknown filterset: %v", err)
	}
}

func TestGetFiltersetWithoutCriterionSelectsNothing(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, _ url.Values) string {
		switch path {
		case "/filtersets/empty":
			return `{"data":[{"id":7,"fset_name":"empty"}]}`
		case "/filtersets/7/export":
			return `{"filtersets":[{"id":7,"fset_name":"empty","filters":[]}]}`
		case "/filtersets/7/usage":
			return `{"data":{}}`
		}
		// On dev2, the collector answers the members of an empty filterset
		// with a 500: they are not read.
		return `{"data":[]}`
	}}
	filterset, err := newTestService(collector).GetFilterset(t.Context(), "empty")
	if err != nil || filterset.NodesCount != 0 || filterset.Criteria == nil || len(collector.requests) != 3 {
		t.Fatalf("filterset = %+v, %v, %d requests", filterset, err, len(collector.requests))
	}
	for _, name := range []string{"", "a/b", " demo"} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).GetFilterset(t.Context(), name); err == nil || len(collector.requests) != 0 {
			t.Fatalf("filterset %q accepted", name)
		}
	}
}

func TestFiltersetFilterReadsTheNodesAndServicesItSelects(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/filtersets/prd-linux":
			return `{"data":[{"id":282,"fset_name":"prd-linux"}]}`
		case "/filtersets/282/nodes":
			if query.Get("stats") == "true" {
				return `{"data":{"os_release":{"9.4":64,"12.7":32}},"meta":{"total":96,"distinct":{"os_release":2}}}`
			}
			return `{"data":[{"node_id":"n-1","nodename":"ams-prd-app01"}],"meta":{"total":187}}`
		case "/filtersets/282/services":
			if query.Get("stats") == "true" {
				return `{"data":{"svc_status":{"up":5}},"meta":{"total":5,"distinct":{"svc_status":1}}}`
			}
			return `{"data":[{"svc_id":"s-1","svcname":"web"}],"meta":{"total":5}}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
	service := newTestService(collector)
	nodes, err := service.ListNodes(t.Context(), ListNodesOptions{NodeFilters: NodeFilters{Filterset: "prd-linux", Country: "France"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[1].path != "/filtersets/282/nodes" || collector.requests[1].query.Get("filter") != "loc_country:~France" ||
		nodes.Filterset == nil || nodes.Filterset.Name != "prd-linux" || nodes.Tag != nil || nodes.Total != 187 || !nodes.Truncated {
		t.Fatalf("nodes = %+v, requests %+v", nodes, collector.requests)
	}
	count, err := service.CountNodes(t.Context(), CountNodesOptions{NodeFilters: NodeFilters{Filterset: "prd-linux"}, GroupBy: "os_release"})
	if err != nil || count.Filterset == nil || count.Total != 96 || len(count.Groups) != 2 {
		t.Fatalf("count = %+v, %v", count, err)
	}
	services, err := service.ListServices(t.Context(), ListServicesOptions{ServiceFilters: ServiceFilters{Filterset: "prd-linux"}})
	if err != nil || services.Filterset == nil || services.Total != 5 || services.Services[0].Svcname != "web" {
		t.Fatalf("services = %+v, %v", services, err)
	}
	serviceCount, err := service.CountServices(t.Context(), CountServicesOptions{ServiceFilters: ServiceFilters{Filterset: "prd-linux"}, GroupBy: "status"})
	if err != nil || serviceCount.Filterset == nil || serviceCount.Groups[0].Value != "up" {
		t.Fatalf("service count = %+v, %v", serviceCount, err)
	}
}

func TestFiltersetFilterRefusesInvalidCombinations(t *testing.T) {
	for _, options := range []ListServicesOptions{
		{ServiceFilters: ServiceFilters{Tag: "pci-dss", Filterset: "prd-linux"}},
		{Node: "dev2n1", ServiceFilters: ServiceFilters{Filterset: "prd-linux"}},
		{ServiceFilters: ServiceFilters{Filterset: "a/b"}},
		{ServiceFilters: ServiceFilters{Filterset: " prd-linux"}},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListServices(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{err: fakeNotFound{}}
	if _, err := newTestService(collector).CountNodes(t.Context(), CountNodesOptions{NodeFilters: NodeFilters{Filterset: "nosuch"}}); err == nil || !strings.Contains(err.Error(), "list_filtersets") {
		t.Fatalf("unknown filterset: %v", err)
	}
}

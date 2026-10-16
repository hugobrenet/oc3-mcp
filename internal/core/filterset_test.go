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

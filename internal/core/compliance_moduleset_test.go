package core

import (
	"net/url"
	"strings"
	"testing"
)

const modulesetModules = `{"data":[
	{"id":11,"modset_id":3,"modset_mod_name":"linux.sysctl","autofix":"F"},
	{"id":10,"modset_id":3,"modset_mod_name":"linux.files","autofix":"T"},
	{"id":0,"modset_id":4,"modset_mod_name":"","autofix":""}],"meta":{"total":3}}`

func TestListComplianceModulesetsWithTheirModules(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, _ url.Values) string {
		if path == "/compliance/modulesets_modules" {
			return modulesetModules
		}
		return `{"data":[{"id":3,"modset_name":"base.linux","modset_author":" Admin ","modset_updated":"2026-09-01 10:00:00"},
			{"id":4,"modset_name":"draft.monitoring"}],"meta":{"total":6}}`
	}}
	list, err := newTestService(collector).ListComplianceModulesets(t.Context(), ListComplianceModulesetsOptions{Name: "a", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[0].path != "/compliance/modulesets" || collector.requests[0].query.Get("filter") != "modset_name:~a" ||
		collector.requests[0].query.Get("orderby") != "modset_name,id" || collector.requests[1].query.Get("filter") != "modset_id:in:3,4" {
		t.Fatalf("requests = %+v", collector.requests)
	}
	base, draft := list.Modulesets[0], list.Modulesets[1]
	if list.Total != 6 || !list.Truncated || base.Author != "Admin" || base.ModulesCount != 2 || base.Modules[0].Name != "linux.sysctl" ||
		base.Modules[1].Name != "linux.files" || !base.Modules[1].Autofix || base.Modules[0].Autofix || draft.ModulesCount != 0 || draft.Modules == nil {
		t.Fatalf("list = %+v", list)
	}
}

func TestListComplianceModulesetsOfANodeAndOfAModule(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/dev2n1":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		case "/nodes/n-1/compliance/modulesets":
			return `{"data":[{"id":3},{"id":5}]}`
		case "/compliance/modulesets_modules":
			if query.Get("props") == "modset_id" {
				return `{"data":[{"modset_id":3},{"modset_id":3},{"modset_id":7}],"meta":{"total":3}}`
			}
			return modulesetModules
		}
		return `{"data":[{"id":3,"modset_name":"base.linux"}],"meta":{"total":1}}`
	}}
	list, err := newTestService(collector).ListComplianceModulesets(t.Context(), ListComplianceModulesetsOptions{Node: "dev2n1", Module: "linux."})
	if err != nil {
		t.Fatal(err)
	}
	// The node attaches 3 and 5, the module is in 3 and 7: only 3 is listed.
	if collector.requests[2].query.Get("filter") != `modset_mod_name:~^linux\.` || collector.requests[3].query.Get("filter") != "id:in:3" ||
		list.Node == nil || list.Node.Nodename != "dev2n1" || list.Total != 1 {
		t.Fatalf("list = %+v, requests %+v", list, collector.requests)
	}
	// Nothing selected is answered without listing the modulesets.
	collector = &fakeCollector{answer: func(path string, _ url.Values) string {
		if path == "/nodes/dev2n1" {
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		}
		return `{"data":[]}`
	}}
	list, err = newTestService(collector).ListComplianceModulesets(t.Context(), ListComplianceModulesetsOptions{Node: "dev2n1"})
	if err != nil || list.Total != 0 || list.Modulesets == nil || len(collector.requests) != 2 {
		t.Fatalf("empty = %+v, %v, %d requests", list, err, len(collector.requests))
	}
	for _, options := range []ListComplianceModulesetsOptions{{Node: "a", Service: "b"}, {Name: " a"}, {Limit: 201}, {Cursor: "garbage"}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListComplianceModulesets(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestGetComplianceModulesetReadsItsTree(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/compliance/modulesets/sec.hardening":
			return `{"data":[{"id":9,"modset_name":"sec.hardening","modset_author":"admin"}]}`
		case "/compliance/modulesets/9/export":
			return `{"filtersets":[],"modulesets":[
				{"id":9,"modset_name":"sec.hardening","modules":[{"modset_mod_name":"sec.ssh","autofix":"T"}],"modulesets":["base.linux"],"rulesets":["os.linux.security"],"publications":["Everybody"],"responsibles":["Infra Unix"]},
				{"id":3,"modset_name":"base.linux","modules":[{"modset_mod_name":"linux.sysctl","autofix":"F"},{"modset_mod_name":"linux.files","autofix":"T"}],"modulesets":["sec.hardening"],"rulesets":[]}],
				"rulesets":[{"ruleset_name":"os.linux.security","ruleset_type":"explicit","fset_name":null,"variables":[{},{},{}],"rulesets":["ctx.dev2"]},
					{"ruleset_name":"ctx.dev2","ruleset_type":"contextual","fset_name":"dev2-nodes","variables":[{}],"rulesets":[]}]}`
		case "/compliance/modulesets/9/usage":
			return `{"data":{"modulesets":[{"id":1,"modset_name":"all.prod"}]}}`
		case "/compliance/modulesets/9/nodes":
			return `{"data":[{"nodename":"dev2n1"},{"nodename":"dev2n2"}],"meta":{"total":60}}`
		case "/compliance/modulesets/9/services":
			if query.Get("slave") == "true" {
				return `{"data":[{"svcname":"db"}],"meta":{"total":1}}`
			}
			return `{"data":[],"meta":{"total":0}}`
		}
		return `{"data":[]}`
	}}
	moduleset, err := newTestService(collector).GetComplianceModuleset(t.Context(), "sec.hardening")
	if err != nil {
		t.Fatal(err)
	}
	// A loop of inclusions is followed once.
	if moduleset.Moduleset.ID != 9 || moduleset.Moduleset.ModulesCount != 1 || !moduleset.Moduleset.Modules[0].Autofix ||
		len(moduleset.Included) != 1 || moduleset.Included[0].Name != "base.linux" || moduleset.Included[0].Modules[0].Name != "linux.files" ||
		len(moduleset.Rulesets) != 2 || moduleset.Rulesets[0].Name != "ctx.dev2" || moduleset.Rulesets[0].Filterset != "dev2-nodes" || moduleset.Rulesets[1].Variables != 3 ||
		moduleset.Publications[0] != "Everybody" || moduleset.Responsibles[0] != "Infra Unix" || moduleset.Parents[0] != "all.prod" ||
		moduleset.NodesCount != 60 || len(moduleset.Nodes) != 2 || moduleset.ServicesCount != 0 || moduleset.EncapsulatedCount != 1 || moduleset.Encapsulated[0] != "db" ||
		strings.Join(moduleset.Rulesets[1].AttachedTo, ",") != "sec.hardening" || moduleset.Rulesets[1].IncludedBy != nil ||
		moduleset.Rulesets[0].AttachedTo != nil || strings.Join(moduleset.Rulesets[0].IncludedBy, ",") != "os.linux.security" {
		t.Fatalf("moduleset = %+v", moduleset)
	}
	if query := collector.requests[3].query; query.Get("props") != "nodename" || query.Get("orderby") != "nodename,node_id" || query.Get("limit") != "50" {
		t.Fatalf("nodes query = %v", query)
	}
	collector = &fakeCollector{err: fakeNotFound{}}
	if _, err := newTestService(collector).GetComplianceModuleset(t.Context(), "nosuch"); err == nil || !strings.Contains(err.Error(), "list_compliance_modulesets") {
		t.Fatalf("unknown moduleset: %v", err)
	}
	for _, moduleset := range []string{"", "a/b", " base.linux"} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).GetComplianceModuleset(t.Context(), moduleset); err == nil || len(collector.requests) != 0 {
			t.Fatalf("moduleset %q accepted", moduleset)
		}
	}
}

func TestListComplianceModulesetsReadsTheModulesInBatches(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/compliance/modulesets_modules" {
			if query.Get("offset") == "0" {
				return `{"data":[{"id":10,"modset_id":3,"modset_mod_name":"linux.files","autofix":"T"}],"meta":{"total":2}}`
			}
			return `{"data":[{"id":11,"modset_id":4,"modset_mod_name":"sec.ssh","autofix":"F"}],"meta":{"total":2}}`
		}
		return `{"data":[{"id":3,"modset_name":"base.linux"},{"id":4,"modset_name":"sec.hardening"}],"meta":{"total":2}}`
	}}
	list, err := newTestService(collector).ListComplianceModulesets(t.Context(), ListComplianceModulesetsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The second batch starts after the first one, and carries the modules
	// of the last moduleset.
	if len(collector.requests) != 3 || collector.requests[2].query.Get("offset") != "1" || list.Modulesets[1].ModulesCount != 1 || list.Modulesets[1].Modules[0].Name != "sec.ssh" {
		t.Fatalf("list = %+v, requests %+v", list, collector.requests)
	}
}

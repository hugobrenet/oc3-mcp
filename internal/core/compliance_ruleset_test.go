package core

import (
	"net/url"
	"strings"
	"testing"
)

func TestListComplianceRulesetsFiltersTheType(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"id":1,"ruleset_name":"ctx.dev2","ruleset_type":"contextual","ruleset_public":"T"},
		{"id":2,"ruleset_name":"os.linux.base","ruleset_type":"explicit","ruleset_public":"F"}],"meta":{"total":7}}`}
	list, err := newTestService(collector).ListComplianceRulesets(t.Context(), ListComplianceRulesetsOptions{Name: "o", Type: "explicit", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[0].path != "/compliance/rulesets" || strings.Join(collector.requests[0].query["filter"], " ") != "ruleset_name:~o ruleset_type:eq:explicit" ||
		collector.requests[0].query.Get("orderby") != "ruleset_name,id" {
		t.Fatalf("request = %+v", collector.requests[0])
	}
	if list.Total != 7 || !list.Truncated || !list.Rulesets[0].Public || list.Rulesets[1].Public || list.Rulesets[0].AppliesBy != "" {
		t.Fatalf("list = %+v", list)
	}
}

func TestListComplianceRulesetsOfANode(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/dev2n1":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		case "/nodes/n-1/compliance/rulesets":
			return `{"data":[{"id":2}]}`
		case "/compliance/rulesets":
			if query.Get("props") == "id" {
				return `{"data":[{"id":1},{"id":5}],"meta":{"total":2}}`
			}
			return `{"data":[{"id":1,"ruleset_name":"ctx.dev2","ruleset_type":"contextual","ruleset_public":"T"},
				{"id":2,"ruleset_name":"os.linux.base","ruleset_type":"explicit","ruleset_public":"T"}],"meta":{"total":2}}`
		case "/compliance/rulesets_variables":
			if query.Get("groupby") != "ruleset_id,fset_id" {
				return `{"data":[],"meta":{"total":99}}`
			}
			return `{"data":[{"ruleset_id":1,"fset_id":40},{"ruleset_id":5,"fset_id":41}],"meta":{"total":2}}`
		case "/filtersets/40/nodes":
			return `{"data":[{"id":1}],"meta":{"total":1}}`
		case "/filtersets/41/nodes":
			return `{"data":[],"meta":{"total":0}}`
		}
		return `{"data":[]}`
	}}
	list, err := newTestService(collector).ListComplianceRulesets(t.Context(), ListComplianceRulesetsOptions{Node: "dev2n1"})
	if err != nil {
		t.Fatal(err)
	}
	var matched, final fakeRequest
	for _, request := range collector.requests {
		if request.path == "/filtersets/40/nodes" {
			matched = request
		}
		if request.path == "/compliance/rulesets" && request.query.Get("props") != "id" {
			final = request
		}
	}
	// The contextual ruleset 1 selects the node through its filterset 40,
	// the ruleset 5 does not; 2 is attached.
	if matched.query.Get("filter") != "node_id:eq:n-1" || matched.query.Get("props") != "node_id" || final.query.Get("filter") != "id:in:1,2" ||
		list.Rulesets[0].AppliesBy != "filterset" || list.Rulesets[1].AppliesBy != "attached" || list.Node.Nodename != "dev2n1" {
		t.Fatalf("list = %+v, final request %+v", list, final)
	}
	for _, options := range []ListComplianceRulesetsOptions{{Type: "implicit"}, {Node: "a", Service: "b"}, {Name: " a"}, {Limit: 201}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListComplianceRulesets(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestListComplianceRulesetsOfAServiceWithoutAny(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/services/foo":
			return `{"data":[{"svc_id":"s-1","svcname":"foo","cluster_id":"c1"}]}`
		case "/compliance/rulesets":
			return `{"data":[],"meta":{"total":0}}`
		}
		return `{"data":[]}`
	}}
	list, err := newTestService(collector).ListComplianceRulesets(t.Context(), ListComplianceRulesetsOptions{Service: "foo"})
	if err != nil || list.Total != 0 || list.Rulesets == nil || list.Service.Svcname != "foo" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if collector.requests[1].path != "/services/s-1/compliance/rulesets" || len(collector.requests) != 3 {
		t.Fatalf("requests = %+v", collector.requests)
	}
}

func TestGetComplianceRulesetReadsItsVariablesAndUsage(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, _ url.Values) string {
		switch path {
		case "/compliance/rulesets/app.web.frontend":
			return `{"data":[{"id":20,"ruleset_name":"app.web.frontend","ruleset_type":"contextual","ruleset_public":"T"}]}`
		case "/compliance/rulesets/20/export":
			return `{"filtersets":[{"id":40,"fset_name":"web-nodes"}],"rulesets":[
				{"id":20,"ruleset_name":"app.web.frontend","ruleset_type":"contextual","fset_name":"web-nodes","rulesets":["os.linux.security"],
				 "publications":["Everybody"],"responsibles":["Test Web"],"variables":[
					{"var_name":"nginx_conf","var_class":"file","var_value":"{\"path\":\"/etc/nginx/nginx.conf\",\"mode\":\"644\",\"password\":\"s3cret\"}"},
					{"var_name":"db_password","var_class":"raw","var_value":"s3cret"},
					{"var_name":"admin","var_class":"raw","var_value":"contact admin@example.com"}]},
				{"id":21,"ruleset_name":"os.linux.security","ruleset_type":"explicit","fset_name":null,"rulesets":["os.linux.base"],"variables":[{"var_name":"sshd","var_class":"file","var_value":"{}"}]},
				{"id":22,"ruleset_name":"os.linux.base","ruleset_type":"explicit","fset_name":null,"rulesets":["app.web.frontend"],"variables":[]}]}`
		case "/filtersets/40/nodes":
			return `{"data":[{"nodename":"web1"}],"meta":{"total":12}}`
		case "/filtersets/40/services":
			return `{"data":[],"meta":{"total":0}}`
		case "/compliance/rulesets/20/usage":
			return `{"data":{"modulesets":[{"id":382,"modset_name":"app.web"}],"rulesets":[],"nodes":[{"node_id":"n-3","nodename":"hidden"}],"services":[]}}`
		case "/compliance/rulesets/20/nodes":
			return `{"data":[{"nodename":"web1"},{"nodename":"web2"}],"meta":{"total":2}}`
		case "/compliance/rulesets/20/services":
			return `{"data":[],"meta":{"total":0}}`
		}
		return `{"data":[]}`
	}}
	ruleset, err := newTestService(collector).GetComplianceRuleset(t.Context(), "app.web.frontend")
	if err != nil {
		t.Fatal(err)
	}
	variables := map[string]string{}
	for _, variable := range ruleset.Variables {
		variables[variable.Name] = variable.Value
	}
	// A loop of inclusions is followed once.
	if ruleset.Ruleset.Type != "contextual" || ruleset.Filterset == nil || ruleset.Filterset.Name != "web-nodes" || ruleset.Filterset.NodesCount != 12 || ruleset.Filterset.Nodes[0] != "web1" ||
		ruleset.VariablesCount != 3 || variables["db_password"] != maskedRequestValue || strings.Contains(variables["nginx_conf"], "s3cret") ||
		!strings.Contains(variables["nginx_conf"], "/etc/nginx/nginx.conf") || strings.Contains(variables["admin"], "@") ||
		len(ruleset.Included) != 2 || ruleset.Included[0].Name != "os.linux.security" || ruleset.Included[1].Name != "os.linux.base" ||
		ruleset.Modulesets[0] != "app.web" || ruleset.NodesCount != 2 || ruleset.Nodes[0] != "web1" || ruleset.Responsibles[0] != "Test Web" {
		t.Fatalf("ruleset = %+v", ruleset)
	}
	for _, request := range collector.requests {
		if strings.HasPrefix(request.path, "/filtersets/40/") && request.query.Get("props") != map[string]string{"/filtersets/40/nodes": "nodename", "/filtersets/40/services": "svcname"}[request.path] {
			t.Fatalf("filterset count request = %+v", request)
		}
	}
	collector = &fakeCollector{err: fakeNotFound{}}
	if _, err := newTestService(collector).GetComplianceRuleset(t.Context(), "nosuch"); err == nil || !strings.Contains(err.Error(), "list_compliance_rulesets") {
		t.Fatalf("unknown ruleset: %v", err)
	}
	for _, ruleset := range []string{"", "a/b", " os.linux.base"} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).GetComplianceRuleset(t.Context(), ruleset); err == nil || len(collector.requests) != 0 {
			t.Fatalf("ruleset %q accepted", ruleset)
		}
	}
}

func TestMaskVariableValue(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{"api_token", "abc", maskedRequestValue},
		{"motd", `{"path":"/etc/motd","owner":"root"}`, `{"path":"/etc/motd","owner":"root"}`},
		{"user", `{"login":"bob","password":"x"}`, `{"login":"bob","password":"(masked)"}`},
		{"sysctl", "vm.swappiness = 10", "vm.swappiness = 10"},
		{"raw", "DB_PASSWORD = x\nport = 5432", "DB_PASSWORD = (masked)\nport = 5432"},
		{"contact", "write to admin@example.com", "write to (masked)"},
	} {
		if got := maskVariableValue(test.name, test.value); got != test.want {
			t.Errorf("maskVariableValue(%q, %q) = %q, want %q", test.name, test.value, got, test.want)
		}
	}
}

func TestListComplianceRulesetsRefusesAnIncompleteFiltersetMatch(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/dev2n1":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		case "/compliance/rulesets":
			return `{"data":[{"id":1}],"meta":{"total":1}}`
		case "/compliance/rulesets_variables":
			return `{"data":[{"ruleset_id":1,"fset_id":40}],"meta":{"total":2}}`
		}
		return `{"data":[]}`
	}}
	if _, err := newTestService(collector).ListComplianceRulesets(t.Context(), ListComplianceRulesetsOptions{Node: "dev2n1"}); err == nil {
		t.Fatal("an incomplete filterset match accepted")
	}
}

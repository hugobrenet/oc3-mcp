package core

import (
	"strings"
	"testing"
)

func TestListComplianceStatusReadsTheLastRuns(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[
		{"id":1102,"run_module":"sec.ssh","node_id":"` + testNodeUUID + `","nodes.nodename":"dev2n1","svc_id":"","services.svcname":null,
		 "run_action":"check","run_status":1,"run_date":"2026-10-11 10:48:10","run_log":"PermitRootLogin is yes\nERR: PermitRootLogin should be no"},
		{"id":1099,"run_module":"sec.audit","node_id":"` + testNodeUUID + `","nodes.nodename":"dev2n1","svc_id":"` + testSvcUUID + `","services.svcname":"web",
		 "run_action":"fix","run_status":-15,"run_date":"2026-10-07 17:48:10","run_log":"` + strings.Repeat("é", maxComplianceLogBytes) + `"}],
		"meta":{"total":3}}`}
	list, err := newTestService(collector).ListComplianceStatus(t.Context(), ListComplianceStatusOptions{Module: "sec.", Status: "nok", Since: "2026-10-01", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/compliance/status" || request.query.Get("orderby") != "-run_date,-id" || request.query.Get("limit") != "2" ||
		strings.Join(request.query["filter"], "|") != `run_module:~^sec\.|run_status:eq:1|run_date:gte:2026-10-01` ||
		!strings.Contains(request.query.Get("props"), "nodes.nodename") || strings.Contains(request.query.Get("props"), "rset_md5") {
		t.Fatalf("request = %+v", request)
	}
	ssh, audit := list.Statuses[0], list.Statuses[1]
	if list.Total != 3 || list.Count != 2 || !list.Truncated || list.NextCursor == "" ||
		ssh.Status != "nok" || ssh.ExitCode != 1 || ssh.Nodename != "dev2n1" || ssh.Svcname != "" || ssh.Log != "PermitRootLogin is yes\nERR: PermitRootLogin should be no" {
		t.Fatalf("ssh = %+v", ssh)
	}
	if audit.Status != "" || audit.ExitCode != -15 || audit.Action != "fix" || audit.Svcname != "web" || len(audit.Log) > maxComplianceLogBytes || !strings.HasSuffix(audit.Log, "é") {
		t.Fatalf("audit = %+v", audit)
	}
}

func TestListComplianceStatusResolvesTheNodeAndTheService(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":      `{"data":[{"node_id":"` + testNodeUUID + `","nodename":"dev2n1"}]}`,
		"/services/web":      `{"data":[{"svc_id":"` + testSvcUUID + `","svcname":"web"}]}`,
		"/compliance/status": `{"data":[],"meta":{"total":0}}`,
	}}
	list, err := newTestService(collector).ListComplianceStatus(t.Context(), ListComplianceStatusOptions{Node: "dev2n1", Service: "web", Module: "sec.ssh"})
	if err != nil {
		t.Fatal(err)
	}
	if filters := strings.Join(collector.requests[2].query["filter"], "|"); filters != "node_id:eq:"+testNodeUUID+"|svc_id:eq:"+testSvcUUID+"|run_module:eq:sec.ssh" ||
		list.Node == nil || list.Service == nil || list.Statuses == nil || list.Truncated || collector.requests[2].query.Get("limit") != "50" {
		t.Fatalf("list = %+v, requests %+v", list, collector.requests)
	}
}

func TestListComplianceStatusFiltersTheStatus(t *testing.T) {
	for status, want := range map[string]string{
		"ok":  "run_status:eq:0",
		"nok": "run_status:eq:1",
		"na":  "run_status:eq:2",
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListComplianceStatus(t.Context(), ListComplianceStatusOptions{Status: status}); err != nil {
			t.Fatal(err)
		}
		if got := collector.requests[0].query["filter"]; len(got) != 1 || got[0] != want {
			t.Fatalf("status %q filters = %q, want %q", status, got, want)
		}
	}
}

func TestListComplianceStatusRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListComplianceStatusOptions{
		{Status: "failed"},
		{Status: "error"},
		{Since: "yesterday"},
		{Limit: 201},
		{Module: " sec.ssh"},
		{Node: "a/b"},
		{Cursor: "garbage"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListComplianceStatus(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

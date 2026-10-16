package core

import (
	"net/url"
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

func TestListComplianceRunsReadsTheHistory(t *testing.T) {
	collector := &fakeCollector{answer: func(path string, _ url.Values) string {
		if path == "/nodes/dev2n1" {
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		}
		return `{"data":[
			{"id":37,"run_module":"linux.files","node_id":"n-1","nodes.nodename":"dev2n1","svc_id":"","run_action":"fix","run_status":0,"run_date":"2026-09-28 16:28:10","run_log":"fixed /etc/motd"},
			{"id":30,"run_module":"linux.files","node_id":"n-1","nodes.nodename":"dev2n1","svc_id":"","run_action":"check","run_status":1,"run_date":"2026-09-28 16:23:10","run_log":"` + strings.Repeat("x", maxComplianceLogBytes+10) + `"},
			{"id":12,"run_module":"linux.files","node_id":"n-1","nodes.nodename":"dev2n1","svc_id":"","run_action":"check","run_status":-15,"run_date":"2026-09-20 10:00:00","run_log":""}],
			"meta":{"total":7}}`
	}}
	runs, err := newTestService(collector).ListComplianceRuns(t.Context(), ListComplianceRunsOptions{
		Node: "dev2n1", Module: "linux.", Action: "check", Since: "2026-09-01", Until: "2026-09-30", Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[1]
	if request.path != "/compliance/logs" || request.query.Get("orderby") != "-run_date,-id" ||
		strings.Join(request.query["filter"], " ") != `node_id:eq:n-1 run_module:~^linux\. run_action:eq:check run_date:gte:2026-09-01 run_date:lte:2026-09-30 23:59:59` {
		t.Fatalf("request = %+v", request)
	}
	if runs.Node == nil || runs.Node.Nodename != "dev2n1" || runs.Total != 7 || runs.Count != 3 || !runs.Truncated || runs.NextCursor == "" ||
		runs.Runs[0].Action != "fix" || runs.Runs[0].Status != "ok" || runs.Runs[1].Status != "nok" || len(runs.Runs[1].Log) != maxComplianceLogBytes ||
		runs.Runs[2].Status != "" || runs.Runs[2].ExitCode != -15 {
		t.Fatalf("runs = %+v", runs)
	}
	// A cursor of the history is refused by the last runs, with the same
	// filters, and the other way round.
	collector = &fakeCollector{body: `{"data":[{"id":1,"run_module":"sec.ssh","run_action":"check","run_status":0}],"meta":{"total":2}}`}
	history, err := newTestService(collector).ListComplianceRuns(t.Context(), ListComplianceRunsOptions{Module: "sec.ssh", Limit: 1})
	if err != nil || history.NextCursor == "" {
		t.Fatalf("history = %+v, %v", history, err)
	}
	last, err := newTestService(collector).ListComplianceStatus(t.Context(), ListComplianceStatusOptions{Module: "sec.ssh", Limit: 1})
	if err != nil || last.NextCursor == "" {
		t.Fatalf("last runs = %+v, %v", last, err)
	}
	if _, err := newTestService(collector).ListComplianceStatus(t.Context(), ListComplianceStatusOptions{Module: "sec.ssh", Cursor: history.NextCursor}); err == nil {
		t.Fatal("cursor of the history accepted by the last runs")
	}
	if _, err := newTestService(collector).ListComplianceRuns(t.Context(), ListComplianceRunsOptions{Module: "sec.ssh", Cursor: last.NextCursor}); err == nil {
		t.Fatal("cursor of the last runs accepted by the history")
	}
	// An until with a time is kept as is.
	collector = &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListComplianceRuns(t.Context(), ListComplianceRunsOptions{Until: "2026-09-30 12:00"}); err != nil {
		t.Fatal(err)
	}
	if got := collector.requests[0].query.Get("filter"); got != "run_date:lte:2026-09-30 12:00" {
		t.Fatalf("until filter = %q", got)
	}
}

func TestListComplianceRunsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListComplianceRunsOptions{
		{Action: "repair"},
		{Status: "failed"},
		{Since: "last week"},
		{Until: "2026/09/30"},
		{Module: " sec.ssh"},
		{Limit: 201},
		{Cursor: "garbage"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListComplianceRuns(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

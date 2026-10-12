package core

import (
	"fmt"
	"strings"
	"testing"
)

const testSessionID = "4e54c743-29a2-46fa-b05f-9828136f008c"

func TestListServiceActionsQueriesActionRows(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/kvm%2Fsvc%2Fvm1": `{"data":[{"svc_id":"s-1","svcname":"kvm/svc/vm1","cluster_id":"c1"}]}`,
		"/nodes/dev2n2":             `{"data":[{"node_id":"n-2","nodename":"dev2n2","cluster_id":"c1"}]}`,
		"/services_actions": `{"data":[{"id":14590421,"sid":"` + testSessionID + `","svc_id":"s-1","services.svcname":"kvm/svc/vm1","node_id":"n-2",
			"nodes.nodename":"dev2n2","rid":"","action":"start","origin":"daemon/monitor","status":"err","begin":"2026-10-11 21:11:19",
			"end":"2026-10-11 21:11:24","time":5,"command":"kvm/svc/vm1 instance start"}],"meta":{"total":4}}`,
	}}
	actions, err := newTestService(collector).ListServiceActions(t.Context(), ListServiceActionsOptions{
		Service: "kvm/svc/vm1", Node: "dev2n2", Status: "err", Action: "start", Origin: "daemon/monitor", Since: "2026-10-11", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[2]
	wantFilters := "log_type:eq:status|svc_id:eq:s-1|node_id:eq:n-2|status:eq:err|action:eq:start|origin:eq:daemon/monitor|begin:gte:2026-10-11"
	if request.path != "/services_actions" || request.query.Get("orderby") != "-begin,-id" || strings.Join(request.query["filter"], "|") != wantFilters ||
		strings.Contains(request.query.Get("props"), "status_log") {
		t.Fatalf("request = %+v", request)
	}
	if actions.Service.Svcname != "kvm/svc/vm1" || actions.Node.Nodename != "dev2n2" || actions.Total != 4 || !actions.Truncated || actions.NextCursor == "" {
		t.Fatalf("page = %+v", actions)
	}
	got := actions.Actions[0]
	if got.SessionID != testSessionID || got.Svcname != "kvm/svc/vm1" || got.Nodename != "dev2n2" || got.Action != "start" || got.Origin != "daemon/monitor" ||
		got.Status != "err" || got.DurationSeconds != 5 || got.Command != "kvm/svc/vm1 instance start" {
		t.Fatalf("action = %+v", got)
	}
}

func TestListServiceActionsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListServiceActionsOptions{
		{Status: "failed"},
		{Since: "yesterday"},
		{Since: "2026-10-11T21:00:00"},
		{Since: "2026-10-11 21"},
		{Action: " start"},
		{Limit: 201},
		{Cursor: "garbage"},
		{Service: "two words"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListServiceActions(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListServiceActions(t.Context(), ListServiceActionsOptions{Since: "2026-10-11 21:00"}); err != nil {
		t.Fatalf("since with minutes: %v", err)
	}
}

func TestGetServiceActionLogsSplitsActionsAndLogs(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[
		{"id":1,"sid":"` + testSessionID + `","log_type":"status","action":"start","status":"err","nodes.nodename":"dev2n2","services.svcname":"kvm/svc/vm1","time":5},
		{"id":2,"sid":"` + testSessionID + `","log_type":"","status":"ok","begin":"2026-10-11 21:11:19","nodes.nodename":"dev2n2","status_log":"instance: kvm/svc/vm1: >>> do start"},
		{"id":3,"sid":"` + testSessionID + `","log_type":"","status":"err","rid":"container#1","status_log":"container#1: stderr: error: Cannot access storage file"}
	],"meta":{"total":3}}`}
	logs, err := newTestService(collector).GetServiceActionLogs(t.Context(), testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/services_actions" || request.query.Get("filter") != "sid:eq:"+testSessionID || request.query.Get("orderby") != "id" || request.query.Get("limit") != "500" {
		t.Fatalf("request = %+v", request)
	}
	if logs.SessionID != testSessionID || logs.Total != 3 || logs.Truncated || len(logs.Actions) != 1 || len(logs.Logs) != 2 ||
		logs.Actions[0].Status != "err" || logs.Logs[0].Time != "2026-10-11 21:11:19" || logs.Logs[0].Nodename != "dev2n2" ||
		logs.Logs[1].Level != "err" || logs.Logs[1].RID != "container#1" || !strings.Contains(logs.Logs[1].Text, "Cannot access storage file") {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestGetServiceActionLogsBoundsTheText(t *testing.T) {
	rows := []string{fmt.Sprintf(`{"id":1,"log_type":"","status":"ok","status_log":"%s"}`, strings.Repeat("é", maxActionLogEntryBytes))}
	for index := range 40 {
		rows = append(rows, fmt.Sprintf(`{"id":%d,"log_type":"","status":"ok","status_log":"%s"}`, index+2, strings.Repeat("a", maxActionLogEntryBytes)))
	}
	collector := &fakeCollector{body: `{"data":[` + strings.Join(rows, ",") + `],"meta":{"total":41}}`}
	logs, err := newTestService(collector).GetServiceActionLogs(t.Context(), testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, entry := range logs.Logs {
		total += len(entry.Text)
	}
	first := logs.Logs[0]
	if !logs.Truncated || !first.Truncated || len(first.Text) > maxActionLogEntryBytes || !strings.HasSuffix(first.Text, "é") ||
		total > maxActionLogTotalBytes || !logs.Logs[len(logs.Logs)-1].Truncated {
		t.Fatalf("truncated=%v first=%d bytes total=%d", logs.Truncated, len(first.Text), total)
	}
}

func TestGetServiceActionLogsRejectsInvalidSessionsAndEmptyAnswers(t *testing.T) {
	for _, session := range []string{"", "not-a-uuid", testSessionID + "x"} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).GetServiceActionLogs(t.Context(), session); err == nil || len(collector.requests) != 0 {
			t.Fatalf("session %q accepted", session)
		}
	}
	collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).GetServiceActionLogs(t.Context(), testSessionID); err == nil || !strings.Contains(err.Error(), "no action") {
		t.Fatalf("empty session error = %v", err)
	}
	collector = &fakeCollector{body: `{"data":[{"id":1}],"meta":{"total":600}}`}
	if logs, err := newTestService(collector).GetServiceActionLogs(t.Context(), testSessionID); err != nil || !logs.Truncated {
		t.Fatalf("rows left out: %+v, %v", logs, err)
	}
}

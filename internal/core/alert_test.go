package core

import (
	"strings"
	"testing"
)

func TestListAlertsQueriesTheDashboard(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[
		{"id":84136172,"dash_type":"flex error","dash_severity":5,"alert":"0 instances started. thresholds: 1 - 3","node_id":"","nodes.nodename":null,
		 "svc_id":"s1","services.svcname":"testflex","dash_env":"PRD","dash_instance":"","dash_created":"2026-09-15 13:44:39","dash_updated":"2026-10-12 12:17:31"}],
		"meta":{"total":12}}`}
	severity := 3
	alerts, err := newTestService(collector).ListAlerts(t.Context(), ListAlertsOptions{MinSeverity: &severity, Type: "flex error", Env: "PRD", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/alerts" || request.query.Get("orderby") != "-dash_severity,-dash_updated,id" || request.query.Get("props") != strings.Join(alertProps, ",") ||
		strings.Join(request.query["filter"], "|") != "dash_severity:gte:3|dash_type:eq:flex error|dash_env:eq:PRD" {
		t.Fatalf("request = %+v", request)
	}
	if alerts.Total != 12 || !alerts.Truncated || alerts.NextCursor == "" || alerts.Node != nil || alerts.Service != nil {
		t.Fatalf("page = %+v", alerts)
	}
	got := alerts.Alerts[0]
	if got.ID != 84136172 || got.Type != "flex error" || got.Severity != 5 || got.Message != "0 instances started. thresholds: 1 - 3" ||
		got.Nodename != "" || got.Svcname != "testflex" || got.Env != "PRD" || got.Updated != "2026-10-12 12:17:31" {
		t.Fatalf("alert = %+v", got)
	}
}

func TestListAlertsResolvesTheNodeAndTheService(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":             `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/services/ns1%2Fsvc%2Fweb": `{"data":[{"svc_id":"s-1","svcname":"ns1/svc/web","cluster_id":"c1"}]}`,
		"/alerts":                   `{"data":[],"meta":{"total":0}}`,
	}}
	alerts, err := newTestService(collector).ListAlerts(t.Context(), ListAlertsOptions{Node: "dev2n1", Service: "ns1/svc/web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(collector.requests) != 3 || strings.Join(collector.requests[2].query["filter"], "|") != "node_id:eq:n-1|svc_id:eq:s-1" {
		t.Fatalf("requests = %+v", collector.requests)
	}
	if alerts.Node == nil || alerts.Node.Nodename != "dev2n1" || alerts.Service == nil || alerts.Service.Svcname != "ns1/svc/web" ||
		alerts.Total != 0 || alerts.Truncated || alerts.Alerts == nil {
		t.Fatalf("page = %+v", alerts)
	}
}

func TestListAlertsRejectsInvalidInputs(t *testing.T) {
	low, high := -1, 6
	for _, options := range []ListAlertsOptions{
		{MinSeverity: &low},
		{MinSeverity: &high},
		{Limit: 201},
		{Type: " padded"},
		{Node: "a/b"},
		{Service: "two words"},
		{Cursor: "garbage"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListAlerts(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{body: `{"data":[{"svc_id":"a","cluster_id":"c-a"},{"svc_id":"b","cluster_id":"c-b"}]}`}
	if _, err := newTestService(collector).ListAlerts(t.Context(), ListAlertsOptions{Service: "web"}); err == nil || !strings.Contains(err.Error(), "pass one service ID") {
		t.Fatalf("ambiguous service error = %v", err)
	}
	zero := 0
	collector = &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	if _, err := newTestService(collector).ListAlerts(t.Context(), ListAlertsOptions{MinSeverity: &zero}); err != nil || collector.requests[0].query.Get("filter") != "dash_severity:gte:0" {
		t.Fatalf("min_severity 0: %v %+v", err, collector.requests)
	}
}

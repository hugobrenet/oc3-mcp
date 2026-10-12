package core

import (
	"fmt"
	"strings"
	"testing"
)

const testServiceLookupBody = `{"data":[{"svc_id":"s-1","svcname":"ns1/svc/web","cluster_id":"c1"}]}`

func TestGetServiceStatusHistoryOfTheService(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/ns1%2Fsvc%2Fweb": testServiceLookupBody,
		"/services/s-1/status_log": `{"availability":{"available_s":3600,"counted_s":7200,"excluded_s":600,"from":"2026-10-05 12:31:00","rate":50,"to":"2026-10-12 12:31:00"},
			"data":[{"begin":"2026-10-02 11:25:17","end":"2026-10-05 23:08:33","status":"down","ack":{"comment":"planned","account":false,"acked_by":"ops","acked_on":"2026-10-06 09:00:00"}},
			{"begin":"2026-10-05 23:08:33","end":"2026-10-12 12:30:46","status":"up"}]}`,
	}}
	history, err := newTestService(collector).GetServiceStatusHistory(t.Context(), ServiceStatusHistoryOptions{Service: "ns1/svc/web", Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if request := collector.requests[1]; request.path != "/services/s-1/status_log" || request.query.Get("days") != "30" {
		t.Fatalf("request = %+v", request)
	}
	if history.Service.Svcname != "ns1/svc/web" || history.Node != nil || history.Days != 30 || history.Availability == nil ||
		history.Availability.RatePercent != 50 || history.Availability.ExcludedSeconds != 600 || history.Total != 2 || history.Truncated {
		t.Fatalf("history = %+v", history)
	}
	first := history.Periods[0]
	if first.Status != "down" || first.Avail != "" || first.Ack == nil || first.Ack.Comment != "planned" || first.Ack.Counted || first.Ack.AckedBy != "ops" ||
		history.Periods[1].Ack != nil {
		t.Fatalf("periods = %+v", history.Periods)
	}
}

func TestGetServiceStatusHistoryOfAnInstance(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/web":                          testServiceLookupBody,
		"/nodes/dev2n1":                          `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/services/s-1/instances/n-1/status_log": `{"data":[{"avail":"down","overall":"warn","begin":"2026-10-07 19:16:05","end":"2026-10-07 19:17:57"}]}`,
	}}
	history, err := newTestService(collector).GetServiceStatusHistory(t.Context(), ServiceStatusHistoryOptions{Service: "web", Node: "dev2n1"})
	if err != nil {
		t.Fatal(err)
	}
	if request := collector.requests[2]; request.path != "/services/s-1/instances/n-1/status_log" || request.query.Get("days") != "7" {
		t.Fatalf("request = %+v", request)
	}
	if history.Node == nil || history.Node.Nodename != "dev2n1" || history.Availability != nil || history.Days != 7 ||
		history.Periods[0].Avail != "down" || history.Periods[0].Overall != "warn" || history.Periods[0].Status != "" {
		t.Fatalf("history = %+v", history)
	}
}

func TestGetServiceStatusHistoryKeepsTheMostRecentPeriods(t *testing.T) {
	periods := make([]string, 0, maxStatusPeriods+3)
	for index := range maxStatusPeriods + 3 {
		periods = append(periods, fmt.Sprintf(`{"begin":"b%d","end":"e%d","status":"up"}`, index, index))
	}
	collector := &fakeCollector{bodies: map[string]string{
		"/services/web":            testServiceLookupBody,
		"/services/s-1/status_log": `{"availability":{"rate":100},"data":[` + strings.Join(periods, ",") + `]}`,
	}}
	history, err := newTestService(collector).GetServiceStatusHistory(t.Context(), ServiceStatusHistoryOptions{Service: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if !history.Truncated || history.Total != maxStatusPeriods+3 || history.Count != maxStatusPeriods || history.Periods[0].Begin != "b3" ||
		history.Periods[maxStatusPeriods-1].Begin != fmt.Sprintf("b%d", maxStatusPeriods+2) {
		t.Fatalf("truncated history: total=%d count=%d first=%s", history.Total, history.Count, history.Periods[0].Begin)
	}
}

func TestGetServiceStatusHistoryRejectsInvalidInputsAndResponses(t *testing.T) {
	for _, options := range []ServiceStatusHistoryOptions{
		{Service: "web", Days: -1},
		{Service: "web", Days: 366},
		{Service: ""},
		{Service: "two words"},
	} {
		collector := &fakeCollector{body: testServiceLookupBody}
		if _, err := newTestService(collector).GetServiceStatusHistory(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{body: testServiceLookupBody}
	if _, err := newTestService(collector).GetServiceStatusHistory(t.Context(), ServiceStatusHistoryOptions{Service: "web", Node: "a/b"}); err == nil {
		t.Fatal("an invalid node was accepted")
	}
	for _, body := range []string{`{"availability":{"rate":1}}`, `{"data":[]}`} {
		collector := &fakeCollector{bodies: map[string]string{"/services/web": testServiceLookupBody, "/services/s-1/status_log": body}}
		if _, err := newTestService(collector).GetServiceStatusHistory(t.Context(), ServiceStatusHistoryOptions{Service: "web"}); err == nil {
			t.Fatalf("response %s accepted", body)
		}
	}
}

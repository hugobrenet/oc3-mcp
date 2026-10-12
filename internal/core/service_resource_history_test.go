package core

import (
	"fmt"
	"strings"
	"testing"
)

const testResourcePeriods = `{"data":[
	{"node_id":"n-1","rid":"container#1","res_status":"undef","res_begin":"2026-10-11 23:03:30","res_end":"2026-10-11 23:35:43"},
	{"node_id":"n-1","rid":"container#1","res_status":"down","res_begin":"2026-10-11 23:35:43","res_end":"2026-10-12 16:48:29","res_log":"virsh exit code 1"},
	{"node_id":"n-1","rid":"container#1","res_status":"down","res_begin":"2026-10-09 23:53:12","res_end":"2026-10-11 23:03:30"},
	{"node_id":"n-2","rid":"container#1","res_status":"down","res_begin":"2026-10-11 23:40:00","res_end":"2026-10-12 16:48:29"},
	{"node_id":"n-1","rid":"fs#flag","res_status":"up","res_begin":"2026-09-15 13:44:39","res_end":"2026-09-20 10:00:00"},
	{"node_id":"n-9","rid":"fs#flag","res_status":"down","res_begin":"2026-09-16 10:00:00","res_end":"2026-09-17 10:00:00"}
],"meta":{"total":6}}`

func newResourceHistoryCollector(periods string) *fakeCollector {
	return &fakeCollector{bodies: map[string]string{
		"/services/kvm%2Fsvc%2Fvm1":              `{"data":[{"svc_id":"s-1","svcname":"kvm/svc/vm1","cluster_id":"c1"}]}`,
		"/nodes/dev2n1":                          `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/services/s-1/resources/logs":           periods,
		"/services/s-1/nodes/n-1/resources/logs": periods,
		"/services/s-1/nodes":                    `{"data":[{"node_id":"n-1","nodes.nodename":"dev2n1"},{"node_id":"n-2","nodes.nodename":"dev2n2"}]}`,
	}}
}

func TestGetServiceResourceHistoryGroupsByResourceAndNode(t *testing.T) {
	collector := newResourceHistoryCollector(testResourcePeriods)
	history, err := newTestService(collector).GetServiceResourceHistory(t.Context(), ServiceResourceHistoryOptions{Service: "kvm/svc/vm1"})
	if err != nil {
		t.Fatal(err)
	}
	if request := collector.requests[1]; request.path != "/services/s-1/resources/logs" || request.query.Get("limit") != "5000" || request.query.Has("orderby") {
		t.Fatalf("request = %+v", request)
	}
	if history.Read != 6 || history.Matching != 6 || history.Count != 6 || history.Truncated || len(history.Resources) != 4 {
		t.Fatalf("history = %+v", history)
	}
	c1 := history.Resources[0]
	if c1.RID != "container#1" || c1.Nodename != "dev2n1" || c1.CurrentStatus != "down" || c1.Since != "2026-10-11 23:35:43" ||
		c1.LastReported != "2026-10-12 16:48:29" || len(c1.Periods) != 3 || c1.Periods[0].Log != "virsh exit code 1" || c1.Periods[1].Status != "undef" {
		t.Fatalf("container#1 on dev2n1 = %+v", c1)
	}
	if c2 := history.Resources[1]; c2.Nodename != "dev2n2" || c2.Since != "2026-10-11 23:40:00" {
		t.Fatalf("container#1 on dev2n2 = %+v", c2)
	}
	if gone := history.Resources[3]; gone.RID != "fs#flag" || gone.NodeID != "n-9" || gone.Nodename != "" {
		t.Fatalf("resource of a node without instance = %+v", gone)
	}
}

func TestGetServiceResourceHistoryFilters(t *testing.T) {
	collector := newResourceHistoryCollector(testResourcePeriods)
	history, err := newTestService(collector).GetServiceResourceHistory(t.Context(), ServiceResourceHistoryOptions{
		Service: "kvm/svc/vm1", Node: "dev2n1", RID: "container#1", Status: "down", Since: "2026-10-11 23:10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[2].path != "/services/s-1/nodes/n-1/resources/logs" || history.Node == nil {
		t.Fatalf("requests = %+v", collector.requests)
	}
	// The node route answers the periods of the test body: the filters keep
	// the down periods of container#1 ending at or after the date.
	if history.Matching != 2 || len(history.Resources) != 2 || history.Resources[0].Periods[0].Begin != "2026-10-11 23:35:43" {
		t.Fatalf("history = %+v", history)
	}
}

func TestGetServiceResourceHistoryBoundsThePeriods(t *testing.T) {
	var rows []string
	for index := range maxResourcePeriodLimit + 20 {
		rows = append(rows, fmt.Sprintf(`{"node_id":"n-1","rid":"fs#1","res_status":"down","res_begin":"2026-10-01 %02d:%02d:00","res_log":"%s"}`, index/60%24, index%60, strings.Repeat("x", maxResourceLogText+5)))
	}
	collector := newResourceHistoryCollector(`{"data":[` + strings.Join(rows, ",") + `],"meta":{"total":9000}}`)
	history, err := newTestService(collector).GetServiceResourceHistory(t.Context(), ServiceResourceHistoryOptions{Service: "kvm/svc/vm1"})
	if err != nil || !history.Truncated || history.Count != defaultResourcePeriodLimit {
		t.Fatalf("default limit: count %d, %v", history.Count, err)
	}
	history, err = newTestService(collector).GetServiceResourceHistory(t.Context(), ServiceResourceHistoryOptions{Service: "kvm/svc/vm1", Limit: maxResourcePeriodLimit})
	if err != nil {
		t.Fatal(err)
	}
	if !history.Truncated || history.Count != maxResourcePeriodLimit || len(history.Resources[0].Periods[0].Log) != maxResourceLogText ||
		history.Resources[0].Periods[0].Begin < history.Resources[0].Periods[1].Begin {
		t.Fatalf("truncated %v count %d log %d", history.Truncated, history.Count, len(history.Resources[0].Periods[0].Log))
	}
}

func TestGetServiceResourceHistoryRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ServiceResourceHistoryOptions{
		{},
		{Service: "two words"},
		{Service: "web", Since: "last week"},
		{Service: "web", RID: " fs#1"},
		{Service: "web", Limit: 501},
		{Service: "web", Limit: -1},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).GetServiceResourceHistory(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := newResourceHistoryCollector(`{"data":[]}`)
	if _, err := newTestService(collector).GetServiceResourceHistory(t.Context(), ServiceResourceHistoryOptions{Service: "kvm/svc/vm1"}); err == nil {
		t.Fatal("a response without total was accepted")
	}
}

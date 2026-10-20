package core

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// slaCollector refuses the changes of some services as the collector does,
// with an HTTP error, and answers the others with fakeCollector.
type slaCollector struct {
	*fakeCollector
	refusals map[string]string
}

func (c slaCollector) PostJSON(ctx context.Context, path string, input any, output any) error {
	if text, ok := c.refusals[path]; ok {
		c.record(fakeRequest{method: "POST", path: path})
		return errString(text)
	}
	return c.fakeCollector.PostJSON(ctx, path, input, output)
}

// newSLACollector answers the nodes, the instances and the services of a
// selection by instance, and the SLA changes: 60 nodes in service, whose
// instances are read in two chunks, and three services, one on two nodes.
func newSLACollector() *fakeCollector {
	var mutex sync.Mutex
	return &fakeCollector{answer: func(path string, query url.Values) string {
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case path == "/nodes":
			rows := make([]string, 60)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"node_id":"n-%02d","nodename":"node%02d"}`, i, i)
			}
			offset := query.Get("offset")
			if offset != "0" {
				return `{"data":[],"meta":{"total":60}}`
			}
			return `{"data":[` + strings.Join(rows, ",") + `],"meta":{"total":60}}`
		case path == "/tags/t-2/nodes":
			return `{"data":[{"node_id":"n-00","nodename":"node00"}],"meta":{"total":1}}`
		case path == "/services_instances" && strings.Contains(query.Get("filter"), "n-01"):
			return `{"data":[{"svc_id":"s-1","node_id":"n-00","services.svcname":"web","services.svc_availability":99.2},{"svc_id":"s-2","node_id":"n-01","services.svcname":"db","services.svc_sla":99.9}],"meta":{"total":2}}`
		case path == "/services_instances" && strings.Contains(query.Get("filter"), "n-50"):
			return `{"data":[{"svc_id":"s-1","node_id":"n-50","services.svcname":"web"},{"svc_id":"s-3","node_id":"n-51","services.svcname":"lab"}],"meta":{"total":2}}`
		case path == "/services_instances" && strings.Contains(query.Get("filter"), "n-00"):
			return `{"data":[{"svc_id":"s-1","node_id":"n-00","services.svcname":"web"}],"meta":{"total":1}}`
		case path == "/services_instances":
			return `{"data":[{"svc_id":"s-3","node_id":"n-51","services.svcname":"lab"}],"meta":{"total":1}}`
		case path == "/tags" && strings.Contains(query.Get("filter"), "rack-b"):
			return `{"data":[{"tag_id":"t-2","tag_name":"rack-b"}]}`
		case path == "/tags":
			return `{"data":[{"tag_id":"t-1","tag_name":"pci"}]}`
		case path == "/tags/t-1/services":
			return `{"data":[{"svc_id":"s-1","svcname":"web"},{"svc_id":"s-3","svcname":"lab"}],"meta":{"total":2}}`
		case strings.HasPrefix(path, "/services/"):
			return `{"info":"changed","data":[{"svc_sla":99.9,"svc_availability":99.2}]}`
		}
		return `{"data":[]}`
	}}
}

func TestSetServiceSLAOnTheServicesOfTheNodesSelected(t *testing.T) {
	collector := slaCollector{fakeCollector: newSLACollector(), refusals: map[string]string{
		"/services/s-3": "HTTP 403 Forbidden: user is not responsible for service s-3",
	}}
	sla := 99.9
	change, err := New(collector).SetServiceSLA(t.Context(), ServiceSLAOptions{SLA: &sla, ServiceSelection: ServiceSelection{OnNodes: &NodeFilters{Status: "In service"}}})
	if err != nil {
		t.Fatal(err)
	}
	// The three services once each, though web has an instance on two
	// nodes read in two chunks of nodes, sorted db, lab, web: db already
	// at 99.9 is left as it is, lab is refused, web is set.
	db, lab, web := change.Services[0], change.Services[1], change.Services[2]
	if change.OnNodes == nil || *change.OnNodes != 60 || change.Selected != 3 || change.Changed != 1 || change.Unchanged != 1 || change.Refused != 1 ||
		db.Outcome != ChangeUnchanged || lab.Outcome != ChangeRefused || !strings.Contains(lab.Reason, "not responsible") ||
		web.Outcome != ChangeSet || web.SLA == nil || *web.SLA != 99.9 || web.Availability == nil || *web.Availability != 99.2 {
		t.Fatalf("change = %+v", change)
	}
	chunks := 0
	for _, request := range collector.requests {
		switch {
		case request.path == "/nodes" && !strings.Contains(strings.Join(request.query["filter"], " "), "status:eq:In service"):
			t.Fatalf("node selection = %+v", request)
		case request.path == "/services_instances":
			chunks++
			if !strings.HasPrefix(request.query.Get("filter"), "node_id:in:") {
				t.Fatalf("instance selection = %+v", request)
			}
		case request.method == "POST" && request.path == "/services/s-2":
			t.Fatalf("a service already as asked was changed: %+v", request)
		case request.method == "POST" && request.path == "/services/s-1" && request.body["svc_sla"] != "99.9":
			t.Fatalf("change = %+v", request)
		}
	}
	if chunks != 2 {
		t.Fatalf("the instances were read in %d chunks", chunks)
	}
}

// The target is rounded to the three decimals the collector stores; a tag
// of the nodes is told in the result.
func TestSetServiceSLARoundsAndTellsTheNodeFilter(t *testing.T) {
	collector := newSLACollector()
	sla := 99.9995
	change, err := newTestService(collector).SetServiceSLA(t.Context(), ServiceSLAOptions{SLA: &sla, ServiceSelection: ServiceSelection{OnNodes: &NodeFilters{Tag: "rack-b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.SLA == nil || *change.SLA != 100 || change.OnNodesFilter == nil || change.OnNodesFilter.Tag == nil || change.OnNodesFilter.Tag.Name != "rack-b" || change.Selected != 1 {
		t.Fatalf("change = %+v", change)
	}
	for _, request := range collector.requests {
		if request.method == "POST" && request.body["svc_sla"] != "100" {
			t.Fatalf("change = %+v", request)
		}
	}
}

// A state of the instances alone reads every instance in it; a tag filter
// keeps the services both select, and a removal sends the empty text.
func TestRemoveServiceSLAByInstanceStateAndTag(t *testing.T) {
	collector := newSLACollector()
	change, err := newTestService(collector).SetServiceSLA(t.Context(), ServiceSLAOptions{Remove: true, ServiceSelection: ServiceSelection{
		Instance: InstanceState{Availability: "down"}, Filters: ServiceFilters{Tag: "pci"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Selected != 1 || change.Services[0].Svcname != "lab" || change.FilterTag == nil || !change.Removed {
		t.Fatalf("change = %+v", change)
	}
	for _, request := range collector.requests {
		if request.path == "/services_instances" && request.query.Get("filter") != "mon_availstatus:eq:down" {
			t.Fatalf("instance selection = %+v", request)
		}
		if request.method == "POST" && request.body["svc_sla"] != "" {
			t.Fatalf("change = %+v", request)
		}
	}
	for _, options := range []ServiceSLAOptions{
		{ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{SLA: new(float64), Remove: true, ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{SLA: func() *float64 { v := 120.0; return &v }(), ServiceSelection: ServiceSelection{Services: []string{"web"}}},
		{Remove: true},
	} {
		collector := newSLACollector()
		if _, err := newTestService(collector).SetServiceSLA(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

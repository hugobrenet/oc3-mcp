package core

import (
	"net/url"
	"strings"
	"testing"
)

// deleteCollector answers a service, a node, and the instance counts: total
// instances, and recent ones when the filter asks for them.
func deleteCollector(instances, recent int) *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch {
		case path == "/services/web":
			return `{"data":[{"svc_id":"s-1","svcname":"web","cluster_id":"c1","svc_app":"WEB"}]}`
		case path == "/services/dup":
			return `{"data":[{"svc_id":"s-1","cluster_id":"c1"},{"svc_id":"s-2","cluster_id":"c2"}]}`
		case path == "/nodes/n1":
			return `{"data":[{"node_id":"n-1","nodename":"n1","cluster_id":"c1"}]}`
		case path == "/services_instances":
			total := instances
			if strings.Contains(strings.Join(query["filter"], " "), "mon_updated:gte:-1h") {
				total = recent
			}
			if total == 0 {
				return `{"data":[],"meta":{"total":0}}`
			}
			return `{"data":[{"svc_id":"s-1","mon_updated":"2026-10-19 16:11:08"}],"meta":{"total":` + itoa(total) + `}}`
		case query == nil:
			return `{"info":"done"}`
		}
		return `{"data":[]}`
	}}
}

func TestDeleteServiceWarnsOfAServiceStillReported(t *testing.T) {
	for _, recent := range []int{0, 2} {
		collector := deleteCollector(3, recent)
		deletion, err := newTestService(collector).DeleteService(t.Context(), "web")
		if err != nil {
			t.Fatal(err)
		}
		last := collector.requests[len(collector.requests)-1]
		if last.method != "DELETE" || last.path != "/services/s-1" || !deletion.Deleted || deletion.Instances != 3 || deletion.App != "WEB" ||
			deletion.Recent != recent || (deletion.Warning != "") != (recent > 0) {
			t.Fatalf("recent %d: deletion = %+v, last %+v", recent, deletion, last)
		}
	}
	collector := deleteCollector(0, 0)
	if _, err := newTestService(collector).DeleteService(t.Context(), "dup"); err == nil || !strings.Contains(err.Error(), "s-1 (cluster c1), s-2 (cluster c2)") {
		t.Fatalf("homonyms: %v", err)
	}
	for _, request := range collector.requests {
		if request.method != "GET" {
			t.Fatalf("homonyms sent %+v", request)
		}
	}
}

func TestDeleteServiceInstance(t *testing.T) {
	collector := deleteCollector(1, 1)
	deletion, err := newTestService(collector).DeleteServiceInstance(t.Context(), "web", "n1")
	if err != nil {
		t.Fatal(err)
	}
	last := collector.requests[len(collector.requests)-1]
	if last.method != "DELETE" || last.path != "/services/s-1/instances/n-1" || deletion.Updated != "2026-10-19 16:11:08" || deletion.Warning == "" {
		t.Fatalf("deletion = %+v, last %+v", deletion, last)
	}
	// No instance of the service on the node: nothing is sent.
	collector = deleteCollector(0, 0)
	if _, err := newTestService(collector).DeleteServiceInstance(t.Context(), "web", "n1"); err == nil || !strings.Contains(err.Error(), "has no instance on node n1") {
		t.Fatalf("no instance: %v", err)
	}
	for _, request := range collector.requests {
		if request.method != "GET" {
			t.Fatalf("no instance sent %+v", request)
		}
	}
}

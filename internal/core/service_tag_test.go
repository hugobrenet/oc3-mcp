package core

import (
	"net/url"
	"strings"
	"sync"
	"testing"
)

// serviceTagCollector answers the tag lookup, the service reads and the
// changes, the answer of a change given by its service ID.
func serviceTagCollector(services string, change map[string]string) *fakeCollector {
	var mutex sync.Mutex
	return &fakeCollector{answer: func(path string, query url.Values) string {
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case path == "/tags":
			return `{"data":[{"tag_id":"t-1","tag_name":"backup-nightly"}]}`
		case path == "/nodes/n1":
			return `{"data":[{"node_id":"n-1","nodename":"n1","cluster_id":"c1"}]}`
		case path == "/services" || path == "/services_instances":
			return services
		case strings.HasPrefix(path, "/tags/t-1/services/"):
			return change[strings.TrimPrefix(path, "/tags/t-1/services/")]
		}
		return `{"data":[]}`
	}}
}

func TestAttachServiceTagByFilters(t *testing.T) {
	collector := serviceTagCollector(`{"data":[{"svc_id":"s-2","svcname":"web"},{"svc_id":"s-1","svcname":"db"}],"meta":{"total":2}}`,
		map[string]string{"s-1": `{"info":"service 's-1' tag 'backup-nightly' attached"}`, "s-2": `{"info":"tag 'backup-nightly' already attached to service 's-2'"}`})
	change, err := newTestService(collector).AttachServiceTag(t.Context(), ServiceTagOptions{Tag: "backup-nightly", ServiceSelection: ServiceSelection{Filters: ServiceFilters{App: "WEB", Status: "down"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Selected != 2 || change.Changed != 1 || change.Unchanged != 1 || change.Services[0].Svcname != "db" || change.Services[0].Outcome != ChangeAttached {
		t.Fatalf("change = %+v", change)
	}
	for _, request := range collector.requests {
		if request.path == "/services" && strings.Join(request.query["filter"], " ") != "svc_app:eq:WEB svc_status:eq:down" {
			t.Fatalf("selection request = %+v", request)
		}
	}
	for _, selection := range []ServiceSelection{{}, {Services: []string{"a"}, Filters: ServiceFilters{App: "x"}}, {Node: "n1", OnNodes: &NodeFilters{Status: "x"}},
		{OnNodes: &NodeFilters{}}, {Instance: InstanceState{Availability: "a\x00"}},
		{Filters: ServiceFilters{Topology: "ring"}}, {Services: make([]string, 51)}} {
		collector := serviceTagCollector(`{"data":[]}`, nil)
		if _, err := newTestService(collector).AttachServiceTag(t.Context(), ServiceTagOptions{Tag: "backup-nightly", ServiceSelection: selection}); err == nil || len(collector.requests) != 0 {
			t.Fatalf("selection %+v accepted", selection)
		}
	}
}

// The services of a node are read from its instances, their props named
// with the services prefix.
func TestDetachServiceTagFromTheServicesOfANode(t *testing.T) {
	collector := serviceTagCollector(`{"data":[{"svc_id":"s-1","services.svcname":"db","services.cluster_id":"c1"}],"meta":{"total":1}}`,
		map[string]string{"s-1": `{"info":"tag detached"}`})
	change, err := newTestService(collector).DetachServiceTag(t.Context(), ServiceTagOptions{Tag: "backup-nightly", ServiceSelection: ServiceSelection{Node: "n1", Filters: ServiceFilters{Topology: "failover"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Node.Nodename != "n1" || change.Changed != 1 || change.Services[0].Svcname != "db" || change.Services[0].ClusterID != "c1" {
		t.Fatalf("change = %+v", change)
	}
	for _, request := range collector.requests {
		if request.path == "/services_instances" &&
			(strings.Join(request.query["filter"], " ") != "node_id:eq:n-1 services.svc_topology:eq:failover" || request.query.Get("props") != "svc_id,services.svcname,services.cluster_id,services.svc_topology,services.svc_sla,services.svc_availability,node_id,nodes.nodename,nodes.version,mon_availstatus") {
			t.Fatalf("selection request = %+v", request)
		}
	}
}

// A service name two clusters hold refuses the whole call, with their IDs.
func TestAttachServiceTagRefusesANameSeveralClustersHold(t *testing.T) {
	collector := serviceTagCollector(`{"data":[{"svc_id":"s-1","svcname":"web","cluster_id":"c1"},{"svc_id":"s-2","svcname":"web","cluster_id":"c2"}],"meta":{"total":2}}`, nil)
	_, err := newTestService(collector).AttachServiceTag(t.Context(), ServiceTagOptions{Tag: "backup-nightly", ServiceSelection: ServiceSelection{Services: []string{"web"}}})
	if err == nil || !strings.Contains(err.Error(), "s-1 (cluster c1), s-2 (cluster c2)") {
		t.Fatalf("homonyms: %v", err)
	}
	for _, request := range collector.requests {
		if request.method != "GET" {
			t.Fatalf("homonyms sent %+v", request)
		}
	}
}

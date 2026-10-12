package core

import (
	"strings"
	"testing"
)

func TestListClustersQueriesClusters(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"cluster_id":"82e47bfb-182f-413b-b5dd-9de8164095d5","cluster_name":"reliable-leopard",
		"cluster_nodes":"dev2n1, dev2n2, dev2n3","node_count":3,"svc_count":618,"agent_versions":"v3.0.0-rc40-2-g2b577","quorum":1,"frozen":0,
		"cluster_updated":"2026-10-12T14:51:00.781813896+02:00"}],"meta":{"total":2}}`}
	clusters, err := newTestService(collector).ListClusters(t.Context(), ListClustersOptions{Name: "reliable", ClusterID: "82e47bfb-182f-413b-b5dd-9de8164095d5", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/clusters" || request.query.Get("orderby") != "cluster_name,cluster_id" || request.query.Get("props") != strings.Join(clusterProps, ",") ||
		strings.Join(request.query["filter"], "|") != "cluster_name:~reliable|cluster_id:eq:82e47bfb-182f-413b-b5dd-9de8164095d5" {
		t.Fatalf("request = %+v", request)
	}
	if clusters.Total != 2 || !clusters.Truncated || clusters.NextCursor == "" {
		t.Fatalf("page = %+v", clusters)
	}
	got := clusters.Clusters[0]
	if got.Name != "reliable-leopard" || strings.Join(got.Nodes, ",") != "dev2n1,dev2n2,dev2n3" || got.NodeCount != 3 || got.ServiceCount != 618 ||
		!got.Quorum || got.Frozen || got.Updated != "2026-10-12T14:51:00.781813896+02:00" {
		t.Fatalf("cluster = %+v", got)
	}
}

func TestListClustersRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListClustersOptions{
		{ClusterID: "reliable-leopard"},
		{Name: " padded"},
		{Limit: 201},
		{Cursor: "garbage"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListClusters(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	if nodes := splitClusterNodes(" n1,, n2 ,"); strings.Join(nodes, ",") != "n1,n2" {
		t.Fatalf("nodes = %q", nodes)
	}
	if nodes := splitClusterNodes(""); nodes != nil {
		t.Fatalf("empty nodes = %q", nodes)
	}
}

package core

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const (
	defaultListClustersLimit = 50
	maxListClustersLimit     = 200
)

var clusterProps = []string{
	"cluster_id", "cluster_name", "cluster_nodes", "node_count", "svc_count", "agent_versions", "quorum", "frozen", "cluster_updated",
}

type ListClustersOptions struct {
	Name      string
	ClusterID string
	Limit     int
	Cursor    string
}

type ClusterList struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total      int        `json:"total" jsonschema:"the number of clusters matching the filters, as the collector counts them"`
	Count      int        `json:"count" jsonschema:"the number of clusters returned in this page"`
	Clusters   []Cluster  `json:"clusters" jsonschema:"the clusters sorted by name, then cluster ID"`
	NextCursor string     `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool       `json:"truncated" jsonschema:"whether matching clusters remain after this page"`
}

type Cluster struct {
	ClusterID     string   `json:"cluster_id" jsonschema:"the OpenSVC cluster ID"`
	Name          string   `json:"name" jsonschema:"the OpenSVC cluster name"`
	Nodes         []string `json:"nodes,omitempty" jsonschema:"the nodes of the cluster, as its daemon last reported them"`
	NodeCount     int      `json:"node_count" jsonschema:"the number of nodes of the cluster"`
	ServiceCount  int      `json:"service_count" jsonschema:"the number of services of the cluster the collector knows"`
	AgentVersions string   `json:"agent_versions,omitempty" jsonschema:"the OpenSVC agent versions running in the cluster"`
	Quorum        bool     `json:"quorum" jsonschema:"whether quorum is enabled in the cluster configuration"`
	Frozen        bool     `json:"frozen" jsonschema:"whether the cluster is frozen"`
	Updated       string   `json:"updated,omitempty" jsonschema:"when the collector last received the cluster status, as the collector gives it"`
}

type collectorCluster struct {
	ClusterID      string `json:"cluster_id"`
	ClusterName    string `json:"cluster_name"`
	ClusterNodes   string `json:"cluster_nodes"`
	NodeCount      int    `json:"node_count"`
	SvcCount       int    `json:"svc_count"`
	AgentVersions  string `json:"agent_versions"`
	Quorum         int    `json:"quorum"`
	Frozen         int    `json:"frozen"`
	ClusterUpdated string `json:"cluster_updated"`
}

// ListClusters reads one page of the clusters the collector knows from the
// status their daemons send, sorted by name, with typed filters.
func (s *Service) ListClusters(ctx context.Context, options ListClustersOptions) (ClusterList, error) {
	if err := validateFilterValue("name", options.Name); err != nil {
		return ClusterList{}, err
	}
	if err := validateCollectorUUID("cluster_id", options.ClusterID); err != nil {
		return ClusterList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListClustersLimit, maxListClustersLimit)
	if err != nil {
		return ClusterList{}, err
	}
	var filters []string
	if options.Name != "" {
		filters = append(filters, filterContains("cluster_name", options.Name))
	}
	if options.ClusterID != "" {
		filters = append(filters, filterEquals("cluster_id", options.ClusterID))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ClusterList{}, err
	}

	var response collectorList[collectorCluster]
	if err := s.client.GetJSON(ctx, "/clusters", pageQuery(clusterProps, "cluster_name,cluster_id", filters, limit, offset), &response); err != nil {
		return ClusterList{}, fmt.Errorf("list clusters: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return ClusterList{}, fmt.Errorf("list clusters: unexpected collector response")
	}
	result := ClusterList{Provenance: s.newProvenance(), Total: *response.Meta.Total, Count: len(response.Data), Clusters: make([]Cluster, 0, len(response.Data))}
	for _, cluster := range response.Data {
		result.Clusters = append(result.Clusters, Cluster{
			ClusterID: cluster.ClusterID, Name: cluster.ClusterName, Nodes: splitClusterNodes(cluster.ClusterNodes),
			NodeCount: cluster.NodeCount, ServiceCount: cluster.SvcCount, AgentVersions: cluster.AgentVersions,
			Quorum: cluster.Quorum != 0, Frozen: cluster.Frozen != 0, Updated: cluster.ClusterUpdated,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// splitClusterNodes splits the "n1, n2" node list of the collector.
func splitClusterNodes(value string) []string {
	var nodes []string
	for _, node := range strings.Split(value, ",") {
		if node = strings.TrimSpace(node); node != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

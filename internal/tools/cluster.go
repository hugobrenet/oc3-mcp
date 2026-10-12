package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListClustersInput struct {
	Name      string `json:"name,omitempty" jsonschema:"optional text the cluster name contains, case-insensitive"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"optional exact cluster ID"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListClustersOutput = core.ClusterList

func RegisterClusterTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_clusters",
			Title: "List collector clusters",
			Description: "List the OpenSVC clusters the collector knows, sorted by name: cluster ID, name, nodes, node and service counts, agent versions, quorum and freeze flags, and when the collector last received their status. " +
				"Filter by text in the name or by exact cluster ID, to name the cluster_id the other tools return. " +
				"The collector knows a cluster from the status its daemon sends: a cluster_id absent from this list is one whose daemon status the collector never received. Collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListClustersInput) (*mcp.CallToolResult, ListClustersOutput, error) {
			clusters, err := service.ListClusters(ctx, core.ListClustersOptions{Name: input.Name, ClusterID: input.ClusterID, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListClustersOutput{}, err
			}
			return nil, clusters, nil
		},
	)
}

package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// nodeGroupProps are the groupings of count_nodes and their collector prop.
var nodeGroupProps = map[string]string{
	"cluster_id":    "cluster_id",
	"app":           "app",
	"env":           "node_env",
	"status":        "status",
	"type":          "type",
	"os_name":       "os_name",
	"os_vendor":     "os_vendor",
	"os_release":    "os_release",
	"os_arch":       "os_arch",
	"agent_version": "version",
	"manufacturer":  "manufacturer",
	"model":         "model",
	"team":          "team_responsible",
	"country":       "loc_country",
	"city":          "loc_city",
}

// NodeGroupings lists the groupings of count_nodes, sorted.
func NodeGroupings() []string {
	names := make([]string, 0, len(nodeGroupProps))
	for name := range nodeGroupProps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type CountNodesOptions struct {
	NodeFilters
	GroupBy string
	Limit   int
}

type NodeCount struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Tag        *TagReference `json:"tag,omitempty" jsonschema:"the tag the tag filter resolved to; only the nodes holding it are counted"`
	Total      int           `json:"total" jsonschema:"the number of nodes matching the filters, as the collector counts them"`
	GroupBy    string        `json:"group_by,omitempty" jsonschema:"the grouping asked"`
	Groups     []CountGroup  `json:"groups,omitempty" jsonschema:"with group_by, the most frequent values first, and the number of matching nodes with each"`
	Distinct   int           `json:"distinct,omitempty" jsonschema:"with group_by, the number of distinct values among the matching nodes"`
	Other      int           `json:"other,omitempty" jsonschema:"with group_by, the number of matching nodes whose value is not in groups, left out by limit"`
	Truncated  bool          `json:"truncated" jsonschema:"whether values were left out by limit"`
}

// CountNodes counts the nodes matching the filters and, with a grouping, the
// nodes of each value, as the collector computes them.
func (s *Service) CountNodes(ctx context.Context, options CountNodesOptions) (NodeCount, error) {
	filters, err := options.NodeFilters.collectorFilters()
	if err != nil {
		return NodeCount{}, err
	}
	prop := ""
	if options.GroupBy != "" {
		var ok bool
		if prop, ok = nodeGroupProps[options.GroupBy]; !ok {
			return NodeCount{}, fmt.Errorf("group_by must be one of %s", strings.Join(NodeGroupings(), ", "))
		}
	}
	limit, err := pageLimit(options.Limit, defaultCountGroups, maxCountGroups)
	if err != nil {
		return NodeCount{}, err
	}
	path, tag, err := s.taggedPath(ctx, options.Tag, "nodes")
	if err != nil {
		return NodeCount{}, err
	}
	total, groups, distinct, other, err := s.countRows(ctx, path, "node_id", filters, prop, limit)
	if err != nil {
		return NodeCount{}, fmt.Errorf("count nodes: %w", err)
	}
	return NodeCount{
		Provenance: s.newProvenance(), Tag: tag, Total: total, GroupBy: options.GroupBy, Groups: groups,
		Distinct: distinct, Other: other, Truncated: other > 0,
	}, nil
}

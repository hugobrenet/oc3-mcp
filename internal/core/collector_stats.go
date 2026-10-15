package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

const (
	defaultCountGroups = 20
	maxCountGroups     = 100
)

// CountGroup is one value of a grouping and its number of items.
type CountGroup struct {
	Value string `json:"value" jsonschema:"the value; empty for the items without one. The collector groups the values case-insensitively and shows one spelling"`
	Count int    `json:"count" jsonschema:"the number of items with this value"`
}

// collectorStats is the response of a list endpoint with stats=true: the
// number of rows of each value of the selected props.
type collectorStats struct {
	Data map[string]map[string]int `json:"data"`
	Meta struct {
		Total    *int           `json:"total"`
		Distinct map[string]int `json:"distinct"`
		Other    map[string]int `json:"other"`
	} `json:"meta"`
}

// countRows counts the rows of path matching filters and, with prop, the
// rows of its most frequent values, as the collector computes them. idProp
// is the prop read to count the rows without prop.
func (s *Service) countRows(ctx context.Context, path string, idProp string, filters []string, prop string, limit int) (total int, groups []CountGroup, distinct int, other int, err error) {
	query := url.Values{"filter": filters}
	if prop == "" {
		query.Set("limit", "1")
		query.Set("props", idProp)
		var response collectorList[struct{}]
		if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
			return 0, nil, 0, 0, err
		}
		if response.Meta.Total == nil || *response.Meta.Total < 0 {
			return 0, nil, 0, 0, fmt.Errorf("unexpected collector response")
		}
		return *response.Meta.Total, nil, 0, 0, nil
	}
	query.Set("stats", "true")
	query.Set("props", prop)
	query.Set("limit", strconv.Itoa(limit))
	var response collectorStats
	if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
		return 0, nil, 0, 0, err
	}
	values, ok := response.Data[prop]
	if !ok || response.Meta.Total == nil || *response.Meta.Total < 0 || len(values) > limit {
		return 0, nil, 0, 0, fmt.Errorf("unexpected collector response")
	}
	groups = make([]CountGroup, 0, len(values))
	for value, count := range values {
		if value == "empty" {
			value = ""
		}
		groups = append(groups, CountGroup{Value: value, Count: count})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Value < groups[j].Value
	})
	return *response.Meta.Total, groups, response.Meta.Distinct[prop], response.Meta.Other[prop], nil
}

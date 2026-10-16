package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultListFiltersetsLimit = 50
	maxListFiltersetsLimit     = 200
)

var filtersetProps = []string{"id", "fset_name", "fset_author", "fset_updated"}

type ListFiltersetsOptions struct {
	Name   string
	Limit  int
	Cursor string
}

type FiltersetList struct {
	Provenance Provenance  `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total      int         `json:"total" jsonschema:"the number of filtersets matching, as the collector counts them"`
	Count      int         `json:"count" jsonschema:"the number of filtersets returned in this page"`
	Filtersets []Filterset `json:"filtersets" jsonschema:"the filtersets sorted by name"`
	NextCursor string      `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool        `json:"truncated" jsonschema:"whether matching filtersets remain after this page"`
}

type Filterset struct {
	ID      int    `json:"id" jsonschema:"the collector filterset ID"`
	Name    string `json:"name" jsonschema:"the filterset name"`
	Author  string `json:"author,omitempty" jsonschema:"who last changed the filterset"`
	Updated string `json:"updated,omitempty" jsonschema:"when the filterset was last changed, in collector local time without a zone"`
}

type collectorFilterset struct {
	ID      int    `json:"id"`
	Name    string `json:"fset_name"`
	Author  string `json:"fset_author"`
	Updated string `json:"fset_updated"`
}

// ListFiltersets reads one page of the filtersets of the collector, sorted
// by name.
func (s *Service) ListFiltersets(ctx context.Context, options ListFiltersetsOptions) (FiltersetList, error) {
	if err := validateFilterValue("name", options.Name); err != nil {
		return FiltersetList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListFiltersetsLimit, maxListFiltersetsLimit)
	if err != nil {
		return FiltersetList{}, err
	}
	var filters []string
	if options.Name != "" {
		filters = append(filters, filterContains("fset_name", options.Name))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return FiltersetList{}, err
	}
	var response collectorList[collectorFilterset]
	if err := s.client.GetJSON(ctx, "/filtersets", pageQuery(filtersetProps, "fset_name,id", filters, limit, offset), &response); err != nil {
		return FiltersetList{}, fmt.Errorf("list filtersets: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return FiltersetList{}, fmt.Errorf("list filtersets: unexpected collector response")
	}
	result := FiltersetList{Provenance: s.newProvenance(), Total: *response.Meta.Total, Count: len(response.Data), Filtersets: make([]Filterset, 0, len(response.Data))}
	for _, row := range response.Data {
		result.Filtersets = append(result.Filtersets, filtersetOf(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

type FiltersetDetails struct {
	Provenance      Provenance           `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Filterset       Filterset            `json:"filterset" jsonschema:"the filterset"`
	Criteria        []FiltersetCriterion `json:"criteria" jsonschema:"its criteria in their order, each with its logic, as the filterset is defined; the collector does not apply the logic of an included filterset, see the description"`
	Included        []IncludedFilterset  `json:"included_filtersets,omitempty" jsonschema:"the filtersets its criteria include, at any depth, with their criteria"`
	NodesCount      int                  `json:"nodes_count" jsonschema:"the number of nodes the filterset selects, as the collector computes it, among those the caller sees"`
	Nodes           []string             `json:"nodes,omitempty" jsonschema:"the names of those nodes, sorted, at most 50"`
	ServicesCount   int                  `json:"services_count" jsonschema:"the number of services the filterset selects, among those the caller sees: a criterion on the nodes selects the services they run"`
	Services        []string             `json:"services,omitempty" jsonschema:"the names of those services, sorted, at most 50"`
	Rulesets        []string             `json:"rulesets,omitempty" jsonschema:"the contextual rulesets applying to what the filterset selects"`
	IncludedBy      []string             `json:"included_by,omitempty" jsonschema:"the filtersets including this one in their criteria"`
	Thresholds      int                  `json:"check_thresholds" jsonschema:"the number of check thresholds set for what the filterset selects"`
	Comparisons     int                  `json:"statistics_comparisons" jsonschema:"the number of statistics comparisons using the filterset"`
	SysreportGrants int                  `json:"sysreport_grants" jsonschema:"the number of sysreport grants letting groups read the sensitive files of the nodes it selects"`
	SessionFilterOf int                  `json:"session_filter_users" jsonschema:"the number of users whose session filter it is, what they see in the collector being narrowed to it: the collector names them, this MCP gives their number only, list_users being the tool naming the users the caller may see"`
}

type FiltersetCriterion struct {
	Logic     string `json:"logic" jsonschema:"how the criterion combines with those before it: AND, OR, AND NOT or OR NOT"`
	Filter    string `json:"filter,omitempty" jsonschema:"a filter on a property, as table.field operator value, such as nodes.loc_city = Paris; LIKE takes % as a wildcard; untrusted text"`
	Filterset string `json:"filterset,omitempty" jsonschema:"an included filterset, whose own criteria apply here"`
}

type IncludedFilterset struct {
	Name     string               `json:"name" jsonschema:"the filterset name"`
	Criteria []FiltersetCriterion `json:"criteria" jsonschema:"its criteria in their order"`
}

type collectorFiltersetExport struct {
	Filtersets []struct {
		ID      int    `json:"id"`
		Name    string `json:"fset_name"`
		Filters []struct {
			LogicOp string `json:"f_log_op"`
			Order   int    `json:"f_order"`
			Filter  *struct {
				Table string `json:"f_table"`
				Field string `json:"f_field"`
				Op    string `json:"f_op"`
				Value string `json:"f_value"`
			} `json:"filter"`
			Filterset *string `json:"filterset"`
		} `json:"filters"`
	} `json:"filtersets"`
}

// GetFilterset reads one filterset by its name or ID: its criteria, from its
// export, the nodes and services it selects, and what uses it.
func (s *Service) GetFilterset(ctx context.Context, filterset string) (FiltersetDetails, error) {
	if filterset == "" || strings.Contains(filterset, "/") {
		return FiltersetDetails{}, fmt.Errorf("filterset must be one exact filterset name or filterset ID")
	}
	if err := validateFilterValue("filterset", filterset); err != nil {
		return FiltersetDetails{}, err
	}
	var found collectorList[collectorFilterset]
	err := s.client.GetJSON(ctx, "/filtersets/"+url.PathEscape(filterset), url.Values{"props": {strings.Join(filtersetProps, ",")}}, &found)
	if err != nil && !isNotFound(err) {
		return FiltersetDetails{}, fmt.Errorf("get filterset %s: %w", filterset, err)
	}
	if err != nil || len(found.Data) != 1 {
		return FiltersetDetails{}, fmt.Errorf("get filterset %s: not found in the collector; list the filtersets with list_filtersets", filterset)
	}
	row := found.Data[0]
	path := "/filtersets/" + strconv.Itoa(row.ID)
	result := FiltersetDetails{Filterset: filtersetOf(row), Criteria: []FiltersetCriterion{}}

	var export collectorFiltersetExport
	if err := s.client.GetJSON(ctx, path+"/export", nil, &export); err != nil {
		return FiltersetDetails{}, fmt.Errorf("export filterset %s: %w", row.Name, err)
	}
	criteria := map[string][]FiltersetCriterion{}
	for _, item := range export.Filtersets {
		filters := slices.Clone(item.Filters)
		sort.SliceStable(filters, func(i, j int) bool { return filters[i].Order < filters[j].Order })
		list := []FiltersetCriterion{}
		for _, filter := range filters {
			criterion := FiltersetCriterion{Logic: filter.LogicOp}
			switch {
			case filter.Filter != nil:
				criterion.Filter = filter.Filter.Table + "." + filter.Filter.Field + " " + filter.Filter.Op + " " + filter.Filter.Value
			case filter.Filterset != nil:
				criterion.Filterset = *filter.Filterset
			default:
				continue
			}
			list = append(list, criterion)
		}
		criteria[item.Name] = list
	}
	own, ok := criteria[row.Name]
	if !ok {
		return FiltersetDetails{}, fmt.Errorf("export filterset %s: unexpected collector response", row.Name)
	}
	result.Criteria = own
	// The included filtersets, at any depth, each once.
	seen := map[string]bool{row.Name: true}
	var queue []string
	for _, criterion := range own {
		queue = append(queue, criterion.Filterset)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		list, ok := criteria[name]
		if !ok {
			continue
		}
		result.Included = append(result.Included, IncludedFilterset{Name: name, Criteria: list})
		for _, criterion := range list {
			queue = append(queue, criterion.Filterset)
		}
	}

	// A filterset without criterion selects nothing; on dev2 the collector
	// answers its members with a 500, cannot resolve filterset.
	if len(result.Criteria) > 0 {
		if result.NodesCount, result.Nodes, err = s.attachedNames(ctx, path+"/nodes", "nodename", "nodename,node_id", false); err != nil {
			return FiltersetDetails{}, fmt.Errorf("list the nodes of filterset %s: %w", row.Name, err)
		}
		if result.ServicesCount, result.Services, err = s.attachedNames(ctx, path+"/services", "svcname", "svcname,svc_id", false); err != nil {
			return FiltersetDetails{}, fmt.Errorf("list the services of filterset %s: %w", row.Name, err)
		}
	}

	var usage struct {
		Data struct {
			Comparisons     []any                `json:"comparisons"`
			Filtersets      []collectorFilterset `json:"filtersets"`
			Rulesets        []collectorRuleset   `json:"rulesets"`
			SysreportGrants []any                `json:"sysreport_grants"`
			Thresholds      []any                `json:"thresholds"`
			Users           []any                `json:"users"`
		} `json:"data"`
	}
	if err := s.client.GetJSON(ctx, path+"/usage", nil, &usage); err != nil {
		return FiltersetDetails{}, fmt.Errorf("get usage of filterset %s: %w", row.Name, err)
	}
	for _, item := range usage.Data.Rulesets {
		result.Rulesets = append(result.Rulesets, item.Name)
	}
	for _, item := range usage.Data.Filtersets {
		result.IncludedBy = append(result.IncludedBy, item.Name)
	}
	// The usage names the users whatever the caller may see of them, with
	// their email address: only their number is given.
	result.SessionFilterOf = len(usage.Data.Users)
	result.Thresholds, result.Comparisons, result.SysreportGrants = len(usage.Data.Thresholds), len(usage.Data.Comparisons), len(usage.Data.SysreportGrants)
	result.Provenance = s.newProvenance()
	return result, nil
}

func filtersetOf(row collectorFilterset) Filterset {
	return Filterset{ID: row.ID, Name: row.Name, Author: strings.TrimSpace(row.Author), Updated: row.Updated}
}

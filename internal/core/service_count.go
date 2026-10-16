package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// serviceGroupProps are the groupings of count_services and their collector
// prop.
var serviceGroupProps = map[string]string{
	"cluster_id":  "cluster_id",
	"app":         "svc_app",
	"env":         "svc_env",
	"topology":    "svc_topology",
	"availstatus": "svc_availstatus",
	"status":      "svc_status",
	"frozen":      "svc_frozen",
	"placement":   "svc_placement",
	"provisioned": "svc_provisioned",
}

// ServiceGroupings lists the groupings of count_services, sorted.
func ServiceGroupings() []string {
	names := make([]string, 0, len(serviceGroupProps))
	for name := range serviceGroupProps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type CountServicesOptions struct {
	ServiceFilters
	GroupBy string
	Limit   int
}

type ServiceCount struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Tag        *TagReference `json:"tag,omitempty" jsonschema:"the tag the tag filter resolved to; only the services holding it are counted"`
	Total      int           `json:"total" jsonschema:"the number of services matching the filters, as the collector counts them"`
	GroupBy    string        `json:"group_by,omitempty" jsonschema:"the grouping asked"`
	Groups     []CountGroup  `json:"groups,omitempty" jsonschema:"with group_by, the most frequent values first, and the number of matching services with each"`
	Distinct   int           `json:"distinct,omitempty" jsonschema:"with group_by, the number of distinct values among the matching services"`
	Other      int           `json:"other,omitempty" jsonschema:"with group_by, the number of matching services whose value is not in groups, left out by limit"`
	Truncated  bool          `json:"truncated" jsonschema:"whether values were left out by limit"`
}

// CountServices counts the services matching the filters and, with a
// grouping, the services of each value, as the collector computes them.
func (s *Service) CountServices(ctx context.Context, options CountServicesOptions) (ServiceCount, error) {
	if err := options.ServiceFilters.validate(); err != nil {
		return ServiceCount{}, err
	}
	prop := ""
	if options.GroupBy != "" {
		var ok bool
		if prop, ok = serviceGroupProps[options.GroupBy]; !ok {
			return ServiceCount{}, fmt.Errorf("group_by must be one of %s", strings.Join(ServiceGroupings(), ", "))
		}
	}
	limit, err := pageLimit(options.Limit, defaultCountGroups, maxCountGroups)
	if err != nil {
		return ServiceCount{}, err
	}
	path, tag, err := s.taggedPath(ctx, options.Tag, "services")
	if err != nil {
		return ServiceCount{}, err
	}
	total, groups, distinct, other, err := s.countRows(ctx, path, "svc_id", options.ServiceFilters.collectorFilters(""), prop, limit)
	if err != nil {
		return ServiceCount{}, fmt.Errorf("count services: %w", err)
	}
	return ServiceCount{
		Provenance: s.newProvenance(), Tag: tag, Total: total, GroupBy: options.GroupBy, Groups: groups,
		Distinct: distinct, Other: other, Truncated: other > 0,
	}, nil
}

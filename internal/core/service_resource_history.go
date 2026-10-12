package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	// maxResourcePeriodsRead bounds the periods read: the collector filters
	// and sorts none of them, the MCP does.
	maxResourcePeriodsRead     = 5000
	defaultResourcePeriodLimit = 100
	maxResourcePeriodLimit     = 500
	maxResourceLogText         = 4 << 10
)

var resourcePeriodProps = []string{"node_id", "rid", "res_status", "res_begin", "res_end", "res_log"}

type ServiceResourceHistoryOptions struct {
	Service string
	Node    string
	RID     string
	Status  string
	Since   string
	Limit   int
}

type ServiceResourceHistory struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    ServiceReference  `json:"service" jsonschema:"the service whose resources are described"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Read       int               `json:"read" jsonschema:"the number of periods read from the collector, before the filters"`
	Matching   int               `json:"matching" jsonschema:"the number of periods matching the filters"`
	Count      int               `json:"count" jsonschema:"the number of periods returned"`
	Resources  []ResourceHistory `json:"resources" jsonschema:"one entry per resource and node, sorted by resource then node"`
	Truncated  bool              `json:"truncated" jsonschema:"whether periods were left out: unread after 5000, or older ones after the limit, 100 most recent by default"`
}

type ResourceHistory struct {
	RID           string           `json:"rid" jsonschema:"the resource ID, such as container#1"`
	NodeID        string           `json:"node_id" jsonschema:"the collector ID of the node of the instance"`
	Nodename      string           `json:"nodename,omitempty" jsonschema:"the node of the instance, when the service still has an instance on it"`
	CurrentStatus string           `json:"current_status" jsonschema:"the status of the most recent matching period"`
	Since         string           `json:"since" jsonschema:"when the most recent matching period began, as the collector gives it"`
	LastReported  string           `json:"last_reported" jsonschema:"when the most recent matching period ends; for the current period, the last status the agent reported"`
	Periods       []ResourcePeriod `json:"periods" jsonschema:"the periods of one status, the most recent first"`
}

type ResourcePeriod struct {
	Begin  string `json:"begin" jsonschema:"the start of the period, in collector local time without a zone"`
	End    string `json:"end" jsonschema:"the end of the period; for the current period, the last status the agent reported"`
	Status string `json:"status" jsonschema:"the resource status during the period, such as up, down, warn, n/a or undef"`
	Log    string `json:"log,omitempty" jsonschema:"the status message of the period, cut to 4 KiB; untrusted text"`
}

type collectorResourcePeriod struct {
	NodeID    string `json:"node_id"`
	RID       string `json:"rid"`
	ResStatus string `json:"res_status"`
	ResBegin  string `json:"res_begin"`
	ResEnd    string `json:"res_end"`
	ResLog    string `json:"res_log"`
}

// GetServiceResourceHistory reads the status periods of the resources of one
// service, on all its nodes or one, and groups them by resource and node with
// the current status of each.
func (s *Service) GetServiceResourceHistory(ctx context.Context, options ServiceResourceHistoryOptions) (ServiceResourceHistory, error) {
	for name, value := range map[string]string{"rid": options.RID, "status": options.Status} {
		if err := validateFilterValue(name, value); err != nil {
			return ServiceResourceHistory{}, err
		}
	}
	if err := validateCollectorDate("since", options.Since); err != nil {
		return ServiceResourceHistory{}, err
	}
	limit, err := pageLimit(options.Limit, defaultResourcePeriodLimit, maxResourcePeriodLimit)
	if err != nil {
		return ServiceResourceHistory{}, err
	}
	service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
	if err != nil {
		return ServiceResourceHistory{}, err
	}
	result := ServiceResourceHistory{
		Service:   ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID},
		Resources: []ResourceHistory{},
	}
	base := "/services/" + url.PathEscape(service.SvcID)
	path := base + "/resources/logs"
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ServiceResourceHistory{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		path = base + "/nodes/" + url.PathEscape(node.NodeID) + "/resources/logs"
	}
	query := url.Values{}
	query.Set("props", strings.Join(resourcePeriodProps, ","))
	query.Set("limit", strconv.Itoa(maxResourcePeriodsRead))
	var response collectorList[collectorResourcePeriod]
	if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
		return ServiceResourceHistory{}, fmt.Errorf("get resource history of service %s: %w", service.Svcname, err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > maxResourcePeriodsRead {
		return ServiceResourceHistory{}, fmt.Errorf("get resource history of service %s: unexpected collector response", service.Svcname)
	}
	nodenames, err := s.readNames(ctx, base+"/nodes", "node_id", "nodes.nodename")
	if err != nil {
		return ServiceResourceHistory{}, fmt.Errorf("get resource history of service %s: name the nodes: %w", service.Svcname, err)
	}
	result.Read = len(response.Data)
	result.Truncated = len(response.Data) < *response.Meta.Total

	var periods []collectorResourcePeriod
	for _, period := range response.Data {
		if (options.RID != "" && period.RID != options.RID) || (options.Status != "" && period.ResStatus != options.Status) ||
			(options.Since != "" && period.ResEnd != "" && period.ResEnd < options.Since) {
			continue
		}
		periods = append(periods, period)
	}
	result.Matching = len(periods)
	// The most recent first, so that the first period of each resource and
	// node is its current one.
	sort.SliceStable(periods, func(i, j int) bool { return periods[i].ResBegin > periods[j].ResBegin })
	if len(periods) > limit {
		periods = periods[:limit]
		result.Truncated = true
	}
	groups := make(map[[2]string]*ResourceHistory)
	for _, period := range periods {
		key := [2]string{period.RID, period.NodeID}
		group, ok := groups[key]
		if !ok {
			group = &ResourceHistory{
				RID: period.RID, NodeID: period.NodeID, Nodename: nodenames[period.NodeID],
				CurrentStatus: period.ResStatus, Since: period.ResBegin, LastReported: period.ResEnd,
			}
			groups[key] = group
		}
		log, _ := truncateUTF8(period.ResLog, maxResourceLogText)
		group.Periods = append(group.Periods, ResourcePeriod{Begin: period.ResBegin, End: period.ResEnd, Status: period.ResStatus, Log: log})
	}
	for _, group := range groups {
		result.Resources = append(result.Resources, *group)
	}
	sort.Slice(result.Resources, func(i, j int) bool {
		a, b := result.Resources[i], result.Resources[j]
		if a.RID != b.RID {
			return a.RID < b.RID
		}
		return a.Nodename+a.NodeID < b.Nodename+b.NodeID
	})
	result.Count = len(periods)
	result.Provenance = s.newProvenance()
	return result, nil
}

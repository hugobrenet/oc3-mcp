package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
)

const (
	defaultListInstancesLimit = 100
	maxListInstancesLimit     = 200
)

var instanceProps = []string{
	"svc_id", "services.svcname", "services.cluster_id", "services.svc_topology", "services.svc_availstatus", "services.svc_frozen",
	"node_id", "nodes.nodename", "mon_availstatus", "mon_overallstatus", "mon_frozen", "mon_frozen_at", "mon_smon_status",
	"mon_vmname", "mon_vmtype", "mon_updated",
}

// instanceGroupProps are the groupings of count_instances and their
// collector prop.
var instanceGroupProps = map[string]string{
	"node":                 "nodes.nodename",
	"service":              "services.svcname",
	"cluster_id":           "services.cluster_id",
	"app":                  "services.svc_app",
	"availability":         "mon_availstatus",
	"overall":              "mon_overallstatus",
	"topology":             "services.svc_topology",
	"service_availability": "services.svc_availstatus",
	"frozen":               "mon_frozen",
}

// InstanceGroupings lists the groupings of count_instances, sorted.
func InstanceGroupings() []string {
	names := make([]string, 0, len(instanceGroupProps))
	for name := range instanceGroupProps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InstanceFilters are the typed filters of list_instances and
// count_instances. Node and Service are resolved to their IDs first.
type InstanceFilters struct {
	Node                string
	Service             string
	App                 string
	Availability        string
	Overall             string
	ServiceAvailability string
	Topology            string
	Frozen              bool
	FrozenBefore        string
}

type ListInstancesOptions struct {
	InstanceFilters
	Limit  int
	Cursor string
}

type InstanceList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of instances matching the filters, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of instances returned in this page"`
	Instances  []Instance        `json:"instances" jsonschema:"the instances sorted by node then service, or with frozen the longest frozen first"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching instances remain after this page"`
}

type Instance struct {
	SvcID               string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname             string `json:"svcname,omitempty" jsonschema:"the service name"`
	ClusterID           string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
	Topology            string `json:"topology,omitempty" jsonschema:"the service topology: failover, running on one node at a time, or flex"`
	ServiceAvailability string `json:"service_availability,omitempty" jsonschema:"the aggregated availability status of the service over all its instances; an instance down of a failover service up is a passive instance, as expected"`
	ServiceFrozen       string `json:"service_frozen,omitempty" jsonschema:"the aggregated freeze state of the service: frozen, unfrozen, mixed or n/a"`
	NodeID              string `json:"node_id" jsonschema:"the collector ID of the node of the instance"`
	Nodename            string `json:"nodename,omitempty" jsonschema:"the node of the instance"`
	Availability        string `json:"availability,omitempty" jsonschema:"the availability status of the instance, such as up, down, warn, stdby up, stdby down or n/a"`
	Overall             string `json:"overall,omitempty" jsonschema:"the overall status of the instance, its availability with the warnings of its resources"`
	Frozen              bool   `json:"frozen" jsonschema:"whether the instance is frozen: its monitor runs no automatic action"`
	FrozenAt            string `json:"frozen_at,omitempty" jsonschema:"when the instance was frozen, as the collector gives it"`
	MonitorStatus       string `json:"monitor_status,omitempty" jsonschema:"the state of the monitor of the instance, such as idle"`
	VMName              string `json:"vmname,omitempty" jsonschema:"the container or virtual machine of an encapsulated instance"`
	VMType              string `json:"vmtype,omitempty" jsonschema:"the container or virtual machine type, such as docker or kvm"`
	LastReportedAt      string `json:"last_reported_at,omitempty" jsonschema:"when the agent last reported the instance, as the collector gives it"`
}

type collectorServiceInstance struct {
	SvcID          string `json:"svc_id"`
	Svcname        string `json:"services.svcname"`
	ClusterID      string `json:"services.cluster_id"`
	Topology       string `json:"services.svc_topology"`
	SvcAvailStatus string `json:"services.svc_availstatus"`
	SvcFrozen      string `json:"services.svc_frozen"`
	NodeID         string `json:"node_id"`
	Nodename       string `json:"nodes.nodename"`
	MonAvailStatus string `json:"mon_availstatus"`
	MonOverall     string `json:"mon_overallstatus"`
	MonFrozen      string `json:"mon_frozen"`
	MonFrozenAt    string `json:"mon_frozen_at"`
	MonSmonStatus  string `json:"mon_smon_status"`
	MonVMName      string `json:"mon_vmname"`
	MonVMType      string `json:"mon_vmtype"`
	MonUpdated     string `json:"mon_updated"`
}

// instanceFilters validates the filters, resolves the node and the service,
// and translates them into collector filters.
func (s *Service) instanceFilters(ctx context.Context, f InstanceFilters) ([]string, *ServiceReference, *NodeReference, error) {
	for name, value := range map[string]string{"app": f.App, "availability": f.Availability, "overall": f.Overall, "service_availability": f.ServiceAvailability} {
		if err := validateFilterValue(name, value); err != nil {
			return nil, nil, nil, err
		}
	}
	if f.Topology != "" && !slices.Contains([]string{"failover", "flex"}, f.Topology) {
		return nil, nil, nil, fmt.Errorf("topology must be one of failover, flex")
	}
	if err := validateCollectorDate("frozen_before", f.FrozenBefore); err != nil {
		return nil, nil, nil, err
	}
	var filters []string
	if f.Frozen || f.FrozenBefore != "" {
		filters = append(filters, "mon_frozen:ne:0")
	}
	var service *ServiceReference
	if f.Service != "" {
		found, err := s.lookupService(ctx, f.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return nil, nil, nil, err
		}
		service = &ServiceReference{SvcID: found.SvcID, Svcname: found.Svcname, ClusterID: found.ClusterID}
		filters = append(filters, filterEquals("svc_id", found.SvcID))
	}
	var node *NodeReference
	if f.Node != "" {
		found, err := s.lookupNode(ctx, f.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return nil, nil, nil, err
		}
		node = &NodeReference{NodeID: found.NodeID, Nodename: found.Nodename, ClusterID: found.ClusterID}
		filters = append(filters, filterEquals("node_id", found.NodeID))
	}
	for _, filter := range [][2]string{
		{"services.svc_app", f.App}, {"mon_availstatus", f.Availability}, {"mon_overallstatus", f.Overall},
		{"services.svc_availstatus", f.ServiceAvailability}, {"services.svc_topology", f.Topology},
	} {
		if filter[1] != "" {
			filters = append(filters, filterEquals(filter[0], filter[1]))
		}
	}
	if f.FrozenBefore != "" {
		filters = append(filters, "mon_frozen_at:lte:"+f.FrozenBefore)
	}
	return filters, service, node, nil
}

// ListInstances reads one page of the service instances, one per service
// and node, and per container of an encapsulated service, as their agents
// last reported them, with typed filters. Frozen instances come the longest
// frozen first. The container completes the order: the collector holds one
// row per container of a service on a node.
func (s *Service) ListInstances(ctx context.Context, options ListInstancesOptions) (InstanceList, error) {
	limit, err := pageLimit(options.Limit, defaultListInstancesLimit, maxListInstancesLimit)
	if err != nil {
		return InstanceList{}, err
	}
	filters, service, node, err := s.instanceFilters(ctx, options.InstanceFilters)
	if err != nil {
		return InstanceList{}, err
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return InstanceList{}, err
	}
	orderby := "nodes.nodename,services.svcname,svc_id,node_id,mon_vmname"
	if options.Frozen || options.FrozenBefore != "" {
		orderby = "mon_frozen_at,svc_id,node_id,mon_vmname"
	}

	var response collectorList[collectorServiceInstance]
	if err := s.client.GetJSON(ctx, "/services_instances", pageQuery(instanceProps, orderby, filters, limit, offset), &response); err != nil {
		return InstanceList{}, fmt.Errorf("list instances: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return InstanceList{}, fmt.Errorf("list instances: unexpected collector response")
	}
	result := InstanceList{
		Provenance: s.newProvenance(), Service: service, Node: node,
		Total: *response.Meta.Total, Count: len(response.Data), Instances: make([]Instance, 0, len(response.Data)),
	}
	for _, instance := range response.Data {
		result.Instances = append(result.Instances, Instance{
			SvcID: instance.SvcID, Svcname: instance.Svcname, ClusterID: instance.ClusterID, Topology: instance.Topology,
			ServiceAvailability: instance.SvcAvailStatus, ServiceFrozen: instance.SvcFrozen, NodeID: instance.NodeID, Nodename: instance.Nodename,
			Availability: instance.MonAvailStatus, Overall: instance.MonOverall, Frozen: instanceFrozen(instance.MonFrozen),
			FrozenAt: collectorDate(instance.MonFrozenAt), MonitorStatus: instance.MonSmonStatus, VMName: instance.MonVMName, VMType: instance.MonVMType,
			LastReportedAt: instance.MonUpdated,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// instanceFrozen reads the mon_frozen flag of an instance, 0 when it is not
// frozen.
func instanceFrozen(value string) bool {
	return value != "" && value != "0"
}

type CountInstancesOptions struct {
	InstanceFilters
	GroupBy string
	Limit   int
}

type InstanceCount struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of instances matching the filters, as the collector counts them"`
	GroupBy    string            `json:"group_by,omitempty" jsonschema:"the grouping asked"`
	Groups     []CountGroup      `json:"groups,omitempty" jsonschema:"with group_by, the most frequent values first, and the number of matching instances with each; frozen groups by frozen and unfrozen"`
	Distinct   int               `json:"distinct,omitempty" jsonschema:"with group_by, the number of distinct values among the matching instances"`
	Other      int               `json:"other,omitempty" jsonschema:"with group_by, the number of matching instances whose value is not in groups, left out by limit"`
	Truncated  bool              `json:"truncated" jsonschema:"whether values were left out by limit"`
}

// CountInstances counts the service instances matching the filters and,
// with a grouping, the instances of each value, as the collector computes
// them.
func (s *Service) CountInstances(ctx context.Context, options CountInstancesOptions) (InstanceCount, error) {
	prop := ""
	if options.GroupBy != "" {
		var ok bool
		if prop, ok = instanceGroupProps[options.GroupBy]; !ok {
			return InstanceCount{}, fmt.Errorf("group_by must be one of %s", strings.Join(InstanceGroupings(), ", "))
		}
	}
	limit, err := pageLimit(options.Limit, defaultCountGroups, maxCountGroups)
	if err != nil {
		return InstanceCount{}, err
	}
	filters, service, node, err := s.instanceFilters(ctx, options.InstanceFilters)
	if err != nil {
		return InstanceCount{}, err
	}
	total, groups, distinct, other, err := s.countRows(ctx, "/services_instances", "svc_id", filters, prop, limit)
	if err != nil {
		return InstanceCount{}, fmt.Errorf("count instances: %w", err)
	}
	if options.GroupBy == "frozen" {
		groups = frozenGroups(groups)
		distinct = len(groups)
	}
	return InstanceCount{
		Provenance: s.newProvenance(), Service: service, Node: node, Total: total, GroupBy: options.GroupBy, Groups: groups,
		Distinct: distinct, Other: other, Truncated: other > 0,
	}, nil
}

// frozenGroups names the mon_frozen flags: 0 is unfrozen, any other flag
// frozen.
func frozenGroups(groups []CountGroup) []CountGroup {
	counts := map[string]int{}
	for _, group := range groups {
		name := "frozen"
		if group.Value == "0" || group.Value == "" {
			name = "unfrozen"
		}
		counts[name] += group.Count
	}
	merged := make([]CountGroup, 0, len(counts))
	for value, count := range counts {
		merged = append(merged, CountGroup{Value: value, Count: count})
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Count != merged[j].Count {
			return merged[i].Count > merged[j].Count
		}
		return merged[i].Value < merged[j].Value
	})
	return merged
}

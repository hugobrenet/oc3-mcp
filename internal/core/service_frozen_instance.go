package core

import (
	"context"
	"fmt"
	"net/url"
)

const (
	defaultListFrozenInstancesLimit = 100
	maxListFrozenInstancesLimit     = 200
)

var frozenInstanceProps = []string{
	"svc_id", "services.svcname", "services.cluster_id", "services.svc_frozen", "node_id", "nodes.nodename",
	"mon_frozen_at", "mon_availstatus", "mon_overallstatus",
}

type ListFrozenInstancesOptions struct {
	FrozenBefore string
	Service      string
	Node         string
	Limit        int
	Cursor       string
}

type FrozenInstanceList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of frozen instances matching the filters, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of frozen instances returned in this page"`
	Instances  []FrozenInstance  `json:"instances" jsonschema:"the frozen instances, the longest frozen first"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching frozen instances remain after this page"`
}

type FrozenInstance struct {
	SvcID         string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname       string `json:"svcname,omitempty" jsonschema:"the service name"`
	ClusterID     string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
	NodeID        string `json:"node_id" jsonschema:"the collector ID of the node of the instance"`
	Nodename      string `json:"nodename,omitempty" jsonschema:"the node of the instance"`
	FrozenAt      string `json:"frozen_at" jsonschema:"when the instance was frozen, as the collector gives it"`
	Avail         string `json:"avail,omitempty" jsonschema:"the availability status of the instance"`
	Overall       string `json:"overall,omitempty" jsonschema:"the overall status of the instance"`
	ServiceFrozen string `json:"service_frozen,omitempty" jsonschema:"the aggregated freeze state of the service: frozen, unfrozen, mixed or n/a"`
}

type collectorFrozenInstance struct {
	SvcID          string `json:"svc_id"`
	Svcname        string `json:"services.svcname"`
	ClusterID      string `json:"services.cluster_id"`
	SvcFrozen      string `json:"services.svc_frozen"`
	NodeID         string `json:"node_id"`
	Nodename       string `json:"nodes.nodename"`
	MonFrozenAt    string `json:"mon_frozen_at"`
	MonAvailStatus string `json:"mon_availstatus"`
	MonOverall     string `json:"mon_overallstatus"`
}

// ListFrozenInstances reads one page of the frozen service instances, the
// longest frozen first. The instances carry the freeze date the collector
// stores; the aggregated service state has none.
func (s *Service) ListFrozenInstances(ctx context.Context, options ListFrozenInstancesOptions) (FrozenInstanceList, error) {
	if err := validateCollectorDate("frozen_before", options.FrozenBefore); err != nil {
		return FrozenInstanceList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListFrozenInstancesLimit, maxListFrozenInstancesLimit)
	if err != nil {
		return FrozenInstanceList{}, err
	}
	result := FrozenInstanceList{}
	filters := []string{"mon_frozen:ne:0"}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return FrozenInstanceList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		filters = append(filters, filterEquals("svc_id", service.SvcID))
	}
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return FrozenInstanceList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	if options.FrozenBefore != "" {
		filters = append(filters, "mon_frozen_at:lte:"+options.FrozenBefore)
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return FrozenInstanceList{}, err
	}

	var response collectorList[collectorFrozenInstance]
	if err := s.client.GetJSON(ctx, "/services_instances", pageQuery(frozenInstanceProps, "mon_frozen_at,svc_id,node_id", filters, limit, offset), &response); err != nil {
		return FrozenInstanceList{}, fmt.Errorf("list frozen instances: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return FrozenInstanceList{}, fmt.Errorf("list frozen instances: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Instances = make([]FrozenInstance, 0, len(response.Data))
	for _, instance := range response.Data {
		result.Instances = append(result.Instances, FrozenInstance{
			SvcID: instance.SvcID, Svcname: instance.Svcname, ClusterID: instance.ClusterID, NodeID: instance.NodeID,
			Nodename: instance.Nodename, FrozenAt: instance.MonFrozenAt, Avail: instance.MonAvailStatus,
			Overall: instance.MonOverall, ServiceFrozen: instance.SvcFrozen,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

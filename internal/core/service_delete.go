package core

import (
	"context"
	"fmt"
	"net/url"
)

// reportedWithin is how recent an instance report makes a deletion warn that
// the collector will recreate what it deletes: an agent still reporting it
// sends it at its next status. The collector computes it, as its dates have
// no zone: every instance an agent reports is updated at each of its
// statuses. The deletion is not refused: the user confirms it.
const reportedWithin = "-1h"

// recreatedWarning tells why a deleted service or instance comes back.
const recreatedWarning = "an agent reported it within the hour: the collector recreates it at the next status of its cluster, with the same ID and without the history deleted; delete the object on its cluster to remove it for good"

type ServiceDeletion struct {
	Provenance Provenance       `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    ServiceReference `json:"service" jsonschema:"the service deleted, as it was"`
	App        string           `json:"app,omitempty" jsonschema:"the app of the service"`
	Instances  int              `json:"instances" jsonschema:"the number of instances the service had in the collector, deleted with it"`
	Recent     int              `json:"recently_reported" jsonschema:"the number of these instances an agent reported within the hour"`
	Deleted    bool             `json:"deleted" jsonschema:"true: the collector deleted the service, its instances and its history"`
	Warning    string           `json:"warning,omitempty" jsonschema:"when an instance was reported within the hour, why the service comes back; tell the user"`
}

type ServiceInstanceDeletion struct {
	Provenance Provenance       `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    ServiceReference `json:"service" jsonschema:"the service of the instance"`
	Node       NodeReference    `json:"node" jsonschema:"the node of the instance"`
	Updated    string           `json:"updated,omitempty" jsonschema:"when the collector last received the instance, in collector local time without a zone"`
	Deleted    bool             `json:"deleted" jsonschema:"true: the collector deleted the instance and its current state"`
	Warning    string           `json:"warning,omitempty" jsonschema:"when the instance was reported within the hour, why it comes back; tell the user"`
}

// DeleteService deletes one service and its history from the collector. A
// service an agent still reports comes back at the next status of its
// cluster, with the same ID and without history: the result warns of it.
func (s *Service) DeleteService(ctx context.Context, service string) (ServiceDeletion, error) {
	found, err := s.lookupService(ctx, service, []string{"svc_id", "svcname", "cluster_id", "svc_app"})
	if err != nil {
		return ServiceDeletion{}, err
	}
	reference := ServiceReference{SvcID: found.SvcID, Svcname: found.Svcname, ClusterID: found.ClusterID}
	instances, err := s.countInstances(ctx, []string{filterEquals("svc_id", found.SvcID)})
	if err != nil {
		return ServiceDeletion{}, err
	}
	recent, err := s.countInstances(ctx, []string{filterEquals("svc_id", found.SvcID), "mon_updated:gte:" + reportedWithin})
	if err != nil {
		return ServiceDeletion{}, err
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, "/services/"+url.PathEscape(found.SvcID), &response); err != nil {
		return ServiceDeletion{}, fmt.Errorf("delete service %s: %w", found.Svcname, err)
	}
	result := ServiceDeletion{Provenance: s.newProvenance(), Service: reference, App: found.SvcApp, Instances: instances, Recent: recent, Deleted: true}
	if recent > 0 {
		result.Warning = recreatedWarning
	}
	return result, nil
}

// DeleteServiceInstance deletes one instance of a service, on one node, from
// the collector. An instance its node still reports comes back at its next
// status: the result warns of it.
func (s *Service) DeleteServiceInstance(ctx context.Context, service, node string) (ServiceInstanceDeletion, error) {
	found, err := s.lookupService(ctx, service, []string{"svc_id", "svcname", "cluster_id"})
	if err != nil {
		return ServiceInstanceDeletion{}, err
	}
	foundNode, err := s.lookupNode(ctx, node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return ServiceInstanceDeletion{}, err
	}
	result := ServiceInstanceDeletion{
		Service: ServiceReference{SvcID: found.SvcID, Svcname: found.Svcname, ClusterID: found.ClusterID},
		Node:    NodeReference{NodeID: foundNode.NodeID, Nodename: foundNode.Nodename, ClusterID: foundNode.ClusterID},
	}
	filters := []string{filterEquals("svc_id", found.SvcID), filterEquals("node_id", foundNode.NodeID)}
	var instance collectorList[map[string]any]
	if err := s.client.GetJSON(ctx, "/services_instances", pageQuery([]string{"svc_id", "mon_updated"}, "svc_id", filters, 1, 0), &instance); err != nil {
		return ServiceInstanceDeletion{}, fmt.Errorf("get instance of %s on %s: %w", found.Svcname, foundNode.Nodename, err)
	}
	if len(instance.Data) == 0 {
		return ServiceInstanceDeletion{}, fmt.Errorf("service %s has no instance on node %s in the collector; nothing was deleted", found.Svcname, foundNode.Nodename)
	}
	result.Updated = propText(instance.Data[0], "mon_updated")
	recent, err := s.countInstances(ctx, append(filters, "mon_updated:gte:"+reportedWithin))
	if err != nil {
		return ServiceInstanceDeletion{}, err
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, "/services/"+url.PathEscape(found.SvcID)+"/instances/"+url.PathEscape(foundNode.NodeID), &response); err != nil {
		return ServiceInstanceDeletion{}, fmt.Errorf("delete instance of %s on %s: %w", found.Svcname, foundNode.Nodename, err)
	}
	result.Provenance, result.Deleted = s.newProvenance(), true
	if recent > 0 {
		result.Warning = recreatedWarning
	}
	return result, nil
}

// countInstances counts the service instances the filters select.
func (s *Service) countInstances(ctx context.Context, filters []string) (int, error) {
	var response collectorList[map[string]any]
	if err := s.client.GetJSON(ctx, "/services_instances", pageQuery([]string{"svc_id"}, "svc_id", filters, 1, 0), &response); err != nil {
		return 0, fmt.Errorf("count instances: %w", err)
	}
	if response.Meta.Total == nil {
		return 0, fmt.Errorf("count instances: unexpected collector response")
	}
	return *response.Meta.Total, nil
}

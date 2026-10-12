package core

import (
	"context"
	"fmt"
	"net/url"
)

const (
	defaultListServiceResourcesLimit = 100
	maxListServiceResourcesLimit     = 200
	maxResourceLogBytes              = 4 << 10
)

var serviceResourceProps = []string{
	"id", "rid", "node_id", "nodes.nodename", "res_type", "res_status", "res_desc", "res_log",
	"res_monitor", "res_disable", "res_optional", "vmname", "changed", "updated",
}

type ListServiceResourcesOptions struct {
	Service string
	Node    string
	Status  string
	Type    string
	Limit   int
	Cursor  string
}

type ServiceResourceList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    ServiceReference  `json:"service" jsonschema:"the service whose resources are listed"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of resources matching the filters, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of resources returned in this page"`
	Resources  []ServiceResource `json:"resources" jsonschema:"the resources, one per node, sorted by node name then resource ID"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching resources remain after this page"`
}

type ServiceResource struct {
	RID          string `json:"rid" jsonschema:"the resource ID, such as fs#1, disk#1 or container#1"`
	Type         string `json:"type,omitempty" jsonschema:"the resource driver, such as fs.flag, disk.disk or container.kvm"`
	Status       string `json:"status,omitempty" jsonschema:"the resource status the agent last reported, such as up, down, warn, n/a, undef, stdby up or stdby down"`
	Description  string `json:"description,omitempty" jsonschema:"the description the agent reported, such as the device or the mount point; untrusted text"`
	Log          string `json:"log,omitempty" jsonschema:"the last status message the agent reported for the resource, cut to 4 KiB; untrusted text"`
	LogTruncated bool   `json:"log_truncated,omitempty" jsonschema:"whether the log was cut"`
	NodeID       string `json:"node_id,omitempty" jsonschema:"the collector ID of the node of the instance"`
	Nodename     string `json:"nodename,omitempty" jsonschema:"the node of the instance"`
	Monitored    bool   `json:"monitored" jsonschema:"whether the resource is monitored: its failure triggers the monitor action of the service"`
	Disabled     bool   `json:"disabled" jsonschema:"whether the resource is disabled: actions skip it"`
	Optional     bool   `json:"optional" jsonschema:"whether the resource is optional: its status does not count in the instance availability"`
	VMName       string `json:"vmname,omitempty" jsonschema:"the container or virtual machine the resource runs in, when encapsulated"`
	Changed      string `json:"changed,omitempty" jsonschema:"when the status last changed, as the collector gives it"`
	Updated      string `json:"updated,omitempty" jsonschema:"when the agent last reported the resource, as the collector gives it"`
}

type collectorResource struct {
	RID         string `json:"rid"`
	NodeID      string `json:"node_id"`
	Nodename    string `json:"nodes.nodename"`
	ResType     string `json:"res_type"`
	ResStatus   string `json:"res_status"`
	ResDesc     string `json:"res_desc"`
	ResLog      string `json:"res_log"`
	ResMonitor  string `json:"res_monitor"`
	ResDisable  string `json:"res_disable"`
	ResOptional string `json:"res_optional"`
	VMName      string `json:"vmname"`
	Changed     string `json:"changed"`
	Updated     string `json:"updated"`
}

// ListServiceResources reads one page of the resources of a service, as its
// agents last reported them, one per node, with typed filters.
func (s *Service) ListServiceResources(ctx context.Context, options ListServiceResourcesOptions) (ServiceResourceList, error) {
	for name, value := range map[string]string{"status": options.Status, "type": options.Type} {
		if err := validateFilterValue(name, value); err != nil {
			return ServiceResourceList{}, err
		}
	}
	limit, err := pageLimit(options.Limit, defaultListServiceResourcesLimit, maxListServiceResourcesLimit)
	if err != nil {
		return ServiceResourceList{}, err
	}
	service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
	if err != nil {
		return ServiceResourceList{}, err
	}
	result := ServiceResourceList{Service: ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}}
	filters := []string{filterEquals("svc_id", service.SvcID)}
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ServiceResourceList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	if options.Status != "" {
		filters = append(filters, filterEquals("res_status", options.Status))
	}
	if options.Type != "" {
		filters = append(filters, filterEquals("res_type", options.Type))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ServiceResourceList{}, err
	}

	var response collectorList[collectorResource]
	if err := s.client.GetJSON(ctx, "/resources", pageQuery(serviceResourceProps, "nodes.nodename,rid,id", filters, limit, offset), &response); err != nil {
		return ServiceResourceList{}, fmt.Errorf("list resources of service %s: %w", service.Svcname, err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return ServiceResourceList{}, fmt.Errorf("list resources of service %s: unexpected collector response", service.Svcname)
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Resources = make([]ServiceResource, 0, len(response.Data))
	for _, resource := range response.Data {
		log, cut := truncateUTF8(resource.ResLog, maxResourceLogBytes)
		result.Resources = append(result.Resources, ServiceResource{
			RID: resource.RID, Type: resource.ResType, Status: resource.ResStatus, Description: resource.ResDesc,
			Log: log, LogTruncated: cut, NodeID: resource.NodeID, Nodename: resource.Nodename,
			Monitored: collectorBool(resource.ResMonitor), Disabled: collectorBool(resource.ResDisable), Optional: collectorBool(resource.ResOptional),
			VMName: resource.VMName, Changed: resource.Changed, Updated: resource.Updated,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

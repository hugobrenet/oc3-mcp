package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultListServicesLimit = 50
	maxListServicesLimit     = 200
	maxServiceInstances      = 200
)

// serviceSummaryProps are the service columns of a summary, read directly
// from /services or joined as "services.<prop>" from /services_instances.
var serviceSummaryProps = []string{
	"svc_id", "svcname", "cluster_id", "svc_app", "svc_env", "svc_topology", "svc_availstatus", "svc_status",
	"svc_frozen", "svc_placement", "svc_provisioned", "svc_nodes", "svc_status_updated",
}

var serviceDetailProps = []string{
	"svc_id", "svcname", "cluster_id", "svc_app", "svc_env", "svc_comment", "svc_created", "updated", "svc_snooze_till",
	"svc_topology", "svc_ha", "svc_autostart", "svc_nodes", "svc_drpnode", "svc_drpnodes", "svc_drptype",
	"svc_flex_min_nodes", "svc_flex_max_nodes", "svc_flex_target",
	"svc_status", "svc_availstatus", "svc_frozen", "svc_placement", "svc_provisioned", "svc_status_updated",
	"svc_availability", "svc_sla", "svc_availability_updated", "svc_config", "svc_config_updated",
}

// maxServiceConfig bounds the configuration text of a service.
const maxServiceConfig = 32 << 10

var serviceInstanceProps = []string{
	"node_id", "nodes.nodename", "mon_availstatus", "mon_overallstatus", "mon_smon_status", "mon_smon_global_expect",
	"mon_frozen", "mon_frozen_at", "mon_updated",
	"mon_ipstatus", "mon_fsstatus", "mon_diskstatus", "mon_containerstatus", "mon_sharestatus", "mon_syncstatus",
	"mon_appstatus", "mon_hbstatus",
}

// ServiceFilters are the typed filters of list_services and count_services.
type ServiceFilters struct {
	Name        string
	ClusterID   string
	App         string
	Env         string
	AvailStatus string
	Status      string
	Topology    string
	Frozen      string
	Placement   string
	// Tag keeps the services holding this tag, by name or tag ID.
	Tag string
	// Filterset keeps the services this filterset selects, by name or ID.
	Filterset string
	// Ruleset keeps the services an explicit ruleset is attached to, by
	// name or ID.
	Ruleset string
	// Moduleset keeps the services a moduleset is attached to, by name or
	// ID.
	Moduleset string
}

// ServiceMembership are the tag, filterset, ruleset or moduleset a service
// filter resolved to: at most one.
type ServiceMembership struct {
	Tag       *TagReference                 `json:"tag,omitempty" jsonschema:"the tag the tag filter resolved to; only the services holding it are kept"`
	Filterset *FiltersetReference           `json:"filterset,omitempty" jsonschema:"the filterset the filterset filter resolved to; only the services it selects are kept"`
	Ruleset   *ComplianceRulesetReference   `json:"ruleset,omitempty" jsonschema:"the ruleset the ruleset filter resolved to; only the services it is attached to are kept"`
	Moduleset *ComplianceModulesetReference `json:"moduleset,omitempty" jsonschema:"the moduleset the moduleset filter resolved to; only the services it is attached to are kept"`
}

// serviceMemberPath is the collector list of the services, or with a tag,
// filterset, ruleset or moduleset filter, the list of its services, which
// takes the other filters too. The attachments listed are those of the
// services themselves, not of their encapsulated services.
func (s *Service) serviceMemberPath(ctx context.Context, f ServiceFilters) (string, ServiceMembership, error) {
	path, found, err := s.memberPath(ctx, "services", f.Tag, f.Filterset, f.Ruleset, f.Moduleset)
	return path, ServiceMembership(found), err
}

// member tells whether a tag, filterset, ruleset or moduleset filter is set.
func (f ServiceFilters) member() bool {
	return f.Tag != "" || f.Filterset != "" || f.Ruleset != "" || f.Moduleset != ""
}

// errNodeWithMember refuses the node filter with a member filter: the
// services of a node are read from its instances.
var errNodeWithMember = fmt.Errorf("pass node or one of tag, filterset, ruleset and moduleset, not both")

type ListServicesOptions struct {
	ServiceFilters
	Node   string
	Limit  int
	Cursor string
}

type ServiceList struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference `json:"node,omitempty" jsonschema:"the node the node filter resolved to; only services with an instance on it are listed"`
	ServiceMembership
	Total      int              `json:"total" jsonschema:"the number of services matching the filters, as the collector counts them"`
	Count      int              `json:"count" jsonschema:"the number of services returned in this page"`
	Services   []ServiceSummary `json:"services" jsonschema:"the services sorted by name, then service ID"`
	NextCursor string           `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool             `json:"truncated" jsonschema:"whether matching services remain after this page"`
}

type NodeReference struct {
	NodeID    string `json:"node_id" jsonschema:"the collector node ID"`
	Nodename  string `json:"nodename" jsonschema:"the node name"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the node"`
}

type ServiceSummary struct {
	SvcID         string   `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname       string   `json:"svcname" jsonschema:"the service name, an OpenSVC object path such as ns/svc/name; several clusters may hold the same name"`
	ClusterID     string   `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
	App           string   `json:"app,omitempty" jsonschema:"the collector app the service belongs to"`
	Env           string   `json:"env,omitempty" jsonschema:"the service environment, such as PRD or DEV"`
	Topology      string   `json:"topology,omitempty" jsonschema:"the service topology, such as failover or flex"`
	AvailStatus   string   `json:"availstatus,omitempty" jsonschema:"the aggregated availability status the collector last received, such as up, down, warn, n/a or undef"`
	Status        string   `json:"status,omitempty" jsonschema:"the service status, the aggregated overall status the collector last received, such as up, down, warn, n/a or undef, as the collector Services page shows it: whether the service is up or down"`
	Frozen        string   `json:"frozen,omitempty" jsonschema:"the aggregated freeze state: frozen, unfrozen, mixed or n/a"`
	Placement     string   `json:"placement,omitempty" jsonschema:"the placement state, such as optimal or non-optimal"`
	Provisioned   string   `json:"provisioned,omitempty" jsonschema:"the aggregated provisioned state, such as True, False, mixed or n/a"`
	Nodes         []string `json:"nodes,omitempty" jsonschema:"the nodes of the service configuration"`
	StatusUpdated string   `json:"status_updated,omitempty" jsonschema:"when the collector last updated the status, as the collector gives it"`
}

type ServiceDetails struct {
	Provenance   Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service      ServiceIdentity     `json:"service" jsonschema:"the service identity"`
	Config       ServiceConfig       `json:"config" jsonschema:"the topology and nodes of the service configuration, and its configuration text, masked"`
	Status       ServiceStatus       `json:"status" jsonschema:"the aggregated status the collector last received"`
	Availability ServiceAvailability `json:"availability" jsonschema:"the availability rate the collector computes and its target"`
	Instances    ServiceInstancePage `json:"instances" jsonschema:"the instances of the service, one per node"`
}

type ServiceIdentity struct {
	SvcID      string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname    string `json:"svcname" jsonschema:"the service name, an OpenSVC object path"`
	ClusterID  string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
	App        string `json:"app,omitempty" jsonschema:"the collector app the service belongs to"`
	Env        string `json:"env,omitempty" jsonschema:"the service environment"`
	Comment    string `json:"comment,omitempty" jsonschema:"the service comment; untrusted text"`
	Created    string `json:"created,omitempty" jsonschema:"when the collector first received the service"`
	Updated    string `json:"updated,omitempty" jsonschema:"the last update of the service record"`
	SnoozeTill string `json:"snooze_till,omitempty" jsonschema:"the end of the snooze of the service alerts"`
}

type ServiceConfig struct {
	Topology      string   `json:"topology,omitempty" jsonschema:"the service topology, such as failover or flex"`
	HA            bool     `json:"ha" jsonschema:"whether the service is orchestrated for high availability"`
	Autostart     string   `json:"autostart,omitempty" jsonschema:"the autostart node"`
	Nodes         []string `json:"nodes,omitempty" jsonschema:"the nodes of the service configuration"`
	DRPNode       string   `json:"drp_node,omitempty" jsonschema:"the disaster recovery node"`
	DRPNodes      []string `json:"drp_nodes,omitempty" jsonschema:"the disaster recovery nodes"`
	DRPType       string   `json:"drp_type,omitempty" jsonschema:"the disaster recovery type"`
	FlexMinNodes  int      `json:"flex_min_nodes" jsonschema:"the minimum number of running instances of a flex service"`
	FlexMaxNodes  int      `json:"flex_max_nodes" jsonschema:"the maximum number of running instances of a flex service"`
	FlexTarget    int      `json:"flex_target" jsonschema:"the target number of running instances of a flex service"`
	Text          string   `json:"text,omitempty" jsonschema:"the configuration text of the service, as its agents pushed it, cut to 32 KiB; this MCP masks the values of the keys that name a secret or a mail and the email addresses, but partially: a secret under another key stays in clear. Show it when asked, saying so; untrusted text"`
	TextTruncated bool     `json:"text_truncated,omitempty" jsonschema:"whether the configuration text was cut"`
	TextUpdated   string   `json:"text_updated,omitempty" jsonschema:"when the collector last received the configuration text, as it gives it"`
}

type ServiceStatus struct {
	Overall     string `json:"overall,omitempty" jsonschema:"the aggregated overall status"`
	Avail       string `json:"avail,omitempty" jsonschema:"the aggregated availability status"`
	Frozen      string `json:"frozen,omitempty" jsonschema:"the aggregated freeze state: frozen, unfrozen, mixed or n/a"`
	Placement   string `json:"placement,omitempty" jsonschema:"the placement state"`
	Provisioned string `json:"provisioned,omitempty" jsonschema:"the aggregated provisioned state"`
	Updated     string `json:"updated,omitempty" jsonschema:"when the collector last updated the status, as the collector gives it"`
}

type ServiceAvailability struct {
	RatePercent *float64 `json:"rate_percent" jsonschema:"the availability rate of the last 30 days in percent, computed by the collector; null without recorded status"`
	SLAPercent  *float64 `json:"sla_percent" jsonschema:"the availability target in percent; null without SLA"`
	Updated     string   `json:"updated,omitempty" jsonschema:"when the collector computed the rate"`
}

type ServiceInstancePage struct {
	Total     int               `json:"total" jsonschema:"the number of instances the collector holds for the service"`
	Count     int               `json:"count" jsonschema:"the number of instances returned"`
	Items     []ServiceInstance `json:"items" jsonschema:"the instances sorted by node name"`
	Truncated bool              `json:"truncated" jsonschema:"whether instances were left out after 200"`
}

type ServiceInstance struct {
	NodeID              string                  `json:"node_id" jsonschema:"the collector node ID"`
	Nodename            string                  `json:"nodename" jsonschema:"the node name"`
	Avail               string                  `json:"avail,omitempty" jsonschema:"the availability status of the instance"`
	Overall             string                  `json:"overall,omitempty" jsonschema:"the overall status of the instance"`
	MonitorState        string                  `json:"monitor_state,omitempty" jsonschema:"the state of the instance monitor, such as idle"`
	MonitorGlobalExpect string                  `json:"monitor_global_expect,omitempty" jsonschema:"the global expectation of the monitor, such as none"`
	Frozen              bool                    `json:"frozen" jsonschema:"whether the instance is frozen"`
	FrozenAt            string                  `json:"frozen_at,omitempty" jsonschema:"when the instance was frozen"`
	Updated             string                  `json:"updated,omitempty" jsonschema:"when the collector last received the instance status"`
	Resources           ServiceResourceStatuses `json:"resources" jsonschema:"the aggregated status of each resource kind of the instance"`
}

type ServiceResourceStatuses struct {
	IP        string `json:"ip,omitempty" jsonschema:"the status of the ip resources"`
	FS        string `json:"fs,omitempty" jsonschema:"the status of the fs resources"`
	Disk      string `json:"disk,omitempty" jsonschema:"the status of the disk resources"`
	Container string `json:"container,omitempty" jsonschema:"the status of the container resources"`
	Share     string `json:"share,omitempty" jsonschema:"the status of the share resources"`
	Sync      string `json:"sync,omitempty" jsonschema:"the status of the sync resources"`
	App       string `json:"app,omitempty" jsonschema:"the status of the app resources"`
	HB        string `json:"hb,omitempty" jsonschema:"the status of the heartbeat resources"`
}

type collectorService struct {
	SvcID                  string   `json:"svc_id"`
	Svcname                string   `json:"svcname"`
	ClusterID              string   `json:"cluster_id"`
	SvcApp                 string   `json:"svc_app"`
	SvcEnv                 string   `json:"svc_env"`
	SvcComment             string   `json:"svc_comment"`
	SvcConfig              string   `json:"svc_config"`
	SvcConfigUpdated       string   `json:"svc_config_updated"`
	SvcCreated             string   `json:"svc_created"`
	Updated                string   `json:"updated"`
	SvcSnoozeTill          string   `json:"svc_snooze_till"`
	SvcTopology            string   `json:"svc_topology"`
	SvcHA                  int      `json:"svc_ha"`
	SvcAutostart           string   `json:"svc_autostart"`
	SvcNodes               string   `json:"svc_nodes"`
	SvcDRPNode             string   `json:"svc_drpnode"`
	SvcDRPNodes            string   `json:"svc_drpnodes"`
	SvcDRPType             string   `json:"svc_drptype"`
	SvcFlexMinNodes        int      `json:"svc_flex_min_nodes"`
	SvcFlexMaxNodes        int      `json:"svc_flex_max_nodes"`
	SvcFlexTarget          int      `json:"svc_flex_target"`
	SvcStatus              string   `json:"svc_status"`
	SvcAvailStatus         string   `json:"svc_availstatus"`
	SvcFrozen              string   `json:"svc_frozen"`
	SvcPlacement           string   `json:"svc_placement"`
	SvcProvisioned         string   `json:"svc_provisioned"`
	SvcStatusUpdated       string   `json:"svc_status_updated"`
	SvcAvailability        *float64 `json:"svc_availability"`
	SvcSLA                 *float64 `json:"svc_sla"`
	SvcAvailabilityUpdated string   `json:"svc_availability_updated"`
}

// collectorNodeService is a row of /services_instances, whose service
// columns are joined.
type collectorNodeService struct {
	SvcID            string `json:"svc_id"`
	Svcname          string `json:"services.svcname"`
	ClusterID        string `json:"services.cluster_id"`
	SvcApp           string `json:"services.svc_app"`
	SvcEnv           string `json:"services.svc_env"`
	SvcTopology      string `json:"services.svc_topology"`
	SvcAvailStatus   string `json:"services.svc_availstatus"`
	SvcStatus        string `json:"services.svc_status"`
	SvcFrozen        string `json:"services.svc_frozen"`
	SvcPlacement     string `json:"services.svc_placement"`
	SvcProvisioned   string `json:"services.svc_provisioned"`
	SvcNodes         string `json:"services.svc_nodes"`
	SvcStatusUpdated string `json:"services.svc_status_updated"`
}

type collectorInstance struct {
	NodeID              string `json:"node_id"`
	Nodename            string `json:"nodes.nodename"`
	MonAvailStatus      string `json:"mon_availstatus"`
	MonOverallStatus    string `json:"mon_overallstatus"`
	MonSmonStatus       string `json:"mon_smon_status"`
	MonSmonGlobalExpect string `json:"mon_smon_global_expect"`
	MonFrozen           string `json:"mon_frozen"`
	MonFrozenAt         string `json:"mon_frozen_at"`
	MonUpdated          string `json:"mon_updated"`
	MonIPStatus         string `json:"mon_ipstatus"`
	MonFSStatus         string `json:"mon_fsstatus"`
	MonDiskStatus       string `json:"mon_diskstatus"`
	MonContainerStatus  string `json:"mon_containerstatus"`
	MonShareStatus      string `json:"mon_sharestatus"`
	MonSyncStatus       string `json:"mon_syncstatus"`
	MonAppStatus        string `json:"mon_appstatus"`
	MonHBStatus         string `json:"mon_hbstatus"`
}

// validate checks the filters given by the caller.
func (f ServiceFilters) validate() error {
	for name, value := range map[string]string{"name": f.Name, "app": f.App, "env": f.Env, "availstatus": f.AvailStatus, "status": f.Status} {
		if err := validateFilterValue(name, value); err != nil {
			return err
		}
	}
	if err := validateCollectorUUID("cluster_id", f.ClusterID); err != nil {
		return err
	}
	for name, allowed := range map[string][2]string{"topology": {f.Topology, "failover, flex"}, "frozen": {f.Frozen, "frozen, unfrozen, mixed"}, "placement": {f.Placement, "optimal, non-optimal"}} {
		if allowed[0] != "" && !slices.Contains(strings.Split(allowed[1], ", "), allowed[0]) {
			return fmt.Errorf("%s must be one of %s", name, allowed[1])
		}
	}
	return nil
}

// collectorFilters translates the filters into collector filters on the
// service props, named with prefix.
func (f ServiceFilters) collectorFilters(prefix string) []string {
	var filters []string
	if f.Name != "" {
		filters = append(filters, filterContains(prefix+"svcname", f.Name))
	}
	for _, filter := range [][2]string{
		{"cluster_id", f.ClusterID}, {"svc_app", f.App}, {"svc_env", f.Env}, {"svc_availstatus", f.AvailStatus}, {"svc_status", f.Status},
		{"svc_topology", f.Topology}, {"svc_frozen", f.Frozen}, {"svc_placement", f.Placement},
	} {
		if filter[1] != "" {
			filters = append(filters, filterEquals(prefix+filter[0], filter[1]))
		}
	}
	return filters
}

// ListServices reads one page of the services the collector account sees,
// sorted by name, with typed filters. With node, it lists the services
// having an instance on that node; with tag, those holding it.
func (s *Service) ListServices(ctx context.Context, options ListServicesOptions) (ServiceList, error) {
	if err := options.ServiceFilters.validate(); err != nil {
		return ServiceList{}, err
	}
	if options.Node != "" && options.ServiceFilters.member() {
		return ServiceList{}, errNodeWithMember
	}
	limit, err := pageLimit(options.Limit, defaultListServicesLimit, maxListServicesLimit)
	if err != nil {
		return ServiceList{}, err
	}
	path, members, err := s.serviceMemberPath(ctx, options.ServiceFilters)
	if err != nil {
		return ServiceList{}, err
	}
	var node *NodeReference
	prefix := ""
	if options.Node != "" {
		found, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ServiceList{}, err
		}
		node = &NodeReference{NodeID: found.NodeID, Nodename: found.Nodename, ClusterID: found.ClusterID}
		prefix = "services."
	}
	var filters []string
	if node != nil {
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	filters = append(filters, options.ServiceFilters.collectorFilters(prefix)...)
	fingerprint := listFingerprint(url.Values{"path": {path}, "filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ServiceList{}, err
	}

	var services []collectorService
	var total *int
	if node == nil {
		var response collectorList[collectorService]
		if err := s.client.GetJSON(ctx, path, pageQuery(serviceSummaryProps, "svcname,svc_id", filters, limit, offset), &response); err != nil {
			return ServiceList{}, fmt.Errorf("list services: %w", err)
		}
		services, total = response.Data, response.Meta.Total
	} else {
		props := make([]string, 0, len(serviceSummaryProps))
		for _, prop := range serviceSummaryProps {
			if prop == "svc_id" {
				props = append(props, prop)
				continue
			}
			props = append(props, "services."+prop)
		}
		var response collectorList[collectorNodeService]
		if err := s.client.GetJSON(ctx, "/services_instances", pageQuery(props, "services.svcname,svc_id", filters, limit, offset), &response); err != nil {
			return ServiceList{}, fmt.Errorf("list services of node %s: %w", node.Nodename, err)
		}
		total = response.Meta.Total
		for _, row := range response.Data {
			services = append(services, collectorService{
				SvcID: row.SvcID, Svcname: row.Svcname, ClusterID: row.ClusterID, SvcApp: row.SvcApp, SvcEnv: row.SvcEnv,
				SvcTopology: row.SvcTopology, SvcAvailStatus: row.SvcAvailStatus, SvcStatus: row.SvcStatus, SvcFrozen: row.SvcFrozen,
				SvcPlacement: row.SvcPlacement, SvcProvisioned: row.SvcProvisioned, SvcNodes: row.SvcNodes, SvcStatusUpdated: row.SvcStatusUpdated,
			})
		}
	}
	if total == nil || *total < 0 || len(services) > limit {
		return ServiceList{}, fmt.Errorf("list services: unexpected collector response")
	}
	result := ServiceList{Provenance: s.newProvenance(), Node: node, ServiceMembership: members, Total: *total, Count: len(services), Services: make([]ServiceSummary, 0, len(services))}
	for _, service := range services {
		result.Services = append(result.Services, ServiceSummary{
			SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID, App: service.SvcApp, Env: service.SvcEnv,
			Topology: service.SvcTopology, AvailStatus: service.SvcAvailStatus, Status: service.SvcStatus, Frozen: service.SvcFrozen,
			Placement: service.SvcPlacement, Provisioned: service.SvcProvisioned, Nodes: strings.Fields(service.SvcNodes),
			StatusUpdated: service.SvcStatusUpdated,
		})
	}
	if next := offset + len(services); len(services) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// GetService reads one service by its exact name or service ID, with its
// instances and its configuration text, masked. A name held by several
// services is refused with their IDs.
func (s *Service) GetService(ctx context.Context, service string) (ServiceDetails, error) {
	svc, err := s.lookupService(ctx, service, serviceDetailProps)
	if err != nil {
		return ServiceDetails{}, err
	}

	instanceQuery := url.Values{}
	instanceQuery.Set("props", strings.Join(serviceInstanceProps, ","))
	instanceQuery.Set("orderby", "nodes.nodename,node_id")
	instanceQuery.Set("limit", strconv.Itoa(maxServiceInstances))
	var instances collectorList[collectorInstance]
	if err := s.client.GetJSON(ctx, "/services/"+url.PathEscape(svc.SvcID)+"/nodes", instanceQuery, &instances); err != nil {
		return ServiceDetails{}, fmt.Errorf("get service %s instances: %w", service, err)
	}
	if instances.Meta.Total == nil || *instances.Meta.Total < 0 || len(instances.Data) > maxServiceInstances {
		return ServiceDetails{}, fmt.Errorf("get service %s instances: unexpected collector response", service)
	}

	result := ServiceDetails{
		Provenance: s.newProvenance(),
		Service: ServiceIdentity{
			SvcID: svc.SvcID, Svcname: svc.Svcname, ClusterID: svc.ClusterID, App: svc.SvcApp, Env: svc.SvcEnv,
			Comment: svc.SvcComment, Created: svc.SvcCreated, Updated: svc.Updated, SnoozeTill: svc.SvcSnoozeTill,
		},
		Config: ServiceConfig{
			Topology: svc.SvcTopology, HA: svc.SvcHA == 1, Autostart: svc.SvcAutostart, Nodes: strings.Fields(svc.SvcNodes),
			DRPNode: svc.SvcDRPNode, DRPNodes: strings.Fields(svc.SvcDRPNodes), DRPType: svc.SvcDRPType,
			FlexMinNodes: svc.SvcFlexMinNodes, FlexMaxNodes: svc.SvcFlexMaxNodes, FlexTarget: svc.SvcFlexTarget,
			TextUpdated: svc.SvcConfigUpdated,
		},
		Status: ServiceStatus{
			Overall: svc.SvcStatus, Avail: svc.SvcAvailStatus, Frozen: svc.SvcFrozen, Placement: svc.SvcPlacement,
			Provisioned: svc.SvcProvisioned, Updated: svc.SvcStatusUpdated,
		},
		Availability: ServiceAvailability{RatePercent: svc.SvcAvailability, SLAPercent: svc.SvcSLA, Updated: svc.SvcAvailabilityUpdated},
		Instances: ServiceInstancePage{
			Total: *instances.Meta.Total, Count: len(instances.Data), Items: make([]ServiceInstance, 0, len(instances.Data)),
			Truncated: len(instances.Data) < *instances.Meta.Total,
		},
	}
	result.Config.Text, result.Config.TextTruncated = maskServiceConfig(svc.SvcConfig)
	for _, instance := range instances.Data {
		result.Instances.Items = append(result.Instances.Items, ServiceInstance{
			NodeID: instance.NodeID, Nodename: instance.Nodename, Avail: instance.MonAvailStatus, Overall: instance.MonOverallStatus,
			MonitorState: instance.MonSmonStatus, MonitorGlobalExpect: instance.MonSmonGlobalExpect,
			Frozen: instanceFrozen(instance.MonFrozen), FrozenAt: collectorDate(instance.MonFrozenAt), Updated: instance.MonUpdated,
			Resources: ServiceResourceStatuses{
				IP: instance.MonIPStatus, FS: instance.MonFSStatus, Disk: instance.MonDiskStatus, Container: instance.MonContainerStatus,
				Share: instance.MonShareStatus, Sync: instance.MonSyncStatus, App: instance.MonAppStatus, HB: instance.MonHBStatus,
			},
		})
	}
	return result, nil
}

// lookupService reads the props of one service by its exact name or service
// ID, and refuses a name several services hold.
func (s *Service) lookupService(ctx context.Context, service string, props []string) (collectorService, error) {
	if service == "" || strings.ContainsAny(service, " ") {
		return collectorService{}, fmt.Errorf("service must be one exact service name or service ID")
	}
	if err := validateFilterValue("service", service); err != nil {
		return collectorService{}, err
	}
	query := url.Values{}
	query.Set("props", strings.Join(props, ","))
	query.Set("meta", "false")
	var response collectorList[collectorService]
	if err := s.client.GetJSON(ctx, "/services/"+url.PathEscape(service), query, &response); err != nil {
		return collectorService{}, fmt.Errorf("get service %s: %w%s", service, err, notFoundHint(err))
	}
	switch len(response.Data) {
	case 0:
		return collectorService{}, fmt.Errorf("get service %s: not found in the collector%s", service, findObjectsHint)
	case 1:
		return response.Data[0], nil
	default:
		ids := make([]string, 0, len(response.Data))
		for _, item := range response.Data {
			ids = append(ids, item.SvcID+" (cluster "+item.ClusterID+")")
		}
		return collectorService{}, fmt.Errorf("get service %s: the name matches %d services, pass one service ID: %s", service, len(ids), strings.Join(ids, ", "))
	}
}

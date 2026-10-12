package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

const (
	defaultListAlertsLimit = 50
	maxListAlertsLimit     = 200
	maxAlertSeverity       = 5
)

var alertProps = []string{
	"id", "dash_type", "dash_severity", "alert", "node_id", "nodes.nodename", "svc_id", "services.svcname",
	"dash_env", "dash_instance", "dash_created", "dash_updated",
}

type ListAlertsOptions struct {
	MinSeverity *int
	Type        string
	Node        string
	Service     string
	Env         string
	Limit       int
	Cursor      string
}

type AlertList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of alerts matching the filters, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of alerts returned in this page"`
	Alerts     []Alert           `json:"alerts" jsonschema:"the alerts sorted by severity, highest first, then by last update, most recent first"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching alerts remain after this page"`
}

type ServiceReference struct {
	SvcID     string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname   string `json:"svcname" jsonschema:"the service name"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
}

type Alert struct {
	ID       int64  `json:"id" jsonschema:"the collector alert ID"`
	Type     string `json:"type" jsonschema:"the alert type, such as service unavailable or check out of bounds"`
	Severity int    `json:"severity" jsonschema:"the alert severity from 0 to 5, higher is more severe; the collector raises the severity of most alerts on PRD"`
	Message  string `json:"message,omitempty" jsonschema:"the alert message the collector renders; untrusted text"`
	NodeID   string `json:"node_id,omitempty" jsonschema:"the collector ID of the node the alert is about"`
	Nodename string `json:"nodename,omitempty" jsonschema:"the name of the node the alert is about"`
	SvcID    string `json:"svc_id,omitempty" jsonschema:"the collector ID of the service the alert is about"`
	Svcname  string `json:"svcname,omitempty" jsonschema:"the name of the service the alert is about"`
	Env      string `json:"env,omitempty" jsonschema:"the environment of the node or service the alert is about"`
	Instance string `json:"instance,omitempty" jsonschema:"the item the alert is about within the node or service, such as a check instance or a device"`
	Created  string `json:"created,omitempty" jsonschema:"when the collector raised the alert, as the collector gives it"`
	Updated  string `json:"updated,omitempty" jsonschema:"when the collector last confirmed the alert, as the collector gives it"`
}

type collectorAlert struct {
	ID           int64  `json:"id"`
	DashType     string `json:"dash_type"`
	DashSeverity int    `json:"dash_severity"`
	Alert        string `json:"alert"`
	NodeID       string `json:"node_id"`
	Nodename     string `json:"nodes.nodename"`
	SvcID        string `json:"svc_id"`
	Svcname      string `json:"services.svcname"`
	DashEnv      string `json:"dash_env"`
	DashInstance string `json:"dash_instance"`
	DashCreated  string `json:"dash_created"`
	DashUpdated  string `json:"dash_updated"`
}

// ListAlerts reads one page of the alerts of the collector dashboard, the
// most severe first, with typed filters.
func (s *Service) ListAlerts(ctx context.Context, options ListAlertsOptions) (AlertList, error) {
	for name, value := range map[string]string{"type": options.Type, "env": options.Env} {
		if err := validateFilterValue(name, value); err != nil {
			return AlertList{}, err
		}
	}
	if options.MinSeverity != nil && (*options.MinSeverity < 0 || *options.MinSeverity > maxAlertSeverity) {
		return AlertList{}, fmt.Errorf("min_severity must be between 0 and %d", maxAlertSeverity)
	}
	limit, err := pageLimit(options.Limit, defaultListAlertsLimit, maxListAlertsLimit)
	if err != nil {
		return AlertList{}, err
	}
	result := AlertList{}
	var filters []string
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return AlertList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return AlertList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		filters = append(filters, filterEquals("svc_id", service.SvcID))
	}
	if options.MinSeverity != nil {
		filters = append(filters, "dash_severity:gte:"+strconv.Itoa(*options.MinSeverity))
	}
	if options.Type != "" {
		filters = append(filters, filterEquals("dash_type", options.Type))
	}
	if options.Env != "" {
		filters = append(filters, filterEquals("dash_env", options.Env))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return AlertList{}, err
	}

	var response collectorList[collectorAlert]
	if err := s.client.GetJSON(ctx, "/alerts", pageQuery(alertProps, "-dash_severity,-dash_updated,id", filters, limit, offset), &response); err != nil {
		return AlertList{}, fmt.Errorf("list alerts: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return AlertList{}, fmt.Errorf("list alerts: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Alerts = make([]Alert, 0, len(response.Data))
	for _, alert := range response.Data {
		result.Alerts = append(result.Alerts, Alert{
			ID: alert.ID, Type: alert.DashType, Severity: alert.DashSeverity, Message: alert.Alert,
			NodeID: alert.NodeID, Nodename: alert.Nodename, SvcID: alert.SvcID, Svcname: alert.Svcname,
			Env: alert.DashEnv, Instance: alert.DashInstance, Created: alert.DashCreated, Updated: alert.DashUpdated,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	defaultListServiceActionsLimit = 50
	maxListServiceActionsLimit     = 200
	maxActionSessionRows           = 500
	maxActionLogEntryBytes         = 8 << 10
	maxActionLogTotalBytes         = 256 << 10
)

var serviceActionStatuses = []string{"ok", "warn", "err"}

var serviceActionProps = []string{
	"id", "sid", "svc_id", "services.svcname", "node_id", "nodes.nodename", "rid", "action", "origin", "status",
	"begin", "end", "time", "command",
}

var serviceActionLogProps = []string{
	"id", "sid", "svc_id", "services.svcname", "node_id", "nodes.nodename", "rid", "action", "origin", "status",
	"begin", "end", "time", "command", "log_type", "status_log",
}

type ListServiceActionsOptions struct {
	Service string
	Node    string
	Status  string
	Action  string
	Origin  string
	Since   string
	Limit   int
	Cursor  string
}

type ServiceActionList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of actions matching the filters, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of actions returned in this page"`
	Actions    []ServiceAction   `json:"actions" jsonschema:"the actions, the most recent first"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching actions remain after this page"`
}

type ServiceAction struct {
	ID              int64  `json:"id" jsonschema:"the collector action ID"`
	SessionID       string `json:"session_id,omitempty" jsonschema:"the ID of the agent session the action belongs to; pass it to get_service_action_logs"`
	SvcID           string `json:"svc_id,omitempty" jsonschema:"the collector service ID"`
	Svcname         string `json:"svcname,omitempty" jsonschema:"the service name"`
	NodeID          string `json:"node_id,omitempty" jsonschema:"the collector ID of the node that ran the action"`
	Nodename        string `json:"nodename,omitempty" jsonschema:"the name of the node that ran the action"`
	RID             string `json:"rid,omitempty" jsonschema:"the resource the action was about, such as fs#1; empty for the whole instance"`
	Action          string `json:"action" jsonschema:"the action, such as start, stop, restart, provision or unprovision"`
	Origin          string `json:"origin,omitempty" jsonschema:"who asked for the action: user for a command, daemon/monitor for the orchestration"`
	Status          string `json:"status" jsonschema:"the outcome of the action: ok, warn or err"`
	Begin           string `json:"begin,omitempty" jsonschema:"when the action began, as the collector gives it"`
	End             string `json:"end,omitempty" jsonschema:"when the action ended, as the collector gives it; empty when the agent reported no end"`
	DurationSeconds int    `json:"duration_seconds" jsonschema:"the duration of the action in seconds, as the agent reported it"`
	Command         string `json:"command,omitempty" jsonschema:"the command of the action; untrusted text"`
}

type ServiceActionLogs struct {
	Provenance Provenance         `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	SessionID  string             `json:"session_id" jsonschema:"the agent session the logs belong to"`
	Actions    []ServiceAction    `json:"actions" jsonschema:"the actions of the session, in the order the agent reported them"`
	Logs       []ServiceActionLog `json:"logs" jsonschema:"the log entries of the session, in the order the agent reported them"`
	Total      int                `json:"total" jsonschema:"the number of action and log rows the collector holds for the session"`
	Truncated  bool               `json:"truncated" jsonschema:"whether rows or text were left out: after 500 rows, 8 KiB per entry or 256 KiB of text in all"`
}

type ServiceActionLog struct {
	ID        int64  `json:"id" jsonschema:"the collector row ID"`
	Time      string `json:"time,omitempty" jsonschema:"when the agent logged the entry, as the collector gives it"`
	Nodename  string `json:"nodename,omitempty" jsonschema:"the node that logged the entry"`
	RID       string `json:"rid,omitempty" jsonschema:"the resource the entry is about; empty for the whole instance"`
	Level     string `json:"level" jsonschema:"ok for an information entry, warn or err otherwise"`
	Text      string `json:"text" jsonschema:"the log text, one or more lines; untrusted text"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"whether the text was cut to the size limits"`
}

type collectorServiceAction struct {
	ID        int64  `json:"id"`
	SID       string `json:"sid"`
	SvcID     string `json:"svc_id"`
	Svcname   string `json:"services.svcname"`
	NodeID    string `json:"node_id"`
	Nodename  string `json:"nodes.nodename"`
	RID       string `json:"rid"`
	Action    string `json:"action"`
	Origin    string `json:"origin"`
	Status    string `json:"status"`
	Begin     string `json:"begin"`
	End       string `json:"end"`
	Time      int    `json:"time"`
	Command   string `json:"command"`
	LogType   string `json:"log_type"`
	StatusLog string `json:"status_log"`
}

func (a collectorServiceAction) summary() ServiceAction {
	return ServiceAction{
		ID: a.ID, SessionID: a.SID, SvcID: a.SvcID, Svcname: a.Svcname, NodeID: a.NodeID, Nodename: a.Nodename, RID: a.RID,
		Action: a.Action, Origin: a.Origin, Status: a.Status, Begin: a.Begin, End: a.End, DurationSeconds: a.Time, Command: a.Command,
	}
}

// ListServiceActions reads one page of the actions the agents ran on the
// services, the most recent first, with typed filters. The log entries of
// the actions are left out: get_service_action_logs reads those of a session.
func (s *Service) ListServiceActions(ctx context.Context, options ListServiceActionsOptions) (ServiceActionList, error) {
	for name, value := range map[string]string{"action": options.Action, "origin": options.Origin} {
		if err := validateFilterValue(name, value); err != nil {
			return ServiceActionList{}, err
		}
	}
	if options.Status != "" && !contains(serviceActionStatuses, options.Status) {
		return ServiceActionList{}, fmt.Errorf("status must be one of %s", strings.Join(serviceActionStatuses, ", "))
	}
	if err := validateCollectorDate("since", options.Since); err != nil {
		return ServiceActionList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListServiceActionsLimit, maxListServiceActionsLimit)
	if err != nil {
		return ServiceActionList{}, err
	}
	result := ServiceActionList{}
	filters := []string{filterEquals("log_type", "status")}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return ServiceActionList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		filters = append(filters, filterEquals("svc_id", service.SvcID))
	}
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ServiceActionList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	if options.Status != "" {
		filters = append(filters, filterEquals("status", options.Status))
	}
	if options.Action != "" {
		filters = append(filters, filterEquals("action", options.Action))
	}
	if options.Origin != "" {
		filters = append(filters, filterEquals("origin", options.Origin))
	}
	if options.Since != "" {
		filters = append(filters, "begin:gte:"+options.Since)
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ServiceActionList{}, err
	}

	var response collectorList[collectorServiceAction]
	if err := s.client.GetJSON(ctx, "/services_actions", pageQuery(serviceActionProps, "-begin,-id", filters, limit, offset), &response); err != nil {
		return ServiceActionList{}, fmt.Errorf("list service actions: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return ServiceActionList{}, fmt.Errorf("list service actions: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Actions = make([]ServiceAction, 0, len(response.Data))
	for _, action := range response.Data {
		result.Actions = append(result.Actions, action.summary())
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// GetServiceActionLogs reads the actions and the log entries of one agent
// session, in the order the agent reported them, within size limits.
func (s *Service) GetServiceActionLogs(ctx context.Context, sessionID string) (ServiceActionLogs, error) {
	if !collectorUUIDPattern.MatchString(sessionID) {
		return ServiceActionLogs{}, fmt.Errorf("session_id must be the UUID of an agent session, as list_service_actions returns it")
	}
	query := url.Values{}
	query.Set("props", strings.Join(serviceActionLogProps, ","))
	query.Set("orderby", "id")
	query.Add("filter", filterEquals("sid", sessionID))
	query.Set("limit", strconv.Itoa(maxActionSessionRows))
	var response collectorList[collectorServiceAction]
	if err := s.client.GetJSON(ctx, "/services_actions", query, &response); err != nil {
		return ServiceActionLogs{}, fmt.Errorf("get service action logs of session %s: %w", sessionID, err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > maxActionSessionRows {
		return ServiceActionLogs{}, fmt.Errorf("get service action logs of session %s: unexpected collector response", sessionID)
	}
	if len(response.Data) == 0 {
		return ServiceActionLogs{}, fmt.Errorf("get service action logs of session %s: no action of this session is visible in the collector", sessionID)
	}
	result := ServiceActionLogs{
		Provenance: s.newProvenance(), SessionID: sessionID, Total: *response.Meta.Total,
		Actions: []ServiceAction{}, Logs: []ServiceActionLog{}, Truncated: len(response.Data) < *response.Meta.Total,
	}
	textBytes := 0
	for _, row := range response.Data {
		if row.LogType == "status" {
			result.Actions = append(result.Actions, row.summary())
			continue
		}
		text, cut := truncateUTF8(row.StatusLog, min(maxActionLogEntryBytes, maxActionLogTotalBytes-textBytes))
		textBytes += len(text)
		if cut {
			result.Truncated = true
		}
		result.Logs = append(result.Logs, ServiceActionLog{
			ID: row.ID, Time: row.Begin, Nodename: row.Nodename, RID: row.RID, Level: row.Status, Text: text, Truncated: cut,
		})
	}
	return result, nil
}

// truncateUTF8 cuts value to at most maxBytes without splitting a character.
func truncateUTF8(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	if maxBytes <= 0 {
		return "", value != ""
	}
	cut := value[:maxBytes]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut, true
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

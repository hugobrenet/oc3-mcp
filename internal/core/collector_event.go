package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	defaultListCollectorEventsLimit = 50
	maxListCollectorEventsLimit     = 200
	maxCollectorEventMessageBytes   = 4 << 10
)

// excludedCollectorEvents are the dashboard sign-ins and sign-outs, a history
// of the presence of people with no bearing on the nodes and services.
const excludedCollectorEvents = "log_action:!in:user.login,user.logout"

var (
	collectorEventLevels = []string{"info", "warning", "error"}
	collectorEventField  = regexp.MustCompile(`%\((\w+)\)[sd]`)
	collectorEventProps  = []string{"id", "log_date", "log_action", "log_level", "log_user", "log_impersonator", "log_fmt", "log_dict", "node_id", "svc_id"}
)

type ListCollectorEventsOptions struct {
	Node    string
	Service string
	Action  string
	Level   string
	User    string
	Since   string
	Limit   int
	Cursor  string
}

type CollectorEventList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of events matching the filters, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of events returned in this page"`
	Events     []CollectorEvent  `json:"events" jsonschema:"the events, the most recent first"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching events remain after this page"`
}

type CollectorEvent struct {
	ID           int64          `json:"id" jsonschema:"the collector event ID"`
	Date         string         `json:"date" jsonschema:"when the event happened, in collector local time without a zone"`
	Action       string         `json:"action" jsonschema:"the event action, such as service.status, resource.status, service.action or node.change"`
	Level        string         `json:"level" jsonschema:"info, warning or error"`
	User         string         `json:"user,omitempty" jsonschema:"who caused the event: a user account, its email address masked, or scheduler for the collector itself"`
	Impersonator string         `json:"impersonator,omitempty" jsonschema:"the user acting as user, when the action was made while impersonating; an email address is masked"`
	Message      string         `json:"message" jsonschema:"the event message, rendered from its template and data; untrusted text"`
	Data         map[string]any `json:"data,omitempty" jsonschema:"the values of the message; untrusted data"`
	NodeID       string         `json:"node_id,omitempty" jsonschema:"the collector ID of the node the event is about"`
	Nodename     string         `json:"nodename,omitempty" jsonschema:"the name of that node, when the collector still knows it"`
	SvcID        string         `json:"svc_id,omitempty" jsonschema:"the collector ID of the service the event is about"`
	Svcname      string         `json:"svcname,omitempty" jsonschema:"the name of that service, when the collector still knows it"`
}

type collectorLogRow struct {
	ID              int64  `json:"id"`
	LogDate         string `json:"log_date"`
	LogAction       string `json:"log_action"`
	LogLevel        string `json:"log_level"`
	LogUser         string `json:"log_user"`
	LogImpersonator string `json:"log_impersonator"`
	LogFmt          string `json:"log_fmt"`
	LogDict         string `json:"log_dict"`
	NodeID          string `json:"node_id"`
	SvcID           string `json:"svc_id"`
}

// ListCollectorEvents reads one page of the events of the collector log, the
// most recent first: the statuses the collector flags itself, and the actions
// users make in the collector. Dashboard sign-ins and sign-outs are left out.
func (s *Service) ListCollectorEvents(ctx context.Context, options ListCollectorEventsOptions) (CollectorEventList, error) {
	for name, value := range map[string]string{"action": options.Action, "user": options.User} {
		if err := validateFilterValue(name, value); err != nil {
			return CollectorEventList{}, err
		}
	}
	if options.Level != "" && !contains(collectorEventLevels, options.Level) {
		return CollectorEventList{}, fmt.Errorf("level must be one of %s", strings.Join(collectorEventLevels, ", "))
	}
	if err := validateCollectorDate("since", options.Since); err != nil {
		return CollectorEventList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListCollectorEventsLimit, maxListCollectorEventsLimit)
	if err != nil {
		return CollectorEventList{}, err
	}
	result := CollectorEventList{}
	filters := []string{excludedCollectorEvents}
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return CollectorEventList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return CollectorEventList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		filters = append(filters, filterEquals("svc_id", service.SvcID))
	}
	if options.Action != "" {
		filters = append(filters, filterEqualsOrFamily("log_action", options.Action))
	}
	if options.Level != "" {
		filters = append(filters, filterEquals("log_level", options.Level))
	}
	if options.User != "" {
		filters = append(filters, filterEquals("log_user", options.User))
	}
	if options.Since != "" {
		filters = append(filters, "log_date:gte:"+options.Since)
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return CollectorEventList{}, err
	}

	var response collectorList[collectorLogRow]
	if err := s.client.GetJSON(ctx, "/logs", pageQuery(collectorEventProps, "-log_date,-id", filters, limit, offset), &response); err != nil {
		return CollectorEventList{}, fmt.Errorf("list collector events: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return CollectorEventList{}, fmt.Errorf("list collector events: unexpected collector response")
	}
	nodenames, svcnames, err := s.eventNames(ctx, response.Data)
	if err != nil {
		return CollectorEventList{}, fmt.Errorf("list collector events: name the nodes and services: %w", err)
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Events = make([]CollectorEvent, 0, len(response.Data))
	for _, row := range response.Data {
		message, data := renderCollectorEvent(row.LogFmt, row.LogDict)
		result.Events = append(result.Events, CollectorEvent{
			ID: row.ID, Date: row.LogDate, Action: row.LogAction, Level: row.LogLevel, User: personName(row.LogUser), Impersonator: personName(row.LogImpersonator),
			Message: message, Data: data, NodeID: row.NodeID, Nodename: nodenames[row.NodeID], SvcID: row.SvcID, Svcname: svcnames[row.SvcID],
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// eventNames names the nodes and services the events of a page are about,
// in one read each.
func (s *Service) eventNames(ctx context.Context, rows []collectorLogRow) (map[string]string, map[string]string, error) {
	nodeIDs, svcIDs := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if collectorUUIDPattern.MatchString(row.NodeID) {
			nodeIDs[row.NodeID] = true
		}
		if collectorUUIDPattern.MatchString(row.SvcID) {
			svcIDs[row.SvcID] = true
		}
	}
	nodenames, err := s.namesByID(ctx, "/nodes", "node_id", "nodename", sortedKeys(nodeIDs))
	if err != nil {
		return nil, nil, err
	}
	svcnames, err := s.namesByID(ctx, "/services", "svc_id", "svcname", sortedKeys(svcIDs))
	if err != nil {
		return nil, nil, err
	}
	return nodenames, svcnames, nil
}

func (s *Service) namesByID(ctx context.Context, path string, idProp string, nameProp string, ids []string) (map[string]string, error) {
	names := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	query := url.Values{}
	query.Set("props", idProp+","+nameProp)
	query.Add("filter", idProp+":in:"+strings.Join(ids, ","))
	query.Set("limit", "0")
	query.Set("meta", "false")
	var response collectorList[map[string]any]
	if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
		return nil, err
	}
	for _, row := range response.Data {
		id, _ := row[idProp].(string)
		name, _ := row[nameProp].(string)
		if id != "" && name != "" {
			names[id] = name
		}
	}
	return names, nil
}

// renderCollectorEvent fills the %(name)s and %(name)d fields of a template
// with the values of its JSON data, as the collector renders its log. A field
// without value is left as is.
func renderCollectorEvent(template string, rawData string) (string, map[string]any) {
	var data map[string]any
	if rawData != "" {
		if err := json.Unmarshal([]byte(rawData), &data); err != nil {
			data = nil
		}
	}
	message := collectorEventField.ReplaceAllStringFunc(template, func(field string) string {
		name := collectorEventField.FindStringSubmatch(field)[1]
		value, ok := data[name]
		if !ok {
			return field
		}
		if text, ok := value.(string); ok {
			return text
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return field
		}
		return string(encoded)
	})
	message, _ = truncateUTF8(message, maxCollectorEventMessageBytes)
	if len(data) == 0 {
		data = nil
	}
	return message, data
}

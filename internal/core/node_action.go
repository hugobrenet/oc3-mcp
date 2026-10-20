package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxNodePropertyBytes bounds a property value the MCP sends.
	maxNodePropertyBytes = 255
	// maxNodenameBytes bounds the name of a created node.
	maxNodenameBytes = 255
)

// NodeProperties are the properties of a node the action tools set, in the
// order the results give them. The agent of a node never sends app,
// team_responsible, status, role, assetname, warranty_end and
// maintenance_end; it rewrites the others at each push when the
// configuration of its node sets them.
var NodeProperties = []string{
	"app", "team_responsible", "status", "role", "assetname", "warranty_end", "maintenance_end",
	"asset_env", "sec_zone", "team_integ", "team_support",
	"loc_country", "loc_city", "loc_addr", "loc_zip", "loc_building", "loc_floor", "loc_room", "loc_rack",
}

// nodeDateProperties are the properties given as a date, YYYY-MM-DD.
var nodeDateProperties = []string{"warranty_end", "maintenance_end"}

type NodePropertyMismatch struct {
	Property  string `json:"property" jsonschema:"the property"`
	Requested string `json:"requested" jsonschema:"the value the call asked for"`
	Applied   string `json:"applied" jsonschema:"the value the collector holds after the call"`
}

type NodeCreation struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference          `json:"node" jsonschema:"the node created, with the node ID the collector gave it"`
	Properties map[string]string      `json:"properties" jsonschema:"app, team_responsible and the properties the call set, as the collector holds them after the creation"`
	NotApplied []NodePropertyMismatch `json:"not_applied,omitempty" jsonschema:"the properties the collector holds with another value than the one asked for"`
}

type NodeChange struct {
	Property string `json:"property" jsonschema:"the property"`
	Before   string `json:"before" jsonschema:"its value before the update, empty when it was not set"`
	After    string `json:"after" jsonschema:"its value after the update, empty when it is cleared"`
}

type NodeUpdate struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference          `json:"node" jsonschema:"the node updated"`
	Changes    []NodeChange           `json:"changes" jsonschema:"each property the call set, with its value before and after the update"`
	NotApplied []NodePropertyMismatch `json:"not_applied,omitempty" jsonschema:"the properties the collector holds with another value than the one asked for"`
}

type NodeDeletion struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference `json:"node" jsonschema:"the node deleted"`
	Deleted    bool          `json:"deleted" jsonschema:"true: the collector deleted the node and its related entries"`
}

// CreateNode creates one node in the collector inventory. The collector
// creates or updates a node by its name on the same route: a name a node
// already has is refused before anything is sent. The MCP never gives the
// node ID, which the collector generates.
func (s *Service) CreateNode(ctx context.Context, nodename string, properties map[string]string) (NodeCreation, error) {
	if nodename == "" || len(nodename) > maxNodenameBytes || !utf8.ValidString(nodename) ||
		strings.ContainsFunc(nodename, func(r rune) bool { return r <= ' ' || r == '/' || r == 0x7f }) {
		return NodeCreation{}, fmt.Errorf("nodename must be 1 to %d bytes without spaces, slashes or control characters", maxNodenameBytes)
	}
	for name, value := range properties {
		if value == "" {
			delete(properties, name)
		}
	}
	body, err := s.nodePropertiesBody(ctx, properties, false)
	if err != nil {
		return NodeCreation{}, err
	}
	var existing collectorList[collectorNode]
	query := url.Values{"props": {"node_id,nodename,cluster_id"}, "filter": {filterEquals("nodename", nodename)}, "limit": {"10"}}
	if err := s.client.GetJSON(ctx, "/nodes", query, &existing); err != nil {
		return NodeCreation{}, fmt.Errorf("check node %s: %w", nodename, err)
	}
	if len(existing.Data) > 0 {
		ids := make([]string, 0, len(existing.Data))
		for _, node := range existing.Data {
			ids = append(ids, node.Nodename+" "+node.NodeID+clusterSuffix(node.ClusterID))
		}
		return NodeCreation{}, fmt.Errorf("node %s already exists: %s; nothing was changed", nodename, strings.Join(ids, ", "))
	}
	body["nodename"] = nodename
	var created map[string]any
	if err := s.client.PostJSON(ctx, "/nodes", body, &created); err != nil {
		return NodeCreation{}, fmt.Errorf("create node %s: %w", nodename, err)
	}
	node := NodeReference{NodeID: propText(created, "node_id"), Nodename: propText(created, "nodename"), ClusterID: propText(created, "cluster_id")}
	if node.NodeID == "" {
		return NodeCreation{}, fmt.Errorf("create node %s: unexpected collector response", nodename)
	}
	result := NodeCreation{Provenance: s.newProvenance(), Node: node, Properties: map[string]string{}}
	for _, name := range NodeProperties {
		requested, asked := body[name].(string)
		if !asked && name != "app" && name != "team_responsible" {
			continue
		}
		applied := propText(created, name)
		result.Properties[name] = applied
		if asked && !samePropertyValue(name, requested, applied) {
			result.NotApplied = append(result.NotApplied, NodePropertyMismatch{Property: name, Requested: requested, Applied: applied})
		}
	}
	return result, nil
}

// UpdateNode sets properties of one node: a property absent from properties
// is left as it is, an empty value clears it.
func (s *Service) UpdateNode(ctx context.Context, node string, properties map[string]string) (NodeUpdate, error) {
	if len(properties) == 0 {
		return NodeUpdate{}, fmt.Errorf("pass at least one property to set")
	}
	body, err := s.nodePropertiesBody(ctx, properties, true)
	if err != nil {
		return NodeUpdate{}, err
	}
	props := []string{"node_id", "nodename", "cluster_id"}
	for _, name := range NodeProperties {
		if _, ok := body[name]; ok {
			props = append(props, name)
		}
	}
	before, err := s.lookupNodeRow(ctx, node, props)
	if err != nil {
		return NodeUpdate{}, err
	}
	reference := NodeReference{NodeID: propText(before, "node_id"), Nodename: propText(before, "nodename"), ClusterID: propText(before, "cluster_id")}
	var after map[string]any
	if err := s.client.PostJSON(ctx, "/nodes/"+url.PathEscape(reference.NodeID), body, &after); err != nil {
		return NodeUpdate{}, fmt.Errorf("update node %s: %w", reference.Nodename, err)
	}
	if propText(after, "node_id") != reference.NodeID {
		return NodeUpdate{}, fmt.Errorf("update node %s: unexpected collector response", reference.Nodename)
	}
	result := NodeUpdate{Provenance: s.newProvenance(), Node: reference, Changes: []NodeChange{}}
	for _, name := range props[3:] {
		requested := body[name].(string)
		applied := propText(after, name)
		result.Changes = append(result.Changes, NodeChange{Property: name, Before: propText(before, name), After: applied})
		if !samePropertyValue(name, requested, applied) {
			result.NotApplied = append(result.NotApplied, NodePropertyMismatch{Property: name, Requested: requested, Applied: applied})
		}
	}
	return result, nil
}

// DeleteNode deletes one node and its related entries from the collector.
func (s *Service) DeleteNode(ctx context.Context, node string) (NodeDeletion, error) {
	row, err := s.lookupNodeRow(ctx, node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return NodeDeletion{}, err
	}
	reference := NodeReference{NodeID: propText(row, "node_id"), Nodename: propText(row, "nodename"), ClusterID: propText(row, "cluster_id")}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, "/nodes/"+url.PathEscape(reference.NodeID), nil, &response); err != nil {
		return NodeDeletion{}, fmt.Errorf("delete node %s: %w", reference.Nodename, err)
	}
	return NodeDeletion{Provenance: s.newProvenance(), Node: reference, Deleted: true}, nil
}

// nodePropertiesBody validates the properties and gives the body the
// collector receives. The collector takes from a Manager an app or a group
// it does not know, and answers a malformed date with an internal error:
// the app and team_responsible must exist, and are sent as the collector
// names them; a date is YYYY-MM-DD. clear allows an empty value, which
// clears the property, but never for app and team_responsible.
func (s *Service) nodePropertiesBody(ctx context.Context, properties map[string]string, clear bool) (map[string]any, error) {
	body := map[string]any{}
	for name, value := range properties {
		if !slices.Contains(NodeProperties, name) {
			return nil, fmt.Errorf("unknown node property %s", name)
		}
		if value == "" {
			if !clear || name == "app" || name == "team_responsible" {
				return nil, fmt.Errorf("%s must not be empty", name)
			}
			body[name] = ""
			continue
		}
		if len(value) > maxNodePropertyBytes || !utf8.ValidString(value) || value != strings.TrimSpace(value) ||
			strings.ContainsFunc(value, func(r rune) bool { return r < ' ' || r == 0x7f }) {
			return nil, fmt.Errorf("%s must be at most %d bytes of printable text without surrounding spaces", name, maxNodePropertyBytes)
		}
		if slices.Contains(nodeDateProperties, name) {
			if _, err := time.Parse(time.DateOnly, value); err != nil {
				return nil, fmt.Errorf("%s must be a date, YYYY-MM-DD", name)
			}
		}
		body[name] = value
	}
	if app, ok := body["app"].(string); ok && app != "" {
		var found collectorApp
		if err := s.client.GetJSON(ctx, "/apps/"+url.PathEscape(app), url.Values{"props": {"id,app"}}, &found); err != nil || found.App == "" {
			if err != nil && !isNotFound(err) {
				return nil, fmt.Errorf("check app %s: %w", app, err)
			}
			return nil, fmt.Errorf("app %s is not an app of the collector, or not visible to the caller; get_app reads an app by its code", app)
		}
		body["app"] = found.App
	}
	if group, ok := body["team_responsible"].(string); ok && group != "" {
		found, err := s.lookupGroup(ctx, group)
		if err != nil {
			return nil, err
		}
		body["team_responsible"] = found.Role
	}
	return body, nil
}

// lookupNodeRow reads the props of one node by its exact name or node ID, as
// lookupNode does, keeping every prop it reads.
func (s *Service) lookupNodeRow(ctx context.Context, node string, props []string) (map[string]any, error) {
	found, err := s.lookupNode(ctx, node, []string{"node_id"})
	if err != nil {
		return nil, err
	}
	var response collectorList[map[string]any]
	if err := s.client.GetJSON(ctx, "/nodes/"+url.PathEscape(found.NodeID), url.Values{"props": {strings.Join(props, ",")}, "meta": {"false"}}, &response); err != nil {
		return nil, fmt.Errorf("get node %s: %w", node, err)
	}
	if len(response.Data) != 1 || propText(response.Data[0], "node_id") != found.NodeID {
		return nil, fmt.Errorf("get node %s: unexpected collector response", node)
	}
	return response.Data[0], nil
}

// samePropertyValue tells whether the collector holds the value asked for:
// it gives a date as a datetime, such as 2027-12-31 00:00:00.000, and
// compares app and group names without case.
func samePropertyValue(name, requested, applied string) bool {
	if slices.Contains(nodeDateProperties, name) && requested != "" {
		return strings.HasPrefix(applied, requested)
	}
	if name == "app" || name == "team_responsible" {
		return strings.EqualFold(requested, applied)
	}
	return requested == applied
}

func clusterSuffix(clusterID string) string {
	if clusterID == "" {
		return ""
	}
	return " (cluster " + clusterID + ")"
}

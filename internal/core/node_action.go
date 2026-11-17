package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/opensvc/oc3-mcp/internal/client"
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
}

// NodeUpdateOptions are an update of node properties: the selected nodes,
// and the properties to set, an empty value clearing one.
type NodeUpdateOptions struct {
	NodeSelection
	Properties map[string]string
}

type NodeUpdate struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Properties map[string]string `json:"properties" jsonschema:"the properties the call set, with the value asked, empty to clear"`
	NodeChangeSummary
	DetailsOmitted int `json:"details_omitted,omitempty" jsonschema:"the number of nodes set whose values before the update and not applied are left out, past the bound of a result; get_node reads a node"`
}

// maxUpdateDetailBytes bounds the values before an update that a result
// gives, under the bound of a tool result the agent reads: past it, the
// nodes set keep their outcome, not their values before.
const maxUpdateDetailBytes = 256 << 10

type NodeDeletion struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	NodeChangeSummary
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

// UpdateNodes sets properties of the selected nodes, one request per node:
// the collector has no bulk route, and an update is not atomic. A property
// absent from properties is left as it is, an empty value clears it; a node
// already holding every value asked is left as it is, nothing sent for it.
// A read the collector answers out of its API, before any update was sent,
// fails the call as incompatible; after one, it ends the call there.
func (s *Service) UpdateNodes(ctx context.Context, options NodeUpdateOptions) (NodeUpdate, error) {
	filters, err := checkSelection(options.NodeSelection)
	if err != nil {
		return NodeUpdate{}, err
	}
	if len(options.Properties) == 0 {
		return NodeUpdate{}, fmt.Errorf("pass at least one property to set")
	}
	body, err := s.nodePropertiesBody(ctx, options.Properties, true)
	if err != nil {
		return NodeUpdate{}, err
	}
	props := []string{"node_id"}
	for _, name := range NodeProperties {
		if _, ok := body[name]; ok {
			props = append(props, name)
		}
	}
	updates := nodeUpdates{body: body, props: props}
	updates.ctx, updates.stop = context.WithCancel(ctx)
	defer updates.stop()
	summary, err := s.changeSelectedNodes(updates.ctx, options.NodeSelection, filters, func(ctx context.Context, node collectorNode) NodeChangeOutcome {
		return s.updateOneNode(ctx, node, &updates)
	})
	if err != nil {
		return NodeUpdate{}, err
	}
	if updates.incompatible != nil && !updates.sent.Load() {
		return NodeUpdate{}, updates.incompatible
	}
	result := NodeUpdate{Provenance: s.newProvenance(), Properties: map[string]string{}, NodeChangeSummary: summary}
	for name, value := range body {
		result.Properties[name] = value.(string)
	}
	// The details of the nodes set, their values before and those not
	// applied, go up to their bound, node after node.
	size := 0
	for index := range result.Nodes {
		outcome := &result.Nodes[index]
		if outcome.Changes == nil && outcome.NotApplied == nil {
			continue
		}
		for _, change := range outcome.Changes {
			size += len(change.Property) + len(change.Before) + 32
		}
		for _, mismatch := range outcome.NotApplied {
			size += len(mismatch.Property) + len(mismatch.Requested) + len(mismatch.Applied) + 48
		}
		if size > maxUpdateDetailBytes {
			outcome.Changes, outcome.NotApplied = nil, nil
			result.DetailsOmitted++
		}
	}
	return result, nil
}

// nodeUpdates is the update the selected nodes share: its body, the columns
// read before it, and what the updates of the nodes found.
type nodeUpdates struct {
	body  map[string]any
	props []string
	ctx   context.Context
	stop  context.CancelFunc
	// sent: an update was sent for a node.
	sent atomic.Bool
	// incompatible: the first read answered out of the collector API.
	mutex        sync.Mutex
	incompatible error
}

// updateOneNode sets the properties of the update on one node: its values
// read first, nothing sent when it holds them already.
func (s *Service) updateOneNode(ctx context.Context, node collectorNode, updates *nodeUpdates) NodeChangeOutcome {
	outcome := NodeChangeOutcome{NodeID: node.NodeID, Nodename: node.Nodename}
	before, err := s.readNodeRow(ctx, node.NodeID, node.Nodename, updates.props)
	if err != nil {
		switch {
		case errors.Is(err, client.ErrIncompatible):
			// The collector moved on: no further node is read.
			updates.mutex.Lock()
			if updates.incompatible == nil {
				updates.incompatible = err
			}
			updates.mutex.Unlock()
			updates.stop()
			outcome.Outcome, outcome.Reason = ChangeNotAttempted, err.Error()
		case collectorRefused(err):
			outcome.Outcome, outcome.Reason = ChangeRefused, err.Error()
		default:
			outcome.Outcome, outcome.Reason = ChangeNotAttempted, err.Error()
		}
		return outcome
	}
	set := updates.props[1:]
	if !slices.ContainsFunc(set, func(name string) bool {
		return !samePropertyValue(name, updates.body[name].(string), propText(before, name))
	}) {
		outcome.Outcome = ChangeUnchanged
		return outcome
	}
	updates.sent.Store(true)
	var after map[string]any
	if err := s.client.PostJSON(ctx, "/nodes/"+url.PathEscape(node.NodeID), updates.body, &after); err != nil {
		if collectorRefused(err) {
			outcome.Outcome, outcome.Reason = ChangeRefused, fmt.Sprintf("update node %s: %v", node.Nodename, err)
			return outcome
		}
		outcome.Outcome, outcome.Reason = ChangeUnknown, fmt.Sprintf("update node %s: %v; get_node tells what the node holds", node.Nodename, err)
		return outcome
	}
	if propText(after, "node_id") != node.NodeID {
		outcome.Outcome, outcome.Reason = ChangeUnknown, fmt.Sprintf("update node %s: the collector answered out of its API; get_node tells what the node holds", node.Nodename)
		return outcome
	}
	outcome.Outcome, outcome.Changes = ChangeSet, []NodeChange{}
	for _, name := range set {
		requested, applied := updates.body[name].(string), propText(after, name)
		outcome.Changes = append(outcome.Changes, NodeChange{Property: name, Before: propText(before, name)})
		if !samePropertyValue(name, requested, applied) {
			outcome.NotApplied = append(outcome.NotApplied, NodePropertyMismatch{Property: name, Requested: requested, Applied: applied})
		}
	}
	return outcome
}

// collectorRefused tells whether err is a refusal of the collector, an
// answer of 4xx: the change was not made. Any other failure of a write,
// such as a timeout, leaves its outcome unknown.
func collectorRefused(err error) bool {
	var status interface{ HTTPStatus() int }
	return errors.As(err, &status) && status.HTTPStatus() >= 400 && status.HTTPStatus() < 500
}

// DeleteNodes deletes the selected nodes and their related entries from the
// collector, one request per node: the collector has no bulk route, and a
// deletion is not atomic. The nodes are named, or selected by filters, as for
// the tags: the user answers for what a filter selects. An unknown or
// ambiguous name refuses the whole call.
func (s *Service) DeleteNodes(ctx context.Context, selection NodeSelection) (NodeDeletion, error) {
	filters, err := checkSelection(selection)
	if err != nil {
		return NodeDeletion{}, err
	}
	summary, err := s.changeSelectedNodes(ctx, selection, filters, func(ctx context.Context, node collectorNode) NodeChangeOutcome {
		return s.deleteOneNode(ctx, node)
	})
	if err != nil {
		return NodeDeletion{}, err
	}
	return NodeDeletion{Provenance: s.newProvenance(), NodeChangeSummary: summary}, nil
}

// deleteOneNode deletes one node: refused on a 4xx of the collector, of an
// outcome unknown on any other failure, which get_node tells.
func (s *Service) deleteOneNode(ctx context.Context, node collectorNode) NodeChangeOutcome {
	outcome := NodeChangeOutcome{NodeID: node.NodeID, Nodename: node.Nodename, Outcome: ChangeDeleted}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, "/nodes/"+url.PathEscape(node.NodeID), nil, &response); err != nil {
		if collectorRefused(err) {
			outcome.Outcome, outcome.Reason = ChangeRefused, fmt.Sprintf("delete node %s: %v", node.Nodename, err)
		} else {
			outcome.Outcome, outcome.Reason = ChangeUnknown, fmt.Sprintf("delete node %s: %v; get_node tells whether it is still there", node.Nodename, err)
		}
	}
	return outcome
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

// readNodeRow reads the props of the node of nodeID, named name in errors,
// node_id among them.
func (s *Service) readNodeRow(ctx context.Context, nodeID, name string, props []string) (map[string]any, error) {
	var response collectorList[map[string]any]
	if err := s.client.GetJSON(ctx, "/nodes/"+url.PathEscape(nodeID), url.Values{"props": {strings.Join(props, ",")}, "meta": {"false"}}, &response); err != nil {
		return nil, fmt.Errorf("get node %s: %w", name, err)
	}
	if len(response.Data) != 1 || propText(response.Data[0], "node_id") != nodeID {
		return nil, fmt.Errorf("get node %s: %w", name, errUnexpectedResponse)
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

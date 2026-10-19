package core

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

const (
	// maxSelectionNodes bounds the nodes named in one call.
	maxSelectionNodes = 50
	// maxSelectionFiltered bounds the nodes a selection by filters holds.
	maxSelectionFiltered = 1000
	// selectionPage is the page size of the selection reads.
	selectionPage = 200
	// selectionWorkers is the number of changes the MCP sends at the same
	// time: the collector has no bulk route.
	selectionWorkers = 4
)

// Outcomes of a change on one node.
const (
	ChangeAttached        = "attached"
	ChangeAlreadyAttached = "already_attached"
	ChangeDetached        = "detached"
	ChangeAlreadyDetached = "already_detached"
	ChangeQueued          = "queued"
	ChangeRefused         = "refused"
	ChangeNotAttempted    = "not_attempted"
)

// NodeSelection selects the nodes of a change: Nodes, exact names or IDs,
// or Filters, at least one, never every node.
type NodeSelection struct {
	Nodes   []string
	Filters NodeFilters
}

// NodeChangeSummary is the outcome of a change on the selected nodes.
type NodeChangeSummary struct {
	Filterset *FiltersetReference `json:"filterset,omitempty" jsonschema:"the filterset the filterset filter resolved to"`
	FilterTag *TagReference       `json:"filter_tag,omitempty" jsonschema:"the tag the tag filter resolved to"`
	Selected  int                 `json:"selected" jsonschema:"the number of nodes selected"`
	Changed   int                 `json:"changed" jsonschema:"the number of nodes changed"`
	Unchanged int                 `json:"unchanged" jsonschema:"the number of nodes already as asked"`
	Refused   int                 `json:"refused" jsonschema:"the number of nodes the collector refused the change for"`
	Nodes     []NodeChangeOutcome `json:"nodes" jsonschema:"the outcome on each node, sorted by node name"`
}

type NodeChangeOutcome struct {
	NodeID    string `json:"node_id" jsonschema:"the collector node ID"`
	Nodename  string `json:"nodename" jsonschema:"the node name"`
	Outcome   string `json:"outcome" jsonschema:"attached, already_attached, detached, already_detached, queued, refused, or not_attempted when the call ended before"`
	Reason    string `json:"reason,omitempty" jsonschema:"with refused, the reason the collector gave"`
	ActionID  int    `json:"action_id,omitempty" jsonschema:"with queued, the ID of the action in the queue, which list_node_actions follows"`
	QueuedFor string `json:"queued_for,omitempty" jsonschema:"with queued, how the node reads its queue: feed, read by an om3 agent; pull or push, the means of the former agent"`
	Warning   string `json:"warning,omitempty" jsonschema:"with queued, why the action may never run, such as a node queued for pull, which an om3 agent never reads; tell the user"`
}

// checkSelection validates a selection without reading the collector, and
// gives the collector filters of a selection by filters.
func checkSelection(selection NodeSelection) ([]string, error) {
	byFilters := !reflect.DeepEqual(selection.Filters, NodeFilters{})
	switch {
	case len(selection.Nodes) > 0 && byFilters:
		return nil, fmt.Errorf("pass nodes or filters, not both")
	case len(selection.Nodes) == 0 && !byFilters:
		return nil, fmt.Errorf("pass the nodes, or at least one filter to select them")
	case len(selection.Nodes) > maxSelectionNodes:
		return nil, fmt.Errorf("pass at most %d nodes; select more nodes with filters", maxSelectionNodes)
	case !byFilters:
		return nil, nil
	}
	return selection.Filters.collectorFilters()
}

// changeSelectedNodes reads the selected nodes, then changes each with
// change, a few at a time: one request per node, not atomic. filters are
// those checkSelection gave.
func (s *Service) changeSelectedNodes(ctx context.Context, selection NodeSelection, filters []string, change func(context.Context, collectorNode) NodeChangeOutcome) (NodeChangeSummary, error) {
	var summary NodeChangeSummary
	var nodes []collectorNode
	if len(selection.Nodes) == 0 {
		path, filterTag, filterset, err := s.memberPath(ctx, selection.Filters.Tag, selection.Filters.Filterset, "nodes")
		if err != nil {
			return NodeChangeSummary{}, err
		}
		summary.FilterTag, summary.Filterset = filterTag, filterset
		if nodes, err = s.selectNodes(ctx, path, filters); err != nil {
			return NodeChangeSummary{}, err
		}
	} else {
		var err error
		if nodes, err = s.resolveNodes(ctx, selection.Nodes); err != nil {
			return NodeChangeSummary{}, err
		}
	}

	outcomes := make([]NodeChangeOutcome, len(nodes))
	work := make(chan int)
	var wait sync.WaitGroup
	for range min(selectionWorkers, len(nodes)) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range work {
				if ctx.Err() != nil {
					outcomes[index] = NodeChangeOutcome{NodeID: nodes[index].NodeID, Nodename: nodes[index].Nodename, Outcome: ChangeNotAttempted}
					continue
				}
				outcomes[index] = change(ctx, nodes[index])
			}
		}()
	}
	for index := range nodes {
		if ctx.Err() != nil {
			outcomes[index] = NodeChangeOutcome{NodeID: nodes[index].NodeID, Nodename: nodes[index].Nodename, Outcome: ChangeNotAttempted}
			continue
		}
		work <- index
	}
	close(work)
	wait.Wait()

	sort.SliceStable(outcomes, func(i, j int) bool {
		if outcomes[i].Nodename != outcomes[j].Nodename {
			return outcomes[i].Nodename < outcomes[j].Nodename
		}
		return outcomes[i].NodeID < outcomes[j].NodeID
	})
	for _, outcome := range outcomes {
		switch outcome.Outcome {
		case ChangeAttached, ChangeDetached, ChangeQueued:
			summary.Changed++
		case ChangeAlreadyAttached, ChangeAlreadyDetached:
			summary.Unchanged++
		case ChangeRefused:
			summary.Refused++
		}
	}
	summary.Selected, summary.Nodes = len(nodes), outcomes
	return summary, nil
}

// selectNodes reads the nodes the filters select, and refuses a selection
// too large, or that changed while it was read.
func (s *Service) selectNodes(ctx context.Context, path string, filters []string) ([]collectorNode, error) {
	var nodes []collectorNode
	expected := -1
	for offset := 0; ; offset += selectionPage {
		var response collectorList[collectorNode]
		if err := s.client.GetJSON(ctx, path, pageQuery([]string{"node_id", "nodename", "cluster_id"}, "nodename,node_id", filters, selectionPage, offset), &response); err != nil {
			return nil, fmt.Errorf("select nodes: %w", err)
		}
		if response.Meta.Total == nil || len(response.Data) > selectionPage {
			return nil, fmt.Errorf("select nodes: unexpected collector response")
		}
		total := *response.Meta.Total
		if expected < 0 {
			expected = total
		}
		if total != expected {
			return nil, fmt.Errorf("the selection changed while it was read, %d nodes then %d: nothing was changed", expected, total)
		}
		if total > maxSelectionFiltered {
			return nil, fmt.Errorf("the filters select %d nodes, more than %d: nothing was changed; narrow the filters", total, maxSelectionFiltered)
		}
		nodes = append(nodes, response.Data...)
		if len(response.Data) == 0 || len(nodes) >= total {
			break
		}
	}
	if len(nodes) != expected {
		return nil, fmt.Errorf("the selection changed while it was read, %d nodes instead of %d: nothing was changed", len(nodes), expected)
	}
	return nodes, nil
}

// resolveNodes reads the named nodes, exact names or node IDs, in one
// request each kind, and refuses the whole call when one is unknown or
// ambiguous, before anything is changed.
func (s *Service) resolveNodes(ctx context.Context, names []string) ([]collectorNode, error) {
	var ids, nodenames []string
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, ", /") {
			return nil, fmt.Errorf("nodes must be exact node names or node IDs, without commas, spaces or slashes")
		}
		if err := validateFilterValue("node", name); err != nil {
			return nil, err
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		if collectorUUIDPattern.MatchString(name) {
			ids = append(ids, name)
		} else {
			nodenames = append(nodenames, name)
		}
	}
	var rows []collectorNode
	for prop, values := range map[string][]string{"node_id": ids, "nodename": nodenames} {
		if len(values) == 0 {
			continue
		}
		// Every row is read, up to the total: a name several clusters hold
		// must not pass for unique because a page was cut.
		filter := []string{prop + ":in:" + strings.Join(values, ",")}
		read := 0
		for offset := 0; ; offset += selectionPage {
			var response collectorList[collectorNode]
			query := pageQuery([]string{"node_id", "nodename", "cluster_id"}, "nodename,node_id", filter, selectionPage, offset)
			if err := s.client.GetJSON(ctx, "/nodes", query, &response); err != nil {
				return nil, fmt.Errorf("get nodes: %w", err)
			}
			if response.Meta.Total == nil || *response.Meta.Total > maxSelectionFiltered || len(response.Data) > selectionPage {
				return nil, fmt.Errorf("get nodes: unexpected collector response")
			}
			rows = append(rows, response.Data...)
			read += len(response.Data)
			if len(response.Data) == 0 || read >= *response.Meta.Total {
				if read != *response.Meta.Total {
					return nil, fmt.Errorf("get nodes: the nodes changed while they were read: nothing was changed")
				}
				break
			}
		}
	}
	var nodes []collectorNode
	var missing, ambiguous []string
	added := map[string]bool{}
	for _, name := range append(ids, nodenames...) {
		var matches []collectorNode
		for _, row := range rows {
			if strings.EqualFold(row.NodeID, name) || strings.EqualFold(row.Nodename, name) {
				matches = append(matches, row)
			}
		}
		switch len(matches) {
		case 0:
			missing = append(missing, name)
		case 1:
			if !added[matches[0].NodeID] {
				added[matches[0].NodeID] = true
				nodes = append(nodes, matches[0])
			}
		default:
			matched := make([]string, 0, len(matches))
			for _, match := range matches {
				matched = append(matched, match.NodeID+clusterSuffix(match.ClusterID))
			}
			ambiguous = append(ambiguous, name+" ("+strings.Join(matched, ", ")+")")
		}
	}
	if len(missing) > 0 || len(ambiguous) > 0 {
		var problems []string
		if len(missing) > 0 {
			problems = append(problems, "not found or not visible: "+strings.Join(missing, ", ")+findObjectsHint)
		}
		if len(ambiguous) > 0 {
			problems = append(problems, "names several nodes hold, pass a node ID: "+strings.Join(ambiguous, "; "))
		}
		return nil, fmt.Errorf("nothing was changed; %s", strings.Join(problems, "; "))
	}
	return nodes, nil
}

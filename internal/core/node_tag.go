package core

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	// maxTagNodes bounds the nodes named in one call.
	maxTagNodes = 50
	// maxTagSelection bounds the nodes a selection by filters holds.
	maxTagSelection = 1000
	// tagSelectionPage is the page size of the selection reads.
	tagSelectionPage = 200
	// tagWorkers is the number of attachments or detachments the MCP sends
	// at the same time: the collector has no bulk route.
	tagWorkers = 4
)

// Outcomes of a tag change on one node.
const (
	TagAttached        = "attached"
	TagAlreadyAttached = "already_attached"
	TagDetached        = "detached"
	TagAlreadyDetached = "already_detached"
	TagRefused         = "refused"
	TagNotAttempted    = "not_attempted"
)

// NodeTagOptions select the nodes of a tag change: Nodes, exact names or
// IDs, or Filters, at least one.
type NodeTagOptions struct {
	Tag     string
	Nodes   []string
	Filters NodeFilters
	// AttachData is the data of the attachment, such as the fields of a
	// form, when attaching.
	AttachData *string
}

type NodeTagChange struct {
	Provenance Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Tag        TagReference        `json:"tag" jsonschema:"the tag"`
	Filterset  *FiltersetReference `json:"filterset,omitempty" jsonschema:"the filterset the filterset filter resolved to"`
	FilterTag  *TagReference       `json:"filter_tag,omitempty" jsonschema:"the tag the tag filter resolved to"`
	Selected   int                 `json:"selected" jsonschema:"the number of nodes selected"`
	Changed    int                 `json:"changed" jsonschema:"the number of nodes the tag was attached to or detached from"`
	Unchanged  int                 `json:"unchanged" jsonschema:"the number of nodes that already held the tag, or did not hold it"`
	Refused    int                 `json:"refused" jsonschema:"the number of nodes the collector refused the change for"`
	Nodes      []NodeTagOutcome    `json:"nodes" jsonschema:"the outcome on each node, sorted by node name"`
}

type NodeTagOutcome struct {
	NodeID   string `json:"node_id" jsonschema:"the collector node ID"`
	Nodename string `json:"nodename" jsonschema:"the node name"`
	Outcome  string `json:"outcome" jsonschema:"attached, already_attached, detached, already_detached, refused, or not_attempted when the call ended before"`
	Reason   string `json:"reason,omitempty" jsonschema:"with refused, the reason the collector gave"`
}

// AttachNodeTag attaches a tag to the selected nodes, one request per node:
// the collector has no bulk route, and a change is not atomic.
func (s *Service) AttachNodeTag(ctx context.Context, options NodeTagOptions) (NodeTagChange, error) {
	if options.AttachData != nil {
		if err := validateTagAttachData(*options.AttachData); err != nil {
			return NodeTagChange{}, err
		}
	}
	return s.changeNodeTag(ctx, options, true)
}

// DetachNodeTag detaches a tag from the selected nodes, one request per node.
func (s *Service) DetachNodeTag(ctx context.Context, options NodeTagOptions) (NodeTagChange, error) {
	if options.AttachData != nil {
		return NodeTagChange{}, fmt.Errorf("attach_data is for an attachment only")
	}
	return s.changeNodeTag(ctx, options, false)
}

func (s *Service) changeNodeTag(ctx context.Context, options NodeTagOptions, attach bool) (NodeTagChange, error) {
	if options.Tag == "" {
		return NodeTagChange{}, fmt.Errorf("tag must be one exact tag name or tag ID")
	}
	byFilters := !reflect.DeepEqual(options.Filters, NodeFilters{})
	switch {
	case len(options.Nodes) > 0 && byFilters:
		return NodeTagChange{}, fmt.Errorf("pass nodes or filters, not both")
	case len(options.Nodes) == 0 && !byFilters:
		return NodeTagChange{}, fmt.Errorf("pass the nodes, or at least one filter to select them")
	case len(options.Nodes) > maxTagNodes:
		return NodeTagChange{}, fmt.Errorf("pass at most %d nodes; select more nodes with filters", maxTagNodes)
	}
	var filters []string
	if byFilters {
		var err error
		if filters, err = options.Filters.collectorFilters(); err != nil {
			return NodeTagChange{}, err
		}
	}
	tag, err := s.lookupTag(ctx, options.Tag)
	if err != nil {
		return NodeTagChange{}, err
	}
	result := NodeTagChange{Tag: tag}
	var nodes []collectorNode
	if byFilters {
		path, filterTag, filterset, err := s.memberPath(ctx, options.Filters.Tag, options.Filters.Filterset, "nodes")
		if err != nil {
			return NodeTagChange{}, err
		}
		result.FilterTag, result.Filterset = filterTag, filterset
		if nodes, err = s.selectNodes(ctx, path, filters); err != nil {
			return NodeTagChange{}, err
		}
	} else if nodes, err = s.resolveNodes(ctx, options.Nodes); err != nil {
		return NodeTagChange{}, err
	}

	outcomes := make([]NodeTagOutcome, len(nodes))
	work := make(chan int)
	var wait sync.WaitGroup
	for range min(tagWorkers, len(nodes)) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range work {
				outcomes[index] = s.changeOneNodeTag(ctx, tag, nodes[index], attach, options.AttachData)
			}
		}()
	}
	for index, node := range nodes {
		if ctx.Err() != nil {
			outcomes[index] = NodeTagOutcome{NodeID: node.NodeID, Nodename: node.Nodename, Outcome: TagNotAttempted}
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
		case TagAttached, TagDetached:
			result.Changed++
		case TagAlreadyAttached, TagAlreadyDetached:
			result.Unchanged++
		case TagRefused:
			result.Refused++
		}
	}
	result.Selected, result.Nodes = len(nodes), outcomes
	result.Provenance = s.newProvenance()
	return result, nil
}

// changeOneNodeTag attaches or detaches the tag on one node. The collector
// answers an attachment or a detachment that changes nothing with 200 and
// an info saying so.
func (s *Service) changeOneNodeTag(ctx context.Context, tag TagReference, node collectorNode, attach bool, attachData *string) NodeTagOutcome {
	outcome := NodeTagOutcome{NodeID: node.NodeID, Nodename: node.Nodename}
	if ctx.Err() != nil {
		outcome.Outcome = TagNotAttempted
		return outcome
	}
	path := "/tags/" + url.PathEscape(tag.TagID) + "/nodes/" + url.PathEscape(node.NodeID)
	var response struct {
		Info string `json:"info"`
	}
	var err error
	if attach {
		body := map[string]any{}
		if attachData != nil {
			body["tag_attach_data"] = *attachData
		}
		err = s.client.PostJSON(ctx, path, body, &response)
	} else {
		err = s.client.DeleteJSON(ctx, path, &response)
	}
	info := strings.ToLower(response.Info)
	switch {
	case err != nil:
		outcome.Outcome, outcome.Reason = TagRefused, err.Error()
	case attach && strings.Contains(info, "already attached"):
		outcome.Outcome = TagAlreadyAttached
	case attach:
		outcome.Outcome = TagAttached
	case strings.Contains(info, "already detached"):
		outcome.Outcome = TagAlreadyDetached
	default:
		outcome.Outcome = TagDetached
	}
	return outcome
}

// selectNodes reads the nodes the filters select, and refuses a selection
// too large, or that changed while it was read.
func (s *Service) selectNodes(ctx context.Context, path string, filters []string) ([]collectorNode, error) {
	var nodes []collectorNode
	expected := -1
	for offset := 0; ; offset += tagSelectionPage {
		var response collectorList[collectorNode]
		if err := s.client.GetJSON(ctx, path, pageQuery([]string{"node_id", "nodename", "cluster_id"}, "nodename,node_id", filters, tagSelectionPage, offset), &response); err != nil {
			return nil, fmt.Errorf("select nodes: %w", err)
		}
		if response.Meta.Total == nil || len(response.Data) > tagSelectionPage {
			return nil, fmt.Errorf("select nodes: unexpected collector response")
		}
		total := *response.Meta.Total
		if expected < 0 {
			expected = total
		}
		if total != expected {
			return nil, fmt.Errorf("the selection changed while it was read, %d nodes then %d: nothing was changed", expected, total)
		}
		if total > maxTagSelection {
			return nil, fmt.Errorf("the filters select %d nodes, more than %d: nothing was changed; narrow the filters", total, maxTagSelection)
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
		var response collectorList[collectorNode]
		query := pageQuery([]string{"node_id", "nodename", "cluster_id"}, "nodename,node_id", []string{prop + ":in:" + strings.Join(values, ",")}, 2*maxTagNodes, 0)
		if err := s.client.GetJSON(ctx, "/nodes", query, &response); err != nil {
			return nil, fmt.Errorf("get nodes: %w", err)
		}
		rows = append(rows, response.Data...)
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

func validateTagAttachData(data string) error {
	if len(data) > 4096 || !utf8.ValidString(data) {
		return fmt.Errorf("attach_data must be at most 4096 bytes of text")
	}
	return nil
}

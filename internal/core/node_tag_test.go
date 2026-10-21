package core

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// tagCollector answers the tag lookup, the node reads and the changes, and
// gives a tag change the answer of change for its node ID.
func tagCollector(t *testing.T, nodes string, total int, change map[string]string) *fakeCollector {
	t.Helper()
	var mutex sync.Mutex
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		mutex.Lock()
		defer mutex.Unlock()
		switch {
		case path == "/tags":
			return `{"data":[{"tag_id":"t-1","tag_name":"tag-mcp"}]}`
		case path == "/nodes" && query != nil:
			if query.Get("offset") != "" && query.Get("offset") != "0" {
				return `{"data":[],"meta":{"total":` + itoa(total) + `}}`
			}
			return `{"data":` + nodes + `,"meta":{"total":` + itoa(total) + `}}`
		case strings.HasPrefix(path, "/tags/t-1/nodes/"):
			return change[strings.TrimPrefix(path, "/tags/t-1/nodes/")]
		}
		return `{"data":[]}`
	}
	return collector
}

func itoa(value int) string { return strconv.Itoa(value) }

func TestAttachNodeTagByFilters(t *testing.T) {
	nodes := `[{"node_id":"n-1","nodename":"b"},{"node_id":"n-2","nodename":"a"}]`
	collector := tagCollector(t, nodes, 2, map[string]string{"n-1": `{"info":"node 'n-1' tag 'tag-mcp' attached"}`, "n-2": `{"info":"tag 'tag-mcp' already attached to node 'n-2'"}`})
	change, err := newTestService(collector).AttachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", NodeSelection: NodeSelection{Filters: NodeFilters{Status: "In service"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Selected != 2 || change.Changed != 1 || change.Unchanged != 1 || change.Nodes[0].Nodename != "a" || change.Nodes[0].Outcome != ChangeAlreadyAttached || change.Nodes[1].Outcome != ChangeAttached {
		t.Fatalf("change = %+v", change)
	}
	for _, request := range collector.requests {
		if request.path == "/nodes" && request.query.Get("filter") != "status:eq:In service" {
			t.Fatalf("selection request = %+v", request)
		}
	}
	// A selection too large changes nothing.
	collector = tagCollector(t, nodes, 1001, nil)
	if _, err := newTestService(collector).AttachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", NodeSelection: NodeSelection{Filters: NodeFilters{Status: "In service"}}}); err == nil || !strings.Contains(err.Error(), "select 1001 nodes, more than 1000") {
		t.Fatalf("too large: %v", err)
	}
	for _, request := range collector.requests {
		if request.method != "GET" {
			t.Fatalf("a refused selection sent %+v", request)
		}
	}
	for _, options := range []NodeTagOptions{
		{Tag: "tag-mcp"}, {Tag: "tag-mcp", NodeSelection: NodeSelection{Nodes: []string{"a"}, Filters: NodeFilters{Status: "x"}}},
		{NodeSelection: NodeSelection{Nodes: []string{"a"}}}, {Tag: "tag-mcp", NodeSelection: NodeSelection{Nodes: make([]string, 51)}},
	} {
		collector := tagCollector(t, nodes, 2, nil)
		if _, err := newTestService(collector).AttachNodeTag(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestDetachNodeTagFromNamedNodes(t *testing.T) {
	nodes := `[{"node_id":"n-1","nodename":"a"},{"node_id":"n-2","nodename":"b"}]`
	collector := tagCollector(t, nodes, 2, map[string]string{"n-1": `{"info":"tag detached"}`, "n-2": `{"info":"tag already detached"}`})
	change, err := newTestService(collector).DetachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", NodeSelection: NodeSelection{Nodes: []string{"a", "B", "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Changed != 1 || change.Unchanged != 1 || change.Nodes[0].Outcome != ChangeDetached || change.Nodes[1].Outcome != ChangeAlreadyDetached {
		t.Fatalf("change = %+v", change)
	}
	var deletes int
	for _, request := range collector.requests {
		if request.method == "DELETE" {
			deletes++
		}
		if request.path == "/nodes" && request.query.Get("filter") != "nodename:in:a,B" {
			t.Fatalf("resolution request = %+v", request)
		}
	}
	if deletes != 2 {
		t.Fatalf("%d deletions", deletes)
	}
	// An unknown name refuses the whole call before any change.
	collector = tagCollector(t, `[{"node_id":"n-1","nodename":"a"}]`, 1, nil)
	if _, err := newTestService(collector).DetachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", NodeSelection: NodeSelection{Nodes: []string{"a", "nosuch"}}}); err == nil || !strings.Contains(err.Error(), "not found or not visible: nosuch") {
		t.Fatalf("unknown node: %v", err)
	}
	for _, request := range collector.requests {
		if request.method != "GET" {
			t.Fatalf("an unknown node sent %+v", request)
		}
	}
}

func TestAttachNodeTagReportsARefusal(t *testing.T) {
	collector := tagCollector(t, `[{"node_id":"n-1","nodename":"a"}]`, 1, nil)
	collector.answer = func(path string, query url.Values) string {
		switch path {
		case "/tags":
			return `{"data":[{"tag_id":"t-1","tag_name":"tag-mcp"}]}`
		case "/nodes":
			return `{"data":[{"node_id":"n-1","nodename":"a"}],"meta":{"total":1}}`
		}
		return `{}`
	}
	refusing := &refusingCollector{fakeCollector: collector}
	change, err := New(refusing).AttachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", NodeSelection: NodeSelection{Nodes: []string{"a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if change.Refused != 1 || change.Nodes[0].Outcome != ChangeRefused || !strings.Contains(change.Nodes[0].Reason, "not compatible") {
		t.Fatalf("change = %+v", change)
	}
}

// refusingCollector refuses every change, as the collector does a tag
// incompatible with the tags of the node.
type refusingCollector struct{ *fakeCollector }

func (refusingCollector) PostJSON(context.Context, string, any, any) error {
	return errString("HTTP 409 Conflict: tag 'tag-mcp' is not compatible with other tags attached to node 'n-1'")
}

type errString string

func (e errString) Error() string { return string(e) }

// A name whose homonym is on a second page of the resolution is refused as
// ambiguous, not taken for the node of the first page.
func TestResolveNodesReadsEveryPage(t *testing.T) {
	var first []string
	first = append(first, `{"node_id":"n-1","nodename":"a","cluster_id":"c1"}`)
	for i := 1; i < selectionPage; i++ {
		first = append(first, `{"node_id":"m-`+strconv.Itoa(i)+`","nodename":"b`+strconv.Itoa(i)+`"}`)
	}
	collector := &fakeCollector{answer: func(path string, query url.Values) string {
		if query.Get("offset") == strconv.Itoa(selectionPage) {
			return `{"data":[{"node_id":"n-2","nodename":"a","cluster_id":"c2"}],"meta":{"total":201}}`
		}
		return `{"data":[` + strings.Join(first, ",") + `],"meta":{"total":201}}`
	}}
	_, err := newTestService(collector).resolveNodes(t.Context(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "names several nodes hold") || !strings.Contains(err.Error(), "n-2 (cluster c2)") {
		t.Fatalf("homonym on the second page: %v", err)
	}
}

// The data of an attachment goes as given, any text: an empty one clears
// it, and none leaves it out of the request.
func TestAttachNodeTagSendsItsDataAsGiven(t *testing.T) {
	for _, data := range []*string{new(string), ptrTo("not json"), nil} {
		collector := tagCollector(t, `[{"node_id":"n-1","nodename":"a"}]`, 1, map[string]string{"n-1": `{"info":"node 'n-1' tag 'tag-mcp' attached"}`})
		if _, err := newTestService(collector).AttachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", AttachData: data, NodeSelection: NodeSelection{Filters: NodeFilters{Status: "In service"}}}); err != nil {
			t.Fatal(err)
		}
		posts := 0
		for _, request := range collector.requests {
			if request.method != "POST" {
				continue
			}
			posts++
			value, sent := request.body["tag_attach_data"]
			if (data == nil) == sent || (data != nil && value != *data) {
				t.Fatalf("data %v: request = %+v", data, request)
			}
		}
		if posts != 1 {
			t.Fatalf("data %v: %d posts", data, posts)
		}
	}
}

func ptrTo[T any](value T) *T { return &value }

func TestTagOutcome(t *testing.T) {
	for _, test := range []struct {
		err             error
		info            string
		attach          bool
		outcome, reason string
	}{
		{nil, "node 'n-1' tag 'x' attached", true, ChangeAttached, ""},
		{nil, "tag 'x' already attached to node 'n-1'", true, ChangeAlreadyAttached, ""},
		{nil, "tag detached", false, ChangeDetached, ""},
		{nil, "tag already detached", false, ChangeAlreadyDetached, ""},
		{errString("HTTP 409 Conflict: not compatible"), "", true, ChangeRefused, "HTTP 409 Conflict: not compatible"},
	} {
		if outcome, reason := tagOutcome(test.err, test.info, test.attach); outcome != test.outcome || reason != test.reason {
			t.Errorf("tagOutcome(%v, %q, %v) = %s, %q", test.err, test.info, test.attach, outcome, reason)
		}
	}
}

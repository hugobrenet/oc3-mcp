package core

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/client"
)

// eventsCollector answers the collector log as a collector past its count
// limit does: the rows of the page asked, no total.
func eventsCollector(rows int) *fakeCollector {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		if path != "/logs" {
			return `{"data":[]}`
		}
		offset, _ := strconv.Atoi(query.Get("offset"))
		limit, _ := strconv.Atoi(query.Get("limit"))
		var page []string
		for id := offset; id < min(offset+limit, rows); id++ {
			page = append(page, `{"id":`+strconv.Itoa(rows-id)+`,"log_date":"2026-11-16 12:00:00","log_action":"service.status","log_level":"info","log_user":"scheduler","log_fmt":"status","log_dict":"{}"}`)
		}
		return `{"data":[` + strings.Join(page, ",") + `],"meta":{}}`
	}
	return collector
}

// readEvents reads the log page after page, as a client follows next_cursor.
func readEvents(t *testing.T, collector *fakeCollector, limit int) []CollectorEventList {
	t.Helper()
	var pages []CollectorEventList
	cursor := ""
	for range 10 {
		page, err := newTestService(collector).ListCollectorEvents(t.Context(), ListCollectorEventsOptions{Limit: limit, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", len(pages)+1, err)
		}
		pages = append(pages, page)
		if !page.Truncated {
			return pages
		}
		if page.NextCursor == "" {
			t.Fatalf("page %d truncated without a cursor: %+v", len(pages), page)
		}
		cursor = page.NextCursor
	}
	t.Fatalf("no end after %d pages", len(pages))
	return nil
}

// A list the collector did not count, longer than its count limit, is read to
// its end by full pages: not counted is neither zero nor an error.
func TestAListNotCountedIsPagedToItsEnd(t *testing.T) {
	pages := readEvents(t, eventsCollector(5), 2)
	var counts []int
	for _, page := range pages {
		if page.Total != nil {
			t.Fatalf("a total the collector did not give: %+v", page)
		}
		counts = append(counts, page.Count)
	}
	if len(counts) != 3 || counts[0] != 2 || counts[1] != 2 || counts[2] != 1 {
		t.Fatalf("got pages of %v, want 2, 2 and 1", counts)
	}
}

// A list of exactly limit rows, not counted, ends on one empty page more: a
// full page tells only that rows may remain.
func TestAListNotCountedOfExactlyLimitRowsEndsOnAnEmptyPage(t *testing.T) {
	pages := readEvents(t, eventsCollector(4), 2)
	if len(pages) != 3 || pages[0].Count != 2 || pages[1].Count != 2 || !pages[1].Truncated || pages[2].Count != 0 || pages[2].Truncated {
		t.Fatalf("got %+v, want 2 and 2 rows, then an empty last page", pages)
	}
}

// A total the collector counted still ends the list without a page more.
func TestAListCountedEndsOnItsTotal(t *testing.T) {
	collector := eventsCollector(4)
	answer := collector.answer
	collector.answer = func(path string, query url.Values) string {
		return strings.Replace(answer(path, query), `"meta":{}`, `"meta":{"total":4}`, 1)
	}
	pages := readEvents(t, collector, 2)
	if len(pages) != 2 || deref(pages[1].Total) != 4 || pages[1].Truncated {
		t.Fatalf("got %+v, want two pages, the last one ending the list", pages)
	}
}

// A selection the collector did not count is beyond what it counts, so beyond
// the bound of a change: refused as too large, before anything is written.
func TestASelectionNotCountedChangesNothing(t *testing.T) {
	collector := tagCollector(t, `[{"node_id":"n-1","nodename":"a"}]`, 1, nil)
	answer := collector.answer
	collector.answer = func(path string, query url.Values) string {
		return strings.Replace(answer(path, query), `,"meta":{"total":1}`, ``, 1)
	}
	_, err := newTestService(collector).AttachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", NodeSelection: NodeSelection{Filters: NodeFilters{Status: "In service"}}})
	if err == nil || !strings.Contains(err.Error(), "or the collector did not count them: nothing was changed; narrow the filters") {
		t.Fatalf("got %v, want the selection refused as too large", err)
	}
	onlyReads(t, collector)
}

// onlyReads fails a test whose collector received anything but a read.
func onlyReads(t *testing.T, collector *fakeCollector) {
	t.Helper()
	for _, request := range collector.requests {
		if request.method != "GET" {
			t.Fatalf("a refused selection sent %+v", request)
		}
	}
}

// The selections of services, by filters, and of the instances of a node
// refuse the same, before anything is written.
func TestAServiceSelectionNotCountedChangesNothing(t *testing.T) {
	for name, test := range map[string]struct {
		services  string
		selection ServiceSelection
		want      string
	}{
		"services by filters": {`{"data":[{"svc_id":"s-1","svcname":"db"}]}`, ServiceSelection{Filters: ServiceFilters{App: "WEB"}}, "more than 1000 services, or the collector did not count them"},
		"instances of a node": {`{"data":[{"svc_id":"s-1","services.svcname":"db","services.cluster_id":"c1"}]}`, ServiceSelection{Node: "n1"}, "instances, or the collector did not count them"},
	} {
		t.Run(name, func(t *testing.T) {
			collector := serviceTagCollector(test.services, map[string]string{"s-1": `{"info":"service 's-1' tag 'backup-nightly' attached"}`})
			_, err := newTestService(collector).AttachServiceTag(t.Context(), ServiceTagOptions{Tag: "backup-nightly", ServiceSelection: test.selection})
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "nothing was changed; narrow the filters") {
				t.Fatalf("got %v, want the selection refused as too large", err)
			}
			onlyReads(t, collector)
		})
	}
}

// A count the collector did not give is said as such, the groups kept.
func TestACountNotGivenIsNull(t *testing.T) {
	count, err := newTestService(&fakeCollector{body: `{"data":{"os_name":{"Linux":3}},"meta":{}}`}).CountNodes(t.Context(), CountNodesOptions{GroupBy: "os_name"})
	if err != nil || count.Total != nil || len(count.Groups) != 1 || count.Groups[0].Count != 3 {
		t.Fatalf("count = %+v, %v", count, err)
	}
}

// A selection the collector answered out of its API, a page longer than
// asked, is an answer of another API, said refused before any write.
func TestASelectionOutOfTheAPIChangesNothing(t *testing.T) {
	nodes := make([]string, selectionPage+1)
	for i := range nodes {
		nodes[i] = `{"node_id":"n-` + strconv.Itoa(i) + `","nodename":"n` + strconv.Itoa(i) + `"}`
	}
	collector := tagCollector(t, "["+strings.Join(nodes, ",")+"]", len(nodes), nil)
	_, err := newTestService(collector).AttachNodeTag(t.Context(), NodeTagOptions{Tag: "tag-mcp", NodeSelection: NodeSelection{Filters: NodeFilters{Status: "In service"}}})
	if !errors.Is(err, client.ErrIncompatible) || !strings.Contains(err.Error(), "select nodes: unexpected collector response: nothing was changed") {
		t.Fatalf("got %v, want an answer of another API, nothing changed", err)
	}
	onlyReads(t, collector)
}

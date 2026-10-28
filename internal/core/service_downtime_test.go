package core

import (
	"net/url"
	"strings"
	"testing"
)

// newDowntimeCollector answers the status history of a service: a down
// period justified, a down period not justified, an up period, and the
// period going on, whose end is the collector time.
func newDowntimeCollector(justified bool) *fakeCollector {
	return &fakeCollector{answer: func(path string, _ url.Values) string {
		switch path {
		case "/services/web":
			return `{"data":[{"svc_id":"s-1","svcname":"web","cluster_id":"c-1"}]}`
		case "/services/s-1/status_log":
			ack := `null`
			if justified {
				ack = `{"comment":"maintenance asked by ops@example.com","account":false,"acked_by":"ops@example.com","acked_on":"2026-10-20 10:00:00"}`
			}
			return `{"data":[
				{"begin":"2026-10-03 00:46:03","end":"2026-10-03 00:46:18","status":"down","ack":` + ack + `},
				{"begin":"2026-10-03 00:46:18","end":"2026-10-03 01:06:18","status":"up"},
				{"begin":"2026-10-03 01:06:18","end":"2026-10-04 09:00:00","status":"down"},
				{"begin":"2026-10-04 09:00:00","end":"2026-10-20 10:28:07","status":"warn"}],
				"availability":{"from":"2026-10-03 00:46:03","to":"2026-10-20 10:28:07","rate":0.08,"available_s":1200,"excluded_s":15,"counted_s":1500000}}`
		}
		return `{"data":[]}`
	}}
}

func TestJustifyServiceDowntimeGivesThePeriodBack(t *testing.T) {
	collector := newDowntimeCollector(true)
	change, err := New(collector).JustifyServiceDowntime(t.Context(), JustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 00:46:03", Comment: " planned maintenance ", Counted: false})
	if err != nil {
		t.Fatal(err)
	}
	var put *fakeRequest
	for i, request := range collector.requests {
		if request.method == "PUT" {
			put = &collector.requests[i]
		}
	}
	if put == nil || put.path != "/services/s-1/status_log/ack" || put.body["begin"] != "2026-10-03 00:46:03" || put.body["end"] != "2026-10-03 00:46:18" ||
		put.body["comment"] != " planned maintenance " || put.body["account"] != false {
		t.Fatalf("put = %+v", put)
	}
	// The justification read back and the one replaced are masked.
	if change.Outcome != DowntimeReplaced || change.Previous == nil || strings.Contains(change.Previous.AckedBy+change.Previous.Comment, "@") ||
		change.Period.Ack == nil || strings.Contains(change.Period.Ack.AckedBy, "@") || change.Availability == nil || change.Availability.ExcludedSeconds != 15 {
		t.Fatalf("change = %+v", change)
	}
	for _, options := range []JustifyServiceDowntimeOptions{
		{Service: "web", Begin: "2026-10-04 09:00:00", Comment: "x"}, // going on
		{Service: "web", Begin: "2026-10-03 00:46:04", Comment: "x"}, // no such period
		{Service: "web", Begin: "2026-10-03T00:46:03", Comment: "x"}, // layout
	} {
		collector := newDowntimeCollector(false)
		if _, err := New(collector).JustifyServiceDowntime(t.Context(), options); err == nil {
			t.Fatalf("options %+v accepted", options)
		}
		for _, request := range collector.requests {
			if request.method == "PUT" {
				t.Fatalf("options %+v sent %+v", options, request)
			}
		}
	}
	collector = newDowntimeCollector(false)
	change, err = New(collector).JustifyServiceDowntime(t.Context(), JustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 01:06:18", Comment: "x", Counted: true})
	if err != nil || change.Outcome != DowntimeJustified || change.Previous != nil || change.Period.End != "2026-10-04 09:00:00" {
		t.Fatalf("change = %+v, %v", change, err)
	}
}

func TestUnjustifyServiceDowntime(t *testing.T) {
	collector := newDowntimeCollector(true)
	change, err := New(collector).UnjustifyServiceDowntime(t.Context(), UnjustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 00:46:03"})
	if err != nil {
		t.Fatal(err)
	}
	var deleted *fakeRequest
	for i, request := range collector.requests {
		if request.method == "DELETE" {
			deleted = &collector.requests[i]
		}
	}
	if deleted == nil || deleted.path != "/services/s-1/status_log/ack" || deleted.query.Get("begin") != "2026-10-03 00:46:03" || deleted.query.Get("end") != "2026-10-03 00:46:18" {
		t.Fatalf("delete = %+v", deleted)
	}
	if change.Outcome != DowntimeRemoved || change.Previous == nil {
		t.Fatalf("change = %+v", change)
	}
	// A period not justified is left as it is, without a request.
	collector = newDowntimeCollector(false)
	change, err = New(collector).UnjustifyServiceDowntime(t.Context(), UnjustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 00:46:03"})
	if err != nil || change.Outcome != DowntimeNotJustified {
		t.Fatalf("change = %+v, %v", change, err)
	}
	for _, request := range collector.requests {
		if request.method == "DELETE" {
			t.Fatalf("a period not justified sent %+v", request)
		}
	}
}

// A change made and not read back says so, without the availability read
// before it.
func TestJustifyServiceDowntimeTellsAReadBackFailure(t *testing.T) {
	collector := newDowntimeCollector(false)
	answer := collector.answer
	reads := 0
	collector.answer = func(path string, query url.Values) string {
		if path == "/services/s-1/status_log" {
			if reads++; reads > 1 {
				return `not json`
			}
		}
		return answer(path, query)
	}
	change, err := New(collector).JustifyServiceDowntime(t.Context(), JustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 00:46:03", Comment: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	if change.Outcome != DowntimeJustified || !change.ReadBackFailed || change.Availability != nil || change.Period.Ack != nil || !strings.Contains(change.Warning, "the change was made") {
		t.Fatalf("change = %+v", change)
	}
	// A period grown since it was read keeps the change, with a warning.
	collector = newDowntimeCollector(false)
	answer = collector.answer
	reads = 0
	collector.answer = func(path string, query url.Values) string {
		text := answer(path, query)
		if path == "/services/s-1/status_log" {
			if reads++; reads > 1 {
				text = strings.Replace(text, `"end":"2026-10-04 09:00:00"`, `"end":"2026-10-04 09:30:00"`, 1)
			}
		}
		return text
	}
	change, err = New(collector).JustifyServiceDowntime(t.Context(), JustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 01:06:18", Comment: "maintenance"})
	if err != nil || change.ReadBackFailed || change.Availability == nil || !strings.Contains(change.Warning, "no period of the history has these bounds") {
		t.Fatalf("change = %+v, %v", change, err)
	}
}

// A period the service was up is justified as the others, as the collector
// takes it: the rate leaves the up intervals out.
func TestJustifyServiceDowntimeTakesAPeriodUp(t *testing.T) {
	collector := newDowntimeCollector(false)
	if _, err := New(collector).JustifyServiceDowntime(t.Context(), JustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 00:46:18", Comment: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, request := range collector.requests {
		if request.method == "PUT" && request.body["begin"] == "2026-10-03 00:46:18" {
			return
		}
	}
	t.Fatalf("requests = %+v", collector.requests)
}

// With end, the period is named by its two bounds, sent as is: the period
// going on is justified on the bounds given, and a justification on bounds
// no period has any more is removed.
func TestServiceDowntimeTakesBothBounds(t *testing.T) {
	collector := newDowntimeCollector(false)
	if _, err := New(collector).JustifyServiceDowntime(t.Context(), JustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-04 09:00:00", End: "2026-10-20 10:28:07", Comment: "x"}); err != nil {
		t.Fatal(err)
	}
	if put := collector.requests[len(collector.requests)-2]; put.method != "PUT" || put.body["begin"] != "2026-10-04 09:00:00" || put.body["end"] != "2026-10-20 10:28:07" {
		t.Fatalf("requests = %+v", collector.requests)
	}
	// Bounds spanning two down periods are sent, and the warning says the
	// justification shows on none, not that a period grew.
	change, err := New(newDowntimeCollector(false)).JustifyServiceDowntime(t.Context(), JustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 00:46:03", End: "2026-10-04 09:00:00", Comment: "x"})
	if err != nil || !strings.Contains(change.Warning, "justify each by its begin") || strings.Contains(change.Warning, "grew") {
		t.Fatalf("change = %+v, %v", change, err)
	}
	// The period begun at 01:06:18 grew to 09:00:00: its justification,
	// ending at 02:00:00, shows on no period.
	collector = newDowntimeCollector(false)
	change, err = New(collector).UnjustifyServiceDowntime(t.Context(), UnjustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 01:06:18", End: "2026-10-03 02:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	var deleted *fakeRequest
	for i, request := range collector.requests {
		if request.method == "DELETE" {
			deleted = &collector.requests[i]
		}
	}
	if deleted == nil || deleted.query.Get("begin") != "2026-10-03 01:06:18" || deleted.query.Get("end") != "2026-10-03 02:00:00" || change.Outcome != DowntimeRemoved {
		t.Fatalf("change = %+v, requests %+v", change, collector.requests)
	}
	if _, err := New(newDowntimeCollector(false)).UnjustifyServiceDowntime(t.Context(), UnjustifyServiceDowntimeOptions{Service: "web", Begin: "2026-10-03 01:06:18", End: "02:00"}); err == nil {
		t.Fatal("an end out of layout was accepted")
	}
}

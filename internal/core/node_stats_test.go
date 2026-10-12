package core

import (
	"strings"
	"testing"
)

// The test service clock is 2026-10-12T09:00:00Z, unix 1791795600.
const testStatsBody = `{"data":[
	{"metric":"usr","points":[[1791792000,10],[1791792600,30],[1791793200,null],[1791793800,20]]},
	{"metric":"idle","points":[[1791792000,90],[1791792600,70],[1791793800,80]]},
	{"metric":"steal","points":[]}]}`

func TestGetNodeStatsSumsTheSeriesUp(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":    `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-1/stats": testStatsBody,
	}}
	stats, err := newTestService(collector).GetNodeStats(t.Context(), GetNodeStatsOptions{Node: "dev2n1", Group: "cpu", Samples: 2})
	if err != nil {
		t.Fatal(err)
	}
	if request := collector.requests[1]; request.query.Get("group") != "cpu" || request.query.Get("days") != "1" {
		t.Fatalf("request = %+v", request)
	}
	if stats.Group != "cpu" || stats.Days != 1 || stats.TotalSeries != 3 || stats.Count != 3 || stats.Devices != nil || stats.Truncated {
		t.Fatalf("stats = %+v", stats)
	}
	idle, steal, usr := stats.Series[0], stats.Series[1], stats.Series[2]
	if usr.Metric != "usr" || usr.Points != 3 || *usr.Last != 20 || *usr.Min != 10 || *usr.Max != 30 || *usr.Avg != 20 ||
		usr.MaxAt != "2026-10-12T08:10:00Z" || usr.FirstAt != "2026-10-12T08:00:00Z" || usr.LastAt != "2026-10-12T08:30:00Z" {
		t.Fatalf("usr = %+v", usr)
	}
	if len(usr.Samples) != 2 || usr.Samples[0].Value != 20 || usr.Samples[1].Value != 20 || usr.Samples[0].At != "2026-10-12T08:00:00Z" {
		t.Fatalf("usr samples = %+v", usr.Samples)
	}
	if steal.Points != 0 || steal.Last != nil || steal.Samples != nil || idle.Metric != "idle" {
		t.Fatalf("steal = %+v, idle = %+v", steal, idle)
	}
}

func TestGetNodeStatsReadsTheValueAtATime(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":    `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-1/stats": testStatsBody,
	}}
	stats, err := newTestService(collector).GetNodeStats(t.Context(), GetNodeStatsOptions{Node: "dev2n1", Group: "cpu", Metric: "usr", At: "2026-10-12T08:12:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	usr := stats.Series[0]
	if stats.At != "2026-10-12T08:12:00Z" || stats.Count != 1 || *usr.AtValue != 30 || usr.AtPoint != "2026-10-12T08:10:00Z" {
		t.Fatalf("stats = %+v, usr = %+v", stats, usr)
	}
	// A time older than the default day extends the days read.
	collector = &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":    `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-1/stats": `{"data":[]}`,
	}}
	stats, err = newTestService(collector).GetNodeStats(t.Context(), GetNodeStatsOptions{Node: "dev2n1", Group: "mem", At: "2026-10-02T09:00:00+02:00"})
	if err != nil || collector.requests[1].query.Get("days") != "11" || stats.Days != 11 || stats.At != "2026-10-02T07:00:00Z" {
		t.Fatalf("old time: %+v, %v, %+v", stats, err, collector.requests)
	}
}

func TestGetNodeStatsFiltersDevicesAndBoundsTheSeries(t *testing.T) {
	var series []string
	for index := range maxNodeStatsSeries + 10 {
		series = append(series, `{"metric":"pct_util","device":"disk`+string(rune('a'+index%26))+strings.Repeat("x", index/26)+`","points":[[1791792000,1]]}`)
	}
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":    `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-1/stats": `{"data":[` + strings.Join(series, ",") + `]}`,
	}}
	stats, err := newTestService(collector).GetNodeStats(t.Context(), GetNodeStatsOptions{Node: "dev2n1", Group: "blockdev", Days: 30})
	if err != nil || !stats.Truncated || stats.Count != maxNodeStatsSeries || len(stats.Devices) != maxNodeStatsSeries+10 {
		t.Fatalf("blockdev: count %d truncated %v devices %d, %v", stats.Count, stats.Truncated, len(stats.Devices), err)
	}
	stats, err = newTestService(collector).GetNodeStats(t.Context(), GetNodeStatsOptions{Node: "dev2n1", Group: "blockdev", Device: "diskb"})
	if err != nil || stats.Count != 1 || stats.Series[0].Device != "diskb" || stats.Truncated {
		t.Fatalf("device filter: %+v, %v", stats, err)
	}
}

func TestGetNodeStatsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []GetNodeStatsOptions{
		{Node: "dev2n1"},
		{Node: "dev2n1", Group: "disk"},
		{Node: "dev2n1", Group: "cpu", Days: 1096},
		{Node: "dev2n1", Group: "blockdev", Days: 31},
		{Node: "dev2n1", Group: "cpu", Samples: 49},
		{Node: "dev2n1", Group: "cpu", Device: "sda"},
		{Node: "dev2n1", Group: "cpu", At: "yesterday"},
		{Node: "dev2n1", Group: "cpu", At: "2026-10-13T09:00:00Z"},
		{Node: "dev2n1", Group: "blockdev", At: "2026-08-01T00:00:00Z"},
		{Node: "a/b", Group: "cpu"},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).GetNodeStats(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{bodies: map[string]string{"/nodes/dev2n1": `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`, "/nodes/n-1/stats": `{}`}}
	if _, err := newTestService(collector).GetNodeStats(t.Context(), GetNodeStatsOptions{Node: "dev2n1", Group: "cpu"}); err == nil {
		t.Fatal("a response without data was accepted")
	}
}

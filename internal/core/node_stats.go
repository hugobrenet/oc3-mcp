package core

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultNodeStatsDays    = 1
	maxNodeStatsDays        = 1095
	maxNodeDeviceStatsDays  = 30
	maxNodeStatsSeries      = 200
	maxNodeStatsSamples     = 48
	maxNodeStatsDeviceNames = 500
)

// nodeStatsGroups are the statistics groups of the collector; the device
// groups hold one series per device and metric.
var nodeStatsGroups = map[string]bool{
	"cpu": false, "mem": false, "swap": false, "proc": false, "block": false,
	"netdev": true, "netdev_err": true, "blockdev": true,
}

type GetNodeStatsOptions struct {
	Node    string
	Group   string
	Days    int
	Metric  string
	Device  string
	Samples int
	// At asks for the values of the series at one time, RFC 3339.
	At string
}

type NodeStats struct {
	Provenance  Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node        NodeReference     `json:"node" jsonschema:"the node whose statistics are summed up"`
	Group       string            `json:"group" jsonschema:"the statistics group"`
	Days        int               `json:"days" jsonschema:"the days of history read"`
	At          string            `json:"at,omitempty" jsonschema:"the UTC time asked with at"`
	TotalSeries int               `json:"total_series" jsonschema:"the number of series the collector returned for the group, before the metric and device filters"`
	Count       int               `json:"count" jsonschema:"the number of series returned"`
	Series      []NodeStatsSeries `json:"series" jsonschema:"one summary per metric, and per device for the device groups, sorted by device then metric"`
	Devices     []string          `json:"devices,omitempty" jsonschema:"for a device group, the devices having series, to pass as device"`
	Truncated   bool              `json:"truncated" jsonschema:"whether series were left out after 200; narrow with metric or device"`
}

type NodeStatsSeries struct {
	Metric  string          `json:"metric" jsonschema:"the metric, as the collector names it after sar, such as usr or pct_memused; its name tells its unit"`
	Device  string          `json:"device,omitempty" jsonschema:"the device of the series, for the device groups"`
	Points  int             `json:"points" jsonschema:"the number of points having a value over the period"`
	FirstAt string          `json:"first_at,omitempty" jsonschema:"UTC time of the first point having a value"`
	LastAt  string          `json:"last_at,omitempty" jsonschema:"UTC time of the last point having a value"`
	Last    *float64        `json:"last" jsonschema:"the last value; null without points"`
	Min     *float64        `json:"min" jsonschema:"the lowest value over the period"`
	Avg     *float64        `json:"avg" jsonschema:"the mean of the values over the period"`
	Max     *float64        `json:"max" jsonschema:"the highest value over the period"`
	MaxAt   string          `json:"max_at,omitempty" jsonschema:"UTC time of the highest value"`
	AtValue *float64        `json:"at_value,omitempty" jsonschema:"with at, the value of the point closest to the time asked"`
	AtPoint string          `json:"at_point,omitempty" jsonschema:"with at, the UTC time of that point; its distance to the time asked depends on the resolution of the period"`
	Samples []NodeStatPoint `json:"samples,omitempty" jsonschema:"with samples, the means of equal time slices of the period, oldest first"`
}

type NodeStatPoint struct {
	At    string  `json:"at" jsonschema:"UTC start time of the slice"`
	Value float64 `json:"value" jsonschema:"the mean of the values of the slice"`
}

type collectorNodeStats struct {
	Data []struct {
		Metric string        `json:"metric"`
		Device string        `json:"device"`
		Points [][2]*float64 `json:"points"`
	} `json:"data"`
}

// GetNodeStats reads the performance statistics of one node for one group
// over the last days, and sums each series up: last, min, mean and max values,
// with the means of equal slices of the period on demand.
func (s *Service) GetNodeStats(ctx context.Context, options GetNodeStatsOptions) (NodeStats, error) {
	perDevice, known := nodeStatsGroups[options.Group]
	if !known {
		return NodeStats{}, fmt.Errorf("group must be one of cpu, mem, swap, proc, block, netdev, netdev_err, blockdev")
	}
	days := options.Days
	if days == 0 {
		days = defaultNodeStatsDays
	}
	maxDays := maxNodeStatsDays
	if perDevice {
		maxDays = maxNodeDeviceStatsDays
	}
	var at time.Time
	if options.At != "" {
		parsed, err := time.Parse(time.RFC3339, options.At)
		if err != nil {
			return NodeStats{}, fmt.Errorf("at must be an RFC 3339 time, such as 2026-10-12T14:30:00Z")
		}
		age := s.now().Sub(parsed)
		if age < 0 {
			return NodeStats{}, fmt.Errorf("at must not be in the future")
		}
		// Read enough days to cover the time asked.
		days = max(days, int(age/(24*time.Hour))+1)
		at = parsed
	}
	if days < 1 || days > maxDays {
		return NodeStats{}, fmt.Errorf("days must be between 1 and %d for the %s group; at reads the days up to the time asked", maxDays, options.Group)
	}
	if options.Samples < 0 || options.Samples > maxNodeStatsSamples {
		return NodeStats{}, fmt.Errorf("samples must be between 0 and %d", maxNodeStatsSamples)
	}
	for name, value := range map[string]string{"metric": options.Metric, "device": options.Device} {
		if err := validateFilterValue(name, value); err != nil {
			return NodeStats{}, err
		}
	}
	if options.Device != "" && !perDevice {
		return NodeStats{}, fmt.Errorf("device applies to the netdev, netdev_err and blockdev groups only")
	}
	node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return NodeStats{}, err
	}
	query := url.Values{}
	query.Set("group", options.Group)
	query.Set("days", strconv.Itoa(days))
	var response collectorNodeStats
	if err := s.client.GetJSON(ctx, "/nodes/"+url.PathEscape(node.NodeID)+"/stats", query, &response); err != nil {
		return NodeStats{}, fmt.Errorf("get %s statistics of node %s: %w", options.Group, node.Nodename, err)
	}
	if response.Data == nil {
		return NodeStats{}, fmt.Errorf("get %s statistics of node %s: unexpected collector response", options.Group, node.Nodename)
	}

	sort.SliceStable(response.Data, func(i, j int) bool {
		a, b := response.Data[i], response.Data[j]
		if a.Device != b.Device {
			return a.Device < b.Device
		}
		return a.Metric < b.Metric
	})
	result := NodeStats{
		Node:  NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID},
		Group: options.Group, Days: days, TotalSeries: len(response.Data), Series: []NodeStatsSeries{},
	}
	if !at.IsZero() {
		result.At = at.UTC().Format(time.RFC3339)
	}
	devices := make(map[string]bool)
	for _, series := range response.Data {
		if series.Device != "" && len(devices) < maxNodeStatsDeviceNames {
			devices[series.Device] = true
		}
		if (options.Metric != "" && series.Metric != options.Metric) || (options.Device != "" && series.Device != options.Device) {
			continue
		}
		if len(result.Series) >= maxNodeStatsSeries {
			result.Truncated = true
			continue
		}
		result.Series = append(result.Series, summarizeNodeStatsSeries(series.Metric, series.Device, series.Points, options.Samples, at))
	}
	if perDevice {
		result.Devices = sortedKeys(devices)
	}
	result.Count = len(result.Series)
	result.Provenance = s.newProvenance()
	return result, nil
}

func summarizeNodeStatsSeries(metric string, device string, points [][2]*float64, samples int, asked time.Time) NodeStatsSeries {
	summary := NodeStatsSeries{Metric: metric, Device: strings.TrimSpace(device)}
	type point struct {
		at    int64
		value float64
	}
	values := make([]point, 0, len(points))
	for _, p := range points {
		if p[0] == nil || p[1] == nil {
			continue
		}
		values = append(values, point{at: int64(*p[0]), value: *p[1]})
	}
	summary.Points = len(values)
	if len(values) == 0 {
		return summary
	}
	at := func(seconds int64) string { return time.Unix(seconds, 0).UTC().Format(time.RFC3339) }
	first, last := values[0], values[len(values)-1]
	lowest, highest, total := first.value, first, 0.0
	for _, p := range values {
		lowest = min(lowest, p.value)
		if p.value > highest.value {
			highest = p
		}
		total += p.value
	}
	mean := roundStat(total / float64(len(values)))
	lastValue, highestValue := last.value, highest.value
	summary.FirstAt, summary.LastAt, summary.MaxAt = at(first.at), at(last.at), at(highest.at)
	summary.Last, summary.Min, summary.Avg, summary.Max = &lastValue, &lowest, &mean, &highestValue
	if !asked.IsZero() {
		closest := values[0]
		for _, p := range values {
			if absDuration(p.at-asked.Unix()) < absDuration(closest.at-asked.Unix()) {
				closest = p
			}
		}
		value := closest.value
		summary.AtValue, summary.AtPoint = &value, at(closest.at)
	}
	if samples > 0 {
		span := last.at - first.at + 1
		width := (span + int64(samples) - 1) / int64(samples)
		sums, counts := make([]float64, samples), make([]int, samples)
		for _, p := range values {
			slice := int((p.at - first.at) / width)
			sums[slice] += p.value
			counts[slice]++
		}
		for slice := range samples {
			if counts[slice] > 0 {
				summary.Samples = append(summary.Samples, NodeStatPoint{At: at(first.at + int64(slice)*width), Value: roundStat(sums[slice] / float64(counts[slice]))})
			}
		}
	}
	return summary
}

// roundStat keeps three decimals of a computed mean.
func roundStat(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func absDuration(seconds int64) int64 {
	if seconds < 0 {
		return -seconds
	}
	return seconds
}

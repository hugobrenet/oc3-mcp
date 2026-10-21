package core

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// thresholdCollector answers the reads of the thresholds of the check
// fs_u:/var of node dev2n1 and of its service checks.
func thresholdCollector(settings string) *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		filters := query["filter"]
		switch path {
		case "/nodes/dev2n1":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		case "/services/test%2Fsvc%2Fmq":
			return `{"data":[{"svc_id":"s-1","svcname":"test/svc/mq"}]}`
		case "/checks/live":
			if slices.Contains(filters, "chk_instance:eq:/var") && slices.Contains(filters, "svc_id:empty") {
				// The collector compares case-insensitively: /VAR is
				// another check.
				return `{"data":[{"id":9,"node_id":"n-1","svc_id":"","chk_type":"fs_u","chk_instance":"/VAR","chk_value":50,"chk_low":0,"chk_high":90,"chk_err":0},
					{"id":7,"node_id":"n-1","svc_id":"","chk_type":"fs_u","chk_instance":"/var","chk_value":96,"chk_low":0,"chk_high":95,"chk_err":1,"chk_threshold_provider":"settings"}],"meta":{"total":2}}`
			}
			if !slices.ContainsFunc(filters, func(filter string) bool { return strings.HasPrefix(filter, "chk_instance:") }) {
				return `{"data":[{"svc_id":"","chk_type":"fs_u","chk_instance":"/"},{"svc_id":"","chk_type":"fs_u","chk_instance":"/var"},
					{"svc_id":"s-1","chk_type":"fs_u","chk_instance":"/srv/mq"}],"meta":{"total":60}}`
			}
			return `{"data":[],"meta":{"total":0}}`
		case "/nodes/n-1/services":
			return `{"data":[{"svc_id":"s-1","services.svcname":"test/svc/mq"}]}`
		case "/checks/settings":
			return settings
		case "/checks/contextual_settings":
			return `{"data":[
				{"id":12,"fset_id":40,"chk_type":"fs_u","chk_instance":"/var","chk_low":0,"chk_high":80,"gen_filtersets.fset_name":"other-nodes"},
				{"id":11,"fset_id":41,"chk_type":"fs_u","chk_instance":"^/v","chk_low":0,"chk_high":85,"gen_filtersets.fset_name":"linux"},
				{"id":10,"fset_id":42,"chk_type":"fs_u","chk_instance":"/var$","chk_low":0,"chk_high":70,"gen_filtersets.fset_name":"all"},
				{"id":9,"fset_id":43,"chk_type":"fs_u","chk_instance":"^/tmp","chk_low":0,"chk_high":60,"gen_filtersets.fset_name":"tmp"}],"meta":{"total":4}}`
		case "/filtersets/40/nodes":
			return `{"data":[],"meta":{"total":0}}`
		case "/filtersets/41/nodes", "/filtersets/42/nodes":
			return `{"data":[{"node_id":"n-1"}],"meta":{"total":1}}`
		case "/checks/defaults":
			return `{"data":[
				{"id":4,"chk_type":"fs_u","chk_inst":"","chk_low":0,"chk_high":98,"chk_prio":0},
				{"id":55,"chk_type":"fs_u","chk_inst":".*\\.free","chk_low":1500,"chk_high":null,"chk_prio":0},
				{"id":30,"chk_type":"fs_u","chk_inst":"/V.*","chk_low":0,"chk_high":92,"chk_prio":0},
				{"id":31,"chk_type":"fs_u","chk_inst":"/var|/tmp","chk_low":0,"chk_high":93,"chk_prio":1},
				{"id":32,"chk_type":"FS_U","chk_inst":"","chk_low":0,"chk_high":1,"chk_prio":9}],"meta":{"total":5}}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
}

const varSetting = `{"data":[{"id":3,"node_id":"n-1","svc_id":"","chk_type":"fs_u","chk_instance":"/var","chk_low":0,"chk_high":95,
	"chk_changed":"2026-10-21 10:00:00","chk_changed_by":"jane@example.com","nodes.nodename":"dev2n1"}],"meta":{"total":1}}`

func TestGetCheckThresholdExplainsTheSources(t *testing.T) {
	collector := thresholdCollector(varSetting)
	details, err := newTestService(collector).GetCheckThreshold(t.Context(), CheckKeyOptions{Node: "dev2n1", Type: "fs_u", Instance: "/var"})
	if err != nil {
		t.Fatal(err)
	}
	if details.Check.ID != 7 || details.Check.ThresholdSource != "settings" || !details.Check.OutOfBounds || details.Check.Nodename != "dev2n1" {
		t.Fatalf("check = %+v", details.Check)
	}
	if len(details.Settings) != 1 || details.Settings[0].Instance != "/var" || strings.Contains(details.Settings[0].ChangedBy, "@") || *details.Settings[0].High != 95 {
		t.Fatalf("settings = %+v", details.Settings)
	}
	// The pattern of 9 does not match; 12 does, but its filterset does not
	// hold the node: 11, the most recent that applies, wins over the more
	// precise 10.
	contextual := details.Contextual
	if len(contextual) != 3 || contextual[0].ID != 12 || *contextual[0].Applies || contextual[0].Reason != "the filterset does not hold the node" ||
		contextual[1].ID != 11 || !*contextual[1].Applies || !contextual[1].Wins || contextual[2].ID != 10 || contextual[2].Wins {
		t.Fatalf("contextual = %+v", contextual)
	}
	// The defaults matching /var, case-insensitively: 31 of the highest
	// priority, its alternation anchored as the collector concatenates it,
	// then 30, the longest pattern, then 4; not 55, and not 32 of another
	// spelling of the type.
	var ids []int64
	for _, item := range details.Defaults {
		ids = append(ids, item.ID)
	}
	if !slices.Equal(ids, []int64{31, 30, 4}) || !details.Defaults[0].Wins || details.Defaults[1].Wins {
		t.Fatalf("defaults = %v", ids)
	}
	if details.Without.Source != "fset:linux" || *details.Without.High != 85 {
		t.Fatalf("without = %+v", details.Without)
	}
	for _, request := range collector.requests {
		if request.path == "/checks/settings" && !slices.Equal(request.query["filter"], []string{"node_id:eq:n-1", "chk_type:eq:fs_u", "chk_instance:eq:/var"}) {
			t.Fatalf("settings request = %+v", request)
		}
		if request.path == "/filtersets/41/nodes" && request.query.Get("filter") != "node_id:eq:n-1" {
			t.Fatalf("membership request = %+v", request)
		}
	}
}

func TestGetCheckThresholdTellsTheInstancesOfAnUnknownCheck(t *testing.T) {
	_, err := newTestService(thresholdCollector(varSetting)).GetCheckThreshold(t.Context(), CheckKeyOptions{Node: "dev2n1", Type: "fs_u", Instance: "/nosuch"})
	if err == nil || err.Error() != `node dev2n1 reports no fs_u check of instance "/nosuch"; the instances of its fs_u checks are: "/", "/var", "/srv/mq" (service test/svc/mq), and 57 more` {
		t.Fatalf("unknown check: %v", err)
	}
	if _, err := newTestService(thresholdCollector(varSetting)).GetCheckThreshold(t.Context(), CheckKeyOptions{Node: "dev2n1", Instance: "/var"}); err == nil {
		t.Fatal("a check without type was accepted")
	}
}

func TestListCheckThresholdsByScope(t *testing.T) {
	collector := thresholdCollector(varSetting)
	list, err := newTestService(collector).ListCheckThresholds(t.Context(), ListCheckThresholdsOptions{Scope: CheckThresholdSettings, Node: "dev2n1", Type: "fs_u"})
	if err != nil {
		t.Fatal(err)
	}
	last := collector.requests[len(collector.requests)-1]
	if list.Total != 1 || list.Thresholds[0].Nodename != "dev2n1" || list.Thresholds[0].Pattern != nil || strings.Contains(list.Thresholds[0].ChangedBy, "@") ||
		!slices.Equal(last.query["filter"], []string{"node_id:eq:n-1", "chk_type:eq:fs_u"}) {
		t.Fatalf("settings = %+v, request = %+v", list, last)
	}
	list, err = newTestService(thresholdCollector(varSetting)).ListCheckThresholds(t.Context(), ListCheckThresholdsOptions{Scope: CheckThresholdContextual})
	if err != nil || list.Count != 4 || list.Thresholds[0].Filterset != "other-nodes" || *list.Thresholds[0].Pattern != "/var" || list.Thresholds[0].Instance != "" {
		t.Fatalf("contextual = %+v, %v", list, err)
	}
	list, err = newTestService(thresholdCollector(varSetting)).ListCheckThresholds(t.Context(), ListCheckThresholdsOptions{Scope: CheckThresholdDefaults})
	if err != nil || list.Count != 5 || *list.Thresholds[0].Pattern != "" || list.Thresholds[1].High != nil {
		t.Fatalf("defaults = %+v, %v", list, err)
	}
	for _, options := range []ListCheckThresholdsOptions{{}, {Scope: "live"}, {Scope: CheckThresholdDefaults, Node: "dev2n1"}, {Scope: CheckThresholdSettings, Filterset: "linux"}} {
		collector := thresholdCollector(varSetting)
		if _, err := newTestService(collector).ListCheckThresholds(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

// statusError is a collector error with its HTTP status and message.
type statusError struct {
	status int
	text   string
}

func (e statusError) Error() string   { return "HTTP " + http.StatusText(e.status) + ": " + e.text }
func (e statusError) HTTPStatus() int { return e.status }
func (e statusError) Message() string { return e.text }
func (e statusError) NotFound() bool  { return e.status == http.StatusNotFound }

// thresholdWriteCollector answers the changes of thresholds with err, after
// recording them.
type thresholdWriteCollector struct {
	*fakeCollector
	err error
}

func (c thresholdWriteCollector) PostJSON(ctx context.Context, path string, input any, output any) error {
	_ = c.fakeCollector.PostJSON(ctx, path, input, output)
	return c.err
}

func (c thresholdWriteCollector) DeleteJSONBody(ctx context.Context, path string, input any, output any) error {
	_ = c.fakeCollector.DeleteJSONBody(ctx, path, input, output)
	return c.err
}

func TestSetCheckThresholdSendsTheKeyAndReadsTheCheckBack(t *testing.T) {
	collector := thresholdCollector(varSetting)
	high := int64(95)
	change, err := newTestService(collector).SetCheckThreshold(t.Context(), CheckKeyOptions{Node: "dev2n1", Type: "fs_u", Instance: "/var"}, nil, &high)
	if err != nil {
		t.Fatal(err)
	}
	var post fakeRequest
	for _, request := range collector.requests {
		if request.method == "POST" {
			post = request
		}
	}
	if post.path != "/checks/settings" || post.body["node_id"] != "n-1" || post.body["chk_instance"] != "/var" || post.body["chk_high"] != float64(95) ||
		post.body["chk_low"] != nil || post.body["svc_id"] != nil {
		t.Fatalf("post = %+v", post)
	}
	if change.Outcome != ChangeSet || change.Check == nil || change.Check.ID != 7 || change.Warning != "" {
		t.Fatalf("change = %+v", change)
	}
	// A check of a service sends its service ID.
	collector = thresholdCollector(varSetting)
	if _, err := newTestService(collector).SetCheckThreshold(t.Context(), CheckKeyOptions{Node: "dev2n1", Type: "fs_u", Instance: "/srv/mq", Service: "test/svc/mq"}, &high, &high); err != nil {
		t.Fatal(err)
	}
	for _, request := range collector.requests {
		if request.method == "POST" && request.body["svc_id"] != "s-1" {
			t.Fatalf("post = %+v", request)
		}
	}
}

func TestSetCheckThresholdReadsTheRefusals(t *testing.T) {
	high := int64(95)
	key := CheckKeyOptions{Node: "dev2n1", Type: "fs_u", Instance: "/nosuch"}
	// The collector holds no such check: the instances of its type are told.
	refusing := thresholdWriteCollector{thresholdCollector(varSetting), statusError{http.StatusNotFound, "check instance not found"}}
	if _, err := New(refusing).SetCheckThreshold(t.Context(), key, nil, &high); err == nil || !strings.Contains(err.Error(), `the instances of its fs_u checks are: "/", "/var"`) {
		t.Fatalf("unknown check: %v", err)
	}
	// Saved but not applied yet: a warning, the change being made.
	key.Instance = "/var"
	refusing = thresholdWriteCollector{thresholdCollector(varSetting), statusError{http.StatusInternalServerError, "thresholds saved, but the checks could not take them yet: they will at the next check run of the node"}}
	change, err := New(refusing).SetCheckThreshold(t.Context(), key, nil, &high)
	if err != nil || change.Outcome != ChangeSet || !strings.Contains(change.Warning, "did not apply it to the checks yet") || change.Check == nil {
		t.Fatalf("saved, not applied: %+v, %v", change, err)
	}
	// Any other refusal is the collector's.
	refusing = thresholdWriteCollector{thresholdCollector(varSetting), statusError{http.StatusBadRequest, "the check has no threshold to keep: give both chk_low and chk_high"}}
	if _, err := New(refusing).SetCheckThreshold(t.Context(), key, nil, &high); err == nil || !strings.Contains(err.Error(), "give both chk_low and chk_high") {
		t.Fatalf("refused: %v", err)
	}
}

func TestUnsetCheckThreshold(t *testing.T) {
	collector := thresholdCollector(varSetting)
	key := CheckKeyOptions{Node: "dev2n1", Type: "fs_u", Instance: "/var"}
	change, err := newTestService(collector).UnsetCheckThreshold(t.Context(), key)
	if err != nil || change.Outcome != ChangeRemoved || change.Check == nil {
		t.Fatalf("unset = %+v, %v", change, err)
	}
	var deleted fakeRequest
	for _, request := range collector.requests {
		if request.method == "DELETE" {
			deleted = request
		}
	}
	if deleted.path != "/checks/settings" || deleted.body["node_id"] != "n-1" || deleted.body["chk_type"] != "fs_u" || deleted.body["chk_instance"] != "/var" {
		t.Fatalf("delete = %+v", deleted)
	}
	refusing := thresholdWriteCollector{thresholdCollector(varSetting), statusError{http.StatusNotFound, "no thresholds set on check fs_u:/var"}}
	change, err = New(refusing).UnsetCheckThreshold(t.Context(), key)
	if err != nil || change.Outcome != ChangeUnchanged || change.Check == nil {
		t.Fatalf("no settings = %+v, %v", change, err)
	}
}

// Past 20 contextual thresholds whose membership is read, none applying,
// the thresholds without the settings are unknown: one not read may apply.
func TestGetCheckThresholdTellsAnUnknownFallback(t *testing.T) {
	collector := thresholdCollector(varSetting)
	answer := collector.answer
	collector.answer = func(path string, query url.Values) string {
		if path == "/checks/contextual_settings" {
			var rows []string
			for id := 100; id > 79; id-- {
				rows = append(rows, `{"id":`+itoa(id)+`,"fset_id":`+itoa(id)+`,"chk_type":"fs_u","chk_instance":"/var","chk_low":0,"chk_high":80,"gen_filtersets.fset_name":"f"}`)
			}
			return `{"data":[` + strings.Join(rows, ",") + `],"meta":{"total":21}}`
		}
		return answer(path, query)
	}
	details, err := newTestService(collector).GetCheckThreshold(t.Context(), CheckKeyOptions{Node: "dev2n1", Type: "fs_u", Instance: "/var"})
	if err != nil {
		t.Fatal(err)
	}
	if details.Unchecked != 1 || len(details.Contextual) != 21 || details.Contextual[20].Applies != nil || details.Without.Source != "unknown" || !details.Defaults[0].Wins {
		t.Fatalf("details = %+v", details)
	}
}

package core

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestFindObjectsNormalizesTheHitsAndTellsWhyTheyMatch(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[
		{"kind":"node","items":[{"node_id":"n-1","nodename":"dev2n1","app":"DEV","node_env":"DEV","os_name":"Linux","cluster_id":"c1"}],"more":false},
		{"kind":"instance","items":[{"svc_id":"s-1","node_id":"n-1","services.svcname":"web","nodes.nodename":"dev2n1","mon_vmname":"c1","mon_availstatus":"up"}],"more":true},
		{"kind":"disk","items":[{"disk_id":"360001","disk_name":"","disk_size":524288,"nodename":"dev2n1","svcname":""}],"more":false},
		{"kind":"tag","items":[{"tag_id":"t-1","tag_name":"decommission-q4","tag_exclude":"^dev2n1$"}],"more":false,"error":""}]}`}
	found, err := newTestService(collector).FindObjects(t.Context(), FindObjectsOptions{Text: " DEV2N1 ", Kinds: []string{"tag", "node", "instance", "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/search" || request.query.Get("q") != "DEV2N1" || request.query.Get("kinds") != "node,instance,disk,tag" || request.query.Get("limit") != "5" {
		t.Fatalf("request = %+v", request)
	}
	node, instance, disk, tag := found.Groups[0].Items[0], found.Groups[1].Items[0], found.Groups[2].Items[0], found.Groups[3].Items[0]
	if node.MatchedBy != "name" || node.ID != "n-1" || node.Name != "dev2n1" || node.NextTool != "get_node" || node.Context["app"] != "DEV" {
		t.Fatalf("node = %+v", node)
	}
	if instance.MatchedBy != "context" || instance.ID != "s-1@n-1" || instance.Name != "web@dev2n1 (c1)" || instance.Context["nodename"] != "dev2n1" ||
		instance.NextTool != "list_instances" || !found.Groups[1].More {
		t.Fatalf("instance = %+v", instance)
	}
	if disk.Name != "360001" || disk.Context["disk_size"] != "524288" || disk.MatchedBy != "context" || disk.NextTool != "list_node_disks" {
		t.Fatalf("disk = %+v", disk)
	}
	if tag.MatchedBy != "context" || tag.Context["tag_exclude"] != "^dev2n1$" || found.Text != "DEV2N1" || found.Provenance.Source != provenanceSourceOpenSVCCollector {
		t.Fatalf("tag = %+v, found = %+v", tag, found)
	}
}

func TestFindObjectsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []FindObjectsOptions{
		{Text: "a"},
		{Text: " a "},
		{Text: strings.Repeat("a", 101)},
		{Text: "web", Kinds: []string{"user"}},
		{Text: "web", Limit: 21},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).FindObjects(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	// A kind the collector returns without being asked is refused.
	collector := &fakeCollector{body: `{"data":[{"kind":"user","items":[]}]}`}
	if _, err := newTestService(collector).FindObjects(t.Context(), FindObjectsOptions{Text: "web", Kinds: []string{"node"}}); err == nil {
		t.Fatal("an unasked kind was accepted")
	}
}

type notFoundCollector struct{}

type fakeNotFound struct{}

func (fakeNotFound) Error() string  { return "HTTP 404 Not Found" }
func (fakeNotFound) NotFound() bool { return true }

func (notFoundCollector) GetJSON(context.Context, string, url.Values, any) error {
	return fakeNotFound{}
}

func (notFoundCollector) PostJSON(context.Context, string, any, any) error { return fakeNotFound{} }

func (notFoundCollector) PutJSON(context.Context, string, any, any) error { return fakeNotFound{} }

func (notFoundCollector) DeleteJSON(context.Context, string, url.Values, any) error {
	return fakeNotFound{}
}

func (notFoundCollector) DeleteJSONBody(context.Context, string, any, any) error {
	return fakeNotFound{}
}

// An exact lookup that finds nothing suggests find_objects, whether the
// collector answers 404 or an empty list.
func TestLookupsSuggestFindObjects(t *testing.T) {
	service := New(notFoundCollector{})
	if _, err := service.GetNode(t.Context(), "dev2"); err == nil || !strings.Contains(err.Error(), "use find_objects") {
		t.Fatalf("get_node = %v", err)
	}
	if _, err := service.GetApp(t.Context(), "WE"); err == nil || !strings.Contains(err.Error(), "use find_objects") {
		t.Fatalf("get_app = %v", err)
	}
	empty := &fakeCollector{body: `{"data":[]}`}
	if _, err := newTestService(empty).GetService(t.Context(), "web"); err == nil || !strings.Contains(err.Error(), "use find_objects") {
		t.Fatalf("get_service = %v", err)
	}
	other := &fakeCollector{err: errors.New("HTTP 500")}
	if _, err := newTestService(other).GetService(t.Context(), "web"); err == nil || strings.Contains(err.Error(), "find_objects") {
		t.Fatalf("a server error suggested find_objects: %v", err)
	}
}

package core

import (
	"strings"
	"testing"
)

const testChecksBody = `{"data":[
	{"id":1,"node_id":"n-1","svc_id":"s-1","chk_type":"fs_u","chk_instance":"/srv/dns.free","chk_value":1000,"chk_low":1500,"chk_high":null,"chk_err":1,
	 "chk_threshold_provider":"defaults","chk_created":"2026-10-12 01:28:05","chk_updated":"2026-10-12 01:28:05"},
	{"id":2,"node_id":"n-1","svc_id":"","chk_type":"eth","chk_instance":"enp2s0.link","chk_value":1,"chk_low":1,"chk_high":1,"chk_err":0,"chk_threshold_provider":"settings"},
	{"id":3,"node_id":"n-1","svc_id":"","chk_type":"fs_u","chk_instance":"/.size","chk_value":48269648,"chk_low":null,"chk_high":null,"chk_err":0,"chk_threshold_provider":"fset:linux"}
],"meta":{"total":3}}`

func TestListNodeChecksReadsAndNamesTheChecks(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":       `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/nodes/n-1/checks":   testChecksBody,
		"/nodes/n-1/services": `{"data":[{"svc_id":"s-1","services.svcname":"system/svc/dns"}]}`,
	}}
	checks, err := newTestService(collector).ListNodeChecks(t.Context(), "dev2n1", ListChecksOptions{})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[1]
	if request.path != "/nodes/n-1/checks" || request.query.Get("orderby") != "-chk_err,chk_type,chk_instance,id" || request.query.Get("limit") != "2000" ||
		len(request.query["filter"]) != 0 {
		t.Fatalf("request = %+v", request)
	}
	if names := collector.requests[2]; names.path != "/nodes/n-1/services" || names.query.Get("props") != "svc_id,services.svcname" {
		t.Fatalf("names request = %+v", names)
	}
	if checks.Node == nil || checks.Node.Nodename != "dev2n1" || checks.Total != 3 || checks.OutOfBoundsRead != 1 || checks.Count != 3 || checks.Truncated {
		t.Fatalf("list = %+v", checks)
	}
	first, second, third := checks.Checks[0], checks.Checks[1], checks.Checks[2]
	if !first.OutOfBounds || *first.Value != 1000 || *first.Low != 1500 || first.High != nil || first.ThresholdSource != "defaults" ||
		first.Svcname != "system/svc/dns" || first.Nodename != "dev2n1" || second.OutOfBounds || second.Svcname != "" ||
		second.ThresholdSource != "settings" || third.Low != nil || third.ThresholdSource != "fset:linux" {
		t.Fatalf("checks = %+v", checks.Checks)
	}
}

func TestListServiceChecksFiltersLocally(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/system%2Fsvc%2Fdns": `{"data":[{"svc_id":"s-1","svcname":"system/svc/dns","cluster_id":"c1"}]}`,
		"/services/s-1/checks":         testChecksBody,
		"/services/s-1/nodes":          `{"data":[{"node_id":"n-1","nodes.nodename":"dev2n1"}]}`,
	}}
	checks, err := newTestService(collector).ListServiceChecks(t.Context(), "system/svc/dns", ListChecksOptions{Type: "fs_u", OutOfBoundsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if checks.Service == nil || checks.Service.Svcname != "system/svc/dns" || checks.Total != 3 || checks.OutOfBoundsRead != 1 || checks.Count != 1 ||
		checks.Checks[0].ID != 1 || checks.Checks[0].Nodename != "dev2n1" || checks.Checks[0].Svcname != "system/svc/dns" {
		t.Fatalf("list = %+v", checks)
	}
	checks, err = newTestService(collector).ListServiceChecks(t.Context(), "system/svc/dns", ListChecksOptions{Type: "eth"})
	if err != nil || checks.Count != 1 || checks.Checks[0].Type != "eth" {
		t.Fatalf("type filter = %+v, %v", checks, err)
	}
}

func TestListChecksReportsUnreadChecksAndRejectsInvalidInputs(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":       `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-1/checks":   `{"data":[{"id":1,"chk_type":"fs_u","chk_err":0}],"meta":{"total":2500}}`,
		"/nodes/n-1/services": `{"data":[]}`,
	}}
	checks, err := newTestService(collector).ListNodeChecks(t.Context(), "dev2n1", ListChecksOptions{})
	if err != nil || !checks.Truncated || checks.Total != 2500 {
		t.Fatalf("unread checks = %+v, %v", checks, err)
	}
	for _, call := range []func(*Service) error{
		func(s *Service) error { _, err := s.ListNodeChecks(t.Context(), "", ListChecksOptions{}); return err },
		func(s *Service) error {
			_, err := s.ListNodeChecks(t.Context(), "dev2n1", ListChecksOptions{Type: " fs_u"})
			return err
		},
		func(s *Service) error {
			_, err := s.ListServiceChecks(t.Context(), "two words", ListChecksOptions{})
			return err
		},
		func(s *Service) error {
			_, err := s.ListServiceChecks(t.Context(), "web", ListChecksOptions{Type: strings.Repeat("a", 256)})
			return err
		},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if err := call(newTestService(collector)); err == nil || len(collector.requests) != 0 {
			t.Fatalf("invalid input accepted: %v", err)
		}
	}
	collector = &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":     `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-1/checks": `{"data":[]}`,
	}}
	if _, err := newTestService(collector).ListNodeChecks(t.Context(), "dev2n1", ListChecksOptions{}); err == nil {
		t.Fatal("a response without total was accepted")
	}
}

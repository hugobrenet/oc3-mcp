package core

import (
	"fmt"
	"strings"
	"testing"
)

const testPackageDiffBody = `{"data":[
	{"node_id":"n-1","pkg_name":"gh","pkg_arch":"amd64","pkg_type":"deb","pkg_version":"2.45.0"},
	{"node_id":"n-1","pkg_name":"opensvc-server","pkg_arch":"amd64","pkg_type":"deb","pkg_version":"3.0.0~rc46"},
	{"node_id":"n-2","pkg_name":"opensvc-server","pkg_arch":"amd64","pkg_type":"deb","pkg_version":"3.0.0~rc45"},
	{"node_id":"n-3","pkg_name":"opensvc-server","pkg_arch":"amd64","pkg_type":"deb","pkg_version":"3.0.0~rc45"},
	{"node_id":"n-2","pkg_name":"curl","pkg_arch":"amd64","pkg_type":"deb","pkg_version":"8.5.0"},
	{"node_id":"n-3","pkg_name":"curl","pkg_arch":"amd64","pkg_type":"deb","pkg_version":"8.5.0"}
],"meta":{"node_ids":["n-1","n-2","n-3"],"nodes":[{"node_id":"n-1","nodename":"dev2n1"},{"node_id":"n-2","nodename":"dev2n2"},{"node_id":"n-3","nodename":"dev2n3"}]}}`

func TestListPackageDifferencesGroupsByPackage(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/services/system%2Fsvc%2Fdns": `{"data":[{"svc_id":"s-1","svcname":"system/svc/dns","cluster_id":"c1"}]}`,
		"/packages/diff":               testPackageDiffBody,
	}}
	differences, err := newTestService(collector).ListPackageDifferences(t.Context(), ListPackageDifferencesOptions{Service: "system/svc/dns"})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[1]
	if request.path != "/packages/diff" || request.query.Get("svc_ids") != "s-1" || request.query.Has("node_ids") || request.query.Has("encap") {
		t.Fatalf("request = %+v", request)
	}
	if differences.Service == nil || len(differences.ComparedNodes) != 3 || differences.Total != 3 || differences.VersionMismatches != 1 ||
		differences.Missing != 2 || differences.Count != 3 || differences.Truncated {
		t.Fatalf("differences = %+v", differences)
	}
	first := differences.Packages[0]
	if first.Name != "opensvc-server" || first.Kind != "version_mismatch" || len(first.MissingOn) != 0 || len(first.Versions) != 2 ||
		first.Versions[0].Version != "3.0.0~rc45" || strings.Join(first.Versions[0].Nodes, ",") != "dev2n2,dev2n3" ||
		strings.Join(first.Versions[1].Nodes, ",") != "dev2n1" {
		t.Fatalf("first = %+v", first)
	}
	if curl := differences.Packages[1]; curl.Name != "curl" || curl.Kind != "missing" || strings.Join(curl.MissingOn, ",") != "dev2n1" {
		t.Fatalf("curl = %+v", curl)
	}
	if gh := differences.Packages[2]; gh.Name != "gh" || strings.Join(gh.MissingOn, ",") != "dev2n2,dev2n3" {
		t.Fatalf("gh = %+v", gh)
	}
}

func TestListPackageDifferencesResolvesNodesAndFilters(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":  `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-2":     `{"data":[{"node_id":"n-2","nodename":"dev2n2"}]}`,
		"/packages/diff": testPackageDiffBody,
	}}
	differences, err := newTestService(collector).ListPackageDifferences(t.Context(), ListPackageDifferencesOptions{Nodes: []string{"dev2n1", "n-2", "dev2n1"}, Name: "OPENSVC"})
	if err != nil {
		t.Fatal(err)
	}
	if request := collector.requests[len(collector.requests)-1]; request.query.Get("node_ids") != "n-1,n-2" {
		t.Fatalf("request = %+v", request)
	}
	if differences.Total != 1 || differences.Packages[0].Name != "opensvc-server" || differences.Missing != 0 {
		t.Fatalf("differences = %+v", differences)
	}
}

func TestListPackageDifferencesBoundsThePackages(t *testing.T) {
	rows := make([]string, 0, maxPackageDifferences+5)
	for index := range maxPackageDifferences + 5 {
		rows = append(rows, fmt.Sprintf(`{"node_id":"n-1","pkg_name":"p%04d","pkg_version":"1"}`, index))
	}
	collector := &fakeCollector{bodies: map[string]string{
		"/services/web":  `{"data":[{"svc_id":"s-1","svcname":"web"}]}`,
		"/packages/diff": `{"data":[` + strings.Join(rows, ",") + `],"meta":{"nodes":[{"node_id":"n-1","nodename":"a"},{"node_id":"n-2","nodename":"b"}]}}`,
	}}
	differences, err := newTestService(collector).ListPackageDifferences(t.Context(), ListPackageDifferencesOptions{Service: "web", Encap: true})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[1].query.Get("encap") != "true" || !differences.Truncated || differences.Total != maxPackageDifferences+5 ||
		differences.Count != maxPackageDifferences || differences.Packages[0].Name != "p0000" {
		t.Fatalf("total=%d count=%d truncated=%v", differences.Total, differences.Count, differences.Truncated)
	}
}

func TestListPackageDifferencesRejectsInvalidInputs(t *testing.T) {
	tooMany := make([]string, maxPackageDiffNodes+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("n%d", index)
	}
	for _, options := range []ListPackageDifferencesOptions{
		{},
		{Nodes: []string{"dev2n1"}},
		{Nodes: tooMany},
		{Nodes: []string{"dev2n1", "dev2n2"}, Encap: true},
		{Service: "web", Name: " padded"},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).ListPackageDifferences(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{body: `{"data":[{"node_id":"a","cluster_id":"c-a"},{"node_id":"b","cluster_id":"c-b"}]}`}
	if _, err := newTestService(collector).ListPackageDifferences(t.Context(), ListPackageDifferencesOptions{Nodes: []string{"n1", "n2"}}); err == nil || !strings.Contains(err.Error(), "pass one node ID") {
		t.Fatalf("ambiguous node error = %v", err)
	}
	collector = &fakeCollector{bodies: map[string]string{"/services/web": `{"data":[{"svc_id":"s-1"}]}`, "/packages/diff": `{"meta":{}}`}}
	if _, err := newTestService(collector).ListPackageDifferences(t.Context(), ListPackageDifferencesOptions{Service: "web"}); err == nil {
		t.Fatal("a response without data was accepted")
	}
}

func TestListNodePackagesQueriesOneNode(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`,
		"/packages": `{"data":[{"pkg_name":"opensvc-server","pkg_version":"3.0.0~rc37","pkg_arch":"amd64","pkg_type":"deb","sig_provider":"",
			"pkg_install_date":"2026-09-29 06:34:43","pkg_updated":"2026-10-12 05:39:28"}],"meta":{"total":2}}`,
	}}
	packages, err := newTestService(collector).ListNodePackages(t.Context(), ListNodePackagesOptions{Node: "dev2n1", Name: "opensvc", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[1]
	if request.path != "/packages" || request.query.Get("orderby") != "pkg_name,pkg_arch,id" ||
		strings.Join(request.query["filter"], "|") != "node_id:eq:n-1|pkg_name:~opensvc" || strings.Contains(request.query.Get("props"), "pkg_sig,") {
		t.Fatalf("request = %+v", request)
	}
	got := packages.Packages[0]
	if packages.Node.Nodename != "dev2n1" || packages.Total != 2 || !packages.Truncated || packages.NextCursor == "" ||
		got.Name != "opensvc-server" || got.Version != "3.0.0~rc37" || got.InstalledAt != "2026-09-29 06:34:43" || got.LastReportedAt != "2026-10-12 05:39:28" || got.SignedBy != "" {
		t.Fatalf("packages = %+v", packages)
	}
}

func TestListNodePackagesRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListNodePackagesOptions{
		{},
		{Node: "a/b"},
		{Node: "dev2n1", Name: " padded"},
		{Node: "dev2n1", Limit: 501},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).ListNodePackages(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	collector := &fakeCollector{body: `{"data":[{"node_id":"n-1","nodename":"dev2n1"}],"meta":{"total":1}}`}
	if _, err := newTestService(collector).ListNodePackages(t.Context(), ListNodePackagesOptions{Node: "dev2n1", Cursor: "garbage"}); err == nil {
		t.Fatal("an invalid cursor was accepted")
	}
}

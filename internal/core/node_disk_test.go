package core

import (
	"net/url"
	"strings"
	"testing"
)

func TestListNodeDisksReadsDisksAndTheirOtherNodes(t *testing.T) {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		switch {
		case path == "/nodes/dev2n1":
			return `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
		case path == "/disks" && strings.HasPrefix(query.Get("filter"), "disk_id:in:"):
			return `{"data":[
				{"disk_id":"360014050afbff271f0a4e019f692be55","node_id":"n-1","nodename":"dev2n1"},
				{"disk_id":"360014050afbff271f0a4e019f692be55","node_id":"n-2","nodename":"dev2n2"},
				{"disk_id":"360014050afbff271f0a4e019f692be55","node_id":"n-3","nodename":"dev2n3"},
				{"disk_id":"360014050afbff271f0a4e019f692be55","node_id":"n-3","nodename":"dev2n3"}]}`
		case path == "/disks":
			return `{"data":[
				{"disk_id":"360014050afbff271f0a4e019f692be55","disk_size":1024,"disk_used":1024,"disk_vendor":"LIO-ORG ","disk_model":"c29_disk5",
				 "disk_arrayid":"array-1","disk_group":"pool1","disk_dg":"vg1","disk_region":"0","svc_id":"s-1","svcname":"web","updated":"2026-10-12 05:32:05"},
				{"disk_id":"vda","disk_size":40960,"disk_used":40960,"disk_vendor":"0x1af4"}],"meta":{"total":3}}`
		}
		return `{}`
	}
	disks, err := newTestService(collector).ListNodeDisks(t.Context(), ListNodeDisksOptions{Node: "dev2n1", DiskID: "ab.c", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	page := collector.requests[1]
	if page.path != "/disks" || page.query.Get("orderby") != "disk_id,svc_id" || strings.Join(page.query["filter"], "|") != `node_id:eq:n-1|disk_id:~ab\.c` {
		t.Fatalf("page request = %+v", page)
	}
	if sharing := collector.requests[2]; sharing.query.Get("filter") != "disk_id:in:360014050afbff271f0a4e019f692be55" || sharing.query.Get("limit") != "5000" {
		t.Fatalf("sharing request = %+v", sharing)
	}
	shared, local := disks.Disks[0], disks.Disks[1]
	if disks.Node.Nodename != "dev2n1" || disks.Total != 3 || !disks.Truncated || disks.NextCursor == "" ||
		shared.Vendor != "LIO-ORG" || shared.SizeMB != 1024 || shared.Array != "array-1" || shared.ArrayGroup != "pool1" || shared.DiskGroup != "vg1" ||
		shared.Svcname != "web" || strings.Join(shared.SharedWith, ",") != "dev2n2,dev2n3" {
		t.Fatalf("shared disk = %+v", shared)
	}
	if local.DiskID != "vda" || local.SharedWith == nil || len(local.SharedWith) != 0 || local.SizeMB != 40960 {
		t.Fatalf("local disk = %+v", local)
	}
}

func TestListNodeDisksWithOnlyLocalDisksSkipsTheSharingQuery(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/disks":        `{"data":[{"disk_id":"sr0"},{"disk_id":"zd16"}],"meta":{"total":2}}`,
	}}
	disks, err := newTestService(collector).ListNodeDisks(t.Context(), ListNodeDisksOptions{Node: "dev2n1"})
	if err != nil || len(collector.requests) != 2 || disks.Count != 2 || collector.requests[1].query.Get("limit") != "100" {
		t.Fatalf("disks = %+v, %v, %d requests", disks, err, len(collector.requests))
	}
}

func TestListNodeDisksRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListNodeDisksOptions{
		{},
		{Node: "a/b"},
		{Node: "dev2n1", DiskID: " padded"},
		{Node: "dev2n1", Limit: 201},
	} {
		collector := &fakeCollector{body: `{"data":[]}`}
		if _, err := newTestService(collector).ListNodeDisks(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	for _, id := range []string{"vda", "sr0", "zd0", "0x1af4", "1c62e2f5-41a8.vda"} {
		if worldWideDiskID.MatchString(id) {
			t.Fatalf("%q is taken for a world wide disk ID", id)
		}
	}
	for _, id := range []string{"360014050afbff271f0a4e019f692be55", "600140550b8e30adba142d5b5fee019f"} {
		if !worldWideDiskID.MatchString(id) {
			t.Fatalf("%q is not taken for a world wide disk ID", id)
		}
	}
}

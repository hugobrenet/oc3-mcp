package core

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const testSanNodeLookup = `{"data":[{"node_id":"00000000-0000-4000-8000-000000000001","nodename":"lyn-prd-db01","cluster_id":"c1"}]}`

func TestGetNodeSanTopologyDerivesThePaths(t *testing.T) {
	graph, err := os.ReadFile("testdata/san_lyn_prd_db01.json")
	if err != nil {
		t.Fatal(err)
	}
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/lyn-prd-db01":                             testSanNodeLookup,
		"/nodes/00000000-0000-4000-8000-000000000001/san": string(graph),
	}}
	topology, err := newTestService(collector).GetNodeSanTopology(t.Context(), "lyn-prd-db01")
	if err != nil {
		t.Fatal(err)
	}
	if !topology.Wired || topology.Truncated || len(topology.Devices) != 6 || len(topology.Links) != 8 || len(topology.HBAs) != 2 {
		t.Fatalf("topology = %+v", topology)
	}
	fabricA := topology.HBAs[0]
	if fabricA.PortWWN != "21000024ff4a1b10" || len(fabricA.Paths) != 2 {
		t.Fatalf("hba = %+v", fabricA)
	}
	path := fabricA.Paths[0]
	var hops []string
	for _, hop := range path.Hops {
		hops = append(hops, hop.From+"→"+hop.To)
	}
	if path.Fabric != "lyn-fabric-a" || path.Array != "lyn-vsp-01" || path.ArrayPort != "50060e8012c0a700" || path.MinSpeedGbps != 16 ||
		strings.Join(hops, ",") != "lyn-prd-db01→lyn-sana-edge01,lyn-sana-edge01→lyn-sana-core01,lyn-sana-core01→lyn-vsp-01" ||
		path.Hops[1].FromPort != "46,47" || len(path.Hops[1].SpeedsGbps) != 2 {
		t.Fatalf("path = %+v", path)
	}
	if len(topology.Arrays) != 1 {
		t.Fatalf("arrays = %+v", topology.Arrays)
	}
	array := topology.Arrays[0]
	if array.Array != "lyn-vsp-01" || strings.Join(array.Fabrics, ",") != "lyn-fabric-a,lyn-fabric-b" || len(array.HBAs) != 2 ||
		len(array.TargetPorts) != 4 || array.Paths != 4 {
		t.Fatalf("array = %+v", array)
	}
}

func TestGetNodeSanTopologyReportsASingleFabric(t *testing.T) {
	graph := `{"data":{"nodes":[
		{"id":"server:n-1","kind":"server","label":"n1","ports":["hba-a","hba-b"]},
		{"id":"switch:a","kind":"switch","label":"sw-a","fabric":"fab-a","ports":["1","2"]},
		{"id":"array:x","kind":"array","label":"x","ports":["t1"]}],
	"links":[
		{"tail":"server:n-1","tail_port":"hba-a","head":"switch:a","head_port":"1","speeds":[8]},
		{"tail":"switch:a","tail_port":"2","head":"array:x","head_port":"t1","speeds":[16]}]}}`
	collector := &fakeCollector{bodies: map[string]string{"/nodes/n1": `{"data":[{"node_id":"n-1","nodename":"n1"}]}`, "/nodes/n-1/san": graph}}
	topology, err := newTestService(collector).GetNodeSanTopology(t.Context(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(topology.HBAs) != 2 || len(topology.HBAs[1].Paths) != 0 || topology.Arrays[0].Paths != 1 ||
		strings.Join(topology.Arrays[0].Fabrics, ",") != "fab-a" || strings.Join(topology.Arrays[0].HBAs, ",") != "hba-a" || topology.HBAs[0].Paths[0].MinSpeedGbps != 8 {
		t.Fatalf("topology = %+v", topology)
	}
}

func TestGetNodeSanTopologyOfANodeWithoutFibreChannel(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1":  `{"data":[{"node_id":"n-1","nodename":"dev2n1"}]}`,
		"/nodes/n-1/san": `{"data":{"links":[],"nodes":[{"id":"server:n-1","kind":"server","label":"dev2n1","ports":[],"rank":0}]}}`,
	}}
	topology, err := newTestService(collector).GetNodeSanTopology(t.Context(), "dev2n1")
	if err != nil {
		t.Fatal(err)
	}
	if topology.Wired || len(topology.HBAs) != 0 || topology.Arrays == nil || len(topology.Arrays) != 0 || len(topology.Devices) != 1 {
		t.Fatalf("topology = %+v", topology)
	}
}

func TestGetNodeSanTopologyStopsOnLoopsAndBoundsThePaths(t *testing.T) {
	loop := `{"data":{"nodes":[
		{"id":"server:n-1","kind":"server","label":"n1","ports":["hba"]},
		{"id":"switch:a","kind":"switch","label":"a"},{"id":"switch:b","kind":"switch","label":"b"}],
	"links":[
		{"tail":"server:n-1","tail_port":"hba","head":"switch:a"},
		{"tail":"switch:a","head":"switch:b"},{"tail":"switch:b","head":"switch:a"}]}}`
	collector := &fakeCollector{bodies: map[string]string{"/nodes/n1": `{"data":[{"node_id":"n-1","nodename":"n1"}]}`, "/nodes/n-1/san": loop}}
	topology, err := newTestService(collector).GetNodeSanTopology(t.Context(), "n1")
	if err != nil || len(topology.HBAs[0].Paths) != 0 || topology.Links[0].SpeedsGbps == nil {
		t.Fatalf("loop = %+v, %v", topology, err)
	}

	var nodes, links []string
	nodes = append(nodes, `{"id":"server:n-1","kind":"server","label":"n1","ports":["hba"]}`, `{"id":"switch:a","kind":"switch","label":"a"}`)
	links = append(links, `{"tail":"server:n-1","tail_port":"hba","head":"switch:a"}`)
	for index := range maxSanPaths + 5 {
		links = append(links, fmt.Sprintf(`{"tail":"switch:a","head":"array:x","head_port":"t%d"}`, index))
	}
	nodes = append(nodes, `{"id":"array:x","kind":"array","label":"x"}`)
	wide := `{"data":{"nodes":[` + strings.Join(nodes, ",") + `],"links":[` + strings.Join(links, ",") + `]}}`
	collector = &fakeCollector{bodies: map[string]string{"/nodes/n1": `{"data":[{"node_id":"n-1","nodename":"n1"}]}`, "/nodes/n-1/san": wide}}
	topology, err = newTestService(collector).GetNodeSanTopology(t.Context(), "n1")
	if err != nil || !topology.Truncated || len(topology.HBAs[0].Paths) != maxSanPaths {
		t.Fatalf("wide = truncated %v, %d paths, %v", topology.Truncated, len(topology.HBAs[0].Paths), err)
	}
}

func TestGetNodeSanTopologyRejectsUnexpectedResponses(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":{"nodes":[{"id":"switch:a","kind":"switch"}],"links":[]}}`} {
		collector := &fakeCollector{bodies: map[string]string{"/nodes/n1": `{"data":[{"node_id":"n-1","nodename":"n1"}]}`, "/nodes/n-1/san": body}}
		if _, err := newTestService(collector).GetNodeSanTopology(t.Context(), "n1"); err == nil {
			t.Fatalf("response %s accepted", body)
		}
	}
	collector := &fakeCollector{body: `{"data":[]}`}
	if _, err := newTestService(collector).GetNodeSanTopology(t.Context(), "a/b"); err == nil || len(collector.requests) != 0 {
		t.Fatal("an invalid node was accepted")
	}
}

package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
)

const (
	maxSanDevices = 500
	maxSanLinks   = 2000
	maxSanPaths   = 500
)

type NodeSanTopology struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference  `json:"node" jsonschema:"the node whose SAN wiring is described"`
	Wired      bool           `json:"wired" jsonschema:"whether the collector knows a fibre channel wiring of the node; false for a node without fibre channel, such as an iSCSI one"`
	HBAs       []SanHBA       `json:"hbas" jsonschema:"the host bus adapter ports of the node and the paths from each to the storage arrays"`
	Arrays     []SanArrayPath `json:"arrays" jsonschema:"for each storage array the node reaches, the fabrics, adapters, target ports and paths leading to it"`
	Devices    []SanDevice    `json:"devices" jsonschema:"the devices of the wiring: the node, the switches and the storage arrays"`
	Links      []SanLink      `json:"links" jsonschema:"the cables of the wiring, from the node toward the storage arrays"`
	Truncated  bool           `json:"truncated" jsonschema:"whether paths were left out after 500"`
}

type SanHBA struct {
	PortWWN string    `json:"port_wwn" jsonschema:"the world wide name of the adapter port"`
	Paths   []SanPath `json:"paths" jsonschema:"the paths from this port to the storage array ports; empty when it reaches none"`
}

type SanPath struct {
	Fabric       string   `json:"fabric,omitempty" jsonschema:"the fabric of the first switch of the path"`
	Hops         []SanHop `json:"hops" jsonschema:"the cables of the path, from the adapter port to the storage array port"`
	Array        string   `json:"array" jsonschema:"the storage array the path reaches"`
	ArrayPort    string   `json:"array_port" jsonschema:"the target port of the storage array"`
	MinSpeedGbps float64  `json:"min_speed_gbps" jsonschema:"the lowest cable speed along the path, in Gb/s; 0 when the collector gives none"`
}

type SanHop struct {
	From       string    `json:"from" jsonschema:"the device the cable leaves"`
	FromPort   string    `json:"from_port,omitempty" jsonschema:"its port, or ports for an aggregated inter-switch link"`
	To         string    `json:"to" jsonschema:"the device the cable reaches"`
	ToPort     string    `json:"to_port,omitempty" jsonschema:"its port, or ports for an aggregated inter-switch link"`
	SpeedsGbps []float64 `json:"speeds_gbps" jsonschema:"the speed of each cable of the link, in Gb/s"`
}

type SanArrayPath struct {
	Array       string   `json:"array" jsonschema:"the storage array name"`
	Fabrics     []string `json:"fabrics" jsonschema:"the fabrics the paths to the array go through"`
	HBAs        []string `json:"hbas" jsonschema:"the adapter ports reaching the array"`
	TargetPorts []string `json:"target_ports" jsonschema:"the array ports the node reaches"`
	Paths       int      `json:"paths" jsonschema:"the number of paths from the node to the array"`
}

type SanDevice struct {
	ID     string   `json:"id" jsonschema:"the device ID in the wiring, prefixed by its kind"`
	Kind   string   `json:"kind" jsonschema:"server, switch or array"`
	Name   string   `json:"name" jsonschema:"the device name"`
	Fabric string   `json:"fabric,omitempty" jsonschema:"the fabric of a switch"`
	Ports  []string `json:"ports,omitempty" jsonschema:"the ports of the device in the wiring"`
}

type SanLink struct {
	From       string    `json:"from" jsonschema:"the device ID the cable leaves"`
	FromPort   string    `json:"from_port,omitempty" jsonschema:"its port, or ports for an aggregated inter-switch link"`
	To         string    `json:"to" jsonschema:"the device ID the cable reaches"`
	ToPort     string    `json:"to_port,omitempty" jsonschema:"its port, or ports for an aggregated inter-switch link"`
	SpeedsGbps []float64 `json:"speeds_gbps" jsonschema:"the speed of each cable of the link, in Gb/s"`
}

type collectorSanGraph struct {
	Data *struct {
		Nodes []struct {
			ID     string   `json:"id"`
			Kind   string   `json:"kind"`
			Label  string   `json:"label"`
			Fabric string   `json:"fabric"`
			Ports  []string `json:"ports"`
		} `json:"nodes"`
		Links []struct {
			Tail     string    `json:"tail"`
			TailPort string    `json:"tail_port"`
			Head     string    `json:"head"`
			HeadPort string    `json:"head_port"`
			Speeds   []float64 `json:"speeds"`
		} `json:"links"`
	} `json:"data"`
}

// GetNodeSanTopology reads the SAN wiring of one node, as the collector
// computes it from the adapters, the switch ports and the zoning, and
// derives the paths from each adapter port to the storage arrays.
func (s *Service) GetNodeSanTopology(ctx context.Context, node string) (NodeSanTopology, error) {
	found, err := s.lookupNode(ctx, node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return NodeSanTopology{}, err
	}
	var response collectorSanGraph
	if err := s.client.GetJSON(ctx, "/nodes/"+url.PathEscape(found.NodeID)+"/san", nil, &response); err != nil {
		return NodeSanTopology{}, fmt.Errorf("get SAN topology of node %s: %w", found.Nodename, err)
	}
	if response.Data == nil || len(response.Data.Nodes) > maxSanDevices || len(response.Data.Links) > maxSanLinks {
		return NodeSanTopology{}, fmt.Errorf("get SAN topology of node %s: unexpected collector response", found.Nodename)
	}
	result := NodeSanTopology{
		Node:    NodeReference{NodeID: found.NodeID, Nodename: found.Nodename, ClusterID: found.ClusterID},
		Wired:   len(response.Data.Links) > 0,
		HBAs:    []SanHBA{},
		Arrays:  []SanArrayPath{},
		Devices: make([]SanDevice, 0, len(response.Data.Nodes)),
		Links:   make([]SanLink, 0, len(response.Data.Links)),
	}
	devices := make(map[string]SanDevice, len(response.Data.Nodes))
	serverID := ""
	var serverPorts []string
	for _, item := range response.Data.Nodes {
		device := SanDevice{ID: item.ID, Kind: item.Kind, Name: item.Label, Fabric: item.Fabric, Ports: item.Ports}
		devices[item.ID] = device
		result.Devices = append(result.Devices, device)
		if item.Kind == "server" && serverID == "" {
			serverID, serverPorts = item.ID, item.Ports
		}
	}
	if serverID == "" {
		return NodeSanTopology{}, fmt.Errorf("get SAN topology of node %s: unexpected collector response without the node", found.Nodename)
	}
	outgoing := make(map[string][]SanLink)
	for _, item := range response.Data.Links {
		link := SanLink{From: item.Tail, FromPort: item.TailPort, To: item.Head, ToPort: item.HeadPort, SpeedsGbps: item.Speeds}
		if link.SpeedsGbps == nil {
			link.SpeedsGbps = []float64{}
		}
		result.Links = append(result.Links, link)
		outgoing[link.From] = append(outgoing[link.From], link)
	}

	name := func(id string) string {
		if device, ok := devices[id]; ok && device.Name != "" {
			return device.Name
		}
		return id
	}
	reach := newArrayReach()
	paths := 0
	for _, port := range serverPorts {
		hba := SanHBA{PortWWN: port, Paths: []SanPath{}}
		var walk func(link SanLink, fabric string, hops []SanHop, visited map[string]bool)
		walk = func(link SanLink, fabric string, hops []SanHop, visited map[string]bool) {
			if paths >= maxSanPaths {
				result.Truncated = true
				return
			}
			hops = append(hops[:len(hops):len(hops)], SanHop{
				From: name(link.From), FromPort: link.FromPort, To: name(link.To), ToPort: link.ToPort, SpeedsGbps: link.SpeedsGbps,
			})
			target := devices[link.To]
			if target.Kind == "array" {
				path := SanPath{Fabric: fabric, Hops: hops, Array: name(link.To), ArrayPort: link.ToPort, MinSpeedGbps: minHopSpeed(hops)}
				hba.Paths = append(hba.Paths, path)
				reach.add(path.Array, fabric, port, link.ToPort)
				paths++
				return
			}
			if visited[link.To] {
				return
			}
			visited[link.To] = true
			for _, next := range outgoing[link.To] {
				walk(next, fabric, hops, visited)
			}
			delete(visited, link.To)
		}
		for _, link := range outgoing[serverID] {
			if link.FromPort == port {
				walk(link, devices[link.To].Fabric, nil, map[string]bool{serverID: true})
			}
		}
		result.HBAs = append(result.HBAs, hba)
	}
	result.Arrays = reach.list()
	result.Provenance = s.newProvenance()
	return result, nil
}

// minHopSpeed is the lowest cable speed along hops, 0 when none is known.
func minHopSpeed(hops []SanHop) float64 {
	lowest := 0.0
	for _, hop := range hops {
		for _, speed := range hop.SpeedsGbps {
			if speed > 0 && (lowest == 0 || speed < lowest) {
				lowest = speed
			}
		}
	}
	return lowest
}

// arrayReach gathers, per storage array, what the paths of the node reach.
type arrayReach struct {
	order  []string
	arrays map[string]*arrayReachSets
}

type arrayReachSets struct {
	fabrics, hbas, ports map[string]bool
	paths                int
}

func newArrayReach() *arrayReach {
	return &arrayReach{arrays: make(map[string]*arrayReachSets)}
}

func (r *arrayReach) add(array, fabric, hba, port string) {
	sets, ok := r.arrays[array]
	if !ok {
		sets = &arrayReachSets{fabrics: map[string]bool{}, hbas: map[string]bool{}, ports: map[string]bool{}}
		r.arrays[array] = sets
		r.order = append(r.order, array)
	}
	if fabric != "" {
		sets.fabrics[fabric] = true
	}
	sets.hbas[hba] = true
	sets.ports[port] = true
	sets.paths++
}

func (r *arrayReach) list() []SanArrayPath {
	sort.Strings(r.order)
	result := make([]SanArrayPath, 0, len(r.order))
	for _, array := range r.order {
		sets := r.arrays[array]
		result = append(result, SanArrayPath{
			Array: array, Fabrics: sortedKeys(sets.fabrics), HBAs: sortedKeys(sets.hbas), TargetPorts: sortedKeys(sets.ports), Paths: sets.paths,
		})
	}
	return result
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

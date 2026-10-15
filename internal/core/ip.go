package core

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultListIPsLimit    = 50
	maxListIPsLimit        = 200
	maxServiceAddresses    = 200
	maxNetworkNameMatches  = 10
	ipResourceTypeFilter   = `res_type:~^ip(\.|$)`
	specialAddressesFilter = `addr:!~^(0\.|127\.|169\.254\.|2(2[4-9]|3[0-9])\.|::1?$|fe[89ab][0-9a-f]:|ff[0-9a-f]{2}:)`
)

var (
	nodeAddressProps    = []string{"id", "node_id", "nodename", "intf", "mac", "type", "addr", "mask", "updated", "flag_deprecated", "net_id", "net_name"}
	serviceAddressProps = []string{"id", "svc_id", "services.svcname", "node_id", "nodes.nodename", "rid", "res_type", "res_status", "res_desc", "updated"}
	networkProps        = []string{"id", "name", "network", "netmask"}
)

type ListIPsOptions struct {
	IP             string
	Node           string
	Network        string
	IncludeSpecial bool
	Limit          int
	Cursor         string
}

type IPList struct {
	Provenance                Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	IP                        string            `json:"ip,omitempty" jsonschema:"the searched address, in the canonical form the collector stores"`
	Node                      *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Network                   *NetworkReference `json:"network,omitempty" jsonschema:"the declared network the network filter resolved to"`
	SpecialExcluded           bool              `json:"special_addresses_excluded" jsonschema:"whether loopback, link-local, multicast and unspecified addresses were left out of node_addresses"`
	Total                     int               `json:"total" jsonschema:"the number of node addresses matching the filters, as the collector counts them"`
	Count                     int               `json:"count" jsonschema:"the number of node addresses returned in this page"`
	NodeAddresses             []NodeAddress     `json:"node_addresses" jsonschema:"the addresses the node inventories list, sorted by address, node name and interface; an address may be listed on several nodes or interfaces"`
	NextCursor                string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page of node_addresses"`
	Truncated                 bool              `json:"truncated" jsonschema:"whether matching node addresses remain after this page"`
	ServiceAddresses          []ServiceAddress  `json:"service_addresses,omitempty" jsonschema:"with ip, on the first page: the service IP resources whose description names the address, one per node, sorted by service, node and resource ID; absent when none names it"`
	ServiceAddressesTruncated bool              `json:"service_addresses_truncated,omitempty" jsonschema:"whether service IP resources were left out after 200"`
}

type NetworkReference struct {
	ID      int    `json:"id" jsonschema:"the collector network ID"`
	Name    string `json:"name" jsonschema:"the network name"`
	Network string `json:"network,omitempty" jsonschema:"the network address"`
	Netmask int    `json:"netmask,omitempty" jsonschema:"the network prefix length"`
}

type NodeAddress struct {
	Address        string `json:"address" jsonschema:"the address"`
	PrefixLength   string `json:"prefix_length,omitempty" jsonschema:"the prefix length of the address, such as 24 or 64"`
	Type           string `json:"type,omitempty" jsonschema:"ipv4 or ipv6"`
	Interface      string `json:"interface,omitempty" jsonschema:"the interface the address is on"`
	MAC            string `json:"mac,omitempty" jsonschema:"the MAC address of the interface"`
	NodeID         string `json:"node_id" jsonschema:"the collector ID of the node"`
	Nodename       string `json:"nodename" jsonschema:"the node the address is on"`
	NetworkID      int    `json:"network_id,omitempty" jsonschema:"the collector ID of the declared network containing the address, when one does"`
	NetworkName    string `json:"network_name,omitempty" jsonschema:"the name of the declared network containing the address, when one does"`
	Deprecated     bool   `json:"deprecated,omitempty" jsonschema:"whether the node reported the address deprecated"`
	LastReportedAt string `json:"last_reported_at,omitempty" jsonschema:"when the node last reported the address, as the collector gives it"`
}

type ServiceAddress struct {
	SvcID          string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname        string `json:"svcname" jsonschema:"the service whose resource holds the address"`
	NodeID         string `json:"node_id" jsonschema:"the collector ID of the node of the instance"`
	Nodename       string `json:"nodename" jsonschema:"the node of the instance"`
	RID            string `json:"rid" jsonschema:"the resource ID, such as ip#1"`
	Type           string `json:"type,omitempty" jsonschema:"the resource driver, such as ip.host or ip.netns"`
	Status         string `json:"status,omitempty" jsonschema:"the resource status the agent last reported on this node, such as up or down: up where the address is plumbed"`
	Description    string `json:"description,omitempty" jsonschema:"the description the agent reported, such as host 10.0.0.1/24 eth0; untrusted text"`
	LastReportedAt string `json:"last_reported_at,omitempty" jsonschema:"when the agent last reported the resource, as the collector gives it"`
}

type collectorNodeAddress struct {
	NodeID         string `json:"node_id"`
	Nodename       string `json:"nodename"`
	Intf           string `json:"intf"`
	MAC            string `json:"mac"`
	Type           string `json:"type"`
	Addr           string `json:"addr"`
	Mask           string `json:"mask"`
	Updated        string `json:"updated"`
	FlagDeprecated int    `json:"flag_deprecated"`
	NetID          *int   `json:"net_id"`
	NetName        string `json:"net_name"`
}

type collectorServiceAddress struct {
	SvcID     string `json:"svc_id"`
	Svcname   string `json:"services.svcname"`
	NodeID    string `json:"node_id"`
	Nodename  string `json:"nodes.nodename"`
	RID       string `json:"rid"`
	ResType   string `json:"res_type"`
	ResStatus string `json:"res_status"`
	ResDesc   string `json:"res_desc"`
	Updated   string `json:"updated"`
}

type collectorNetwork struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Network string `json:"network"`
	Netmask int    `json:"netmask"`
}

// ListIPs reads one page of the addresses the node inventories list, by
// address, node or declared network. With an address, the first page also
// lists the service IP resources whose description names it: a service
// address is listed on the node where it is plumbed, without the service.
func (s *Service) ListIPs(ctx context.Context, options ListIPsOptions) (IPList, error) {
	if options.IP == "" && options.Node == "" && options.Network == "" {
		return IPList{}, fmt.Errorf("pass at least one of ip, node or network")
	}
	if err := validateFilterValue("network", options.Network); err != nil {
		return IPList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListIPsLimit, maxListIPsLimit)
	if err != nil {
		return IPList{}, err
	}
	var result IPList
	if options.IP != "" {
		if result.IP, err = canonicalIP(options.IP); err != nil {
			return IPList{}, err
		}
	}
	var filters, serviceFilters []string
	if result.IP != "" {
		filters = append(filters, filterEquals("addr", result.IP))
		serviceFilters = append(serviceFilters, ipResourceTypeFilter, `res_desc:~(^|[[:space:]])`+regexp.QuoteMeta(result.IP)+`(/|[[:space:]]|$)`)
	} else if !options.IncludeSpecial {
		// The node inventories list the multicast groups the nodes joined and
		// their loopback and link-local addresses: most of the rows.
		filters = append(filters, specialAddressesFilter)
		result.SpecialExcluded = true
	}
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return IPList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
		serviceFilters = append(serviceFilters, filterEquals("node_id", node.NodeID))
	}
	if options.Network != "" {
		network, err := s.lookupNetwork(ctx, options.Network)
		if err != nil {
			return IPList{}, err
		}
		result.Network = &NetworkReference{ID: network.ID, Name: network.Name, Network: network.Network, Netmask: network.Netmask}
		filters = append(filters, filterEquals("net_id", strconv.Itoa(network.ID)))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return IPList{}, err
	}

	var response collectorList[collectorNodeAddress]
	if err := s.client.GetJSON(ctx, "/ips", pageQuery(nodeAddressProps, "addr,nodename,intf,id", filters, limit, offset), &response); err != nil {
		return IPList{}, fmt.Errorf("list node addresses: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return IPList{}, fmt.Errorf("list node addresses: unexpected collector response")
	}
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.NodeAddresses = make([]NodeAddress, 0, len(response.Data))
	for _, row := range response.Data {
		address := NodeAddress{
			Address: row.Addr, PrefixLength: row.Mask, Type: row.Type, Interface: row.Intf, MAC: row.MAC,
			NodeID: row.NodeID, Nodename: row.Nodename, NetworkName: row.NetName,
			Deprecated: row.FlagDeprecated != 0, LastReportedAt: row.Updated,
		}
		if row.NetID != nil {
			address.NetworkID = *row.NetID
		}
		result.NodeAddresses = append(result.NodeAddresses, address)
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}

	if result.IP != "" && offset == 0 {
		var resources collectorList[collectorServiceAddress]
		query := pageQuery(serviceAddressProps, "services.svcname,nodes.nodename,rid,id", serviceFilters, maxServiceAddresses, 0)
		if err := s.client.GetJSON(ctx, "/resources", query, &resources); err != nil {
			return IPList{}, fmt.Errorf("list service addresses: %w", err)
		}
		if resources.Meta.Total == nil || *resources.Meta.Total < 0 || len(resources.Data) > maxServiceAddresses {
			return IPList{}, fmt.Errorf("list service addresses: unexpected collector response")
		}
		result.ServiceAddresses = make([]ServiceAddress, 0, len(resources.Data))
		for _, row := range resources.Data {
			result.ServiceAddresses = append(result.ServiceAddresses, ServiceAddress{
				SvcID: row.SvcID, Svcname: row.Svcname, NodeID: row.NodeID, Nodename: row.Nodename, RID: row.RID,
				Type: row.ResType, Status: row.ResStatus, Description: row.ResDesc, LastReportedAt: row.Updated,
			})
		}
		result.ServiceAddressesTruncated = *resources.Meta.Total > len(resources.Data)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// canonicalIP checks an address given by the caller and returns the form
// the collector stores: IPv6 compressed in lowercase, so that equivalent
// spellings match.
func canonicalIP(value string) (string, error) {
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return "", fmt.Errorf("ip must be one IPv4 or IPv6 address, without prefix length or zone, such as 10.0.3.12 or 2001:db8::1")
	}
	return addr.String(), nil
}

// lookupNetwork resolves a declared network by its ID or its exact name.
func (s *Service) lookupNetwork(ctx context.Context, network string) (collectorNetwork, error) {
	filter := filterEquals("name", network)
	if _, err := strconv.Atoi(network); err == nil {
		filter = filterEquals("id", network)
	}
	query := url.Values{}
	query.Set("props", strings.Join(networkProps, ","))
	query.Set("filter", filter)
	query.Set("orderby", "id")
	query.Set("limit", strconv.Itoa(maxNetworkNameMatches))
	query.Set("meta", "false")
	var response collectorList[collectorNetwork]
	if err := s.client.GetJSON(ctx, "/networks", query, &response); err != nil {
		return collectorNetwork{}, fmt.Errorf("get network %s: %w", network, err)
	}
	switch len(response.Data) {
	case 0:
		return collectorNetwork{}, fmt.Errorf("get network %s: no declared network has this name or ID in the collector", network)
	case 1:
		return response.Data[0], nil
	default:
		ids := make([]string, 0, len(response.Data))
		for _, item := range response.Data {
			ids = append(ids, strconv.Itoa(item.ID)+" ("+item.Network+"/"+strconv.Itoa(item.Netmask)+")")
		}
		return collectorNetwork{}, fmt.Errorf("get network %s: the name matches several networks, pass one network ID: %s", network, strings.Join(ids, ", "))
	}
}

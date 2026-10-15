package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	maxNetworkNameMatches  = 10
	specialAddressesFilter = `addr:!~^(0\.|127\.|169\.254\.|2(2[4-9]|3[0-9])\.|::1?$|fe[89ab][0-9a-f]:|ff[0-9a-f]{2}:)`
	// addressResourceTypesFilter keeps the IP drivers whose description
	// names the address they plumb, as "host 10.0.0.1/24 eth0" or "netns
	// backend 10.0.0.1/24@eth1 in container#1". Other drivers name addresses
	// they do not hold, such as the gateway of an ip.route, or none, such as
	// ip.cni.
	addressResourceTypesFilter = "res_type:in:ip.host,ip.netns"
)

var (
	nodeAddressProps    = []string{"id", "node_id", "nodename", "intf", "mac", "type", "addr", "mask", "updated", "flag_deprecated", "net_id", "net_name"}
	serviceAddressProps = []string{"id", "svc_id", "services.svcname", "node_id", "nodes.nodename", "vmname", "rid", "res_type", "res_status", "res_desc", "updated"}
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
	Provenance       Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	IP               string            `json:"ip,omitempty" jsonschema:"the searched address, in the canonical form the collector stores"`
	Node             *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Network          *NetworkReference `json:"network,omitempty" jsonschema:"the declared network the network filter resolved to"`
	SpecialExcluded  bool              `json:"special_addresses_excluded" jsonschema:"whether loopback, link-local, multicast and unspecified addresses were left out of node_addresses"`
	Total            int               `json:"total" jsonschema:"the number of node addresses matching the filters, as the collector counts them"`
	ServiceTotal     *int              `json:"service_total,omitempty" jsonschema:"with ip, the number of service IP resources naming the address, as the collector counts them, once this page reached them"`
	Count            int               `json:"count" jsonschema:"the number of node and service addresses returned in this page"`
	NodeAddresses    []NodeAddress     `json:"node_addresses" jsonschema:"the addresses the node inventories list, sorted by address, node name and interface; an address may be listed on several nodes or interfaces"`
	ServiceAddresses []ServiceAddress  `json:"service_addresses,omitempty" jsonschema:"with ip, after the node addresses: the ip.host and ip.netns resources of the services whose description names the address, one per node and container, sorted by service, node, container and resource ID"`
	NextCursor       string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated        bool              `json:"truncated" jsonschema:"whether matching node or service addresses remain after this page"`
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
	VMName         string `json:"vmname,omitempty" jsonschema:"the container or virtual machine the resource runs in, when encapsulated"`
	RID            string `json:"rid" jsonschema:"the resource ID, such as ip#1"`
	Type           string `json:"type,omitempty" jsonschema:"the resource driver: ip.host or ip.netns"`
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
	VMName    string `json:"vmname"`
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

// ipCursor is the position of a page in the node addresses, then in the
// service addresses, bound to the filters of the listing.
type ipCursor struct {
	Nodes    int    `json:"o"`
	Services int    `json:"s"`
	Filters  string `json:"f"`
}

func encodeIPCursor(cursor ipCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeIPCursor(cursor string, fingerprint string) (ipCursor, error) {
	if cursor == "" {
		return ipCursor{Filters: fingerprint}, nil
	}
	invalid := fmt.Errorf("cursor is invalid: pass the next_cursor of a previous call with the same filters")
	if len(cursor) > maxListCursorLength {
		return ipCursor{}, invalid
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return ipCursor{}, invalid
	}
	var decoded ipCursor
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Nodes < 0 || decoded.Services < 0 || decoded.Nodes+decoded.Services < 1 || decoded.Filters != fingerprint {
		return ipCursor{}, invalid
	}
	return decoded, nil
}

// ListIPs reads one page of the addresses the node inventories list, by
// address, node or declared network. With an address, the page goes on with
// the service IP resources whose description names it: a service address
// is listed on the node where it is plumbed, without the service. The page
// size bounds the node and service addresses together.
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
	var addr netip.Addr
	if options.IP != "" {
		if addr, err = canonicalIP(options.IP); err != nil {
			return IPList{}, err
		}
		result.IP = addr.String()
	}
	var filters, serviceFilters []string
	searchServices := result.IP != ""
	if searchServices {
		filters = append(filters, filterEquals("addr", result.IP))
		// The address must be followed by its prefix length, as both drivers
		// write it: an address followed by a space may be another one's.
		serviceFilters = append(serviceFilters, addressResourceTypesFilter, `res_desc:~(^|[[:space:]])`+regexp.QuoteMeta(result.IP)+`/`)
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
		// A resource description names no network: its address is in the
		// network, as the collector joins the node addresses, or not.
		if searchServices && !networkContains(network, addr) {
			searchServices = false
		}
	}
	fingerprint := listFingerprint(url.Values{"filter": filters, "service_filter": serviceFilters})
	cursor, err := decodeIPCursor(options.Cursor, fingerprint)
	if err != nil {
		return IPList{}, err
	}

	var response collectorList[collectorNodeAddress]
	if err := s.client.GetJSON(ctx, "/ips", pageQuery(nodeAddressProps, "addr,nodename,intf,id", filters, limit, cursor.Nodes), &response); err != nil {
		return IPList{}, fmt.Errorf("list node addresses: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return IPList{}, fmt.Errorf("list node addresses: unexpected collector response")
	}
	result.Total = *response.Meta.Total
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
	next := ipCursor{Nodes: cursor.Nodes + len(response.Data), Services: cursor.Services, Filters: fingerprint}
	nodesLeft := len(response.Data) > 0 && next.Nodes < result.Total
	servicesLeft := false

	// The service addresses follow the node addresses, in the room the page
	// has left.
	if room := limit - len(response.Data); searchServices && !nodesLeft && room > 0 {
		var resources collectorList[collectorServiceAddress]
		query := pageQuery(serviceAddressProps, "services.svcname,nodes.nodename,vmname,rid,id", serviceFilters, room, cursor.Services)
		if err := s.client.GetJSON(ctx, "/resources", query, &resources); err != nil {
			return IPList{}, fmt.Errorf("list service addresses: %w", err)
		}
		if resources.Meta.Total == nil || *resources.Meta.Total < 0 || len(resources.Data) > room {
			return IPList{}, fmt.Errorf("list service addresses: unexpected collector response")
		}
		total := *resources.Meta.Total
		result.ServiceTotal = &total
		for _, row := range resources.Data {
			result.ServiceAddresses = append(result.ServiceAddresses, ServiceAddress{
				SvcID: row.SvcID, Svcname: row.Svcname, NodeID: row.NodeID, Nodename: row.Nodename, VMName: row.VMName, RID: row.RID,
				Type: row.ResType, Status: row.ResStatus, Description: row.ResDesc, LastReportedAt: row.Updated,
			})
		}
		next.Services += len(resources.Data)
		servicesLeft = len(resources.Data) > 0 && next.Services < total
	} else if searchServices && !nodesLeft {
		// The node addresses fill the page: only count the service addresses
		// a next page would read.
		var resources collectorList[collectorServiceAddress]
		if err := s.client.GetJSON(ctx, "/resources", pageQuery([]string{"id"}, "id", serviceFilters, 1, cursor.Services), &resources); err != nil {
			return IPList{}, fmt.Errorf("count service addresses: %w", err)
		}
		if resources.Meta.Total == nil || *resources.Meta.Total < 0 {
			return IPList{}, fmt.Errorf("count service addresses: unexpected collector response")
		}
		total := *resources.Meta.Total
		result.ServiceTotal = &total
		servicesLeft = cursor.Services < total
	} else if searchServices {
		// The node addresses go on: a next page reads the service addresses.
		servicesLeft = true
	}
	result.Count = len(result.NodeAddresses) + len(result.ServiceAddresses)
	if nodesLeft || servicesLeft {
		result.NextCursor = encodeIPCursor(next)
		result.Truncated = true
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// networkContains reports whether a declared network holds addr.
func networkContains(network collectorNetwork, addr netip.Addr) bool {
	base, err := netip.ParseAddr(network.Network)
	if err != nil {
		return false
	}
	prefix, err := base.Prefix(network.Netmask)
	return err == nil && prefix.Contains(addr)
}

// canonicalIP checks an address given by the caller. Its String is the form
// the collector stores: IPv6 compressed in lowercase, so that equivalent
// spellings match.
func canonicalIP(value string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("ip must be one IPv4 or IPv6 address, without prefix length or zone, such as 10.0.3.12 or 2001:db8::1")
	}
	return addr, nil
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

---
domain: ip
tools:
  - list_ips
stability: experimental
---

# IP Tools

This document describes the tool that finds the node or the service holding
an IP address, and lists the addresses of a node or of a declared network,
from the network inventory the OpenSVC agents report to the collector. It
reports inventory facts: an address listed on a node was reported there, which
does not prove that it answers or that it is in use.

Implementation:

- business logic: `internal/core/ip.go`, with the shared list helpers of
  `internal/core/collector_list.go` and the node lookup;
- MCP definitions: `internal/tools/ip.go`.

Visibility is that of the collector account of the caller: only the nodes and
services of the apps its groups are responsible for are listed, unless it is a
manager.

## Tools

### `list_ips`

Returns one page of the node addresses matching an address, a node or a
declared network, sorted by address, node name and interface. With an address,
the page goes on with the service IP resources whose description names it.
The page size counts both lists, and the cursor continues them: the node
addresses first, then the service addresses.

#### Collector API

The MCP first resolves the node, as `get_node` does, and the network:

```text
GET /api/networks?props=id,name,network,netmask&filter=id:eq:<id>|name:eq:<name>&orderby=id&limit=10&meta=false
```

It then reads the node addresses, from the `v_nodenetworks` view of the
collector, which joins each address to the declared network containing it:

```text
GET /api/ips?props=…&orderby=addr,nodename,intf,id&filter=addr:eq:<ip>&filter=node_id:eq:<node_id>&filter=net_id:eq:<net_id>&limit=…&offset=…
```

With an address, once the node addresses are read, it reads in the room the
page has left the `ip.host` and `ip.netns` resources of the services whose
description names the address, followed by its prefix length, as both drivers
write it: `host 10.29.0.21/24 br-prd`, `netns backend 10.100.0.46/24@eth1 in
container#0`. The slash keeps `10.29.0.2` from matching `10.29.0.21`. The
other IP drivers are not searched: an `ip.route` names its gateway, which it
does not hold, and an `ip.cni` names no address.

```text
GET /api/resources?props=…,services.svcname,nodes.nodename,vmname&orderby=services.svcname,nodes.nodename,vmname,rid,id&filter=res_type:in:ip.host,ip.netns&filter=res_desc:~(^|[[:space:]])<ip>/&filter=node_id:eq:<node_id>&limit=…&offset=…
```

When the node addresses fill the page, it only counts the service addresses
left, with `limit=1`, to tell whether a next page holds some.

#### Input

| Input | Meaning |
|---|---|
| `ip` | Optional exact IPv4 or IPv6 address, without prefix length or zone |
| `node` | Optional exact node name or node ID. A name several nodes hold is refused with their node IDs |
| `network` | Optional exact name or ID of a network declared in the collector. A name several networks hold is refused with their IDs |
| `include_special` | Optional; without `ip`, also lists the loopback, link-local, multicast and unspecified addresses |
| `limit` | Optional page size, node and service addresses together, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

Pass at least one of `ip`, `node` or `network`; they combine with AND, for
both lists. A resource description names no declared network: with `ip` and
`network`, the service addresses are listed only when the network contains
the address, as the collector joins the node addresses to it.

The collector stores IPv6 addresses compressed in lowercase and compares them
as text: the MCP rewrites `ip` in that canonical form, so that
`2001:0DB8::0021` finds `2001:db8::21`.

Without `ip`, the MCP leaves out by default the addresses of `0.0.0.0/8`,
`127.0.0.0/8`, `169.254.0.0/16`, `224.0.0.0/4`, `::`, `::1`, `fe80::/10` and
`ff00::/8`. The node inventories list every multicast group a node joined, and
these addresses are most of their rows: 539 of 619 on the test collector.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `ip` | The searched address, in canonical form |
| `node`, `network` | The node and the declared network the filters resolved to |
| `special_addresses_excluded` | Whether special addresses were left out |
| `total` | Node addresses matching the filters, as the collector counts them |
| `service_total` | With `ip`, service addresses naming it, as the collector counts them, once a page reached them |
| `count`, `truncated` | Node and service addresses in this page, and whether more remain in either list |
| `node_addresses[]` | `address`, `prefix_length`, `type`, `interface`, `mac`, `node_id`, `nodename`, `network_id`, `network_name`, `deprecated`, `last_reported_at` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |
| `service_addresses[]` | With `ip`, after the node addresses: `svc_id`, `svcname`, `node_id`, `nodename`, `vmname`, `rid`, `type`, `status`, `description`, `last_reported_at` |

An address may be listed on several nodes or interfaces: the private address
of a local bridge, such as `docker0`, `virbr0` or `lxcbr0`, is often the same
on every node. A service virtual IP is listed in `node_addresses` on the node
where it is plumbed, without the service; `service_addresses` names the
service, with the resource status on each of its nodes, `up` where the address
is plumbed. Container addresses of `ip.netns` resources are not in the node
inventories: they appear in `service_addresses` only.

The node inventories and the service resources are reported at different
times: each item keeps its own `last_reported_at`.

#### Limits

- An empty result means that no visible inventory or resource description
  names the address, not that the address is free.
- A service address is found only when the description of an `ip.host` or
  `ip.netns` resource names it. Resources whose address is allocated at run
  time, such as `ip.cni`, or whose description holds no address, such as
  `<nil>/24` or `host from network …`, cannot be found by address: 40 of the
  175 IP resources of the test collector.
- A resource is identified by its service, node, container (`vmname`) and
  resource ID: the same private address may be held in several containers.
- A network is a network declared in the collector. An arbitrary subnet, such
  as `10.0.3.0/24`, is not supported: the collector filters have no CIDR
  operator.

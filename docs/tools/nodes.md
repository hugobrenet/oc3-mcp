---
domain: node
tools:
  - get_node
  - get_node_san_topology
  - get_node_stats
  - list_node_disks
  - list_nodes
stability: experimental
---

# Node Tools

This document describes tools that report the nodes the OpenSVC collector
knows: their inventory, the cluster and app they belong to, and when their
daemon last reported. They answer which nodes exist and what a node is,
without a diagnostic verdict.

Implementation:

- business logic: `internal/core/node.go`, `internal/core/node_san.go`,
  `internal/core/node_disk.go` and `internal/core/node_stats.go`, with the
  shared list helpers of `internal/core/collector_list.go`;
- MCP definitions: `internal/tools/node.go`.

The collector stores what the node daemons push, plus the properties set by
its users. Visibility is that of the collector account of the caller.

- `asset_status` is an inventory property of the node record, not a live
  state. `last_comm` tells when the node daemon last contacted the collector.
- Collector dates are returned as the collector gives them, in its local time
  without a zone, such as `2026-10-12 11:00:07.000`.
- Several clusters may hold the same node name. The node ID is unique.

## Tools

### `list_nodes`

Returns one page of nodes, sorted by node name then node ID. Use it to find a
node, or the nodes of a cluster, app or environment.

#### Collector API

```text
GET /api/nodes?props=…&orderby=nodename,node_id&filter=…&limit=…&offset=…
```

The filters are built by the MCP from the typed inputs; the collector filter
language is not exposed.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the node name contains, case-insensitive |
| `cluster_id` | Optional exact cluster ID |
| `app` | Optional exact collector app |
| `env` | Optional exact node environment, such as `PRD` or `DEV` |
| `frozen` | Optional; `true` lists only the frozen nodes |
| `frozen_before` | Optional; lists only the nodes frozen at or before this date, in collector local time: `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or `YYYY-MM-DD HH:MM:SS`. It implies `frozen` |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

Filter values are at most 255 bytes of printable text without surrounding
spaces. A cursor used with other filters is refused.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Nodes matching the filters, as the collector counts them |
| `count`, `truncated` | Nodes in this page, and whether more remain |
| `nodes[]` | `node_id`, `nodename`, `cluster_id`, `app`, `env`, `asset_status`, `os`, `agent_version`, `last_comm`, `frozen`, `frozen_at` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

Pages follow the collector offsets: a node added or removed between two calls
can shift the following pages.

`frozen_at` is always returned, empty when the node is not frozen. It is also
empty for a frozen node when the collector does not store the freeze date its
daemon reports: `frozen_before` then lists no node, while `frozen` still lists
them. `last_comm` is not the freeze date.

### `get_node`

Returns one node by its exact name or node ID. Use it after `list_nodes`, or
when the operator names a node.

#### Collector API

```text
GET /api/nodes/<nodename or node_id>?props=…&meta=false
```

An unknown node returns the collector 404 message. A name that several nodes
hold is refused, with their node and cluster IDs: pass one node ID.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or collector node ID, without spaces or `/` |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `fqdn`, `cluster_id`, `app`, `env`, `asset_status`, `role`, `type`, `frozen`, `frozen_at`, `snooze_till` |
| `agent` | `version`, `last_comm`, and `updated`, the last update of the node record |
| `os` | `name`, `vendor`, `release`, `kernel`, `arch`, `timezone` (the offset the node reported), `last_boot` |
| `hardware` | `manufacturer`, `model`, `serial`, CPU `model`, `vendor`, `cores`, `threads`, `dies` and `freq` (MHz), `mem_mb`, `mem_banks`, `mem_slots`, `bios_version`, `sp_version` |
| `teams` | `responsible`, `integration`, `support` |
| `location` | `country`, `city`, `address`, `zip`, `building`, `floor`, `room`, `rack` |
| `lifecycle` | `warranty_end`, `maintenance_end`, hardware and operating system obsolescence warning and alert dates |

`mem_mb` is the memory size in MiB: the collector names it `mem_bytes` but
stores MiB. Empty text fields are omitted. Power, chassis and blade fields are
left out.

### `get_node_san_topology`

Returns the fibre channel SAN wiring of one node and the paths from its
adapters to the storage arrays. Use it to check the redundancy of the SAN
paths of a node: an array reached through one fabric only, an adapter
reaching no array, or a slower hop.

The collector computes the wiring from the adapters of the node, the ports of
the SAN switches and the zoning: the node and its adapter ports, the switches
these ports are plugged in, the inter-switch links followed from there, and
the storage arrays owning the target ports zoned to the adapters. A switch
reached through an inter-switch link is kept only when it leads to one of
those target ports. A node without a fibre channel wiring known to the
collector, such as an iSCSI node, has no cable.

#### Collector API

The MCP first resolves the node, as `get_node` does, then reads:

```text
GET /api/nodes/<node_id>/san
```

The collector returns the wiring as a graph of devices and cables. The MCP
follows the cables from each adapter port to the array ports to build the
paths, and sums them per array.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID. A name several nodes hold is refused with their node IDs |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | The node whose wiring is described |
| `wired` | Whether the collector knows a fibre channel wiring of the node |
| `hbas[]` | Each adapter port `port_wwn` and its `paths[]`: `fabric`, `hops[]`, `array`, `array_port`, `min_speed_gbps` |
| `arrays[]` | Each storage array reached: `array`, `fabrics`, `hbas`, `target_ports`, `paths` |
| `devices[]` | The devices of the wiring: `id`, `kind` (`server`, `switch` or `array`), `name`, `fabric`, `ports` |
| `links[]` | The cables of the wiring: `from`, `from_port`, `to`, `to_port`, `speeds_gbps` |
| `truncated` | Whether paths were left out after 500 |

A hop is one cable, from the adapter port to a switch, between switches, or
from a switch to an array port: `from`, `from_port`, `to`, `to_port` and
`speeds_gbps`. An aggregated inter-switch link holds several ports, such as
`46,47`, and one speed per cable. The fabric of a path is the fabric of its
first switch. An array reached through a single fabric, or by a single
adapter, has one entry in `fabrics` or `hbas`.

### `list_node_disks`

Returns one page of the disks one node sees, as the collector knows them, with
the other nodes seeing each shared disk. Use it to tell which LUNs a node sees,
on which storage array, which service uses them, and whether a disk is visible
from the other nodes a service may fail over to.

The collector merges what the agent of each node reports, the disks it sees
and the service using each, with what the storage arrays declare. A disk used
by several services is one row per service. A shared disk is named by its
world wide identifier, such as `36001405…`. A local disk is named by its device,
such as `vda` or `zd0`, which other nodes reuse for other disks: it is never
reported as shared.

#### Collector API

The MCP first resolves the node, as `get_node` does, then reads the page and
the other nodes of its shared disks:

```text
GET /api/disks?props=…&orderby=disk_id,svc_id&filter=node_id:eq:<node_id>&filter=…&limit=…&offset=…
GET /api/disks?props=disk_id,node_id,nodename&filter=disk_id:in:<disk_id>,…&limit=5000&meta=false
```

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID. A name several nodes hold is refused with their node IDs |
| `disk_id` | Optional text the disk ID contains, case-insensitive, such as the end of a world wide identifier |
| `limit` | Optional page size, 1 to 200, default 100 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | The node whose disks are listed |
| `total` | Disk rows matching the filters, as the collector counts them |
| `count`, `truncated` | Rows in this page, and whether more remain |
| `disks[]` | `disk_id`, `size_mb`, `used_mb`, `vendor`, `model`, `array`, `array_disk_group`, `disk_group`, `region`, `svc_id`, `svcname`, `shared_with`, `updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

Sizes are in MiB. `array` and `array_disk_group` are empty when no storage
array declares the disk, `svcname` when the collector does not know the
service using it. `disk_group` is the disk group the node reports, such as a
volume group or a pool. `shared_with` lists the other nodes seeing the same
disk, and is empty for a local disk.

### `get_node_stats`

Sums up the performance statistics the collector keeps for one node over the
last days, for one group. Use it to tell whether a node is or was loaded,
saturated or swapping, and when: the CPU, memory, swap, load, I/O and network
usage, where `get_node` only gives the capacity of the node.

The collector keeps the statistics the agents push, in series of points whose
resolution coarsens with age: a minute over half an hour, ten minutes over
three days, an hour over ninety days, a day beyond.

| Group | Series |
|---|---|
| `cpu` | `usr`, `sys`, `iowait`, `idle`, `steal`, `nice`, `irq`, `soft`, `guest`, `gnice`, in % |
| `mem` | `pct_memused`, `pct_commit`, `kbmemused`, `kbavail`, `kbcached`… |
| `swap` | `pct_swpused`, `kbswpused`… |
| `proc` | load averages `ldavg_1`, `ldavg_5`, `ldavg_15`, process counts |
| `block` | I/O operations and throughput of the node |
| `netdev`, `netdev_err` | traffic and errors per network interface |
| `blockdev` | utilization, queue and latency per disk |

The metrics are named as the collector names them, after sar; their names
tell their units. The device groups hold one series per device and metric.

#### Collector API

The MCP first resolves the node, as `get_node` does, then reads:

```text
GET /api/nodes/<node_id>/stats?group=<group>&days=<days>
```

The route has no time range: it returns the last days up to now.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID. A name several nodes hold is refused with their node IDs |
| `group` | `cpu`, `mem`, `swap`, `proc`, `block`, `netdev`, `netdev_err` or `blockdev` |
| `days` | Optional days of history, default 1, at most 1095, or 30 for the device groups |
| `metric` | Optional exact metric, such as `usr` or `pct_memused` |
| `device` | Optional exact device, for the device groups |
| `samples` | Optional number of equal time slices, up to 48, whose means each series returns to show the trend; 0 by default |
| `at` | Optional UTC time, RFC 3339, to read the value of each series at that time. The days read are extended to cover it, within the limit of the group |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `group`, `days`, `at` | What the statistics are about; `days` is the days read |
| `total_series` | Series the collector returned, before the filters |
| `count`, `truncated` | Series returned, and whether some were left out after 200 |
| `series[]` | `metric`, `device`, `points`, `first_at`, `last_at`, `last`, `min`, `avg`, `max`, `max_at`, and with `at` the `at_value` of the closest point at `at_point`, and with `samples` the `samples[]` with their `at` and `value` |
| `devices` | For a device group, the devices having series, to pass as `device` |

Times are UTC. A series without points has null values. The value at a time is
that of the closest point: its distance to the time asked depends on the
resolution of the period. Means are rounded to three decimals.

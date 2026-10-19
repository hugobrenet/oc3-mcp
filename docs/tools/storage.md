---
domain: storage
tools:
  - list_storage_arrays
  - list_storage_quotas
  - list_san_switch_ports
stability: experimental
---

# Storage Tools

This document describes the tools that report the storage of the OpenSVC
collector: the storage arrays and their disk groups, the storage quotas of the
apps on these disk groups, and the ports of the SAN switches. The SAN paths of
one node, from its adapters to the arrays, are given by
`get_node_san_topology` ([node tools](nodes.md)).

Implementation:

- business logic: `internal/core/storage.go`, with the shared list helpers
  of `internal/core/collector_list.go`;
- MCP definitions: `internal/tools/storage.go`.

Sizes are in MB, as the collector records them, and port speeds in Gbit/s.

## Tools

### `list_storage_arrays`

Returns one page of the storage arrays, sorted by name, each with its disk
groups, or pools, and the totals of its disk groups.

#### Collector API

```text
GET /api/arrays?props=id,array_name,array_model,array_firmware,array_cache,array_comment,array_updated&orderby=array_name,id&filter=array_name:~<name>&filter=array_model:~<model>&limit=…&offset=…
GET /api/arrays/<id>/diskgroups?props=id,dg_name,dg_size,dg_used,dg_free,dg_reserved,dg_updated&orderby=dg_name,id&limit=100
```

The disk groups are read by array ID: the collector does not find an array by
its name in this path.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the array name contains, case-insensitive |
| `model` | Optional text the array model contains, case-insensitive |
| `limit` | Optional page size, 1 to 50, default 20 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Arrays matching, as the collector counts them |
| `count`, `truncated` | Arrays in this page, and whether more remain |
| `arrays[]` | `id`, `name`, `model`, `firmware`, `cache`, `comment`, `updated`, the totals `size_mb`, `used_mb`, `free_mb`, and `disk_groups[]` |
| `arrays[].disk_groups[]` | `id`, `name`, `size_mb`, `used_mb`, `free_mb`, `reserved_mb`, `used_percent`, `updated` |

`reserved_mb` is `dg_reserved` as the collector records it: oc3 does not
compute it, the value comes from the array feeds, and its meaning is not
verified on the data.
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

### `list_storage_quotas`

Returns one page of the storage quotas the apps hold on the disk groups,
sorted by app, array and disk group.

#### Collector API

```text
GET /api/apps?props=id,app&orderby=app,id&limit=500
GET /api/apps/<app>/quotas?props=app,array_name,array_model,dg_name,quota,quota_used,dg_size,dg_free,dg_reservable&orderby=array_name,dg_name&filter=array_name:eq:<array>&limit=100
```

The collector gives the quotas of one app at a time. Without `app`, the MCP
reads the apps the caller sees, at most 100, then the quotas of each, and
sorts and pages them itself; `scan_truncated` tells when apps were left out.

#### Input

| Input | Meaning |
|---|---|
| `app` | Optional exact app code or app ID |
| `array` | Optional exact array name |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Quotas matching |
| `scan_truncated` | Without `app`, whether apps beyond the first 100 were not read |
| `count`, `truncated` | Quotas in this page, and whether more remain |
| `quotas[]` | `app`, `array`, `array_model`, `disk_group`, `quota_mb`, `used_mb`, `disk_group_size_mb`, `disk_group_free_mb`, `disk_group_reservable_mb` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

An unknown app is refused with the collector's not found error.

Every page without `app` reads the apps and all their quotas again, up to 101
requests: the cursor is an offset in the sorted quotas. The bound is low and
the description asks the model to pass `app`.

`used_mb` is the space of the disks of the app on the disk group, as the
collector links them: through `diskinfo.disk_group`, which the node feed of
the collector does not write, only the array feeds may. Without it, the used
space is zero, which the description tells the model to read as unknown as
often as unused: on dev2, every quota has a used space of zero.

### `list_san_switch_ports`

Returns one page of the ports of the SAN switches, sorted by fabric, switch,
slot and port, with what is plugged on their other end.

#### Collector API

```text
GET /api/nodes/<node>?props=node_id,nodename,cluster_id
GET /api/san-switches?props=sw_fabric,sw_name,sw_index,sw_slot,sw_port,sw_portspeed,sw_portnego,sw_porttype,sw_portstate,sw_portname,sw_rportname,sw_rname,node_id,sw_updated&orderby=sw_fabric,sw_name,sw_slot,sw_port,id&filter=…&limit=…&offset=…
```

The filters are `sw_fabric:eq:`, `sw_name:eq:`, `sw_portstate:eq:`,
`sw_porttype:eq:`, `sw_rname:eq:` and, for `node`, `node_id:eq:` with the
resolved node ID. Every authenticated user sees every port.

When no port matches, each of `fabric`, `switch` and `connected_to` given is
checked against the names of the ports:

```text
GET /api/san-switches?props=<prop>&groupby=<prop>&orderby=<prop>&filter=<prop>:~<value>&limit=20
```

For a name no port has, `names_containing` gives the names containing it,
else the first 20 names: a model tends to pass a short form, such as `a` for
`lyn-fabric-a` or `edge01` for `lyn-sana-edge01`, and would read the empty
page as no port. They are a result, not an error: the OpenSVC AI agent does
not take an identifier from a failed tool output.

#### Input

| Input | Meaning |
|---|---|
| `fabric` | Optional exact fabric name |
| `switch` | Optional exact switch name |
| `state` | Optional exact port state, such as `Online` or `No_Light` |
| `type` | Optional port type: `F-Port`, `E-Port`, `U-Port`, `N-Port`, `L-Port` or `G-Port` |
| `connected_to` | Optional exact name of what is on the other end: a node, an array or a switch |
| `node` | Optional exact node name or node ID |
| `limit` | Optional page size, 1 to 500, default 100 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | With `node`, the node it resolved to |
| `total` | Ports matching, as the collector counts them |
| `count`, `truncated` | Ports in this page, and whether more remain |
| `ports[]` | `fabric`, `switch`, `slot`, `port`, `index`, `speed_gbps`, `negotiation`, `type`, `state`, `port_wwn`, `remote_port_wwn`, `connected_to`, `node_id`, `updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |
| `names_containing` | When no port matches, for each of `fabric`, `switch` and `connected_to` that no port has: the names containing it, else the first 20 |

---
domain: node
tools:
  - get_node
  - list_nodes
stability: experimental
---

# Node Tools

This document describes tools that report the nodes the OpenSVC collector
knows: their inventory, the cluster and app they belong to, and when their
daemon last reported. They answer which nodes exist and what a node is,
without a diagnostic verdict.

Implementation:

- business logic: `internal/core/node.go`, with the shared list helpers of
  `internal/core/collector_list.go`;
- MCP definitions: `internal/tools/node.go`.

The collector stores what the node daemons push, plus the properties set by
its users. Visibility is that of the collector account of the MCP.

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
| `nodes[]` | `node_id`, `nodename`, `cluster_id`, `app`, `env`, `asset_status`, `os`, `agent_version`, `last_comm`, `frozen` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

Pages follow the collector offsets: a node added or removed between two calls
can shift the following pages.

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

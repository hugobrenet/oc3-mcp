---
domain: node
tools:
  - create_node
  - update_node
  - delete_node
stability: experimental
---

# Node Action Tools

This document describes the tools that create, update and delete the nodes
of the OpenSVC collector inventory. They change the collector state, and are
registered only when `OPENSVC_MCP_ACTIONS=enabled` ([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/node_action.go`;
- MCP definitions: `internal/tools/node_action.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `create_node` | not read-only, not destructive | Manager |
| `update_node` | destructive, idempotent | NodeManager, and responsible for the node |
| `delete_node` | destructive, idempotent | NodeManager, and responsible for the node |

The OpenSVC AI agent asks the user to confirm a destructive tool before it
runs: the update and the deletion. The collector decides the access, and
records each change in its log: `node.add`, `node.change`, `node.delete`.

## Properties

The three tools set the same properties of a node; the others are left to the
agent of the node.

| Property | Meaning |
|---|---|
| `app` | App code; must be an app of the collector, never empty |
| `team_responsible` | Group responsible for the node, which may manage it; must be a group of the collector, never empty |
| `status`, `role` | Free texts the organization defines, such as `In service` |
| `assetname` | Asset name in the organization inventory |
| `warranty_end`, `maintenance_end` | Dates, `YYYY-MM-DD` |
| `asset_env`, `sec_zone`, `team_integ`, `team_support` | Asset environment, security zone, integration and support teams |
| `loc_country`, `loc_city`, `loc_addr`, `loc_zip`, `loc_building`, `loc_floor`, `loc_room`, `loc_rack` | Location |

The agent never sends `app`, `team_responsible`, `status`, `role`,
`assetname`, `warranty_end` and `maintenance_end`. It rewrites `asset_env`,
`sec_zone`, `team_integ`, `team_support` and the location at each push when
the configuration of its node sets them, and always rewrites the hardware,
operating system, name and cluster of the node: the tools do not set these.

The MCP validates the properties before sending anything, as the collector
does not:

- it takes from a Manager an app or a group it does not know: the MCP reads
  `GET /api/apps/<app>` and `GET /api/groups/<group>`, refuses an unknown one,
  and sends the names as the collector writes them;
- it answers a malformed date with an internal error: the MCP takes
  `YYYY-MM-DD` only. The collector gives a date back as a datetime, such as
  `2027-12-31 00:00:00.000`.

A value is at most 255 bytes of printable text without surrounding spaces.

## Tools

### `create_node`

Creates one node by its name, with optional properties.

#### Collector API

```text
GET  /api/apps/<app>, GET /api/groups/<team_responsible>   when given
GET  /api/nodes?props=node_id,nodename,cluster_id&filter=nodename:eq:<nodename>&limit=10
POST /api/nodes   {"nodename": …, properties}
```

The collector creates or updates a node by its name on the same route: the
MCP refuses a name a node already has, compared without case, with the node
IDs, and sends nothing. The node ID is never given: the collector generates
it. Between the check and the creation, a node created by someone else with
the same name would be updated: the collector has no create-only route.

Without `app` or `team_responsible`, the collector gives the default app and
the default group of the caller, on dev2 the personal group `user_<id>`.
The node holds its properties only until an agent registers and pushes it.

#### Input

| Input | Meaning |
|---|---|
| `nodename` | Name of the node, without spaces, slashes or control characters, at most 255 bytes |
| properties | Optional, see [properties](#properties); an empty value is ignored |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `cluster_id` of the created node |
| `properties` | `app`, `team_responsible` and the properties set, as the collector holds them |
| `not_applied[]` | `property`, `requested`, `applied`: the properties the collector holds with another value than asked for |

### `update_node`

Sets properties of one node. A property left out is not changed; an empty
value clears it, except `app` and `team_responsible`.

#### Collector API

```text
GET  /api/apps/<app>, GET /api/groups/<team_responsible>   when given
GET  /api/nodes/<node>?props=node_id
GET  /api/nodes/<node_id>?props=node_id,nodename,cluster_id,<properties>
POST /api/nodes/<node_id>   {properties}
```

The node is one exact node name or node ID; a name several nodes hold is
refused with their IDs. The values before the update are read first, so that
none is lost without a trace.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID |
| properties | At least one, see [properties](#properties) |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `cluster_id` |
| `changes[]` | `property`, `before`, `after`, for each property set |
| `not_applied[]` | `property`, `requested`, `applied`: the properties the collector holds with another value than asked for |

### `delete_node`

Deletes one node from the collector, irreversibly.

#### Collector API

```text
GET    /api/nodes/<node>?props=node_id
GET    /api/nodes/<node_id>?props=node_id,nodename,cluster_id
DELETE /api/nodes/<node_id>
```

The collector deletes the node and, in one transaction, its addresses
(`node_ip`), adapters (`node_hba`), tags, packages, patches, service instance
states and their history (`svcmon`, `svcmon_log`), resource history
(`resmon_log`), live checks, alerts (`dashboard`), SAN zones and the
credentials of its agent (`auth_node`): the agent must register again to
push. The node itself is not stopped or changed. Its compliance, sysreport
and statistics history are not deleted.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `cluster_id` of the deleted node |
| `deleted` | `true` |

---
domain: node
tools:
  - create_node
  - update_node
  - delete_node
  - attach_node_tag
  - detach_node_tag
  - attach_node_ruleset
  - detach_node_ruleset
  - attach_node_moduleset
  - detach_node_moduleset
stability: experimental
---

# Node Action Tools

This document describes the tools that create, update and delete the nodes
of the OpenSVC collector inventory, and attach to them, or detach from them,
tags and compliance rulesets and modulesets. They change the collector state, and are
registered only when `OPENSVC_MCP_ACTIONS=enabled` ([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/node_action.go`, `internal/core/node_tag.go`
  and `internal/core/node_compliance.go`, with the node selection of
  `internal/core/node_selection.go`;
- MCP definitions: `internal/tools/node_action.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `create_node` | not read-only, not destructive | Manager |
| `update_node` | destructive, idempotent | NodeManager, and responsible for the node |
| `delete_node` | destructive, idempotent | NodeManager, and responsible for the node |
| `attach_node_tag` | not read-only, not destructive, idempotent | Responsible for each node |
| `detach_node_tag` | destructive, idempotent | Responsible for each node |
| `attach_node_ruleset`, `detach_node_ruleset`, `attach_node_moduleset`, `detach_node_moduleset` | destructive, idempotent | Responsible for each node |

The OpenSVC AI agent asks the user to confirm a destructive tool before it
runs: the update, the deletion, the detachments and the compliance
attachments. A compliance attachment is destructive though it adds: it makes
the agents run modules, which may change the nodes, as a module in autofix
does at the next run, on up to 1000 nodes at once. The collector decides the access, and
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

### `attach_node_tag` and `detach_node_tag`

Attach one tag to nodes, or detach it from them. The collector has no bulk
route: the MCP sends one request per node, four at a time, and a change is
not atomic. On dev2, 403 nodes took 3.5 s to attach and 2.8 s to detach.

#### Selection

The nodes are given one of two ways:

- `nodes`: at most 50 exact node names or node IDs, read in one request
  (`filter=nodename:in:…`, `filter=node_id:in:…`). An unknown, invisible or
  ambiguous name refuses the whole call before any change; a name given
  twice is changed once.
- filters, at least one of those of `list_nodes` and `count_nodes`: the MCP
  reads the selection, and refuses one of more than 1000 nodes, or one that
  changed while it was read, before any change. The confirmation of the
  agent shows the filters, not the names of hundreds of nodes, and the
  result gives the number selected and the outcome on each node.

The tag filter of `list_nodes` is `filter_tag` here, as `tag` names the tag
changed: to detach a tag from every node holding it, pass the same tag as
`tag` and `filter_tag`. At least one filter is required: no call changes
every node.

| Filter | Meaning |
|---|---|
| `name` | Text the node name contains |
| `cluster_id` | Exact cluster ID |
| `app`, `env`, `status` | Exact app, node environment, node status |
| `frozen`, `frozen_before` | Frozen nodes, frozen at or before a date |
| `os_name`, `os_vendor` | Text the operating system name or vendor contains |
| `os_release`, `agent_version` | Exact release or version; ending with a dot, a family |
| `manufacturer`, `model` | Text the hardware manufacturer or model contains |
| `team` | Text the responsible team contains |
| `country`, `city` | Text the location contains |
| `silent_for` | Nodes silent for longer than a duration, such as `7d` |
| `filter_tag` | Nodes holding a tag |
| `filterset` | Nodes a filterset selects; not with `filter_tag` |

#### Collector API

```text
GET    /api/tags?props=tag_id,tag_name&filter=tag_name:eq:<tag>
GET    /api/nodes, /api/tags/<tag_id>/nodes or /api/filtersets/<id>/nodes?props=node_id,nodename,cluster_id&filter=…&limit=200&offset=…
POST   /api/tags/<tag_id>/nodes/<node_id>   {"tag_attach_data": …}   attach
DELETE /api/tags/<tag_id>/nodes/<node_id>                            detach
```

The collector takes the tag by its ID, not its name, and the node by its ID.
It answers an attachment already there, or a detachment of a tag the node
does not hold, with 200 and an info saying so; it refuses a tag incompatible
with the other tags of the node, by their exclusions, with 409, and a node
the caller is not responsible for with 403.

#### Input

| Input | Meaning |
|---|---|
| `tag` | One exact tag name, case-insensitive, or tag ID |
| `nodes` | Optional, see [selection](#selection) |
| filters | Optional, see [selection](#selection) |
| `attach_data` | `attach_node_tag` only: optional data of the attachment, as JSON text, at most 4096 bytes |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `tag` | `tag_id`, `name` |
| `filter_tag`, `filterset` | The tag or filterset filter, resolved |
| `selected` | Nodes selected |
| `changed`, `unchanged`, `refused` | Nodes changed, already as asked, refused by the collector |
| `nodes[]` | `node_id`, `nodename`, `outcome` (`attached`, `already_attached`, `detached`, `already_detached`, `refused`, `not_attempted` when the call ended before) and `reason` of a refusal, sorted by node name |

### `attach_node_ruleset`, `detach_node_ruleset`, `attach_node_moduleset` and `detach_node_moduleset`

Attach one compliance ruleset or moduleset to nodes, or detach it from them.
A moduleset is the modules the agent runs at each compliance check, which
a compliance fix lets change the node; a ruleset is the variables, the
expected values the modules check. Only an explicit ruleset is attached: a
contextual one applies to the nodes its filterset selects. A moduleset has
no filterset: it applies to the nodes and services it is attached to, and
through the modulesets including it.

The nodes are selected as for the tags, see [selection](#selection), by
`nodes` or by filters; here the tag filter of `list_nodes` keeps its name,
`tag`. One request per node, four at a time, not atomic.

#### Collector API

```text
GET    /api/compliance/rulesets/<ruleset>?props=id,ruleset_name,ruleset_type
GET    /api/compliance/modulesets/<moduleset>?props=id,modset_name
GET    /api/nodes, /api/tags/<tag_id>/nodes or /api/filtersets/<id>/nodes?props=node_id,nodename,cluster_id&filter=…
POST   /api/nodes/<node_id>/compliance/rulesets/<id>     attach
DELETE /api/nodes/<node_id>/compliance/rulesets/<id>     detach
POST   /api/nodes/<node_id>/compliance/modulesets/<id>   attach
DELETE /api/nodes/<node_id>/compliance/modulesets/<id>   detach
```

The collector requires the caller to be responsible for the node, and
attaches a ruleset or a moduleset published to the group responsible for
the node, `team_responsible`, not to the caller, or published to Everybody;
it refuses another with 403, `not attachable to this node`. It answers 409
an attachment already there, `already attached`, and a detachment of what
is not attached, `not attached`: the MCP reports them as `already_attached`
and `already_detached`. It writes no log entry for these changes.

The collector checks that a ruleset is explicit and public only when it is
not published to Everybody: it attaches a contextual ruleset published to
Everybody. The MCP refuses to attach a contextual ruleset, before any
change, and detaches one.

#### Input

| Input | Meaning |
|---|---|
| `ruleset` or `moduleset` | One exact name or ID |
| `nodes` | Optional, see [selection](#selection) |
| filters | Optional, those of `list_nodes`, with `tag`; see [selection](#selection) |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `ruleset` or `moduleset` | `id`, `name`, and for a ruleset `type` |
| `filter_tag`, `filterset`, `selected`, `changed`, `unchanged`, `refused`, `nodes[]` | As for the tags |

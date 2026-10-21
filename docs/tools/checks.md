---
domain: check
tools:
  - get_check_threshold
  - list_check_thresholds
  - list_node_checks
  - list_service_checks
  - set_check_threshold
  - unset_check_threshold
stability: experimental
---

# Check Tools

This document describes tools that report the checks the OpenSVC agents send
to the collector: measures of the filesystems, inodes, multipath paths, volume
groups, links and pools of a node, compared to thresholds. They answer which
measures are out of their thresholds and why, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/check.go`, with the node and service
  lookups; the thresholds in `internal/core/check_threshold.go`, their
  changes in `internal/core/check_threshold_write.go`;
- MCP definitions: `internal/tools/check.go` and
  `internal/tools/check_threshold.go`.

Visibility is that of the collector account of the caller.

- A check measures one instance of a type: a mount point for `fs_u` and
  `fs_i`, a multipath WWID for `mpath`, an interface link for `eth`. A check
  of a resource of a service, such as a volume, also names that service.
- The collector sets the thresholds of a check from the first source that has
  some: the settings of the check on the node (`settings`), the thresholds of
  a filterset matching the node and the service (`fset:<name>`), or the
  defaults of the check type (`defaults`). A missing bound is null.
- The collector flags a check out of bounds when its value is below its low
  or above its high threshold. It raises a `check out of bounds` alert for it,
  which `list_alerts` returns: use `list_alerts` for the checks out of bounds
  across all the nodes, and these tools for the checks of one node or service.
- Values are in the unit of the check type, as the agent reports it. The
  `fs_u` instances ending with `.free` or `.size` are sizes, the others
  percentages; the API does not state the units.

The collector does not filter the checks of a node or a service: the MCP reads
at most 2000 of them, the checks out of bounds first, and filters them.

## Tools

### `list_node_checks`

Returns the checks of one node, those of the resources of its services
included, with the name of their service. Use it to see what is out of bounds
on a node.

#### Collector API

```text
GET /api/nodes/<node>?props=node_id,nodename,cluster_id&meta=false
GET /api/nodes/<node_id>/checks?props=…&orderby=-chk_err,chk_type,chk_instance,id&limit=2000
GET /api/nodes/<node_id>/services?props=svc_id,services.svcname&limit=1000&meta=false
```

The last call names the services of the checks.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID. A name several nodes hold is refused with their node IDs |
| `type` | Optional exact check type, such as `fs_u`, `fs_i`, `mpath`, `vg_u`, `eth` or `zpool` |
| `out_of_bounds_only` | Optional; `true` keeps only the checks out of bounds |

### `list_service_checks`

Returns the checks of the resources of one service, on all its nodes, with
the name of their node. Use it to see whether the volumes or filesystems of a
service are out of bounds.

#### Collector API

```text
GET /api/services/<service>?props=svc_id,svcname,cluster_id&meta=false
GET /api/services/<svc_id>/checks?props=…&orderby=-chk_err,chk_type,chk_instance,id&limit=2000
GET /api/services/<svc_id>/nodes?props=node_id,nodes.nodename&limit=1000&meta=false
```

The last call names the nodes of the checks.

#### Input

| Input | Meaning |
|---|---|
| `service` | One exact service name or service ID. A name several services hold is refused with their service IDs |
| `type` | Optional exact check type |
| `out_of_bounds_only` | Optional; `true` keeps only the checks out of bounds |

### Output of both tools

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` or `service` | The node or service whose checks are listed |
| `total` | Checks the collector holds for it, before the filters |
| `out_of_bounds_read` | Checks read that are out of bounds, before the type filter |
| `count` | Checks returned |
| `checks[]` | Out of bounds first, then by type and instance: `id`, `type`, `instance`, `value`, `low`, `high`, `out_of_bounds`, `threshold_source`, `node_id`, `nodename`, `svc_id`, `svcname`, `created`, `updated` |
| `truncated` | Whether checks were left unread after 2000 |

`updated` is when the agent last reported the check, as the collector gives
it.

### How the collector sets the thresholds of a check

The collector writes `chk_low`, `chk_high` and `chk_threshold_provider` in
`checks_live` (`ChecksLiveUpdateThresholds`, `cdb/db_checks_feed.go`), from
the first source that has some:

1. `settings`: the row of `checks_settings` of the node, the type and the
   instance of the check, whose low threshold is set. The collector stores
   the service with the key but does not compare it there: a setting
   applies to the checks of the same type and instance of every service of
   the node (oc3 todo 16). The types `netdev_err` and `save` are left out
   (`checksNotPurged`): a setting of such a check is stored, and never
   applied.
2. `fset:<name>`: among the contextual thresholds of the type whose
   pattern, an unanchored Go regular expression, matches the instance, and
   whose filterset holds the node, and the service of a check of a
   service, the last by ID: the most recently created wins, not the most
   precise.
3. `defaults`: among the defaults of the type whose pattern matches the
   instance, as SQL compares it, `instance RLIKE CONCAT("^", pattern, "$")`,
   the pattern itself or an empty one, the highest priority, then the
   longest pattern, then the lowest ID. Without one, the thresholds are null
   and the provider is `defaults` all the same: the check has none.

The thresholds are integers. A change of the settings of a check is applied
to the checks of its node before the collector answers; a change of the
defaults or of the contextual thresholds is applied in the background, on
every node having checks of the type, within 30 minutes.

### `get_check_threshold`

Explains the thresholds of one check: the check, as `list_node_checks`
gives it, then each source. Use it for "why is this check out of bounds",
"where does this threshold come from" and "what if I remove it".

```text
GET /api/nodes/<node>?props=node_id,nodename,cluster_id&meta=false
GET /api/services/<service>?props=svc_id,svcname&meta=false               with service
GET /api/checks/live?filter=node_id:eq:…&filter=svc_id:eq:…&filter=chk_type:eq:…&filter=chk_instance:eq:…
GET /api/checks/settings?filter=node_id:eq:…&filter=chk_type:eq:…&filter=chk_instance:eq:…
GET /api/checks/contextual_settings?filter=chk_type:eq:…&orderby=-id&limit=500
GET /api/filtersets/<fset_id>/nodes?filter=node_id:eq:…&limit=1           each matching pattern, 20 at most
GET /api/filtersets/<fset_id>/services?filter=svc_id:eq:…&limit=1         for a check of a service
GET /api/checks/defaults?filter=chk_type:eq:…&limit=500
```

The collector compares the filters case-insensitively: the MCP keeps the
rows of the exact spelling. It orders the candidates as the collector does,
and marks `wins` on the first contextual thresholds that apply and on the
first defaults. The defaults are matched with a Go regular expression,
case-insensitively as MySQL compares under its collation: an exotic pattern
may read otherwise there. A check of the node is read with `svc_id:empty`, which matches the null
`svc_id` of rows of the historical collector as well as the empty one; the
collector writes still find no such row (oc3 todo 17). The thresholds of the check, `check.low`,
`check.high` and `check.threshold_source`, are those the collector applies,
whatever the MCP reads of the sources.

A check the node does not report is refused with the instances of its type
on the node, 50 at most, those of a service named with it, so that the
model picks one without another call.

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID |
| `type` | The exact check type |
| `instance` | The exact instance, such as `/var` or `/var.free` |
| `service` | For a check of a resource of a service, one exact service name or service ID |

| Output field | Meaning |
|---|---|
| `check` | The check, as `list_node_checks` gives it |
| `settings[]` | The settings of the node, type and instance: `low`, `high`, `changed`, `changed_by` (masked), `svc_id`, `svcname` |
| `contextual[]` | The contextual thresholds whose pattern matches, most recent first: `filterset`, `pattern`, `low`, `high`, `applies`, `reason`, `wins` |
| `defaults[]` | The defaults whose pattern matches, in the order of the collector: `pattern`, `low`, `high`, `priority`, `wins` |
| `without_settings` | The source and thresholds the check would take without its settings: `fset:<name>`, `defaults`, `none`, or `unknown` when no contextual thresholds read apply but some were not read |
| `truncated` | Whether defaults or contextual thresholds were left unread after 500 |
| `unchecked_memberships` | The contextual thresholds whose filterset membership was not read, after 20 |

### `list_check_thresholds`

Lists one page of the rows of one scope, with their IDs.

```text
GET /api/checks/settings?props=…,nodes.nodename,services.svcname          scope settings
GET /api/checks/defaults?orderby=chk_type,-chk_prio,id                     scope defaults
GET /api/checks/contextual_settings?props=…,gen_filtersets.fset_name       scope contextual
```

The settings are those of the nodes the caller is responsible for, as the
collector restricts them; the defaults and the contextual thresholds are
not restricted. `changed_by` is an email address: `mask.go` keeps the name.

| Input | Meaning |
|---|---|
| `scope` | `settings`, `defaults` or `contextual` |
| `type` | Optional exact check type |
| `node` | With `settings`, optional exact node name or node ID |
| `filterset` | With `contextual`, optional exact filterset name or ID |
| `limit`, `cursor` | Page size, 50 by default, 500 at most, and the `next_cursor` of the previous page |

### `set_check_threshold` and `unset_check_threshold`

Set the thresholds of one check, or remove them. Registered only when
`OPENSVC_MCP_ACTIONS=enabled`. Neither is destructive, as the SLA: nothing
is lost, `unset_check_threshold` gives the check its contextual or default
thresholds again, so the agent asks no confirmation.

```text
POST   /api/checks/settings {"node_id": …, "svc_id": …, "chk_type": …, "chk_instance": …, "chk_low": …, "chk_high": …}
DELETE /api/checks/settings {"node_id": …, "svc_id": …, "chk_type": …, "chk_instance": …}
```

The MCP resolves the node and the service to their collector IDs, which the
collector takes, and sends the key, `svc_id` only for a check of a service,
with the thresholds given, integers by the input schema. A threshold not
given keeps the value the check has now; both must then be known, and low
at most high, or the collector answers 400. With neither, the collector
records the current thresholds as settings, which the changes of the
defaults and the contextual thresholds then no longer reach: the MCP sends
it as given, the collector accepting it. The collector requires the
CheckExec or the Manager privilege and to be responsible for the node, by
its apps, and logs the change with the email of the caller.

The MCP sends the change without reading the check first. When the
collector answers 404 `check instance not found`, the MCP tells the
instances of the type on the node, as `get_check_threshold` does. The 500
`thresholds saved, but the checks could not take them yet` is a change
made: `outcome` is `set` or `removed`, with `warning`. A removal answered
404 `no thresholds set` is `unchanged`. The MCP recognises these three
messages by their start, as `server/handlers/checks.go` of oc3 words them:
reworded there, they come back as the collector errors they are.

The collector applies the change to the checks of the node, and their
alerts, before it answers: the MCP reads the check back, `read_back`, with
its thresholds, their source and whether it is out of bounds.

| Input | Meaning |
|---|---|
| `node`, `type`, `instance`, `service` | The check, as for `get_check_threshold` |
| `low`, `high` | `set_check_threshold`: the thresholds, integers; one not given keeps its value |

| Output field | Meaning |
|---|---|
| `outcome` | `set`, `removed`, or `unchanged` for a removal of no settings |
| `read_back` | The check read back, as `list_node_checks` gives it |
| `warning` | The check not read back, or the change saved but not applied yet |

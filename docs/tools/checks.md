---
domain: check
tools:
  - list_node_checks
  - list_service_checks
stability: experimental
---

# Check Tools

This document describes tools that report the checks the OpenSVC agents send
to the collector: measures of the filesystems, inodes, multipath paths, volume
groups, links and pools of a node, compared to thresholds. They answer which
measures are out of their thresholds and why, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/check.go`, with the node and service
  lookups;
- MCP definitions: `internal/tools/check.go`.

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

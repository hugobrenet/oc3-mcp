---
domain: service
tools:
  - count_instances
  - count_services
  - get_service
  - get_service_resource_history
  - get_service_status_history
  - list_instances
  - list_service_resources
  - list_services
stability: experimental
---

# Service Tools

This document describes tools that report the services the OpenSVC collector
knows: their aggregated status, their configured nodes and the state of their
instances on each node. They answer which services exist, which ones are down
or degraded, and where a service runs, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/service.go`,
  `internal/core/service_count.go`,
  `internal/core/service_status_history.go`,
  `internal/core/service_resource.go`,
  `internal/core/service_resource_history.go` and
  `internal/core/service_instance.go`, with the shared list and count
  helpers of `internal/core/collector_list.go` and
  `internal/core/collector_stats.go`, and the node lookup of
  `internal/core/node.go`;
- MCP definitions: `internal/tools/service.go`.

The collector stores the states the node daemons last pushed. Visibility is
that of the collector account of the caller.

- A service name is an OpenSVC object path, such as `ns1/svc/web`. Several
  clusters may hold the same name; the service ID is unique.
- States are those the collector last received, not a live probe. Collector
  dates are returned as the collector gives them.
- `get_service` returns the configuration text of a service, as its agents
  pushed it, masked partially: see below.

## Tools

### `list_services`

Returns one page of services, sorted by name then service ID. Use it to find
a service, the services down or degraded, or the services of a cluster, app,
environment or node.

#### Collector API

Without `node`:

```text
GET /api/services?props=…&orderby=svcname,svc_id&filter=…&limit=…&offset=…
```

With `node`, the MCP resolves the node, then reads the services having an
instance on it:

```text
GET /api/nodes/<node>?props=node_id,nodename,cluster_id&meta=false
GET /api/services_instances?props=svc_id,services.…&orderby=services.svcname,svc_id&filter=node_id:eq:<node_id>&filter=…
```

The filters are built by the MCP from the typed inputs; the collector filter
language is not exposed.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the service name contains, case-insensitive |
| `cluster_id` | Optional exact cluster ID |
| `app` | Optional exact collector app |
| `env` | Optional exact service environment, such as `PRD` or `DEV` |
| `availstatus` | Optional exact availability status, such as `up`, `down`, `warn`, `n/a` or `undef` |
| `status` | Optional exact service status, the aggregated overall status, such as `up`, `down`, `warn`, `n/a` or `undef`, as the collector Services page shows it: a service is up or down by it |
| `topology` | Optional topology: `failover` or `flex` |
| `frozen` | Optional freeze state of the service as a whole: `frozen` (every instance), `unfrozen` or `mixed` |
| `placement` | Optional placement state: `optimal` or `non-optimal` |
| `node` | Optional exact node name or node ID: only the services with an instance on it. A name several nodes hold is refused with their node IDs |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | With `node`, the node it resolved to: `node_id`, `nodename`, `cluster_id` |
| `total` | Services matching the filters, as the collector counts them |
| `count`, `truncated` | Services in this page, and whether more remain |
| `services[]` | `svc_id`, `svcname`, `cluster_id`, `app`, `env`, `topology`, `status` (the aggregated overall status), `availstatus`, `frozen` (`frozen`, `unfrozen`, `mixed` or `n/a`), `placement`, `provisioned`, `nodes` (the nodes of the configuration), `status_updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

Pages follow the collector offsets: a service added or removed between two
calls can shift the following pages.

### `count_services`

Counts the services matching the filters of `list_services`, except `node`,
as the collector computes it, and with `group_by` breaks them down by one
property, the most frequent values first. Use it for how many services, and
for their distribution by state, instead of reading every page of
`list_services`.

#### Collector API

```text
GET /api/services?props=svc_id&filter=…&limit=1
GET /api/services?stats=true&props=<grouping prop>&filter=…&limit=…
```

With `stats`, the collector counts the rows of each value over the whole
selection. It groups the values case-insensitively and shows one spelling of
each, and refuses a count longer than 5 seconds.

#### Input

| Input | Meaning |
|---|---|
| `name` … `placement` | The filters of `list_services` except `node`, with the same meaning, `status` included |
| `group_by` | Optional grouping: `cluster_id`, `app`, `env`, `topology`, `status`, `availstatus`, `frozen`, `placement` or `provisioned` |
| `limit` | Optional number of values returned with `group_by`, 1 to 100, default 20 |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Services matching the filters |
| `group_by` | The grouping asked |
| `groups[]` | With `group_by`: `value`, empty for the services without one, and `count`, the most frequent first |
| `distinct` | With `group_by`: distinct values among the matching services |
| `other`, `truncated` | With `group_by`: matching services whose value was left out by `limit`, and whether there are any |

### `get_service`

Returns one service by its exact name or service ID, with its instances. Use
it after `list_services`, or when the operator names a service.

#### Collector API

```text
GET /api/services/<svcname or svc_id>?props=…&meta=false
GET /api/services/<svc_id>/nodes?props=…&orderby=nodes.nodename,node_id&limit=200
```

An unknown service returns the collector 404 message. A name that several
services hold is refused, with their service and cluster IDs: pass one
service ID.

#### Input

| Input | Meaning |
|---|---|
| `service` | One exact service name, such as `ns1/svc/web`, or collector service ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service` | `svc_id`, `svcname`, `cluster_id`, `app`, `env`, `comment` (untrusted text), `created`, `updated`, `snooze_till` |
| `config` | `topology`, `ha`, `autostart`, `nodes`, `drp_node`, `drp_nodes`, `drp_type`, `flex_min_nodes`, `flex_max_nodes`, `flex_target`, and the configuration `text`, `text_truncated`, `text_updated` |
| `status` | Aggregated `overall`, `avail`, `frozen`, `placement`, `provisioned`, and `updated` |
| `availability` | `rate_percent`, the availability of the last 30 days the collector computes, `sla_percent`, the target, each null when unknown, and `updated` |
| `instances` | `total`, `count`, `truncated` after 200, and `items[]` sorted by node name |

Each instance holds `node_id`, `nodename`, `avail`, `overall`,
`monitor_state` (such as `idle`), `monitor_global_expect` (such as `none`),
`frozen`, `frozen_at`, `updated`, and `resources`: the status of its `ip`,
`fs`, `disk`, `container`, `share`, `sync`, `app` and `hb` resources.

#### Configuration text

`config.text` is the `svc_config` the collector stores, the configuration the
agents pushed, cut to 32 KiB, and `text_updated` when it last received it. The
collector serves it to the same account; the MCP masks, with the rules of
`internal/core/mask.go`:

- the value of a `key = value` line whose key names a secret or a mail, such
  as `env.DB_PASSWORD` or `acme_email`, and its indented continuation lines;
  a key scoped to a node, such as `password@node1`, is read without its scope;
- the email addresses of the values and of the comments, not of the keys,
  which may hold a node scope such as `schedule@node1.example.com`.

`secrets_environment` and `configs_environment` stay in clear: their values
name the keys of the secret and configuration objects to read, such as
`DB_PASSWORD=app/db_password`, not the secrets. The masking is partial: a
secret under a key that names none stays in clear, and the tool description
tells the model not to present the text as free of secrets.

### `get_service_status_history`

Returns the status history of one service over the last days, or with `node`,
that of its instance on that node. Use it to tell since when a service or an
instance is down, how often it changed status, and its availability rate.

#### Collector API

The MCP first resolves the service, and the node when given, as
`get_service` and `get_node` do, then reads:

```text
GET /api/services/<svc_id>/status_log?days=<days>
GET /api/services/<svc_id>/instances/<node_id>/status_log?days=<days>
```

The justifications of the periods are written through other collector routes,
which no tool calls.

#### Input

| Input | Meaning |
|---|---|
| `service` | One exact service name or service ID. A name several services hold is refused with their service IDs |
| `node` | Optional exact node name or node ID: the history of the instance on that node instead of the service |
| `days` | Optional days of history, 1 to 365, default 7 |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service, and the node with `node`, the history is about |
| `days` | The days asked |
| `availability` | For the service only: `from`, `to`, `rate_percent`, `available_seconds`, `excluded_seconds`, `counted_seconds` |
| `total`, `count`, `truncated` | Periods the collector returned, periods kept, and whether the oldest were left out after 500 |
| `periods[]` | Oldest first: `begin`, `end`, and `status` for the service or `avail` and `overall` for an instance, with `ack` when justified |

The collector computes the rate as the time up or standby up over the counted
time, from the start of the history when later than the days asked. The time
no status covers counts as downtime; justified downtime marked not to count is
left out, in `excluded_seconds`. A justification `ack` holds `comment`
(untrusted text), `counted`, `acked_by` and `acked_on`. The end of the current
period is the last status received. Periods are in collector local time
without a zone.

### `list_service_resources`

Returns one page of the resources of one service, as its agents last reported
them, one per node, sorted by node name then resource ID. Use it to find which
resource of a service is down or degraded, and on which node.

#### Collector API

The MCP first resolves the service, and the node when given, then reads:

```text
GET /api/resources?props=…&orderby=nodes.nodename,rid,id&filter=svc_id:eq:<svc_id>&filter=…&limit=…&offset=…
```

#### Input

| Input | Meaning |
|---|---|
| `service` | One exact service name or service ID. A name several services hold is refused with their service IDs |
| `node` | Optional exact node name or node ID: only the resources of the instance on that node |
| `status` | Optional exact resource status, such as `up`, `down`, `warn`, `n/a`, `undef`, `stdby up` or `stdby down` |
| `type` | Optional exact resource driver, such as `fs.flag`, `disk.disk` or `container.kvm` |
| `limit` | Optional page size, 1 to 200, default 100 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service, and the node with `node`, the filters resolved to |
| `total` | Resources matching the filters, as the collector counts them |
| `count`, `truncated` | Resources in this page, and whether more remain |
| `resources[]` | `rid`, `type`, `status`, `description`, `log`, `log_truncated`, `node_id`, `nodename`, `monitored`, `disabled`, `optional`, `vmname`, `changed`, `updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`log` is the last status message the agent reported for the resource, cut to
4 KiB; it may be empty. `description` and `log` are untrusted text. `changed`
is when the status last changed and `updated` when the agent last reported the
resource, both as the collector gives them.

### `list_instances`

Returns one page of the service instances, one per service and node, and per
container of an encapsulated service, as their agents last reported them,
sorted by node then service. The collector holds one row per container of an
encapsulated service on a node: on the test collector, 345 instances for 339
service and node pairs. Use it to tell which
instances are down on a node, where a service runs, or which services are
frozen and since when.

A failover service runs on one node at a time: its instances on the other
nodes are `down`, or `stdby up` when standby resources run there, as expected
while the service is up. Each instance therefore carries the topology and the
aggregated availability of its service: to find the services really down,
filter on `service_availability` `down` rather than on the availability of
the instances. On the test collector, 22 of the 84 instances down on one node
belonged to failover services up on another node.

The freeze date is known per instance only: the aggregated freeze state of a
service (`frozen`, `mixed`, `unfrozen`) has no date, and may differ from the
states of its instances. With `frozen` or `frozen_before`, the instances come
the longest frozen first.

#### Collector API

With `service` or `node`, the MCP first resolves it, then reads:

```text
GET /api/services_instances?props=…,services.svcname,services.svc_topology,services.svc_availstatus,nodes.nodename&orderby=nodes.nodename,services.svcname,svc_id,node_id,mon_vmname&filter=…&limit=…&offset=…
```

With `frozen` or `frozen_before`, it adds `filter=mon_frozen:ne:0` and sorts
by `mon_frozen_at,svc_id,node_id,mon_vmname`.

#### Input

| Input | Meaning |
|---|---|
| `node` | Optional exact node name or node ID |
| `service` | Optional exact service name or service ID |
| `app` | Optional exact collector app of the service |
| `availability` | Optional exact availability status of the instance, such as `up`, `down`, `warn`, `stdby up`, `stdby down` or `n/a` |
| `overall` | Optional exact overall status of the instance |
| `service_availability` | Optional exact aggregated availability status of the service |
| `topology` | Optional service topology: `failover` or `flex` |
| `frozen` | Optional; keeps the frozen instances |
| `frozen_before` | Optional; keeps the instances frozen at or before this date, in collector local time: `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or `YYYY-MM-DD HH:MM:SS` |
| `limit` | Optional page size, 1 to 200, default 100 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service or node the filters resolved to |
| `total` | Instances matching the filters, as the collector counts them |
| `count`, `truncated` | Instances in this page, and whether more remain |
| `instances[]` | `svc_id`, `svcname`, `cluster_id`, `topology`, `service_availability`, `service_frozen`, `node_id`, `nodename`, `availability`, `overall`, `frozen`, `frozen_at`, `monitor_status`, `vmname`, `vmtype`, `last_reported_at` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`vmname` and `vmtype` name the container or virtual machine of an
encapsulated instance. `frozen_at` and `last_reported_at` are in collector
local time without a zone, while `provenance.observed_at` is in UTC: to ask
for the instances frozen for more than a duration, compute the date in the
collector local time.

### `count_instances`

Counts the service instances matching the filters of `list_instances`, as the
collector counts them, optionally grouped by one property: such as the
instances down per node.

#### Collector API

```text
GET /api/services_instances?filter=…&props=<grouping prop>&stats=true&limit=…
```

Without `group_by`, the MCP reads one row and the collector `meta.total`.

#### Input

The filters of `list_instances`, and:

| Input | Meaning |
|---|---|
| `group_by` | Optional grouping: `node`, `service`, `cluster_id`, `app`, `availability`, `overall`, `topology`, `service_availability` or `frozen` |
| `limit` | Optional number of values returned with `group_by`, the most frequent first, 1 to 100, default 20 |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service or node the filters resolved to |
| `total` | Instances matching the filters |
| `group_by`, `groups[]` | With `group_by`: each `value` and its `count`, the most frequent first |
| `distinct`, `other`, `truncated` | The number of distinct values, the instances whose value was left out by `limit`, and whether some were |

Grouped by `frozen`, the collector freeze flags are named `frozen` and
`unfrozen`.

### `get_service_resource_history`

Returns the status history of the resources of one service, on all its nodes
or one, grouped by resource and node. Use it to tell since when a resource is
down or degraded, on which nodes, and which resource changed status first. It
is the level below `get_service_status_history`, which describes the service.

#### Collector API

The MCP first resolves the service, and the node when given, then reads the
periods and the nodes of the service, to name them:

```text
GET /api/services/<svc_id>/resources/logs?props=…&limit=5000
GET /api/services/<svc_id>/nodes/<node_id>/resources/logs?props=…&limit=5000
GET /api/services/<svc_id>/nodes?props=node_id,nodes.nodename&limit=1000&meta=false
```

The routes neither filter nor sort the periods: the MCP reads at most 5000,
filters and sorts them.

#### Input

| Input | Meaning |
|---|---|
| `service` | One exact service name or service ID. A name several services hold is refused with their service IDs |
| `node` | Optional exact node name or node ID: the resources of the instance on that node |
| `rid` | Optional exact resource ID, such as `container#1` or `fs#1` |
| `status` | Optional exact resource status, such as `down`, `warn` or `undef` |
| `since` | Optional; keeps the periods ending at or after this date, in collector local time: `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or `YYYY-MM-DD HH:MM:SS` |
| `limit` | Optional number of the most recent matching periods to return, 1 to 500, **default 100** |

By default, only the **100 most recent matching periods** are returned, across
all the resources and nodes: a service with many resources holds hundreds of
periods. Narrow the history with `node`, `rid`, `status` or `since`, or raise
`limit`, to see older periods.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service, and the node with `node`, the filters resolved to |
| `read`, `matching`, `count` | Periods read, matching the filters, and returned |
| `resources[]` | One per resource and node: `rid`, `node_id`, `nodename`, `current_status`, `since`, `last_reported`, and `periods[]` with their `begin`, `end`, `status` and `log` |
| `truncated` | Whether periods were left unread after 5000, or older matching ones left out after `limit` |

Each resource and node gives the status of its most recent matching period,
since when, and when the agent last reported it; its periods follow, the most
recent first. The end of the current period is the last status the agent
reported. `nodename` is empty for a node the service no longer has an instance
on. `log` is the status message of the period, cut to 4 KiB: untrusted text.
Periods are in collector local time without a zone.

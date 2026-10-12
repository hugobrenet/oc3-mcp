---
domain: service
tools:
  - get_service
  - list_services
stability: experimental
---

# Service Tools

This document describes tools that report the services the OpenSVC collector
knows: their aggregated status, their configured nodes and the state of their
instances on each node. They answer which services exist, which ones are down
or degraded, and where a service runs, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/service.go`, with the shared list helpers of
  `internal/core/collector_list.go` and the node lookup of
  `internal/core/node.go`;
- MCP definitions: `internal/tools/service.go`.

The collector stores the states the node daemons last pushed. Visibility is
that of the collector account of the MCP.

- A service name is an OpenSVC object path, such as `ns1/svc/web`. Several
  clusters may hold the same name; the service ID is unique.
- States are those the collector last received, not a live probe. Collector
  dates are returned as the collector gives them.
- The configuration text of a service is never returned: it may hold secrets.

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
| `services[]` | `svc_id`, `svcname`, `cluster_id`, `app`, `env`, `topology`, `availstatus`, `overall_status`, `frozen` (`frozen`, `unfrozen`, `mixed` or `n/a`), `placement`, `provisioned`, `nodes` (the nodes of the configuration), `status_updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

Pages follow the collector offsets: a service added or removed between two
calls can shift the following pages.

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
| `config` | `topology`, `ha`, `autostart`, `nodes`, `drp_node`, `drp_nodes`, `drp_type`, `flex_min_nodes`, `flex_max_nodes`, `flex_target` |
| `status` | Aggregated `overall`, `avail`, `frozen`, `placement`, `provisioned`, and `updated` |
| `availability` | `rate_percent`, the availability of the last 30 days the collector computes, `sla_percent`, the target, each null when unknown, and `updated` |
| `instances` | `total`, `count`, `truncated` after 200, and `items[]` sorted by node name |

Each instance holds `node_id`, `nodename`, `avail`, `overall`,
`monitor_state` (such as `idle`), `monitor_global_expect` (such as `none`),
`frozen`, `frozen_at`, `updated`, and `resources`: the status of its `ip`,
`fs`, `disk`, `container`, `share`, `sync`, `app` and `hb` resources.

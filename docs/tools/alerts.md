---
domain: alert
tools:
  - list_alerts
stability: experimental
---

# Alert Tools

This document describes the tool that reports the alerts of the OpenSVC
collector dashboard. The collector raises them by crossing what all the nodes
report: they answer what is wrong across the nodes and services it knows,
without a diagnostic verdict.

Implementation:

- business logic: `internal/core/alert.go`, with the shared list helpers of
  `internal/core/collector_list.go` and the node and service lookups;
- MCP definitions: `internal/tools/alert.go`.

Visibility is that of the collector account of the caller.

## Tools

### `list_alerts`

Returns one page of dashboard alerts, the most severe first, then the most
recently updated. Use it to see what is wrong, on the whole collector or on
one node or service.

The alert types include `service unavailable`, `service available but
degraded`, `check out of bounds`, `check value not updated`, `node
information not updated`, `service configuration not updated`, `package
differences in cluster`, `service placement`, `flex error`, `action errors`
and the node maintenance or warranty alerts.

#### Collector API

```text
GET /api/alerts?props=…&orderby=-dash_severity,-dash_updated,id&filter=…&limit=…&offset=…
```

With `node` or `service`, the MCP first resolves it, as `get_node` and
`get_service` do, and filters on its ID. The filters are built by the MCP from
the typed inputs; the collector filter language is not exposed.

#### Input

| Input | Meaning |
|---|---|
| `min_severity` | Optional lowest severity, 0 to 5 |
| `type` | Optional exact alert type |
| `node` | Optional exact node name or node ID. A name several nodes hold is refused with their node IDs |
| `service` | Optional exact service name or service ID. A name several services hold is refused with their service IDs |
| `env` | Optional exact environment, such as `PRD` or `DEV` |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service the filters resolved to |
| `total` | Alerts matching the filters, as the collector counts them |
| `count`, `truncated` | Alerts in this page, and whether more remain |
| `alerts[]` | `id`, `type`, `severity`, `message`, `node_id`, `nodename`, `svc_id`, `svcname`, `env`, `instance`, `created`, `updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

The severity goes from 0 to 5, higher being more severe. The collector raises
the severity of most alerts by one on a `PRD` node or service. `message` is the
text the collector renders for the alert: untrusted text. `instance` names the
item the alert is about within the node or service, such as a check instance
or a device. `created` and `updated` are returned as the collector gives them.
The internal format, dictionary and checksum fields of the dashboard are left
out.

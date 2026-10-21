---
domain: service_sla
tools:
  - set_service_sla
stability: experimental
---

# Service SLA Tool

This document describes the tool that sets the SLA of the services of the
OpenSVC collector, or removes it. It is registered only when
`OPENSVC_MCP_ACTIONS=enabled` ([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/service_sla.go`, with the selection of
  `internal/core/service_selection.go`;
- MCP definitions: `internal/tools/service_sla.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `set_service_sla` | not destructive, idempotent | Responsible for each service, by its app |

The SLA of a service is its availability target, a percent between 0 and
100. The collector stores it beside the availability it computes over 30
days, and `get_service` gives both: a service under its target is below its
SLA. Nothing else in the collector reads it: no alert, no report. The agents
never send it, unlike the app, the environment or the comment of a service,
which their configuration rewrites: the value set stays.

The tool is not destructive, so the agent asks no confirmation: the value
replaces the one the services had, which another call sets again. A
service already as asked, at the target asked or without a target to
remove, is changed all the same, as the collector decides, and told
`unchanged`: the collector writes a log entry for it. The selection reads
the target and the availability of each service, so that it costs no
request more.

The collector stores three decimals and rounds the others away: the MCP
rounds the target to three decimals before it sends it, so that the value
asked, 99.9995 for example, is the one held, 100.

## Selection

The services are selected as for the [service tag tools](service-tags.md#selection):
`services`, at most 50 exact names or IDs, or the filters of `list_services`
and those of the instances, `node`, `on_nodes`, an object of the filters of
`list_nodes`, and `instance_availability`, `instance_overall` and
`instance_frozen`, up to 1000 services. For example, the services with an
instance on the nodes in maintenance: `on_nodes: {"status": "Maintenance"}`.
One request per service, four at a time, not atomic.

## Collector API

```text
POST /api/services/<svc_id>   {"svc_sla": "99.9"}   set
POST /api/services/<svc_id>   {"svc_sla": ""}       remove
```

The collector takes the target as text, a percent between 0 and 100, a
trailing `%` and a decimal comma accepted, and refuses another with 400; it
refuses a number. The empty text removes the target. The MCP sends the
target rounded to three decimals. It requires the caller
to be responsible for the service, by a group responsible for its app, or
Manager, refuses another with 403, logs each change, and answers with the
service as it holds it, whose `svc_sla` the result gives.

## Input

| Input | Meaning |
|---|---|
| `sla` | The availability target, a percent between 0 and 100; not with `remove` |
| `remove` | True removes the availability target; not with `sla` |
| `services`, filters | See [selection](#selection) |

## Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `sla`, `removed` | The target set, rounded to three decimals, or true when removed |
| `node`, `on_nodes_selected`, `on_nodes_filter`, `filter_tag`, `filterset`, `filter_ruleset`, `filter_moduleset` | The filters of the selection, resolved |
| `selected` | Services selected |
| `changed`, `unchanged`, `refused` | Services changed, already as asked, refused by the collector or not attempted |
| `services[]` | `svc_id`, `svcname`, `cluster_id`, `outcome` (`set`, `removed`, `unchanged`, `refused`, `not_attempted`), `reason` of a refusal, `sla`, the target the collector holds after the change, and `availability_percent`, the availability of its last 30 days, sorted by service name |

The availability is the one the collector stored at its last computation:
compared with `sla`, it tells at once the services below their new target.

---
domain: compliance
tools:
  - list_compliance_status
  - list_compliance_runs
  - count_compliance_runs
stability: experimental
---

# Compliance Tools

This document describes the tools that report the compliance runs the OpenSVC
collector holds: the last run of each compliance module on the nodes and on
the service instances, and the history of the runs, with their output, which
tells why a node or an instance is not compliant, without a diagnostic
verdict.

Implementation:

- business logic: `internal/core/compliance.go`, with the shared list helpers
  of `internal/core/collector_list.go` and the node and service lookups;
- MCP definitions: `internal/tools/compliance.go`.

The administration of the compliance, modulesets, rulesets, variables and
their publications, is not exposed. Visibility is that of the collector account
of the caller.

## Tools

### `list_compliance_status`

Returns one page of module statuses, the most recent run first.

#### Collector API

With `node` or `service`, the MCP first resolves it, as `get_node` and
`get_service` do. It then reads the page, the names of the nodes and services
joined:

```text
GET /api/compliance/status?props=…,nodes.nodename,services.svcname,…&orderby=-run_date,-id&filter=…&limit=…&offset=…
```

A `module` ending with a dot, such as `sec.`, selects the family of the modules
starting with it, through a `run_module:~^sec\.` filter. A `status` selects
the exit code it names, through a `run_status:eq:<code>` filter.

#### Input

| Input | Meaning |
|---|---|
| `node` | Optional exact node name or node ID; its service instances included |
| `service` | Optional exact service name or service ID |
| `module` | Optional exact module, such as `sec.ssh`, or with a trailing dot a whole family, such as `sec.` |
| `status` | Optional `ok`, `nok` or `na` |
| `since` | Optional earliest run date, in collector local time: `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or `YYYY-MM-DD HH:MM:SS` |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service the filters resolved to |
| `total` | Statuses matching the filters, as the collector counts them |
| `count`, `truncated` | Statuses in this page, and whether more remain |
| `statuses[]` | `id`, `module`, `node_id`, `nodename`, `svc_id`, `svcname`, `action`, `status`, `exit_code`, `date`, `log` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`exit_code` is the exit code of the module and `status` its name, for the
codes the collector documents: `ok` (0), `nok` (1, not compliant) and `na` (2,
not applicable). `status` is empty for any other exit code. `action` is `check`,
`fixable` or `fix`. `svc_id` and `svcname` are empty for a run on the node
itself. `log` is the output of the module, cut to 4 KiB; it is untrusted text.
The ruleset fingerprint is left out. `date` is in collector local time without
a zone.

### `list_compliance_runs`

Returns one page of the runs of the compliance modules, the most recent first:
every check, fixable check and fix the collector keeps, where
`list_compliance_status` holds the last run of each module. Use it for since
when a module fails on a node, what a fix did, and when a module last ran.

#### Collector API

```text
GET /api/compliance/logs?props=id,run_module,node_id,nodes.nodename,svc_id,services.svcname,run_action,run_status,run_date,run_log&orderby=-run_date,-id&filter=…&limit=…&offset=…
```

The node and the service are resolved as for `list_compliance_status`. The
`/api/nodes/{node_id}/compliance/logs` endpoint ignores `filter`: the MCP reads
`/api/compliance/logs` with a `node_id` filter instead.

#### Input

| Input | Meaning |
|---|---|
| `node`, `service`, `module`, `status`, `since`, `limit`, `cursor` | As for `list_compliance_status` |
| `action` | Optional action: `check`, `fixable` or `fix` |
| `until` | Optional latest run date, in collector local time; a day alone is taken to its end, `23:59:59` |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service the filters resolved to |
| `total` | Runs matching the filters, as the collector counts them |
| `count`, `truncated` | Runs in this page, and whether more remain |
| `runs[]` | `id`, `module`, `node_id`, `nodename`, `svc_id`, `svcname`, `action`, `status`, `exit_code`, `date`, `log`, as in `list_compliance_status` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

### `count_compliance_runs`

Counts the runs matching the filters of `list_compliance_runs`, as the
collector computes it, and with `group_by` breaks them down by one property,
the most frequent values first. Use it for how many checks failed or fixes ran
over a period, and which modules or nodes have the most.

#### Collector API

```text
GET /api/compliance/logs?props=id&filter=…&limit=1
GET /api/compliance/logs?stats=true&props=<grouping prop>&filter=…&limit=…
```

The groupings read `run_module`, `nodes.nodename`, `services.svcname`,
`run_action` and `run_status`; the exit codes 0, 1 and 2 are named `ok`, `nok`
and `na`, another code is given as is. Runs on a node itself have no service:
their service group is the empty value.

#### Input

| Input | Meaning |
|---|---|
| `node`, `service`, `module`, `action`, `status`, `since`, `until` | As for `list_compliance_runs` |
| `group_by` | Optional grouping: `module`, `node`, `service`, `action` or `status` |
| `limit` | Optional number of values returned with `group_by`, 1 to 100, default 20 |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service the filters resolved to |
| `total` | Runs matching the filters |
| `group_by` | The grouping asked |
| `groups[]` | With `group_by`: `value`, empty for the runs without one, and `count`, the most frequent first |
| `distinct` | With `group_by`: distinct values among the matching runs |
| `other`, `truncated` | With `group_by`: matching runs whose value was left out by `limit`, and whether there are any |

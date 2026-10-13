---
domain: compliance
tools:
  - list_compliance_status
stability: experimental
---

# Compliance Tools

This document describes the tool that reports the compliance status the
OpenSVC collector holds: the last run of each compliance module on the nodes
and on the service instances, with its output, which tells why a node or an
instance is not compliant, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/compliance.go`, with the shared list helpers
  of `internal/core/collector_list.go` and the node and service lookups;
- MCP definitions: `internal/tools/compliance.go`.

The administration of the compliance, modulesets, rulesets, variables and
their publications, is not exposed. Visibility is that of the collector account
of the MCP.

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

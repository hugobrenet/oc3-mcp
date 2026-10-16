---
domain: filterset
tools:
  - list_filtersets
  - get_filterset
stability: experimental
---

# Filterset Tools

This document describes the tools that report the filtersets of the OpenSVC
collector. A filterset selects nodes and services by criteria on their
properties, such as `nodes.loc_city = Paris`, combined with `AND`, `OR` and
`NOT` and possibly including other filtersets. Contextual compliance rulesets,
check thresholds, statistics comparisons, sysreport grants and the session
filters of the users rely on them.

Implementation:

- business logic: `internal/core/filterset.go`, with the shared list helpers
  of `internal/core/collector_list.go`;
- MCP definitions: `internal/tools/filterset.go`.

## Tools

### `list_filtersets`

Returns one page of the filtersets, sorted by name.

#### Collector API

```text
GET /api/filtersets?props=id,fset_name,fset_author,fset_updated&orderby=fset_name,id&filter=fset_name:~<name>&limit=…&offset=…
```

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the filterset name contains, case-insensitive |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Filtersets matching, as the collector counts them |
| `count`, `truncated` | Filtersets in this page, and whether more remain |
| `filtersets[]` | `id`, `name`, `author`, `updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

### `get_filterset`

Returns one filterset by its name or ID: its criteria, what it selects and
what uses it.

#### Collector API

```text
GET /api/filtersets/<name or id>?props=id,fset_name,fset_author,fset_updated
GET /api/filtersets/<id>/export
GET /api/filtersets/<id>/nodes?props=nodename&orderby=nodename,node_id&limit=50
GET /api/filtersets/<id>/services?props=svcname&orderby=svcname,svc_id&limit=50
GET /api/filtersets/<id>/usage
```

The export is the read giving the whole definition: the criteria in their
order, a filter or an included filterset each, and the definitions of the
included filtersets, walked each once at any depth. `/filtersets/<id>/filters`
lists the filters only, not the included filtersets. A filterset without
criterion selects nothing: the MCP does not read its members, which on dev2
the collector answers with a 500, `cannot resolve filterset`. The usage names
the users whose session filter the filterset is, with their email address,
whatever the caller may see of them: the MCP gives their number only.

The collector does not apply the logic of an included filterset: when it
resolves the members, it chains the criteria of the included filterset as if
written in place, without the `NOT` of the inclusion and without grouping
them. On dev2, `demo`, defined as `nodes.loc_city = Paris AND NOT
big-memory`, selects the 23 nodes of Paris with big memory, not the 66
without. The criteria the tool gives are the definition; the members are what
the collector computes, as are the contextual rulesets, thresholds and session
filters relying on the filterset. The description tells the model so.

#### Input

| Input | Meaning |
|---|---|
| `filterset` | One exact filterset name, such as `prd-linux`, or filterset ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `filterset` | `id`, `name`, `author`, `updated` |
| `criteria[]` | In their order: `logic` (`AND`, `OR`, `AND NOT`, `OR NOT`), and `filter`, as `table.field operator value`, or `filterset`, an included one |
| `included_filtersets[]` | The filtersets the criteria include, at any depth: `name`, `criteria` |
| `nodes_count`, `nodes` | Nodes it selects, as the collector computes it, among those the caller sees, at most 50 named |
| `services_count`, `services` | Services it selects, at most 50 named: a criterion on the nodes selects the services they run |
| `rulesets` | The contextual rulesets applying to what it selects |
| `included_by` | The filtersets including it |
| `check_thresholds`, `statistics_comparisons`, `sysreport_grants` | The number of check thresholds, statistics comparisons and sysreport grants using it |
| `session_filter_users` | The number of users whose session filter it is |

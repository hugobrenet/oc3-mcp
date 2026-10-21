---
domain: filterset
tools:
  - list_filtersets
  - get_filterset
  - create_filterset
  - rename_filterset
  - add_filterset_criterion
  - remove_filterset_criterion
  - delete_filterset
stability: experimental
---

# Filterset Tools

This document describes the tools that report, create, change and delete the
filtersets of the OpenSVC collector. A filterset selects nodes and services by criteria on their
properties, such as `nodes.loc_city = Paris`, combined with `AND`, `OR` and
`NOT` and possibly including other filtersets. Contextual compliance rulesets,
check thresholds, statistics comparisons, sysreport grants and the session
filters of the users rely on them.

Implementation:

- business logic: `internal/core/filterset.go`, with the shared list helpers
  of `internal/core/collector_list.go`;
- changes of the filtersets: `internal/core/filterset_write.go`;
- MCP definitions: `internal/tools/filterset.go` and
  `internal/tools/filterset_write.go`.

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

### How the collector evaluates the criteria

`ResolveFilterset` (`cdb/db_filtersets.go`) reads the criteria by rank, then
by filter ID, an included filterset, of filter ID 0, before the filters of
its rank, and chains them from left to right, without precedence nor
parentheses: `AND` intersects what the criteria before select with what the
criterion selects, `OR` adds it, and `NOT` negates the criterion itself. The
logic of the first criterion is ignored, its `NOT` kept. An included
filterset is read in place, its criteria with their own logic, the logic of
the inclusion ignored (oc3 todo 18): on dev2, a filterset of 3 nodes fell to
none once `OR site-paris` was added, `site-paris` being `AND
nodes.loc_city = Paris`.

A filter compares a database column of one table, `nodes`, `node_ip`,
`services`, `svcmon`, `resmon`, `apps`, `node_hba`, `diskinfo`, `svcdisks`,
`v_comp_moduleset_attachments`, `v_tags` or `packages`, with `=`, `LIKE`,
whose `%` is the wildcard the caller writes, `>`, `>=`, `<`, `<=` or `IN`,
whose value the collector splits on commas without trimming. The column is
the database name, checked against `information_schema`, not the property of
the API: `nodes.mem_bytes` holds MB. The collector stores a filter once per
definition, compared under the case-insensitive collation, and shares it
between the filtersets using it. `!=` and `NOT LIKE`, which the evaluation
reads, are refused at the creation: a negation is written `AND NOT` or `OR
NOT`.

### Changes of the filtersets

`create_filterset`, `rename_filterset`, `add_filterset_criterion`,
`remove_filterset_criterion` and `delete_filterset` are registered only when
`OPENSVC_MCP_ACTIONS=enabled`. The collector requires the CompManager or the
Manager privilege for each, and no responsibility for the filterset, and
logs each change with the email of the caller. The input schemas list the
logics, tables and operators the collector takes, as
`server/handlers/post_filters.go` lists them. The descriptions give the
evaluation, a few columns, and point to the criteria of a similar filterset,
which `get_filterset` gives: the collector refuses an unknown column with
400 `field <field> not found in table <table>`, a message the model reads.

`create_filterset` and `rename_filterset` are not destructive: a new
filterset is used by nothing, and what uses a filterset refers to its ID.
The three others are, so that the agent asks the user to confirm them: a
criterion changes what the filterset selects for the rulesets, thresholds
and grants using it, and a deletion removes them.

```text
POST   /api/filtersets {"fset_name": …, "entries": [{"f_log_op": …, "f_table": …, "f_field": …, "f_op": …, "f_value": …} | {"f_log_op": …, "filterset": …}]}
POST   /api/filtersets/<id> {"fset_name": …}                                rename
GET    /api/filtersets/<id>/filters, /export                                the criteria, before a change
GET    /api/filters?filter=f_table:eq:…&filter=f_field:eq:…&filter=f_op:eq:…&filter=f_value:eq:…
POST   /api/filters {"f_table": …, "f_field": …, "f_op": …, "f_value": …}   a filter no filterset holds yet
POST   /api/filtersets/<id>/filters/<f_id> {"f_log_op": …, "f_order": …}
POST   /api/filtersets/<id>/filtersets/<child_id> {"f_log_op": …, "f_order": …}
DELETE /api/filtersets/<id>/filters/<f_id>, /filtersets/<child_id>
DELETE /api/filtersets/<id>
```

`create_filterset` sends the criteria as the entries of one creation: the
collector checks them all before it writes, then creates the filterset,
the filters no other definition holds and their attachments in one
transaction, ranked in the order given. Criteria left empty create a
filterset without criteria, which the collector takes: it selects nothing.
A name taken is refused with 409. A criterion naming a filter and an
included filterset at once is sent with all its fields, for the collector
to decide: it refuses it today, `a filter or a filterset, not both`.
`add_filterset_criterion`, whose routes name one filter or one filterset,
attaches the included filterset of such a criterion.

`add_filterset_criterion` attaches a filter by its ID: the MCP finds the
filter of the definition, compared case-insensitively as the collector
compares it, or creates it, then attaches it. A criterion attached already
keeps its rank unless `order` is given: of the same logic and without
`order`, nothing is sent and `outcome` is `unchanged`; otherwise its logic,
or its rank, is changed. A new criterion without `order` goes last, its rank
one above the highest; the collector would give it rank 0, before the
others. The ranks are not renumbered: two criteria may share one, then read
by filter ID. The collector refuses with 409 an inclusion that would loop.

`rename_filterset` to the name the filterset has sends nothing:
`outcome` is `unchanged`.

`remove_filterset_criterion` detaches a filter, named by its definition, or
an included filterset; the filter stays, for the other filtersets. A
criterion the filterset does not hold is `unchanged`, as is one the
collector tells `already detached`. A filterset left without criteria
selects nothing; it is read back without its members, which the collector
answers with a 500.

`delete_filterset` reads the filterset first, as `get_filterset` gives it,
then deletes it: the collector removes its criteria, its inclusions in other
filtersets, the attachments of contextual rulesets, the contextual check
thresholds, the session filters of users, the statistics comparisons and the
sysreport grants using it. The result gives the filterset as it was, with
what used it. The filters stay.

The other changes read the filterset back as `get_filterset` gives it, with
the nodes and services it selects now, in `read_back`; a read back that
fails is a `warning`, the change being made.

| Input | Meaning |
|---|---|
| `name` | `create_filterset` and `rename_filterset`: the name, unique, at most 100 characters |
| `criteria[]` | `create_filterset`: the criteria, in their order |
| `filterset` | The filterset to change or delete, by exact name or ID; in a criterion of `create_filterset`, the filterset to include |
| `logic` | `AND`, `AND NOT`, `OR` or `OR NOT` |
| `table`, `field`, `op`, `value` | A filter |
| `included_filterset` | `add_filterset_criterion` and `remove_filterset_criterion`: the filterset to include or to remove, apart from `filterset`, the filterset changed |
| `order` | `add_filterset_criterion`: optional rank |

| Output field | Meaning |
|---|---|
| `outcome` | `created`, `changed`, `added`, `removed`, `unchanged` or `deleted` |
| `read_back` | The filterset read back, as `get_filterset` gives it; with `delete_filterset`, as it was |
| `warning` | The filterset not read back |

The statistics flag of a filterset, `fset_stats`, has no tool: the collector
stores it, but no part of oc3 reads it, the historical collector having
computed the daily statistics of the filtersets flagged.

The standalone filters, `/filters` and `/filters/<id>`, have no tool: a
filter is shared by the filtersets using it, and a change of it would
change them all; the criterion tools create and reuse them.

---
domain: compliance
tools:
  - list_compliance_status
  - list_compliance_runs
  - count_compliance_runs
  - list_compliance_modulesets
  - get_compliance_moduleset
  - list_compliance_rulesets
  - get_compliance_ruleset
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

The modulesets and the rulesets are read, with their modules, variables,
inclusions and attachments; changing them is not exposed. Visibility is that of the collector account
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

### `list_compliance_modulesets`

Returns one page of the compliance modulesets the caller sees, those published
to one of its groups, every moduleset for a manager, sorted by name, each with
its own modules and their `autofix` flag. A moduleset groups compliance
modules the agent checks and fixes on the nodes and service instances it is
attached to.

#### Collector API

```text
GET /api/compliance/modulesets?props=id,modset_name,modset_author,modset_updated&orderby=modset_name,id&filter=…&limit=…&offset=…
GET /api/compliance/modulesets_modules?props=id,modset_id,modset_mod_name,autofix&filter=modset_id:in:<ids of the page>
```

With `node` or `service`, the MCP first reads the IDs of the modulesets
attached to it, `GET /api/nodes/<node_id>/compliance/modulesets` or
`GET /api/services/<svc_id>/compliance/modulesets`; with `module`, those
holding it, `GET /api/compliance/modulesets_modules?filter=modset_mod_name:…`.
A service lists the modulesets attached to the service itself, not those of
its encapsulated service. The modules of a page are read in batches of 1000
up to their total.
The list is then filtered with `id:in:…`, at most 1000 IDs. The node and
service endpoints ignore `filter`.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the moduleset name contains, case-insensitive |
| `module` | Optional exact module, or with a trailing dot a whole family: the modulesets holding it |
| `node`, `service` | Optional exact node or service, by name or ID: the modulesets attached to it. Not both |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service the filters resolved to |
| `total` | Modulesets matching, as the collector counts them |
| `count`, `truncated` | Modulesets in this page, and whether more remain |
| `modulesets[]` | `id`, `name`, `author`, `updated`, `modules_count`, `modules[]` (`name`, `autofix`), at most 50, and `modules_truncated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

### `get_compliance_moduleset`

Returns one moduleset by its name or ID, with its tree and its attachments.

#### Collector API

```text
GET /api/compliance/modulesets/<name or id>?props=id,modset_name,modset_author,modset_updated
GET /api/compliance/modulesets/<id>/export
GET /api/compliance/modulesets/<id>/usage
GET /api/compliance/modulesets/<id>/nodes?props=nodename&orderby=nodename,node_id&limit=50
GET /api/compliance/modulesets/<id>/services?props=svcname&orderby=svcname,svc_id&limit=50[&slave=true]
```

The export is the only read giving the modulesets a moduleset includes and
the rulesets attached to it: the MCP walks it from the moduleset, each
included moduleset once, at any depth.

#### Input

| Input | Meaning |
|---|---|
| `moduleset` | One exact moduleset name, such as `base.linux`, or moduleset ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `moduleset` | `id`, `name`, `author`, `updated` and its own modules, as in `list_compliance_modulesets` |
| `publications`, `responsibles` | The groups it is published to, and those responsible for it |
| `included_modulesets[]` | The modulesets it includes, at any depth: `name` and `modules` |
| `rulesets[]` | The rulesets attached to it or to an included moduleset, and those they include: `name`, `type`, `filterset` of a contextual one, number of `variables`, `attached_to`, the modulesets of the tree it is attached to, and `included_by`, the rulesets including it |
| `included_by` | The modulesets including it |
| `nodes_count`, `nodes` | Nodes it is attached to directly, and their names, at most 50; `list_nodes` with `moduleset` reads them all |
| `services_count`, `services` | Services it is attached to directly, as the service itself, and their names, at most 50; `list_services` with `moduleset` reads them all |
| `encapsulated_services_count`, `encapsulated_services` | Services it is attached to directly as their encapsulated service, run in their containers, `slave=true` |

The attachments are the direct ones: a moduleset also applies where the
modulesets including it, in `included_by`, are attached.

### `list_compliance_rulesets`

Returns one page of the compliance rulesets the caller sees, those published
to one of its groups, every ruleset for a manager, sorted by name, or those of
one node or service. A ruleset holds the variables the compliance modules
read; an explicit ruleset applies where it is attached, a contextual one to
the nodes and services its filterset selects, and a ruleset may include
others.

#### Collector API

```text
GET /api/compliance/rulesets?props=id,ruleset_name,ruleset_type,ruleset_public&orderby=ruleset_name,id&filter=…&limit=…&offset=…
```

The collector has no read giving the rulesets that apply to a node. With
`node` or `service`, the MCP reads:

- the rulesets attached to it, `GET /api/nodes/<node_id>/compliance/rulesets`
  or `GET /api/services/<svc_id>/compliance/rulesets`;
- the contextual rulesets, at most 100, `GET /api/compliance/rulesets?filter=ruleset_type:eq:contextual`,
  and their filtersets, `GET /api/compliance/rulesets_variables?props=ruleset_id,fset_id&groupby=ruleset_id,fset_id`,
  one row per ruleset and filterset, the view giving one per variable of the
  ruleset and of those it includes;
- whether each filterset selects the node or the service,
  `GET /api/filtersets/<fset_id>/nodes?filter=node_id:eq:<node_id>&limit=1`, or
  `…/services?filter=svc_id:eq:<svc_id>`;

then lists those rulesets with `id:in:…`. `applies_by` tells `attached` or
`filterset`. The rulesets they include, and those of the modulesets attached
to the node or the service, apply too, and are not listed:
`get_compliance_ruleset` and `get_compliance_moduleset` give them.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the ruleset name contains, case-insensitive |
| `type` | Optional `explicit` or `contextual` |
| `node`, `service` | Optional exact node or service, by name or ID: its rulesets. Not both |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service the filters resolved to |
| `total` | Rulesets matching, as the collector counts them |
| `count`, `truncated` | Rulesets in this page, and whether more remain |
| `rulesets[]` | `id`, `name`, `type`, `public` (whether it may be attached), and with `node` or `service` `applies_by` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

### `get_compliance_ruleset`

Returns one ruleset by its name or ID, with its variables, inclusions,
filterset and usage.

#### Collector API

```text
GET /api/compliance/rulesets/<name or id>?props=id,ruleset_name,ruleset_type,ruleset_public
GET /api/compliance/rulesets/<id>/export
GET /api/filtersets/<fset_id>/nodes?props=nodename&orderby=nodename,node_id&limit=50, …/services
GET /api/compliance/rulesets/<id>/usage
GET /api/compliance/rulesets/<id>/nodes?props=nodename&orderby=nodename,node_id&limit=50
GET /api/compliance/rulesets/<id>/services?props=svcname&orderby=svcname,svc_id&limit=50[&slave=true]
```

The export gives the variables, the included rulesets, walked each once at
any depth, the filterset and the groups; the usage the rulesets including it
and the modulesets it is attached to. The nodes and services it is attached to
directly are read from the routes restricted to those the caller sees: the
usage names them all.

The variable values are configuration text: each is masked by the shared
rules of `internal/core/mask.go`, whole when the variable names a secret, the
sensitive keys and email addresses of a JSON value, or the sensitive
`key = value` lines and the email addresses of a text, then cut to 2 KiB. The
masking is partial, and the description says so.

#### Input

| Input | Meaning |
|---|---|
| `ruleset` | One exact ruleset name, such as `os.linux.base`, or ruleset ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `ruleset` | `id`, `name`, `type`, `public` |
| `filterset` | For a contextual ruleset: `name`, `nodes_count` and `nodes`, `services_count` and `services`, the nodes and services it selects, at most 50 named |
| `variables_count`, `variables[]`, `variables_truncated` | Its own variables, at most 200: `name`, `class`, `value`, `value_truncated`, `author`, `updated` |
| `included_rulesets[]` | The rulesets it includes, at any depth: `name`, `type`, `variables` |
| `publications`, `responsibles` | The groups it is published to, and those responsible for it |
| `included_by` | The rulesets including it |
| `modulesets` | The modulesets it is attached to |
| `nodes_count`, `nodes`, `services_count`, `services` | Nodes and services it is attached to directly, among those the caller sees, at most 50 named; `list_nodes` and `list_services` with `ruleset` read them all |
| `encapsulated_services_count`, `encapsulated_services` | Services it is attached to directly as their encapsulated service, `slave=true` |

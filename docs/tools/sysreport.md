---
domain: sysreport
tools:
  - list_node_sysreport_changes
  - get_node_sysreport_diff
  - list_node_sysreport_files
  - get_node_sysreport_file
stability: experimental
---

# Sysreport Tools

This document describes the tools that read the sysreport of a node: what its
OpenSVC agent reports of the node configuration. A report holds system files,
such as `/etc/fstab`, `/etc/hosts`, the sysctl, ssh, multipath, lvm or network
files, the OpenSVC configurations, and command outputs, such as `ip -br addr`,
`lsblk`, `pvs` or `uname -r`, plus the files and commands the
`sysreport.conf.d` of the node adds. The collector keeps them in one git
repository per node, one revision per report that changed something, up to one
per hour.

Implementation:

- business logic: `internal/core/node_sysreport.go`, with the shared list
  helpers of `internal/core/collector_list.go` and the node lookup;
- MCP definitions: `internal/tools/sysreport.go`.

Reading the sysreport of a node needs being responsible for it. The paths
matching a secure pattern of the collector are sensitive: their content is
given only to managers, to the responsible team of the node and to the groups
an authorization names; for the others the file is listed with `restricted`
and no content. Without any secure pattern every path is sensitive.

The contents and diffs are returned as the collector serves them, without the
masking of `internal/core/mask.go`: a sysreport holds the configuration files
of the node and of its OpenSVC objects, which may hold secrets. The tool
descriptions say so.

## Dates and revisions

A revision is the git commit ID of a report, abbreviated or not, as
`list_node_sysreport_changes` gives it. A date is `YYYY-MM-DD`, or
`YYYY-MM-DDTHH:MM[:SS]` with an optional `Z` or offset; without one it is in
the time zone of the collector. The collector hands dates to git, which reads
a day without a time as that day at the current time: the MCP completes a day
to `T00:00:00` for a lower bound and to `T23:59:59` for an upper one. Report
dates are returned in ISO 8601 with their offset, as git gives them.

## Tools

### `list_node_sysreport_changes`

Returns one page of the reports of one node that changed something, the
newest first, with the files and commands each changed.

#### Collector API

```text
GET /api/nodes/<node>?props=node_id,nodename,cluster_id&meta=false
GET /api/nodes/<node_id>/sysreport?path=…&begin=…&end=…&limit=…&offset=…
```

The endpoint ignores `filter`, `orderby`, `props` and `stats`: the order is
always the newest first.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID |
| `path` | Optional text the path of a file or the command line contains, case-insensitive: only the reports changing one, with those files only |
| `since`, `until` | Optional dates bounding the reports |
| `limit` | Optional page size, 1 to 100, default 20 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `cluster_id` |
| `total` | Reports matching, as the collector counts them |
| `count`, `truncated` | Reports in this page, and whether more remain |
| `changes[]` | `revision`, `date`, `initial` (the first report, adding every file), `files_count`, `files[]` (`path`, `kind`, `added`, `deleted`, `binary`), at most 50, and `files_truncated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

Some command outputs, such as `pvs`, `vgs` or `ip -br addr`, change at most
reports: `path` narrows the list to one file.

### `get_node_sysreport_diff`

Returns the unified diff of the change one report made, or of what changed
between two states of the sysreport of a node.

#### Collector API

```text
GET /api/nodes/<node_id>/sysreport/<revision>
GET /api/nodes/<node_id>/sysreport/timediff?begin=…&end=…
```

A state given as a date is the last report at or before it; a `since` older
than the first report compares with nothing: every file is then added. The
`timediff` endpoint ignores `path`: the MCP filters the files.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID |
| `revision` | The revision of one report: its change. Pass it or `since` |
| `since` | The state compared from, a date or a revision |
| `until` | Optional, with `since`: the state compared to, the latest report by default |
| `path` | Optional text the path or the command line contains, case-insensitive |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `cluster_id` |
| `from` | With `since`: `revision` and `date` of the state compared from, empty before the first report |
| `to` | `revision` and `date` of the report shown, or of the state compared to |
| `files_count` | Matching files that changed |
| `files[]` | At most 200: `path`, `kind`, `added`, `deleted`, `binary`, `restricted`, `diff`, `diff_truncated`, `diff_omitted` |
| `files_truncated` | Whether files were left out beyond 200 |
| `diffs_omitted` | Files whose diff was left out by the bound of the call |

Each diff is cut to 16 KiB at a line end. The diffs of a call are bounded to
64 KiB: a diff that does not fit is left out, its file listed with
`diff_omitted`, and the next smaller ones may still fit. Asking again with
`path` reads one of them.

### `list_node_sysreport_files`

Returns the files and commands one report of a node holds.

#### Collector API

Without `at`, or with a date, the MCP first reads the revision of the latest
report, or of the last one at or before the date:

```text
GET /api/nodes/<node_id>/sysreport?limit=1[&end=…]
GET /api/nodes/<node_id>/sysreport/<revision>/tree
```

The `tree` endpoint ignores `path`: the MCP filters the files.

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID |
| `at` | Optional revision, or date for the last report at or before it; the latest report by default |
| `path` | Optional text the path or the command line contains, case-insensitive |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `cluster_id` |
| `revision` | `revision` and `date` of the report read; `date` is empty when `at` is a revision |
| `total`, `count`, `truncated` | Matching files, files returned, at most 200, and whether some were left out |
| `files[]` | `path`, `kind`, `size` in bytes, `restricted` |

### `get_node_sysreport_file`

Returns the content of one file, or the output of one command, of one report
of a node.

#### Collector API

The MCP reads the files of the report, as `list_node_sysreport_files` does,
finds the exact path, and reads its content:

```text
GET /api/nodes/<node_id>/sysreport/<revision>/tree/<object_id>
```

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID |
| `path` | The exact path of the file or the exact command line, as the other sysreport tools give them |
| `at` | Optional revision or date, as for `list_node_sysreport_files` |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | `node_id`, `nodename`, `cluster_id` |
| `revision` | The report read |
| `path`, `kind`, `size` | The file or command, and the size of its content in bytes |
| `content` | The content, cut to 32 KiB; absent for a binary content |
| `truncated`, `binary` | Whether the content was cut, by the collector at 1 MiB or to 32 KiB, and whether it is binary |

An unknown path is refused with up to 5 paths holding its last element, or
that element without its extension. A `restricted` file is refused.

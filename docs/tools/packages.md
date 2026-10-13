---
domain: package
tools:
  - list_node_packages
  - list_package_differences
stability: experimental
---

# Package Tools

This document describes the tools that report the packages the OpenSVC agents
report installed on the nodes: those of one node, and the differences between
several nodes, the detail behind the `package differences in cluster` alert
the collector raises for each service whose nodes do not have the same
packages. They report facts, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/package.go`, with the shared list helpers of
  `internal/core/collector_list.go` and the node and service lookups;
- MCP definitions: `internal/tools/package.go`.

Visibility is that of the collector account of the caller: only the nodes and
services of the apps its groups are responsible for are compared, unless it is
a manager.

## Tools

### `list_node_packages`

Returns one page of the packages of one node, sorted by name then
architecture. Use it to tell which version of a package a node runs.

#### Collector API

The MCP first resolves the node, as `get_node` does, then reads:

```text
GET /api/packages?props=…&orderby=pkg_name,pkg_arch,id&filter=node_id:eq:<node_id>&filter=…&limit=…&offset=…
```

#### Input

| Input | Meaning |
|---|---|
| `node` | One exact node name or node ID. A name several nodes hold is refused with their node IDs |
| `name` | Optional text the package name contains, case-insensitive |
| `limit` | Optional page size, 1 to 500, default 100 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node` | The node whose packages are listed |
| `total` | Packages matching the filters, as the collector counts them |
| `count`, `truncated` | Packages in this page, and whether more remain |
| `packages[]` | `name`, `version`, `arch`, `type`, `signed_by`, `installed_at`, `last_reported_at` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`signed_by` is the provider of the key that signed the package, when the
collector knows it. `installed_at` is as the agent reported it, and
`last_reported_at` as the collector gives it.

### `list_package_differences`

Returns the packages that differ between the compared nodes, grouped by
package. Use it to see why the nodes of a service or a cluster differ, or to
compare two nodes before moving a service between them.

#### Collector API

The MCP first resolves the nodes and the service, as `get_node` and
`get_service` do, then reads:

```text
GET /api/packages/diff?node_ids=<node_id>,…&svc_ids=<svc_id>[&encap=true]
```

The collector compares the given nodes plus the nodes running the service, or
with `encap` the encapsulated nodes of the service: the containers or virtual
machines named after its instances. It returns one row per node having a
package version that not every compared node has. The route has no filter and
no pagination: the MCP filters by name and groups the rows.

#### Input

| Input | Meaning |
|---|---|
| `nodes` | Optional exact node names or node IDs, at most 50. A name several nodes hold is refused with their node IDs |
| `service` | Optional exact service name or service ID whose nodes to compare, with the given nodes |
| `encap` | Optional; with `service`, compares its encapsulated nodes instead of its hosts |
| `name` | Optional text the package name contains, case-insensitive |

Pass at least two nodes, or a service. With fewer than two nodes to compare,
the collector refuses the request, or with `encap` returns no difference.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `encap` | The service whose nodes are compared, and whether its encapsulated nodes are |
| `compared_nodes` | The nodes the collector compared |
| `total` | Packages that differ, after the name filter |
| `version_mismatches` | Packages every compared node has, but not in the same version everywhere |
| `missing` | Packages some compared nodes do not have |
| `count`, `truncated` | Packages returned, and whether some were left out after 500 |
| `packages[]` | `name`, `arch`, `type`, `kind` (`version_mismatch` or `missing`), `versions[]` with their `version` and `nodes`, and `missing_on` |

Version mismatches come first, then the packages by name, architecture and
type. A package installed in the same version on every compared node is not
listed.

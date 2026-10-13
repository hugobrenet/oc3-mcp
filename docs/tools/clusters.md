---
domain: cluster
tools:
  - list_clusters
stability: experimental
---

# Cluster Tools

This document describes the tool that reports the OpenSVC clusters the
collector knows. It names the `cluster_id` the other tools return.

Implementation:

- business logic: `internal/core/cluster.go`;
- MCP definitions: `internal/tools/cluster.go`.

The collector knows a cluster from the status its daemon sends. A `cluster_id`
that a node or service carries but that this tool does not list is a cluster
whose daemon status the collector never received: its nodes and services are
known, its name is not. Visibility is that of the collector account of the caller.

## Tools

### `list_clusters`

Returns one page of clusters, sorted by name then cluster ID. Use it to name a
cluster, or to find the cluster of a name the operator gives.

#### Collector API

```text
GET /api/clusters?props=…&orderby=cluster_name,cluster_id&filter=…&limit=…&offset=…
```

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the cluster name contains, case-insensitive |
| `cluster_id` | Optional exact cluster ID |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Clusters matching the filters, as the collector counts them |
| `count`, `truncated` | Clusters in this page, and whether more remain |
| `clusters[]` | `cluster_id`, `name`, `nodes`, `node_count`, `service_count`, `agent_versions`, `quorum`, `frozen`, `updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`nodes` are the nodes of the cluster as its daemon last reported them.
`updated` is when the collector last received the cluster status, as the
collector gives it.

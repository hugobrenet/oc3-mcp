---
domain: tag
tools:
  - list_tags
stability: experimental
---

# Tag Tools

This document describes the tool that reports the tags of the OpenSVC
collector: the labels its users attach to nodes and services, with names and
data each organization chooses.

Implementation:

- business logic: `internal/core/tag.go`, with the shared list helpers of
  `internal/core/collector_list.go` and the node and service lookups;
- MCP definitions: `internal/tools/tag.go`.

Tags are a relation, not a column of the node or the service: a node may hold
several, each with its own attach data. `get_node` and `get_service` therefore
leave them out and point to `list_tags`. The nodes and services holding a tag
are listed and counted by `list_nodes`, `count_nodes`, `list_services` and
`count_services` with their `tag` filter.

## Tools

### `list_tags`

Returns one page of the tags of the collector, or of the tags of one node or
one service, sorted by name.

#### Collector API

With `node` or `service`, the MCP first resolves it, as `get_node` or
`get_service` does, then reads:

```text
GET /api/nodes/<node_id>/tags?props=…,tag_attach_data&orderby=tag_name,tag_id&limit=…&offset=…
GET /api/services/<svc_id>/tags?props=…,tag_attach_data&orderby=tag_name,tag_id&limit=…&offset=…
```

Without them, it reads the tags of the collector:

```text
GET /api/tags?props=tag_id,tag_name,tag_created,tag_exclude,tag_data&orderby=tag_name,tag_id&filter=tag_name:~<name>&limit=…&offset=…
```

#### Input

| Input | Meaning |
|---|---|
| `node` | Optional exact node name or node ID whose tags to list |
| `service` | Optional exact service name or service ID whose tags to list |
| `name` | Optional text the tag name contains, case-insensitive; without `node` or `service` only, since the collector does not filter the tags of a node or a service |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

Pass `node` or `service`, not both.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service whose tags are listed |
| `total` | Tags matching, as the collector counts them |
| `count`, `truncated` | Tags in this page, and whether more remain |
| `tags[]` | `tag_id`, `name`, `created`, `exclude`, `data`, `attach_data`, `data_truncated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`exclude` is a regular expression of the tag names a node or service holding
the tag cannot hold. `data` is the data of the tag and `attach_data` its data
on the node or service: free text, often JSON, that the collector gives as a
JSON value or as a string. The MCP returns both as text, cut to 1 KiB.
`created` is as the collector gives it, which differs between the routes.

The nodes or services holding a tag are not listed: the collector routes
`/tags/<tag_id>/nodes` and `/tags/<tag_id>/services` take neither filters nor
counts yet. A `tag` filter of `list_nodes`, `count_nodes`, `list_services` and
`count_services` waits for them.

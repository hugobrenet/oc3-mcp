---
domain: tag
tools:
  - create_tag
  - delete_tag
  - list_tags
  - update_tag
stability: experimental
---

# Tag Tools

This document describes the tools that report, create, change and delete
the tags of the OpenSVC collector: the labels its users attach to nodes and
services, with names and data each organization chooses.

Implementation:

- business logic: `internal/core/tag.go`, with the shared list helpers of
  `internal/core/collector_list.go` and the node and service lookups;
- changes of the tags: `internal/core/tag_write.go`;
- MCP definitions: `internal/tools/tag.go` and `internal/tools/tag_write.go`.

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

### `create_tag`, `update_tag` and `delete_tag`

Create, change and delete tags. Registered only when
`OPENSVC_MCP_ACTIONS=enabled`. `create_tag` is not destructive, the tag
holding nothing until attached; `update_tag` and `delete_tag` are, so that
the agent asks the user to confirm them: a new name or exclusion pattern
reaches the filters, filtersets and contextual rulesets relying on the tag,
and a deletion detaches it from every node and service.

```text
POST   /api/tags          {"tag_name": …, "tag_exclude": …, "tag_data": …}   create
GET    /api/tags/<tag_id>?props=…                                          read before a change
POST   /api/tags/<tag_id> {the fields given}                                change
DELETE /api/tags/<tag_id>                                                   delete
```

The collector requires the TagManager or the Manager privilege for the
three, and logs each with the email of the caller. It refuses with 409 a
name another tag holds, comparing them case-insensitively. At the creation
it checks neither the exclusion pattern nor the data; at a change, it
trims the name, refuses an empty one or one over 128 characters, and an
exclusion pattern over 128 characters or that the database does not read
as a regular expression. The data is stored as given, whatever its format.
The MCP sends the fields as given, and an empty `exclude` or `data` clears
it.

The creation and the change answer with the tag, as `list_tags` gives it:
the MCP returns it, and `outcome` is `unchanged` when the change leaves it
as it was read before, a change of no field included, which the collector
takes. The deletion answers with the numbers of the node and service
attachments it deleted, in its message, which the MCP reads as
`server/handlers/delete_tag.go` of oc3 words it; a tag the collector found
no more is `unchanged`, with `warning`.

| Input | Meaning |
|---|---|
| `name` | `create_tag`: the name; `update_tag`: optional new name |
| `exclude` | Optional regular expression of the tag names a node or service holding this tag cannot hold; for `update_tag`, empty clears it |
| `data` | Optional data of the tag, often JSON text; for `update_tag`, empty clears it |
| `tag` | `update_tag` and `delete_tag`: one exact tag name, case-insensitive, or tag ID |

| Output field | Meaning |
|---|---|
| `outcome` | `created`, `changed`, `unchanged` or `deleted` |
| `tag` | The tag after the change, as `list_tags` gives it; with `delete_tag`, the tag deleted |
| `node_attachments_deleted`, `service_attachments_deleted` | `delete_tag`: the nodes and services the tag was detached from |
| `warning` | `delete_tag`: the collector told the tag not deleted |

---
domain: find
tools:
  - find_objects
stability: experimental
---

# Find Tools

This document describes the tool that finds the objects of the OpenSVC
collector by a part of their name, when the exact name or the kind of an
object is unknown. It is the entry point before the tools that take an exact
name, such as `get_node`, `get_service` or `get_app`.

Implementation:

- business logic: `internal/core/find.go`;
- MCP definitions: `internal/tools/find.go`.

Visibility is that of the collector account of the caller: each kind is read
through its own collector list and access control.

## Tools

### `find_objects`

Returns, for each searched kind, the objects whose name, ID or context
contains the text, case-insensitively: those whose name or ID contains it
first, then those whose context only does.

#### Collector API

```text
GET /api/search?q=<text>&kinds=node,service,instance,app,network,disk,tag&limit=<limit>
```

The collector search covers more kinds. The MCP asks for those another tool
details, and leaves out the users, whose names and emails are personal data,
the groups, and the collector administration objects: requests, forms,
modulesets, rulesets and filtersets.

#### Input

| Input | Meaning |
|---|---|
| `text` | The text to search, 2 to 100 characters, case-insensitive |
| `kinds` | Optional kinds to search, all by default: `node`, `service`, `instance`, `app`, `network`, `disk`, `tag` |
| `limit` | Optional number of objects returned per kind, 1 to 20, default 5 |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `text` | The searched text |
| `groups[]` | One group per searched kind: `kind`, `items[]`, `more`, and `error` when the kind could not be searched |
| `items[]` | `kind`, `id`, `name`, `context`, `matched_by`, `next_tool` |

The collector returns each kind in its own shape. The MCP gives every object
an `id` and a `name`, an instance being `<service>@<node>`, with its container
when encapsulated, and a `context` of the props describing it:

| Kind | Name or ID matched | Context | `next_tool` |
|---|---|---|---|
| `node` | `nodename`, `node_id`, `fqdn` | `app`, `node_env`, `os_name`, `cluster_id` | `get_node` |
| `service` | `svcname`, `svc_id` | `svc_app`, `svc_env`, `svc_topology`, `svc_availstatus`, `cluster_id` | `get_service` |
| `instance` | service name, container | node name, `mon_availstatus` | `list_instances` |
| `app` | `app`, `description` | `app_domain`, `description` | `get_app` |
| `network` | `addr`, `mac` | `nodename`, `intf`, `mask`, `net_name` | `list_ips` |
| `disk` | `disk_id`, `disk_name` | `nodename`, `svcname`, `disk_arrayid`, `disk_size` | `list_node_disks` |
| `tag` | `tag_name` | `tag_exclude` | `list_tags` |

The collector does not tell why an object matches. The MCP sets `matched_by`
to `name` when the text is in one of the props it matches by name or ID, and
to `context` otherwise: searching `dev2n1` returns the node, then its
instances, addresses and disks, which only match by their node. A disk found
by its device ID, which the search does not return, is marked `context`.

`more` tells that other objects of the kind match beyond `limit`. A text of
two letters, such as `de`, fills every kind: search a longer part of the
name.

#### Lookups

When `get_node`, `get_service` or `get_app` find no item of the given name, or
the collector answers 404, their error ends with `use find_objects to search
the names containing it`.

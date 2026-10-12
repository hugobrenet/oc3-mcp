---
domain: event
tools:
  - list_collector_events
stability: experimental
---

# Collector Event Tools

This document describes the tool that reports the events of the OpenSVC
collector log. It tells whether a status comes from the nodes or from the
collector itself, and who changed what in the collector, and when, without a
diagnostic verdict.

Implementation:

- business logic: `internal/core/collector_event.go`, with the shared list
  helpers of `internal/core/collector_list.go` and the node and service
  lookups;
- MCP definitions: `internal/tools/collector_event.go`.

The log holds two kinds of events:

- what the collector does itself, logged by `scheduler`: for example, when it
  no longer receives a fresh status of a service or a resource, it flags it
  `undef` (`service.status`, `resource.status`). A status history showing
  `undef` periods on every resource at once usually comes from these flags,
  not from the nodes;
- what users do in the collector, logged with their account: running an action
  on a service or a node (`service.action`, `node.action`), changing a node or
  a service (`node.change`, `service.change`), justifying an unavailability
  (`availability.ack`), and the administration of users, groups, compliance,
  filters and forms.

The dashboard sign-ins and sign-outs (`user.login`, `user.logout`) are always
left out: they are a history of the presence of people, with no bearing on the
nodes and services. Visibility is that of the collector account of the MCP.

## Tools

### `list_collector_events`

Returns one page of events, the most recent first.

#### Collector API

With `node` or `service`, the MCP first resolves it, as `get_node` and
`get_service` do. It then reads the page, and names the nodes and services of
its events in one read each:

```text
GET /api/logs?props=…&orderby=-log_date,-id&filter=log_action:!in:user.login,user.logout&filter=…&limit=…&offset=…
GET /api/nodes?props=node_id,nodename&filter=node_id:in:<node_id>,…
GET /api/services?props=svc_id,svcname&filter=svc_id:in:<svc_id>,…
```

An `action` ending with a dot, such as `service.`, selects the family of the
actions starting with it, through a `log_action:~^service\.` filter.

#### Input

| Input | Meaning |
|---|---|
| `node` | Optional exact node name or node ID |
| `service` | Optional exact service name or service ID |
| `action` | Optional exact action, such as `service.action` or `availability.ack`, or with a trailing dot a whole family, such as `service.` or `node.` |
| `level` | Optional `info`, `warning` or `error` |
| `user` | Optional exact user: a user account, or `scheduler` for the collector itself |
| `since` | Optional earliest date, in collector local time: `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or `YYYY-MM-DD HH:MM:SS` |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `node`, `service` | The node or service the filters resolved to |
| `total` | Events matching the filters, as the collector counts them |
| `count`, `truncated` | Events in this page, and whether more remain |
| `events[]` | `id`, `date`, `action`, `level`, `user`, `impersonator`, `message`, `data`, `node_id`, `nodename`, `svc_id`, `svcname` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`message` is rendered by the MCP from the template and the data of the event,
as the collector renders its log, and cut to 4 KiB; `data` holds the values.
Both are untrusted text. `impersonator` is set when a user acted as another
one. `nodename` and `svcname` are empty for a node or service the collector no
longer knows. The internal notification fields of the log are left out.
`date` is in collector local time without a zone.

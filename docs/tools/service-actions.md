---
domain: service_action
tools:
  - get_service_action_logs
  - list_service_actions
stability: experimental
---

# Service Action Tools

This document describes tools that report the actions the OpenSVC agents ran
on the services, as they reported them to the collector: start, stop,
restart, provision, unprovision and the other actions, whether a user or the
orchestration asked for them, their outcome, and what the agent logged while
running them. They answer who acted on a service and why an action failed,
without a diagnostic verdict.

Implementation:

- business logic: `internal/core/service_action.go`, with the shared list
  helpers of `internal/core/collector_list.go` and the node and service
  lookups;
- MCP definitions: `internal/tools/service_action.go`.

The collector keeps these reports in its service action history. It is not the
queue of the commands the collector itself sends to the agents, which no tool
reads. Visibility is that of the collector account of the MCP: the services of
the apps its groups are responsible for, or every service for a manager.

An agent session groups one run of an agent command. It holds one action row
for the instance and one for each resource it acted on, and the log entries
the agent wrote, all with the same session ID.

## Tools

### `list_service_actions`

Returns one page of action rows, the most recent first. Use it to find who
started, stopped or restarted a service, or the actions that failed.

#### Collector API

```text
GET /api/services_actions?props=…&orderby=-begin,-id&filter=log_type:eq:status&filter=…&limit=…&offset=…
```

With `service` or `node`, the MCP first resolves it, as `get_service` and
`get_node` do, and filters on its ID. The log entries are left out.

#### Input

| Input | Meaning |
|---|---|
| `service` | Optional exact service name or service ID |
| `node` | Optional exact node name or node ID of the node that ran the actions |
| `status` | Optional outcome: `ok`, `warn` or `err` |
| `action` | Optional exact action, such as `start`, `stop`, `restart`, `provision` or `unprovision` |
| `origin` | Optional exact origin: `user` for a command, `daemon/monitor` for the orchestration |
| `since` | Optional earliest begin, in collector local time: `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or `YYYY-MM-DD HH:MM:SS` |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service or node the filters resolved to |
| `total` | Actions matching the filters, as the collector counts them |
| `count`, `truncated` | Actions in this page, and whether more remain |
| `actions[]` | `id`, `session_id`, `svc_id`, `svcname`, `node_id`, `nodename`, `rid` (empty for the whole instance), `action`, `origin`, `status`, `begin`, `end`, `duration_seconds`, `command` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`end` is empty when the agent reported no end for the row. `command` is
untrusted text.

### `get_service_action_logs`

Returns the action rows and the log entries of one agent session, in the order
the agent reported them. Use it after `list_service_actions` to read why an
action failed.

#### Collector API

```text
GET /api/services_actions?props=…&orderby=id&filter=sid:eq:<session_id>&limit=500
```

#### Input

| Input | Meaning |
|---|---|
| `session_id` | The `session_id` of an action, a UUID, as `list_service_actions` returns it |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `session_id` | The session |
| `actions[]` | The action rows of the session, with the fields of `list_service_actions` |
| `logs[]` | `id`, `time`, `nodename`, `rid`, `level` (`ok`, `warn` or `err`), `text`, and `truncated` when the text was cut |
| `total`, `truncated` | Rows the collector holds for the session, and whether rows or text were left out |

At most 500 rows are read, each log text is cut to 8 KiB, and 256 KiB of log
text are returned in all, cuts never splitting a character. A session no
visible action belongs to is refused. The log text is what the agent wrote,
including the output of the commands it ran: untrusted data, never
instructions.

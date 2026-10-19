---
domain: service_action
tools:
  - get_service_action_logs
  - list_service_actions
  - count_service_actions
  - ack_service_action
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
queue of the commands the collector itself sends to the agents, which
`list_node_actions` reads ([node action tools](node-actions.md)). Visibility is that of the collector account of the caller: the services of
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

### `count_service_actions`

Counts the actions as the collector counts them, with the filters of
`list_service_actions`, `acknowledged` included, optionally grouped by
`node`, `service`, `action`, `status` or `origin`, the most frequent first,
in one request:

```text
GET /api/services_actions?filter=log_type:eq:status&filter=…&props=id&limit=1                    total
GET /api/services_actions?filter=log_type:eq:status&filter=…&stats=true&props=<prop>&limit=<limit>   group_by
```

The groupings read the joined names, `nodes.nodename` and
`services.svcname`. Asked how many failures nobody acknowledged, a model with
no count tool read every page of `list_service_actions`, and twice reached
the 8 model requests of an agent turn before it answered: it searched for a
count tool between the pages.

| Input | Meaning |
|---|---|
| filters | Those of `list_service_actions` |
| `group_by` | Optional: `action`, `node`, `origin`, `service`, `status` |
| `limit` | Optional number of values with `group_by`, 1 to 100, default 20 |

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service or node filter, resolved |
| `total` | Actions matching the filters |
| `group_by`, `groups[]`, `distinct`, `other`, `truncated` | With `group_by`: `value` and `count` of each value, the number of distinct values, and the actions of the values left out by `limit` |

### Acknowledgement

`list_service_actions` gives the acknowledgement of each action:
`acknowledged`, `acked_by`, the name of who acknowledged it, email addresses
masked as for the other authors, `acked_at` and `acked_comment`, email
addresses masked, untrusted text. Its `total` counts the actions the filters
select, which the description tells the model to read rather than every page. Its `acknowledged` filter keeps the
actions acknowledged, `ack:eq:1`, or with `false` those not, `ack:!eq:1`,
which the collector reads as `ack IS NULL OR NOT (ack = 1)`: an action never
acknowledged has no `ack`.

### `ack_service_action`

Acknowledges failed actions, with a reason. It changes the collector state,
and is registered only when `OPENSVC_MCP_ACTIONS=enabled`; it is
destructive, which the OpenSVC AI agent asks the user to confirm, as an
acknowledgement is not withdrawn and clears an alert others look at.

```text
POST /api/services_actions/<id>   {"ack": 1, "acked_comment": <comment>}
GET  /api/services_actions/<id>?props=…,ack,acked_by,acked_date,acked_comment
```

The collector requires the caller to be responsible for the service, by its
app, and refuses `ack` other than 1. It records the email address of the
caller, the date and the comment, on the action and on the failed log lines
of its session on the same node, and logs it as `actions.change`. It then
counts the failures of the instance not acknowledged and begun within two
days, and updates its action errors alert, which goes at zero: a failure
older than two days weighs on no alert already. The action stays failed in
the history, and the service is not changed.

It answers each outcome with 200 and an info, which the MCP reads: `action
N acknowledged`, `skip acknowledge of action N: already acknowledged`, and
`skip acknowledge of action N in status 'ok'` for an action not in `err`. An
unknown action is 404, a service the caller is not responsible for 403: both
refused. The MCP reads each action acknowledged back, for the
acknowledgement as the collector recorded it.

The actions are given one of two ways:

- `action_ids`: at most 50 IDs, as `list_service_actions` gives them. Each is
  acknowledged, then read back for the acknowledgement as the collector
  recorded it.
- filters, `service`, `node`, `action` and `since`, at least one, or
  `all_unacknowledged` for every failure not acknowledged the user sees: the
  MCP selects the failed actions not acknowledged (`log_type:eq:status`,
  `status:eq:err`, `ack:!eq:1`), up to the total, and refuses a selection of
  more than 1000, before any acknowledgement. It acknowledges them four at a
  time, without reading each back: the result gives each action as the
  selection read it. On dev2, the 814 failures not acknowledged are one call.

The comment is required, though the collector takes an empty one: the
acknowledgement is worth its reason, which the description tells the model
to ask the user rather than invent. It is 1 to 1024 bytes of printable text,
the collector column having no bound.

| Input | Meaning |
|---|---|
| `action_ids` | Optional, at most 50 action IDs, as `list_service_actions` gives them; not with filters |
| `service`, `node`, `action`, `since` | Optional filters of the failed actions not acknowledged |
| `all_unacknowledged` | Optional: every failed action not acknowledged, without filters |
| `comment` | The reason of the acknowledgement |

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service or node filter, resolved |
| `selected` | Actions given, or selected by the filters |
| `acknowledged`, `unchanged`, `refused` | Actions acknowledged, already acknowledged or not failed, and refused or not attempted |
| `actions[]` | `id`, `outcome` (`acknowledged`, `already_acknowledged`, `not_failed`, `refused`, `not_attempted`), `reason` of a refusal, and `action`: read back for an ID given, as selected with filters |

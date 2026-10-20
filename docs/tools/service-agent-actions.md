---
domain: service_agent_action
tools:
  - freeze_service
  - thaw_service
  - start_service
  - stop_service
  - restart_service
  - switch_service
  - giveback_service
  - takeover_service
  - abort_service_action
stability: experimental
---

# Service Agent Action Tools

This document describes the tools that queue agent actions on services in
the OpenSVC collector: the agents run them on their nodes, as the collector
queues them. They are registered only when `OPENSVC_MCP_ACTIONS=enabled`
([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/service_agent_action.go`, with the
  selection of `internal/core/service_selection.go`;
- MCP definitions: `internal/tools/service_agent_action.go`.

| Tool | Action | Acts on | Annotations |
|---|---|---|---|
| `freeze_service` | `freeze` | services, or instances with `local` | not destructive, idempotent |
| `thaw_service` | `thaw` | services, or instances with `local` | destructive, idempotent |
| `start_service` | `start` | services, or instances with `local` | not destructive, idempotent |
| `stop_service` | `stop` | services, or instances with `local` | destructive, idempotent |
| `restart_service` | `restart` | instances | destructive, not idempotent: each call restarts again |
| `switch_service` | `switch` | failover services | destructive, not idempotent: each call moves again |
| `giveback_service` | `giveback` | services | destructive, idempotent |
| `takeover_service` | `takeover` | the instances of one node | destructive, idempotent |
| `abort_service_action` | `abort` | services | destructive, idempotent |

A freeze and a start are not destructive: a freeze stops no instance, and is
undone by a thaw; a start stops none. A thaw is, as for the nodes: the
daemons may then start, move or restart instances at once.

## Selection

The services are given by `services`, at most 150 exact names or IDs, or
selected by the filters of `list_services`, `node`, `on_nodes`, an object of
the filters of `list_nodes`, and `instance_availability`, `instance_overall`
and `instance_frozen`; they combine, the filters narrowing the services
named. `node` alone selects every service with an instance on the node, and
`services` with `node` those services on it. A call never selects every
service: it passes `services` or at least one filter. A service named that
the filters leave out is refused with the reason, not dropped.

The guard is the confirmation: the tools are destructive but for
`freeze_service` and `start_service`, and the OpenSVC AI agent shows the
call, its arguments as the model gave them, for the user to approve before
anything is queued. A call `{"node": "dev2n1"}` reads as every service of
dev2n1, which a user asking for one service rejects.

An action on a service is queued for a node of the service the collector
saw in the last 15 minutes, the first by name: the daemons orchestrate it
for the whole service. With `local`, an action on an instance is queued for
the node of each instance selected, on `node` or on the nodes of `on_nodes`:
its agent runs it on its instance only. `restart_service` and
`takeover_service` act on instances only; `switch_service`,
`giveback_service` and `abort_service_action` on whole services only. Up to
200 services or instances are selected; a larger selection is refused
before anything is queued.

`takeover_service` takes `to`, the destination, and selects the services
to move as the other tools do: `services`, or the filters, such as `node`
for the services with an instance on a node. `{"node": "dev2n1", "to":
"dev2n2"}` moves the services of dev2n1 to dev2n2. The collector queues the
action for the destination, whose agent runs `om svc -s <service>
takeover`: om3 orchestrates `placed@` to the local node, as `om svc switch
--to` that node. A service without instance on the destination is refused,
before anything is queued, with the reason. A failover service already
running there is left as it is, `unchanged`. The agent refuses a
destination whose instance is not provisioned, which the collector does not
tell: the action is then done with exit code 1 and the reason.
`switch_service` takes no destination, which the collector does not pass:
the daemons choose the next preferred node with a provisioned instance, and
choose none for a flex service, which the MCP refuses before sending,
pointing to `takeover_service`.

Likewise, with `local` and `services`, a service named without instance on
`node` or the nodes of `on_nodes` is refused with the reason, not left out. `switch_service` takes no destination, which the collector does not
pass: the daemons choose the next preferred node with a provisioned
instance, and choose none for a flex service, which the MCP refuses before
sending, pointing to `takeover_service`.

## Agents

An om3 agent runs a queued action as `om svc -s <service> <command>`, the
command the collector built:

| Action | On a service | On an instance |
|---|---|---|
| `freeze`, `thaw`, `start`, `stop`, `restart` | `<action>` | `<action> --local` |
| `takeover`, `switch` | `<action>` | `<action>` |
| `giveback`, `abort` | `<action>` | not offered: the collector adds `--local`, which om3 refuses |

`--local` is a hidden flag of om3, the action inline on the local instance.
The collector adds it only for an agent of version 1.9 or later, as the
`version` of the node, which `pushasset` writes, not the status of the
daemon: for a node it holds no version for, it would queue the bare action,
which the daemon orchestrates on the whole service. The MCP reads the
version of the node of each instance, and refuses an action on an instance
of a node without, pointing to `queue_node_action` `pushasset`; it warns
too when the command the collector queued lacks `--local`.
The collector also takes `push resinfo`, `push config`, `syncall`,
`syncnodes` and `syncdrp`, which om3 does not know: the MCP offers none of
them.

The agent takes the action within seconds of its queueing. An orchestrated
action, such as a start, a stop, a switch, a giveback or a takeover, is done
with exit code 0 when the daemon accepted it: the orchestration goes on
after, and its outcome shows in the state of the instances, which the
collector receives 10 to 20 seconds later. The daemon refuses with exit code
1 and its reason what it cannot do, such as a switch without destination
candidate: `list_node_actions` gives the exit code and the output.

Right after a stop of the whole service, a start or a giveback may start it
on another node than the preferred one, or on two: the daemons choose the
node on the view they have of their peers, which lags behind the stop
flags they clear. The tool descriptions say so, to check the placement.

## Collector API

```text
GET    /api/services, /api/services_instances, …   the selection
PUT    /api/actions   {"svc_id": …, "action": …}                 on a service
PUT    /api/actions   {"svc_id": …, "node_id": …, "action": …}   on an instance
```

The collector requires the NodeExec privilege and the responsibility of the
service, by a group responsible for its app, or Manager, and refuses
another with 403. It refuses an action on a service with no node seen in the
last 15 minutes with 409. It logs each action, and answers with the row of
the queue: its `id`, `command`, `node_id` and `action_type`. Only an action
queued for `feed` reaches an om3 agent: one queued for `pull` or `push`
carries a warning.

## Input

| Input | Meaning |
|---|---|
| `services` | Optional exact names or IDs of the services to act on, at most 150 |
| `to` | `takeover_service` only, required: the destination node |
| filters, `node`, `on_nodes`, `instance_*` | Optional: they select the services, or narrow those named, see [selection](#selection) |
| `local` | `freeze_service`, `thaw_service`, `start_service` and `stop_service` only: true acts on the instances on `node` or the nodes of `on_nodes` |
| `node` or `on_nodes` | Required by `restart_service`, the nodes of the instances, and with `local` |

## Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `action`, `local`, `to` | The action, whether it acts on instances, and the destination of a takeover |
| `node`, `on_nodes_selected`, `on_nodes_filter`, `filter_tag`, `filterset`, `filter_ruleset`, `filter_moduleset` | The filters of the selection, resolved |
| `selected`, `queued`, `unchanged`, `refused` | Services or instances selected, actions queued, left as they were, and not queued: refused or not attempted |
| `actions[]` | `svc_id`, `svcname`, `cluster_id`, `node_id` and `nodename`, the node of the instance or the one the collector chose, `outcome` (`queued`, `unchanged`, `refused`, `not_attempted`), `reason`, `action_id`, `command`, `queued_for` and `warning`, sorted by service then node |
| `follow_up` | How to follow the actions: `list_node_actions` with the `service` of one service, or the command, then `get_service` |

The actions are followed and cancelled with the tools of the queue,
`list_node_actions` and `cancel_node_action`, which take every action of
the queue, of nodes and of services.

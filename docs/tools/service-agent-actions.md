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
  - check_service_compliance
  - fix_service_compliance
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
| `check_service_compliance` | `compliance_check` | instances | not destructive, idempotent |
| `fix_service_compliance` | `compliance_fix` | instances | destructive, idempotent |

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
| `start`, `stop`, `restart` with `resources` | not taken by the collector | `<action> --rid <resources>` |
| `takeover`, `switch` | `<action>` | `<action>` |
| `giveback`, `abort` | `<action>` | not offered: the collector adds `--local`, which om3 refuses |
| `compliance_check`, `compliance_fix` | not taken by the collector | `compliance check`, `compliance fix`, without `--local` |

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

## Resources

`start_service`, `stop_service` and `restart_service` take `resources`, the
IDs of some resources of the services, such as `fs#1`: the action acts on
those resources only, on each instance selected, which requires `node` or
`on_nodes`. The collector takes them as `rid`, comma-separated, with a
`node_id` alone, and queues `<action> --rid <resources>` instead of
`--local`, whatever the agent version: the MCP checks no version for them.
om3 runs an action with `--rid` inline on the local instance, without
orchestration: when the action is done, it is over.

A resource ID is checked against the pattern of the collector, without the
comma, which the MCP adds, at most 20 per call. The MCP reads the resources
of each instance selected, as its agent reported them, one request each: an
instance without one of the resources named is refused, with the reason;
one of them disabled, which the agent skips, is warned of. `freeze_service`
and `thaw_service` do not take `resources`. `get_service`
gives the configuration text of the service, which defines its resources,
and `list_service_resources` their status on each node: the descriptions
point to both.

A stop of some resources leaves the instance expected started: the om3
daemon disables its monitor on a stop of the whole instance only
(`daemon/imon/main_cmd.go`, `IsPartial`). It then sees a monitored resource
stopped as a failure: it restarts it, or runs the monitor action of the
service, such as a failover. `freeze_service` with `local` on the instance
first keeps the daemon from acting. The MCP neither warns of nor refuses a
stop of a monitored resource: the confirmation of the call, which its
destructive annotation asks for, is the guard.

## Compliance runs

`check_service_compliance` and `fix_service_compliance` act on instances
only: the collector takes `compliance_check` and `compliance_fix` with a
`node_id` alone. It queues `compliance check` or `compliance fix`, without
`--local` and without module or moduleset, which om3 refuses or the
collector does not pass: the agent of the node runs, on its instance only,
every module of the modulesets attached to the service. The MCP therefore
checks no agent version for them. A compliance run is not orchestrated:
when its action is done, the run is over, with exit code 0 when every module
passed for a check, and when every fix did for a fix. The modules report
their runs to the collector: `list_compliance_runs` and
`list_compliance_status` with the service give them, and the `follow_up`
points to both.

A service with no moduleset attached runs no module: the MCP reads the
modulesets attached to each service selected, one request each, and warns
of the ones without, pointing to `attach_service_moduleset`; the action is
queued all the same. The collector requires the CompExec privilege for
them, not NodeExec.

## Collector API

```text
GET    /api/services, /api/services_instances, …   the selection
PUT    /api/actions   {"svc_id": …, "action": …}                 on a service
PUT    /api/actions   {"svc_id": …, "node_id": …, "action": …}   on an instance
PUT    /api/actions   {"svc_id": …, "node_id": …, "action": …, "rid": …}   on resources
GET    /api/resources?filter=svc_id:eq:…&filter=node_id:eq:…   the resources of an instance
```

The collector requires the NodeExec privilege, CompExec for the compliance
runs, and the responsibility of the
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
| `resources` | `start_service`, `stop_service` and `restart_service` only: exact resource IDs, such as `fs#1`, at most 20; the action acts on those resources of the instances, see [resources](#resources) |
| `node` or `on_nodes` | Required by `restart_service`, the nodes of the instances, and with `local` or `resources` |

## Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `action`, `local`, `to`, `resources` | The action, whether it acts on instances, the destination of a takeover, and the resources acted on |
| `node`, `on_nodes_selected`, `on_nodes_filter`, `filter_tag`, `filterset`, `filter_ruleset`, `filter_moduleset` | The filters of the selection, resolved |
| `selected`, `queued`, `unchanged`, `refused` | Services or instances selected, actions queued, left as they were, and not queued: refused or not attempted |
| `actions[]` | `svc_id`, `svcname`, `cluster_id`, `node_id` and `nodename`, the node of the instance or the one the collector chose, `outcome` (`queued`, `unchanged`, `refused`, `not_attempted`), `reason`, `action_id`, `command`, `queued_for` and `warning`, sorted by service then node |
| `follow_up` | How to follow the actions: `list_node_actions` with the `service` of one service, or the command, then `get_service` |

The actions are followed and cancelled with the tools of the queue,
`list_node_actions` and `cancel_node_action`, which take every action of
the queue, of nodes and of services.

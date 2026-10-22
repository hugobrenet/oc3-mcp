---
domain: orchestration
tools:
  - list_orchestrations
  - get_orchestration
  - check_orchestration
  - list_orchestration_runs
  - get_orchestration_run
  - create_orchestration
  - update_orchestration
  - delete_orchestration
  - start_orchestration_run
  - stop_orchestration_run
  - resume_orchestration_run
  - continue_orchestration_run
stability: experimental
---

# Orchestration Tools

This document describes the tools that read the orchestrations of the
OpenSVC collector and their runs, check an orchestration before it runs,
create, change and delete orchestrations, and start, stop, resume and
continue their runs.

An orchestration is an ordered list of steps. Each step is one agent action,
one of those the action queue accepts (`PUT /api/actions`), such as `freeze`,
`stop`, `drain` or `pushasset`, on a set of targets of one kind: `service`, a
service as a whole; `instance`, the instance of a service on a node; `node`;
or `cluster`. A step names its targets by `pick`, their IDs (`svc_id`,
`svc_id@node_id`, `node_id`, `cluster_id`), by `filterset`, what a filterset
matches when the run starts, or by `same_as`, the targets of an earlier step
of the same kind. Its `pace` runs them `all` at once, `one` after the other,
or by `batch` of `batch_size`; its `timeout`, in seconds, fails a target whose
action has not ended by then, its action cancelled in the queue.

A run freezes the steps as its `plan` and resolves the filtersets when it
starts. Each step starts when every target of the previous one succeeded; a
target that fails, its action returning non-zero, refused by the queue,
cancelled or timed out, stops the run: `failed`. A run is `stopped` on
request. A failed or stopped run is resumed, its failed targets queued again,
or continued, the failure of the step acknowledged with a comment and the
next step started: a run whose failures were all acknowledged ends
`succeeded`, though their targets did not succeed.

The steps are numbered from 0 in the API, `same_as` and `current_step`
included, and in these tools; the collector messages number them from 1, as
`step 1: ams-dev-app02 timed out` for the step 0.

Every route requires the `Manager` privilege. The routes were read on the
collector of dev2, which serves them before the oc3 repository: its OpenAPI
1.0.3 describes them.

Implementation:

- business logic: `internal/core/orchestration.go` and
  `internal/core/orchestration_run.go`, with the shared list helpers of
  `internal/core/collector_list.go`;
- business logic of the changes: `internal/core/orchestration_write.go` and
  `internal/core/orchestration_run_write.go`;
- MCP definitions: `internal/tools/orchestration.go`,
  `internal/tools/orchestration_write.go` and
  `internal/tools/orchestration_run_write.go`.

## Tools

### `list_orchestrations`

Returns one page of the orchestrations, by name, with their number of steps
and of runs and their last run, its ID, status and start.

```text
GET /api/orchestrations?props=id,name,description,step_count,run_count,last_run_id,last_run_status,last_run_started,created,updated,updated_by&orderby=name,id&filter=…&limit=…&offset=…
```

| Input | Filter |
|---|---|
| `name` | `name:eq:<name>`, case-insensitive |
| `action` | `steps:~\b<action>\b`: the collector reads a filter on `steps` in the actions of the steps, not in their JSON, so the action is matched as a word |
| `last_run_status` | `last_run_status:eq:<status>` |

### `get_orchestration`

Reads one orchestration by its name or ID, with its steps. The listing holds
the steps, as a JSON text, with the counts and the last run, which
`GET /api/orchestrations/{id}` does not: one listing call, filtered on `id` or
`name`, reads all of it. A name matching several orchestrations is refused
with their IDs. The ids a step picks are returned 200 at most, with
`ids_truncated`.

### `check_orchestration`

Checks a saved orchestration, by its name or ID, which the tool reads first,
or a draft, by its steps, at most 50, each picking at most 200 ids.

```text
POST /api/orchestrations/check {"name", "description", "steps"}
```

The collector resolves the targets of each step as a run starting now would,
checks each one as the action queue would, and stores nothing. It answers 200
whatever it finds: a `message` for a step that is not valid, such as an action
its kind does not accept, a `same_as` naming no earlier step of that kind or a
`batch` without `batch_size`, and `ok` and `message` for each target, such as
`node … not found`. The tool returns `valid`, false when a step has a message
or a refused target, and for each step its number of targets and of those
refused, and its targets, the refused first, 100 at most. A draft is sent
with the name `draft`. It runs no agent action, and is annotated read-only.

### `list_orchestration_runs`

Returns one page of the runs, the latest first, with their status, step
reached, steps done, message, start, end, author and duration.

```text
GET /api/orchestration_runs?props=id,orchestration_id,name,status,current_step,step_count,steps_done,message,started,ended,started_by,duration&orderby=-started,-id&filter=…&limit=…&offset=…
```

| Input | Filter |
|---|---|
| `orchestration` | An ID: `orchestration_id:eq:<id>`; a name: `name:eq:<name>`, the name of the orchestration when the run started, which finds the runs of a deleted one, whose `orchestration_id` is null |
| `status` | `status:eq:<status>`: `running`, `succeeded`, `failed` or `stopped` |
| `started_by` | `started_by:~<part>` |
| `started_since`, `started_until` | `started:gte:`, `started:lt:` |

### `get_orchestration_run`

Reads one run, `GET /api/orchestration_runs/{id}`: its status and message,
its plan, its steps with the count of their targets by status and the
acknowledgement of a failure, and one page of its targets, by step then
position, 20 by default, 100 at most, filtered by `step` and
`target_status`. The detail gives neither the steps done nor the duration of
the run: the tool counts the steps succeeded or acknowledged, and computes
the duration from the start to the end, or to now.

Each target holds its status, its message, its queued action ID and command,
its dates and its exit code. The collector keeps the whole output of each
action, 46 KB for a `pushasset`: the tool gives at most 2000 bytes of stdout
and of stderr, without terminal colors, as `list_node_actions`, and only for
the targets that did not succeed, unless `all_output`.

## Action tools

Registered only when `OPENSVC_MCP_ACTIONS=enabled`. The first three write an
orchestration and run nothing; the last four drive its runs, which make the
agents act on the nodes and services.

| Tool | Collector API | Destructive |
|---|---|---:|
| `create_orchestration` | `POST /api/orchestrations {"name", "description", "steps"}` | No |
| `update_orchestration` | `PUT /api/orchestrations/{id} {"name", "description", "steps"}` | Yes |
| `delete_orchestration` | `DELETE /api/orchestrations/{id}` | Yes |
| `start_orchestration_run` | `POST /api/orchestrations/{id}/runs` | Yes |
| `stop_orchestration_run` | `POST /api/orchestration_runs/{id}/stop` | Yes |
| `resume_orchestration_run` | `POST /api/orchestration_runs/{id}/resume` | Yes |
| `continue_orchestration_run` | `POST /api/orchestration_runs/{id}/continue {"comment"}` | Yes |

The collector checks the steps as a whole at creation and change, and
refuses with 400 the first it does not accept, numbered from 1, such as
`step 1: action "explode" is not accepted on a node`, and with 409 a name
taken. It saves the targets as they are given: a `pick` of a node that does
not exist, or that no agent reaches, is saved, and refused only when a run
starts. After a creation or a change, the tool reads the orchestration back
and checks it, `POST /api/orchestrations/check`, and returns the check with
it: `valid`, the messages of the steps and the targets refused. A read back
or a check that fails is told in `warning`, the change made.

- `create_orchestration` takes the name, an optional description and the
  steps, 50 at most, each picking 200 ids at most.
- `update_orchestration` takes the orchestration, by name or ID, and the
  name, the description and the steps to set. The collector replaces the
  three together: an absent description is cleared, so the tool reads the
  orchestration first and sends the values not given as they are. Steps
  replace the whole list; an empty description clears it. `outcome` is
  `unchanged` when the values given are those held. The runs, past or
  running, keep the plan they started with.
- `delete_orchestration` reads the orchestration first and returns it. Its
  runs stay, with `orchestration_id` null, listed by
  `list_orchestration_runs` under its name; a run running goes on, which
  `warning` tells.

### Runs

The four run tools are destructive: a run makes the agents freeze, stop,
drain or reboot what its steps name, and the OpenSVC AI agent asks the user
to confirm each one. They return the run as the collector answers the
request, as `get_orchestration_run` gives it with its first 20 targets, and
`action`. The actions of the targets run in the agents later: the agent
follows the run with `get_orchestration_run`.

- `start_orchestration_run` takes the orchestration, by name or ID. The
  collector freezes the steps in the run, resolves the filtersets and queues
  the actions of the first step at once; it refuses with 400 a step that
  matches no target. The description asks the agent to read the steps and
  check the targets first, so that the confirmation names what will act.
- `stop_orchestration_run` cancels the actions still waiting in the queue,
  and runs neither the targets not started nor the next steps; an action an
  agent already runs goes on. 409 for a run not running.
- `resume_orchestration_run` queues again the actions of the targets of the
  step that did not succeed, then runs the next steps. 409 for a run neither
  failed nor stopped, or one an action of which still runs.
- `continue_orchestration_run` acknowledges the failed or stopped step with a
  comment, required, and runs the next steps; its targets do not run again.
  When the step was the last, the run ends `succeeded`, though its targets
  did not succeed, even those a stop cancelled. Same 409.

The collector of dev2 was driven through this cycle on 2026-10-22 with one
step on a node no agent serves: its action, queued, timed out and was
cancelled in the queue, a resume queued it again, a stop cancelled it, and a
continue ended the run `succeeded`.

## Output

Every result includes `provenance`. The collector gives the dates of the
orchestrations with their zone, unlike most of its tables. Names,
descriptions, messages, comments and outputs are untrusted text; the authors,
`updated_by`, `started_by` and `acked_by`, have their email addresses masked.

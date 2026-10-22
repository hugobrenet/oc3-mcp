---
domain: orchestration
tools:
  - list_orchestrations
  - get_orchestration
  - check_orchestration
  - list_orchestration_runs
  - get_orchestration_run
stability: experimental
---

# Orchestration Tools

This document describes the tools that read the orchestrations of the
OpenSVC collector and their runs, and check an orchestration before it runs.

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
`succeeded`, though their targets did not succeed. Starting, stopping,
resuming and continuing a run, and writing an orchestration, are not offered
by these tools.

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
- MCP definitions: `internal/tools/orchestration.go`.

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

## Output

Every result includes `provenance`. The collector gives the dates of the
orchestrations with their zone, unlike most of its tables. Names,
descriptions, messages, comments and outputs are untrusted text; the authors,
`updated_by`, `started_by` and `acked_by`, have their email addresses masked.

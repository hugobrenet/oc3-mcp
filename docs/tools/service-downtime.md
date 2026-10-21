---
domain: service_downtime
tools:
  - justify_service_downtime
  - unjustify_service_downtime
stability: experimental
---

# Service Downtime Tools

This document describes the tools that justify the downtime periods of a
service in the OpenSVC collector, or remove a justification. They are
registered only when `OPENSVC_MCP_ACTIONS=enabled`
([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/service_downtime.go`, with the status
  history read of `internal/core/service_status_history.go`;
- MCP definitions: `internal/tools/service_downtime.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `justify_service_downtime` | not destructive, idempotent | Responsible for the service, by its app |
| `unjustify_service_downtime` | not destructive, idempotent | Responsible for the service, by its app |

A justification is a comment on a period of the status history of a
service, the periods `get_service_status_history` gives, recorded with who
gave it and when, and whether the period still counts in the availability
rate. A period left out of the rate is not counted as downtime: the rate the
SLA of the service is compared with goes up. It is not a maintenance window:
it snoozes no alert, and covers no time to come.

Neither tool is destructive, so the agent asks no confirmation: a
justification changes the availability rate and its comment only, which
another call sets again; it replaces a justification already there, which
the result gives back in `previous`.

## Periods

The collector keeps a justification by its bounds, apart from the periods,
and takes any bounds: it leaves the downtime they cover out of the rate, but
shows the justification only on the period of the same bounds. A
justification on other bounds would change the rate, unseen in the history.
The MCP therefore names a period by its begin, exactly as
`get_service_status_history` gives it, reads the status history back to it,
and sends the bounds of that period. It refuses, before any change:

- a begin that is not `YYYY-MM-DD HH:MM:SS`, or no period begins at, or one
  more than about 363 days old: the history is read over 365 days at most,
  two of them a margin for the time zone;
- the period still going on: the last one, ending less than 15 minutes
  before the collector time, as the collector tells the current status of a
  service. Its end moves with each status the agents send, and the
  justification would no longer match it.

With `end`, the period is named by its two bounds, sent as is to the
collector, which takes any bounds, begin first: the period of the history
with these bounds when there is one, the period going on included, or the
bounds alone. The history does not show a justification whose period grew
since, on bounds no period has any more; with the bounds it was given,
`unjustify_service_downtime` removes it, the collector answering not found
when it holds none. Finding those bounds is for the collector to allow, by
a list of the justifications of a service: none exists today.

A period the service was `up` or `stdby up` is justified as the others, as
the collector takes it: the availability rate leaves the up intervals out
of the justified ones, so that the justification only adds a comment.

The last period of a service whose agents stopped reporting more than 15
minutes ago is justified. When they resume with the same status, the
collector extends that period again: the justification keeps its bounds,
no longer shows on the period, and still leaves the original period out of
the rate. This is the case of a node unreachable during an incident: justify
its downtime once the service is back, or expect it to show no more.

`unjustify_service_downtime` takes a period the service was up too, and
the period still going on: a justification given in the collector web
interface may lie on one, and removing it is right. A period without
justification is answered `not_justified`, without a request.

The history is read over the days from the begin to now, two more for the
offset of the collector local time, which has no zone.

## Collector API

```text
GET    /api/services/<service>?props=svc_id,svcname,cluster_id
GET    /api/services/<svc_id>/status_log?days=<days>
PUT    /api/services/<svc_id>/status_log/ack   {"begin": …, "end": …, "comment": …, "account": …}
DELETE /api/services/<svc_id>/status_log/ack?begin=…&end=…
```

The collector requires the caller to be responsible for the service, by a
group responsible for its app, or Manager, and refuses another with 403. It
takes the bounds as `YYYY-MM-DD HH:MM:SS`, the begin first, and a comment
not empty. A justification of the same bounds is replaced. It answers 404 a
removal of what is not justified: the MCP reads the period first, and sends
no removal for one not justified. It logs each change, and computes the
availability of the service again at once.

## Input

| Input | Meaning |
|---|---|
| `service` | One exact service name or service ID |
| `begin` | The begin of the period, as `get_service_status_history` gives it |
| `end` | Optional end of the period: with it, the period is named by both its bounds, sent as is |
| `comment` | `justify_service_downtime` only: why the service was down, as the user gives it, sent as is; the collector refuses an empty one; the MCP never makes it up |
| `counted` | `justify_service_downtime` only: true keeps the period in the availability rate, false leaves it out |

## Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service` | `svc_id`, `svcname`, `cluster_id` |
| `outcome` | `justified`, `replaced` when a justification was there, `removed`, or `not_justified` when the period had none |
| `previous` | The justification the period had before, replaced or removed |
| `period` | `begin`, `end`, `status`, and `ack`, the justification read back after the change |
| `availability` | The availability of the service after the change, over the days read, as `get_service_status_history` gives it |
| `read_back_failed` | True when the change was made but the period could not be read back: `period.ack` and `availability` are then not given |
| `warning` | Why the change does not show on the period: the read back failed, or no period has its bounds any more, the period having grown since it was read |

The change is made before it is read back: a read back that fails, or that
finds no period of the same bounds, is told in `read_back_failed` and
`warning`, and the result never shows the availability read before the
change.

A justification holds `comment`, its email addresses masked, `counted`,
`acked_by`, an email address the collector records, masked, and `acked_on`.

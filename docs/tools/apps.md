---
domain: app
tools:
  - get_app
stability: experimental
---

# App Tools

This document describes the tool that reports one app of the OpenSVC
collector: the code that groups nodes and services, and gives the groups
responsible for them.

Implementation:

- business logic: `internal/core/app.go`, with the count helper of
  `internal/core/collector_stats.go`;
- MCP definitions: `internal/tools/app.go`.

## Tools

### `get_app`

Returns one app: its description, domain and operations team, the collector
groups responsible for it, and the number of its nodes and services. Use it to
tell who is responsible for an app, or for a node or a service through their
`app` field.

#### Collector API

```text
GET /api/apps/<app>?props=id,app,description,app_domain,app_team_ops,updated
GET /api/apps/<app_id>/responsibles?props=role,description&limit=101
GET /api/nodes?filter=app:eq:<app>&props=node_id&limit=1
GET /api/services?filter=svc_app:eq:<app>&props=svc_id&limit=1
```

The collector resolves the app by its code, case-insensitively, or its ID.

#### Input

| Input | Meaning |
|---|---|
| `app` | One exact app code, such as `WEB`, or app ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `id`, `name` | The app ID and code |
| `description`, `domain`, `team_ops`, `updated` | The app record |
| `responsibles[]` | The responsible groups, sorted by role: `role`, `description` |
| `responsibles_truncated` | Whether groups were left out after 100 |
| `nodes`, `services` | The nodes and services of the app the caller sees |

The responsibles are groups, not people: the members of a group are not
returned. A group named `user_` followed by a number is the own group of one
collector user.

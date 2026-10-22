---
domain: app
tools:
  - get_app
  - create_app
  - update_app
  - delete_app
  - attach_app_groups
  - detach_app_groups
stability: experimental
---

# App Tools

This document describes the tools that report, create, change and delete
the apps of the OpenSVC collector: the code that groups nodes and services,
gives the groups responsible for them the management of them, and the groups
it is published to the sight of them.

Implementation:

- business logic: `internal/core/app.go`, with the count helper of
  `internal/core/collector_stats.go`;
- changes of the apps and their groups: `internal/core/app_write.go`;
- MCP definitions: `internal/tools/app.go` and `internal/tools/app_write.go`.

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
GET /api/apps/<app_id>/publications?props=role,description&limit=101
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
| `publications[]`, `publications_truncated` | The groups the app is published to, whose members see its nodes and services, likewise |
| `nodes`, `services` | The nodes and services of the app the caller sees |

The responsibles are groups, not people: the members of a group are not
returned. A group named `user_` followed by a number is the own group of one
collector user.

### `create_app`, `update_app` and `delete_app`

Create, change and delete app codes. Registered only when
`OPENSVC_MCP_ACTIONS=enabled`. `create_app` is not destructive, the app
holding nothing yet; `update_app` and `delete_app` are, so that the agent
asks the user to confirm them: a new code renames the app on its nodes and
services, and a deletion removes its groups.

```text
POST   /api/apps {"app": …, "description": …, "app_domain": …, "app_team_ops": …}   create
POST   /api/apps/<app_id> {the fields given}                                     change
DELETE /api/apps/<app_id>                                                        delete
GET    /api/apps/<app>, /responsibles, /publications, /api/nodes, /api/services  read before and back
```

The collector requires the AppManager or the Manager privilege for the
three, and to be responsible for the app for the change and the deletion,
and logs each with the email of the caller. A creation is refused with 409
for a code another app holds, compared case-insensitively, and with 403
past the app quota of the caller; the collector makes the default group of
the caller, their personal group `user_<id>` on dev2, responsible for the
app and publishes it to that group. It checks nothing else at the creation:
the code is neither trimmed nor bounded, the domain not bounded, the
operations team not checked (oc3 todo 20). The MCP sends the fields as
given, and reads the app back by the ID the collector answers with, so that
a code it could not address by name, such as one holding `/`, is read all
the same. A change
trims the code and refuses an empty one, one over 64 characters or held by
another app, a domain over 64 characters or out of the field constraints of
the collector, and an operations team that is not an organizational group;
a new code renames the app on its nodes and services in the same
transaction. The filtersets filtering on the old code, `nodes.app`,
`services.svc_app` or `apps.app`, keep it and no longer select them: the
description of `update_app` tells the model so. A deletion is refused with 409 while nodes or services carry
the app, whatever the caller sees of them, and removes its responsibles and
publications.

The MCP sends the fields as given, an empty one clearing it, and reads the
app back as `get_app` gives it, in `read_back`: `outcome` is `unchanged`
when a change leaves the app as it was read before. `delete_app` reads the
app first, and gives it as it was.

| Input | Meaning |
|---|---|
| `name` | `create_app`: the code; `update_app`: optional new code |
| `description`, `domain`, `team_ops` | Optional fields; for `update_app`, empty clears them |
| `app` | `update_app` and `delete_app`: one exact app code or app ID |

| Output field | Meaning |
|---|---|
| `outcome` | `created`, `changed`, `unchanged` or `deleted` |
| `read_back` | The app read back, as `get_app` gives it; with `delete_app`, as it was |
| `warning` | The app not read back, the change being made |

### `attach_app_groups` and `detach_app_groups`

Make groups responsible for an app, whose members manage its nodes and
services, or publish it to groups, whose members see them; or withdraw
them. Both are destructive, so that the agent asks the user to confirm
them: an attachment grants rights on nodes and services, a detachment
withdraws them.

```text
POST/DELETE /api/apps/<app_id>/responsibles/<group>
POST/DELETE /api/apps/<app_id>/publications/<group>
```

The MCP sends one request per group, responsibles first, at most 50 groups
in all, the group as given in the path: the collector resolves it by its ID
or its name, and the MCP does not look it up, the collector seeing groups a
caller may not list. The collector requires AppManager and to be
responsible for the app, refuses a privilege group, such as Manager, with
403, and to a caller not a Manager a group they are not a member of. It
refuses a group already attached with 409 and one not attached with 404, as
`server/handlers/post_app_responsible.go` and its siblings word them: the
MCP reads those as `already_attached` and `already_detached`, any other
refusal as `refused` with its reason.

After a detachment of responsibles, the MCP reads back the groups still
responsible, `responsibles`, and `warning` tells when none is left: only a
Manager may then change the app, or its nodes and services. It then asks
`GET /api/apps/<app_id>/am_i_responsible`, which answers `true` or 403:
`warning` tells a caller who withdrew their own group, and may no longer
attach it back.

| Input | Meaning |
|---|---|
| `app` | One exact app code or app ID |
| `responsibles`, `publications` | The groups, by exact name or ID |

| Output field | Meaning |
|---|---|
| `app` | The app: `id`, `name` |
| `changed`, `unchanged`, `refused` | The numbers of groups by outcome |
| `groups[]` | Each group: `group`, `role` (`responsible` or `publication`), `outcome`, `reason` |
| `responsibles` | `detach_app_groups` of responsibles: the groups still responsible |
| `warning` | `detach_app_groups`: no group is responsible any more, or the caller no longer is |

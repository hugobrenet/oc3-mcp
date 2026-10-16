---
domain: group
tools:
  - list_groups
  - get_group
  - list_users
stability: experimental
---

# Group and User Tools

This document describes the tools that report the groups and the users of the
OpenSVC collector: the teams responsible for apps and nodes, the groups
granting collector rights, and the people member of them.

Implementation:

- business logic: `internal/core/group.go`, with the shared list helpers of
  `internal/core/collector_list.go`;
- MCP definitions: `internal/tools/group.go`.

A collector group is one of three kinds:

- `organization`: a team of people, such as `DBA` or `Infra Unix`, which may be
  responsible for apps, and be the responsible team of nodes;
- `privilege`: a group granting collector rights, such as `Manager`,
  `NodeManager` or `CompExec`;
- `personal`: the own group of one user, named `user_` and the user ID, which
  the collector creates for each user.

Visibility is that of the collector account of the caller: a manager sees
every group and user; another user sees the groups it belongs to, and the
users sharing one of its organization groups.

The email address, phone number and instant messaging account of a user are
never read: the tools name the people.

## Tools

### `list_groups`

Returns one page of the groups, sorted by name, or of the groups of one user.

#### Collector API

```text
GET /api/groups?props=id,role,privilege,description&orderby=role,id&filter=…&limit=…&offset=…
```

With `user`, the MCP resolves the user, then reads its groups:

```text
GET /api/users/<user_id>/groups?props=…&filter=…
```

The kind is a collector filter: `privilege:!eq:T`, which keeps the groups
whose privilege is not set, and a name not matching `^user_[0-9]+$` for an
organization group, `privilege:eq:T` for a privilege
group, a name matching it for a personal group.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the group name contains, case-insensitive |
| `kind` | Optional `organization` (default), `privilege`, `personal` or `all` |
| `user` | Optional user whose groups to list: a user ID, an email address the caller knows, a username, or a first and last name. A name several users hold is refused with their IDs |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

A username or a full name is resolved with collector filters, without reading
the users: the username first, then each split of the name in a first and a
last name, at most 6 words.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `user` | With `user`, the user it resolved to: `id`, `name` |
| `total` | Groups matching, as the collector counts them |
| `count`, `truncated` | Groups in this page, and whether more remain |
| `groups[]` | `id`, `name`, `description`, `kind`, and `user_id` for a personal group |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

### `get_group`

Returns one group by its name or ID, with what it is responsible for.

#### Collector API

```text
GET /api/groups/<name or id>?props=id,role,privilege,description
GET /api/groups/<id>/apps?props=app,description&orderby=app&limit=50
GET /api/groups/<id>/users?props=id&limit=1
GET /api/groups/<id>/nodes?props=node_id&limit=1
GET /api/groups/<id>/services?props=svc_id&limit=1
```

For a personal group, the MCP also reads its user, `GET /api/users/<id>`; a
group whose user is deleted or not visible to the caller is answered without
it.

The nodes of a group are the nodes whose `team_responsible` is the group name,
as the collector Nodes page shows it, not the nodes of its apps: the count is
exact, while the `team` filter of `list_nodes` is a text the team contains, and
may also list the nodes of a team whose name holds this one, such as `DBA2` for
`DBA`. Its services are the services of its apps. The relation endpoints ignore
`filter`.

#### Input

| Input | Meaning |
|---|---|
| `group` | One exact group name, such as `DBA` or `user_12`, or group ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `group` | `id`, `name`, `description`, `kind`, `user_id` |
| `user` | For a personal group, its user: `id`, `name` |
| `apps[]`, `apps_total`, `apps_truncated` | The apps the group is responsible for, `name` and `description`, at most 50 |
| `members` | Users member of the group: list them with `list_users` and `group` |
| `nodes` | Nodes whose responsible team is the group: list them with `list_nodes` and `team` |
| `services` | Services of its apps: list them with `list_services` and `app` |

### `list_users`

Returns one page of the users, sorted by last name, then first name, or of the
members of one group.

#### Collector API

```text
GET /api/users?props=id,username,first_name,last_name&orderby=last_name,first_name,id&limit=…&offset=…
GET /api/groups/<id>/users?props=…
```

With `name`, the MCP reads the first 1000 users and keeps those whose first
name, last name, full name or username contains it: the collector filters one
column at a time, and the members of a group not at all.

#### Input

| Input | Meaning |
|---|---|
| `group` | Optional exact group name or group ID: its members |
| `name` | Optional text the first name, last name or username contains, case-insensitive |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `group` | With `group`, the group it resolved to |
| `total` | Users matching, as the collector counts them, or with `name` as the MCP counts them among those it read |
| `scan_truncated` | With `name`, whether only the first 1000 users were read |
| `count`, `truncated` | Users in this page, and whether more remain |
| `users[]` | `id`, `username`, `first_name`, `last_name` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

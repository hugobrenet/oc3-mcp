# Collector API: AI resources and skills

[Back to README](../README.md) · [Configuration](configuration.md)

The collector serves these routes from its migration `0030_ai_content`;
oc3-mcp reads them. om3-mcp is to follow.

The collector stores the MCP resources and the Agent Skills that the AI
assistant reads. An administrator creates, updates and deletes them in the
collector. Each object is marked for one or both backends, `om3` and `oc3`.

The MCP servers keep none of these objects. On each MCP request they read
the collector and give the answer back:

| MCP request | Collector route |
|---|---|
| `resources/list` | `GET /ai/resources?backend=…` |
| `resources/read` of a resource | `GET /ai/resources/{name}?backend=…` |
| `skills/list`, `skills/get` | `GET /ai/skills?backend=…` |
| `resources/read` of a `SKILL.md` | `GET /ai/skills/{name}?backend=…` |

Every request is made as the user:
- `oc3-mcp` forwards the collector token of the user, or exchanges the MCP
  token of an external client for one, as it does for its tools.
- `om3-mcp` exchanges the token of the user for a collector token (RFC 8693),
  with its exchange client. The provider of each cluster must be federated in
  that client; see the om3-mcp token exchange guide.

So the collector applies its own access rules to every read, with the
privileges and groups of the user.

## Objects

Resources and skills are two collections with the same rules: a name unique
within its collection, the backends it is for, the groups it is published to,
and a text of at most 64 KiB.

### Resource

A reference text that a model reads on demand, such as the naming conventions
of the company.

| Field | Type | Rules |
|---|---|---|
| `name` | string | required, `^[a-z0-9]+(-[a-z0-9]+)*$`, at most 64 bytes, unique among resources; it cannot be changed |
| `title` | string | optional, at most 128 bytes, one line |
| `description` | string | required, at most 1024 bytes: what the text holds and when to read it; the model reads this to decide |
| `mime_type` | string | `text/markdown` (default) or `text/plain` |
| `backends` | array of string | required, one or both of `om3`, `oc3`, no duplicate |
| `publications` | array of integer | the ids of the groups that may read it; empty means everybody; given back to an AIManager only |
| `body` | string | required, valid UTF-8, at most 65536 bytes, no NUL byte |
| `size` | integer | read only: the bytes of `body` |
| `digest` | string | read only: `sha256:<hex>` of the bytes of `body` |
| `updated` | string | read only: RFC 3339 UTC time of the last write |

### Skill

An [Agent Skill](https://agentskills.io/specification): one `SKILL.md`, a
YAML frontmatter followed by Markdown. The collector stores the whole file in
`body` and checks its frontmatter on every write:

- the frontmatter is a YAML map between two `---` lines that start the file;
- `name` is required and equals the `name` of the object;
- `description` is required, at most 1024 bytes;
- every value is a string, or a map of strings on one level (`metadata`);
- `metadata.opensvc/tools`, when present, lists at most 24 MCP tool names
  separated by spaces: the tools the skill uses.

A skill with any other YAML is refused: the AI agent would refuse it too.

| Field | Type | Rules |
|---|---|---|
| `name` | string | as for a resource, unique among skills |
| `backends` | array of string | as for a resource |
| `publications` | array of integer | as for a resource |
| `body` | string | the whole `SKILL.md`, as for a resource |
| `frontmatter` | object | read only: the frontmatter as written, its values kept as strings |
| `size`, `digest`, `updated` | | read only, as for a resource |

Only the `SKILL.md` is stored: a skill has no other files.

## Routes

### Reads (any authenticated user)

`GET /ai/resources?backend={om3|oc3}`

Lists the resources of the backend that the caller may read: their metadata
only, never the body.

```json
{
  "data": [
    {
      "name": "naming",
      "title": "Naming conventions",
      "description": "The naming rules of the services and nodes of the company. Read it before creating a service.",
      "mime_type": "text/markdown",
      "size": 4210,
      "digest": "sha256:9f2c…",
      "updated": "2026-11-04T15:20:00Z"
    }
  ]
}
```

- `backend` is optional, `om3` or `oc3`, any other value a 400: the MCP servers give it, an administration interface leaves it out to list every object.
- Sorted by `name`.
- The response has an `ETag`. A request with `If-None-Match` set to that
  value gets a 304 while the visible list is the same. The list is private to
  the caller: `Cache-Control: private, no-cache`. The ETag serves an
  administration interface polling the list: the MCP servers keep nothing,
  and the agent lists the resources at each turn.
- The `publications` are given to an AIManager only: a reader learns which
  objects it may read, not which groups may.

`GET /ai/resources/{name}?backend={om3|oc3}`

Gives one resource with its `body`. It answers 404 when the resource does not
exist, is not for that backend, or is not published to the caller. One answer
covers all three cases, so that a caller learns nothing of an object it may
not read.

`GET /ai/skills?backend={om3|oc3}` and `GET /ai/skills/{name}?backend={om3|oc3}`

Behave the same way for skills. The list gives `name`, `frontmatter`, `size`,
`digest` and `updated` of each skill. The single skill also gives its `body`.

### Writes (privilege `AIManager`, or `Manager`)

What an AIManager writes, the model reads as instructions for every user it
is published to: granting `AIManager` amounts to letting someone write the
system prompt of other users. Grant it as `Manager` is granted, not as an
editorial role.

| Route | Effect | Answer |
|---|---|---|
| `POST /ai/resources` | create; the body holds the writable fields | 201 with the object, 409 when the name exists |
| `PUT /ai/resources/{name}` | replace the writable fields; `name` stays | 200 with the object |
| `DELETE /ai/resources/{name}` | delete | 204 |
| `POST /ai/skills`, `PUT /ai/skills/{name}`, `DELETE /ai/skills/{name}` | the same for skills | the same |

- A write that breaks a rule is a 400 whose message names the field and the
  rule, such as `body: more than 65536 bytes`.
- A caller without the privilege gets a 403.
- An object and its publications are written in one transaction, with the
  line of the collector log: a read never sees an object without its
  publications, published to everybody.
- A request body is read up to 256 KiB; a larger one is a 413.
- A backend holds at most 100 objects of a kind, a replace adding a backend
  included: the collector refuses the write that would pass it, with a 400.
- A group deleted keeps its id in the publications: the object stays hidden
  from all but the AIManagers until they publish it again.
- Every write is logged in the collector log, as the other writes are.
- A write answer gives the object without its body.

## What the MCP servers do with them

| Object | URI in MCP |
|---|---|
| resource `naming` on om3-mcp | `om3://resources/naming` |
| resource `naming` on oc3-mcp | `oc3://resources/naming` |
| skill `maintenance` | `skill://maintenance/SKILL.md` |

- `resources/list` gives the resources of the collector list: `uri`, `name`,
  `title`, `description`, `mimeType`, `size`. The `SKILL.md` files are not
  listed; `skills/list` names them.
- `skills/list` and `skills/get` follow the MCP Skills extension
  (`io.modelcontextprotocol/skills`): the URI of the `SKILL.md`, its
  frontmatter, and its manifest with the `digest` and the `size` of the
  collector.
- `resources/read` gives the `body` of the collector, with its `mime_type`
  as `mimeType`, `text/markdown` or `text/plain`; another type reads as
  `text/markdown`. The `SKILL.md` of a skill is `text/markdown`.
- `resources/read` of any other URI is left to the server.
- A 404 of the collector is an MCP "resource not found" error. Any other
  failure is an MCP error that names no URL and no credential.
- Nothing is cached: each MCP request reads the collector.

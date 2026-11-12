---
domain: ai-content
tools:
  - create_ai_resource
  - create_ai_skill
  - delete_ai_resource
  - delete_ai_skill
  - get_ai_resource
  - get_ai_skill
  - list_ai_resources
  - list_ai_skills
  - update_ai_resource
  - update_ai_skill
stability: experimental
---

# AI Resource and Skill Tools

This document describes the tools that read and write the resources and the
skills of the AI assistant, which the collector stores and the MCP servers
serve to the agent (see [the collector API](../collector-ai-content.md)):

- a resource is a reference text, such as a procedure, a glossary or the
  conventions of a site, which the assistant reads when its description tells
  it a question needs it;
- a skill is a `SKILL.md`, a procedure the assistant loads to carry out a
  task, its frontmatter naming it, telling when to load it and the tools it
  uses.

Implementation:

- business logic: `internal/core/ai_content_write.go`;
- MCP definitions: `internal/tools/ai_content.go`.

The model reads what these tools write as instructions, for every user the
text is published to. The write tools therefore:

- are registered only when actions are enabled (`OPENSVC_MCP_ACTIONS`), as
  the other tools that change the collector;
- are all destructive, a creation included, so that the AI agent asks the
  user to confirm each of them;
- publish to the groups given only: a text is published to every user when
  `everybody` is true, never by default;
- change a text by exact `edits`, each replacing the only occurrence of its
  `old` text by its `new` one, which the confirmation shows, rather than by a
  whole new body;
- tell the model, in their description, to write only what the user asked,
  never text taken from a tool result, an alert, a log or a document.

The collector writes them as the caller: an AIManager or a Manager only. With
opensvc/oc3#192, its log names them as made through the assistant.

## Tools

### `list_ai_resources` and `list_ai_skills`

Return the texts of a kind the caller may read, by name, without their body:
backends, size, digest and last write; for an AIManager, the IDs of the
groups each is published to, empty for everybody, and, from a collector that
counts them, the reads of the assistants by backend.

```text
GET /api/ai/resources
GET /api/ai/skills
```

### `get_ai_resource` and `get_ai_skill`

Return one text with its body, as an editor reads it: the MCP names no
backend, so that the read does not count as one of the assistants. For an
AIManager, the groups it is published to, named. The body is untrusted text.

```text
GET /api/ai/resources/<name>
GET /api/groups/<id>        one per group, to name it
```

### `create_ai_resource` and `create_ai_skill`

| Input | Meaning |
|---|---|
| `name` | The name, lowercase letters and digits separated by single dashes, at most 64 bytes; for a skill, the name of its frontmatter too |
| `title`, `description`, `mime_type` | A resource only: its title, what it holds and when to read it (required), and its format, `text/markdown` by default or `text/plain` |
| `backends` | `oc3`, `om3` or both |
| `groups` | The groups, by name or ID, whose members' assistant reads it |
| `everybody` | `true` publishes it to every user instead of groups |
| `body` | The text; for a skill, the whole `SKILL.md` |

Without `groups` nor `everybody`, the tool refuses before calling the
collector, which checks the rest: the bounds, and the frontmatter of a skill.

```text
GET /api/groups/<group>     one per group, to resolve it
POST /api/ai/resources
POST /api/ai/skills
```

### `update_ai_resource` and `update_ai_skill`

| Input | Meaning |
|---|---|
| `name` | The exact name; it stays |
| `edits` | The changes of the text: each `{old, new}` replaces the only occurrence of `old`, in order. An `old` missing or found several times refuses the whole change |
| `body` | A whole new text instead of `edits` |
| `title`, `description`, `mime_type` | A resource only: the new values |
| `backends` | The new backends |
| `groups`, `everybody` | The new publications, replacing the former ones |

The fields not given are kept. The collector replaces the whole object, so
the MCP reads it first, as an editor, and sends it back changed. A caller the
collector gives no publications, who is no AIManager, is refused before any
write: the MCP does not send back what it could not read whole.

```text
GET /api/ai/resources/<name>
PUT /api/ai/resources/<name>
```

### `delete_ai_resource` and `delete_ai_skill`

Delete a text; the collector answers 204.

```text
DELETE /api/ai/resources/<name>
DELETE /api/ai/skills/<name>
```

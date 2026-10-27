---
domain: compliance_changes
tools:
  - apply_compliance_changes
stability: experimental
---

# Compliance Change Tools

This document describes the tool that changes the compliance rulesets and
modulesets of the OpenSVC collector: a list of operations, applied all of
them or none, and recorded as one version of the compliance history. It is
registered only when `OPENSVC_MCP_ACTIONS=enabled`
([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/compliance_change.go`, with the lookups of
  `internal/core/node_compliance.go` and the reads of
  `internal/core/compliance_ruleset.go` and
  `internal/core/compliance_moduleset.go`;
- MCP definitions: `internal/tools/compliance_change.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `apply_compliance_changes` | destructive, not idempotent | CompManager or Manager, and responsible for each object changed |

The tool is destructive: a change to a ruleset or a moduleset attached
applies at the next compliance check of its nodes and services, a module in
autofix changing them, and publications and responsible groups give rights.
The OpenSVC AI agent asks the user to confirm the whole list once.

## `apply_compliance_changes`

### Collector API

```text
POST /api/compliance/designer/commits
{"message": "[AI assistant] <message>", "operations": [{"op": …, "kind": …, …}, …]}
```

The collector applies the operations in one database transaction, each with
the checks of its single route, seeing the operations before it. The first
refusal rolls all of them back, and names the operation by its index. After
the commit, it records a version of its compliance history with the message;
before the operations, it records the changes made by other means since the
last version, as a version of their own. The route is on the `dev` branch of
the collector at the time of writing.

### Operations

| `op` | Kind | Fields |
|---|---|---|
| `create` | both | `name`, `as`; a ruleset is created explicit and public |
| `rename` | both | `name` |
| `clone` | both | `as`, optional `name`, `<name>_clone` by default |
| `delete` | both | — |
| `set_type` | ruleset | `ruleset_type`: `explicit` or `contextual`; `explicit` keeps the filterset, which `set_filterset` with an empty value removes |
| `set_public` | ruleset | `ruleset_public` |
| `set_filterset` | ruleset | `filterset`, which makes the ruleset contextual; empty removes it |
| `add_variable` | ruleset | `var_name`, `var_class`, `var_value`; a variable of the same name is refused |
| `update_variable` | ruleset | `variable`, then `var_name`, `var_class` or `var_value` |
| `delete_variable` | ruleset | `variable` |
| `copy_variable` | ruleset | `variable`, `dst`, `move` |
| `include`, `detach` | both | `child_kind`, `child`: a ruleset in a ruleset, a moduleset or a ruleset in a moduleset |
| `add_module` | moduleset | `name`; autofix starts off |
| `rename_module` | moduleset | `module`, `name` |
| `set_autofix` | moduleset | `module`, `autofix` |
| `delete_module` | moduleset | `module` |
| `add_team`, `remove_team` | both | `role` (`publication` or `responsible`), `team`, a group name or ID |
| `move_team` | both | `role`, `to_role`, `team` |

An object is named by `ref`, `child` or `dst`: its exact name, its ID, or the
`tmp:` reference that an earlier `create` or `clone` gave it with `as`. A
variable value is a text: a JSON value is serialized in it.

### What the MCP checks first

Nothing is sent when one of these fails, and the error names the operation:

- the message, required, at most 2 KiB, and 1 to 100 operations, far below the
  5000 of the collector: a longer list is no request a person reads and
  confirms;
- the fields each op requires, its kind, and the kind of the child;
- the `tmp:` references: given by `create` and `clone` only, each once, a
  ruleset and a moduleset not sharing one, and named only after the operation
  giving them. The collector takes an `as` without the prefix, and a
  reference given twice, without telling;
- the objects named by name or ID, read among those the caller sees, in
  the order of the list. The collector clones an object the caller does not
  see; the MCP refuses it. A name an earlier operation of the list gives an
  object, by `create`, `rename` or `clone` (`<name>_clone` without a name),
  is left to the collector;
- an operation naming an object a `delete` earlier in the list removed, which
  the collector would refuse, and the whole list with it; the `detach` of a
  child deleted has nothing to do, below.

An object resolved is sent by its ID: the collector resolves each reference
again in the change, and a name a `rename` earlier in the list changed would
be refused. A name the list gives, and a `tmp:` reference, are sent as given.

The operations on the links and the groups, `include`, `detach`,
`add_team`, `remove_team` and `move_team`, are judged on the export of their
object, read once, then changed in memory by the operations of the list in
their order. One with nothing to do, an include or an add_team of what is
there, a detach or a remove_team of what is not, a detach of a child deleted
earlier in the list, is listed in `unchanged_operations` and not sent: the
collector refuses a link it holds already, or a group it does not know, and
the whole list with it. A child created or named by the list, or a group
given by ID, is left to the collector. The index of a refusal is mapped back
to the list passed. When every operation has nothing to do, nothing is sent
and no version is recorded.

The message is sent with the mark `[AI assistant] ` before it: the collector
names the source of these versions `designer`, as for its own designer, and
the mark tells them apart in `list_compliance_history`.

### Input

| Input | Meaning |
|---|---|
| `message` | What the change does, its first line a summary |
| `operations[]` | 1 to 100 operations, with the fields above; `op`, `kind`, `child_kind`, `role`, `to_role` and `ruleset_type` list their values |

### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `outcome` | `applied`, or `unchanged` when nothing was sent |
| `applied` | The operations the collector applied |
| `version` | `commit` and `recorded`, false when the configuration was already as recorded |
| `version_error` | Why no version was recorded, the change being saved |
| `created[]` | `ref`, `kind` and `id` of the objects created or cloned |
| `unchanged_operations[]` | `index`, `operation` and `reason` of the operations with nothing to do |
| `deleted` | The objects deleted, as `kind:name` |
| `read_back_rulesets`, `read_back_modulesets` | The objects changed, at most 10, read after the change as `get_compliance_ruleset` and `get_compliance_moduleset` give them, the variables masked |
| `warning` | The objects changed and not read back, and why |

A refusal of the collector is returned as an error naming the operation, by
its index, op, kind and object, with the HTTP status and the text of the
collector; nothing was applied. The values passed in the operations are not
masked in the confirmation the agent shows nor in its audit: the description
says so.

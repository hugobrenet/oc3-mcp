---
domain: form
tools:
  - list_forms
stability: experimental
---

# Form Tools

This document describes the tool that reports the forms of the OpenSVC
collector: the forms its users submit to start a request, to run an action,
or to set a compliance variable.

Implementation:

- business logic: `internal/core/form.go`, with the shared list helpers of
  `internal/core/collector_list.go`;
- MCP definitions: `internal/tools/form.go`.

Visibility is that of the collector account of the caller: a manager sees
every form, the others the forms published to one of their groups.

## Tools

### `list_forms`

Returns one page of the forms, sorted by folder then name, without their
definition text. Use it to tell which form to submit for a request, or which
compliance variable forms exist.

#### Collector API

```text
GET /api/forms?props=id,form_name,form_type,form_folder,form_author,form_created,form_definition&orderby=form_folder,form_name,id&filter=…&limit=…&offset=…
```

`form_definition` is the YAML of the form parsed by the collector: the MCP
reads its title, description, inputs and outputs, and returns neither the YAML
nor the input definitions.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional text the form name contains, case-insensitive |
| `type` | Optional form type: `obj`, `custo`, `generic` or `folder` |
| `folder` | Optional exact folder, such as `/compliance` or `/samples` |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Forms matching the filters, as the collector counts them |
| `count`, `truncated` | Forms in this page, and whether more remain |
| `forms[]` | `id`, `name`, `label`, `description`, `type`, `folder`, `author`, `created`, `inputs`, `outputs`, `next_steps`, `definition_unavailable` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`outputs` lists what a submission does, by output: `workflow` starts or goes
on with a request, as `list_requests` lists them; `compliance variable` sets a
compliance variable; `script`, `rest`, `mail` or `db` run an action.
`next_steps` are the steps a workflow output asks for next: each holds its
`condition`, the `forms` to submit next, and the `assignee` of the next step.
A workflow output running a script has a `Scripts` block whose `Success` or
`Error` branch the collector takes by the return code of the script, in place
of the direct `NextForms`: its steps come `on_success` and `on_error`, and a
branch without forms closes the request. Without `Scripts`, the step comes
`always`. A form named in the next steps of another is a step of a request,
not its start.

The collector leaves `form_definition` out when it cannot read the YAML of a
form: such a form has `definition_unavailable`, and no `inputs`, outputs or
next steps, which are unknown rather than empty. `obj` forms are the
compliance variable forms; `custo` and `generic` forms start requests or run
scripts. The description is cut to 512 bytes.

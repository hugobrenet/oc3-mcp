---
domain: form
tools:
  - get_form
  - get_form_result
  - list_forms
  - submit_form
stability: experimental
---

# Form Tools

This document describes the tools that report the forms of the OpenSVC
collector, the forms its users submit to start a request, to run an action,
or to set a compliance variable, and the tools that submit them and read
the result of a submission.

Implementation:

- business logic: `internal/core/form.go` and `internal/core/form_detail.go`,
  with the shared list helpers of `internal/core/collector_list.go` and the
  masking rules of `internal/core/mask.go`;
- submission: `internal/core/form_submit.go`, with the masks of
  `internal/core/mask.go`;
- MCP definitions: `internal/tools/form.go` and `internal/tools/form_submit.go`.

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

The collector filters on the form name only: the title and the description
live in the definition. With `text`, the MCP reads the first 1000 forms
matching the other filters, keeps those whose name, title or description holds
the text, and pages them itself: `total` is then its own count, and
`scan_truncated` tells that more forms matched the other filters than it
read.

#### Input

| Input | Meaning |
|---|---|
| `text` | Optional text the name, title or description contains, case-insensitive, to find a form by what it is for |
| `name` | Optional text the form name itself contains, case-insensitive; it does not search the titles |
| `type` | Optional form type: `obj`, `custo`, `generic` or `folder` |
| `folder` | Optional exact folder, such as `/compliance` or `/samples` |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Forms matching the filters, as the collector counts them, or with `text` as the MCP counts them among those it read |
| `scan_truncated` | With `text`, whether more forms matched the other filters than the 1000 the MCP read |
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

### `get_form`

Returns one form by its exact name or ID: what it asks, what its submission
does, who may submit or change it, and its definition text.

#### Collector API

```text
GET /api/forms?props=id,form_name,form_type,form_folder,form_author,form_created,form_yaml,form_definition&filter=form_name:eq:<name>|id:eq:<id>&orderby=id&limit=10&offset=0
GET /api/forms/<id>/publications?props=role&limit=100
GET /api/forms/<id>/responsibles?props=role&limit=100
```

A number is read as a form ID first, then as a form name, since a form may be
named with digits. A name several forms hold is refused with their IDs; a form
the caller may not see is not found.

#### Input

| Input | Meaning |
|---|---|
| `form` | One exact form name, as `list_forms` and the requests give it, or form ID |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `form` | The form, as `list_forms` returns it |
| `inputs[]` | `key`, `id`, `label`, `type`, `mandatory`, `default`, `candidates[]` (`value`, `label`), `strict_candidates`, `candidates_source`, `condition`, `help`, `sensitive` |
| `inputs_omitted`, `candidates_omitted` | The inputs left out after 100, and the candidates left out by the size bounds |
| `publications` | The groups the form is published to: their members may submit it |
| `responsibles` | The groups responsible for the form: their members may change it |
| `groups_truncated` | Whether publications or responsibles were left out after 100 |
| `yaml`, `yaml_truncated` | The definition text of the form, cut to 32 KiB, and whether it was |

The definition is read as the collector reads it: the elements of `Inputs` and
`Outputs` that are not objects are left out, and `Mandatory` and
`StrictCandidates` are true for a YAML boolean or for `yes`, `true` or `on`,
since historical forms write `Mandatory: Yes`, which YAML keeps a string. The
`key` of an input is its `Key` when it has one, else its `Id`, as a
submission keys its value; `id` is given when it differs. A candidate that is
an object submits its `Value` and shows its `Label`. `candidates` are the
values a form lists, at most 50 per input, 256 bytes each and 32 KiB in all;
`candidates_source` is where the collector computes them from, such as the
`Function` of the input. `sensitive` tells an input whose `Id`, `Key`, full
label or type names a secret or a mail, as `get_request` masks its values.

The definition text is the YAML of the form, as its authors wrote it: what a
submission runs, such as the scripts, the REST calls and their headers, the
mails or the database writes. It is returned with these masks: the value of a key
that names a secret, an authorization header, a cookie or a mail, on the same
line, as a block scalar (`Password: |`), on the more indented lines below a
key alone on its line, or in a flow mapping (`{password: x}`); any email
address; and the default value of a sensitive input where it is the value of
a key. A secret written under a key that names none, such as an
argument of a script, is not recognized: the masking reduces the exposure, it
does not guarantee it.

### `submit_form`

Submits one form with the values of its inputs. Registered only when
`OPENSVC_MCP_ACTIONS=enabled`; destructive and not idempotent: the agent
asks the user to confirm each call, and each call runs the outputs again.

#### Collector API

```text
GET /api/forms?props=id,form_name,form_definition&filter=…        the form, by name or ID
PUT /api/forms/<id>   {"data": {<key>: <value>, …}}                the submission
GET /api/form_output_results/<results_id>                          with wait, for an Async form
```

The MCP finds the form as `get_form` does, and sends the values as given:
it checks none of them. The collector refuses the submission of a form not
published to a group of the caller, unless a Manager, with 404, and with
400 and the reason a mandatory value missing, a key the form forces, or a
value out of strict candidates, before any output runs. It does not check
the type, the default or the constraint of an input: a mandatory value may
be an empty string. It then runs the outputs in order, and answers with
their results:

| `Dest` | What the collector does |
|---|---|
| `workflow` | Starts a request, or goes on with one, which `get_request` reads |
| `rest` | Calls its API, with the credentials of the submitter, or an external URL |
| `mail` | Sends a mail |
| `script` | Runs a program on the collector host |
| `db` | Inserts a row in a table of the collector database |
| none, or any other such as `compliance variable` | Nothing, and tells nothing |

The collector answers with success even when an output failed:
`returncode` is the sum of the failures, 0 when none failed, and the log of
the output tells why. On dev2, `test-mcp` has a `Script:` output without
`Dest`: its submission does nothing, `COMPLETED` with `returncode` 0.

A form that is not `Async` is answered once its outputs ran: the MCP bounds
the submission by 2 minutes instead of the 30 seconds of the other
requests, as the calls of its rest outputs and their `WaitResult` may take
long. Beyond, the MCP ends the request, which ends the outputs of the
collector midway, without a result to read. An `Async` form is answered at
once, `QUEUED`: with `wait`, at most 120 seconds, the MCP reads its result
again every 3 seconds until `COMPLETED`; without, `follow_up` names
`get_form_result`. A rest output replays the token of the caller: a short
one may expire while it runs.

#### Input

| Input | Meaning |
|---|---|
| `form` | One exact form name or form ID |
| `data` | The values of the inputs, each under the `key` `get_form` gives, as the user gives them, as text: the collector compares the strict candidates and the forced keys as text, and refuses the number 2 for the candidate "2" |
| `wait` | Optional seconds, 0 to 120, to wait for the outputs of an `Async` form to end |

The internal forms of the collector, of negative IDs, are not found: the
collector does not list them, nor give their inputs. They wait for a later
lot, and for the collector to list them.

### `get_form_result`

Reads the result of one submission by its `result_id`. Read-only.

```text
GET /api/form_output_results/<results_id>
GET /api/forms?props=id,form_name,form_definition&filter=id:eq:<form_id>   for the masks
```

The collector gives a result to a Manager, to who submitted it, and to the
members of the ordinary groups they share. `wait` reads it again as
`submit_form` does. The result names its form: the MCP reads its definition
for the inputs to mask; without it, such as for an internal form,
`definition_unavailable` tells that only the keys naming a secret are
masked.

#### Output of both tools

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `form` | `id` and `name` of the form |
| `result_id` | The result, which `get_form_result` reads |
| `status` | `queued`, `running`, or `completed` once every output ran |
| `returncode` | 0 when no output failed, else the sum of the failures |
| `outputs[]` | In the order they run: `id`, or `submission` for the lines of the submission itself, such as a step already completed, which the collector logs under no output, `log[]` (`error`, `message`, its `%(key)s` fields filled, 1 KiB each, at most 50 lines), `lines_omitted`, `data` the output gave back and `sent`, the data a rest output sent, as text cut to 1 KiB, `truncated` |
| `request` | The request a workflow output started or went on with: `id`, which `get_request` takes, and `step_id` |
| `values[]`, `values_omitted` | `get_form_result` only: the values submitted, as `get_request` gives them |
| `definition_unavailable` | `get_form_result` only: whether the definition of the form could not be read |
| `waited_seconds` | With `wait`, the time waited |
| `follow_up` | `get_form_result` with `wait` while the outputs run, else `get_request` for a request |

`submit_form` does not give back the values it was given. The collector
holds them in clear in the result, and gives them back in `submitted_data`,
the data sent by rest outputs in `request_data`, and possibly in the logs
and data of the outputs. The MCP masks, in the four, the values of the
inputs the definition or their key tells sensitive, by the rules of
`get_request`, wherever such a value of at least 4 characters shows, and
the email addresses. A secret under a key that names none stays in clear.


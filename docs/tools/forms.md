---
domain: form
tools:
  - attach_form_groups
  - create_form
  - delete_form
  - detach_form_groups
  - get_form
  - get_form_result
  - get_form_revision
  - list_form_revisions
  - list_forms
  - rollback_form
  - submit_form
  - update_form
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
- changes of the forms and their groups: `internal/core/form_write.go`;
- history of the definitions: `internal/core/form_revision.go`, the
  rollback in `internal/core/form_write.go`;
- MCP definitions: `internal/tools/form.go`, `internal/tools/form_submit.go`,
  `internal/tools/form_revision.go` and `internal/tools/form_write.go`.

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
| `data` | The values of the inputs, each under the `key` `get_form` gives, as the user gives them, as text: the collector compares the strict candidates and the forced keys as text, and refuses the number 2 for the candidate "2"; an input that is not mandatory, such as a comment, is left out unless the user gives it |
| `wait` | Optional seconds, 0 to 120, to wait for the outputs of an `Async` form to end |
| `request` | Optional request ID: the form decides the pending step of that request, see [deciding a step](#deciding-a-step-of-a-request) |

#### Deciding a step of a request

The collector has no route to approve or reject a request: the form
submitted after a step decides it, such as `sample-vm-approval` after
`sample-vm-request`, its outputs running as any. With `request`, the MCP
reads the last step of the request, `last_form_id` of `/api/workflows`, and
sends it as `prev_wfid`:

```text
GET /api/workflows?props=id,last_form_id&filter=id:eq:<request>
PUT /api/forms/<id>   {"data": {…}, "prev_wfid": <last_form_id>}
```

The collector checks the rest, Manager included: the form must be one of
the `NextForms` of the step, else 403 `the <form> form is not a next step of
workflow step <id>`, and the caller its assignee, by name or group, else
403. A step already decided is answered 200, `QUEUED`, without
`results_id`, the line `This step is already completed` under `submission`,
and no output run. `submit_form` gives `next_step`, the forms a request waits
for and their assignee, from the workflow output of the form submitted, or
from the `Success` or `Error` branch of its `Scripts` by the return code, and
its `follow_up` names the call that decides the step: the model needs no
read of the request to go on with it. The assignee is the `NextAssignee` of
the form; without one, the collector chooses, and `get_request` gives it.
For a request started earlier, `get_request` gives its last form and its
assignee, and `get_form` of that form its `next_steps`. On dev2, the
approval of request 1376 closed it: `Workflow 1447 closed on last step
sample-vm-approval with id 1449`.

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
| `next_step` | `submit_form` only: what the request waits for next, as the definition of the form names it, `forms` and `assignee`; no `forms` when the step closed the request |
| `values[]`, `values_omitted` | `get_form_result` only: the values submitted, as `get_request` gives them |
| `definition_unavailable` | `get_form_result` only: whether the definition of the form could not be read |
| `waited_seconds` | With `wait`, the time waited |
| `follow_up` | `get_form_result` with `wait` while the outputs run; for a request, the forms it waits for and `submit_form` with `request`, or that it is closed |

`submit_form` does not give back the values it was given. The collector
holds them in clear in the result, and gives them back in `submitted_data`,
the data sent by rest outputs in `request_data`, and possibly in the logs
and data of the outputs. The MCP masks, in the four, the values of the
inputs the definition or their key tells sensitive, by the rules of
`get_request`, wherever such a value of at least 4 characters shows, and
the email addresses. A secret under a key that names none stays in clear.

### `create_form`, `update_form` and `delete_form`

Create, change and delete forms. Registered only when
`OPENSVC_MCP_ACTIONS=enabled`. The three are destructive, so that the
agent asks the user to confirm each call: `create_form` too, though a form
does nothing until submitted, for the user to approve the definition as
sent, which the confirmation shows. A step where the model would show the
YAML first was left out: it rests on the model, which replays showed
skipping it in half the draws.

#### The reference of the format: `oc3://docs/form-definition`

The MCP serves the reference of the YAML of a form as an MCP resource,
`oc3://docs/form-definition`, in Markdown
([`internal/resources/form_definition.md`](../../internal/resources/form_definition.md),
embedded in the binary by the `internal/resources` package), served
whatever `OPENSVC_MCP_ACTIONS`. Read from the collector code, it gives the keys of a
form, of its inputs and of its outputs; for each input key, whether the
collector checks it at submission or only the web interface reads it; the
syntax of the conditions; what each `Dest` runs, and that an output without
one does nothing; and three examples. The descriptions of `create_form` and
`update_form` tell the model to read it in the same turn as the call: the OpenSVC
AI agent keeps no string longer than 256 bytes of the earlier turns, so
that a reference read in one turn is gone in the next. A test keeps the
examples honest: each parses, and uses only the keys the reference
documents.

The `yaml` input of `create_form` and `update_form` carries
`"contentMediaType": "application/yaml"` in the input schema, the JSON
Schema keyword of the media type of a text: the agent gives it in its
confirmation, for the client to show the definition as YAML, its lines and
indentation kept, rather than guess the format of a text.

```text
POST   /api/forms        {"form_name": …, "form_yaml": …, "form_folder": …, "form_type": …}   create
POST   /api/forms/<id>   {the fields given}                                                   change
DELETE /api/forms/<id>                                                                        delete
GET    /api/forms?…&filter=id:eq:<id>, /publications, /responsibles                          read back
```

The collector requires the FormsManager or the Manager privilege for the
three, and to be responsible for the form for the deletion, not for the
change. It checks only that `form_yaml` is YAML: a key it does not know,
or an output without a `Dest` it runs, is ignored, and the form does
nothing on submission, as `test-mcp` on dev2 shows. The MCP sends the
definition as given, and reads the form back after the change as
`get_form` gives it, its inputs and outputs as the collector reads them:
an output missing shows there, and the descriptions tell the model to tell
the user. The descriptions point the model to the definition of a similar
form, which `get_form` gives, rather than its memory; a reference of the
format, as a resource of the MCP, comes in a later lot.

A creation answers 409 for a name already taken. The collector sets the
author and the date, publishes the form to the default group of the
creator and makes that group responsible for it, and commits the YAML in
its git history, as it does for each change of the definition. A change
answers with the old and new values of each field, which the MCP does not
give back: `outcome` is `unchanged` when the collector lists none. A
deletion removes the publications and responsibles of the form; the
requests it started keep their steps.

| Input | Meaning |
|---|---|
| `name` | `create_form`: the name, unique; `update_form`: optional new name |
| `yaml` | The definition; for `update_form`, optional, and it replaces the whole definition |
| `folder`, `type` | Optional folder, and form type, which the collector only stores |
| `form` | `update_form` and `delete_form`: one exact form name or form ID |

| Output field | Meaning |
|---|---|
| `outcome` | `created`, `changed`, `unchanged`, `rolled_back` (with `rollback_form`) or `deleted` |
| `id`, `name` | The form |
| `read_back` | `create_form`, `update_form` and `rollback_form`: the form read back, as `get_form` gives it |
| `warning` | Why the form read back is not what was asked, the change being made: not read back, or a definition in which the collector reads no form, such as a scalar or a list, which it stores all the same |

### `list_form_revisions`, `get_form_revision` and `rollback_form`

The history of the definition of a form, which the collector keeps in a
git repository: one revision at each creation, change of the definition
or rollback, with its date and author.

```text
GET  /api/forms/<id>/revisions                     the revisions, most recent first, 300 at most
GET  /api/forms/<id>/revisions/<revision>          {"data": {"oid": …, "content": "<the YAML>"}}
GET  /api/forms/<id>/diff/<revision>[?other=<rev>] {"data": "<git show, or git diff with other>"}
POST /api/forms/<id>/rollback/<revision>           null
```

`list_form_revisions` gives each `revision`, the git commit ID the other
two take, its `date`, in collector local time, its `author`, and for a
revision made by a rollback the message of the collector, `rollback to
<date of the revision restored>`. The collector writes the author as
`Name <email>`, or the email in place of a name the account has none of:
the email addresses are masked by `mask.go`. `limit` defaults to 20, and
goes to 300, the most the collector gives; `total` counts those it gives,
`truncated` tells older ones were left out.

`get_form_revision` gives the definition at a revision, `yaml`, masked as
`get_form` masks it: the MCP reads the YAML of the revision as the
collector reads it into its `form_definition`, for the default values of
its sensitive inputs. `change` gives what the revision changed, as `git
show` gives it, or with `compare_to` the differences from `revision` to
`compare_to`, as `git diff` gives them: the `-` lines are those of
`revision`, the `+` lines those of `compare_to`. `change` is masked line
by line: the `+`, `-` and context lines as YAML, with the default values
of the sensitive inputs of `revision`, and of `compare_to` when given,
the others for their email addresses only. The mask of `change` is more
partial than that of `yaml`: a key of a secret alone on its line leaves
its value on the next lines in clear, and without `compare_to` the
default of a sensitive input the revision removed, a `-` line of the
revision before, is masked only under a key that names a secret. Both
are cut to 32 KiB, `truncated` telling it. A revision, as `compare_to`,
must be 4 to 64 hexadecimal characters, which the MCP checks so that no
path or git reference goes to the collector; one the collector does not
know answers 404 `revision <revision> not found`. A form whose creation
the collector failed to commit in its history answers an empty `data`,
which the MCP tells as `no history recorded for form <name>`.

`rollback_form` restores the definition at an earlier revision: the
collector commits it as a new revision, the earlier ones kept, so that a
rollback is undone by another one. Registered only when
`OPENSVC_MCP_ACTIONS=enabled`, and destructive, as it replaces the
current definition. The collector requires FormsManager or Manager, and to
be responsible for the form. The form is read back as `get_form` gives
it, in `read_back`, with `outcome` `rolled_back`. A rollback to the
current definition commits nothing, the collector having nothing to
change: `outcome` is `rolled_back` all the same, and
`list_form_revisions` shows no new revision, as the description of the
tool tells the model.

| Input | Meaning |
|---|---|
| `form` | One exact form name or form ID |
| `limit` | `list_form_revisions`: the most recent revisions to return, 20 by default, 300 at most |
| `revision` | `get_form_revision` and `rollback_form`: a revision, as `list_form_revisions` gives it |
| `compare_to` | `get_form_revision`: optional other revision; `change` then gives the differences from `revision` to it |

### `attach_form_groups` and `detach_form_groups`

Publish a form to groups, whose members may submit it, and make groups
responsible for it, whose members may change it; or withdraw them.
Attaching is not destructive; detaching is, as it withdraws a right.

```text
GET         /api/groups/<group>                                 by name or ID
POST/DELETE /api/forms/<id>/publications/<group_id>
POST/DELETE /api/forms/<id>/responsibles/<group_id>
```

The MCP resolves each group, then sends one request per group, publications
first, at most 50 groups in all, and gives the outcome on each:
`attached`, `already_attached`, `detached`, `already_detached`, read from
the message the collector answers with 200, or `refused` with the reason.
The collector requires FormsManager and to be responsible for the form,
refuses a privilege group, such as Manager, with 403, and to a caller not a
Manager a group they are not a member of.

After a detachment of responsibles, the MCP reads back the groups still
responsible, `responsibles`, and `warning` tells when none is left: only a
Manager may then change, publish or delete the form, the caller included
if not a Manager. The detachment is made all the same.


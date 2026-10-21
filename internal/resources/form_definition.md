# OpenSVC collector form definition

The YAML a form of the OpenSVC collector is written in, as `create_form` and
`update_form` send it. Read from the collector code (`server/handlers`:
`forms_validate.go`, `forms_engine.go`, `forms_submit.go`,
`forms_outputs.go`) and from its built-in forms (`forms_internal.json`).

The collector checks only that the text is YAML. A key it does not know, a
key written in another case (`inputs` for `Inputs`), or an output without a
`Dest` it runs is stored, then ignored: the form does nothing on submission,
and nothing says so. Keys are capitalized, as below. Booleans read as YAML
1.1: `true`, `yes` and `on` are true, whatever their case.

## Top-level keys

| Key | Read by | Meaning |
|---|---|---|
| `Label` | collector and web interface | Title of the form; the subject of the mails of its `mail` outputs |
| `Desc` | web interface | What the form is for |
| `Inputs` | collector and web interface | List of the inputs, see below |
| `Outputs` | collector | List of the outputs, run in order, see below |
| `Async` | collector | `true`: the submission is answered at once, queued, its outputs running after; else it is answered once they ran |
| `Vertical` | web interface | Layout of the inputs |
| `Css` | web interface | Style of the form |

## Inputs

The submission carries the value of each input under its key: its `Key`
when it has one, else its `Id`. Submit every value as text: the collector
compares candidates, forced keys and conditions as text, and the number 2
never equals the candidate "2".

| Key | Checked at submission | Meaning |
|---|---|---|
| `Id` | yes | Name of the input; its key unless `Key` is set |
| `Key` | yes | Key of the value in the submission, when it differs from `Id` |
| `Mandatory` | yes | `true`: a missing value is refused; an empty text passes |
| `Condition` | yes | The input applies only when its condition holds, see below |
| `Candidates` | with `StrictCandidates` | Values offered: texts, or objects with `Value` (submitted) and a label |
| `StrictCandidates` | yes | `true`: a value out of `Candidates`, or out of the candidates `Function` computes, is refused |
| `Function` | with `StrictCandidates` | Collector API path the candidates are read from, such as `/services`, `#key` replaced by the value of another input |
| `Args` | with `Function` | Query parameters of `Function`, as lines `key = value`, `#key` replaced likewise |
| `Value` | with `Function` | Property of the rows of `Function` a value is compared with, such as `#svc_id` |
| `Keys` | yes | Lines `key = value` the submitted data must carry, `#key` replaced; a mismatch is refused |
| `Label` | web interface | Label shown |
| `Type` | web interface | Kind of field; the collector never reads it, see below |
| `Default` | web interface | Value proposed; the collector does not apply it |
| `Help` | web interface | Help text |
| `Format` | web interface | Display of the candidates of `Function`, such as `#svcname` |
| `Constraint` | web interface | Constraint shown; the collector does not enforce it |
| `Unit` | web interface | Unit shown beside the value |
| `Form` | web interface | With `Type: form`, the name of a form whose inputs are nested; the collector does not check them |
| `DisplayModeLabel` | web interface | Label of the input in display mode |
| `DisableAutoDefault` | web interface | No default proposed |
| `LabelCss` | web interface | Style of the label |

`Type` values the built-in forms of the collector use: `string`, `integer`,
`password`, `time` and `form`. The values of an input of type `password` are
masked in the results the MCP gives. Any other value is not refused, and
the web interface may not know it: prefer these.

### Conditions

A condition compares the value of another input, named `#key`, as text:

| Condition | Holds when |
|---|---|
| `#os == linux` | the value is the text `linux` |
| `#os != linux` | the value is missing or another text |
| `#os IN linux,sunos` | the value is one of the texts, comma-separated |
| `#os NOT IN linux,sunos` | the value is missing or none of the texts |
| `#size > 10`, `#size < 10` | the value reads as a number above, or below |
| `#ip == empty`, `#ip != empty` | the value is missing or empty, or not |

A list of conditions holds when all of them hold.

## Outputs

The outputs run in order, each as its `Dest` tells. The return code of
the submission counts the failures, which the log of each output tells.
Some stop the run, such as a `rest` output without `Handler` or a
`workflow` output that cannot store its step; most are logged, and the
next outputs run. The failures of `db` and `mail` outputs are logged
without counting in the return code: an insert or a mail that fails does
not take the `Error` branch of `Scripts`.

| Key | Meaning |
|---|---|
| `Dest` | What the output does: `workflow`, `rest`, `script`, `mail` or `db`; without it, or with another value, the output does nothing and logs nothing |
| `Id` | Name of the output in the result; `output-<n>` by default |
| `Type` | `json`: a `script` or `workflow` output gets the submitted data as JSON text; a `rest` output sends the data as they are |
| `Format` | `dict`, or `list`; a `Condition` needs `dict` |
| `Condition` | The output runs only when its condition holds: one condition, written as for an input, not a list, which is logged as a malformed condition and the output skipped; on an output not `dict`, the condition is logged as an error and the output skipped |
| `SkipOnErrors` | `true`: skipped when an earlier output failed |
| `NextForms` | `workflow`: the forms one of which decides the next step; none closes the request, `NextAssignee` then asked nothing |
| `NextAssignee` | `workflow`: the team or user name the next step is assigned to; without it, the primary group of the submitter, else the earlier submitter, else the submitter |
| `Mail` | `workflow`: `true` mails the assignee of a pending step; a step that closes the request mails nobody |
| `Scripts` | `workflow`: `Success` and `Error` branches, each with its own `NextForms` and `NextAssignee`, taken by the return code of the earlier outputs |
| `Success`, `Error` | The branches of `Scripts` |
| `Handler` | `rest`: `GET`, `POST`, `PUT` or `DELETE` |
| `Function` | `rest`: collector API path, `#key` replaced, or an external `https://` URL |
| `Keys` | `rest`: the keys of the data sent, the others left out |
| `Mangle` | `rest`: JavaScript changing the data before it is sent |
| `WaitResult` | `rest` with `GET`: seconds to read again until data comes |
| `LogRequestData` | `rest`: `false` keeps the data sent out of the result |
| `Path` | `script`: the program the collector runs on its host, with the data, the output id and the result as arguments |
| `To` | `mail`: recipients, email addresses or user names |
| `Table` | `db`: the collector table a row is inserted in, the data keys that are columns |

A `workflow` output is the one that starts a request, or decides its step
when the form is submitted for a pending step. The other outputs run
actions, and leave only the result of the submission.

## Examples

A request, assigned to a team, that another form decides:

```yaml
Label: Virtual machine request
Desc: Request a new virtual machine.
Inputs:
  - Id: hostname
    Label: Hostname
    Type: string
    Mandatory: true
  - Id: cpu
    Label: vCPUs
    Type: string
    Candidates:
      - "2"
      - "4"
      - "8"
    StrictCandidates: true
  - Id: comment
    Label: Comment
    Type: string
Outputs:
  - Dest: workflow
    Id: workflow
    NextAssignee: Infra Unix
    NextForms:
      - sample-vm-approval
```

The form that decides it, closing the request:

```yaml
Label: Virtual machine approval
Inputs:
  - Id: decision
    Label: Decision
    Type: string
    Mandatory: true
    Candidates:
      - approved
      - rejected
    StrictCandidates: true
  - Id: comment
    Label: Comment
    Type: string
Outputs:
  - Dest: workflow
    Id: workflow
```

A change of the comment of a service chosen among those the collector
lists, by a call of its API, then a request whose next step depends on the
call:

```yaml
Label: Service analysis
Inputs:
  - Id: svc_id
    Label: Service
    Type: string
    Mandatory: true
    Function: /services
    Args:
      - props = svc_id,svcname
      - limit = 0
    Format: "#svcname"
    Value: "#svc_id"
    StrictCandidates: true
  - Id: svc_comment
    Label: Comment
    Type: string
    Mandatory: true
    Condition: "#svc_id != empty"
Outputs:
  - Dest: rest
    Id: comment
    Handler: POST
    Function: /services/#svc_id
    Keys:
      - svc_comment
  - Dest: workflow
    Id: workflow
    Scripts:
      Success:
        NextAssignee: Infra Unix
        NextForms:
          - service-analysis-report
      Error:
        NextAssignee: Manager
        NextForms:
          - service-analysis-report
```

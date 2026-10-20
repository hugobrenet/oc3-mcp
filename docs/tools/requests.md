---
domain: request
tools:
  - get_request
  - list_requests
stability: experimental
---

# Request Tools

This document describes the tool that reports the requests of the OpenSVC
collector: the workflows its users start by submitting a form with a workflow
output, such as a virtual machine or a firewall flow request, and that go on
form after form, each step assigned to a team or a user.

Implementation:

- business logic: `internal/core/request.go`, with the shared list helpers of
  `internal/core/collector_list.go` and the tag data reader of
  `internal/core/tag.go`;
- MCP definitions: `internal/tools/request.go`.

Visibility is that of the collector account of the caller: a manager sees
every request, the others those they or one of their teams take part in, as
creator, assignee, or submitter or assignee of one of their steps.

## Tools

### `list_requests`

Returns one page of the requests, the most recently updated first. Use it to
tell which requests wait for a team or a user, what a user requested, or
where a request stands.

#### Collector API

```text
GET /api/workflows?props=id,form_name,form_folder,last_form_name,status,steps,creator,create_date,last_assignee,last_update&orderby=-last_update,-id&filter=…&limit=…&offset=…
```

`form_name` and `form_folder` are those of the form revision the request
started from, which the collector joins by its md5.

#### Input

| Input | Meaning |
|---|---|
| `id` | Optional exact request ID, to tell where one request stands |
| `status` | Optional exact status: `pending` or `closed` |
| `assignee` | Optional exact name of the team or user the last step was assigned to, case-insensitive |
| `creator` | Optional exact name of who created the request, case-insensitive |
| `form` | Optional exact name of the form that started the request |
| `created_since`, `updated_since` | Optional earliest creation or update date, included, in collector local time: `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or `YYYY-MM-DD HH:MM:SS` |
| `created_until`, `updated_until` | Optional latest creation or update date, excluded, in the same format: `created_since 2026-09-01` and `created_until 2026-10-01` keep September |
| `limit` | Optional page size, 1 to 200, default 50 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

The collector stores the user names of the requests with stray spaces, such
as ` Manager` for a user without a first name: the MCP matches the creator and
the assignee exactly whatever the spaces around them, with an anchored
regular expression, and returns the names trimmed. `Manager` matches
` Manager`, not `Test Manager`.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `total` | Requests matching the filters, as the collector counts them |
| `count`, `truncated` | Requests in this page, and whether more remain |
| `requests[]` | `id`, `form`, `form_folder`, `last_form`, `status`, `steps`, `creator`, `created`, `assignee`, `updated` |
| `next_cursor` | Opaque cursor of the next page, when `truncated` |

`form` is the form that started the request, `last_form` the form last
submitted. For a pending request, `assignee` is who is to submit the next
form. `closed` tells that the workflow is closed, not that the requested
operation succeeded. The values the forms hold are read with `get_request`.

The cursor is an offset in requests sorted by their last update: it is no
snapshot. A request updated between two pages moves to the front, and may be
missed or listed twice.

### `get_request`

Returns one request with a page of the forms submitted in it, the first one
first. Use it to tell what was requested, who submitted each step and what
they decided.

#### Collector API

```text
GET /api/workflows?props=…,form_head_id&filter=id:eq:<id>&orderby=id&limit=1&offset=0
GET /api/forms_store?props=id,form_name,form_submitter,form_submit_date,form_assignee,form_data,form_definition&filter=form_head_id:eq:<form_head_id>&orderby=id&limit=…&offset=…
```

The stored forms of a request share the ID of its first one, `form_head_id`.
`form_definition` is the definition of the form revision each was submitted
with, which gives the title of the form and the label of each input.

#### Input

| Input | Meaning |
|---|---|
| `id` | One request ID, as `list_requests` returns it |
| `limit` | Optional number of steps returned, 1 to 50, default 20 |
| `cursor` | Optional `next_cursor` of a previous call with the same `id` |

A request the caller takes no part in is not found, as in `list_requests`.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `request` | The request, as `list_requests` returns it |
| `total_steps` | The forms submitted in the request |
| `steps[]` | `step` (from 1), `form`, `label`, `submitter` and `assignee`, an email address in them masked, `submitted`, `values[]`, `values_omitted`, `data_error` |
| `values[]` | `entry`, `input`, `label`, `value`, `masked`, `truncated` |
| `next_cursor`, `truncated` | The cursor of the next steps, and whether some remain |

The `assignee` of a step is who the step after it was assigned to.

**Values.** The collector stores the values of a step as a JSON object keyed
by input, or as a list of such objects, one per entry: the values of a list
carry their `entry`, from 1. A value is keyed by the `Key` of its input when
it has one, else its `Id`, as the collector reads the data; the values of the
inputs come in their order, then the others by key. A value is returned as
text: a string as it is, another JSON value compacted, cut to 1 KiB. A step
whose values are not one JSON value, are followed by other data, or are
neither an object nor a list of objects, has a `data_error` and no values; an
empty step has neither. Several inputs may share a key, such as under
conditions: the first one gives the label, and any sensitive one masks the
value.

**Masking.** A value is replaced by `(masked)` when the `Id`, `Key`, label or
type of its input names, as a whole word, a password, a secret, a token, a
credential, a private, API, SSH, secret or access key, or a mail, such as
`admin_password`, `apiKey` or `requestor_email`, but not `private_ip` or
`bypass`. Inside an object or a list, the values of such keys are masked the
same way before the text is written, and any string holding an email address
is masked too: `masked` then tells that part of the value was.

**Size.** A step returns at most 100 values, and a page at most 256 KiB of
values, well under the size of a result an AI client accepts: a step that does
not fit waits for the next page, and the first step of a page, when it alone
does not fit, has its last values left out, counted in `values_omitted`.

The output of the scripts a form runs, under `/form_output_results`, is not
returned.

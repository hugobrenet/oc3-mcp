---
domain: request
tools:
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
  `internal/core/collector_list.go`;
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
operation succeeded. The values the forms hold are not returned.

The cursor is an offset in requests sorted by their last update: it is no
snapshot. A request updated between two pages moves to the front, and may be
missed or listed twice.

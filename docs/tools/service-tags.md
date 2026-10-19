---
domain: service_tag
tools:
  - attach_service_tag
  - detach_service_tag
stability: experimental
---

# Service Tag Tools

This document describes the tools that change the services of the OpenSVC
collector inventory: they attach tags to services or detach them. They are
registered only when `OPENSVC_MCP_ACTIONS=enabled` ([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/service_tag.go`, with the worker loop of
  `internal/core/node_selection.go`;
- MCP definitions: `internal/tools/service_tag.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `attach_service_tag` | not read-only, not destructive, idempotent | Responsible for each service |
| `detach_service_tag` | destructive, idempotent | Responsible for each service |

## Selection

The services are given one of two ways, as the nodes of the
[node tag tools](node-actions.md#selection):

- `services`: at most 50 exact service names or service IDs, read up to the
  total of the collector (`filter=svcname:in:…`, `filter=svc_id:in:…`). An
  unknown or invisible name, or a name several clusters hold, refuses the
  whole call before any change, with the IDs and clusters of the services
  holding it; a name given twice is changed once.
- at least one of the filters of `list_services`: `name`, `cluster_id`,
  `app`, `env`, `availstatus`, `status`, `topology`, `frozen`, `placement`,
  `filterset`, `ruleset`, `moduleset`, `filter_tag` for its tag filter, as
  `tag` names the tag changed, and `node`, the services with an instance on
  a node, read from `/api/services_instances`. At most one of `filter_tag`,
  `filterset`, `ruleset` and `moduleset`, and `node` with none of them. The MCP reads the selection, and refuses one of more than
  1000 services, or one that changed while it was read, before any change.

The MCP sends one request per service, four at a time: the change is not
atomic.

## Collector API

```text
GET    /api/tags?props=tag_id,tag_name&filter=tag_name:eq:<tag>
GET    /api/services, /api/tags/<tag_id>/services, /api/filtersets/<id>/services,
       /api/compliance/rulesets/<id>/services or /api/compliance/modulesets/<id>/services?props=svc_id,svcname,cluster_id&filter=…
GET    /api/services_instances?props=svc_id,services.svcname,services.cluster_id&filter=node_id:eq:<node_id>&filter=services.…
POST   /api/tags/<tag_id>/services/<svc_id>   {"tag_attach_data": …}   attach
DELETE /api/tags/<tag_id>/services/<svc_id>                            detach
```

The MCP always gives the service ID: by its name, the collector would take
one of the services several clusters may hold under it. The collector
answers an attachment already there, or a detachment of a tag the service
does not hold, with 200 and an info saying so; it refuses a tag incompatible
with the other tags of the service with 409, and a service the caller is
not responsible for with 403, and logs each change.

## Input

| Input | Meaning |
|---|---|
| `tag` | One exact tag name, case-insensitive, or tag ID |
| `services` | Optional, see [selection](#selection) |
| filters, `filter_tag`, `node` | Optional, see [selection](#selection) |
| `attach_data` | `attach_service_tag` only: optional data of the attachment, JSON text of 1 to 4096 bytes; left out, the data of an attachment already there is kept |

## Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `tag` | `tag_id`, `name` |
| `node`, `filter_tag`, `filterset`, `filter_ruleset`, `filter_moduleset` | The filter of the selection, resolved |
| `selected` | Services selected |
| `changed`, `unchanged`, `refused` | Services changed, already as asked, refused by the collector |
| `services[]` | `svc_id`, `svcname`, `cluster_id`, `outcome` (`attached`, `already_attached`, `detached`, `already_detached`, `refused`, `not_attempted`) and `reason` of a refusal, sorted by service name |

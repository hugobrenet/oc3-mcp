---
domain: service_compliance
tools:
  - attach_service_ruleset
  - detach_service_ruleset
  - attach_service_moduleset
  - detach_service_moduleset
stability: experimental
---

# Service Compliance Tools

This document describes the tools that attach compliance rulesets and
modulesets to the services of the OpenSVC collector, or detach them. They
are registered only when `OPENSVC_MCP_ACTIONS=enabled`
([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/service_compliance.go`, with the selection
  of `internal/core/service_tag.go` and the outcome of
  `internal/core/node_compliance.go`;
- MCP definitions: `internal/tools/service_compliance.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `attach_service_ruleset`, `detach_service_ruleset`, `attach_service_moduleset`, `detach_service_moduleset` | destructive, idempotent | Responsible for each service, by its app |

A moduleset is the modules the agent runs at each compliance check of the
service, which a compliance fix lets change it; a ruleset is the variables,
the expected values the modules check. Only an explicit ruleset is attached:
a contextual one applies to the services its filterset selects. An
attachment is destructive, as for the nodes: it makes the agent run modules,
which may change what the service runs on.

## Selection

The services are selected as for the [service tag tools](service-tags.md#selection),
by `services`, at most 50 exact names or IDs, or by the filters of
`list_services`, up to 1000 services, with `node`. The ruleset or moduleset
filter is `filter_ruleset` or `filter_moduleset` here, as `ruleset` or
`moduleset` names the one changed: to detach a ruleset or a moduleset from
every service it is attached to, pass the same name to both. A contextual
ruleset is refused as a filter: to detach one attached, pass its services.
One request per service, four at a time, not atomic.

## Encapsulated services

The collector keeps the attachments of a service apart from those of its
encapsulated services, flagged `slave`. Without the `slave` parameter, which
the MCP never sends, its routes attach, detach and list the attachments of
the service itself: these tools change those, and leave the attachments of
the encapsulated services as they are, as the `ruleset` and `moduleset`
filters of `list_services` read them.

## Collector API

```text
GET    /api/compliance/rulesets/<ruleset>?props=id,ruleset_name,ruleset_type
GET    /api/compliance/modulesets/<moduleset>?props=id,modset_name
GET    /api/services, /api/tags/<tag_id>/services, /api/filtersets/<id>/services,
       /api/compliance/rulesets/<id>/services or /api/compliance/modulesets/<id>/services?props=svc_id,svcname,cluster_id&filter=…
GET    /api/services_instances?props=svc_id,services.svcname,services.cluster_id&filter=node_id:eq:<node_id>&filter=services.…
POST   /api/services/<svc_id>/compliance/rulesets/<id>     attach
DELETE /api/services/<svc_id>/compliance/rulesets/<id>     detach
POST   /api/services/<svc_id>/compliance/modulesets/<id>   attach
DELETE /api/services/<svc_id>/compliance/modulesets/<id>   detach
```

The MCP always gives the service ID: by its name, the collector would take
the first of the services several clusters may hold under it, and the
ruleset or moduleset ID, which the collector takes alone.

The collector requires the caller to be responsible for the service, by a
group responsible for its app, or Manager, and refuses another with 403. It
attaches a ruleset or a moduleset published to a group responsible for the
app of the service, or published to Everybody, and refuses another with 403,
`not attachable to this service`. It answers 409 an attachment already
there, `already attached`, and a detachment of what is not attached, `not
attached`: the MCP reports them as `already_attached` and
`already_detached`. It writes no log entry for these changes.

The collector checks that a ruleset is explicit and public only when it is
not published to Everybody: it attaches a contextual ruleset published to
Everybody. The MCP refuses to attach a contextual ruleset, before any
change, and detaches one.

## Input

| Input | Meaning |
|---|---|
| `ruleset` or `moduleset` | One exact name or ID |
| `services` | Optional, see [selection](#selection) |
| filters, `node` | Optional, those of `list_services`, with `filter_ruleset` or `filter_moduleset` for the filter of the kind changed; see [selection](#selection) |

## Output

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `ruleset` or `moduleset` | `id`, `name`, and for a ruleset `type` |
| `node`, `filter_tag`, `filterset`, `filter_ruleset`, `filter_moduleset` | The filter of the selection, resolved |
| `selected` | Services selected |
| `changed`, `unchanged`, `refused` | Services changed, already as asked, refused by the collector |
| `services[]` | `svc_id`, `svcname`, `cluster_id`, `outcome` (`attached`, `already_attached`, `detached`, `already_detached`, `refused`, `not_attempted`) and `reason` of a refusal, sorted by service name |

---
domain: service_delete
tools:
  - delete_service
  - delete_service_instance
stability: experimental
---

# Service Deletion Tools

This document describes the tools that delete a service, or one of its
instances, from the OpenSVC collector inventory. They are registered only
when `OPENSVC_MCP_ACTIONS=enabled` ([configuration](../configuration.md)).

Implementation:

- business logic: `internal/core/service_delete.go`;
- MCP definitions: `internal/tools/service_delete.go`.

| Tool | Annotations | Collector privilege |
|---|---|---|
| `delete_service` | destructive, idempotent | Responsible for the service, by its app; a Manager is responsible for every service |
| `delete_service_instance` | destructive, idempotent | Responsible for the service |

The target is exact and unique: one service, or one service and one node,
never a selection. A service name several clusters hold is refused with
their IDs, and the MCP sends the IDs to the collector, which would take one
of the homonyms by a name. The OpenSVC AI agent asks the user to confirm each
deletion: there is no other guard.

## A service still deployed comes back

The collector recreates, at each status of a cluster, the services the
cluster reports and the inventory no longer holds, with the same ID, kept in
`service_ids`, which the deletion leaves. A deletion of a service an agent
still reports lasts until its next status, and its history is lost. The MCP
counts, before the deletion, the instances an agent reported within the
hour (`mon_updated:gte:-1h`, computed by the collector: an instance is
updated at each status of its agent), and the result warns of them.

Two mechanisms of the collector clean the inventory by themselves, and the
descriptions tell the model of them:

- at each status of a cluster, it purges the instances of the objects a node
  that reports no longer has: an instance tool is for a node that reports no
  more;
- at each status of a cluster, it purges the services holding the `@purge`
  tag that have no instance left, which `attach_service_tag` attaches.

## Tools

### `delete_service`

```text
GET    /api/services/<service>?props=svc_id,svcname,cluster_id,svc_app
GET    /api/services_instances?props=svc_id&filter=svc_id:eq:<svc_id>                              instances
GET    /api/services_instances?props=svc_id&filter=svc_id:eq:<svc_id>&filter=mon_updated:gte:-1h   reported within the hour
DELETE /api/services/<svc_id>
```

The collector deletes, in one transaction logged as `service.delete`, the
service and its instances with their history: `services`, `svcmon` and its
history, `svcactions`, `action_queue`, `svc_tags`, `svcdisks`, `resmon`,
`resinfo` and their history, `checks_live`, `checks_settings`, `comp_status`,
`comp_log`, the attached rulesets and modulesets, `dashboard` and its events,
`saves`, `form_output_results` and `drpservices`. Its availability history,
`services_log`, and its ID, `service_ids`, stay.

| Input | Meaning |
|---|---|
| `service` | One exact service name or service ID |

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service` | `svc_id`, `svcname`, `cluster_id`, as it was |
| `app` | Its app |
| `instances`, `recently_reported` | Its instances, and those reported within the hour |
| `deleted` | `true` |
| `warning` | With an instance reported within the hour, why the service comes back |

### `delete_service_instance`

```text
GET    /api/services/<service>?props=svc_id,svcname,cluster_id
GET    /api/nodes/<node>?props=node_id,nodename,cluster_id
GET    /api/services_instances?props=svc_id,mon_updated&filter=svc_id:eq:<svc_id>&filter=node_id:eq:<node_id>
DELETE /api/services/<svc_id>/instances/<node_id>
```

An instance missing in the collector is refused before anything is sent.
The collector deletes, in one transaction logged as
`service_instance.delete`, the current state of the instance, `svcmon`, and
its `dashboard`, `resmon`, `resinfo` and `checks_live` entries; the history
of the service stays.

| Input | Meaning |
|---|---|
| `service` | One exact service name or service ID |
| `node` | One exact node name or node ID |

| Output field | Meaning |
|---|---|
| `provenance` | Collector API source and MCP collection time |
| `service`, `node` | The service and the node of the instance |
| `updated` | When the collector last received the instance |
| `deleted` | `true` |
| `warning` | With the instance reported within the hour, why it comes back |

# OpenSVC Collector MCP Tools

This directory documents the human-facing contracts of the OpenSVC collector
MCP tools. Names, descriptions, annotations and JSON Schemas are defined in
`internal/tools` and exposed by `tools/list`.

## Domains

| Domain | Tools | Documentation |
|---|---|---|
| Clusters | `list_clusters` | [Cluster tools](clusters.md) |
| Nodes | `list_nodes`, `count_nodes`, `get_node`, `get_node_san_topology`, `list_node_disks`, `get_node_stats` | [Node tools](nodes.md) |
| Services | `list_services`, `count_services`, `get_service`, `get_service_status_history`, `list_service_resources`, `get_service_resource_history`, `list_frozen_instances` | [Service tools](services.md) |
| Service actions | `list_service_actions`, `get_service_action_logs` | [Service action tools](service-actions.md) |
| Alerts | `list_alerts` | [Alert tools](alerts.md) |
| Checks | `list_node_checks`, `list_service_checks` | [Check tools](checks.md) |
| Packages | `list_node_packages`, `list_package_differences` | [Package tools](packages.md) |
| Events | `list_collector_events` | [Collector event tools](events.md) |
| Compliance | `list_compliance_status` | [Compliance tools](compliance.md) |

## Conventions

- Tools are read-only and closed-world.
- `list_*` tools return a bounded page with an opaque `next_cursor`; `get_*`
  tools return one exact item, and refuse a name that matches several;
  `count_*` tools return the number of matching items, as the collector
  counts them, optionally broken down by one property.
- Every result carries `provenance`: `source` (`opensvc_collector`) and
  `observed_at`, the UTC time the MCP collected it. Collector timestamps are
  returned as the collector gives them, in its local time without a zone.
- Visibility is that of the collector account of the caller: the MCP sends
  the collector the verified token of the socket, see
  [Unix socket authentication](../configuration.md#unix-socket-authentication),
  or a token exchanged for the collector on HTTPS, see
  [token exchange](../configuration.md#token-exchange).

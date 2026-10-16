# OpenSVC Collector MCP Tools

This directory documents the human-facing contracts of the OpenSVC collector
MCP tools. Names, descriptions, annotations and JSON Schemas are defined in
`internal/tools` and exposed by `tools/list`.

## Domains

| Domain | Tools | Documentation |
|---|---|---|
| Clusters | `list_clusters` | [Cluster tools](clusters.md) |
| Nodes | `list_nodes`, `count_nodes`, `get_node`, `get_node_san_topology`, `list_node_disks`, `get_node_stats` | [Node tools](nodes.md) |
| Services | `list_services`, `count_services`, `get_service`, `get_service_status_history`, `list_service_resources`, `get_service_resource_history`, `list_instances`, `count_instances` | [Service tools](services.md) |
| Service actions | `list_service_actions`, `get_service_action_logs` | [Service action tools](service-actions.md) |
| Alerts | `list_alerts` | [Alert tools](alerts.md) |
| Checks | `list_node_checks`, `list_service_checks` | [Check tools](checks.md) |
| Packages | `list_node_packages`, `list_package_differences` | [Package tools](packages.md) |
| Events | `list_collector_events` | [Collector event tools](events.md) |
| Compliance | `list_compliance_status`, `list_compliance_runs`, `count_compliance_runs`, `list_compliance_modulesets`, `get_compliance_moduleset` | [Compliance tools](compliance.md) |
| IP addresses | `list_ips` | [IP tools](ips.md) |
| Tags | `list_tags` | [Tag tools](tags.md) |
| Apps | `get_app` | [App tools](apps.md) |
| Find | `find_objects` | [Find tools](find.md) |
| Requests | `list_requests`, `get_request` | [Request tools](requests.md) |
| Forms | `list_forms`, `get_form` | [Form tools](forms.md) |
| Sysreport | `list_node_sysreport_changes`, `get_node_sysreport_diff`, `list_node_sysreport_files`, `get_node_sysreport_file` | [Sysreport tools](sysreport.md) |
| Groups and users | `list_groups`, `get_group`, `list_users` | [Group and user tools](groups.md) |

## Conventions

- Tools are read-only and closed-world.
- Each tool carries in its `_meta` the keys the tool search of an AI client
  reads, such as the OpenSVC AI agent: `opensvc/keywords`, the words a
  question may use for the tool that its description does not hold, and
  `opensvc/core`, true for the tools always shown: `count_nodes`,
  `list_nodes`, `get_node`, `count_services`, `list_services`, `get_service`
  and `list_alerts`. The registrar sets them from
  `internal/tools/search_meta.go` and refuses a tool without keywords. The
  first sentence of a description is its summary in a search: it says what
  the tool is for, in at most 240 bytes.
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

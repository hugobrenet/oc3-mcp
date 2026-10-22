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
| Service actions | `list_service_actions`, `count_service_actions`, `get_service_action_logs`, `ack_service_action` | [Service action tools](service-actions.md) |
| Alerts | `list_alerts` | [Alert tools](alerts.md) |
| Checks | `list_node_checks`, `list_service_checks`, `get_check_threshold`, `list_check_thresholds` | [Check tools](checks.md) |
| Packages | `list_node_packages`, `list_package_differences` | [Package tools](packages.md) |
| Events | `list_collector_events` | [Collector event tools](events.md) |
| Compliance | `list_compliance_status`, `list_compliance_runs`, `count_compliance_runs`, `list_compliance_modulesets`, `get_compliance_moduleset`, `list_compliance_rulesets`, `get_compliance_ruleset` | [Compliance tools](compliance.md) |
| IP addresses | `list_ips` | [IP tools](ips.md) |
| Tags | `list_tags`, `create_tag`, `update_tag`, `delete_tag` | [Tag tools](tags.md) |
| Apps | `get_app`, `create_app`, `update_app`, `delete_app`, `attach_app_groups`, `detach_app_groups` | [App tools](apps.md) |
| Find | `find_objects` | [Find tools](find.md) |
| Requests | `list_requests`, `get_request` | [Request tools](requests.md) |
| Forms | `list_forms`, `get_form`, `get_form_result`, `submit_form`, `create_form`, `update_form`, `delete_form`, `attach_form_groups`, `detach_form_groups`, `list_form_revisions`, `get_form_revision`, `rollback_form` | [Form tools](forms.md) |
| Sysreport | `list_node_sysreport_changes`, `get_node_sysreport_diff`, `list_node_sysreport_files`, `get_node_sysreport_file` | [Sysreport tools](sysreport.md) |
| Filtersets | `list_filtersets`, `get_filterset`, `create_filterset`, `rename_filterset`, `add_filterset_criterion`, `remove_filterset_criterion`, `delete_filterset` | [Filterset tools](filtersets.md) |
| Groups and users | `list_groups`, `get_group`, `list_users` | [Group and user tools](groups.md) |
| Storage | `list_storage_arrays`, `list_storage_quotas`, `list_san_switch_ports` | [Storage tools](storage.md) |
| Node actions | `create_node`, `update_node`, `delete_node`, `attach_node_tag`, `detach_node_tag`, `attach_node_ruleset`, `detach_node_ruleset`, `attach_node_moduleset`, `detach_node_moduleset`, `queue_node_action`, `freeze_node`, `unfreeze_node`, `drain_node`, `fix_node_compliance`, `cancel_node_action` | [Node action tools](node-actions.md) |
| Node action queue | `list_node_actions` | [Node action tools](node-actions.md#list_node_actions) |
| Service tags | `attach_service_tag`, `detach_service_tag` | [Service tag tools](service-tags.md) |
| Service compliance | `attach_service_ruleset`, `detach_service_ruleset`, `attach_service_moduleset`, `detach_service_moduleset` | [Service compliance tools](service-compliance.md) |
| Service downtime | `justify_service_downtime`, `unjustify_service_downtime` | [Service downtime tools](service-downtime.md) |
| Service SLA | `set_service_sla` | [Service SLA tool](service-sla.md) |
| Check thresholds | `set_check_threshold`, `unset_check_threshold` | [Check tools](checks.md#set_check_threshold-and-unset_check_threshold) |
| Service agent actions | `freeze_service`, `thaw_service`, `start_service`, `stop_service`, `restart_service`, `switch_service`, `giveback_service`, `takeover_service`, `abort_service_action`, `check_service_compliance`, `fix_service_compliance` | [Service agent action tools](service-agent-actions.md) |
| Service deletion | `delete_service`, `delete_service_instance` | [Service deletion tools](service-delete.md) |

## Conventions

- Tools are closed-world. The tools that read the collector are read-only.
  The [node action tools](node-actions.md), which change the collector
  state, are registered only when `OPENSVC_MCP_ACTIONS=enabled`; they set
  `readOnlyHint: false`, and `destructiveHint: true` for the update, the
  deletion, the detachments, the compliance attachments and the agent
  actions that change the nodes, which the OpenSVC AI agent asks the user to
  confirm.
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
- The email addresses and phone numbers of people are not returned. The
  person fields the collector records, the `author` of a form, a moduleset, a
  ruleset variable or a filterset, the `creator` and `assignee` of a request
  and of its steps, the `user` and `impersonator` of an event, mask the email
  addresses as `(masked)`: the collector records many of them as an email
  address. The user tools read no email address or phone number.
- Visibility is that of the collector account of the caller: the MCP sends
  the collector the verified token of the socket, see
  [Unix socket authentication](../configuration.md#unix-socket-authentication),
  or a token exchanged for the collector on HTTPS, see
  [token exchange](../configuration.md#token-exchange).

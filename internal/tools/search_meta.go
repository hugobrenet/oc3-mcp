package tools

import (
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The _meta keys the tool search of an AI client reads, such as the OpenSVC
// AI agent: a client searching the catalog always shows the core tools, and
// ranks the others with their keywords besides their name, title,
// description and parameters. A client that does not search ignores them.
const (
	coreToolMeta     = "opensvc/core"
	keywordsToolMeta = "opensvc/keywords"

	maxToolKeywords     = 12
	maxToolKeywordBytes = 40
)

// coreTools are always shown to the model: the questions about the fleet,
// its nodes and services, and the alerts start with them. The other tools
// are found by a search; a core tool describing an item points to the tools
// of its collections and relations.
var coreTools = []string{"count_nodes", "list_nodes", "get_node", "count_services", "list_services", "get_service", "list_alerts"}

// toolKeywords are the words a question may use for a tool that its
// description does not hold, such as synonyms: the model does not see them,
// they only rank the tool in a search.
var toolKeywords = map[string][]string{
	"abort_service_action":         {"abort orchestration", "cancel orchestration", "stuck service action"},
	"ack_service_action":           {"acknowledge", "ack failure", "ack error", "clear action errors", "acknowledge failed action"},
	"add_filterset_criterion":      {"add filter", "extend selection", "narrow selection", "filterset criteria", "change filter logic"},
	"attach_app_groups":            {"app responsible", "app owner", "give app access", "publish app", "app visibility"},
	"attach_form_groups":           {"publish form", "form publication", "form responsible", "who can submit", "share form"},
	"attach_node_moduleset":        {"add moduleset", "assign moduleset", "compliance modules", "enable compliance"},
	"attach_node_ruleset":          {"add ruleset", "assign ruleset", "compliance rules", "apply ruleset"},
	"attach_node_tag":              {"add tag", "tag nodes", "label nodes", "set tag", "bulk tag", "mass tag"},
	"attach_service_moduleset":     {"add moduleset service", "assign moduleset service", "service compliance modules"},
	"attach_service_ruleset":       {"add ruleset service", "assign ruleset service", "service compliance rules", "apply ruleset service"},
	"attach_service_tag":           {"tag services", "label services", "add tag service", "bulk tag services"},
	"cancel_node_action":           {"cancel action", "abort action", "remove queued action", "dequeue"},
	"check_orchestration":          {"check orchestration", "validate workflow", "dry run", "resolve targets", "would it run"},
	"check_service_compliance":     {"service compliance check", "check compliance service", "audit service compliance", "is service compliant"},
	"continue_orchestration_run":   {"acknowledge failure", "accept failed step", "skip failed step", "go on with run"},
	"count_compliance_runs":        {"how many", "number of fixes", "failed checks", "compliance statistics", "per module", "per node", "distribution"},
	"count_instances":              {"how many instances", "number of instances", "instances per node", "statistics", "distribution"},
	"count_nodes":                  {"how many nodes", "number", "distribution", "statistics", "fleet", "inventory", "servers", "hosts"},
	"count_service_actions":        {"how many failures", "number of failed actions", "unacknowledged failures", "action errors count", "failures by node"},
	"count_services":               {"how many services", "number", "distribution", "statistics", "services down"},
	"create_app":                   {"new app", "application code", "new application", "add application"},
	"create_filterset":             {"new filterset", "define selection", "group of nodes", "node selection", "dynamic group"},
	"create_form":                  {"new form", "create form", "write form", "form yaml", "form definition"},
	"create_node":                  {"add node", "new node", "register node", "declare node", "inventory"},
	"create_orchestration":         {"create orchestration", "new workflow", "chain actions", "sequence of actions", "runbook"},
	"create_tag":                   {"new tag", "define tag", "make tag", "tag creation"},
	"delete_app":                   {"remove app", "drop application", "app deletion"},
	"delete_filterset":             {"drop filterset", "erase filterset", "filterset deletion"},
	"delete_form":                  {"remove form", "delete form", "drop form"},
	"delete_node":                  {"remove node", "decommission", "purge node", "drop node"},
	"delete_orchestration":         {"delete orchestration", "remove workflow", "drop runbook"},
	"delete_service":               {"remove service", "purge service", "decommission service", "clean inventory"},
	"delete_service_instance":      {"remove instance", "stale instance", "orphan instance", "purge instance"},
	"delete_tag":                   {"delete tag", "drop tag", "erase tag", "destroy tag", "tag deletion"},
	"detach_app_groups":            {"remove app responsible", "revoke app access", "unpublish app", "app visibility"},
	"detach_form_groups":           {"unpublish form", "remove form publication", "remove form responsible", "restrict form"},
	"detach_node_moduleset":        {"remove moduleset", "unassign moduleset", "disable compliance"},
	"detach_node_ruleset":          {"remove ruleset", "unassign ruleset", "drop rules"},
	"detach_node_tag":              {"remove tag", "untag", "untag nodes", "unset tag", "bulk untag"},
	"detach_service_moduleset":     {"remove moduleset service", "unassign moduleset service"},
	"detach_service_ruleset":       {"remove ruleset service", "unassign ruleset service"},
	"detach_service_tag":           {"untag services", "remove tag service", "bulk untag services"},
	"drain_node":                   {"evacuate node", "stop all services", "maintenance", "empty node"},
	"find_objects":                 {"search", "lookup", "partial name", "unknown name", "what is", "fuzzy", "find"},
	"fix_node_compliance":          {"compliance fix", "remediate", "enforce compliance", "apply rules"},
	"fix_service_compliance":       {"service compliance fix", "fix compliance service", "remediate service", "enforce service compliance"},
	"freeze_node":                  {"freeze", "block failover", "stop automatic actions", "maintenance mode"},
	"freeze_service":               {"freeze service", "block failover", "maintenance service", "suspend ha"},
	"get_app":                      {"application", "owner", "ownership", "responsible", "team", "who", "contact"},
	"get_check_threshold":          {"why threshold", "threshold origin", "threshold source", "default threshold", "alert threshold", "check limit"},
	"get_compliance_moduleset":     {"moduleset details", "compliance policy", "included modulesets", "moduleset rulesets", "where used", "autofix"},
	"apply_compliance_changes":     {"change compliance", "create ruleset", "ruleset variable", "set variable", "create moduleset", "add module", "autofix", "include ruleset", "publish ruleset", "compliance designer"},
	"list_compliance_history":      {"compliance history", "compliance versions", "who changed ruleset", "ruleset changes", "designer commits"},
	"get_compliance_version":       {"compliance version", "compliance diff", "what changed in compliance", "commit"},
	"get_compliance_ruleset":       {"ruleset details", "compliance variables", "variable values", "included rulesets", "filterset", "compliance policy"},
	"get_filterset":                {"filterset", "filterset criteria", "filterset definition", "filterset members", "which nodes match", "selection", "session filter"},
	"get_form_result":              {"form result", "submission result", "form output", "form log", "async form"},
	"get_form":                     {"form details", "form inputs", "fields", "form yaml", "who can submit", "form definition"},
	"get_form_revision":            {"form version", "form diff", "form change", "what changed", "compare form"},
	"get_group":                    {"group details", "responsible for", "what does the team manage", "team responsibilities", "group apps"},
	"get_node":                     {"node details", "server", "host", "machine", "hardware", "operating system", "warranty", "location", "rack"},
	"get_node_san_topology":        {"fc", "fibre channel", "hba", "fabric", "switch", "zoning", "storage paths", "wwn", "multipath"},
	"get_node_sysreport_diff":      {"sysreport", "what changed", "configuration change", "diff", "before after", "file change", "drift"},
	"get_node_sysreport_file":      {"sysreport", "file content", "configuration file", "command output", "show file", "hosts"},
	"get_node_stats":               {"performance", "metrics", "cpu", "memory", "load", "usage", "io", "network traffic", "saturation"},
	"get_orchestration":            {"orchestration", "orchestration steps", "workflow definition", "chained actions", "runbook"},
	"get_orchestration_run":        {"orchestration run", "run output", "why the run failed", "step targets", "workflow result"},
	"get_request":                  {"request details", "request steps", "decision", "form values", "who approved", "values entered"},
	"get_service":                  {"service details", "instances", "availability", "sla", "topology", "where it runs"},
	"get_service_action_logs":      {"action log", "output", "error", "failure", "why it failed", "session"},
	"get_service_resource_history": {"resource history", "since when", "degraded", "flapping", "resource down"},
	"get_service_status_history":   {"outage", "downtime", "availability", "uptime", "sla", "since when", "history"},
	"giveback_service":             {"giveback service", "give back", "preferred node", "move back service"},
	"justify_service_downtime":     {"justify downtime", "explain outage", "planned maintenance", "exclude from availability", "downtime comment"},
	"list_alerts":                  {"alert", "alarm", "warning", "problem", "issue", "incident", "dashboard"},
	"list_check_thresholds":        {"thresholds", "default thresholds", "check defaults", "contextual thresholds", "threshold settings"},
	"list_clusters":                {"cluster", "quorum", "cluster list", "agent version"},
	"list_collector_events":        {"event", "audit", "collector log", "who changed", "history", "undef"},
	"list_compliance_modulesets":   {"modulesets", "list modulesets", "compliance modules", "compliance policy", "which modules", "autofix", "modulesets of node", "attached modulesets"},
	"list_compliance_rulesets":     {"rulesets", "list rulesets", "compliance rules", "contextual", "explicit", "rules of node", "compliance policy"},
	"list_compliance_runs":         {"compliance history", "since when", "run history", "fix history", "fixed", "failing", "check runs", "last run"},
	"list_compliance_status":       {"compliance", "non-compliant", "module", "policy", "security", "fix", "drift"},
	"list_groups":                  {"groups", "teams", "list groups", "organization", "privilege", "role", "user groups", "membership", "belong"},
	"list_instances":               {"instance", "instances down", "passive", "failover", "standby", "frozen", "running on node"},
	"list_filtersets":              {"filtersets", "list filtersets", "node selection", "saved filters"},
	"list_form_revisions":          {"form history", "form versions", "who changed form", "form changes", "revisions"},
	"list_forms":                   {"form", "forms catalog", "which form", "request form", "compliance variable", "template"},
	"list_ips":                     {"ip", "address", "owner", "ownership", "vip", "interface", "mac", "network", "subnet"},
	"list_node_actions":            {"action queue", "pending actions", "queued actions", "agent actions", "action result", "action output"},
	"list_node_checks":             {"node check", "threshold", "filesystem usage", "disk full", "inode", "multipath", "monitoring"},
	"list_node_disks":              {"disk", "lun", "storage", "san", "wwid", "array", "volume"},
	"list_node_sysreport_changes":  {"sysreport", "what changed", "configuration change", "when changed", "change history", "file change", "recent changes", "node changes", "modification"},
	"list_node_sysreport_files":    {"sysreport", "tracked files", "configuration files", "commands", "collected files"},
	"list_node_packages":           {"package", "rpm", "deb", "software", "version", "installed"},
	"list_nodes":                   {"node list", "servers", "hosts", "machines", "inventory", "frozen", "silent", "stopped reporting"},
	"list_orchestration_runs":      {"orchestration runs", "run history", "failed runs", "workflow executions", "running orchestration"},
	"list_orchestrations":          {"orchestrations", "workflows", "sequenced actions", "runbooks", "chained tasks"},
	"list_package_differences":     {"package diff", "version mismatch", "compare", "drift", "inconsistency"},
	"list_requests":                {"request", "workflow", "ticket", "approval", "pending request", "form submission", "self service", "assigned", "my team", "to approve", "waiting for me"},
	"list_san_switch_ports":        {"san switch", "switch port", "fabric", "zoning", "no light", "port down", "isl", "brocade", "wwn"},
	"list_service_actions":         {"action", "start", "stop", "restart", "failed action", "who ran", "operation history"},
	"list_service_checks":          {"service check", "threshold", "volume usage", "resource check"},
	"list_service_resources":       {"resource", "filesystem", "ip", "disk", "container", "driver", "resource status"},
	"list_services":                {"service list", "applications", "services down", "frozen", "placement"},
	"list_storage_arrays":          {"storage array", "baie", "capacity", "free space", "pool", "disk group", "powermax", "storage usage"},
	"list_storage_quotas":          {"storage quota", "quota", "allocation", "reserved space", "app storage"},
	"list_tags":                    {"tag", "label", "labels", "attribute", "classification", "metadata", "attached tags"},
	"list_users":                   {"user", "users", "people", "person", "who", "member", "members", "contact", "team members", "group members", "who is in"},
	"queue_node_action":            {"run action", "push asset", "refresh inventory", "run checks", "compliance check", "trigger sysreport"},
	"remove_filterset_criterion":   {"remove filter", "drop criterion", "widen selection", "filterset criteria"},
	"rename_filterset":             {"filterset name", "change filterset name"},
	"restart_service":              {"restart service", "restart instance", "bounce service", "restart resource"},
	"resume_orchestration_run":     {"resume run", "retry failed targets", "rerun step", "restart run"},
	"rollback_form":                {"rollback form", "revert form", "restore form", "previous version", "undo form change"},
	"set_check_threshold":          {"raise threshold", "change threshold", "set threshold", "alert threshold", "check limit", "silence check"},
	"set_service_sla":              {"sla", "availability target", "service level", "set sla", "unset sla"},
	"start_orchestration_run":      {"run orchestration", "launch workflow", "execute runbook", "start run"},
	"start_service":                {"start service", "bring up service", "service up", "start resource"},
	"stop_orchestration_run":       {"stop run", "cancel orchestration", "abort workflow", "halt run"},
	"stop_service":                 {"stop service", "shut down service", "service down", "stop resource"},
	"submit_form":                  {"submit form", "run form", "fill form", "request", "order", "ask for", "workflow"},
	"switch_service":               {"switch service", "failover service", "move service next node"},
	"takeover_service":             {"takeover service", "move service to node", "relocate service", "switch to node"},
	"thaw_service":                 {"thaw service", "unfreeze service", "resume ha"},
	"unfreeze_node":                {"thaw", "unfreeze", "resume automatic actions", "end maintenance"},
	"unset_check_threshold":        {"reset threshold", "default threshold", "remove threshold", "restore threshold"},
	"unjustify_service_downtime":   {"remove downtime justification", "count downtime again"},
	"update_app":                   {"rename app", "edit app", "app domain", "app ops team", "application code"},
	"update_form":                  {"edit form", "change form", "modify form", "form yaml", "rename form"},
	"update_node":                  {"modify node", "change node", "set app", "set status", "edit node", "node owner", "node location"},
	"update_orchestration":         {"edit orchestration", "change steps", "rename orchestration", "modify workflow", "add step"},
	"update_tag":                   {"rename tag", "edit tag", "change tag", "tag exclusion", "tag data"},
}

// searchMeta is the _meta of a tool for the tool search. Every tool has
// keywords, so that a new tool is not registered without them.
func searchMeta(name string) (mcp.Meta, error) {
	keywords, ok := toolKeywords[name]
	if !ok || len(keywords) == 0 || len(keywords) > maxToolKeywords {
		return nil, fmt.Errorf("tool %q needs 1 to %d search keywords", name, maxToolKeywords)
	}
	for _, keyword := range keywords {
		if keyword == "" || len(keyword) > maxToolKeywordBytes || keyword != strings.ToLower(strings.TrimSpace(keyword)) {
			return nil, fmt.Errorf("tool %q search keyword %q must be lowercase text of at most %d bytes", name, keyword, maxToolKeywordBytes)
		}
	}
	meta := mcp.Meta{keywordsToolMeta: slices.Clone(keywords)}
	if slices.Contains(coreTools, name) {
		meta[coreToolMeta] = true
	}
	return meta, nil
}

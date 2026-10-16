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
	"count_instances":              {"how many instances", "number of instances", "instances per node", "statistics", "distribution"},
	"count_nodes":                  {"how many nodes", "number", "distribution", "statistics", "fleet", "inventory", "servers", "hosts"},
	"count_services":               {"how many services", "number", "distribution", "statistics", "services down"},
	"find_objects":                 {"search", "lookup", "partial name", "unknown name", "what is", "fuzzy", "find"},
	"get_app":                      {"application", "owner", "ownership", "responsible", "team", "who", "contact"},
	"get_form":                     {"form details", "form inputs", "fields", "form yaml", "who can submit", "form definition"},
	"get_node":                     {"node details", "server", "host", "machine", "hardware", "operating system", "warranty", "location", "rack"},
	"get_node_san_topology":        {"fc", "fibre channel", "hba", "fabric", "switch", "zoning", "storage paths", "wwn", "multipath"},
	"get_node_sysreport_diff":      {"sysreport", "what changed", "configuration change", "diff", "before after", "file change", "drift"},
	"get_node_sysreport_file":      {"sysreport", "file content", "configuration file", "command output", "fstab", "sysctl", "hosts"},
	"get_node_stats":               {"performance", "metrics", "cpu", "memory", "load", "usage", "io", "network traffic", "saturation"},
	"get_request":                  {"request details", "request steps", "decision", "form values", "who approved", "values entered"},
	"get_service":                  {"service details", "instances", "availability", "sla", "topology", "where it runs"},
	"get_service_action_logs":      {"action log", "output", "error", "failure", "why it failed", "session"},
	"get_service_resource_history": {"resource history", "since when", "degraded", "flapping", "resource down"},
	"get_service_status_history":   {"outage", "downtime", "availability", "uptime", "sla", "since when", "history"},
	"list_alerts":                  {"alert", "alarm", "warning", "problem", "issue", "incident", "dashboard"},
	"list_clusters":                {"cluster", "quorum", "cluster list", "agent version"},
	"list_collector_events":        {"event", "audit", "collector log", "who changed", "history", "undef"},
	"list_compliance_status":       {"compliance", "non-compliant", "module", "policy", "security", "fix", "drift"},
	"list_instances":               {"instance", "instances down", "passive", "failover", "standby", "frozen", "running on node"},
	"list_forms":                   {"form", "forms catalog", "which form", "request form", "compliance variable", "template"},
	"list_ips":                     {"ip", "address", "owner", "ownership", "vip", "interface", "mac", "network", "subnet"},
	"list_node_checks":             {"node check", "threshold", "filesystem usage", "disk full", "inode", "multipath", "monitoring"},
	"list_node_disks":              {"disk", "lun", "storage", "san", "wwid", "array", "volume"},
	"list_node_sysreport_changes":  {"sysreport", "what changed", "configuration change", "when changed", "change history", "file change", "recent changes", "node changes", "modification"},
	"list_node_sysreport_files":    {"sysreport", "tracked files", "configuration files", "commands", "collected files"},
	"list_node_packages":           {"package", "rpm", "deb", "software", "version", "installed"},
	"list_nodes":                   {"node list", "servers", "hosts", "machines", "inventory", "frozen", "silent", "stopped reporting"},
	"list_package_differences":     {"package diff", "version mismatch", "compare", "drift", "inconsistency"},
	"list_requests":                {"request", "workflow", "ticket", "approval", "pending request", "form submission", "self service", "assigned"},
	"list_service_actions":         {"action", "start", "stop", "restart", "failed action", "who ran", "operation history"},
	"list_service_checks":          {"service check", "threshold", "volume usage", "resource check"},
	"list_service_resources":       {"resource", "filesystem", "ip", "disk", "container", "driver", "resource status"},
	"list_services":                {"service list", "applications", "services down", "frozen", "placement"},
	"list_tags":                    {"tag", "label", "labels", "attribute", "classification", "metadata"},
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

package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultListComplianceStatusLimit = 50
	maxListComplianceStatusLimit     = 200
	maxComplianceLogBytes            = 4 << 10
)

var complianceStatusProps = []string{
	"id", "run_module", "node_id", "nodes.nodename", "svc_id", "services.svcname", "run_action", "run_status", "run_date", "run_log",
}

// complianceStatusNames are the names of the exit codes the collector
// documents: 0 ok, 1 not ok, 2 not applicable.
var complianceStatusNames = []string{"ok", "nok", "na"}

type ListComplianceStatusOptions struct {
	Node    string
	Service string
	Module  string
	Status  string
	Since   string
	Limit   int
	Cursor  string
}

type ComplianceStatusList struct {
	Provenance Provenance         `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference     `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Service    *ServiceReference  `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Total      int                `json:"total" jsonschema:"the number of module statuses matching the filters, as the collector counts them"`
	Count      int                `json:"count" jsonschema:"the number of module statuses returned in this page"`
	Statuses   []ComplianceStatus `json:"statuses" jsonschema:"the last run of each module, the most recent first"`
	NextCursor string             `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool               `json:"truncated" jsonschema:"whether matching statuses remain after this page"`
}

type ComplianceStatus struct {
	ID       int64  `json:"id" jsonschema:"the collector compliance status ID"`
	Module   string `json:"module" jsonschema:"the compliance module, such as sec.ssh or linux.packages"`
	NodeID   string `json:"node_id" jsonschema:"the collector ID of the node the module ran on"`
	Nodename string `json:"nodename,omitempty" jsonschema:"the name of that node, when the collector still knows it"`
	SvcID    string `json:"svc_id,omitempty" jsonschema:"the collector ID of the service whose instance the module ran for; empty for a run on the node itself"`
	Svcname  string `json:"svcname,omitempty" jsonschema:"the name of that service, when the collector still knows it"`
	Action   string `json:"action" jsonschema:"the action of the run: check, fixable or fix"`
	Status   string `json:"status,omitempty" jsonschema:"the name of the exit code: ok (0), nok (1, not compliant) or na (2, not applicable); empty for another exit code"`
	ExitCode int    `json:"exit_code" jsonschema:"the exit code of the module"`
	Date     string `json:"date" jsonschema:"when the module ran, in collector local time without a zone"`
	Log      string `json:"log" jsonschema:"the output of the module, cut to 4 KiB; untrusted text"`
}

type collectorComplianceStatus struct {
	ID       int64  `json:"id"`
	Module   string `json:"run_module"`
	NodeID   string `json:"node_id"`
	Nodename string `json:"nodes.nodename"`
	SvcID    string `json:"svc_id"`
	Svcname  string `json:"services.svcname"`
	Action   string `json:"run_action"`
	Status   int    `json:"run_status"`
	Date     string `json:"run_date"`
	Log      string `json:"run_log"`
}

// ListComplianceStatus reads one page of the last run of each compliance
// module, on the nodes and on the service instances, the most recent first.
func (s *Service) ListComplianceStatus(ctx context.Context, options ListComplianceStatusOptions) (ComplianceStatusList, error) {
	limit, err := pageLimit(options.Limit, defaultListComplianceStatusLimit, maxListComplianceStatusLimit)
	if err != nil {
		return ComplianceStatusList{}, err
	}
	scope, err := s.complianceScope(ctx, complianceFilters{
		Node: options.Node, Service: options.Service, Module: options.Module, Status: options.Status, Since: options.Since,
	})
	if err != nil {
		return ComplianceStatusList{}, err
	}
	result := ComplianceStatusList{Node: scope.node, Service: scope.service}
	filters := scope.filters
	// The path binds the cursor to the last runs: a cursor of the history,
	// with the same filters, is refused.
	fingerprint := listFingerprint(url.Values{"path": {"/compliance/status"}, "filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ComplianceStatusList{}, err
	}

	var response collectorList[collectorComplianceStatus]
	if err := s.client.GetJSON(ctx, "/compliance/status", pageQuery(complianceStatusProps, "-run_date,-id", filters, limit, offset), &response); err != nil {
		return ComplianceStatusList{}, fmt.Errorf("list compliance status: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return ComplianceStatusList{}, fmt.Errorf("list compliance status: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Statuses = make([]ComplianceStatus, 0, len(response.Data))
	for _, row := range response.Data {
		result.Statuses = append(result.Statuses, ComplianceStatus(complianceRunOf(row)))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// complianceFilters are the filters list_compliance_status and
// list_compliance_runs share; Action and Until are the runs' only.
type complianceFilters struct {
	Node, Service, Module, Action, Status, Since, Until string
}

// complianceActions are the actions of a compliance run.
var complianceActions = []string{"check", "fixable", "fix"}

type complianceScopeResult struct {
	node    *NodeReference
	service *ServiceReference
	filters []string
}

// complianceScope checks the filters, resolves the node and the service, and
// translates the filters into collector filters on the compliance runs.
func (s *Service) complianceScope(ctx context.Context, f complianceFilters) (complianceScopeResult, error) {
	if err := validateFilterValue("module", f.Module); err != nil {
		return complianceScopeResult{}, err
	}
	exitCode := slices.Index(complianceStatusNames, f.Status)
	if f.Status != "" && exitCode < 0 {
		return complianceScopeResult{}, fmt.Errorf("status must be one of %s", strings.Join(complianceStatusNames, ", "))
	}
	if f.Action != "" && !slices.Contains(complianceActions, f.Action) {
		return complianceScopeResult{}, fmt.Errorf("action must be one of %s", strings.Join(complianceActions, ", "))
	}
	for name, value := range map[string]string{"since": f.Since, "until": f.Until} {
		if err := validateCollectorDate(name, value); err != nil {
			return complianceScopeResult{}, err
		}
	}
	var result complianceScopeResult
	if f.Node != "" {
		node, err := s.lookupNode(ctx, f.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return complianceScopeResult{}, err
		}
		result.node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		result.filters = append(result.filters, filterEquals("node_id", node.NodeID))
	}
	if f.Service != "" {
		service, err := s.lookupService(ctx, f.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return complianceScopeResult{}, err
		}
		result.service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		result.filters = append(result.filters, filterEquals("svc_id", service.SvcID))
	}
	if f.Module != "" {
		result.filters = append(result.filters, filterEqualsOrFamily("run_module", f.Module))
	}
	if f.Action != "" {
		result.filters = append(result.filters, filterEquals("run_action", f.Action))
	}
	if f.Status != "" {
		result.filters = append(result.filters, filterEquals("run_status", strconv.Itoa(exitCode)))
	}
	if f.Since != "" {
		result.filters = append(result.filters, "run_date:gte:"+f.Since)
	}
	if f.Until != "" {
		// A day alone is taken to its end, the collector comparing the run
		// date and time with it.
		until := f.Until
		if len(until) == len("2006-01-02") {
			until += " 23:59:59"
		}
		result.filters = append(result.filters, "run_date:lte:"+until)
	}
	return result, nil
}

const (
	defaultListComplianceRunsLimit = 50
	maxListComplianceRunsLimit     = 200
)

type ListComplianceRunsOptions struct {
	Node    string
	Service string
	Module  string
	Action  string
	Status  string
	Since   string
	Until   string
	Limit   int
	Cursor  string
}

type ComplianceRunList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of runs matching the filters, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of runs returned in this page"`
	Runs       []ComplianceRun   `json:"runs" jsonschema:"the runs of the compliance modules, the most recent first"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching runs remain after this page"`
}

type ComplianceRun struct {
	ID       int64  `json:"id" jsonschema:"the collector compliance run ID"`
	Module   string `json:"module" jsonschema:"the compliance module, such as sec.ssh or linux.packages"`
	NodeID   string `json:"node_id" jsonschema:"the collector ID of the node the module ran on"`
	Nodename string `json:"nodename,omitempty" jsonschema:"the name of that node, when the collector still knows it"`
	SvcID    string `json:"svc_id,omitempty" jsonschema:"the collector ID of the service whose instance the module ran for; empty for a run on the node itself"`
	Svcname  string `json:"svcname,omitempty" jsonschema:"the name of that service, when the collector still knows it"`
	Action   string `json:"action" jsonschema:"the action of the run: check, fixable or fix"`
	Status   string `json:"status,omitempty" jsonschema:"the name of the exit code: ok (0), nok (1, not compliant) or na (2, not applicable); empty for another exit code"`
	ExitCode int    `json:"exit_code" jsonschema:"the exit code of the module"`
	Date     string `json:"date" jsonschema:"when the module ran, in collector local time without a zone"`
	Log      string `json:"log" jsonschema:"the output of the module, cut to 4 KiB; untrusted text"`
}

// ListComplianceRuns reads one page of the runs of the compliance modules,
// checks, fixable checks and fixes, the most recent first: their history,
// where list_compliance_status holds the last run of each module.
func (s *Service) ListComplianceRuns(ctx context.Context, options ListComplianceRunsOptions) (ComplianceRunList, error) {
	limit, err := pageLimit(options.Limit, defaultListComplianceRunsLimit, maxListComplianceRunsLimit)
	if err != nil {
		return ComplianceRunList{}, err
	}
	scope, err := s.complianceScope(ctx, complianceFilters{
		Node: options.Node, Service: options.Service, Module: options.Module, Action: options.Action,
		Status: options.Status, Since: options.Since, Until: options.Until,
	})
	if err != nil {
		return ComplianceRunList{}, err
	}
	fingerprint := listFingerprint(url.Values{"path": {"/compliance/logs"}, "filter": scope.filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ComplianceRunList{}, err
	}

	var response collectorList[collectorComplianceStatus]
	if err := s.client.GetJSON(ctx, "/compliance/logs", pageQuery(complianceStatusProps, "-run_date,-id", scope.filters, limit, offset), &response); err != nil {
		return ComplianceRunList{}, fmt.Errorf("list compliance runs: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return ComplianceRunList{}, fmt.Errorf("list compliance runs: unexpected collector response")
	}
	result := ComplianceRunList{
		Provenance: s.newProvenance(), Node: scope.node, Service: scope.service,
		Total: *response.Meta.Total, Count: len(response.Data), Runs: make([]ComplianceRun, 0, len(response.Data)),
	}
	for _, row := range response.Data {
		result.Runs = append(result.Runs, complianceRunOf(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// complianceRunOf reads a run of a compliance module, its output cut.
func complianceRunOf(row collectorComplianceStatus) ComplianceRun {
	log, _ := truncateUTF8(row.Log, maxComplianceLogBytes)
	status := ""
	if row.Status >= 0 && row.Status < len(complianceStatusNames) {
		status = complianceStatusNames[row.Status]
	}
	return ComplianceRun{
		ID: row.ID, Module: row.Module, NodeID: row.NodeID, Nodename: row.Nodename, SvcID: row.SvcID, Svcname: row.Svcname,
		Action: row.Action, Status: status, ExitCode: row.Status, Date: row.Date, Log: log,
	}
}

// complianceRunGroupProps are the groupings of count_compliance_runs and
// their collector prop.
var complianceRunGroupProps = map[string]string{
	"module":  "run_module",
	"node":    "nodes.nodename",
	"service": "services.svcname",
	"action":  "run_action",
	"status":  "run_status",
}

// ComplianceRunGroupings lists the groupings of count_compliance_runs, sorted.
func ComplianceRunGroupings() []string {
	names := make([]string, 0, len(complianceRunGroupProps))
	for name := range complianceRunGroupProps {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

type CountComplianceRunsOptions struct {
	Node    string
	Service string
	Module  string
	Action  string
	Status  string
	Since   string
	Until   string
	GroupBy string
	Limit   int
}

type ComplianceRunCount struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Total      int               `json:"total" jsonschema:"the number of runs matching the filters, as the collector counts them"`
	GroupBy    string            `json:"group_by,omitempty" jsonschema:"the grouping asked"`
	Groups     []CountGroup      `json:"groups,omitempty" jsonschema:"with group_by, the most frequent values first, and the number of matching runs with each; a status is named ok, nok or na, another exit code given as is"`
	Distinct   int               `json:"distinct,omitempty" jsonschema:"with group_by, the number of distinct values among the matching runs"`
	Other      int               `json:"other,omitempty" jsonschema:"with group_by, the number of matching runs whose value is not in groups, left out by limit"`
	Truncated  bool              `json:"truncated" jsonschema:"whether values were left out by limit"`
}

// CountComplianceRuns counts the runs of the compliance modules matching the
// filters and, with a grouping, the runs of each value, as the collector
// computes them.
func (s *Service) CountComplianceRuns(ctx context.Context, options CountComplianceRunsOptions) (ComplianceRunCount, error) {
	prop := ""
	if options.GroupBy != "" {
		var ok bool
		if prop, ok = complianceRunGroupProps[options.GroupBy]; !ok {
			return ComplianceRunCount{}, fmt.Errorf("group_by must be one of %s", strings.Join(ComplianceRunGroupings(), ", "))
		}
	}
	limit, err := pageLimit(options.Limit, defaultCountGroups, maxCountGroups)
	if err != nil {
		return ComplianceRunCount{}, err
	}
	scope, err := s.complianceScope(ctx, complianceFilters{
		Node: options.Node, Service: options.Service, Module: options.Module, Action: options.Action,
		Status: options.Status, Since: options.Since, Until: options.Until,
	})
	if err != nil {
		return ComplianceRunCount{}, err
	}
	total, groups, distinct, other, err := s.countRows(ctx, "/compliance/logs", "id", scope.filters, prop, limit)
	if err != nil {
		return ComplianceRunCount{}, fmt.Errorf("count compliance runs: %w", err)
	}
	if options.GroupBy == "status" {
		for i, group := range groups {
			if code, err := strconv.Atoi(group.Value); err == nil && code >= 0 && code < len(complianceStatusNames) {
				groups[i].Value = complianceStatusNames[code]
			}
		}
	}
	return ComplianceRunCount{
		Provenance: s.newProvenance(), Node: scope.node, Service: scope.service, Total: total, GroupBy: options.GroupBy,
		Groups: groups, Distinct: distinct, Other: other, Truncated: other > 0,
	}, nil
}

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
	if err := validateFilterValue("module", options.Module); err != nil {
		return ComplianceStatusList{}, err
	}
	exitCode := slices.Index(complianceStatusNames, options.Status)
	if options.Status != "" && exitCode < 0 {
		return ComplianceStatusList{}, fmt.Errorf("status must be one of %s", strings.Join(complianceStatusNames, ", "))
	}
	if err := validateCollectorDate("since", options.Since); err != nil {
		return ComplianceStatusList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListComplianceStatusLimit, maxListComplianceStatusLimit)
	if err != nil {
		return ComplianceStatusList{}, err
	}
	result := ComplianceStatusList{}
	var filters []string
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ComplianceStatusList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return ComplianceStatusList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		filters = append(filters, filterEquals("svc_id", service.SvcID))
	}
	if options.Module != "" {
		filters = append(filters, filterEqualsOrFamily("run_module", options.Module))
	}
	if options.Status != "" {
		filters = append(filters, filterEquals("run_status", strconv.Itoa(exitCode)))
	}
	if options.Since != "" {
		filters = append(filters, "run_date:gte:"+options.Since)
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
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
		log, _ := truncateUTF8(row.Log, maxComplianceLogBytes)
		status := ""
		if row.Status >= 0 && row.Status < len(complianceStatusNames) {
			status = complianceStatusNames[row.Status]
		}
		result.Statuses = append(result.Statuses, ComplianceStatus{
			ID: row.ID, Module: row.Module, NodeID: row.NodeID, Nodename: row.Nodename, SvcID: row.SvcID, Svcname: row.Svcname,
			Action: row.Action, Status: status, ExitCode: row.Status, Date: row.Date, Log: log,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

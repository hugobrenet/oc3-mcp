package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	// maxChecks bounds the checks read for one node or service: the
	// collector filters none of them, the MCP does.
	maxChecks = 2000
	// maxCheckNames bounds the instances read to name the services or nodes
	// of the checks.
	maxCheckNames = 1000
)

var checkProps = []string{
	"id", "node_id", "svc_id", "chk_type", "chk_instance", "chk_value", "chk_low", "chk_high", "chk_err",
	"chk_threshold_provider", "chk_created", "chk_updated",
}

type ListChecksOptions struct {
	Type            string
	OutOfBoundsOnly bool
}

type CheckList struct {
	Provenance      Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node            *NodeReference    `json:"node,omitempty" jsonschema:"the node whose checks are listed"`
	Service         *ServiceReference `json:"service,omitempty" jsonschema:"the service whose checks are listed"`
	Total           int               `json:"total" jsonschema:"the number of checks the collector holds for the node or service, before the type and out_of_bounds_only filters"`
	OutOfBoundsRead int               `json:"out_of_bounds_read" jsonschema:"the number of checks read that are out of their thresholds, before the type filter"`
	Count           int               `json:"count" jsonschema:"the number of checks returned"`
	Checks          []Check           `json:"checks" jsonschema:"the checks out of their thresholds first, then by type and instance"`
	Truncated       bool              `json:"truncated" jsonschema:"whether checks were left unread after 2000"`
}

type Check struct {
	ID              int64    `json:"id" jsonschema:"the collector check ID"`
	Type            string   `json:"type" jsonschema:"the check type, such as fs_u (filesystem usage), fs_i (inode usage), mpath (multipath paths), vg_u, eth or zpool"`
	Instance        string   `json:"instance" jsonschema:"what the check measures, such as a mount point, a multipath WWID or an interface link"`
	Value           *float64 `json:"value" jsonschema:"the last value the agent reported, in the unit of the check type; null when absent"`
	Low             *float64 `json:"low" jsonschema:"the low threshold; null when there is no low bound"`
	High            *float64 `json:"high" jsonschema:"the high threshold; null when there is no high bound"`
	OutOfBounds     bool     `json:"out_of_bounds" jsonschema:"whether the collector flags the value out of its thresholds"`
	ThresholdSource string   `json:"threshold_source,omitempty" jsonschema:"where the thresholds come from: settings of the check on the node, fset:<name> for a filterset, or defaults of the check type"`
	NodeID          string   `json:"node_id,omitempty" jsonschema:"the collector ID of the node the check runs on"`
	Nodename        string   `json:"nodename,omitempty" jsonschema:"the name of the node the check runs on, when known"`
	SvcID           string   `json:"svc_id,omitempty" jsonschema:"the collector ID of the service whose resource the check measures; empty for a node check"`
	Svcname         string   `json:"svcname,omitempty" jsonschema:"the name of that service, when known"`
	Created         string   `json:"created,omitempty" jsonschema:"when the collector first received the check, as the collector gives it"`
	Updated         string   `json:"updated,omitempty" jsonschema:"when the agent last reported the check, as the collector gives it"`
}

type collectorCheck struct {
	ID                   int64    `json:"id"`
	NodeID               string   `json:"node_id"`
	SvcID                string   `json:"svc_id"`
	ChkType              string   `json:"chk_type"`
	ChkInstance          string   `json:"chk_instance"`
	ChkValue             *float64 `json:"chk_value"`
	ChkLow               *float64 `json:"chk_low"`
	ChkHigh              *float64 `json:"chk_high"`
	ChkErr               int      `json:"chk_err"`
	ChkThresholdProvider string   `json:"chk_threshold_provider"`
	ChkCreated           string   `json:"chk_created"`
	ChkUpdated           string   `json:"chk_updated"`
}

// ListNodeChecks reads the checks of one node, those of the resources of its
// services included.
func (s *Service) ListNodeChecks(ctx context.Context, node string, options ListChecksOptions) (CheckList, error) {
	if err := validateFilterValue("type", options.Type); err != nil {
		return CheckList{}, err
	}
	found, err := s.lookupNode(ctx, node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return CheckList{}, err
	}
	reference := NodeReference{NodeID: found.NodeID, Nodename: found.Nodename, ClusterID: found.ClusterID}
	base := "/nodes/" + url.PathEscape(found.NodeID)
	checks, total, err := s.readChecks(ctx, base+"/checks")
	if err != nil {
		return CheckList{}, fmt.Errorf("list checks of node %s: %w", found.Nodename, err)
	}
	svcnames, err := s.readNames(ctx, base+"/services", "svc_id", "services.svcname")
	if err != nil {
		return CheckList{}, fmt.Errorf("list checks of node %s: name the services: %w", found.Nodename, err)
	}
	result := buildCheckList(checks, total, options, func(check *Check) {
		check.Nodename = found.Nodename
		check.Svcname = svcnames[check.SvcID]
	})
	result.Node = &reference
	result.Provenance = s.newProvenance()
	return result, nil
}

// ListServiceChecks reads the checks of the resources of one service, on all
// its nodes.
func (s *Service) ListServiceChecks(ctx context.Context, service string, options ListChecksOptions) (CheckList, error) {
	if err := validateFilterValue("type", options.Type); err != nil {
		return CheckList{}, err
	}
	found, err := s.lookupService(ctx, service, []string{"svc_id", "svcname", "cluster_id"})
	if err != nil {
		return CheckList{}, err
	}
	reference := ServiceReference{SvcID: found.SvcID, Svcname: found.Svcname, ClusterID: found.ClusterID}
	base := "/services/" + url.PathEscape(found.SvcID)
	checks, total, err := s.readChecks(ctx, base+"/checks")
	if err != nil {
		return CheckList{}, fmt.Errorf("list checks of service %s: %w", found.Svcname, err)
	}
	nodenames, err := s.readNames(ctx, base+"/nodes", "node_id", "nodes.nodename")
	if err != nil {
		return CheckList{}, fmt.Errorf("list checks of service %s: name the nodes: %w", found.Svcname, err)
	}
	result := buildCheckList(checks, total, options, func(check *Check) {
		check.Svcname = found.Svcname
		check.Nodename = nodenames[check.NodeID]
	})
	result.Service = &reference
	result.Provenance = s.newProvenance()
	return result, nil
}

// readChecks reads at most maxChecks checks of path, out of bounds first.
func (s *Service) readChecks(ctx context.Context, path string) ([]collectorCheck, int, error) {
	query := url.Values{}
	query.Set("props", strings.Join(checkProps, ","))
	query.Set("orderby", "-chk_err,chk_type,chk_instance,id")
	query.Set("limit", strconv.Itoa(maxChecks))
	var response collectorList[collectorCheck]
	if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
		return nil, 0, err
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > maxChecks {
		return nil, 0, fmt.Errorf("unexpected collector response")
	}
	return response.Data, *response.Meta.Total, nil
}

// readNames maps the IDs to the names of the instances of path, which list
// the nodes of a service or the services of a node.
func (s *Service) readNames(ctx context.Context, path string, idProp string, nameProp string) (map[string]string, error) {
	query := url.Values{}
	query.Set("props", idProp+","+nameProp)
	query.Set("limit", strconv.Itoa(maxCheckNames))
	query.Set("meta", "false")
	var response collectorList[map[string]any]
	if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
		return nil, err
	}
	names := make(map[string]string, len(response.Data))
	for _, row := range response.Data {
		id, _ := row[idProp].(string)
		name, _ := row[nameProp].(string)
		if id != "" && name != "" {
			names[id] = name
		}
	}
	return names, nil
}

func buildCheckList(checks []collectorCheck, total int, options ListChecksOptions, name func(*Check)) CheckList {
	result := CheckList{Total: total, Truncated: len(checks) < total, Checks: make([]Check, 0, len(checks))}
	for _, row := range checks {
		outOfBounds := row.ChkErr != 0
		if outOfBounds {
			result.OutOfBoundsRead++
		}
		if (options.Type != "" && row.ChkType != options.Type) || (options.OutOfBoundsOnly && !outOfBounds) {
			continue
		}
		check := Check{
			ID: row.ID, Type: row.ChkType, Instance: row.ChkInstance, Value: row.ChkValue, Low: row.ChkLow, High: row.ChkHigh,
			OutOfBounds: outOfBounds, ThresholdSource: row.ChkThresholdProvider, NodeID: row.NodeID, SvcID: row.SvcID,
			Created: row.ChkCreated, Updated: row.ChkUpdated,
		}
		name(&check)
		result.Checks = append(result.Checks, check)
	}
	result.Count = len(result.Checks)
	return result
}

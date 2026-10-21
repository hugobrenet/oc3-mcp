package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	// maxAckActions bounds the actions acknowledged in one call.
	maxAckActions = 50
)

// Outcomes of an acknowledgement.
const (
	AckDone    = "acknowledged"
	AckAlready = "already_acknowledged"
	AckSkipped = "not_failed"
	AckRefused = "refused"
	// AckNotAttempted is an action left when the call ended before.
	AckNotAttempted = "not_attempted"
)

type ServiceActionAcknowledgement struct {
	Provenance   Provenance              `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service      *ServiceReference       `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Node         *NodeReference          `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Selected     int                     `json:"selected" jsonschema:"the number of actions selected: given, or the failed actions not acknowledged the filters select"`
	Acknowledged int                     `json:"acknowledged" jsonschema:"the number of actions acknowledged by this call"`
	Unchanged    int                     `json:"unchanged" jsonschema:"the number of actions already acknowledged, or not failed"`
	Refused      int                     `json:"refused" jsonschema:"the number of actions not acknowledged: refused, or not attempted when the call ended before"`
	Actions      []ServiceActionAckCheck `json:"actions" jsonschema:"the outcome on each action, in the order given, or the most recent first with filters"`
}

type ServiceActionAckCheck struct {
	ID      int64  `json:"id" jsonschema:"the collector action ID"`
	Outcome string `json:"outcome" jsonschema:"acknowledged, already_acknowledged, not_failed for an action not in err, refused, or not_attempted when the call ended before"`
	Reason  string `json:"reason,omitempty" jsonschema:"with refused, the reason the collector gave"`
	// Action is the action as the collector holds it: read back for an ID
	// given, for every outcome but a refusal, or as the selection by filters
	// read it, before the acknowledgement.
	Action *ServiceAction `json:"action,omitempty" jsonschema:"the action as the collector holds it: with an ID given, read back with its acknowledgement; with filters, as selected, before the acknowledgement"`
}

// AckServiceActionsOptions select the failed actions to acknowledge: IDs, or
// filters, at least one, or AllUnacknowledged for every failed action not
// acknowledged the caller sees.
type AckServiceActionsOptions struct {
	IDs               []int64
	Service           string
	Node              string
	Action            string
	Since             string
	AllUnacknowledged bool
	Comment           string
}

// AckServiceActions acknowledges failed service actions, with a comment the
// collector records with the caller and the date. The collector also
// acknowledges the failed log lines of the session of the action on the same
// node, and updates the action errors alert of the instance. An
// acknowledgement is not withdrawn.
func (s *Service) AckServiceActions(ctx context.Context, options AckServiceActionsOptions) (ServiceActionAcknowledgement, error) {
	// The comment goes as the user gives it: the collector takes any, an
	// empty one included.
	comment := options.Comment
	byFilters := options.Service != "" || options.Node != "" || options.Action != "" || options.Since != ""
	switch {
	case len(options.IDs) > 0 && (byFilters || options.AllUnacknowledged):
		return ServiceActionAcknowledgement{}, fmt.Errorf("pass action IDs or filters, not both")
	case byFilters && options.AllUnacknowledged:
		return ServiceActionAcknowledgement{}, fmt.Errorf("all_unacknowledged goes without filters")
	case len(options.IDs) == 0 && !byFilters && !options.AllUnacknowledged:
		return ServiceActionAcknowledgement{}, fmt.Errorf("pass the action IDs, at least one filter, or all_unacknowledged")
	case len(options.IDs) > maxAckActions:
		return ServiceActionAcknowledgement{}, fmt.Errorf("pass at most %d action IDs; select more actions with filters", maxAckActions)
	}
	if len(options.IDs) == 0 {
		return s.ackSelectedServiceActions(ctx, options, comment)
	}
	result := ServiceActionAcknowledgement{Actions: []ServiceActionAckCheck{}}
	seen := map[int64]bool{}
	for _, id := range options.IDs {
		if id <= 0 {
			return ServiceActionAcknowledgement{}, fmt.Errorf("action IDs must be positive integers")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		check := s.ackServiceAction(ctx, id, comment, true)
		result.count(check)
		result.Actions = append(result.Actions, check)
	}
	result.Selected = len(result.Actions)
	result.Provenance = s.newProvenance()
	return result, nil
}

func (r *ServiceActionAcknowledgement) count(check ServiceActionAckCheck) {
	switch check.Outcome {
	case AckDone:
		r.Acknowledged++
	case AckAlready, AckSkipped:
		r.Unchanged++
	default:
		r.Refused++
	}
}

// ackSelectedServiceActions acknowledges the failed actions not acknowledged
// the filters select, a few at a time: the collector has no bulk route.
// The actions are not read back one by one: the result gives them as the
// selection read them.
func (s *Service) ackSelectedServiceActions(ctx context.Context, options AckServiceActionsOptions, comment string) (ServiceActionAcknowledgement, error) {
	if err := validateFilterValue("action", options.Action); err != nil {
		return ServiceActionAcknowledgement{}, err
	}
	if err := validateCollectorDate("since", options.Since); err != nil {
		return ServiceActionAcknowledgement{}, err
	}
	result := ServiceActionAcknowledgement{Actions: []ServiceActionAckCheck{}}
	// The rows of the actions, failed, and never acknowledged: the collector
	// takes a negation for a null too.
	filters := []string{filterEquals("log_type", "status"), filterEquals("status", "err"), "ack:!eq:1"}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return ServiceActionAcknowledgement{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		filters = append(filters, filterEquals("svc_id", service.SvcID))
	}
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ServiceActionAcknowledgement{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	if options.Action != "" {
		filters = append(filters, filterEquals("action", options.Action))
	}
	if options.Since != "" {
		filters = append(filters, "begin:gte:"+options.Since)
	}
	var selected []collectorServiceAction
	expected := -1
	for offset := 0; ; offset += selectionPage {
		var response collectorList[collectorServiceAction]
		if err := s.client.GetJSON(ctx, "/services_actions", pageQuery(serviceActionProps, "-begin,-id", filters, selectionPage, offset), &response); err != nil {
			return ServiceActionAcknowledgement{}, fmt.Errorf("select service actions: %w", err)
		}
		if response.Meta.Total == nil || len(response.Data) > selectionPage {
			return ServiceActionAcknowledgement{}, fmt.Errorf("select service actions: unexpected collector response")
		}
		total := *response.Meta.Total
		if expected < 0 {
			expected = total
		}
		if total > maxSelectionFiltered {
			return ServiceActionAcknowledgement{}, fmt.Errorf("the filters select %d failed actions not acknowledged, more than %d: nothing was acknowledged; narrow the filters, such as by node or since", total, maxSelectionFiltered)
		}
		// An acknowledgement made meanwhile changes the total: the actions
		// read are those to acknowledge.
		selected = append(selected, response.Data...)
		if len(response.Data) == 0 || len(selected) >= total {
			break
		}
	}
	checks := make([]ServiceActionAckCheck, len(selected))
	forEachSelected(ctx, len(selected), func(index int) {
		checks[index] = s.ackServiceAction(ctx, selected[index].ID, comment, false)
	}, func(index int) {
		checks[index] = ServiceActionAckCheck{ID: selected[index].ID, Outcome: AckNotAttempted}
	})
	for index, check := range checks {
		action := selected[index].summary()
		check.Action = &action
		result.count(check)
		result.Actions = append(result.Actions, check)
	}
	result.Selected = len(selected)
	result.Provenance = s.newProvenance()
	return result, nil
}

// ackServiceAction acknowledges one action, and with readBack reads it back.
func (s *Service) ackServiceAction(ctx context.Context, id int64, comment string, readBack bool) ServiceActionAckCheck {
	check := ServiceActionAckCheck{ID: id, Outcome: AckRefused}
	path := "/services_actions/" + strconv.FormatInt(id, 10)
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.PostJSON(ctx, path, map[string]any{"ack": 1, "acked_comment": comment}, &response); err != nil {
		check.Reason = err.Error()
		return check
	}
	if check.Outcome = ackOutcome(response.Info); check.Outcome == AckRefused {
		check.Reason = "unexpected collector response"
		return check
	}
	if !readBack {
		return check
	}
	// Read back, for the action as the collector holds it, with the
	// acknowledgement it recorded.
	var found collectorList[collectorServiceAction]
	if err := s.client.GetJSON(ctx, path, url.Values{"props": {strings.Join(serviceActionProps, ",")}}, &found); err == nil && len(found.Data) == 1 {
		action := found.Data[0].summary()
		check.Action = &action
	}
	return check
}

// ackOutcome reads the outcome of an acknowledgement in the info the
// collector answers it with, as it has no outcome code:
//
//	action N acknowledged
//	skip acknowledge of action N: already acknowledged
//	skip acknowledge of action N in status 'ok'
//
// The order of the cases matters: the second text holds the words of the two
// others, the third those of the first.
func ackOutcome(info string) string {
	info = strings.ToLower(info)
	switch {
	case strings.Contains(info, "already acknowledged"):
		return AckAlready
	case strings.Contains(info, "skip acknowledge"):
		return AckSkipped
	case strings.Contains(info, "acknowledged"):
		return AckDone
	}
	return AckRefused
}

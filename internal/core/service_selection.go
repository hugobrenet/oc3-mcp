package core

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const (
	// maxSelectionInstances bounds the instances a selection by instance
	// reads: several instances of a service select it once.
	maxSelectionInstances = 5000
	// selectionNodeChunk is the number of node IDs one read of the
	// instances names.
	selectionNodeChunk = 50
)

// ServiceSelection selects the services of a change: Services, exact names
// or IDs, or filters, at least one, never every service. The filters are
// those of list_services, and those of the instances of the services: Node,
// OnNodes and Instance keep the services having an instance on that node, on
// the nodes the node filters select, or in that state. A service is selected
// when one of its instances matches, whichever it is.
type ServiceSelection struct {
	Services []string
	Filters  ServiceFilters
	// Node keeps the services having an instance on this node.
	Node string
	// OnNodes keeps the services having an instance on the nodes these
	// filters select, those of list_nodes.
	OnNodes *NodeFilters
	// Instance keeps the services having an instance in this state.
	Instance InstanceState
}

// InstanceState is the state of an instance a selection of services keeps.
type InstanceState struct {
	Availability string
	Overall      string
	Frozen       bool
}

func (i InstanceState) collectorFilters() []string {
	var filters []string
	if i.Availability != "" {
		filters = append(filters, filterEquals("mon_availstatus", i.Availability))
	}
	if i.Overall != "" {
		filters = append(filters, filterEquals("mon_overallstatus", i.Overall))
	}
	if i.Frozen {
		filters = append(filters, "mon_frozen:ne:0")
	}
	return filters
}

// byInstance tells a selection reading the instances of the services.
func (selection ServiceSelection) byInstance() bool {
	return selection.Node != "" || selection.OnNodes != nil || selection.Instance != InstanceState{}
}

// ServiceChangeSummary is the outcome of a change on the selected services.
type ServiceChangeSummary struct {
	Node            *NodeReference                `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	OnNodes         *int                          `json:"on_nodes_selected,omitempty" jsonschema:"with on_nodes, the number of nodes the node filters selected"`
	OnNodesFilter   *NodeMembership               `json:"on_nodes_filter,omitempty" jsonschema:"with on_nodes and its tag, filterset, ruleset or moduleset filter, what it resolved to"`
	Filterset       *FiltersetReference           `json:"filterset,omitempty" jsonschema:"the filterset the filterset filter resolved to"`
	FilterTag       *TagReference                 `json:"filter_tag,omitempty" jsonschema:"the tag the tag filter resolved to"`
	FilterRuleset   *ComplianceRulesetReference   `json:"filter_ruleset,omitempty" jsonschema:"the ruleset the ruleset filter resolved to"`
	FilterModuleset *ComplianceModulesetReference `json:"filter_moduleset,omitempty" jsonschema:"the moduleset the moduleset filter resolved to"`
	Selected        int                           `json:"selected" jsonschema:"the number of services selected"`
	Changed         int                           `json:"changed" jsonschema:"the number of services changed"`
	Unchanged       int                           `json:"unchanged" jsonschema:"the number of services already as asked"`
	Refused         int                           `json:"refused" jsonschema:"the number of services not changed: refused by the collector, or not attempted when the call ended before"`
	Services        []ServiceChangeOutcome        `json:"services" jsonschema:"the outcome on each service, sorted by service name"`
}

type ServiceChangeOutcome struct {
	SvcID     string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname   string `json:"svcname" jsonschema:"the service name"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
	Outcome   string `json:"outcome" jsonschema:"for a tag, a ruleset or a moduleset: attached, already_attached, detached or already_detached; for an SLA: set, removed or unchanged when already as asked; refused, or not_attempted when the call ended before"`
	Reason    string `json:"reason,omitempty" jsonschema:"with refused, the reason the collector gave"`
	// SLA and Availability are those of the service after set_service_sla.
	SLA          *float64 `json:"sla,omitempty" jsonschema:"with set_service_sla, the availability target the collector holds after the change, in percent"`
	Availability *float64 `json:"availability_percent,omitempty" jsonschema:"with set_service_sla, the availability of the last 30 days the collector computes, in percent, to compare with the sla"`
}

// selectedService is a service of a selection, with its SLA and
// availability, which set_service_sla compares.
type selectedService struct {
	SvcID, Svcname, ClusterID string
	SLA, Availability         *float64
}

// selectionProps are the props a selection reads, named with prefix.
func selectionProps(prefix string) []string {
	return []string{"svc_id", prefix + "svcname", prefix + "cluster_id", prefix + "svc_sla", prefix + "svc_availability"}
}

// selectedRow is the service of a row a selection read, its props named
// with prefix.
func selectedRow(row map[string]any, prefix string) selectedService {
	return selectedService{
		SvcID: propText(row, "svc_id"), Svcname: propText(row, prefix+"svcname"), ClusterID: propText(row, prefix+"cluster_id"),
		SLA: propNumber(row, prefix+"svc_sla"), Availability: propNumber(row, prefix+"svc_availability"),
	}
}

// propNumber is a number of a collector row, nil when it has none.
func propNumber(row map[string]any, name string) *float64 {
	switch value := row[name].(type) {
	case float64:
		return &value
	case string:
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return &number
		}
	}
	return nil
}

// checkServiceSelection validates a selection without reading the
// collector.
func checkServiceSelection(selection ServiceSelection) error {
	byFilters := selection.byInstance() || !reflect.DeepEqual(selection.Filters, ServiceFilters{})
	switch {
	case len(selection.Services) > 0 && byFilters:
		return fmt.Errorf("pass services or filters, not both")
	case len(selection.Services) == 0 && !byFilters:
		return fmt.Errorf("pass the services, or at least one filter to select them")
	case len(selection.Services) > maxSelectionNodes:
		return fmt.Errorf("pass at most %d services; select more services with filters", maxSelectionNodes)
	case selection.Node != "" && selection.OnNodes != nil:
		return fmt.Errorf("pass node or on_nodes, not both")
	}
	if selection.OnNodes != nil {
		if _, err := checkSelection(NodeSelection{Filters: *selection.OnNodes}); err != nil {
			return fmt.Errorf("on_nodes: %w", err)
		}
	}
	for name, value := range map[string]string{"instance_availability": selection.Instance.Availability, "instance_overall": selection.Instance.Overall} {
		if err := validateFilterValue(name, value); err != nil {
			return err
		}
	}
	return selection.Filters.validate()
}

// changeSelectedServices reads the selected services, then changes each with
// change, a few at a time: one request per service, not atomic.
func (s *Service) changeSelectedServices(ctx context.Context, selection ServiceSelection, change func(context.Context, selectedService) ServiceChangeOutcome) (ServiceChangeSummary, error) {
	var summary ServiceChangeSummary
	var services []selectedService
	var err error
	switch {
	case len(selection.Services) > 0:
		services, err = s.resolveServices(ctx, selection.Services)
	case selection.byInstance():
		services, err = s.selectServicesByInstance(ctx, selection, &summary)
	default:
		path, members, pathErr := s.serviceMemberPath(ctx, selection.Filters)
		if pathErr != nil {
			return ServiceChangeSummary{}, pathErr
		}
		summary.FilterTag, summary.Filterset = members.Tag, members.Filterset
		summary.FilterRuleset, summary.FilterModuleset = members.Ruleset, members.Moduleset
		services, err = s.selectServices(ctx, path, "", selection.Filters.collectorFilters(""))
	}
	if err != nil {
		return ServiceChangeSummary{}, err
	}

	outcomes := make([]ServiceChangeOutcome, len(services))
	forEachSelected(ctx, len(services), func(index int) {
		outcomes[index] = change(ctx, services[index])
	}, func(index int) {
		outcomes[index] = ServiceChangeOutcome{SvcID: services[index].SvcID, Svcname: services[index].Svcname, ClusterID: services[index].ClusterID, Outcome: ChangeNotAttempted}
	})
	sort.SliceStable(outcomes, func(i, j int) bool {
		if outcomes[i].Svcname != outcomes[j].Svcname {
			return outcomes[i].Svcname < outcomes[j].Svcname
		}
		return outcomes[i].SvcID < outcomes[j].SvcID
	})
	for _, outcome := range outcomes {
		switch outcome.Outcome {
		case ChangeAttached, ChangeDetached, ChangeSet, ChangeRemoved:
			summary.Changed++
		case ChangeAlreadyAttached, ChangeAlreadyDetached, ChangeUnchanged:
			summary.Unchanged++
		case ChangeRefused, ChangeNotAttempted:
			summary.Refused++
		}
	}
	summary.Selected, summary.Services = len(services), outcomes
	return summary, nil
}

// selectServicesByInstance reads the services having an instance on the
// node, on the nodes of the node filters or in the instance state of
// selection, with the filters of the services. The tag, filterset, ruleset
// and moduleset filters, read from their own lists, keep the services in
// both.
func (s *Service) selectServicesByInstance(ctx context.Context, selection ServiceSelection, summary *ServiceChangeSummary) ([]selectedService, error) {
	base := append(selection.Instance.collectorFilters(), selection.Filters.collectorFilters("services.")...)
	var nodeIDs []string
	switch {
	case selection.Node != "":
		node, err := s.lookupNode(ctx, selection.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return nil, err
		}
		summary.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		nodeIDs = []string{node.NodeID}
	case selection.OnNodes != nil:
		filters, err := checkSelection(NodeSelection{Filters: *selection.OnNodes})
		if err != nil {
			return nil, fmt.Errorf("on_nodes: %w", err)
		}
		path, membership, err := s.nodeMemberPath(ctx, *selection.OnNodes)
		if err != nil {
			return nil, fmt.Errorf("on_nodes: %w", err)
		}
		if membership != (NodeMembership{}) {
			summary.OnNodesFilter = &membership
		}
		nodes, err := s.selectNodes(ctx, path, filters)
		if err != nil {
			return nil, fmt.Errorf("on_nodes: %w", err)
		}
		count := len(nodes)
		summary.OnNodes = &count
		if count == 0 {
			return nil, nil
		}
		for _, node := range nodes {
			nodeIDs = append(nodeIDs, node.NodeID)
		}
	}
	var queries [][]string
	if nodeIDs == nil {
		queries = [][]string{base}
	}
	for start := 0; start < len(nodeIDs); start += selectionNodeChunk {
		chunk := nodeIDs[start:min(start+selectionNodeChunk, len(nodeIDs))]
		filter := "node_id:in:" + strings.Join(chunk, ",")
		if len(chunk) == 1 {
			filter = filterEquals("node_id", chunk[0])
		}
		queries = append(queries, append([]string{filter}, base...))
	}
	byID := map[string]selectedService{}
	read := 0
	for _, filters := range queries {
		instances, err := s.selectInstances(ctx, filters, maxSelectionInstances-read)
		if err != nil {
			return nil, err
		}
		read += len(instances)
		for _, service := range instances {
			byID[service.SvcID] = service
		}
	}
	if selection.Filters.member() {
		path, members, err := s.serviceMemberPath(ctx, selection.Filters)
		if err != nil {
			return nil, err
		}
		summary.FilterTag, summary.Filterset = members.Tag, members.Filterset
		summary.FilterRuleset, summary.FilterModuleset = members.Ruleset, members.Moduleset
		kept, err := s.selectServices(ctx, path, "", selection.Filters.collectorFilters(""))
		if err != nil {
			return nil, err
		}
		in := map[string]bool{}
		for _, service := range kept {
			in[service.SvcID] = true
		}
		for id := range byID {
			if !in[id] {
				delete(byID, id)
			}
		}
	}
	if len(byID) > maxSelectionFiltered {
		return nil, fmt.Errorf("the filters select %d services, more than %d: nothing was changed; narrow the filters", len(byID), maxSelectionFiltered)
	}
	services := make([]selectedService, 0, len(byID))
	for _, service := range byID {
		services = append(services, service)
	}
	return services, nil
}

// selectInstances reads the services of the instances the filters select,
// at most limit instances, and refuses more, or a selection that changed
// while it was read.
func (s *Service) selectInstances(ctx context.Context, filters []string, limit int) ([]selectedService, error) {
	props := append(selectionProps("services."), "node_id")
	var services []selectedService
	expected := -1
	for offset := 0; ; offset += selectionPage {
		var response collectorList[map[string]any]
		if err := s.client.GetJSON(ctx, "/services_instances", pageQuery(props, "services.svcname,svc_id,node_id", filters, selectionPage, offset), &response); err != nil {
			return nil, fmt.Errorf("select service instances: %w", err)
		}
		if response.Meta.Total == nil || len(response.Data) > selectionPage {
			return nil, fmt.Errorf("select service instances: unexpected collector response")
		}
		total := *response.Meta.Total
		if expected < 0 {
			expected = total
		}
		if total != expected {
			return nil, fmt.Errorf("the selection changed while it was read, %d instances then %d: nothing was changed", expected, total)
		}
		if total > limit {
			return nil, fmt.Errorf("the filters select more than %d instances: nothing was changed; narrow the filters", maxSelectionInstances)
		}
		for _, row := range response.Data {
			services = append(services, selectedRow(row, "services."))
		}
		if len(response.Data) == 0 || len(services) >= total {
			break
		}
	}
	return services, nil
}

// selectServices reads the services the filters select on path, whose
// service props carry prefix, and refuses a selection too large, or that
// changed while it was read.
func (s *Service) selectServices(ctx context.Context, path, prefix string, filters []string) ([]selectedService, error) {
	props := selectionProps(prefix)
	var services []selectedService
	expected := -1
	for offset := 0; ; offset += selectionPage {
		var response collectorList[map[string]any]
		if err := s.client.GetJSON(ctx, path, pageQuery(props, prefix+"svcname,svc_id", filters, selectionPage, offset), &response); err != nil {
			return nil, fmt.Errorf("select services: %w", err)
		}
		if response.Meta.Total == nil || len(response.Data) > selectionPage {
			return nil, fmt.Errorf("select services: unexpected collector response")
		}
		total := *response.Meta.Total
		if expected < 0 {
			expected = total
		}
		if total != expected {
			return nil, fmt.Errorf("the selection changed while it was read, %d services then %d: nothing was changed", expected, total)
		}
		if total > maxSelectionFiltered {
			return nil, fmt.Errorf("the filters select %d services, more than %d: nothing was changed; narrow the filters", total, maxSelectionFiltered)
		}
		for _, row := range response.Data {
			services = append(services, selectedRow(row, prefix))
		}
		if len(response.Data) == 0 || len(services) >= total {
			break
		}
	}
	if len(services) != expected {
		return nil, fmt.Errorf("the selection changed while it was read, %d services instead of %d: nothing was changed", len(services), expected)
	}
	return services, nil
}

// resolveServices reads the named services, exact names or service IDs, up
// to the total, and refuses the whole call when one is unknown or a name
// several clusters hold, before anything is changed.
func (s *Service) resolveServices(ctx context.Context, names []string) ([]selectedService, error) {
	var ids, svcnames []string
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, ", ") {
			return nil, fmt.Errorf("services must be exact service names or service IDs, without commas or spaces")
		}
		if err := validateFilterValue("service", name); err != nil {
			return nil, err
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		if collectorUUIDPattern.MatchString(name) {
			ids = append(ids, name)
		} else {
			svcnames = append(svcnames, name)
		}
	}
	var rows []selectedService
	for prop, values := range map[string][]string{"svc_id": ids, "svcname": svcnames} {
		if len(values) == 0 {
			continue
		}
		filter := []string{prop + ":in:" + strings.Join(values, ",")}
		read := 0
		for offset := 0; ; offset += selectionPage {
			var response collectorList[map[string]any]
			if err := s.client.GetJSON(ctx, "/services", pageQuery(selectionProps(""), "svcname,svc_id", filter, selectionPage, offset), &response); err != nil {
				return nil, fmt.Errorf("get services: %w", err)
			}
			if response.Meta.Total == nil || *response.Meta.Total > maxSelectionFiltered || len(response.Data) > selectionPage {
				return nil, fmt.Errorf("get services: unexpected collector response")
			}
			for _, row := range response.Data {
				rows = append(rows, selectedRow(row, ""))
			}
			read += len(response.Data)
			if len(response.Data) == 0 || read >= *response.Meta.Total {
				if read != *response.Meta.Total {
					return nil, fmt.Errorf("get services: the services changed while they were read: nothing was changed")
				}
				break
			}
		}
	}
	var services []selectedService
	var missing, ambiguous []string
	added := map[string]bool{}
	for _, name := range append(ids, svcnames...) {
		var matches []selectedService
		for _, row := range rows {
			if strings.EqualFold(row.SvcID, name) || strings.EqualFold(row.Svcname, name) {
				matches = append(matches, row)
			}
		}
		switch len(matches) {
		case 0:
			missing = append(missing, name)
		case 1:
			if !added[matches[0].SvcID] {
				added[matches[0].SvcID] = true
				services = append(services, matches[0])
			}
		default:
			matched := make([]string, 0, len(matches))
			for _, match := range matches {
				matched = append(matched, match.SvcID+clusterSuffix(match.ClusterID))
			}
			ambiguous = append(ambiguous, name+" ("+strings.Join(matched, ", ")+")")
		}
	}
	if len(missing) > 0 || len(ambiguous) > 0 {
		var problems []string
		if len(missing) > 0 {
			problems = append(problems, "not found or not visible: "+strings.Join(missing, ", ")+findObjectsHint)
		}
		if len(ambiguous) > 0 {
			problems = append(problems, "names several services hold, pass a service ID: "+strings.Join(ambiguous, "; "))
		}
		return nil, fmt.Errorf("nothing was changed; %s", strings.Join(problems, "; "))
	}
	return services, nil
}

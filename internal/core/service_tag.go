package core

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

// ServiceSelection selects the services of a change: Services, exact names
// or IDs, or Filters and Node, at least one, never every service.
type ServiceSelection struct {
	Services []string
	Filters  ServiceFilters
	// Node keeps the services having an instance on this node.
	Node string
}

// ServiceChangeSummary is the outcome of a change on the selected services.
type ServiceChangeSummary struct {
	Node      *NodeReference         `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Filterset *FiltersetReference    `json:"filterset,omitempty" jsonschema:"the filterset the filterset filter resolved to"`
	FilterTag *TagReference          `json:"filter_tag,omitempty" jsonschema:"the tag the tag filter resolved to"`
	Selected  int                    `json:"selected" jsonschema:"the number of services selected"`
	Changed   int                    `json:"changed" jsonschema:"the number of services changed"`
	Unchanged int                    `json:"unchanged" jsonschema:"the number of services already as asked"`
	Refused   int                    `json:"refused" jsonschema:"the number of services the collector refused the change for"`
	Services  []ServiceChangeOutcome `json:"services" jsonschema:"the outcome on each service, sorted by service name"`
}

type ServiceChangeOutcome struct {
	SvcID     string `json:"svc_id" jsonschema:"the collector service ID"`
	Svcname   string `json:"svcname" jsonschema:"the service name"`
	ClusterID string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the service"`
	Outcome   string `json:"outcome" jsonschema:"attached, already_attached, detached, already_detached, refused, or not_attempted when the call ended before"`
	Reason    string `json:"reason,omitempty" jsonschema:"with refused, the reason the collector gave"`
}

// selectedService is a service of a selection.
type selectedService struct {
	SvcID, Svcname, ClusterID string
}

type ServiceTagOptions struct {
	Tag string
	ServiceSelection
	// AttachData is the data of the attachment, such as the fields of a
	// form, when attaching.
	AttachData *string
}

type ServiceTagChange struct {
	Provenance Provenance   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Tag        TagReference `json:"tag" jsonschema:"the tag"`
	ServiceChangeSummary
}

// AttachServiceTag attaches a tag to the selected services, one request per
// service: the collector has no bulk route, and a change is not atomic.
func (s *Service) AttachServiceTag(ctx context.Context, options ServiceTagOptions) (ServiceTagChange, error) {
	if options.AttachData != nil {
		if err := validateTagAttachData(*options.AttachData); err != nil {
			return ServiceTagChange{}, err
		}
	}
	return s.changeServiceTag(ctx, options, true)
}

// DetachServiceTag detaches a tag from the selected services.
func (s *Service) DetachServiceTag(ctx context.Context, options ServiceTagOptions) (ServiceTagChange, error) {
	if options.AttachData != nil {
		return ServiceTagChange{}, fmt.Errorf("attach_data is for an attachment only")
	}
	return s.changeServiceTag(ctx, options, false)
}

func (s *Service) changeServiceTag(ctx context.Context, options ServiceTagOptions, attach bool) (ServiceTagChange, error) {
	if options.Tag == "" {
		return ServiceTagChange{}, fmt.Errorf("tag must be one exact tag name or tag ID")
	}
	if err := checkServiceSelection(options.ServiceSelection); err != nil {
		return ServiceTagChange{}, err
	}
	tag, err := s.lookupTag(ctx, options.Tag)
	if err != nil {
		return ServiceTagChange{}, err
	}
	summary, err := s.changeSelectedServices(ctx, options.ServiceSelection, func(ctx context.Context, service selectedService) ServiceChangeOutcome {
		outcome := ServiceChangeOutcome{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		path := "/tags/" + url.PathEscape(tag.TagID) + "/services/" + url.PathEscape(service.SvcID)
		var response struct {
			Info string `json:"info"`
		}
		var err error
		if attach {
			body := map[string]any{}
			if options.AttachData != nil {
				body["tag_attach_data"] = *options.AttachData
			}
			err = s.client.PostJSON(ctx, path, body, &response)
		} else {
			err = s.client.DeleteJSON(ctx, path, &response)
		}
		outcome.Outcome, outcome.Reason = tagOutcome(err, response.Info, attach)
		return outcome
	})
	if err != nil {
		return ServiceTagChange{}, err
	}
	return ServiceTagChange{Provenance: s.newProvenance(), Tag: tag, ServiceChangeSummary: summary}, nil
}

// checkServiceSelection validates a selection without reading the
// collector.
func checkServiceSelection(selection ServiceSelection) error {
	byFilters := selection.Node != "" || !reflect.DeepEqual(selection.Filters, ServiceFilters{})
	switch {
	case len(selection.Services) > 0 && byFilters:
		return fmt.Errorf("pass services or filters, not both")
	case len(selection.Services) == 0 && !byFilters:
		return fmt.Errorf("pass the services, or at least one filter to select them")
	case len(selection.Services) > maxSelectionNodes:
		return fmt.Errorf("pass at most %d services; select more services with filters", maxSelectionNodes)
	case selection.Node != "" && (selection.Filters.Tag != "" || selection.Filters.Filterset != ""):
		return fmt.Errorf("pass node, tag or filterset, not two of them")
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
	case selection.Node != "":
		node, lookupErr := s.lookupNode(ctx, selection.Node, []string{"node_id", "nodename", "cluster_id"})
		if lookupErr != nil {
			return ServiceChangeSummary{}, lookupErr
		}
		summary.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters := append([]string{filterEquals("node_id", node.NodeID)}, selection.Filters.collectorFilters("services.")...)
		services, err = s.selectServices(ctx, "/services_instances", "services.", filters)
	default:
		path, filterTag, filterset, pathErr := s.memberPath(ctx, selection.Filters.Tag, selection.Filters.Filterset, "services")
		if pathErr != nil {
			return ServiceChangeSummary{}, pathErr
		}
		summary.FilterTag, summary.Filterset = filterTag, filterset
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
		case ChangeAttached, ChangeDetached:
			summary.Changed++
		case ChangeAlreadyAttached, ChangeAlreadyDetached:
			summary.Unchanged++
		case ChangeRefused:
			summary.Refused++
		}
	}
	summary.Selected, summary.Services = len(services), outcomes
	return summary, nil
}

// selectServices reads the services the filters select on path, whose
// service props carry prefix, and refuses a selection too large, or that
// changed while it was read.
func (s *Service) selectServices(ctx context.Context, path, prefix string, filters []string) ([]selectedService, error) {
	props := []string{"svc_id", prefix + "svcname", prefix + "cluster_id"}
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
			services = append(services, selectedService{SvcID: propText(row, "svc_id"), Svcname: propText(row, prefix+"svcname"), ClusterID: propText(row, prefix+"cluster_id")})
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
			if err := s.client.GetJSON(ctx, "/services", pageQuery([]string{"svc_id", "svcname", "cluster_id"}, "svcname,svc_id", filter, selectionPage, offset), &response); err != nil {
				return nil, fmt.Errorf("get services: %w", err)
			}
			if response.Meta.Total == nil || *response.Meta.Total > maxSelectionFiltered || len(response.Data) > selectionPage {
				return nil, fmt.Errorf("get services: unexpected collector response")
			}
			for _, row := range response.Data {
				rows = append(rows, selectedService{SvcID: propText(row, "svc_id"), Svcname: propText(row, "svcname"), ClusterID: propText(row, "cluster_id")})
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

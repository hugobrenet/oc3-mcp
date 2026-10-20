package core

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// ServiceSLAOptions set or remove the availability target of the selected
// services: SLA, a percent between 0 and 100, or Remove.
type ServiceSLAOptions struct {
	SLA    *float64
	Remove bool
	ServiceSelection
}

type ServiceSLAChange struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	SLA        *float64   `json:"sla,omitempty" jsonschema:"the availability target set, in percent, rounded to three decimals as the collector stores it; absent when removed"`
	Removed    bool       `json:"removed,omitempty" jsonschema:"true when the availability target was removed"`
	ServiceChangeSummary
}

// SetServiceSLA sets or removes the availability target of the selected
// services, which the collector shows beside the availability it computes
// over 30 days. The agents never send it: the value stays. One request per
// service whose target differs: the collector has no bulk route, and a
// change is not atomic. A service already as asked is left as it is, with
// no request and no log entry.
func (s *Service) SetServiceSLA(ctx context.Context, options ServiceSLAOptions) (ServiceSLAChange, error) {
	switch {
	case options.SLA != nil && options.Remove:
		return ServiceSLAChange{}, fmt.Errorf("pass sla or remove, not both")
	case options.SLA == nil && !options.Remove:
		return ServiceSLAChange{}, fmt.Errorf("pass sla, a percent between 0 and 100, or remove")
	case options.SLA != nil && (math.IsNaN(*options.SLA) || *options.SLA < 0 || *options.SLA > 100):
		return ServiceSLAChange{}, fmt.Errorf("sla must be a percent between 0 and 100")
	}
	if err := checkServiceSelection(options.ServiceSelection); err != nil {
		return ServiceSLAChange{}, err
	}
	// The collector stores three decimals, and rounds the others away: the
	// target is rounded first, so that the value asked is the value held.
	// It takes the target as text, and the empty text removes it.
	var target *float64
	value, outcome := "", ChangeRemoved
	if options.SLA != nil {
		rounded := math.Round(*options.SLA*1000) / 1000
		target = &rounded
		value, outcome = strconv.FormatFloat(rounded, 'f', -1, 64), ChangeSet
	}
	summary, err := s.changeSelectedServices(ctx, options.ServiceSelection, func(ctx context.Context, service selectedService) ServiceChangeOutcome {
		result := ServiceChangeOutcome{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID, Outcome: outcome, Availability: service.Availability}
		if (target == nil && service.SLA == nil) || (target != nil && service.SLA != nil && *service.SLA == *target) {
			result.Outcome, result.SLA = ChangeUnchanged, service.SLA
			return result
		}
		var response struct {
			Data []struct {
				SLA          *float64 `json:"svc_sla"`
				Availability *float64 `json:"svc_availability"`
			} `json:"data"`
		}
		if err := s.client.PostJSON(ctx, "/services/"+url.PathEscape(service.SvcID), map[string]any{"svc_sla": value}, &response); err != nil {
			result.Outcome, result.Reason, result.SLA = ChangeRefused, err.Error(), service.SLA
			return result
		}
		// The collector answers with the service as it holds it.
		if len(response.Data) == 1 {
			result.SLA, result.Availability = response.Data[0].SLA, response.Data[0].Availability
		}
		return result
	})
	if err != nil {
		return ServiceSLAChange{}, err
	}
	return ServiceSLAChange{Provenance: s.newProvenance(), SLA: target, Removed: options.Remove, ServiceChangeSummary: summary}, nil
}

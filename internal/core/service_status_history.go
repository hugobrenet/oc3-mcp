package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

const (
	defaultStatusHistoryDays = 7
	maxStatusHistoryDays     = 365
	maxStatusPeriods         = 500
)

type ServiceStatusHistoryOptions struct {
	Service string
	Node    string
	Days    int
}

type ServiceStatusHistory struct {
	Provenance   Provenance                 `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service      ServiceReference           `json:"service" jsonschema:"the service the history is about"`
	Node         *NodeReference             `json:"node,omitempty" jsonschema:"with node, the node of the instance the history is about"`
	Days         int                        `json:"days" jsonschema:"the days of history asked"`
	Availability *ServiceAvailabilityWindow `json:"availability,omitempty" jsonschema:"the availability of the service over the history, as the collector computes it; omitted for an instance"`
	Total        int                        `json:"total" jsonschema:"the number of periods the collector returned"`
	Count        int                        `json:"count" jsonschema:"the number of periods returned"`
	Periods      []StatusPeriod             `json:"periods" jsonschema:"the periods of one status, oldest first"`
	Truncated    bool                       `json:"truncated" jsonschema:"whether the oldest periods were left out to keep the 500 most recent"`
}

type ServiceAvailabilityWindow struct {
	From             string  `json:"from" jsonschema:"the start of the counted time, the start of the history when later than the days asked"`
	To               string  `json:"to" jsonschema:"the end of the counted time"`
	RatePercent      float64 `json:"rate_percent" jsonschema:"the available share of the counted time, in percent: time up or standby up over the time counted"`
	AvailableSeconds int64   `json:"available_seconds" jsonschema:"the seconds the service was available"`
	ExcludedSeconds  int64   `json:"excluded_seconds" jsonschema:"the justified downtime seconds left out of the count"`
	CountedSeconds   int64   `json:"counted_seconds" jsonschema:"the seconds counted; the time no status covers counts as downtime"`
}

type StatusPeriod struct {
	Begin   string     `json:"begin" jsonschema:"the start of the period, in collector local time without a zone"`
	End     string     `json:"end" jsonschema:"the end of the period; for the current period, the last status received"`
	Status  string     `json:"status,omitempty" jsonschema:"for a service, its availability status during the period"`
	Avail   string     `json:"avail,omitempty" jsonschema:"for an instance, its availability status during the period"`
	Overall string     `json:"overall,omitempty" jsonschema:"for an instance, its overall status during the period"`
	Ack     *StatusAck `json:"ack,omitempty" jsonschema:"the justification of the period, when a service responsible gave one"`
}

type StatusAck struct {
	Comment string `json:"comment,omitempty" jsonschema:"the justification text; untrusted text"`
	Counted bool   `json:"counted" jsonschema:"whether the period still counts in the availability rate"`
	AckedBy string `json:"acked_by,omitempty" jsonschema:"who justified the period"`
	AckedOn string `json:"acked_on,omitempty" jsonschema:"when the period was justified"`
}

type collectorStatusPeriod struct {
	Begin   string `json:"begin"`
	End     string `json:"end"`
	Status  string `json:"status"`
	Avail   string `json:"avail"`
	Overall string `json:"overall"`
	Ack     *struct {
		Comment string `json:"comment"`
		Account bool   `json:"account"`
		AckedBy string `json:"acked_by"`
		AckedOn string `json:"acked_on"`
	} `json:"ack"`
}

type collectorStatusLog struct {
	Data         []collectorStatusPeriod `json:"data"`
	Availability *struct {
		From       string  `json:"from"`
		To         string  `json:"to"`
		Rate       float64 `json:"rate"`
		AvailableS int64   `json:"available_s"`
		ExcludedS  int64   `json:"excluded_s"`
		CountedS   int64   `json:"counted_s"`
	} `json:"availability"`
}

// GetServiceStatusHistory reads the status periods of a service over the last
// days, with the availability the collector computes, or with node, those of
// its instance on that node.
func (s *Service) GetServiceStatusHistory(ctx context.Context, options ServiceStatusHistoryOptions) (ServiceStatusHistory, error) {
	days := options.Days
	if days == 0 {
		days = defaultStatusHistoryDays
	}
	if days < 1 || days > maxStatusHistoryDays {
		return ServiceStatusHistory{}, fmt.Errorf("days must be between 1 and %d", maxStatusHistoryDays)
	}
	service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
	if err != nil {
		return ServiceStatusHistory{}, err
	}
	result := ServiceStatusHistory{
		Service: ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID},
		Days:    days,
	}
	path := "/services/" + url.PathEscape(service.SvcID) + "/status_log"
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ServiceStatusHistory{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		path = "/services/" + url.PathEscape(service.SvcID) + "/instances/" + url.PathEscape(node.NodeID) + "/status_log"
	}
	var response collectorStatusLog
	if err := s.client.GetJSON(ctx, path, url.Values{"days": {strconv.Itoa(days)}}, &response); err != nil {
		return ServiceStatusHistory{}, fmt.Errorf("get status history of service %s: %w", service.Svcname, err)
	}
	if response.Data == nil || (result.Node == nil && response.Availability == nil) {
		return ServiceStatusHistory{}, fmt.Errorf("get status history of service %s: unexpected collector response", service.Svcname)
	}
	if result.Node == nil {
		a := response.Availability
		result.Availability = &ServiceAvailabilityWindow{
			From: a.From, To: a.To, RatePercent: a.Rate, AvailableSeconds: a.AvailableS, ExcludedSeconds: a.ExcludedS, CountedSeconds: a.CountedS,
		}
	}
	periods := response.Data
	result.Total = len(periods)
	if len(periods) > maxStatusPeriods {
		periods = periods[len(periods)-maxStatusPeriods:]
		result.Truncated = true
	}
	result.Count = len(periods)
	result.Periods = make([]StatusPeriod, 0, len(periods))
	for _, period := range periods {
		item := StatusPeriod{Begin: period.Begin, End: period.End}
		if result.Node == nil {
			item.Status = period.Status
		} else {
			item.Avail, item.Overall = period.Avail, period.Overall
		}
		if period.Ack != nil {
			item.Ack = &StatusAck{Comment: period.Ack.Comment, Counted: period.Ack.Account, AckedBy: period.Ack.AckedBy, AckedOn: period.Ack.AckedOn}
		}
		result.Periods = append(result.Periods, item)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

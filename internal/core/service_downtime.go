package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// Outcomes of a justification change.
const (
	DowntimeJustified    = "justified"
	DowntimeReplaced     = "replaced"
	DowntimeRemoved      = "removed"
	DowntimeNotJustified = "not_justified"
)

// collectorTimeLayout is the layout of the collector dates, in its local
// time without a zone.
const collectorTimeLayout = "2006-01-02 15:04:05"

// periodBeginPattern is the begin of a period, as the status history gives
// it: the collector takes the exact bounds only.
var periodBeginPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

// currentPeriodWindow is how recent the end of the last period is when the
// agents still send statuses, as the collector counts it: the end of a
// period still going on moves with each of them. A last period older than
// that is justified, and still grows if the agents resume with the same
// status: its justification keeps its bounds, and no longer shows on it.
const currentPeriodWindow = 15 * time.Minute

type JustifyServiceDowntimeOptions struct {
	Service string
	Begin   string
	// End, when given, names the period by both its bounds, sent as is.
	End     string
	Comment string
	// Counted keeps the period in the availability rate.
	Counted bool
}

type UnjustifyServiceDowntimeOptions struct {
	Service string
	Begin   string
	// End, when given, names the period by both its bounds, sent as is.
	End string
}

type ServiceDowntimeChange struct {
	Provenance   Provenance                 `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service      ServiceReference           `json:"service" jsonschema:"the service"`
	Outcome      string                     `json:"outcome" jsonschema:"justified, replaced when a justification was there, removed, or not_justified when the period had none"`
	Previous     *StatusAck                 `json:"previous,omitempty" jsonschema:"the justification the period had before, replaced or removed"`
	Period       StatusPeriod               `json:"period" jsonschema:"the period as the collector holds it after the change, with its justification; without it when the read back failed"`
	Availability *ServiceAvailabilityWindow `json:"availability,omitempty" jsonschema:"the availability of the service after the change, over the days read, as the collector computes it; omitted when the read back failed"`
	// ReadBackFailed tells a change made, and not read back: the result
	// does not show it.
	ReadBackFailed bool   `json:"read_back_failed,omitempty" jsonschema:"true when the change was made but the period could not be read back: period.ack and availability are then not given"`
	Warning        string `json:"warning,omitempty" jsonschema:"why the change is not shown on the period, when the read back failed or no period has its bounds any more; tell the user"`
}

// JustifyServiceDowntime justifies one period of downtime of a service, named
// by its begin: a comment, and whether the period still counts in the
// availability rate. The collector keeps a justification by its bounds and
// shows it on the period of the same bounds only: the MCP takes the bounds
// of a period of the history, and refuses a begin that names none.
func (s *Service) JustifyServiceDowntime(ctx context.Context, options JustifyServiceDowntimeOptions) (ServiceDowntimeChange, error) {
	// The comment goes as the user gives it: the collector refuses an empty
	// one.
	comment := options.Comment
	period, err := s.findStatusPeriod(ctx, options.Service, options.Begin, options.End)
	if err != nil {
		return ServiceDowntimeChange{}, err
	}
	if period.current && options.End == "" {
		return ServiceDowntimeChange{}, fmt.Errorf("the period of service %s begun at %s is still going on: its end moves with each status the agents send, and the justification would no longer match it; nothing was changed; justify it once it ended", period.service.Svcname, period.period.Begin)
	}
	body := map[string]any{"begin": period.period.Begin, "end": period.period.End, "comment": comment, "account": options.Counted}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.PutJSON(ctx, period.path+"/ack", body, &response); err != nil {
		return ServiceDowntimeChange{}, fmt.Errorf("justify the downtime of service %s: %w", period.service.Svcname, err)
	}
	outcome := DowntimeJustified
	if period.period.Ack != nil {
		outcome = DowntimeReplaced
	}
	return s.readBackStatusPeriod(ctx, period, outcome)
}

// UnjustifyServiceDowntime removes the justification of one period of a
// service, named by its begin: the period counts again in the availability
// rate.
func (s *Service) UnjustifyServiceDowntime(ctx context.Context, options UnjustifyServiceDowntimeOptions) (ServiceDowntimeChange, error) {
	period, err := s.findStatusPeriod(ctx, options.Service, options.Begin, options.End)
	if err != nil {
		return ServiceDowntimeChange{}, err
	}
	// A justification whose period grew lies on bounds no period has any
	// more: with end, it is removed all the same.
	if period.period.Ack == nil && !period.unmatched {
		return ServiceDowntimeChange{
			Provenance: s.newProvenance(), Service: period.reference(), Outcome: DowntimeNotJustified,
			Period: period.summary(), Availability: period.availability,
		}, nil
	}
	query := url.Values{"begin": {period.period.Begin}, "end": {period.period.End}}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, period.path+"/ack", query, &response); err != nil {
		return ServiceDowntimeChange{}, fmt.Errorf("remove the justification of service %s: %w", period.service.Svcname, err)
	}
	return s.readBackStatusPeriod(ctx, period, DowntimeRemoved)
}

// foundStatusPeriod is a period of the status history of a service, as read
// before a change.
type foundStatusPeriod struct {
	service      collectorService
	days         int
	path         string
	period       collectorStatusPeriod
	current      bool
	availability *ServiceAvailabilityWindow
	// unmatched is a period named by bounds no period of the history has.
	unmatched bool
}

func (p foundStatusPeriod) reference() ServiceReference {
	return ServiceReference{SvcID: p.service.SvcID, Svcname: p.service.Svcname, ClusterID: p.service.ClusterID}
}

func (p foundStatusPeriod) summary() StatusPeriod {
	return StatusPeriod{Begin: p.period.Begin, End: p.period.End, Status: p.period.Status, Ack: p.period.Ack.statusAck()}
}

// findStatusPeriod reads the status history of a service back to begin, and
// gives the period begun then. With end, it gives the period of these
// bounds, or these bounds when no period of the history has them, as the
// collector takes them.
func (s *Service) findStatusPeriod(ctx context.Context, service, begin, end string) (foundStatusPeriod, error) {
	if !periodBeginPattern.MatchString(begin) {
		return foundStatusPeriod{}, fmt.Errorf("begin must be the begin of a period, YYYY-MM-DD HH:MM:SS, as get_service_status_history gives it")
	}
	if end != "" && !periodBeginPattern.MatchString(end) {
		return foundStatusPeriod{}, fmt.Errorf("end must be YYYY-MM-DD HH:MM:SS, in collector local time")
	}
	start, err := time.Parse(collectorTimeLayout, begin)
	if err != nil {
		return foundStatusPeriod{}, fmt.Errorf("begin must be the begin of a period, YYYY-MM-DD HH:MM:SS, as get_service_status_history gives it")
	}
	// The collector dates have no zone: two days more cover any offset.
	days := int(time.Since(start).Hours()/24) + 2
	if days < 2 {
		days = 2
	}
	if days > maxStatusHistoryDays {
		return foundStatusPeriod{}, fmt.Errorf("begin must be within the last %d days", maxStatusHistoryDays)
	}
	found, err := s.lookupService(ctx, service, []string{"svc_id", "svcname", "cluster_id"})
	if err != nil {
		return foundStatusPeriod{}, err
	}
	result := foundStatusPeriod{service: found, days: days, path: "/services/" + url.PathEscape(found.SvcID) + "/status_log"}
	log, err := s.readStatusLog(ctx, result)
	if err != nil {
		return foundStatusPeriod{}, err
	}
	result.availability = log.availabilityWindow()
	for index, period := range log.Data {
		if period.Begin != begin || (end != "" && period.End != end) {
			continue
		}
		result.period = period
		// The last period is the one going on while the agents send
		// statuses: its end is then the last of them.
		if index == len(log.Data)-1 && log.Availability != nil {
			end, endErr := time.Parse(collectorTimeLayout, period.End)
			now, nowErr := time.Parse(collectorTimeLayout, log.Availability.To)
			result.current = endErr == nil && nowErr == nil && now.Sub(end) < currentPeriodWindow
		}
		return result, nil
	}
	if end != "" {
		result.period, result.unmatched = collectorStatusPeriod{Begin: begin, End: end}, true
		return result, nil
	}
	return foundStatusPeriod{}, fmt.Errorf("no period of service %s begins at %s in its status history; nothing was changed; get_service_status_history gives the periods and their begin", found.Svcname, begin)
}

// readStatusLog reads the status history of the service of period.
func (s *Service) readStatusLog(ctx context.Context, period foundStatusPeriod) (collectorStatusLog, error) {
	var log collectorStatusLog
	if err := s.client.GetJSON(ctx, period.path, url.Values{"days": {strconv.Itoa(period.days)}}, &log); err != nil {
		return collectorStatusLog{}, fmt.Errorf("get status history of service %s: %w", period.service.Svcname, err)
	}
	if log.Data == nil || log.Availability == nil {
		return collectorStatusLog{}, fmt.Errorf("get status history of service %s: unexpected collector response", period.service.Svcname)
	}
	return log, nil
}

// readBackStatusPeriod reads the period again after a change, for its
// justification and the availability the collector computed again. The
// change is made: a read back that fails, or finds no period of the same
// bounds, is told in the result, without the values read before the change.
func (s *Service) readBackStatusPeriod(ctx context.Context, period foundStatusPeriod, outcome string) (ServiceDowntimeChange, error) {
	result := ServiceDowntimeChange{Service: period.reference(), Outcome: outcome, Previous: period.period.Ack.statusAck(), Period: period.summary()}
	result.Period.Ack = nil
	result.Provenance = s.newProvenance()
	log, err := s.readStatusLog(ctx, period)
	if err != nil {
		result.ReadBackFailed = true
		result.Warning = fmt.Sprintf("the change was made, but the period could not be read back: %v; get_service_status_history gives the period and the availability", err)
		return result, nil
	}
	result.Availability = log.availabilityWindow()
	for _, read := range log.Data {
		if read.Begin == period.period.Begin && read.End == period.period.End {
			result.Period.Ack = read.Ack.statusAck()
			return result, nil
		}
	}
	// The last period grows when the agents resume with the same status:
	// the justification keeps its bounds, and no longer shows on it.
	result.Warning = "the change was made, but no period of the history has these bounds any more: the period grew since it was read, and the justification, which keeps its bounds, no longer shows on it; get_service_status_history gives the periods"
	return result, nil
}

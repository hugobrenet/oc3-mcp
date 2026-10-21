package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	// maxCheckThresholdRules bounds the defaults and the contextual
	// thresholds of one check type read to explain a threshold.
	maxCheckThresholdRules = 500
	// maxContextualMemberships bounds the filtersets whose members are read
	// to tell whether their thresholds apply to a check.
	maxContextualMemberships = 20
	// maxCheckInstancesHint bounds the instances an unknown check names.
	maxCheckInstancesHint = 50

	defaultListCheckThresholdsLimit = 50
	maxListCheckThresholdsLimit     = 500
)

// Scopes of list_check_thresholds.
const (
	CheckThresholdSettings   = "settings"
	CheckThresholdDefaults   = "defaults"
	CheckThresholdContextual = "contextual"
)

var (
	checkSettingProps    = []string{"id", "node_id", "svc_id", "chk_type", "chk_instance", "chk_low", "chk_high", "chk_changed", "chk_changed_by", "nodes.nodename", "services.svcname"}
	checkDefaultProps    = []string{"id", "chk_type", "chk_inst", "chk_low", "chk_high", "chk_prio"}
	checkContextualProps = []string{"id", "fset_id", "chk_type", "chk_instance", "chk_low", "chk_high", "gen_filtersets.fset_name"}
)

// CheckKeyOptions name one check: its node, its type, its instance, and the
// service of the resource it measures, none for a check of the node.
type CheckKeyOptions struct {
	Node     string
	Type     string
	Instance string
	Service  string
}

type CheckThresholdDetails struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Check      Check                  `json:"check" jsonschema:"the check, as list_node_checks gives it: its value, the thresholds the collector applies and where they come from, threshold_source"`
	Settings   []CheckThresholdRule   `json:"settings" jsonschema:"the thresholds set on the check, which take precedence over the others; empty when none"`
	Contextual []ContextualThreshold  `json:"contextual" jsonschema:"the thresholds of filtersets for the check type whose instance pattern matches the instance, the most recent first: the first that applies wins, whatever the precision of its pattern"`
	Defaults   []DefaultThreshold     `json:"defaults" jsonschema:"the defaults of the check type whose instance pattern matches the instance, in the order the collector picks them: the highest priority, then the longest pattern, then the oldest"`
	Without    CheckThresholdFallback `json:"without_settings" jsonschema:"the thresholds the check would take without its settings, as unset_check_threshold leaves it: those of the first contextual thresholds that apply, or else of the first defaults, or none; unknown when no contextual thresholds read apply but some were not read"`
	Truncated  bool                   `json:"truncated,omitempty" jsonschema:"whether defaults or contextual thresholds of the type were left unread after 500"`
	Unchecked  int                    `json:"unchecked_memberships,omitempty" jsonschema:"the number of contextual thresholds whose filterset membership was not read, after 20"`
}

// CheckThresholdRule is one row of the thresholds of the collector: a
// setting of a check, defaults or contextual thresholds.
type CheckThresholdRule struct {
	ID          int64    `json:"id" jsonschema:"the collector ID of the row"`
	Type        string   `json:"type" jsonschema:"the check type"`
	Instance    string   `json:"instance,omitempty" jsonschema:"settings: the instance of the check"`
	Pattern     *string  `json:"pattern,omitempty" jsonschema:"defaults and contextual: the instance pattern; defaults: anchored regular expression, empty for every instance; contextual: unanchored regular expression"`
	Low         *float64 `json:"low" jsonschema:"the low threshold; null when none"`
	High        *float64 `json:"high" jsonschema:"the high threshold; null when none"`
	Priority    *int64   `json:"priority,omitempty" jsonschema:"defaults: the priority, the highest winning"`
	FiltersetID int64    `json:"filterset_id,omitempty" jsonschema:"contextual: the collector ID of the filterset whose nodes and services the thresholds apply to"`
	Filterset   string   `json:"filterset,omitempty" jsonschema:"contextual: the name of that filterset"`
	NodeID      string   `json:"node_id,omitempty" jsonschema:"settings: the collector ID of the node of the check"`
	Nodename    string   `json:"nodename,omitempty" jsonschema:"settings: the name of that node"`
	SvcID       string   `json:"svc_id,omitempty" jsonschema:"settings: the collector ID of the service of the check; empty for a check of the node"`
	Svcname     string   `json:"svcname,omitempty" jsonschema:"settings: the name of that service"`
	Changed     string   `json:"changed,omitempty" jsonschema:"settings: when the thresholds were set, as the collector gives it"`
	ChangedBy   string   `json:"changed_by,omitempty" jsonschema:"settings: who set them; an email address is masked"`
}

type ContextualThreshold struct {
	CheckThresholdRule
	Applies *bool  `json:"applies,omitempty" jsonschema:"whether the filterset holds the node, and the service of a check of a service; absent when not read"`
	Reason  string `json:"reason,omitempty" jsonschema:"why applies is false or absent"`
	Wins    bool   `json:"wins,omitempty" jsonschema:"true for the first contextual thresholds that apply, which the check takes when it has no settings"`
}

type DefaultThreshold struct {
	CheckThresholdRule
	Wins bool `json:"wins,omitempty" jsonschema:"true for the defaults the collector picks, which the check takes when no settings and no contextual thresholds apply"`
}

type CheckThresholdFallback struct {
	Source string   `json:"source" jsonschema:"fset:<name> for contextual thresholds, defaults for the defaults of the type, none, or unknown when contextual thresholds were not read"`
	Low    *float64 `json:"low" jsonschema:"the low threshold; null when none"`
	High   *float64 `json:"high" jsonschema:"the high threshold; null when none"`
}

type ListCheckThresholdsOptions struct {
	Scope     string
	Type      string
	Node      string
	Filterset string
	Limit     int
	Cursor    string
}

type CheckThresholdList struct {
	Provenance Provenance           `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Scope      string               `json:"scope" jsonschema:"settings, defaults or contextual"`
	Total      int                  `json:"total" jsonschema:"the number of rows matching, as the collector counts them"`
	Count      int                  `json:"count" jsonschema:"the number of rows returned in this page"`
	Thresholds []CheckThresholdRule `json:"thresholds" jsonschema:"the rows, by check type; defaults by priority, contextual thresholds the most recent first"`
	NextCursor string               `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool                 `json:"truncated" jsonschema:"whether matching rows remain after this page"`
}

type collectorCheckRule struct {
	ID          int64    `json:"id"`
	NodeID      string   `json:"node_id"`
	SvcID       string   `json:"svc_id"`
	ChkType     string   `json:"chk_type"`
	ChkInstance *string  `json:"chk_instance"`
	ChkInst     *string  `json:"chk_inst"`
	ChkLow      *float64 `json:"chk_low"`
	ChkHigh     *float64 `json:"chk_high"`
	ChkPrio     *int64   `json:"chk_prio"`
	ChkChanged  string   `json:"chk_changed"`
	ChangedBy   string   `json:"chk_changed_by"`
	FsetID      int64    `json:"fset_id"`
	FsetName    string   `json:"gen_filtersets.fset_name"`
	Nodename    string   `json:"nodes.nodename"`
	Svcname     string   `json:"services.svcname"`
}

func (row collectorCheckRule) rule() CheckThresholdRule {
	rule := CheckThresholdRule{
		ID: row.ID, Type: row.ChkType, Low: row.ChkLow, High: row.ChkHigh, Priority: row.ChkPrio,
		FiltersetID: row.FsetID, Filterset: row.FsetName, NodeID: row.NodeID, Nodename: row.Nodename,
		SvcID: row.SvcID, Svcname: row.Svcname, Changed: row.ChkChanged, ChangedBy: personName(row.ChangedBy),
	}
	switch {
	case row.ChkInst != nil:
		pattern := *row.ChkInst
		rule.Pattern = &pattern
	case row.FsetID != 0 && row.ChkInstance != nil:
		pattern := *row.ChkInstance
		rule.Pattern = &pattern
	case row.ChkInstance != nil:
		rule.Instance = *row.ChkInstance
	}
	return rule
}

// checkKey is a check named by the collector IDs of its node and service.
type checkKey struct {
	node     NodeReference
	svcID    string
	svcname  string
	chkType  string
	instance string
}

// resolveCheckKey resolves the node and the service of a check to their
// collector IDs, which the collector takes.
func (s *Service) resolveCheckKey(ctx context.Context, options CheckKeyOptions) (checkKey, error) {
	if options.Type == "" {
		return checkKey{}, fmt.Errorf("type must be the check type, such as fs_u, as list_node_checks gives it")
	}
	if err := errors.Join(validateFilterValue("type", options.Type), validateFilterValue("instance", options.Instance)); err != nil {
		return checkKey{}, err
	}
	node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return checkKey{}, err
	}
	key := checkKey{node: NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}, chkType: options.Type, instance: options.Instance}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname"})
		if err != nil {
			return checkKey{}, err
		}
		key.svcID, key.svcname = service.SvcID, service.Svcname
	}
	return key, nil
}

// readCheck reads the check of a key, nil when the collector holds none.
// The collector compares the filters case-insensitively: the check is the
// row of this exact spelling.
func (s *Service) readCheck(ctx context.Context, key checkKey) (*Check, error) {
	filters := []string{filterEquals("node_id", key.node.NodeID), filterEqualsOrEmpty("svc_id", key.svcID), filterEquals("chk_type", key.chkType), filterEqualsOrEmpty("chk_instance", key.instance)}
	var response collectorList[collectorCheck]
	if err := s.client.GetJSON(ctx, "/checks/live", pageQuery(checkProps, "id", filters, 100, 0), &response); err != nil {
		return nil, fmt.Errorf("read check %s:%s of node %s: %w", key.chkType, key.instance, key.node.Nodename, err)
	}
	for _, row := range response.Data {
		if row.ChkType == key.chkType && row.ChkInstance == key.instance && row.SvcID == key.svcID {
			check := Check{
				ID: row.ID, Type: row.ChkType, Instance: row.ChkInstance, Value: row.ChkValue, Low: row.ChkLow, High: row.ChkHigh,
				OutOfBounds: row.ChkErr != 0, ThresholdSource: row.ChkThresholdProvider, NodeID: row.NodeID, Nodename: key.node.Nodename,
				SvcID: row.SvcID, Svcname: key.svcname, Created: row.ChkCreated, Updated: row.ChkUpdated,
			}
			return &check, nil
		}
	}
	return nil, nil
}

// unknownCheck tells a check the node does not report, with the instances
// of its type the node reports, so that the model picks one without another
// call.
func (s *Service) unknownCheck(ctx context.Context, key checkKey) error {
	what := fmt.Sprintf("node %s reports no %s check of instance %q", key.node.Nodename, key.chkType, key.instance)
	if key.svcname != "" {
		what += " for service " + key.svcname
	}
	var response collectorList[collectorCheck]
	filters := []string{filterEquals("node_id", key.node.NodeID), filterEquals("chk_type", key.chkType)}
	if err := s.client.GetJSON(ctx, "/checks/live", pageQuery([]string{"svc_id", "chk_type", "chk_instance"}, "chk_instance,id", filters, maxCheckInstancesHint, 0), &response); err != nil || len(response.Data) == 0 {
		return fmt.Errorf("%s; list_node_checks lists the checks of the node", what)
	}
	svcnames, _ := s.readNames(ctx, "/nodes/"+url.PathEscape(key.node.NodeID)+"/services", "svc_id", "services.svcname")
	var instances []string
	for _, row := range response.Data {
		switch {
		case row.SvcID == "":
			instances = append(instances, strconv.Quote(row.ChkInstance))
		case svcnames[row.SvcID] != "":
			instances = append(instances, strconv.Quote(row.ChkInstance)+" (service "+svcnames[row.SvcID]+")")
		default:
			instances = append(instances, strconv.Quote(row.ChkInstance)+" (service "+row.SvcID+")")
		}
	}
	more := ""
	if total := derefInt(response.Meta.Total); total > len(response.Data) {
		more = fmt.Sprintf(", and %d more", total-len(response.Data))
	}
	return fmt.Errorf("%s; the instances of its %s checks are: %s%s", what, key.chkType, strings.Join(instances, ", "), more)
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// GetCheckThreshold reads the thresholds of one check and where they come
// from: its settings, the contextual thresholds and the defaults of its type
// that match its instance, ordered as the collector picks them.
func (s *Service) GetCheckThreshold(ctx context.Context, options CheckKeyOptions) (CheckThresholdDetails, error) {
	key, err := s.resolveCheckKey(ctx, options)
	if err != nil {
		return CheckThresholdDetails{}, err
	}
	check, err := s.readCheck(ctx, key)
	if err != nil {
		return CheckThresholdDetails{}, err
	}
	if check == nil {
		return CheckThresholdDetails{}, s.unknownCheck(ctx, key)
	}
	result := CheckThresholdDetails{Check: *check, Settings: []CheckThresholdRule{}, Contextual: []ContextualThreshold{}, Defaults: []DefaultThreshold{}}

	// The collector applies the settings of the node, type and instance,
	// whatever their service.
	var settings collectorList[collectorCheckRule]
	filters := []string{filterEquals("node_id", key.node.NodeID), filterEquals("chk_type", key.chkType), filterEqualsOrEmpty("chk_instance", key.instance)}
	if err := s.client.GetJSON(ctx, "/checks/settings", pageQuery(checkSettingProps, "id", filters, 100, 0), &settings); err != nil {
		return CheckThresholdDetails{}, fmt.Errorf("read the settings of check %s:%s: %w", key.chkType, key.instance, err)
	}
	for _, row := range settings.Data {
		if row.ChkType == key.chkType && row.ChkInstance != nil && *row.ChkInstance == key.instance {
			result.Settings = append(result.Settings, row.rule())
		}
	}

	contextual, truncated, err := s.checkRules(ctx, "/checks/contextual_settings", checkContextualProps, "-id", key.chkType)
	if err != nil {
		return CheckThresholdDetails{}, fmt.Errorf("read the contextual thresholds of %s: %w", key.chkType, err)
	}
	result.Truncated = truncated
	for _, row := range contextual {
		// The collector reads the pattern as a Go regular expression, and
		// skips one it cannot.
		pattern, err := regexp.Compile(derefString(row.ChkInstance))
		if err != nil || !pattern.MatchString(key.instance) {
			continue
		}
		candidate := ContextualThreshold{CheckThresholdRule: row.rule()}
		if result.Unchecked > 0 || len(result.Contextual) >= maxContextualMemberships {
			result.Unchecked++
			candidate.Reason = "filterset membership not read"
		} else {
			applies, reason := s.contextualApplies(ctx, row.FsetID, key)
			candidate.Applies, candidate.Reason = &applies, reason
		}
		result.Contextual = append(result.Contextual, candidate)
	}

	defaults, truncated, err := s.checkRules(ctx, "/checks/defaults", checkDefaultProps, "-chk_prio,id", key.chkType)
	if err != nil {
		return CheckThresholdDetails{}, fmt.Errorf("read the defaults of %s: %w", key.chkType, err)
	}
	result.Truncated = result.Truncated || truncated
	for _, row := range defaults {
		if defaultMatches(derefString(row.ChkInst), key.instance) {
			result.Defaults = append(result.Defaults, DefaultThreshold{CheckThresholdRule: row.rule()})
		}
	}
	// The order of the collector: the highest priority, the longest
	// pattern, the oldest.
	slices.SortStableFunc(result.Defaults, func(a, b DefaultThreshold) int {
		return cmp.Or(cmp.Compare(derefInt64(b.Priority), derefInt64(a.Priority)), cmp.Compare(len(derefString(b.Pattern)), len(derefString(a.Pattern))), cmp.Compare(a.ID, b.ID))
	})

	result.Without = CheckThresholdFallback{Source: "none"}
	for index := range result.Contextual {
		if candidate := &result.Contextual[index]; candidate.Applies != nil && *candidate.Applies {
			candidate.Wins = true
			result.Without = CheckThresholdFallback{Source: "fset:" + candidate.Filterset, Low: candidate.Low, High: candidate.High}
			break
		}
	}
	// Contextual thresholds whose membership was not read may apply: the
	// fallback is then unknown.
	if result.Without.Source == "none" && result.Unchecked > 0 {
		result.Without.Source = "unknown"
	}
	if len(result.Defaults) > 0 {
		result.Defaults[0].Wins = true
		if result.Without.Source == "none" {
			result.Without = CheckThresholdFallback{Source: CheckThresholdDefaults, Low: result.Defaults[0].Low, High: result.Defaults[0].High}
		}
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// checkRules reads the defaults or the contextual thresholds of one check
// type, the collector comparing the type case-insensitively.
func (s *Service) checkRules(ctx context.Context, path string, props []string, orderby string, chkType string) ([]collectorCheckRule, bool, error) {
	var response collectorList[collectorCheckRule]
	if err := s.client.GetJSON(ctx, path, pageQuery(props, orderby, []string{filterEquals("chk_type", chkType)}, maxCheckThresholdRules, 0), &response); err != nil {
		return nil, false, err
	}
	rows := slices.DeleteFunc(response.Data, func(row collectorCheckRule) bool { return row.ChkType != chkType })
	return rows, derefInt(response.Meta.Total) > len(response.Data), nil
}

// contextualApplies tells whether the filterset of contextual thresholds
// holds the node of a check, and its service for a check of a service, as
// the collector resolves it.
func (s *Service) contextualApplies(ctx context.Context, fsetID int64, key checkKey) (bool, string) {
	members := []struct{ path, prop, id, what string }{{"nodes", "node_id", key.node.NodeID, "node"}}
	if key.svcID != "" {
		members = append(members, struct{ path, prop, id, what string }{"services", "svc_id", key.svcID, "service"})
	}
	for _, member := range members {
		var response collectorList[map[string]any]
		path := "/filtersets/" + strconv.FormatInt(fsetID, 10) + "/" + member.path
		if err := s.client.GetJSON(ctx, path, pageQuery([]string{member.prop}, member.prop, []string{filterEquals(member.prop, member.id)}, 1, 0), &response); err != nil {
			return false, "the collector does not resolve the filterset: " + err.Error()
		}
		if derefInt(response.Meta.Total) == 0 && len(response.Data) == 0 {
			return false, "the filterset does not hold the " + member.what
		}
	}
	return true, ""
}

// defaultMatches tells whether defaults of an instance pattern match an
// instance, as the collector compares them in SQL: the pattern anchored by
// concatenation, the pattern itself, or an empty one.
func defaultMatches(pattern, instance string) bool {
	if pattern == "" || pattern == instance {
		return true
	}
	// MySQL compares under a case-insensitive collation.
	expression, err := regexp.Compile("(?i)^" + pattern + "$")
	return err == nil && expression.MatchString(instance)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// ListCheckThresholds reads one page of the settings of checks, of the
// defaults of the check types or of the contextual thresholds.
func (s *Service) ListCheckThresholds(ctx context.Context, options ListCheckThresholdsOptions) (CheckThresholdList, error) {
	limit, err := pageLimit(options.Limit, defaultListCheckThresholdsLimit, maxListCheckThresholdsLimit)
	if err != nil {
		return CheckThresholdList{}, err
	}
	if err := validateFilterValue("type", options.Type); err != nil {
		return CheckThresholdList{}, err
	}
	if options.Node != "" && options.Scope != CheckThresholdSettings {
		return CheckThresholdList{}, fmt.Errorf("node filters the settings: pass it with scope settings")
	}
	if options.Filterset != "" && options.Scope != CheckThresholdContextual {
		return CheckThresholdList{}, fmt.Errorf("filterset filters the contextual thresholds: pass it with scope contextual")
	}
	var path, orderby string
	var props, filters []string
	switch options.Scope {
	case CheckThresholdSettings:
		path, props, orderby = "/checks/settings", checkSettingProps, "chk_type,chk_instance,id"
		if options.Node != "" {
			node, err := s.lookupNode(ctx, options.Node, []string{"node_id"})
			if err != nil {
				return CheckThresholdList{}, err
			}
			filters = append(filters, filterEquals("node_id", node.NodeID))
		}
	case CheckThresholdDefaults:
		path, props, orderby = "/checks/defaults", checkDefaultProps, "chk_type,-chk_prio,id"
	case CheckThresholdContextual:
		path, props, orderby = "/checks/contextual_settings", checkContextualProps, "chk_type,-id"
		if options.Filterset != "" {
			if strings.Contains(options.Filterset, "/") {
				return CheckThresholdList{}, fmt.Errorf("filterset must be one exact filterset name or filterset ID")
			}
			if err := validateFilterValue("filterset", options.Filterset); err != nil {
				return CheckThresholdList{}, err
			}
			filterset, err := s.lookupFilterset(ctx, options.Filterset)
			if err != nil {
				return CheckThresholdList{}, err
			}
			filters = append(filters, filterEquals("fset_id", strconv.Itoa(filterset.ID)))
		}
	default:
		return CheckThresholdList{}, fmt.Errorf("scope must be settings, defaults or contextual")
	}
	if options.Type != "" {
		filters = append(filters, filterEquals("chk_type", options.Type))
	}
	fingerprint := listFingerprint(url.Values{"path": {path}, "filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return CheckThresholdList{}, err
	}
	var response collectorList[collectorCheckRule]
	if err := s.client.GetJSON(ctx, path, pageQuery(props, orderby, filters, limit, offset), &response); err != nil {
		return CheckThresholdList{}, fmt.Errorf("list check thresholds: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return CheckThresholdList{}, fmt.Errorf("list check thresholds: unexpected collector response")
	}
	result := CheckThresholdList{Scope: options.Scope, Total: *response.Meta.Total, Count: len(response.Data), Thresholds: make([]CheckThresholdRule, 0, len(response.Data))}
	for _, row := range response.Data {
		result.Thresholds = append(result.Thresholds, row.rule())
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

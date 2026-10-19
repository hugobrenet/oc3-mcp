package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultListRulesetsLimit = 50
	maxListRulesetsLimit     = 200
	// maxContextualRulesets bounds the contextual rulesets whose filterset
	// is matched against a node or a service: one request each.
	maxContextualRulesets = 100
	// maxRulesetVariables bounds the variables given for one ruleset.
	maxRulesetVariables = 200
	// maxRulesetVariableBytes bounds the value of one variable.
	maxRulesetVariableBytes = 2 << 10
)

// rulesetTypes are the types of a ruleset: an explicit one applies where it
// is attached, a contextual one to the nodes and services its filterset
// selects.
var rulesetTypes = []string{"explicit", "contextual"}

type ListComplianceRulesetsOptions struct {
	Name    string
	Type    string
	Node    string
	Service string
	Limit   int
	Cursor  string
}

type ComplianceRulesetList struct {
	Provenance Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference      `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Service    *ServiceReference   `json:"service,omitempty" jsonschema:"the service the service filter resolved to"`
	Total      int                 `json:"total" jsonschema:"the number of rulesets matching the filters, as the collector counts them"`
	Count      int                 `json:"count" jsonschema:"the number of rulesets returned in this page"`
	Rulesets   []ComplianceRuleset `json:"rulesets" jsonschema:"the rulesets sorted by name"`
	NextCursor string              `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool                `json:"truncated" jsonschema:"whether matching rulesets remain after this page"`
}

type ComplianceRuleset struct {
	ID        int    `json:"id" jsonschema:"the collector ruleset ID"`
	Name      string `json:"name" jsonschema:"the ruleset name"`
	Type      string `json:"type" jsonschema:"explicit, applied where it is attached, or contextual, applied to the nodes and services its filterset selects"`
	Public    bool   `json:"public" jsonschema:"whether the ruleset may be attached to nodes and services, rather than only included in other rulesets"`
	AppliesBy string `json:"applies_by,omitempty" jsonschema:"with node or service: attached, when the ruleset is attached to it directly, or filterset, when it is a contextual ruleset whose filterset selects it"`
}

type collectorRuleset struct {
	ID     int    `json:"id"`
	Name   string `json:"ruleset_name"`
	Type   string `json:"ruleset_type"`
	Public string `json:"ruleset_public"`
}

// ListComplianceRulesets reads one page of the compliance rulesets the
// caller sees, those published to one of its groups, every ruleset for a
// manager. With a node or a service, it lists the rulesets attached to it and
// the contextual rulesets whose filterset selects it.
func (s *Service) ListComplianceRulesets(ctx context.Context, options ListComplianceRulesetsOptions) (ComplianceRulesetList, error) {
	if err := validateFilterValue("name", options.Name); err != nil {
		return ComplianceRulesetList{}, err
	}
	if options.Type != "" && !slices.Contains(rulesetTypes, options.Type) {
		return ComplianceRulesetList{}, fmt.Errorf("type must be one of %s", strings.Join(rulesetTypes, ", "))
	}
	if options.Node != "" && options.Service != "" {
		return ComplianceRulesetList{}, fmt.Errorf("pass node or service, not both")
	}
	limit, err := pageLimit(options.Limit, defaultListRulesetsLimit, maxListRulesetsLimit)
	if err != nil {
		return ComplianceRulesetList{}, err
	}
	result := ComplianceRulesetList{Rulesets: []ComplianceRuleset{}}

	// With a node or a service, the rulesets applying to it select IDs,
	// which the collector list then filters and pages.
	var appliesBy map[int]string
	switch {
	case options.Node != "":
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ComplianceRulesetList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		if appliesBy, err = s.applyingRulesets(ctx, "/nodes/"+url.PathEscape(node.NodeID)+"/compliance/rulesets", "nodes", "node_id", node.NodeID); err != nil {
			return ComplianceRulesetList{}, err
		}
	case options.Service != "":
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return ComplianceRulesetList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		if appliesBy, err = s.applyingRulesets(ctx, "/services/"+url.PathEscape(service.SvcID)+"/compliance/rulesets", "services", "svc_id", service.SvcID); err != nil {
			return ComplianceRulesetList{}, err
		}
	}
	if appliesBy != nil && len(appliesBy) == 0 {
		result.Provenance = s.newProvenance()
		return result, nil
	}

	var filters []string
	if appliesBy != nil {
		ids := make([]int, 0, len(appliesBy))
		for id := range appliesBy {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		filters = append(filters, "id:in:"+joinInts(ids))
	}
	if options.Name != "" {
		filters = append(filters, filterContains("ruleset_name", options.Name))
	}
	if options.Type != "" {
		filters = append(filters, filterEquals("ruleset_type", options.Type))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ComplianceRulesetList{}, err
	}
	var response collectorList[collectorRuleset]
	if err := s.client.GetJSON(ctx, "/compliance/rulesets", pageQuery([]string{"id", "ruleset_name", "ruleset_type", "ruleset_public"}, "ruleset_name,id", filters, limit, offset), &response); err != nil {
		return ComplianceRulesetList{}, fmt.Errorf("list rulesets: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return ComplianceRulesetList{}, fmt.Errorf("list rulesets: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	for _, row := range response.Data {
		result.Rulesets = append(result.Rulesets, ComplianceRuleset{
			ID: row.ID, Name: row.Name, Type: row.Type, Public: collectorBool(row.Public), AppliesBy: appliesBy[row.ID],
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// applyingRulesets reads the rulesets attached to a node or a service, at
// attached, and the contextual rulesets whose filterset selects it, matched
// on the filterset nodes or services by their ID prop.
func (s *Service) applyingRulesets(ctx context.Context, attached, kind, idProp, id string) (map[int]string, error) {
	appliesBy := map[int]string{}
	var direct collectorList[collectorRuleset]
	if err := s.client.GetJSON(ctx, attached, url.Values{"props": {"id"}, "limit": {strconv.Itoa(maxModulesetScan + 1)}}, &direct); err != nil {
		return nil, fmt.Errorf("list attached rulesets: %w", err)
	}
	if len(direct.Data) > maxModulesetScan {
		return nil, fmt.Errorf("list attached rulesets: more than %d rulesets attached", maxModulesetScan)
	}
	for _, row := range direct.Data {
		appliesBy[row.ID] = "attached"
	}

	var contextual collectorList[collectorRuleset]
	if err := s.client.GetJSON(ctx, "/compliance/rulesets", pageQuery([]string{"id"}, "id", []string{filterEquals("ruleset_type", "contextual")}, maxContextualRulesets, 0), &contextual); err != nil {
		return nil, fmt.Errorf("list contextual rulesets: %w", err)
	}
	if contextual.Meta.Total == nil || *contextual.Meta.Total > maxContextualRulesets {
		return nil, fmt.Errorf("list contextual rulesets: more than %d contextual rulesets to match", maxContextualRulesets)
	}
	if len(contextual.Data) == 0 {
		return appliesBy, nil
	}
	ids := make([]int, 0, len(contextual.Data))
	for _, row := range contextual.Data {
		ids = append(ids, row.ID)
	}
	// The filterset of each contextual ruleset, from the variables view,
	// which carries it on every row of the ruleset and of the rulesets it
	// includes: grouped, it gives one row per ruleset and filterset.
	var variables collectorList[struct {
		RulesetID int `json:"ruleset_id"`
		FsetID    int `json:"fset_id"`
	}]
	query := pageQuery([]string{"ruleset_id", "fset_id"}, "ruleset_id", []string{"ruleset_id:in:" + joinInts(ids)}, maxModulesetScan, 0)
	query.Set("groupby", "ruleset_id,fset_id")
	if err := s.client.GetJSON(ctx, "/compliance/rulesets_variables", query, &variables); err != nil {
		return nil, fmt.Errorf("read the filtersets of the contextual rulesets: %w", err)
	}
	if variables.Meta.Total == nil || *variables.Meta.Total > len(variables.Data) {
		return nil, fmt.Errorf("read the filtersets of the contextual rulesets: unexpected collector response")
	}
	filtersets := map[int][]int{}
	for _, row := range variables.Data {
		if row.FsetID != 0 && !slices.Contains(filtersets[row.FsetID], row.RulesetID) {
			filtersets[row.FsetID] = append(filtersets[row.FsetID], row.RulesetID)
		}
	}
	for fsetID, rulesets := range filtersets {
		var members collectorList[map[string]any]
		path := "/filtersets/" + strconv.Itoa(fsetID) + "/" + kind
		if err := s.client.GetJSON(ctx, path, pageQuery([]string{idProp}, "", []string{filterEquals(idProp, id)}, 1, 0), &members); err != nil {
			return nil, fmt.Errorf("match the filterset %d: %w", fsetID, err)
		}
		if members.Meta.Total == nil || *members.Meta.Total == 0 {
			continue
		}
		for _, id := range rulesets {
			if _, ok := appliesBy[id]; !ok {
				appliesBy[id] = "filterset"
			}
		}
	}
	return appliesBy, nil
}

type ComplianceRulesetDetails struct {
	Provenance         Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Ruleset            ComplianceRuleset `json:"ruleset" jsonschema:"the ruleset"`
	Filterset          *RulesetFilterset `json:"filterset,omitempty" jsonschema:"for a contextual ruleset, the filterset selecting the nodes and services it applies to"`
	VariablesCount     int               `json:"variables_count" jsonschema:"the number of its own variables"`
	Variables          []RulesetVariable `json:"variables" jsonschema:"its own variables, sorted by name, at most 200"`
	VariablesTruncated bool              `json:"variables_truncated,omitempty" jsonschema:"whether variables were left out beyond 200"`
	Included           []IncludedRuleset `json:"included_rulesets,omitempty" jsonschema:"the rulesets it includes, at any depth, with their variables: they apply with it"`
	Publications       []string          `json:"publications,omitempty" jsonschema:"the groups the ruleset is published to: their members see it and may attach it"`
	Responsibles       []string          `json:"responsibles,omitempty" jsonschema:"the groups responsible for the ruleset: their members may change it"`
	IncludedBy         []string          `json:"included_by,omitempty" jsonschema:"the rulesets including this one: it applies where they apply"`
	Modulesets         []string          `json:"modulesets,omitempty" jsonschema:"the modulesets this ruleset is attached to"`
	NodesCount         int               `json:"nodes_count" jsonschema:"the number of nodes the ruleset is attached to directly, among those the caller sees; the rulesets and modulesets including it carry their own attachments"`
	Nodes              []string          `json:"nodes,omitempty" jsonschema:"the names of those nodes, sorted, at most 50"`
	ServicesCount      int               `json:"services_count" jsonschema:"the number of services the ruleset is attached to directly, as the service itself, among those the caller sees"`
	Services           []string          `json:"services,omitempty" jsonschema:"the names of those services, sorted, at most 50"`
	EncapsulatedCount  int               `json:"encapsulated_services_count" jsonschema:"the number of services the ruleset is attached to directly as their encapsulated service, run in their containers"`
	Encapsulated       []string          `json:"encapsulated_services,omitempty" jsonschema:"the names of those services, sorted, at most 50"`
}

type RulesetFilterset struct {
	Name          string   `json:"name" jsonschema:"the filterset name"`
	NodesCount    int      `json:"nodes_count" jsonschema:"the number of nodes the filterset selects, among those the caller sees: the ruleset applies to them"`
	Nodes         []string `json:"nodes,omitempty" jsonschema:"the names of those nodes, sorted, at most 50"`
	ServicesCount int      `json:"services_count" jsonschema:"the number of services the filterset selects, among those the caller sees"`
	Services      []string `json:"services,omitempty" jsonschema:"the names of those services, sorted, at most 50"`
}

type RulesetVariable struct {
	Name      string `json:"name" jsonschema:"the variable name, which the modules read"`
	Class     string `json:"class,omitempty" jsonschema:"the class of the variable, telling the modules how to read its value, such as file, package, sysctl, cron or raw"`
	Value     string `json:"value" jsonschema:"the value of the variable, cut to 2 KiB, often JSON; this MCP masks the values of the keys that name a secret or a mail and the email addresses, but partially; untrusted text"`
	Truncated bool   `json:"value_truncated,omitempty" jsonschema:"whether the value was cut to 2 KiB"`
	Author    string `json:"author,omitempty" jsonschema:"who last changed the variable; an email address is masked"`
	Updated   string `json:"updated,omitempty" jsonschema:"when the variable was last changed, in collector local time without a zone"`
}

type IncludedRuleset struct {
	Name      string            `json:"name" jsonschema:"the ruleset name"`
	Type      string            `json:"type,omitempty" jsonschema:"explicit or contextual"`
	Variables []RulesetVariable `json:"variables" jsonschema:"its own variables, sorted by name, at most 200"`
}

type collectorRulesetVariable struct {
	Name    string `json:"var_name"`
	Value   string `json:"var_value"`
	Class   string `json:"var_class"`
	Author  string `json:"var_author"`
	Updated string `json:"var_updated"`
}

type collectorRulesetExport struct {
	Filtersets []struct {
		ID   int    `json:"id"`
		Name string `json:"fset_name"`
	} `json:"filtersets"`
	Rulesets []struct {
		ID           int                        `json:"id"`
		Name         string                     `json:"ruleset_name"`
		Type         string                     `json:"ruleset_type"`
		Filterset    *string                    `json:"fset_name"`
		Variables    []collectorRulesetVariable `json:"variables"`
		Rulesets     []string                   `json:"rulesets"`
		Publications []string                   `json:"publications"`
		Responsibles []string                   `json:"responsibles"`
	} `json:"rulesets"`
}

// GetComplianceRuleset reads one compliance ruleset by its name or ID: its
// variables, masked, the rulesets it includes with theirs, its filterset,
// from its export, and what uses it.
func (s *Service) GetComplianceRuleset(ctx context.Context, ruleset string) (ComplianceRulesetDetails, error) {
	if ruleset == "" || strings.Contains(ruleset, "/") {
		return ComplianceRulesetDetails{}, fmt.Errorf("ruleset must be one exact ruleset name or ruleset ID")
	}
	if err := validateFilterValue("ruleset", ruleset); err != nil {
		return ComplianceRulesetDetails{}, err
	}
	var found collectorList[collectorRuleset]
	err := s.client.GetJSON(ctx, "/compliance/rulesets/"+url.PathEscape(ruleset), url.Values{"props": {"id,ruleset_name,ruleset_type,ruleset_public"}}, &found)
	if err != nil && !isNotFound(err) {
		return ComplianceRulesetDetails{}, fmt.Errorf("get ruleset %s: %w", ruleset, err)
	}
	if err != nil || len(found.Data) != 1 {
		return ComplianceRulesetDetails{}, fmt.Errorf("get ruleset %s: not found in the collector, or not published to the caller; list the rulesets with list_compliance_rulesets", ruleset)
	}
	row := found.Data[0]
	path := "/compliance/rulesets/" + strconv.Itoa(row.ID)
	result := ComplianceRulesetDetails{Ruleset: ComplianceRuleset{ID: row.ID, Name: row.Name, Type: row.Type, Public: collectorBool(row.Public)}}

	var export collectorRulesetExport
	if err := s.client.GetJSON(ctx, path+"/export", nil, &export); err != nil {
		return ComplianceRulesetDetails{}, fmt.Errorf("export ruleset %s: %w", row.Name, err)
	}
	byName := map[string]int{}
	for i, item := range export.Rulesets {
		byName[item.Name] = i
	}
	own, ok := byName[row.Name]
	if !ok {
		return ComplianceRulesetDetails{}, fmt.Errorf("export ruleset %s: unexpected collector response", row.Name)
	}
	self := export.Rulesets[own]
	result.Publications, result.Responsibles = self.Publications, self.Responsibles
	result.Variables, result.VariablesCount, result.VariablesTruncated = rulesetVariables(self.Variables)
	// The included rulesets, at any depth, each once.
	seen := map[string]bool{row.Name: true}
	queue := slices.Clone(self.Rulesets)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		i, ok := byName[name]
		if seen[name] || !ok {
			continue
		}
		seen[name] = true
		variables, _, _ := rulesetVariables(export.Rulesets[i].Variables)
		result.Included = append(result.Included, IncludedRuleset{Name: name, Type: export.Rulesets[i].Type, Variables: variables})
		queue = append(queue, export.Rulesets[i].Rulesets...)
	}
	if self.Filterset != nil && *self.Filterset != "" {
		result.Filterset = &RulesetFilterset{Name: *self.Filterset}
		for _, fset := range export.Filtersets {
			if fset.Name != *self.Filterset {
				continue
			}
			path := "/filtersets/" + strconv.Itoa(fset.ID)
			if result.Filterset.NodesCount, result.Filterset.Nodes, err = s.attachedNames(ctx, path+"/nodes", "nodename", "nodename,node_id", false); err != nil {
				return ComplianceRulesetDetails{}, fmt.Errorf("list the nodes of filterset %s: %w", fset.Name, err)
			}
			if result.Filterset.ServicesCount, result.Filterset.Services, err = s.attachedNames(ctx, path+"/services", "svcname", "svcname,svc_id", false); err != nil {
				return ComplianceRulesetDetails{}, fmt.Errorf("list the services of filterset %s: %w", fset.Name, err)
			}
		}
	}

	var usage struct {
		Data struct {
			Modulesets []collectorModuleset `json:"modulesets"`
			Rulesets   []collectorRuleset   `json:"rulesets"`
		} `json:"data"`
	}
	if err := s.client.GetJSON(ctx, path+"/usage", nil, &usage); err != nil {
		return ComplianceRulesetDetails{}, fmt.Errorf("get usage of ruleset %s: %w", row.Name, err)
	}
	for _, item := range usage.Data.Rulesets {
		result.IncludedBy = append(result.IncludedBy, item.Name)
	}
	for _, item := range usage.Data.Modulesets {
		result.Modulesets = append(result.Modulesets, item.Name)
	}
	// The attachments are read from the routes restricted to the nodes and
	// services the caller sees, not from the usage, which is not.
	if result.NodesCount, result.Nodes, err = s.attachedNames(ctx, path+"/nodes", "nodename", "nodename,node_id", false); err != nil {
		return ComplianceRulesetDetails{}, fmt.Errorf("list nodes of ruleset %s: %w", row.Name, err)
	}
	if result.ServicesCount, result.Services, err = s.attachedNames(ctx, path+"/services", "svcname", "svcname,svc_id", false); err != nil {
		return ComplianceRulesetDetails{}, fmt.Errorf("list services of ruleset %s: %w", row.Name, err)
	}
	if result.EncapsulatedCount, result.Encapsulated, err = s.attachedNames(ctx, path+"/services", "svcname", "svcname,svc_id", true); err != nil {
		return ComplianceRulesetDetails{}, fmt.Errorf("list encapsulated services of ruleset %s: %w", row.Name, err)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// rulesetVariables reads the variables of a ruleset, sorted by name, their
// values masked and cut.
func rulesetVariables(rows []collectorRulesetVariable) ([]RulesetVariable, int, bool) {
	variables := make([]RulesetVariable, 0, min(len(rows), maxRulesetVariables))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		value, cut := truncateUTF8(maskVariableValue(row.Name, row.Value), maxRulesetVariableBytes)
		variables = append(variables, RulesetVariable{
			Name: row.Name, Class: row.Class, Value: value, Truncated: cut, Author: personName(row.Author), Updated: row.Updated,
		})
	}
	sort.Slice(variables, func(i, j int) bool { return variables[i].Name < variables[j].Name })
	count := len(variables)
	if count > maxRulesetVariables {
		return variables[:maxRulesetVariables], count, true
	}
	return variables, count, false
}

// maskVariableValue masks the value of a ruleset variable: whole when the
// variable names a secret, else the sensitive keys and the email addresses of
// a JSON value, or the sensitive keys and the email addresses of a text, as
// in a service configuration.
func maskVariableValue(name, value string) string {
	if sensitiveName(name) {
		return maskedRequestValue
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err == nil {
		switch decoded.(type) {
		case map[string]any, []any:
			if masked, found := maskValue(decoded); found {
				if text, err := json.Marshal(masked); err == nil {
					return string(text)
				}
				return maskedRequestValue
			}
			return value
		}
	}
	// A text masks as a configuration, then any email address left, the
	// value not being made of key = value lines alone.
	masked, _ := maskServiceConfig(value)
	return emailPattern.ReplaceAllString(masked, maskedRequestValue)
}

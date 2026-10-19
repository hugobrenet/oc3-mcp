package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultListModulesetsLimit = 50
	maxListModulesetsLimit     = 200
	// maxModulesetModules bounds the modules given for one moduleset.
	maxModulesetModules = 50
	// maxModulesetScan bounds the modulesets a node, a service or a module
	// selects: their IDs are passed to the collector in one filter.
	maxModulesetScan = 1000
	// maxModulesetAttachments bounds the nodes and services named for one
	// moduleset; their number is counted beyond.
	maxModulesetAttachments = 50
	// maxModulesetModuleRows bounds the modules read for one page.
	maxModulesetModuleRows = 20000
)

type ListComplianceModulesetsOptions struct {
	Name    string
	Module  string
	Node    string
	Service string
	Limit   int
	Cursor  string
}

type ComplianceModulesetList struct {
	Provenance Provenance            `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference        `json:"node,omitempty" jsonschema:"the node the node filter resolved to: the modulesets attached to it"`
	Service    *ServiceReference     `json:"service,omitempty" jsonschema:"the service the service filter resolved to: the modulesets attached to it"`
	Total      int                   `json:"total" jsonschema:"the number of modulesets matching the filters, as the collector counts them"`
	Count      int                   `json:"count" jsonschema:"the number of modulesets returned in this page"`
	Modulesets []ComplianceModuleset `json:"modulesets" jsonschema:"the modulesets sorted by name"`
	NextCursor string                `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool                  `json:"truncated" jsonschema:"whether matching modulesets remain after this page"`
}

type ComplianceModuleset struct {
	ID               int                `json:"id" jsonschema:"the collector moduleset ID"`
	Name             string             `json:"name" jsonschema:"the moduleset name"`
	Author           string             `json:"author,omitempty" jsonschema:"who last changed the moduleset; an email address is masked"`
	Updated          string             `json:"updated,omitempty" jsonschema:"when the moduleset was last changed, in collector local time without a zone"`
	ModulesCount     int                `json:"modules_count" jsonschema:"the number of modules of the moduleset itself, those of its included modulesets apart"`
	Modules          []ComplianceModule `json:"modules" jsonschema:"the modules of the moduleset itself, sorted by name, at most 50"`
	ModulesTruncated bool               `json:"modules_truncated,omitempty" jsonschema:"whether modules were left out beyond 50"`
}

type ComplianceModule struct {
	Name    string `json:"name" jsonschema:"the compliance module, such as linux.files, as list_compliance_runs gives it"`
	Autofix bool   `json:"autofix" jsonschema:"whether the agent fixes the module on its own when a check fails"`
}

type collectorModuleset struct {
	ID      int    `json:"id"`
	Name    string `json:"modset_name"`
	Author  string `json:"modset_author"`
	Updated string `json:"modset_updated"`
}

type collectorModulesetModule struct {
	ID          int    `json:"id"`
	ModulesetID int    `json:"modset_id"`
	Name        string `json:"modset_mod_name"`
	Autofix     string `json:"autofix"`
}

// ListComplianceModulesets reads one page of the compliance modulesets the
// caller sees, those published to one of its groups, every moduleset for a
// manager, with their modules.
func (s *Service) ListComplianceModulesets(ctx context.Context, options ListComplianceModulesetsOptions) (ComplianceModulesetList, error) {
	for name, value := range map[string]string{"name": options.Name, "module": options.Module} {
		if err := validateFilterValue(name, value); err != nil {
			return ComplianceModulesetList{}, err
		}
	}
	if options.Node != "" && options.Service != "" {
		return ComplianceModulesetList{}, fmt.Errorf("pass node or service, not both")
	}
	limit, err := pageLimit(options.Limit, defaultListModulesetsLimit, maxListModulesetsLimit)
	if err != nil {
		return ComplianceModulesetList{}, err
	}
	result := ComplianceModulesetList{Modulesets: []ComplianceModuleset{}}

	// The node, the service and the module select moduleset IDs, which the
	// collector list then filters with the name and pages.
	var ids []int
	selected := false
	attached := ""
	switch {
	case options.Node != "":
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return ComplianceModulesetList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		attached = "/nodes/" + url.PathEscape(node.NodeID) + "/compliance/modulesets"
	case options.Service != "":
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return ComplianceModulesetList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		attached = "/services/" + url.PathEscape(service.SvcID) + "/compliance/modulesets"
	}
	if attached != "" {
		var response collectorList[collectorModuleset]
		if err := s.client.GetJSON(ctx, attached, url.Values{"props": {"id"}, "limit": {strconv.Itoa(maxModulesetScan + 1)}}, &response); err != nil {
			return ComplianceModulesetList{}, fmt.Errorf("list attached modulesets: %w", err)
		}
		if len(response.Data) > maxModulesetScan {
			return ComplianceModulesetList{}, fmt.Errorf("list attached modulesets: more than %d modulesets attached", maxModulesetScan)
		}
		for _, row := range response.Data {
			ids = append(ids, row.ID)
		}
		selected = true
	}
	if options.Module != "" {
		var response collectorList[collectorModulesetModule]
		query := pageQuery([]string{"modset_id"}, "modset_id", []string{filterEqualsOrFamily("modset_mod_name", options.Module)}, maxModulesetScan, 0)
		if err := s.client.GetJSON(ctx, "/compliance/modulesets_modules", query, &response); err != nil {
			return ComplianceModulesetList{}, fmt.Errorf("list modulesets of module %s: %w", options.Module, err)
		}
		if response.Meta.Total != nil && *response.Meta.Total > maxModulesetScan {
			return ComplianceModulesetList{}, fmt.Errorf("list modulesets of module %s: more than %d modules match", options.Module, maxModulesetScan)
		}
		var withModule []int
		for _, row := range response.Data {
			if !slices.Contains(withModule, row.ModulesetID) && (!selected || slices.Contains(ids, row.ModulesetID)) {
				withModule = append(withModule, row.ModulesetID)
			}
		}
		ids, selected = withModule, true
	}
	if selected && len(ids) == 0 {
		result.Provenance = s.newProvenance()
		return result, nil
	}

	var filters []string
	if selected {
		filters = append(filters, "id:in:"+joinInts(ids))
	}
	if options.Name != "" {
		filters = append(filters, filterContains("modset_name", options.Name))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return ComplianceModulesetList{}, err
	}
	var response collectorList[collectorModuleset]
	if err := s.client.GetJSON(ctx, "/compliance/modulesets", pageQuery([]string{"id", "modset_name", "modset_author", "modset_updated"}, "modset_name,id", filters, limit, offset), &response); err != nil {
		return ComplianceModulesetList{}, fmt.Errorf("list modulesets: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return ComplianceModulesetList{}, fmt.Errorf("list modulesets: unexpected collector response")
	}
	modules, err := s.modulesetModules(ctx, response.Data)
	if err != nil {
		return ComplianceModulesetList{}, err
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	for _, row := range response.Data {
		moduleset := ComplianceModuleset{ID: row.ID, Name: row.Name, Author: personName(row.Author), Updated: row.Updated, Modules: []ComplianceModule{}}
		all := modules[row.ID]
		moduleset.ModulesCount = len(all)
		if len(all) > maxModulesetModules {
			all, moduleset.ModulesTruncated = all[:maxModulesetModules], true
		}
		moduleset.Modules = append(moduleset.Modules, all...)
		result.Modulesets = append(result.Modulesets, moduleset)
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// modulesetModules reads the modules of the modulesets of a page, sorted by
// name, in one request.
func (s *Service) modulesetModules(ctx context.Context, modulesets []collectorModuleset) (map[int][]ComplianceModule, error) {
	modules := map[int][]ComplianceModule{}
	if len(modulesets) == 0 {
		return modules, nil
	}
	ids := make([]int, 0, len(modulesets))
	for _, row := range modulesets {
		ids = append(ids, row.ID)
	}
	// The modules are read in batches up to their total: a page of
	// modulesets may hold more modules than one batch.
	for offset := 0; ; {
		var response collectorList[collectorModulesetModule]
		query := pageQuery([]string{"id", "modset_id", "modset_mod_name", "autofix"}, "modset_mod_name,id", []string{"modset_id:in:" + joinInts(ids)}, maxModulesetScan, offset)
		if err := s.client.GetJSON(ctx, "/compliance/modulesets_modules", query, &response); err != nil {
			return nil, fmt.Errorf("list modules of the modulesets: %w", err)
		}
		if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > maxModulesetScan {
			return nil, fmt.Errorf("list modules of the modulesets: unexpected collector response")
		}
		for _, row := range response.Data {
			// A moduleset without module is a row with a zero ID.
			if row.ID == 0 || row.Name == "" {
				continue
			}
			modules[row.ModulesetID] = append(modules[row.ModulesetID], ComplianceModule{Name: row.Name, Autofix: collectorBool(row.Autofix)})
		}
		offset += len(response.Data)
		if len(response.Data) == 0 || offset >= *response.Meta.Total {
			return modules, nil
		}
		if offset >= maxModulesetModuleRows {
			return nil, fmt.Errorf("list modules of the modulesets: more than %d modules in the page, ask a smaller limit", maxModulesetModuleRows)
		}
	}
}

type ComplianceModulesetDetails struct {
	Provenance        Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Moduleset         ComplianceModuleset `json:"moduleset" jsonschema:"the moduleset and its own modules"`
	Publications      []string            `json:"publications,omitempty" jsonschema:"the groups the moduleset is published to: their members see it and may attach it"`
	Responsibles      []string            `json:"responsibles,omitempty" jsonschema:"the groups responsible for the moduleset: their members may change it"`
	Included          []IncludedModuleset `json:"included_modulesets,omitempty" jsonschema:"the modulesets it includes, at any depth, with their modules: their modules run with its own"`
	Rulesets          []ModulesetRuleset  `json:"rulesets,omitempty" jsonschema:"the rulesets attached to the moduleset or to an included one, and the rulesets they include, sorted by name: their variables are what its modules read"`
	Parents           []string            `json:"included_by,omitempty" jsonschema:"the modulesets including this one"`
	NodesCount        int                 `json:"nodes_count" jsonschema:"the number of nodes the moduleset is attached to directly, among those the caller sees; the modulesets including it, in included_by, carry their own attachments"`
	Nodes             []string            `json:"nodes,omitempty" jsonschema:"the names of those nodes, sorted, at most 50"`
	ServicesCount     int                 `json:"services_count" jsonschema:"the number of services the moduleset is attached to directly, as the service itself, among those the caller sees"`
	Services          []string            `json:"services,omitempty" jsonschema:"the names of those services, sorted, at most 50"`
	EncapsulatedCount int                 `json:"encapsulated_services_count" jsonschema:"the number of services the moduleset is attached to directly as their encapsulated service, run in their containers"`
	Encapsulated      []string            `json:"encapsulated_services,omitempty" jsonschema:"the names of those services, sorted, at most 50"`
}

type IncludedModuleset struct {
	Name    string             `json:"name" jsonschema:"the moduleset name"`
	Modules []ComplianceModule `json:"modules" jsonschema:"its own modules, sorted by name"`
}

type ModulesetRuleset struct {
	Name      string `json:"name" jsonschema:"the ruleset name"`
	Type      string `json:"type,omitempty" jsonschema:"explicit, applied where it is attached, or contextual, applied to the nodes and services of its filterset"`
	Filterset string `json:"filterset,omitempty" jsonschema:"for a contextual ruleset, the filterset selecting what it applies to"`
	Variables int    `json:"variables" jsonschema:"the number of its own variables"`
	// AttachedTo and IncludedBy tell how the ruleset comes in: attached to
	// a moduleset of the tree, or included by another ruleset.
	AttachedTo []string `json:"attached_to,omitempty" jsonschema:"the modulesets, this one or an included one, the ruleset is attached to"`
	IncludedBy []string `json:"included_by,omitempty" jsonschema:"the rulesets including this one, through which it comes in"`
}

type collectorModulesetExport struct {
	Modulesets []struct {
		ID           int                        `json:"id"`
		Name         string                     `json:"modset_name"`
		Modules      []collectorModulesetModule `json:"modules"`
		Modulesets   []string                   `json:"modulesets"`
		Rulesets     []string                   `json:"rulesets"`
		Publications []string                   `json:"publications"`
		Responsibles []string                   `json:"responsibles"`
	} `json:"modulesets"`
	Rulesets []struct {
		Name      string   `json:"ruleset_name"`
		Type      string   `json:"ruleset_type"`
		Filterset *string  `json:"fset_name"`
		Variables []any    `json:"variables"`
		Rulesets  []string `json:"rulesets"`
	} `json:"rulesets"`
}

// GetComplianceModuleset reads one compliance moduleset by its name or ID:
// its modules, the modulesets it includes and the rulesets attached, from
// its export, what includes it, and the nodes and services it is attached
// to.
func (s *Service) GetComplianceModuleset(ctx context.Context, moduleset string) (ComplianceModulesetDetails, error) {
	if moduleset == "" || strings.Contains(moduleset, "/") {
		return ComplianceModulesetDetails{}, fmt.Errorf("moduleset must be one exact moduleset name or moduleset ID")
	}
	if err := validateFilterValue("moduleset", moduleset); err != nil {
		return ComplianceModulesetDetails{}, err
	}
	var found collectorList[collectorModuleset]
	err := s.client.GetJSON(ctx, "/compliance/modulesets/"+url.PathEscape(moduleset), url.Values{"props": {"id,modset_name,modset_author,modset_updated"}}, &found)
	if err != nil && !isNotFound(err) {
		return ComplianceModulesetDetails{}, fmt.Errorf("get moduleset %s: %w", moduleset, err)
	}
	if err != nil || len(found.Data) != 1 {
		return ComplianceModulesetDetails{}, fmt.Errorf("get moduleset %s: not found in the collector, or not published to the caller; list the modulesets with list_compliance_modulesets", moduleset)
	}
	row := found.Data[0]
	path := "/compliance/modulesets/" + strconv.Itoa(row.ID)

	var export collectorModulesetExport
	if err := s.client.GetJSON(ctx, path+"/export", nil, &export); err != nil {
		return ComplianceModulesetDetails{}, fmt.Errorf("export moduleset %s: %w", row.Name, err)
	}
	byName := map[string]int{}
	for i, item := range export.Modulesets {
		byName[item.Name] = i
	}
	own, ok := byName[row.Name]
	if !ok {
		return ComplianceModulesetDetails{}, fmt.Errorf("export moduleset %s: unexpected collector response", row.Name)
	}
	result := ComplianceModulesetDetails{
		Moduleset:    ComplianceModuleset{ID: row.ID, Name: row.Name, Author: personName(row.Author), Updated: row.Updated},
		Publications: export.Modulesets[own].Publications,
		Responsibles: export.Modulesets[own].Responsibles,
	}
	result.Moduleset.Modules = exportModules(export.Modulesets[own].Modules)
	result.Moduleset.ModulesCount = len(result.Moduleset.Modules)
	if len(result.Moduleset.Modules) > maxModulesetModules {
		result.Moduleset.Modules, result.Moduleset.ModulesTruncated = result.Moduleset.Modules[:maxModulesetModules], true
	}
	// The included modulesets, at any depth, in the order of the tree.
	seen := map[string]bool{row.Name: true}
	queue := slices.Clone(export.Modulesets[own].Modulesets)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		i, ok := byName[name]
		if seen[name] || !ok {
			continue
		}
		seen[name] = true
		result.Included = append(result.Included, IncludedModuleset{Name: name, Modules: exportModules(export.Modulesets[i].Modules)})
		queue = append(queue, export.Modulesets[i].Modulesets...)
	}
	attachedTo, includedBy := map[string][]string{}, map[string][]string{}
	for _, name := range append([]string{row.Name}, includedNames(result.Included)...) {
		for _, ruleset := range export.Modulesets[byName[name]].Rulesets {
			attachedTo[ruleset] = append(attachedTo[ruleset], name)
		}
	}
	for _, ruleset := range export.Rulesets {
		for _, child := range ruleset.Rulesets {
			includedBy[child] = append(includedBy[child], ruleset.Name)
		}
	}
	for _, ruleset := range export.Rulesets {
		item := ModulesetRuleset{
			Name: ruleset.Name, Type: ruleset.Type, Variables: len(ruleset.Variables),
			AttachedTo: attachedTo[ruleset.Name], IncludedBy: includedBy[ruleset.Name],
		}
		if ruleset.Filterset != nil {
			item.Filterset = *ruleset.Filterset
		}
		result.Rulesets = append(result.Rulesets, item)
	}
	sort.Slice(result.Rulesets, func(i, j int) bool { return result.Rulesets[i].Name < result.Rulesets[j].Name })

	var usage struct {
		Data struct {
			Modulesets []collectorModuleset `json:"modulesets"`
		} `json:"data"`
	}
	if err := s.client.GetJSON(ctx, path+"/usage", nil, &usage); err != nil {
		return ComplianceModulesetDetails{}, fmt.Errorf("get usage of moduleset %s: %w", row.Name, err)
	}
	for _, parent := range usage.Data.Modulesets {
		result.Parents = append(result.Parents, parent.Name)
	}
	if result.NodesCount, result.Nodes, err = s.attachedNames(ctx, path+"/nodes", "nodename", "nodename,node_id", false); err != nil {
		return ComplianceModulesetDetails{}, fmt.Errorf("list nodes of moduleset %s: %w", row.Name, err)
	}
	if result.ServicesCount, result.Services, err = s.attachedNames(ctx, path+"/services", "svcname", "svcname,svc_id", false); err != nil {
		return ComplianceModulesetDetails{}, fmt.Errorf("list services of moduleset %s: %w", row.Name, err)
	}
	if result.EncapsulatedCount, result.Encapsulated, err = s.attachedNames(ctx, path+"/services", "svcname", "svcname,svc_id", true); err != nil {
		return ComplianceModulesetDetails{}, fmt.Errorf("list encapsulated services of moduleset %s: %w", row.Name, err)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// attachedNames reads the number of the nodes or services a compliance
// object is attached to, and the first of their names; with slave, the
// services it is attached to as their encapsulated service.
func (s *Service) attachedNames(ctx context.Context, path, prop, orderby string, slave bool) (int, []string, error) {
	query := pageQuery([]string{prop}, orderby, nil, maxModulesetAttachments, 0)
	if slave {
		query.Set("slave", "true")
	}
	var response collectorList[map[string]any]
	if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
		return 0, nil, err
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > maxModulesetAttachments {
		return 0, nil, fmt.Errorf("unexpected collector response")
	}
	names := make([]string, 0, len(response.Data))
	for _, row := range response.Data {
		names = append(names, propText(row, prop))
	}
	return *response.Meta.Total, names, nil
}

func includedNames(included []IncludedModuleset) []string {
	names := make([]string, 0, len(included))
	for _, item := range included {
		names = append(names, item.Name)
	}
	return names
}

func exportModules(rows []collectorModulesetModule) []ComplianceModule {
	modules := make([]ComplianceModule, 0, len(rows))
	for _, row := range rows {
		if row.Name != "" {
			modules = append(modules, ComplianceModule{Name: row.Name, Autofix: collectorBool(row.Autofix)})
		}
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	return modules
}

func joinInts(values []int) string {
	text := make([]string, 0, len(values))
	for _, value := range values {
		text = append(text, strconv.Itoa(value))
	}
	return strings.Join(text, ",")
}

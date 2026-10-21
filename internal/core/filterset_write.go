package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// maxFiltersetCriteria bounds the criteria of a filterset read to change
// one, and those one creation takes.
const maxFiltersetCriteria = 200

// The logics, tables and operators the collector takes in a criterion, as
// server/handlers/post_filters.go of oc3 lists them.
var (
	FiltersetLogics = []string{"AND", "AND NOT", "OR", "OR NOT"}
	FilterTables    = []string{"nodes", "node_ip", "services", "svcmon", "resmon", "apps", "node_hba", "diskinfo", "svcdisks", "v_comp_moduleset_attachments", "v_tags", "packages"}
	FilterOperators = []string{"=", "LIKE", ">", ">=", "<", "<=", "IN"}
)

// Outcomes of a change of a filterset.
const (
	FiltersetCreated   = "created"
	FiltersetChanged   = "changed"
	FiltersetUnchanged = "unchanged"
	FiltersetAdded     = "added"
	FiltersetRemoved   = "removed"
	FiltersetDeleted   = "deleted"
)

// FiltersetCriterionInput is one criterion of a filterset: a filter on a
// column of a table, or an included filterset, joined to the criteria before
// it by Logic.
type FiltersetCriterionInput struct {
	Logic     string
	Table     string
	Field     string
	Op        string
	Value     string
	Filterset string
}

type FiltersetWrite struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome    string            `json:"outcome" jsonschema:"created, changed, added or removed (a criterion), unchanged when the filterset is already as asked, or deleted"`
	Filterset  *FiltersetDetails `json:"read_back,omitempty" jsonschema:"the filterset read back after the change, as get_filterset gives it: its criteria, the nodes and services it selects now, and what uses it; with delete_filterset, as it was before the deletion"`
	Warning    string            `json:"warning,omitempty" jsonschema:"why the filterset could not be read back, the change being made; get_filterset reads it"`
}

func (criterion FiltersetCriterionInput) isFilter() bool { return criterion.Filterset == "" }

// entry is a criterion as the collector takes it in the entries of a
// creation, with the fields given: one naming a filter and a filterset at
// once goes as it is, for the collector to decide.
func (criterion FiltersetCriterionInput) entry() map[string]any {
	entry := map[string]any{"f_log_op": criterion.Logic}
	for key, value := range map[string]string{"f_table": criterion.Table, "f_field": criterion.Field, "f_op": criterion.Op, "f_value": criterion.Value, "filterset": criterion.Filterset} {
		if value != "" {
			entry[key] = value
		}
	}
	return entry
}

// CreateFilterset creates a filterset and its criteria in one transaction
// of the collector, which reuses the filters of the same definition and
// creates the others, and reads it back as get_filterset gives it.
func (s *Service) CreateFilterset(ctx context.Context, name string, criteria []FiltersetCriterionInput) (FiltersetWrite, error) {
	if len(criteria) > maxFiltersetCriteria {
		return FiltersetWrite{}, fmt.Errorf("pass at most %d criteria", maxFiltersetCriteria)
	}
	entries := make([]map[string]any, 0, len(criteria))
	for _, criterion := range criteria {
		entries = append(entries, criterion.entry())
	}
	var response collectorList[collectorFilterset]
	if err := s.client.PostJSON(ctx, "/filtersets", map[string]any{"fset_name": name, "entries": entries}, &response); err != nil {
		return FiltersetWrite{}, fmt.Errorf("create filterset %s: %w", name, err)
	}
	if len(response.Data) != 1 || response.Data[0].ID <= 0 {
		return FiltersetWrite{}, fmt.Errorf("create filterset %s: unexpected collector response; list_filtersets tells whether it was created", name)
	}
	return s.filtersetWritten(ctx, FiltersetCreated, response.Data[0].ID), nil
}

// RenameFilterset renames a filterset: what uses it refers to its ID.
func (s *Service) RenameFilterset(ctx context.Context, filterset, name string) (FiltersetWrite, error) {
	found, err := s.findFilterset(ctx, filterset)
	if err != nil {
		return FiltersetWrite{}, err
	}
	// The name of the filterset already: nothing is sent, the collector
	// logging a change of nothing otherwise.
	if found.Name == name {
		return s.filtersetWritten(ctx, FiltersetUnchanged, found.ID), nil
	}
	var response collectorList[collectorFilterset]
	if err := s.client.PostJSON(ctx, "/filtersets/"+strconv.Itoa(found.ID), map[string]any{"fset_name": name}, &response); err != nil {
		return FiltersetWrite{}, fmt.Errorf("rename filterset %s: %w", found.Name, err)
	}
	return s.filtersetWritten(ctx, FiltersetChanged, found.ID), nil
}

// AddFiltersetCriterion attaches a criterion to a filterset, or changes the
// logic and the order of one attached already. A filter is attached by its
// ID: the MCP finds the filter of the definition, or creates it. A new
// criterion without order goes last.
func (s *Service) AddFiltersetCriterion(ctx context.Context, filterset string, criterion FiltersetCriterionInput, order *int) (FiltersetWrite, error) {
	parent, err := s.findFilterset(ctx, filterset)
	if err != nil {
		return FiltersetWrite{}, err
	}
	attached, err := s.filtersetAttachments(ctx, parent.ID)
	if err != nil {
		return FiltersetWrite{}, err
	}
	current, child, err := s.attachedCriterion(ctx, attached, criterion)
	if err != nil {
		return FiltersetWrite{}, err
	}
	base := "/filtersets/" + strconv.Itoa(parent.ID)
	body := map[string]any{"f_log_op": criterion.Logic}
	outcome := FiltersetAdded
	var path string
	switch {
	case current != nil:
		// Attached already: the order is kept unless given.
		if current.Logic == criterion.Logic && (order == nil || *order == current.Order) {
			return s.filtersetWritten(ctx, FiltersetUnchanged, parent.ID), nil
		}
		outcome, path = FiltersetChanged, current.path(base)
	case criterion.isFilter():
		id, err := s.filterID(ctx, criterion)
		if err != nil {
			return FiltersetWrite{}, err
		}
		path = base + "/filters/" + strconv.Itoa(id)
	default:
		path = base + "/filtersets/" + strconv.Itoa(child)
	}
	switch {
	case order != nil:
		body["f_order"] = *order
	case current == nil:
		last := 0
		for _, item := range attached {
			last = max(last, item.Order)
		}
		body["f_order"] = last + 1
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.PostJSON(ctx, path, body, &response); err != nil {
		return FiltersetWrite{}, fmt.Errorf("add a criterion to filterset %s: %w", parent.Name, err)
	}
	return s.filtersetWritten(ctx, outcome, parent.ID), nil
}

// RemoveFiltersetCriterion detaches a criterion from a filterset. The
// filter itself stays, for the other filtersets using it.
func (s *Service) RemoveFiltersetCriterion(ctx context.Context, filterset string, criterion FiltersetCriterionInput) (FiltersetWrite, error) {
	parent, err := s.findFilterset(ctx, filterset)
	if err != nil {
		return FiltersetWrite{}, err
	}
	attached, err := s.filtersetAttachments(ctx, parent.ID)
	if err != nil {
		return FiltersetWrite{}, err
	}
	current, _, err := s.attachedCriterion(ctx, attached, criterion)
	if err != nil {
		return FiltersetWrite{}, err
	}
	if current == nil {
		return s.filtersetWritten(ctx, FiltersetUnchanged, parent.ID), nil
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, current.path("/filtersets/"+strconv.Itoa(parent.ID)), nil, &response); err != nil {
		return FiltersetWrite{}, fmt.Errorf("remove a criterion of filterset %s: %w", parent.Name, err)
	}
	// The collector answers a criterion detached meanwhile with 200.
	outcome := FiltersetRemoved
	if strings.Contains(response.Info, "already detached") {
		outcome = FiltersetUnchanged
	}
	return s.filtersetWritten(ctx, outcome, parent.ID), nil
}

// DeleteFilterset deletes a filterset, with what refers to it: its
// criteria, its inclusions in other filtersets, the contextual rulesets and
// check thresholds, the session filters, the statistics comparisons and the
// sysreport grants using it. It is read first, for the result to tell what
// used it.
func (s *Service) DeleteFilterset(ctx context.Context, filterset string) (FiltersetWrite, error) {
	before, err := s.GetFilterset(ctx, filterset)
	if err != nil {
		return FiltersetWrite{}, err
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, "/filtersets/"+strconv.Itoa(before.Filterset.ID), nil, &response); err != nil {
		return FiltersetWrite{}, fmt.Errorf("delete filterset %s: %w", before.Filterset.Name, err)
	}
	return FiltersetWrite{Provenance: s.newProvenance(), Outcome: FiltersetDeleted, Filterset: &before}, nil
}

// findFilterset reads one filterset by its exact name or ID.
func (s *Service) findFilterset(ctx context.Context, filterset string) (collectorFilterset, error) {
	if filterset == "" || strings.Contains(filterset, "/") {
		return collectorFilterset{}, fmt.Errorf("filterset must be one exact filterset name or filterset ID")
	}
	if err := validateFilterValue("filterset", filterset); err != nil {
		return collectorFilterset{}, err
	}
	return s.lookupFilterset(ctx, filterset)
}

// filtersetAttachment is a criterion of a filterset as attached: a filter
// by its ID, or an included filterset by its ID.
type filtersetAttachment struct {
	FilterID    int
	FiltersetID int
	Table       string
	Field       string
	Op          string
	Value       string
	Logic       string
	Order       int
}

func (attachment filtersetAttachment) path(base string) string {
	if attachment.FiltersetID != 0 {
		return base + "/filtersets/" + strconv.Itoa(attachment.FiltersetID)
	}
	return base + "/filters/" + strconv.Itoa(attachment.FilterID)
}

// filtersetAttachments reads the criteria of a filterset: its filters, and
// its included filtersets, which its export names with their order and
// logic.
func (s *Service) filtersetAttachments(ctx context.Context, id int) ([]filtersetAttachment, error) {
	path := "/filtersets/" + strconv.Itoa(id)
	var filters collectorList[struct {
		ID    int    `json:"id"`
		Table string `json:"f_table"`
		Field string `json:"f_field"`
		Op    string `json:"f_op"`
		Value string `json:"f_value"`
		Logic string `json:"f_log_op"`
		Order int    `json:"f_order"`
	}]
	query := pageQuery([]string{"id", "f_table", "f_field", "f_op", "f_value", "f_log_op", "f_order"}, "f_order,id", nil, maxFiltersetCriteria, 0)
	if err := s.client.GetJSON(ctx, path+"/filters", query, &filters); err != nil {
		return nil, fmt.Errorf("read the criteria of filterset %d: %w", id, err)
	}
	var attached []filtersetAttachment
	for _, row := range filters.Data {
		attached = append(attached, filtersetAttachment{FilterID: row.ID, Table: row.Table, Field: row.Field, Op: row.Op, Value: row.Value, Logic: row.Logic, Order: row.Order})
	}
	var export collectorFiltersetExport
	if err := s.client.GetJSON(ctx, path+"/export", nil, &export); err != nil {
		return nil, fmt.Errorf("read the criteria of filterset %d: %w", id, err)
	}
	for _, item := range export.Filtersets {
		if item.ID != id {
			continue
		}
		for _, filter := range item.Filters {
			if filter.Filterset == nil {
				continue
			}
			child, err := s.lookupFilterset(ctx, *filter.Filterset)
			if err != nil {
				return nil, err
			}
			attached = append(attached, filtersetAttachment{FiltersetID: child.ID, Logic: filter.LogicOp, Order: filter.Order})
		}
	}
	return attached, nil
}

// attachedCriterion finds a criterion among the attachments of a filterset,
// the filter definition compared case-insensitively as the collector
// compares it; for an included filterset, it gives its ID.
func (s *Service) attachedCriterion(ctx context.Context, attached []filtersetAttachment, criterion FiltersetCriterionInput) (*filtersetAttachment, int, error) {
	child := 0
	if !criterion.isFilter() {
		found, err := s.findFilterset(ctx, criterion.Filterset)
		if err != nil {
			return nil, 0, err
		}
		child = found.ID
	}
	for index, item := range attached {
		if criterion.isFilter() && item.FiltersetID == 0 && strings.EqualFold(item.Table, criterion.Table) && strings.EqualFold(item.Field, criterion.Field) &&
			strings.EqualFold(item.Op, criterion.Op) && strings.EqualFold(item.Value, criterion.Value) {
			return &attached[index], 0, nil
		}
		if !criterion.isFilter() && item.FiltersetID == child {
			return &attached[index], child, nil
		}
	}
	return nil, child, nil
}

// filterID gives the ID of the filter of a definition, created when none
// holds it: the collector attaches a filter by its ID.
func (s *Service) filterID(ctx context.Context, criterion FiltersetCriterionInput) (int, error) {
	filters := []string{filterEquals("f_table", criterion.Table), filterEquals("f_field", criterion.Field), filterEquals("f_op", criterion.Op), filterEquals("f_value", criterion.Value)}
	var found collectorList[struct {
		ID    int    `json:"id"`
		Value string `json:"f_value"`
	}]
	if err := s.client.GetJSON(ctx, "/filters", pageQuery([]string{"id", "f_value"}, "id", filters, 10, 0), &found); err != nil {
		return 0, fmt.Errorf("find the filter %s.%s %s %s: %w", criterion.Table, criterion.Field, criterion.Op, criterion.Value, err)
	}
	for _, row := range found.Data {
		if strings.EqualFold(row.Value, criterion.Value) {
			return row.ID, nil
		}
	}
	var created collectorList[struct {
		ID int `json:"id"`
	}]
	body := map[string]any{"f_table": criterion.Table, "f_field": criterion.Field, "f_op": criterion.Op, "f_value": criterion.Value}
	if err := s.client.PostJSON(ctx, "/filters", body, &created); err != nil {
		return 0, fmt.Errorf("create the filter %s.%s %s %s: %w", criterion.Table, criterion.Field, criterion.Op, criterion.Value, err)
	}
	if len(created.Data) != 1 || created.Data[0].ID <= 0 {
		return 0, fmt.Errorf("create the filter %s.%s %s %s: unexpected collector response", criterion.Table, criterion.Field, criterion.Op, criterion.Value)
	}
	return created.Data[0].ID, nil
}

// filtersetWritten reads a filterset back after a change, which is made: a
// read back that fails is told, not returned as an error.
func (s *Service) filtersetWritten(ctx context.Context, outcome string, id int) FiltersetWrite {
	result := FiltersetWrite{Outcome: outcome}
	details, err := s.GetFilterset(ctx, strconv.Itoa(id))
	if err != nil {
		result.Warning = fmt.Sprintf("the filterset could not be read back: %v", err)
	} else {
		result.Filterset = &details
	}
	result.Provenance = s.newProvenance()
	return result
}

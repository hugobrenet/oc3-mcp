package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// filtersetWriteCollector holds the filterset demo, 295: the filter
// nodes.loc_city = Paris, filter 231 of rank 1, and the filterset
// big-memory, 283, included with rank 2; and answers the changes with
// writes.
type filtersetWriteCollector struct {
	*fakeCollector
	writes map[string]string
}

func newFiltersetWriteCollector(writes map[string]string) filtersetWriteCollector {
	return filtersetWriteCollector{fakeCollector: &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/filtersets/demo", "/filtersets/295":
			return `{"data":[{"id":295,"fset_name":"demo"}]}`
		case "/filtersets/big-memory":
			return `{"data":[{"id":283,"fset_name":"big-memory"}]}`
		case "/filtersets/295/filters":
			return `{"data":[{"id":231,"f_table":"nodes","f_field":"loc_city","f_op":"=","f_value":"Paris","f_log_op":"AND","f_order":1}],"meta":{"total":1}}`
		case "/filtersets/295/export":
			return `{"filtersets":[{"id":295,"fset_name":"demo","filters":[
				{"f_log_op":"AND","f_order":1,"filter":{"f_table":"nodes","f_field":"loc_city","f_op":"=","f_value":"Paris"},"filterset":null},
				{"f_log_op":"AND NOT","f_order":2,"filter":null,"filterset":"big-memory"}]},
				{"id":283,"fset_name":"big-memory","filters":[{"f_log_op":"AND","f_order":1,"filter":{"f_table":"nodes","f_field":"mem_bytes","f_op":">=","f_value":"131072"},"filterset":null}]}]}`
		case "/filtersets/295/nodes":
			return `{"data":[{"nodename":"par1"}],"meta":{"total":1}}`
		case "/filtersets/295/services", "/filters":
			return `{"data":[],"meta":{"total":0}}`
		case "/filtersets/295/usage":
			return `{"data":{"rulesets":[{"id":1030,"ruleset_name":"ctx.paris"}]}}`
		}
		return `{"data":[]}`
	}}, writes: writes}
}

func (c filtersetWriteCollector) write(path string, output any) error {
	answer, ok := c.writes[path]
	if !ok {
		answer = `{"info":"ok"}`
	}
	return json.Unmarshal([]byte(answer), output)
}

func (c filtersetWriteCollector) PostJSON(ctx context.Context, path string, input any, output any) error {
	_ = c.fakeCollector.PostJSON(ctx, path, input, nil)
	return c.write(path, output)
}

func (c filtersetWriteCollector) DeleteJSON(ctx context.Context, path string, query url.Values, output any) error {
	_ = c.fakeCollector.DeleteJSON(ctx, path, query, nil)
	return c.write(path, output)
}

func (c filtersetWriteCollector) changes() []fakeRequest {
	var changes []fakeRequest
	for _, request := range c.requests {
		if request.method != "GET" {
			changes = append(changes, request)
		}
	}
	return changes
}

func TestCreateFiltersetSendsTheEntries(t *testing.T) {
	collector := newFiltersetWriteCollector(map[string]string{"/filtersets": `{"data":[{"id":295,"fset_name":"demo"}]}`})
	result, err := New(collector).CreateFilterset(t.Context(), "demo", []FiltersetCriterionInput{
		{Logic: "AND", Table: "nodes", Field: "loc_city", Op: "=", Value: "Paris"}, {Logic: "AND NOT", Filterset: "big-memory"}})
	if err != nil {
		t.Fatal(err)
	}
	changes := collector.changes()
	entries, _ := json.Marshal(changes[0].body["entries"])
	if len(changes) != 1 || changes[0].path != "/filtersets" || changes[0].body["fset_name"] != "demo" ||
		string(entries) != `[{"f_field":"loc_city","f_log_op":"AND","f_op":"=","f_table":"nodes","f_value":"Paris"},{"f_log_op":"AND NOT","filterset":"big-memory"}]` {
		t.Fatalf("changes = %+v", changes)
	}
	if result.Outcome != FiltersetCreated || result.Filterset == nil || result.Filterset.NodesCount != 1 || len(result.Filterset.Criteria) != 2 {
		t.Fatalf("result = %+v", result)
	}
	// No criteria are sent as an empty list: the collector creates the
	// filterset, which selects nothing.
	collector = newFiltersetWriteCollector(map[string]string{"/filtersets": `{"data":[{"id":295,"fset_name":"demo"}]}`})
	if _, err := New(collector).CreateFilterset(t.Context(), "demo", nil); err != nil {
		t.Fatal(err)
	}
	if entries, _ := json.Marshal(collector.changes()[0].body["entries"]); string(entries) != `[]` {
		t.Fatalf("entries = %s", entries)
	}
}

func TestAddFiltersetCriterion(t *testing.T) {
	// A new filter: created, then attached last.
	collector := newFiltersetWriteCollector(map[string]string{"/filters": `{"data":[{"id":260}]}`})
	result, err := New(collector).AddFiltersetCriterion(t.Context(), "demo", FiltersetCriterionInput{Logic: "OR", Table: "nodes", Field: "loc_city", Op: "=", Value: "London"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	changes := collector.changes()
	if len(changes) != 2 || changes[0].path != "/filters" || changes[0].body["f_value"] != "London" ||
		changes[1].path != "/filtersets/295/filters/260" || changes[1].body["f_log_op"] != "OR" || changes[1].body["f_order"] != float64(3) || result.Outcome != FiltersetAdded {
		t.Fatalf("changes = %+v, result = %+v", changes, result)
	}
	// A filter attached already, of the same logic: nothing sent, its case
	// as the collector compares it.
	collector = newFiltersetWriteCollector(nil)
	result, err = New(collector).AddFiltersetCriterion(t.Context(), "demo", FiltersetCriterionInput{Logic: "AND", Table: "nodes", Field: "loc_city", Op: "=", Value: "paris"}, nil)
	if err != nil || result.Outcome != FiltersetUnchanged || len(collector.changes()) != 0 {
		t.Fatalf("attached: %+v, %v, %+v", result, err, collector.changes())
	}
	// Another logic: changed, its rank kept.
	collector = newFiltersetWriteCollector(nil)
	result, err = New(collector).AddFiltersetCriterion(t.Context(), "demo", FiltersetCriterionInput{Logic: "OR", Table: "nodes", Field: "loc_city", Op: "=", Value: "Paris"}, nil)
	changes = collector.changes()
	if err != nil || result.Outcome != FiltersetChanged || len(changes) != 1 || changes[0].path != "/filtersets/295/filters/231" || changes[0].body["f_order"] != nil {
		t.Fatalf("logic: %+v, %v, %+v", result, err, changes)
	}
	// An included filterset attached already, moved.
	collector = newFiltersetWriteCollector(nil)
	order := 0
	result, err = New(collector).AddFiltersetCriterion(t.Context(), "demo", FiltersetCriterionInput{Logic: "AND NOT", Filterset: "big-memory"}, &order)
	changes = collector.changes()
	if err != nil || result.Outcome != FiltersetChanged || len(changes) != 1 || changes[0].path != "/filtersets/295/filtersets/283" || changes[0].body["f_order"] != float64(0) {
		t.Fatalf("included: %+v, %v, %+v", result, err, changes)
	}
}

func TestRemoveFiltersetCriterion(t *testing.T) {
	collector := newFiltersetWriteCollector(map[string]string{"/filtersets/295/filters/231": `{"info":"filter detached"}`})
	result, err := New(collector).RemoveFiltersetCriterion(t.Context(), "demo", FiltersetCriterionInput{Table: "nodes", Field: "loc_city", Op: "=", Value: "Paris"})
	changes := collector.changes()
	if err != nil || result.Outcome != FiltersetRemoved || len(changes) != 1 || changes[0].method != "DELETE" || changes[0].path != "/filtersets/295/filters/231" {
		t.Fatalf("remove: %+v, %v, %+v", result, err, changes)
	}
	collector = newFiltersetWriteCollector(map[string]string{"/filtersets/295/filtersets/283": `{"info":"filterset already detached"}`})
	result, err = New(collector).RemoveFiltersetCriterion(t.Context(), "demo", FiltersetCriterionInput{Filterset: "big-memory"})
	if err != nil || result.Outcome != FiltersetUnchanged {
		t.Fatalf("detached meanwhile: %+v, %v", result, err)
	}
	collector = newFiltersetWriteCollector(nil)
	result, err = New(collector).RemoveFiltersetCriterion(t.Context(), "demo", FiltersetCriterionInput{Table: "nodes", Field: "loc_city", Op: "=", Value: "London"})
	if err != nil || result.Outcome != FiltersetUnchanged || len(collector.changes()) != 0 {
		t.Fatalf("not attached: %+v, %v", result, err)
	}
}

// A criterion naming a filter and a filterset at once goes as given in a
// creation, for the collector to decide.
func TestCreateFiltersetSendsAMixedCriterionAsGiven(t *testing.T) {
	collector := newFiltersetWriteCollector(map[string]string{"/filtersets": `{"data":[{"id":295,"fset_name":"demo"}]}`})
	mixed := FiltersetCriterionInput{Logic: "OR", Table: "nodes", Field: "loc_city", Op: "=", Value: "Paris", Filterset: "big-memory"}
	if _, err := New(collector).CreateFilterset(t.Context(), "demo", []FiltersetCriterionInput{mixed}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := json.Marshal(collector.changes()[0].body["entries"]); string(entries) != `[{"f_field":"loc_city","f_log_op":"OR","f_op":"=","f_table":"nodes","f_value":"Paris","filterset":"big-memory"}]` {
		t.Fatalf("entries = %s", entries)
	}
}

func TestRenameAndDeleteFilterset(t *testing.T) {
	collector := newFiltersetWriteCollector(map[string]string{"/filtersets/295": `{"data":[{"id":295,"fset_name":"demo2"}]}`})
	result, err := New(collector).RenameFilterset(t.Context(), "demo", "demo2")
	if changes := collector.changes(); err != nil || result.Outcome != FiltersetChanged || changes[0].body["fset_name"] != "demo2" {
		t.Fatalf("rename: %+v, %v", result, err)
	}
	// The same name: nothing sent.
	collector = newFiltersetWriteCollector(nil)
	result, err = New(collector).RenameFilterset(t.Context(), "demo", "demo")
	if err != nil || result.Outcome != FiltersetUnchanged || len(collector.changes()) != 0 {
		t.Fatalf("same name: %+v, %v", result, err)
	}
	collector = newFiltersetWriteCollector(map[string]string{"/filtersets/295": `{"info":"filterset deleted"}`})
	result, err = New(collector).DeleteFilterset(t.Context(), "demo")
	changes := collector.changes()
	if err != nil || result.Outcome != FiltersetDeleted || len(changes) != 1 || changes[0].method != "DELETE" || changes[0].path != "/filtersets/295" ||
		result.Filterset == nil || strings.Join(result.Filterset.Rulesets, ",") != "ctx.paris" {
		t.Fatalf("delete: %+v, %v, %+v", result, err, changes)
	}
}

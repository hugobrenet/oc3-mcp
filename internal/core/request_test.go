package core

import (
	"strings"
	"testing"
)

func TestListRequestsTranslatesTheFiltersAndTrimsTheNames(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/workflows": `{"data":[{"id":1355,"form_name":"sample-firewall-flow","form_folder":"/samples","last_form_name":"sample-firewall-review",
			"status":"pending","steps":2,"creator":" Manager","create_date":"2026-09-22 13:04:18","last_assignee":"Sebastien Guiheneuf ","last_update":"2026-09-22 18:04:18"}],
			"meta":{"total":3}}`,
	}}
	requests, err := newTestService(collector).ListRequests(t.Context(), ListRequestsOptions{
		Status: "pending", Assignee: "net.work", Creator: "Manager", Form: "sample-firewall-flow", CreatedSince: "2026-09-01", CreatedUntil: "2026-10-01",
		UpdatedSince: "2026-09-20 10:00", UpdatedUntil: "2026-10-01", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/workflows" || request.query.Get("orderby") != "-last_update,-id" || request.query.Get("limit") != "1" ||
		strings.Join(request.query["filter"], "|") != `status:eq:pending|last_assignee:~^[[:space:]]*net\.work[[:space:]]*$|creator:~^[[:space:]]*Manager[[:space:]]*$|form_name:eq:sample-firewall-flow|`+
			`create_date:gte:2026-09-01|create_date:lt:2026-10-01|last_update:gte:2026-09-20 10:00|last_update:lt:2026-10-01` {
		t.Fatalf("request = %+v", request)
	}
	got := requests.Requests[0]
	if requests.Total != 3 || !requests.Truncated || requests.NextCursor == "" || got.ID != 1355 || got.Form != "sample-firewall-flow" ||
		got.LastForm != "sample-firewall-review" || got.Steps != 2 || got.Creator != "Manager" || got.Assignee != "Sebastien Guiheneuf" ||
		requests.Provenance.Source != provenanceSourceOpenSVCCollector {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestListRequestsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListRequestsOptions{
		{Status: " pending"},
		{Status: "open"},
		{ID: -1},
		{CreatedSince: "last week"},
		{UpdatedSince: "2026/09/01"},
		{CreatedUntil: "soon"},
		{UpdatedUntil: "2026-13"},
		{Limit: 201},
		{Cursor: "garbage"},
	} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListRequests(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

func TestListRequestsByID(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"id":1355,"status":"closed","steps":2}],"meta":{"total":1}}`}
	requests, err := newTestService(collector).ListRequests(t.Context(), ListRequestsOptions{ID: 1355})
	if err != nil || collector.requests[0].query.Get("filter") != "id:eq:1355" || requests.Requests[0].ID != 1355 {
		t.Fatalf("requests = %+v, %v, %+v", requests, err, collector.requests)
	}
}

// assigned goes to the collector as it is, which keeps the requests of the
// team of the caller; a cursor is bound to it.
func TestListRequestsAssignedToTheTeamOfTheCaller(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[{"id":1378,"status":"pending","last_assignee":"Manager"}],"meta":{"total":3}}`}
	result, err := newTestService(collector).ListRequests(t.Context(), ListRequestsOptions{Assigned: "team", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if query := collector.requests[0].query; query.Get("assigned") != "team" || len(query["filter"]) != 0 || result.NextCursor == "" {
		t.Fatalf("query = %v, result = %+v", query, result)
	}
	if _, err := newTestService(&fakeCollector{body: `{"data":[],"meta":{"total":0}}`}).ListRequests(t.Context(), ListRequestsOptions{Assigned: "tiers", Cursor: result.NextCursor}); err == nil {
		t.Fatal("a cursor of assigned team was taken for assigned tiers")
	}
}

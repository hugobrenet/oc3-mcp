package core

import (
	"strings"
	"testing"
)

func TestListFormsReadsTheirDefinition(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/forms": `{"data":[
			{"id":198,"form_name":"sample-firewall-flow","form_type":"custo","form_folder":"/samples","form_author":" Manager","form_created":"2026-09-28 13:03:50",
			 "form_definition":{"Label":"Firewall flow opening","Desc":"Request a firewall flow.\n","Inputs":[{"Id":"source"},{"Id":"destination"},{"Id":"port"}],
			 "Outputs":[{"Dest":"workflow","NextForms":["sample-firewall-review"],"NextAssignee":"Network "},{"Dest":"workflow","NextForms":["sample-firewall-review"],"NextAssignee":"Network"}]}},
			{"id":12,"form_name":"user","form_type":"obj","form_folder":"/compliance","form_definition":{"Outputs":[{"Dest":"compliance variable"}]}},
			{"id":13,"form_name":"broken","form_type":"generic","form_definition":null},
			{"id":14,"form_name":"disk","form_type":"generic","form_definition":{"Inputs":[],"Outputs":[{"Dest":"workflow","NextForms":["ignored"],
			 "Scripts":{"Success":{"NextForms":["validation"],"NextAssignee":"Storage"},"Error":{"NextForms":["correction"]}}},{"Dest":"script"}]}},
			{"id":15,"form_name":"closing","form_type":"custo","form_definition":{"Outputs":[{"Dest":"workflow","Scripts":{"Success":{"NextForms":["done"]}}}]}},
			{"id":16,"form_name":"odd","form_type":"custo","form_definition":{"Inputs":"none"}}],"meta":{"total":8}}`,
	}}
	forms, err := newTestService(collector).ListForms(t.Context(), ListFormsOptions{Name: "fire.wall", Type: "custo", Folder: "/samples", Limit: 6})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.path != "/forms" || request.query.Get("orderby") != "form_folder,form_name,id" || strings.Contains(request.query.Get("props"), "form_yaml") ||
		strings.Join(request.query["filter"], "|") != `form_name:~fire\.wall|form_type:eq:custo|form_folder:eq:/samples` {
		t.Fatalf("request = %+v", request)
	}
	flow, user, broken, disk, closing, odd := forms.Forms[0], forms.Forms[1], forms.Forms[2], forms.Forms[3], forms.Forms[4], forms.Forms[5]
	if forms.Total != 8 || !forms.Truncated || forms.NextCursor == "" || flow.Label != "Firewall flow opening" || flow.Description != "Request a firewall flow." ||
		flow.Author != "Manager" || flow.Inputs == nil || *flow.Inputs != 3 || strings.Join(flow.Outputs, ",") != "workflow" || len(flow.NextSteps) != 1 ||
		flow.NextSteps[0].Condition != "always" || strings.Join(flow.NextSteps[0].Forms, ",") != "sample-firewall-review" || flow.NextSteps[0].Assignee != "Network" {
		t.Fatalf("flow = %+v", flow)
	}
	if strings.Join(user.Outputs, ",") != "compliance variable" || user.NextSteps != nil || user.DefinitionUnavailable {
		t.Fatalf("user = %+v", user)
	}
	// Inputs that are not a list hold no input, as the collector reads them.
	if !broken.DefinitionUnavailable || broken.Inputs != nil || odd.DefinitionUnavailable || odd.Inputs == nil || *odd.Inputs != 0 {
		t.Fatalf("broken = %+v, odd = %+v", broken, odd)
	}
	// Scripts replace the direct next forms, by the outcome of the script.
	if *disk.Inputs != 0 || strings.Join(disk.Outputs, ",") != "workflow,script" || len(disk.NextSteps) != 2 ||
		disk.NextSteps[0].Condition != "on_success" || strings.Join(disk.NextSteps[0].Forms, ",") != "validation" || disk.NextSteps[0].Assignee != "Storage" ||
		disk.NextSteps[1].Condition != "on_error" || strings.Join(disk.NextSteps[1].Forms, ",") != "correction" {
		t.Fatalf("disk = %+v", disk)
	}
	// A branch the definition leaves out closes the request.
	if len(closing.NextSteps) != 2 || closing.NextSteps[1].Condition != "on_error" || closing.NextSteps[1].Forms != nil {
		t.Fatalf("closing = %+v", closing)
	}
}

func TestListFormsRejectsInvalidInputs(t *testing.T) {
	for _, options := range []ListFormsOptions{{Type: "request"}, {Name: " padded"}, {Limit: 201}, {Cursor: "garbage"}} {
		collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
		if _, err := newTestService(collector).ListForms(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

// text searches the names, titles and descriptions the MCP reads, then
// pages the forms it keeps.
func TestListFormsSearchesTheTitlesWithText(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/forms": `{"data":[
			{"id":1,"form_name":"sample-vm-request","form_definition":{"Label":"Virtual machine request"}},
			{"id":2,"form_name":"sample-firewall-flow","form_definition":{"Label":"Firewall flow opening"}},
			{"id":3,"form_name":"Amundi_01","form_definition":{"Label":"Add a node SAN disk","Desc":"For a VIRTUAL machine too"}},
			{"id":4,"form_name":"virtual-ip","form_definition":null}],"meta":{"total":1200}}`,
	}}
	forms, err := newTestService(collector).ListForms(t.Context(), ListFormsOptions{Text: "Virtual", Type: "custo", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	request := collector.requests[0]
	if request.query.Get("limit") != "1000" || request.query.Get("offset") != "0" || request.query.Get("filter") != "form_type:eq:custo" {
		t.Fatalf("request = %+v", request)
	}
	if forms.Total != 3 || forms.Count != 2 || !forms.ScanTruncated || !forms.Truncated || forms.Forms[0].Name != "sample-vm-request" || forms.Forms[1].Name != "Amundi_01" {
		t.Fatalf("forms = %+v", forms)
	}
	next, err := newTestService(collector).ListForms(t.Context(), ListFormsOptions{Text: "virtual", Type: "custo", Limit: 2, Cursor: forms.NextCursor})
	if err != nil || next.Count != 1 || next.Forms[0].Name != "virtual-ip" || next.Truncated {
		t.Fatalf("next = %+v, %v", next, err)
	}
	if _, err := newTestService(collector).ListForms(t.Context(), ListFormsOptions{Text: "other", Type: "custo", Cursor: forms.NextCursor}); err == nil {
		t.Fatal("a cursor was accepted with another text")
	}
}

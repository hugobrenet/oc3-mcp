package core

import (
	"net/url"
	"strings"
	"testing"
)

const formYAML = `Label: Add a node SAN disk
Inputs:
    - Id: size
      Label: Size (GB)
      Candidates: ["10", "20"]
    - Id: password
      Key: value
      Type: password
      Default: hunter2-default
Outputs:
    - Dest: rest
      Headers:
        Authorization: Bearer abc.def
      Mail: ops@example.com
      Dest: script
`

func formCollector() *fakeCollector {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		switch path {
		case "/forms":
			if query.Get("filter") == "form_name:eq:dup" {
				return `{"data":[{"id":1,"form_folder":"/a"},{"id":2,"form_folder":"/b"}],"meta":{"total":2}}`
			}
			if query.Get("filter") != "form_name:eq:Amundi_01" && query.Get("filter") != "id:eq:42" {
				return `{"data":[],"meta":{"total":0}}`
			}
			return `{"data":[{"id":42,"form_name":"Amundi_01","form_type":"generic","form_folder":"/","form_author":"Seb ","form_yaml":` + jsonString(formYAML) + `,
				"form_definition":{"Label":"Add a node SAN disk","Inputs":[
					{"Id":"size","Label":"Size (GB)","Candidates":["10","20"],"StrictCandidates":true,"Mandatory":true,"Help":"In GB"},
					{"Id":"node","Label":"Node","Function":"/nodes","Args":["props=nodename"],"Condition":"#size > 10"},
					{"Id":"password","Key":"value","Type":"password","Default":"hunter2-default"},
					{"Id":"contact","Default":"ops@example.com"}],
				"Outputs":[{"Dest":"workflow","NextForms":["Amundi_01_approval"],"NextAssignee":"Storage"},{"Dest":"script"}]}}],"meta":{"total":1}}`
		case "/forms/42/publications":
			return `{"data":[{"role":"Storage"},{"role":"Infra Unix"}]}`
		case "/forms/42/responsibles":
			return `{"data":[{"role":"Storage"}]}`
		}
		return `{}`
	}
	return collector
}

func jsonString(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestGetFormReadsItsInputsGroupsAndMaskedDefinition(t *testing.T) {
	for _, name := range []string{"Amundi_01", "42"} {
		collector := formCollector()
		form, err := newTestService(collector).GetForm(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		if form.Form.Name != "Amundi_01" || form.Form.Author != "Seb" || strings.Join(form.Publications, ",") != "Storage,Infra Unix" ||
			strings.Join(form.Responsibles, ",") != "Storage" || len(form.Form.NextSteps) != 1 || form.Form.NextSteps[0].Assignee != "Storage" {
			t.Fatalf("%s: form = %+v", name, form)
		}
		size, node, password, contact := form.Inputs[0], form.Inputs[1], form.Inputs[2], form.Inputs[3]
		if size.Key != "size" || !size.Mandatory || !size.StrictCandidates || len(size.Candidates) != 2 || size.Candidates[1].Value != "20" || size.Help != "In GB" ||
			node.CandidatesSource != "/nodes" || node.Condition != "#size > 10" || node.Candidates != nil {
			t.Fatalf("inputs = %+v", form.Inputs)
		}
		if password.Key != "value" || password.ID != "password" || !password.Sensitive || password.Default != "(masked)" ||
			contact.Default != "(masked)" || contact.Sensitive {
			t.Fatalf("masked inputs = %+v, %+v", password, contact)
		}
		for _, secret := range []string{"hunter2-default", "Bearer abc.def", "ops@example.com"} {
			if strings.Contains(form.YAML, secret) {
				t.Fatalf("%q left in the definition text:\n%s", secret, form.YAML)
			}
		}
		if !strings.Contains(form.YAML, "Authorization: (masked)") || !strings.Contains(form.YAML, "Label: Size (GB)") || form.YAMLTruncated {
			t.Fatalf("definition text:\n%s", form.YAML)
		}
	}
}

func TestGetFormRefusesUnknownAndAmbiguousNames(t *testing.T) {
	if _, err := newTestService(formCollector()).GetForm(t.Context(), "nope"); err == nil || !strings.Contains(err.Error(), "use list_forms") {
		t.Fatalf("unknown form: %v", err)
	}
	if _, err := newTestService(formCollector()).GetForm(t.Context(), "dup"); err == nil || !strings.Contains(err.Error(), "1 (/a), 2 (/b)") {
		t.Fatalf("ambiguous form: %v", err)
	}
	collector := &fakeCollector{body: `{}`}
	for _, name := range []string{"", " padded"} {
		if _, err := newTestService(collector).GetForm(t.Context(), name); err == nil || len(collector.requests) != 0 {
			t.Fatalf("form %q accepted", name)
		}
	}
}

func TestFormInputsBoundTheirCandidates(t *testing.T) {
	var many []any
	for i := range 80 {
		many = append(many, strings.Repeat("c", 300)+string(rune('a'+i%26)))
	}
	inputs, _, omitted, _ := formInputs([]map[string]any{{"Id": "a", "Candidates": many}})
	if len(inputs[0].Candidates) != maxFormCandidates || omitted != 30 || len(inputs[0].Candidates[0].Value) > maxFormCandidateLen {
		t.Fatalf("%d candidates, %d omitted", len(inputs[0].Candidates), omitted)
	}
}

// Booleans are read as the collector does: Mandatory: Yes stays a string in
// YAML. A candidate object submits its Value.
func TestFormInputsReadTheCollectorBooleansAndCandidateObjects(t *testing.T) {
	inputs, _, _, _ := formInputs([]map[string]any{
		{"Id": "a", "Mandatory": "Yes", "StrictCandidates": "on", "Candidates": []any{map[string]any{"Value": "v1", "Label": "First"}, "v2", map[string]any{"Label": "no value"}}},
		{"Id": "b", "Mandatory": "no"},
		{"Id": "c", "Mandatory": true},
	})
	a, b, c := inputs[0], inputs[1], inputs[2]
	if !a.Mandatory || !a.StrictCandidates || b.Mandatory || !c.Mandatory {
		t.Fatalf("booleans = %+v %+v %+v", a, b, c)
	}
	if a.Candidates[0] != (FormCandidate{Value: "v1", Label: "First"}) || a.Candidates[1] != (FormCandidate{Value: "v2"}) || !strings.Contains(a.Candidates[2].Value, "no value") {
		t.Fatalf("candidates = %+v", a.Candidates)
	}
}

// A definition element that is not an object is left out, as the collector
// does, rather than making the definition unavailable.
func TestFormDefinitionLeavesOutElementsThatAreNotObjects(t *testing.T) {
	form := formOf(collectorForm{ID: 1, FormName: "odd", FormDefinition: []byte(`{"Label":"Odd","Inputs":["x",{"Id":"a"}],"Outputs":[3,{"Dest":"workflow","NextForms":["next"]}]}`)})
	if form.DefinitionUnavailable || *form.Inputs != 1 || len(form.NextSteps) != 1 || form.NextSteps[0].Forms[0] != "next" {
		t.Fatalf("form = %+v", form)
	}
	for _, raw := range []string{"", "null", `"text"`, "{"} {
		if form := formOf(collectorForm{FormDefinition: []byte(raw)}); !form.DefinitionUnavailable || form.Inputs != nil {
			t.Fatalf("definition %q = %+v", raw, form)
		}
	}
}

func TestMaskFormYAMLCoversBlockFlowAndNextLineValues(t *testing.T) {
	text := `Label: Account
Password: |
    first-secret-line
    second-secret-line
Note: kept
token:
    next-line-secret
Settings: {password: flow-secret, user: kept-user}
Default: none
Inputs:
    - Id: answer
      Default: none
`
	masked, _ := maskFormYAML(text, []string{"none"})
	for _, secret := range []string{"first-secret-line", "second-secret-line", "next-line-secret", "flow-secret"} {
		if strings.Contains(masked, secret) {
			t.Fatalf("%q left in:\n%s", secret, masked)
		}
	}
	for _, kept := range []string{"Note: kept", "user: kept-user", "Label: Account"} {
		if !strings.Contains(masked, kept) {
			t.Fatalf("%q masked in:\n%s", kept, masked)
		}
	}
	// A masked default is masked as a value only.
	if strings.Count(masked, "Default: (masked)") != 2 {
		t.Fatalf("defaults in:\n%s", masked)
	}
	if masked, _ := maskFormYAML("Desc: nonexistent nonetheless\n", []string{"none"}); !strings.Contains(masked, "nonexistent nonetheless") {
		t.Fatalf("a default masked inside words: %s", masked)
	}
}

// A number is a form ID first, then a form name.
func TestGetFormFallsBackFromAnIDToAName(t *testing.T) {
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		if path == "/forms" && query.Get("filter") == "form_name:eq:2026" {
			return `{"data":[{"id":7,"form_name":"2026","form_definition":{"Label":"Year"}}],"meta":{"total":1}}`
		}
		if path == "/forms/7/publications" {
			var groups []string
			for i := range 101 {
				groups = append(groups, `{"role":"g`+string(rune('a'+i%26))+`"}`)
			}
			return `{"data":[` + strings.Join(groups, ",") + `]}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}
	form, err := newTestService(collector).GetForm(t.Context(), "2026")
	if err != nil || form.Form.ID != 7 || collector.requests[0].query.Get("filter") != "id:eq:2026" {
		t.Fatalf("form = %+v, %v, %+v", form, err, collector.requests)
	}
	if len(form.Publications) != maxFormGroups || !form.GroupsTruncated || collector.requests[2].query.Get("limit") != "101" {
		t.Fatalf("%d publications, truncated %v", len(form.Publications), form.GroupsTruncated)
	}
}

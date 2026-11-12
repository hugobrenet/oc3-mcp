package core

import (
	"net/url"
	"strings"
	"testing"
)

// aiCollector answers the groups ops (4) and Ops Team (7), and the resource
// naming as an AIManager reads it, published to group 4; the PUT and POST
// echo a stored object.
func aiCollector() *fakeCollector {
	collector := &fakeCollector{}
	collector.answer = func(path string, _ url.Values) string {
		last := collector.requests[len(collector.requests)-1]
		switch {
		case path == "/groups/ops" || path == "/groups/4":
			return `{"data":[{"id":4,"role":"ops"}]}`
		case path == "/groups/Ops%20Team" || path == "/groups/7":
			return `{"data":[{"id":7,"role":"Ops Team"}]}`
		case last.method == "GET" && path == "/ai/resources/naming":
			return `{"name":"naming","title":"Naming","description":"The naming rules.","mime_type":"text/markdown","backends":["oc3"],
				"publications":[4],"size":40,"digest":"sha256:x","updated":"2026-11-12T10:00:00Z",
				"body":"# Naming\n\nClusters: <site>-<env>.\nNodes: <cluster>n<n>.\n"}`
		case last.method == "GET" && path == "/ai/skills/check":
			return `{"name":"check","backends":["oc3"],"size":10,"digest":"sha256:y","updated":"2026-11-12T10:00:00Z","body":"---\nname: check\n---\n"}`
		}
		return `{"name":"saved","backends":["oc3"],"publications":[],"size":1,"digest":"sha256:z","updated":"2026-11-12T10:00:00Z"}`
	}
	return collector
}

func lastWrite(t *testing.T, collector *fakeCollector, method string) fakeRequest {
	t.Helper()
	for i := len(collector.requests) - 1; i >= 0; i-- {
		if collector.requests[i].method == method {
			return collector.requests[i]
		}
	}
	t.Fatalf("no %s request in %+v", method, collector.requests)
	return fakeRequest{}
}

func ptr(s string) *string { return &s }

func TestCreateAIResourcePublishesToTheGroupsGiven(t *testing.T) {
	collector := aiCollector()
	result, err := newTestService(collector).CreateAIContent(t.Context(), AIContentOptions{Kind: AIResources, Name: "naming",
		Description: ptr("The naming rules."), Body: ptr("# Naming\n"), Backends: []string{"oc3", "om3"}, Groups: []string{"ops", "Ops Team", "4"}})
	if err != nil {
		t.Fatal(err)
	}
	post := lastWrite(t, collector, "POST")
	if post.path != "/ai/resources" || post.body["name"] != "naming" || post.body["description"] != "The naming rules." {
		t.Fatalf("post = %+v", post)
	}
	if ids, _ := post.body["publications"].([]any); len(ids) != 2 || ids[0] != float64(4) || ids[1] != float64(7) {
		t.Fatalf("publications = %v, want the two groups once each", post.body["publications"])
	}
	if result.Outcome != "created" || len(result.Groups) != 2 || result.Groups[1].Name != "Ops Team" || result.Everyone == nil || *result.Everyone {
		t.Fatalf("result = %+v", result)
	}
}

func TestCreateAIContentPublishesToEverybodyOnlyWhenAsked(t *testing.T) {
	collector := aiCollector()
	service := newTestService(collector)
	base := AIContentOptions{Kind: AISkills, Name: "check", Body: ptr("---\nname: check\n---\n"), Backends: []string{"oc3"}}
	if _, err := service.CreateAIContent(t.Context(), base); err == nil || !strings.Contains(err.Error(), "everybody") {
		t.Fatalf("a skill without groups was created: %v", err)
	}
	if len(collector.requests) != 0 {
		t.Fatalf("the collector was called: %+v", collector.requests)
	}
	everybody := base
	everybody.Everyone = true
	result, err := service.CreateAIContent(t.Context(), everybody)
	if err != nil {
		t.Fatal(err)
	}
	if ids, ok := lastWrite(t, collector, "POST").body["publications"].([]any); !ok || len(ids) != 0 || result.Everyone == nil || !*result.Everyone {
		t.Fatalf("publications = %v, everybody %v", ids, result.Everyone)
	}
	both := everybody
	both.Groups = []string{"ops"}
	if _, err := service.CreateAIContent(t.Context(), both); err == nil {
		t.Fatal("groups and everybody together were accepted")
	}
}

func TestCreateAIContentRejectsInvalidInputs(t *testing.T) {
	for name, options := range map[string]AIContentOptions{
		"bad name":            {Kind: AIResources, Name: "Naming", Description: ptr("d"), Body: ptr("b"), Backends: []string{"oc3"}, Everyone: true},
		"no body":             {Kind: AIResources, Name: "naming", Description: ptr("d"), Backends: []string{"oc3"}, Everyone: true},
		"no description":      {Kind: AIResources, Name: "naming", Body: ptr("b"), Backends: []string{"oc3"}, Everyone: true},
		"no backend":          {Kind: AIResources, Name: "naming", Description: ptr("d"), Body: ptr("b"), Everyone: true},
		"bad backend":         {Kind: AIResources, Name: "naming", Description: ptr("d"), Body: ptr("b"), Backends: []string{"om2"}, Everyone: true},
		"skill with a title":  {Kind: AISkills, Name: "check", Title: ptr("t"), Body: ptr("b"), Backends: []string{"oc3"}, Everyone: true},
		"creation with edits": {Kind: AISkills, Name: "check", Body: ptr("b"), Edits: []AIEdit{{Old: "a", New: "b"}}, Backends: []string{"oc3"}, Everyone: true},
		"unknown kind":        {Kind: "prompts", Name: "check", Body: ptr("b"), Backends: []string{"oc3"}, Everyone: true},
	} {
		collector := aiCollector()
		if _, err := newTestService(collector).CreateAIContent(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("%s: accepted (%v), requests %+v", name, err, collector.requests)
		}
	}
}

func TestUpdateAIResourceAppliesTheEditsAndKeepsTheRest(t *testing.T) {
	collector := aiCollector()
	result, err := newTestService(collector).UpdateAIContent(t.Context(), AIContentOptions{Kind: AIResources, Name: "naming",
		Edits: []AIEdit{{Old: "<site>-<env>", New: "<site>-<env>-<usage><nn>"}, {Old: "Nodes: <cluster>n<n>.\n", New: ""}}})
	if err != nil {
		t.Fatal(err)
	}
	put := lastWrite(t, collector, "PUT")
	if put.path != "/ai/resources/naming" || put.body["body"] != "# Naming\n\nClusters: <site>-<env>-<usage><nn>.\n" ||
		put.body["title"] != "Naming" || put.body["description"] != "The naming rules." || put.body["mime_type"] != "text/markdown" {
		t.Fatalf("put = %+v", put)
	}
	if ids, _ := put.body["publications"].([]any); len(ids) != 1 || ids[0] != float64(4) {
		t.Fatalf("publications = %v, want the ones kept", put.body["publications"])
	}
	if result.Edits != 2 || result.Outcome != "updated" || len(result.Groups) != 1 || result.Groups[0].Name != "ops" {
		t.Fatalf("result = %+v", result)
	}
}

func TestUpdateAIContentRefusesEditsThatDoNotMatchOnce(t *testing.T) {
	for name, edit := range map[string]AIEdit{
		"missing":  {Old: "Services:", New: "x"},
		"repeated": {Old: "<", New: "["},
		"empty":    {Old: "", New: "x"},
	} {
		collector := aiCollector()
		_, err := newTestService(collector).UpdateAIContent(t.Context(), AIContentOptions{Kind: AIResources, Name: "naming", Edits: []AIEdit{edit}})
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		for _, request := range collector.requests {
			if request.method == "PUT" {
				t.Fatalf("%s: the text was written", name)
			}
		}
	}
}

func TestUpdateAIContentNeedsTheEditorView(t *testing.T) {
	// A reader gets no publications: the MCP does not write back what it
	// cannot read whole, which would publish the text to everybody.
	collector := aiCollector()
	_, err := newTestService(collector).UpdateAIContent(t.Context(), AIContentOptions{Kind: AISkills, Name: "check", Backends: []string{"oc3", "om3"}})
	if err == nil || !strings.Contains(err.Error(), "AIManager") {
		t.Fatalf("err = %v", err)
	}
	if lastRequest := collector.requests[len(collector.requests)-1]; lastRequest.method != "GET" {
		t.Fatalf("the skill was written: %+v", lastRequest)
	}
}

func TestUpdateAIContentReplacesTheGroups(t *testing.T) {
	collector := aiCollector()
	if _, err := newTestService(collector).UpdateAIContent(t.Context(), AIContentOptions{Kind: AIResources, Name: "naming", Everyone: true}); err != nil {
		t.Fatal(err)
	}
	if ids, ok := lastWrite(t, collector, "PUT").body["publications"].([]any); !ok || len(ids) != 0 {
		t.Fatalf("publications = %v", ids)
	}
	if _, err := newTestService(aiCollector()).UpdateAIContent(t.Context(), AIContentOptions{Kind: AIResources, Name: "naming"}); err == nil {
		t.Fatal("a change of nothing was accepted")
	}
}

func TestDeleteAIContent(t *testing.T) {
	collector := aiCollector()
	result, err := newTestService(collector).DeleteAIContent(t.Context(), AISkills, "check")
	if err != nil {
		t.Fatal(err)
	}
	if del := lastWrite(t, collector, "DELETE"); del.path != "/ai/skills/check" || result.Outcome != "deleted" {
		t.Fatalf("delete = %+v, result %+v", del, result)
	}
}

func TestGetAIContentNamesItsGroups(t *testing.T) {
	item, err := newTestService(aiCollector()).GetAIContent(t.Context(), AIResources, "naming")
	if err != nil {
		t.Fatal(err)
	}
	if item.Body == "" || len(item.Groups) != 1 || item.Groups[0] != (AIGroup{ID: 4, Name: "ops"}) || item.Everyone == nil || *item.Everyone {
		t.Fatalf("item = %+v", item)
	}
}

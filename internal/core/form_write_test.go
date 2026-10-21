package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// newFormWriteCollector answers form 215, mcp-lab-crud, its creation, its
// change, its deletion and the changes of its groups: Lab, a group already
// publishing it, and Ops; no group is left responsible for it.
func newFormWriteCollector(changeInfo string) *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch {
		case path == "/forms" && query == nil:
			return `{"data":[{"id":215,"form_name":"mcp-lab-crud"}]}`
		case path == "/forms":
			return `{"data":[{"id":215,"form_name":"mcp-lab-crud","form_folder":"/mcp-lab","form_definition":{"Inputs":[{"Id":"subject"}],"Outputs":[{"Dest":"workflow"}]},"form_yaml":"Inputs: []"}],"meta":{"total":1}}`
		case path == "/forms/215" && query == nil:
			return `{"info":"` + changeInfo + `"}`
		case path == "/forms/215/responsibles" && query != nil:
			return `{"data":[]}`
		case strings.HasPrefix(path, "/forms/215/publications") || strings.HasPrefix(path, "/forms/215/responsibles"):
			if strings.HasSuffix(path, "/2") {
				return `{"info":"Form 215 already published to group Lab"}`
			}
			return `{"info":"Form 215 responsibility to group Lab added"}`
		case path == "/groups/Lab":
			return `{"data":[{"id":2,"role":"Lab","privilege":"F"}]}`
		case path == "/groups/Ops":
			return `{"data":[{"id":3,"role":"Ops","privilege":"F"}]}`
		case path == "/groups/Manager":
			return `{"data":[{"id":1,"role":"Manager","privilege":"T"}]}`
		}
		return `{"data":[]}`
	}}
}

func TestCreateFormReadsTheFormBack(t *testing.T) {
	collector := newFormWriteCollector("")
	result, err := New(collector).CreateForm(t.Context(), CreateFormOptions{Name: "mcp-lab-crud", YAML: "Inputs: []", Folder: "/mcp-lab"})
	if err != nil {
		t.Fatal(err)
	}
	post := collector.requests[0]
	if post.method != "POST" || post.path != "/forms" || post.body["form_name"] != "mcp-lab-crud" || post.body["form_yaml"] != "Inputs: []" || post.body["form_folder"] != "/mcp-lab" || post.body["form_type"] != nil {
		t.Fatalf("creation = %+v", post)
	}
	if result.Outcome != FormCreated || result.ID != 215 || result.Form == nil || len(result.Form.Inputs) != 1 || result.Warning != "" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := New(newFormWriteCollector("")).CreateForm(t.Context(), CreateFormOptions{YAML: "x"}); err == nil {
		t.Fatal("a form without name was created")
	}
}

func TestUpdateFormSendsTheFieldsGiven(t *testing.T) {
	yaml := "Inputs: []"
	collector := newFormWriteCollector("Form mcp-lab-crud change: form_yaml: a => b")
	result, err := New(collector).UpdateForm(t.Context(), UpdateFormOptions{Form: "mcp-lab-crud", YAML: &yaml})
	if err != nil {
		t.Fatal(err)
	}
	var post fakeRequest
	for _, request := range collector.requests {
		if request.method == "POST" {
			post = request
		}
	}
	if post.path != "/forms/215" || len(post.body) != 1 || post.body["form_yaml"] != yaml || result.Outcome != FormChanged || result.Form == nil {
		t.Fatalf("change = %+v, result %+v", post, result)
	}
	// The collector lists no change for the values the form had.
	result, err = New(newFormWriteCollector("Form mcp-lab-crud change: ")).UpdateForm(t.Context(), UpdateFormOptions{Form: "215", YAML: &yaml})
	if err != nil || result.Outcome != FormUnchanged {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if _, err := New(newFormWriteCollector("")).UpdateForm(t.Context(), UpdateFormOptions{Form: "215"}); err == nil {
		t.Fatal("a change of nothing was sent")
	}
}

func TestDeleteForm(t *testing.T) {
	collector := newFormWriteCollector("")
	result, err := New(collector).DeleteForm(t.Context(), "mcp-lab-crud")
	last := collector.requests[len(collector.requests)-1]
	if err != nil || last.method != "DELETE" || last.path != "/forms/215" || result.Outcome != FormDeleted || result.Name != "mcp-lab-crud" {
		t.Fatalf("result = %+v, %v, last request %+v", result, err, last)
	}
}

func TestChangeFormGroupsGivesTheOutcomeOnEachGroup(t *testing.T) {
	collector := newFormWriteCollector("")
	result, err := New(collector).AttachFormGroups(t.Context(), FormGroupOptions{Form: "mcp-lab-crud", Publications: []string{"Lab", "Nope"}, Responsibles: []string{"Ops"}})
	if err != nil {
		t.Fatal(err)
	}
	lab, nope, ops := result.Groups[0], result.Groups[1], result.Groups[2]
	if result.Changed != 1 || result.Unchanged != 1 || result.Refused != 1 || lab.Outcome != ChangeAlreadyAttached || lab.Role != "publication" || lab.GroupID != 2 ||
		nope.Outcome != ChangeRefused || !strings.Contains(nope.Reason, "list_groups") || ops.Outcome != ChangeAttached || ops.Role != "responsible" {
		t.Fatalf("result = %+v", result)
	}
	var paths []string
	for _, request := range collector.requests {
		if request.method == "POST" {
			paths = append(paths, request.path)
		}
	}
	if strings.Join(paths, ",") != "/forms/215/publications/2,/forms/215/responsibles/3" {
		t.Fatalf("changes = %v", paths)
	}
	// The last responsible group withdrawn, the result tells it.
	result, err = New(newFormWriteCollector("")).DetachFormGroups(t.Context(), FormGroupOptions{Form: "215", Responsibles: []string{"Ops"}})
	if err != nil || result.Groups[0].Outcome != ChangeDetached || len(result.Responsibles) != 0 || !strings.Contains(result.Warning, "only a Manager") {
		t.Fatalf("detach = %+v, %v", result, err)
	}
	// The collector refuses a privilege group: its refusal is given.
	privileged := &pathRefusingCollector{fakeCollector: newFormWriteCollector(""), path: "/forms/215/publications/1", err: errString("HTTP 403 Forbidden: Operation not allowed on privilege group: Manager")}
	result, err = New(privileged).AttachFormGroups(t.Context(), FormGroupOptions{Form: "215", Publications: []string{"Manager"}})
	if err != nil || result.Refused != 1 || !strings.Contains(result.Groups[0].Reason, "privilege group") {
		t.Fatalf("privilege group = %+v, %v", result, err)
	}
	if _, err := New(newFormWriteCollector("")).AttachFormGroups(t.Context(), FormGroupOptions{Form: "215"}); err == nil {
		t.Fatal("a change of no group was accepted")
	}
}

// A definition in which the collector reads no form is told.
func TestCreateFormTellsADefinitionThatIsNoForm(t *testing.T) {
	collector := newFormWriteCollector("")
	answer := collector.answer
	collector.answer = func(path string, query url.Values) string {
		if path == "/forms" && query != nil {
			return `{"data":[{"id":215,"form_name":"mcp-lab-crud","form_definition":null}],"meta":{"total":1}}`
		}
		return answer(path, query)
	}
	result, err := New(collector).CreateForm(t.Context(), CreateFormOptions{Name: "mcp-lab-crud", YAML: "just text"})
	if err != nil || result.Form == nil || !strings.Contains(result.Warning, "reads no form in it") {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

// pathRefusingCollector answers a POST of path with err, as the collector
// refuses it.
type pathRefusingCollector struct {
	*fakeCollector
	path string
	err  error
}

func (c *pathRefusingCollector) PostJSON(ctx context.Context, path string, input any, output any) error {
	if path == c.path {
		return c.err
	}
	return c.fakeCollector.PostJSON(ctx, path, input, output)
}

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/oc3-mcp/internal/testutil"
	"go.yaml.in/yaml/v2"
)

// maxFormDefinitionBytes bounds the reference of the YAML of a form: the
// agent reads a resource of 64 KiB at most, and the reference is read in the
// turns that write a form.
const maxFormDefinitionBytes = 16 << 10

// The MCP serves the reference of the YAML of a form as its only resource,
// in Markdown, whose examples parse and use only the keys it documents.
func TestFormDefinitionResource(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { notFound(w, "missing") }))
	api, err := newCollectorClient(config.Config{CollectorURL: collector.URL, CollectorCAFile: collector.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newHandler(api, testDelegator(t), false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: testutil.Token, requestID: testRequestID}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	list, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Resources) != 1 || list.Resources[0].URI != "oc3://docs/form-definition" || list.Resources[0].MIMEType != "text/markdown" || list.Resources[0].Description == "" {
		t.Fatalf("resources = %+v", list.Resources)
	}
	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "oc3://docs/form-definition"})
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Contents) != 1 || read.Contents[0].MIMEType != "text/markdown" || read.Contents[0].Text == "" || len(read.Contents[0].Text) > maxFormDefinitionBytes {
		t.Fatalf("contents = %d, %d bytes", len(read.Contents), len(read.Contents[0].Text))
	}
	text := read.Contents[0].Text

	// The documented keys are the keys of the first column of its tables.
	documented := map[string]bool{}
	cell := regexp.MustCompile("`([A-Z][A-Za-z]*)`")
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		first := strings.SplitN(strings.TrimPrefix(line, "|"), "|", 2)[0]
		for _, match := range cell.FindAllStringSubmatch(first, -1) {
			documented[match[1]] = true
		}
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(text, -1)
	if len(blocks) < 3 {
		t.Fatalf("%d examples", len(blocks))
	}
	check := func(where string, keys map[any]any) {
		for key := range keys {
			name, _ := key.(string)
			if !documented[name] {
				t.Errorf("example key %q of %s is not documented", key, where)
			}
		}
	}
	for index, block := range blocks {
		var form map[any]any
		if err := yaml.Unmarshal([]byte(block[1]), &form); err != nil {
			t.Fatalf("example %d: %v", index+1, err)
		}
		check("the form", form)
		for _, list := range []string{"Inputs", "Outputs"} {
			items, _ := form[list].([]any)
			if len(items) == 0 && list == "Outputs" {
				t.Errorf("example %d has no outputs", index+1)
			}
			for _, item := range items {
				element, _ := item.(map[any]any)
				check(list, element)
				if list == "Outputs" && element["Dest"] == nil {
					t.Errorf("example %d has an output without Dest", index+1)
				}
				scripts, _ := element["Scripts"].(map[any]any)
				check("Scripts", scripts)
				for _, branch := range scripts {
					keys, _ := branch.(map[any]any)
					check("a branch of Scripts", keys)
				}
			}
		}
	}
}

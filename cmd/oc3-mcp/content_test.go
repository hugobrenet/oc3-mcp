package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/oc3-mcp/internal/content"
	"github.com/opensvc/oc3-mcp/internal/testutil"
)

// TestContentFromTheCollector serves the resources and the skills the
// collector stores for the oc3 backend, read as the caller at each request:
// resources/list and resources/read, skills/list and skills/get, a 404 of the
// collector a resource not found.
func TestContentFromTheCollector(t *testing.T) {
	var (
		mu    sync.Mutex
		asked []string
	)
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ai/resources":
			_, _ = w.Write([]byte(`{"data":[{"name":"naming","title":"Naming conventions","description":"The naming rules.","mime_type":"text/markdown","size":9,"digest":"sha256:aa","updated":"2026-11-05T11:30:10Z"}]}`))
		case "/ai/resources/naming":
			_, _ = w.Write([]byte(`{"name":"naming","mime_type":"text/markdown","size":9,"digest":"sha256:aa","body":"# Naming\n"}`))
		case "/ai/skills":
			_, _ = w.Write([]byte(`{"data":[{"name":"maintenance","frontmatter":{"name":"maintenance","description":"The maintenance of a node.","metadata":{"opensvc/tools":"freeze_node"}},"size":30,"digest":"sha256:bb"}]}`))
		case "/ai/skills/maintenance":
			_, _ = w.Write([]byte(`{"name":"maintenance","frontmatter":{"name":"maintenance","description":"d"},"size":30,"digest":"sha256:bb","body":"---\nname: maintenance\n---\n"}`))
		default:
			notFound(w, "no such object")
		}
	}))
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
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	if err := mcp.AddSendingCustomMethod[*content.ListParams, *content.ListResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*content.GetParams, *content.GetResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: testutil.Token, requestID: testRequestID}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if extensions := session.InitializeResult().Capabilities.Extensions; extensions[content.Extension] == nil {
		t.Errorf("extensions = %v", extensions)
	}
	list, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Resources) != 1 || list.Resources[0].URI != "oc3://resources/naming" || list.Resources[0].Title != "Naming conventions" || list.Resources[0].Size != 9 {
		t.Fatalf("resources = %+v", list.Resources)
	}
	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "oc3://resources/naming"})
	if err != nil || len(read.Contents) != 1 || read.Contents[0].Text != "# Naming\n" || read.Contents[0].MIMEType != "text/markdown" {
		t.Fatalf("read = %+v, %v", read, err)
	}
	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "oc3://resources/missing"})
	var rpc, notFoundError *jsonrpc.Error
	errors.As(mcp.ResourceNotFoundError(""), &notFoundError)
	if !errors.As(err, &rpc) || rpc.Code != notFoundError.Code {
		t.Errorf("a missing resource: %v", err)
	}
	skills, err := mcp.CallCustomMethod[*content.ListParams, *content.ListResult](ctx, session, "skills/list", &content.ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills.Skills) != 1 || skills.Skills[0].URI != "skill://maintenance/SKILL.md" || skills.Skills[0].Resources[0].Digest != "sha256:bb" ||
		skills.Skills[0].Resources[0].Size != 30 || skills.Skills[0].Frontmatter["description"] != "The maintenance of a node." || skills.CacheScope != "private" {
		t.Fatalf("skills = %+v", skills)
	}
	get, err := mcp.CallCustomMethod[*content.GetParams, *content.GetResult](ctx, session, "skills/get", &content.GetParams{URI: "skill://maintenance/SKILL.md"})
	if err != nil || get.Skill.URI != "skill://maintenance/SKILL.md" {
		t.Fatalf("get = %+v, %v", get, err)
	}
	file, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "skill://maintenance/SKILL.md"})
	if err != nil || !strings.HasPrefix(file.Contents[0].Text, "---\nname: maintenance") {
		t.Fatalf("SKILL.md = %+v, %v", file, err)
	}
	if _, err := mcp.CallCustomMethod[*content.GetParams, *content.GetResult](ctx, session, "skills/get", &content.GetParams{URI: "skill://other/SKILL.md"}); !errors.As(err, &rpc) || rpc.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("an unknown skill: %v", err)
	}
	// Each request read the collector, for the oc3 backend.
	mu.Lock()
	defer mu.Unlock()
	for _, a := range asked {
		if !strings.HasSuffix(a, "?backend=oc3") {
			t.Errorf("collector asked %s", a)
		}
	}
	if len(asked) != 7 {
		t.Errorf("collector asked %d times: %v", len(asked), asked)
	}
}

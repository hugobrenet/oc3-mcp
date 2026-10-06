package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHealth(t *testing.T) {
	server := httptest.NewTLSServer(newHandler())
	defer server.Close()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), method, server.URL+"/health", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if method == http.MethodPost {
				if response.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("POST status=%d, want 405", response.StatusCode)
				}
				return
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			want := "{\"status\":\"ok\"}\n"
			if method == http.MethodHead {
				want = ""
			}
			if response.StatusCode != http.StatusOK || string(body) != want || response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected health response: status=%d headers=%v body=%q", response.StatusCode, response.Header, body)
			}
		})
	}
}

func TestMCPInitializeAndEmptyTools(t *testing.T) {
	server := httptest.NewTLSServer(newHandler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: server.Client(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if info := session.InitializeResult().ServerInfo; info.Name != serverName || info.Version != serverVersion {
		t.Fatalf("unexpected server info: %+v", info)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 0 {
		t.Fatalf("expected no tools, got %d", len(tools.Tools))
	}
}

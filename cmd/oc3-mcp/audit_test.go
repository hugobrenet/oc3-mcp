package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/testutil"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestToolCallsAreAuditedWithoutCredentials(t *testing.T) {
	var audit syncBuffer
	defer func(logger *slog.Logger) { auditLogger = logger }(auditLogger)
	auditLogger = slog.New(slog.NewTextHandler(&audit, nil))
	call := newToolCaller(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/clusters":
			_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0}}`))
		default:
			notFound(w, "node missing")
		}
	})
	if result, _ := call("list_clusters", map[string]any{}); result.IsError {
		t.Fatalf("list_clusters failed: %+v", result)
	}
	if result, _ := call("get_node", map[string]any{"node": "missing"}); !result.IsError {
		t.Fatal("get_node of a missing node succeeded")
	}
	lines := strings.Split(strings.TrimSpace(audit.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one audit line per tool call, got:\n%s", audit.String())
	}
	identity := `issuer=` + testutil.TokenIssuer + ` subject=` + testutil.TokenSubject
	socket := ` client_id="" exchange=delegated collector_subject=` + testutil.TokenSubject
	if !strings.Contains(lines[0], `msg="mcp tool call" tool=list_clusters `+identity+socket+` outcome=ok duration=`) {
		t.Fatalf("unexpected audit line for a successful call:\n%s", lines[0])
	}
	if !strings.Contains(lines[1], `tool=get_node `+identity+socket+` outcome=tool_error`) || !strings.Contains(lines[1], "detail=") || !strings.Contains(lines[1], "404") {
		t.Fatalf("unexpected audit line for a failed call:\n%s", lines[1])
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, "request_id="+testRequestID) {
			t.Fatalf("audit line misses the request ID of the agent:\n%s", line)
		}
	}
	if strings.Contains(audit.String(), testutil.Token) || strings.Contains(audit.String(), `"missing"`) {
		t.Fatalf("audit logged the token or the arguments:\n%s", audit.String())
	}
}

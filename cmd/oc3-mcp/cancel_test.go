package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/oc3-mcp/internal/testutil"
)

// postMCP sends one raw JSON-RPC message, or a batch, to an MCP endpoint and
// returns the status and the start of the body; status 0 when the request
// failed, such as by the deadline of ctx.
func postMCP(ctx context.Context, url, token, body string) (int, string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err.Error()
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return response.StatusCode, string(data)
}

// awaitAudit waits for an audit line holding needle.
func awaitAudit(audit *syncBuffer, needle string, limit time.Duration) string {
	for end := time.Now().Add(limit); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if text := audit.String(); strings.Contains(text, needle) {
			return text
		}
	}
	return audit.String()
}

// newSocketServer serves the Unix socket handler over a fake collector
// accepting token.
func newSocketServer(t *testing.T, token string, collectorHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	collector := testutil.NewCollectorWithToken(t, token, collectorHandler)
	api, err := newCollectorClient(config.Config{CollectorURL: collector.URL, CollectorCAFile: collector.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newHandler(api, testDelegator(t), false)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// TestToolCallsEndWithTheirRequest checks that a wait stops when its client
// goes away, instead of polling the collector to its bound for nobody, and
// that the audit line still names the caller of the cancelled call.
func TestToolCallsEndWithTheirRequest(t *testing.T) {
	var audit syncBuffer
	defer func(logger *slog.Logger) { auditLogger = logger }(auditLogger)
	auditLogger = slog.New(slog.NewTextHandler(&audit, nil))
	var reads atomic.Int32
	server := newSocketServer(t, testutil.Token, func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"form_id":7,"results_id":621,"status":"RUNNING","outputs_order":[],"log":{}}`))
	})
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	status, _ := postMCP(ctx, server.URL+"/mcp", testutil.Token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_form_result","arguments":{"result_id":621,"wait":20}}}`)
	cancel()
	if status != 0 {
		t.Fatalf("the wait answered before the client deadline: status %d", status)
	}
	line := awaitAudit(&audit, "tool=get_form_result", 5*time.Second)
	if !strings.Contains(line, "tool=get_form_result") {
		t.Fatalf("the abandoned call kept running past 5 s; reads=%d", reads.Load())
	}
	afterAudit := reads.Load()
	time.Sleep(3500 * time.Millisecond)
	if reads.Load() != afterAudit {
		t.Fatalf("the collector was still read after the call ended: %d then %d reads", afterAudit, reads.Load())
	}
	if !strings.Contains(line, "subject="+testutil.TokenSubject) || !strings.Contains(line, "exchange=delegated") || strings.Contains(line, "outcome=ok") {
		t.Fatalf("unexpected audit line for a cancelled call:\n%s", line)
	}
}

// TestAuditNamesTheCallerWhenTheTokenExpiresDuringTheCall checks that the
// audit line of a call that outlives its delegated token still names the
// caller the Delegator admitted.
func TestAuditNamesTheCallerWhenTheTokenExpiresDuringTheCall(t *testing.T) {
	var audit syncBuffer
	defer func(logger *slog.Logger) { auditLogger = logger }(auditLogger)
	auditLogger = slog.New(slog.NewTextHandler(&audit, nil))
	token := testutil.NewToken(testutil.TokenIssuer, "expiring-user", time.Now().Add(2*time.Second))
	var reached atomic.Bool
	server := newSocketServer(t, token, func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"meta":{"total":0}}`))
	})
	_, _ = postMCP(t.Context(), server.URL+"/mcp", token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_clusters","arguments":{}}}`)
	line := awaitAudit(&audit, "tool=list_clusters", 10*time.Second)
	if !reached.Load() {
		t.Fatal("the call did not reach the collector")
	}
	if !strings.Contains(line, "issuer="+testutil.TokenIssuer+" subject=expiring-user") || !strings.Contains(line, "exchange=delegated collector_subject=expiring-user") {
		t.Fatalf("the audit line lost the caller of a call that outlived its token:\n%s", line)
	}
}

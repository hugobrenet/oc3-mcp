package client

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/testutil"
)

// answering serves one answer, its status, content type and body.
func answering(t *testing.T, status int, contentType, body string) *Client {
	t.Helper()
	return newTestClient(t, testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})))
}

func TestAnAnswerOfAnotherAPIIsIncompatible(t *testing.T) {
	for name, test := range map[string]struct {
		status      int
		contentType string
		body        string
		reason      string
	}{
		"a response of another shape":         {http.StatusOK, "application/json", `{"data":{"nodename":"n1"}}`, "not in the form this MCP reads"},
		"a response not JSON":                 {http.StatusOK, "application/json", `<html>`, "not in the form this MCP reads"},
		"a response of another type":          {http.StatusOK, "text/html", `<html>`, `unexpected content type "text/html"`},
		"a route the collector does not know": {http.StatusNotFound, "application/json", `{"message":"Not Found"}`, "knows no such route"},
		"a method the route does not take":    {http.StatusMethodNotAllowed, "application/json", `{"message":"Method Not Allowed"}`, "does not take this method"},
	} {
		t.Run(name, func(t *testing.T) {
			var output struct {
				Data []map[string]string `json:"data"`
			}
			err := answering(t, test.status, test.contentType, test.body).GetJSON(callerContext(t), "/nodes", nil, &output)
			if !errors.Is(err, ErrIncompatible) {
				t.Fatalf("got %v, want an incompatible collector", err)
			}
			if !strings.Contains(err.Error(), "GET /nodes") || !strings.Contains(err.Error(), test.reason) {
				t.Errorf("got %q, want the request and %q", err, test.reason)
			}
		})
	}
}

func TestAFailureOfTheCollectorIsNoIncompatibility(t *testing.T) {
	for name, test := range map[string]struct {
		status int
		body   string
	}{
		// A handler: an item not found, the problem of the collector.
		"a problem not found": {http.StatusNotFound, `{"text":"node missing not found"}`},
		"a refusal":           {http.StatusForbidden, `{"text":"missing privilege"}`},
		"a server error":      {http.StatusInternalServerError, `{"message":"Internal Server Error"}`},
	} {
		t.Run(name, func(t *testing.T) {
			err := answering(t, test.status, "application/json", test.body).GetJSON(callerContext(t), "/nodes/missing", nil, &struct{}{})
			if err == nil || errors.Is(err, ErrIncompatible) {
				t.Fatalf("got %v, want an error of the request, not an incompatible collector", err)
			}
		})
	}
}

package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/opensvc/oc3-mcp/internal/auth"
	"github.com/opensvc/oc3-mcp/internal/testutil"
)

func newTestClient(t *testing.T, collector testutil.Collector) *Client {
	t.Helper()
	httpClient, err := NewHTTPClient(collector.URL, collector.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(collector.URL, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestGetJSONSendsTheCallerTokenAndEscapedPath(t *testing.T) {
	var gotPath, gotQuery string
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"data":[{"svcname":"avn/svc/tst1"}]}`))
	}))
	client := newTestClient(t, collector)
	var output struct {
		Data []map[string]string `json:"data"`
	}
	if err := client.GetJSON(callerContext(t), "/services/"+url.PathEscape("avn/svc/tst1"), url.Values{"limit": {"1"}}, &output); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/services/avn%2Fsvc%2Ftst1" || gotQuery != "limit=1" || len(output.Data) != 1 || output.Data[0]["svcname"] != "avn/svc/tst1" {
		t.Fatalf("path=%q query=%q output=%+v", gotPath, gotQuery, output)
	}
}

func TestGetJSONNormalizesErrors(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("{\"text\":\"node\\n missing  not found\"}"))
	}))
	err := newTestClient(t, collector).GetJSON(callerContext(t), "/nodes/missing", nil, &struct{}{})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotFound || apiError.Text != "node missing not found" {
		t.Fatalf("error = %#v", err)
	}
	if !strings.Contains(err.Error(), "OpenSVC collector GET /nodes/missing returned HTTP 404") {
		t.Fatalf("error text = %q", err)
	}

	other, cancel, err := auth.NewContext(t.Context(), testutil.NewToken(testutil.TokenIssuer, "other-subject", time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	err = newTestClient(t, collector).GetJSON(other, "/nodes", nil, &struct{}{})
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusUnauthorized || strings.Contains(err.Error(), "strategies") {
		t.Fatalf("unauthorized error = %v", err)
	}
}

func TestGetJSONRejectsUnexpectedResponses(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"content type", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html></html>"))
		}},
		{"oversized", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`"` + strings.Repeat("a", maxResponseBodySize) + `"`))
		}},
		{"invalid JSON", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{`))
		}},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example.com/api/nodes", http.StatusFound)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			collector := testutil.NewCollector(t, test.handler)
			if err := newTestClient(t, collector).GetJSON(callerContext(t), "/nodes", nil, &struct{}{}); err == nil {
				t.Fatal("unexpected response accepted")
			}
		})
	}
}

func TestGetJSONRejectsInvalidPaths(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("an invalid path reached the collector")
	}))
	client := newTestClient(t, collector)
	for _, path := range []string{"nodes", "/nodes?limit=1", "/nodes#x", ""} {
		if err := client.GetJSON(callerContext(t), path, nil, &struct{}{}); err == nil {
			t.Fatalf("path %q accepted", path)
		}
	}
}

func TestTheCallerTokenIsBoundToTheCollectorOrigin(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	httpClient, err := NewHTTPClient(collector.URL, collector.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(callerContext(t), http.MethodGet, "https://elsewhere.example.com/api/nodes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := httpClient.Do(request); err == nil || !strings.Contains(err.Error(), "collector origin") {
		t.Fatalf("request to another origin error = %v", err)
	}
}

func TestNewHTTPClientValidatesItsInputs(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if _, err := NewHTTPClient("http://collector.example.com/api", ""); err == nil {
		t.Fatal("an HTTP collector URL was accepted")
	}
	if _, err := NewHTTPClient(collector.URL, "/nonexistent/ca.pem"); err == nil {
		t.Fatal("a missing CA file was accepted")
	}
	// Without the CA bundle, system roots do not trust the test collector.
	client := newTestClient(t, testutil.Collector{URL: collector.URL})
	if err := client.GetJSON(callerContext(t), "/nodes", nil, &struct{}{}); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
	if _, err := New("https://collector.example.com/api", nil); err == nil {
		t.Fatal("a nil HTTP client was accepted")
	}
}

func TestNoRequestIsSentWithoutACaller(t *testing.T) {
	collector := testutil.NewCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("a request without a caller reached the collector")
	}))
	client := newTestClient(t, collector)
	expired, cancel := context.WithCancel(callerContext(t))
	cancel()
	for _, ctx := range []context.Context{t.Context(), expired} {
		if err := client.GetJSON(ctx, "/nodes", nil, &struct{}{}); err == nil {
			t.Fatal("a request without a valid caller was sent")
		}
	}
}

// callerContext returns a context carrying testutil.Token as the caller's
// token.
func callerContext(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel, err := auth.NewContext(t.Context(), testutil.Token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	return ctx
}

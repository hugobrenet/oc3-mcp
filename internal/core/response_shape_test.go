package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/client"
)

// A read answered out of the collector API is an answer of another API: the
// tool tells the model the collector moved on, not a passing failure.
func TestAReadOutOfTheAPIIsIncompatible(t *testing.T) {
	for name, body := range map[string]string{
		"a negative total":           `{"data":[],"meta":{"total":-1}}`,
		"a page longer than asked":   `{"data":[{},{},{}],"meta":{"total":3}}`,
		"a count without its groups": `{"data":{"other":{"a":1}},"meta":{"total":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var err error
			if strings.Contains(body, "other") {
				_, err = newTestService(&fakeCollector{body: body}).CountNodes(t.Context(), CountNodesOptions{GroupBy: "os_name"})
			} else {
				_, err = newTestService(&fakeCollector{body: body}).ListNodes(t.Context(), ListNodesOptions{Limit: 2})
			}
			if !errors.Is(err, client.ErrIncompatible) || !strings.Contains(err.Error(), "unexpected collector response") {
				t.Fatalf("got %v, want an unexpected response of another API", err)
			}
		})
	}
}

// A write the collector answered out of its API is ambiguous, not
// incompatible: the error says how to find what was done.
func TestAWriteAnsweredOutOfTheAPIStaysAmbiguous(t *testing.T) {
	_, err := newTestService(&fakeCollector{body: `{"data":[]}`}).CreateTag(t.Context(), "backup", nil, nil)
	if err == nil || errors.Is(err, client.ErrIncompatible) || !strings.Contains(err.Error(), "list_tags tells whether it was created") {
		t.Fatalf("got %v, want the ambiguous outcome of the write", err)
	}
}

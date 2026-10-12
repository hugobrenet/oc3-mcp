package core

import (
	"net/url"
	"regexp"
	"testing"
)

func TestFilterContainsQuotesTheValue(t *testing.T) {
	for value, want := range map[string]string{
		"dev2":    "nodename:~dev2",
		"a.b":     `nodename:~a\.b`,
		"eq:x":    "nodename:~eq:x",
		"!(x|y)*": `nodename:~!\(x\|y\)\*`,
		"empty":   "nodename:~empty",
		"in:a,b":  "nodename:~in:a,b",
	} {
		got := filterContains("nodename", value)
		if got != want {
			t.Fatalf("filterContains(%q) = %q, want %q", value, got, want)
		}
		pattern, err := regexp.Compile(got[len("nodename:~"):])
		if err != nil || !pattern.MatchString("x"+value+"y") {
			t.Fatalf("pattern %q does not match the literal value %q", got, value)
		}
	}
}

func TestListCursorIsBoundToItsFilters(t *testing.T) {
	fingerprint := listFingerprint(url.Values{"filter": {"app:eq:WEB"}})
	cursor := encodeListCursor(100, fingerprint)
	if offset, err := decodeListCursor(cursor, fingerprint); err != nil || offset != 100 {
		t.Fatalf("decode = %d, %v", offset, err)
	}
	if _, err := decodeListCursor(cursor, listFingerprint(url.Values{"filter": {"app:eq:DB"}})); err == nil {
		t.Fatal("a cursor of other filters was accepted")
	}
	if _, err := decodeListCursor(encodeListCursor(0, fingerprint), fingerprint); err == nil {
		t.Fatal("a cursor at offset 0 was accepted")
	}
	if offset, err := decodeListCursor("", fingerprint); err != nil || offset != 0 {
		t.Fatalf("empty cursor = %d, %v", offset, err)
	}
}

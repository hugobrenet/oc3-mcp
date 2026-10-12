package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/testutil"
)

func TestLoadBasicCredentials(t *testing.T) {
	path := testutil.WriteNetrc(t, "collector.example.com", "mcp@example.com", "secret", 0o600)
	credentials, err := LoadBasicCredentials(path, "collector.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if credentials != (BasicCredentials{Login: "mcp@example.com", Password: "secret"}) {
		t.Fatalf("credentials = %+v", credentials)
	}
}

func TestLoadBasicCredentialsRejectsAccessibleFiles(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o604, 0o644} {
		path := testutil.WriteNetrc(t, "collector.example.com", "mcp@example.com", "secret", mode)
		if _, err := LoadBasicCredentials(path, "collector.example.com"); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("mode %04o error = %v", mode, err)
		}
	}
	if _, err := LoadBasicCredentials(t.TempDir(), "collector.example.com"); err == nil {
		t.Fatal("a directory was accepted")
	}
	if _, err := LoadBasicCredentials(filepath.Join(t.TempDir(), "missing"), "collector.example.com"); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestParseNetrc(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    BasicCredentials
	}{
		{"exact machine", "machine other.example.com login a password b\nmachine collector.example.com login c password d", BasicCredentials{Login: "c", Password: "d"}},
		{"case-insensitive host", "machine COLLECTOR.example.com login c password d", BasicCredentials{Login: "c", Password: "d"}},
		{"machine before default", "default login a password b\nmachine collector.example.com login c password d", BasicCredentials{Login: "c", Password: "d"}},
		{"default", "machine other.example.com login a password b\ndefault login c password d", BasicCredentials{Login: "c", Password: "d"}},
		{"account ignored", "machine collector.example.com login c account x password d", BasicCredentials{Login: "c", Password: "d"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseNetrc(test.content, "collector.example.com")
			if err != nil || got != test.want {
				t.Fatalf("parseNetrc() = %+v, %v", got, err)
			}
		})
	}
	for _, content := range []string{
		"",
		"machine other.example.com login a password b",
		"machine collector.example.com login c",
		"machine collector.example.com password secret-value",
		"login c password d",
		"machine collector.example.com login c password",
		"machine collector.example.com login c password d macdef init",
		"machine collector.example.com login c password d unknown secret-value",
		"machine",
	} {
		if _, err := parseNetrc(content, "collector.example.com"); err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("parseNetrc(%q) error = %v", content, err)
		}
	}
}

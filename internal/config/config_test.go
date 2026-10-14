package config

import (
	"os"
	"path/filepath"
	"testing"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENSVC_MCP_DELEGATED_SOCKET", "/run/opensvc-oc3-mcp/delegated.sock")
	t.Setenv("OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com/api")
	t.Setenv("OPENSVC_MCP_COLLECTOR_CA_FILE", "")
	for _, name := range []string{"OPENSVC_MCP_TLS_CERT_FILE", "OPENSVC_MCP_TLS_KEY_FILE", "OPENSVC_MCP_LISTEN_ADDR", "OPENSVC_MCP_OAUTH_RESOURCE_URL",
		"OPENSVC_MCP_OAUTH_ISSUER", "OPENSVC_MCP_OAUTH_RESOURCE_NAME", "OPENSVC_MCP_OAUTH_CA_FILE", "OPENSVC_MCP_AUTH_CONFIG_FILE",
		"OPENSVC_MCP_COLLECTOR_AUTH_PROFILE", "OPENSVC_MCP_COLLECTOR_AUDIENCE", "OPENSVC_MCP_MAX_CONCURRENT_CALLS", "OPENSVC_MCP_MAX_CALLS_PER_MINUTE"} {
		t.Setenv(name, "")
	}
}

func TestLoad(t *testing.T) {
	setValidEnv(t)
	t.Setenv("OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com:8443/api/")
	t.Setenv("OPENSVC_MCP_COLLECTOR_CA_FILE", "/etc/opensvc-oc3-mcp/collector-ca.pem")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DelegatedSocket != "/run/opensvc-oc3-mcp/delegated.sock" || cfg.CollectorURL != "https://collector.example.com:8443/api" || cfg.CollectorCAFile != "/etc/opensvc-oc3-mcp/collector-ca.pem" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"OPENSVC_MCP_DELEGATED_SOCKET", ""},
		{"OPENSVC_MCP_DELEGATED_SOCKET", "delegated.sock"},
		{"OPENSVC_MCP_COLLECTOR_URL", ""},
		{"OPENSVC_MCP_COLLECTOR_URL", "http://collector.example.com/api"},
		{"OPENSVC_MCP_COLLECTOR_URL", "collector.example.com/api"},
		{"OPENSVC_MCP_COLLECTOR_URL", "https://user:secret@collector.example.com/api"},
		{"OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com/api?x=1"},
		{"OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com/api#top"},
		{"OPENSVC_MCP_COLLECTOR_CA_FILE", "ca.pem"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tc.name, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

// writeExchangeProfile writes an exchange profile file named test-sso and its
// client secret, owner-only.
func writeExchangeProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auth.yaml")
	doc := "version: 1\nprofiles:\n  test-sso:\n    issuer: https://sso.example.com/application/o/oc3-mcp-exchange/\n    client_id: oc3-mcp-exchange\n    client_secret_file: " + secret + "\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setHTTPSEnv(t *testing.T) {
	t.Helper()
	setValidEnv(t)
	t.Setenv("OPENSVC_MCP_DELEGATED_SOCKET", "")
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/etc/opensvc-oc3-mcp/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/etc/opensvc-oc3-mcp/server.key")
	t.Setenv("OPENSVC_MCP_LISTEN_ADDR", "")
	t.Setenv("OPENSVC_MCP_OAUTH_RESOURCE_URL", "https://mcp.example.com:8444/mcp")
	t.Setenv("OPENSVC_MCP_OAUTH_ISSUER", "https://sso.example.com/application/o/oc3-mcp/")
	t.Setenv("OPENSVC_MCP_OAUTH_RESOURCE_NAME", "")
	t.Setenv("OPENSVC_MCP_OAUTH_CA_FILE", "")
	t.Setenv("OPENSVC_MCP_AUTH_CONFIG_FILE", writeExchangeProfile(t))
	t.Setenv("OPENSVC_MCP_COLLECTOR_AUTH_PROFILE", "test-sso")
	t.Setenv("OPENSVC_MCP_COLLECTOR_AUDIENCE", "collector-client-id")
	t.Setenv("OPENSVC_MCP_MAX_CONCURRENT_CALLS", "")
	t.Setenv("OPENSVC_MCP_MAX_CALLS_PER_MINUTE", "")
}

func TestLoadHTTPSWithTokenExchange(t *testing.T) {
	setHTTPSEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HTTPSEnabled() || cfg.ListenAddress != "127.0.0.1:8444" || cfg.DelegatedSocket != "" || !cfg.ExchangeEnabled() ||
		cfg.ExchangeProfile != "test-sso" || cfg.CollectorAudience != "collector-client-id" || cfg.OAuth.ResourceURL != "https://mcp.example.com:8444/mcp" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	// Without the exchange, the HTTPS listener starts with its tool calls blocked.
	for _, name := range []string{"OPENSVC_MCP_AUTH_CONFIG_FILE", "OPENSVC_MCP_COLLECTOR_AUTH_PROFILE", "OPENSVC_MCP_COLLECTOR_AUDIENCE"} {
		t.Setenv(name, "")
	}
	if cfg, err := Load(); err != nil || cfg.ExchangeEnabled() {
		t.Fatalf("HTTPS without exchange: %+v, %v", cfg, err)
	}
}

func TestLoadRejectsInvalidHTTPSConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"OPENSVC_MCP_TLS_KEY_FILE", ""},
		{"OPENSVC_MCP_TLS_CERT_FILE", "server.crt"},
		{"OPENSVC_MCP_LISTEN_ADDR", "localhost:8444"},
		{"OPENSVC_MCP_OAUTH_RESOURCE_URL", "https://mcp.example.com:8444/other"},
		{"OPENSVC_MCP_OAUTH_RESOURCE_URL", "http://mcp.example.com:8444/mcp"},
		{"OPENSVC_MCP_OAUTH_ISSUER", ""},
		{"OPENSVC_MCP_COLLECTOR_AUTH_PROFILE", ""},
		{"OPENSVC_MCP_COLLECTOR_AUTH_PROFILE", "unknown"},
		{"OPENSVC_MCP_COLLECTOR_AUDIENCE", ""},
		{"OPENSVC_MCP_COLLECTOR_AUDIENCE", "two words"},
		{"OPENSVC_MCP_AUTH_CONFIG_FILE", "/nonexistent/auth.yaml"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			setHTTPSEnv(t)
			t.Setenv(tc.name, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	// The HTTPS settings need the HTTPS listener.
	setValidEnv(t)
	t.Setenv("OPENSVC_MCP_COLLECTOR_AUDIENCE", "collector-client-id")
	if _, err := Load(); err == nil {
		t.Fatal("exchange settings without the HTTPS listener accepted")
	}
	// At least one listener is required.
	setValidEnv(t)
	t.Setenv("OPENSVC_MCP_DELEGATED_SOCKET", "")
	if _, err := Load(); err == nil {
		t.Fatal("configuration without listener accepted")
	}
}

func TestLoadCallLimits(t *testing.T) {
	setHTTPSEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConcurrentCalls != 10 || cfg.MaxCallsPerMinute != 120 {
		t.Fatalf("default limits: %d %d", cfg.MaxConcurrentCalls, cfg.MaxCallsPerMinute)
	}
	t.Setenv("OPENSVC_MCP_MAX_CONCURRENT_CALLS", "0")
	t.Setenv("OPENSVC_MCP_MAX_CALLS_PER_MINUTE", "600")
	if cfg, err = Load(); err != nil || cfg.MaxConcurrentCalls != 0 || cfg.MaxCallsPerMinute != 600 {
		t.Fatalf("configured limits: %d %d %v", cfg.MaxConcurrentCalls, cfg.MaxCallsPerMinute, err)
	}
	for _, tc := range []struct{ name, value string }{
		{"OPENSVC_MCP_MAX_CONCURRENT_CALLS", "-1"},
		{"OPENSVC_MCP_MAX_CONCURRENT_CALLS", "1001"},
		{"OPENSVC_MCP_MAX_CALLS_PER_MINUTE", "many"},
		{"OPENSVC_MCP_MAX_CALLS_PER_MINUTE", "100001"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			setHTTPSEnv(t)
			t.Setenv(tc.name, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid limit accepted")
			}
		})
	}
	t.Run("without HTTPS", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("OPENSVC_MCP_MAX_CALLS_PER_MINUTE", "60")
		if _, err := Load(); err == nil {
			t.Fatal("limit accepted without the HTTPS listener")
		}
	})
}

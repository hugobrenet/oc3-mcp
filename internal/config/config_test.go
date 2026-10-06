package config

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/etc/opensvc-oc3-mcp/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/etc/opensvc-oc3-mcp/server.key")
	for _, tc := range []struct {
		address string
		want    string
	}{
		{"", "127.0.0.1:8443"},
		{"0.0.0.0:8443", "0.0.0.0:8443"},
		{"[::]:8443", "[::]:8443"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Setenv("OPENSVC_MCP_LISTEN_ADDR", tc.address)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ListenAddress != tc.want || cfg.TLSCertFile != "/etc/opensvc-oc3-mcp/server.crt" || cfg.TLSKeyFile != "/etc/opensvc-oc3-mcp/server.key" {
				t.Fatalf("unexpected config: %+v", cfg)
			}
		})
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"OPENSVC_MCP_LISTEN_ADDR", "localhost:8443"},
		{"OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1:0"},
		{"OPENSVC_MCP_LISTEN_ADDR", "[::]:65536"},
		{"OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1"},
		{"OPENSVC_MCP_TLS_CERT_FILE", ""},
		{"OPENSVC_MCP_TLS_CERT_FILE", "server.crt"},
		{"OPENSVC_MCP_TLS_KEY_FILE", ""},
		{"OPENSVC_MCP_TLS_KEY_FILE", "server.key"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			t.Setenv("OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1:8443")
			t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/etc/opensvc-oc3-mcp/server.crt")
			t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/etc/opensvc-oc3-mcp/server.key")
			t.Setenv(tc.name, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

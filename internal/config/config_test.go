package config

import "testing"

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1:8444")
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/etc/opensvc-oc3-mcp/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/etc/opensvc-oc3-mcp/server.key")
	t.Setenv("OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com/api")
	t.Setenv("OPENSVC_MCP_COLLECTOR_CA_FILE", "")
	t.Setenv("OPENSVC_MCP_COLLECTOR_CREDENTIALS_FILE", "/etc/opensvc-oc3-mcp/collector.netrc")
}

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		address string
		want    string
	}{
		{"", "127.0.0.1:8444"},
		{"127.0.0.2:8444", "127.0.0.2:8444"},
		{"[::1]:8444", "[::1]:8444"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			setValidEnv(t)
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

func TestLoadCollector(t *testing.T) {
	setValidEnv(t)
	t.Setenv("OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com:8443/api/")
	t.Setenv("OPENSVC_MCP_COLLECTOR_CA_FILE", "/etc/opensvc-oc3-mcp/collector-ca.pem")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CollectorURL != "https://collector.example.com:8443/api" || cfg.CollectorCAFile != "/etc/opensvc-oc3-mcp/collector-ca.pem" || cfg.CollectorCredentialsFile != "/etc/opensvc-oc3-mcp/collector.netrc" {
		t.Fatalf("unexpected collector config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"OPENSVC_MCP_LISTEN_ADDR", "localhost:8443"},
		{"OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1:0"},
		{"OPENSVC_MCP_LISTEN_ADDR", "[::1]:65536"},
		{"OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1"},
		{"OPENSVC_MCP_LISTEN_ADDR", "0.0.0.0:8443"},
		{"OPENSVC_MCP_LISTEN_ADDR", "[::]:8443"},
		{"OPENSVC_MCP_LISTEN_ADDR", "192.0.2.10:8443"},
		{"OPENSVC_MCP_TLS_CERT_FILE", ""},
		{"OPENSVC_MCP_TLS_CERT_FILE", "server.crt"},
		{"OPENSVC_MCP_TLS_KEY_FILE", ""},
		{"OPENSVC_MCP_TLS_KEY_FILE", "server.key"},
		{"OPENSVC_MCP_COLLECTOR_URL", ""},
		{"OPENSVC_MCP_COLLECTOR_URL", "http://collector.example.com/api"},
		{"OPENSVC_MCP_COLLECTOR_URL", "collector.example.com/api"},
		{"OPENSVC_MCP_COLLECTOR_URL", "https://user:secret@collector.example.com/api"},
		{"OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com/api?x=1"},
		{"OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com/api#top"},
		{"OPENSVC_MCP_COLLECTOR_CA_FILE", "ca.pem"},
		{"OPENSVC_MCP_COLLECTOR_CREDENTIALS_FILE", ""},
		{"OPENSVC_MCP_COLLECTOR_CREDENTIALS_FILE", "collector.netrc"},
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

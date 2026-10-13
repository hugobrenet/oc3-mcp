package config

import "testing"

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENSVC_MCP_DELEGATED_SOCKET", "/run/opensvc-oc3-mcp/delegated.sock")
	t.Setenv("OPENSVC_MCP_COLLECTOR_URL", "https://collector.example.com/api")
	t.Setenv("OPENSVC_MCP_COLLECTOR_CA_FILE", "")
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

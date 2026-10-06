package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddress string
	TLSCertFile   string
	TLSKeyFile    string
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress: strings.TrimSpace(os.Getenv("OPENSVC_MCP_LISTEN_ADDR")),
		TLSCertFile:   strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_CERT_FILE")),
		TLSKeyFile:    strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_KEY_FILE")),
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "127.0.0.1:8443"
	}
	host, port, err := net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR must be IP:port: %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if net.ParseIP(host) == nil || err != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR requires an explicit IP and a port between 1 and 65535")
	}
	if !filepath.IsAbs(cfg.TLSCertFile) || !filepath.IsAbs(cfg.TLSKeyFile) {
		return Config{}, fmt.Errorf("OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE must be absolute file paths")
	}
	return cfg, nil
}

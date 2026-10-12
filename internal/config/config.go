package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddress string
	TLSCertFile   string
	TLSKeyFile    string
	// CollectorURL is the base URL of the collector API, such as
	// https://collector.example.com/api.
	CollectorURL             string
	CollectorCAFile          string
	CollectorCredentialsFile string
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress:            strings.TrimSpace(os.Getenv("OPENSVC_MCP_LISTEN_ADDR")),
		TLSCertFile:              strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_CERT_FILE")),
		TLSKeyFile:               strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_KEY_FILE")),
		CollectorURL:             strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_URL")),
		CollectorCAFile:          strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_CA_FILE")),
		CollectorCredentialsFile: strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_CREDENTIALS_FILE")),
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "127.0.0.1:8444"
	}
	host, port, err := net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR must be IP:port: %w", err)
	}
	ip := net.ParseIP(host)
	portNumber, err := strconv.Atoi(port)
	if ip == nil || err != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR requires an explicit IP and a port between 1 and 65535")
	}
	// The MCP does not authenticate its clients yet: every caller acts with
	// the collector account of the MCP, so it only listens on loopback.
	if !ip.IsLoopback() {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR must be a loopback address while the MCP does not authenticate its clients")
	}
	if !filepath.IsAbs(cfg.TLSCertFile) || !filepath.IsAbs(cfg.TLSKeyFile) {
		return Config{}, fmt.Errorf("OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE must be absolute file paths")
	}
	if err := validateCollectorURL(cfg.CollectorURL); err != nil {
		return Config{}, fmt.Errorf("OPENSVC_MCP_COLLECTOR_URL %w", err)
	}
	cfg.CollectorURL = strings.TrimRight(cfg.CollectorURL, "/")
	if cfg.CollectorCAFile != "" && !filepath.IsAbs(cfg.CollectorCAFile) {
		return Config{}, fmt.Errorf("OPENSVC_MCP_COLLECTOR_CA_FILE must be an absolute file path")
	}
	if !filepath.IsAbs(cfg.CollectorCredentialsFile) {
		return Config{}, fmt.Errorf("OPENSVC_MCP_COLLECTOR_CREDENTIALS_FILE must be an absolute file path")
	}
	return cfg, nil
}

func validateCollectorURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" {
		return fmt.Errorf("must be an absolute HTTPS URL")
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("must be an absolute HTTPS URL with a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return fmt.Errorf("must not contain credentials, a query or a fragment")
	}
	return nil
}

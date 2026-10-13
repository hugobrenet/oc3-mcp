package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	// DelegatedSocket is the Unix socket the MCP listens on. Its callers
	// forward the OpenID Connect access token of their user.
	DelegatedSocket string
	// CollectorURL is the base URL of the collector API, such as
	// https://collector.example.com/api.
	CollectorURL    string
	CollectorCAFile string
}

func Load() (Config, error) {
	cfg := Config{
		DelegatedSocket: strings.TrimSpace(os.Getenv("OPENSVC_MCP_DELEGATED_SOCKET")),
		CollectorURL:    strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_URL")),
		CollectorCAFile: strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_CA_FILE")),
	}
	if !filepath.IsAbs(cfg.DelegatedSocket) {
		return Config{}, fmt.Errorf("OPENSVC_MCP_DELEGATED_SOCKET must be an absolute socket path")
	}
	if err := validateCollectorURL(cfg.CollectorURL); err != nil {
		return Config{}, fmt.Errorf("OPENSVC_MCP_COLLECTOR_URL %w", err)
	}
	cfg.CollectorURL = strings.TrimRight(cfg.CollectorURL, "/")
	if cfg.CollectorCAFile != "" && !filepath.IsAbs(cfg.CollectorCAFile) {
		return Config{}, fmt.Errorf("OPENSVC_MCP_COLLECTOR_CA_FILE must be an absolute file path")
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

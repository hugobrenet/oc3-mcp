package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

const (
	defaultListenAddress = "127.0.0.1:8444"
	// maxUnixSocketPathBytes is the portable sun_path limit, including the NUL.
	maxUnixSocketPathBytes = 103
)

// Config contains the runtime configuration of the MCP server process. It has
// two optional listeners, at least one of which is enabled: HTTPS for external
// clients authenticated with OAuth, and a local Unix socket for the AI agent.
type Config struct {
	// HTTPS listener, enabled when TLSCertFile is set. It requires OAuth.
	ListenAddress string
	TLSCertFile   string
	TLSKeyFile    string
	OAuth         mcpauth.OAuthConfig
	// Exchange, ExchangeProfile and CollectorAudience configure the token
	// exchange of the HTTPS listener. Without them, its tool calls are blocked.
	Exchange          *mcpauth.ExchangeProfiles
	ExchangeProfile   string
	CollectorAudience string
	// MaxConcurrentCalls and MaxCallsPerMinute bound the tool calls of each
	// identity on the HTTPS listener; 0 disables a limit.
	MaxConcurrentCalls int
	MaxCallsPerMinute  int
	// DelegatedSocket enables the Unix socket listener. Its callers forward
	// the OpenID Connect access token of their user.
	DelegatedSocket string
	// CollectorURL is the base URL of the collector API, such as
	// https://collector.example.com/api.
	CollectorURL    string
	CollectorCAFile string
}

func (c Config) HTTPSEnabled() bool { return c.TLSCertFile != "" }

// ExchangeEnabled tells whether the HTTPS listener can call the collector.
func (c Config) ExchangeEnabled() bool { return c.Exchange != nil }

func Load() (Config, error) {
	cfg := Config{
		TLSCertFile:     strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_CERT_FILE")),
		TLSKeyFile:      strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_KEY_FILE")),
		CollectorURL:    strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_URL")),
		CollectorCAFile: strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_CA_FILE")),
	}
	listenAddress := strings.TrimSpace(os.Getenv("OPENSVC_MCP_LISTEN_ADDR"))
	oauth := mcpauth.OAuthConfig{
		ResourceURL:  strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_RESOURCE_URL")),
		ResourceName: strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_RESOURCE_NAME")),
		Issuer:       strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_ISSUER")),
		CAFile:       strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_CA_FILE")),
	}
	exchangeFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_AUTH_CONFIG_FILE"))
	profile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_AUTH_PROFILE"))
	audience := strings.TrimSpace(os.Getenv("OPENSVC_MCP_COLLECTOR_AUDIENCE"))
	maxConcurrent := strings.TrimSpace(os.Getenv("OPENSVC_MCP_MAX_CONCURRENT_CALLS"))
	maxPerMinute := strings.TrimSpace(os.Getenv("OPENSVC_MCP_MAX_CALLS_PER_MINUTE"))
	if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
		if !filepath.IsAbs(cfg.TLSCertFile) || !filepath.IsAbs(cfg.TLSKeyFile) {
			return Config{}, fmt.Errorf("OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE must be absolute file paths")
		}
		if listenAddress == "" {
			listenAddress = defaultListenAddress
		}
		if err := validateListenAddress(listenAddress); err != nil {
			return Config{}, fmt.Errorf("parse OPENSVC_MCP_LISTEN_ADDR: %w", err)
		}
		if err := oauth.Validate(); err != nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_OAUTH configuration: %w", err)
		}
		cfg.ListenAddress, cfg.OAuth = listenAddress, oauth
		var err error
		if cfg.MaxConcurrentCalls, err = callLimit("OPENSVC_MCP_MAX_CONCURRENT_CALLS", maxConcurrent, mcpauth.DefaultMaxConcurrentCalls, 1000); err != nil {
			return Config{}, err
		}
		if cfg.MaxCallsPerMinute, err = callLimit("OPENSVC_MCP_MAX_CALLS_PER_MINUTE", maxPerMinute, mcpauth.DefaultMaxCallsPerMinute, 100000); err != nil {
			return Config{}, err
		}
	} else if listenAddress != "" || oauth != (mcpauth.OAuthConfig{}) || exchangeFile != "" || profile != "" || audience != "" || maxConcurrent != "" || maxPerMinute != "" {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR, OPENSVC_MCP_OAUTH_*, OPENSVC_MCP_AUTH_CONFIG_FILE, OPENSVC_MCP_COLLECTOR_AUTH_PROFILE/AUDIENCE and OPENSVC_MCP_MAX_* require the HTTPS listener (OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE)")
	}
	if exchangeFile != "" || profile != "" || audience != "" {
		if exchangeFile == "" || profile == "" || audience == "" {
			return Config{}, fmt.Errorf("the token exchange needs OPENSVC_MCP_AUTH_CONFIG_FILE, OPENSVC_MCP_COLLECTOR_AUTH_PROFILE and OPENSVC_MCP_COLLECTOR_AUDIENCE together")
		}
		exchange, err := mcpauth.LoadExchangeProfiles(exchangeFile)
		if err != nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_AUTH_CONFIG_FILE: %w", err)
		}
		if !exchange.Has(profile) {
			return Config{}, fmt.Errorf("OPENSVC_MCP_COLLECTOR_AUTH_PROFILE names a profile missing from OPENSVC_MCP_AUTH_CONFIG_FILE")
		}
		if !validAudience(audience) {
			return Config{}, fmt.Errorf("OPENSVC_MCP_COLLECTOR_AUDIENCE must be 1 to 256 bytes of text without whitespace or control characters")
		}
		cfg.Exchange, cfg.ExchangeProfile, cfg.CollectorAudience = exchange, profile, audience
	}
	if socket := strings.TrimSpace(os.Getenv("OPENSVC_MCP_DELEGATED_SOCKET")); socket != "" {
		path := filepath.Clean(socket)
		if !filepath.IsAbs(path) || path == string(filepath.Separator) || len(path) > maxUnixSocketPathBytes {
			return Config{}, fmt.Errorf("OPENSVC_MCP_DELEGATED_SOCKET must be an absolute socket path of at most %d bytes", maxUnixSocketPathBytes)
		}
		cfg.DelegatedSocket = path
	}
	if !cfg.HTTPSEnabled() && cfg.DelegatedSocket == "" {
		return Config{}, fmt.Errorf("configure the HTTPS listener (OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE), OPENSVC_MCP_DELEGATED_SOCKET, or both")
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

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("expected IP:port: %w", err)
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf("host must be an explicit IPv4 or IPv6 address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}
	return nil
}

func validAudience(value string) bool {
	return len(value) > 0 && len(value) <= 256 && !strings.ContainsFunc(value, func(r rune) bool { return r <= ' ' || r == 0x7f })
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

// callLimit reads a limit of the tool calls of an identity: def when value is
// empty, 0 to disable the limit, at most max.
func callLimit(name, value string, def, max int) (int, error) {
	if value == "" {
		return def, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > max {
		return 0, fmt.Errorf("%s must be an integer from 0 (no limit) to %d", name, max)
	}
	return n, nil
}

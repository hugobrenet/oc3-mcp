package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/opensvc/oc3-mcp/internal/auth"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

const (
	maxResponseBodySize = 10 << 20
	requestTimeout      = 30 * time.Second
)

// Client reads the collector API under one base URL.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

func New(rawBaseURL string, httpClient *http.Client) (*Client, error) {
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenSVC collector URL: %w", err)
	}
	if baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, fmt.Errorf("OpenSVC collector URL must be an absolute HTTPS URL")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("OpenSVC collector HTTP client is nil")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawPath = ""
	return &Client{baseURL: baseURL, httpClient: httpClient}, nil
}

// NewHTTPClient returns the HTTP client of the collector: TLS 1.2 or later,
// system roots or the CA bundle at caFile, no proxy, no redirect, and the
// access token of the caller sent to the collector origin only.
func NewHTTPClient(rawBaseURL string, caFile string) (*http.Client, error) {
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, fmt.Errorf("OpenSVC collector URL must be an absolute HTTPS URL")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read OpenSVC collector CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("OpenSVC collector CA file contains no PEM certificate")
		}
		tlsConfig.RootCAs = roots
	}
	transport := &http.Transport{
		// Proxy is intentionally nil: the caller's token travels directly.
		TLSClientConfig:     tlsConfig,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
	}
	return &http.Client{
		Transport:     bearerTransport{base: transport, origin: baseURL.Scheme + "://" + baseURL.Host},
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

// bearerTransport sends the collector token of the caller: its delegated token
// unchanged, or the token exchanged for it. The MCP holds no collector
// account: a request without a caller is never sent.
type bearerTransport struct {
	base   http.RoundTripper
	origin string
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.User != nil || request.URL.Scheme+"://"+request.URL.Host != t.origin || request.Host != "" && request.Host != request.URL.Host {
		return nil, fmt.Errorf("send OpenSVC collector request: destination differs from the configured collector origin")
	}
	token, ok := auth.CollectorToken(request.Context())
	if !ok {
		return nil, fmt.Errorf("send OpenSVC collector request: no valid access token of the caller")
	}
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+token)
	response, err := t.base.RoundTrip(clone)
	if err == nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
		// A refused exchanged token is not reused: the next tool call
		// exchanges again. Without exchange, this does nothing.
		mcpauth.ForgetExchanged(request.Context())
	}
	return response, err
}

// GetJSON reads a bounded JSON response. path is relative to the base URL,
// starts with "/" and has its segments already escaped by the caller.
func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, output any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, output)
}

// PostJSON sends input as a JSON body and reads the bounded JSON response.
// It changes the collector state: only the action tools call it.
func (c *Client) PostJSON(ctx context.Context, path string, input any, output any) error {
	return c.do(ctx, http.MethodPost, path, nil, input, output)
}

// DeleteJSON sends a DELETE request and reads the bounded JSON response.
// It changes the collector state: only the action tools call it.
func (c *Client) DeleteJSON(ctx context.Context, path string, output any) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil, output)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, input any, output any) error {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return fmt.Errorf("OpenSVC collector path %q is invalid", path)
	}
	endpoint, err := url.Parse(c.baseURL.String() + path)
	if err != nil {
		return fmt.Errorf("build OpenSVC collector URL: %w", err)
	}
	endpoint.RawQuery = query.Encode()
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode OpenSVC collector %s %s request: %w", method, path, err)
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("create OpenSVC collector %s request: %w", method, err)
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request OpenSVC collector %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return newAPIError(method, path, response)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("OpenSVC collector %s returned unexpected content type %q", path, response.Header.Get("Content-Type"))
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodySize+1))
	if err != nil {
		return fmt.Errorf("read OpenSVC collector %s response: %w", path, err)
	}
	if len(payload) > maxResponseBodySize {
		return fmt.Errorf("OpenSVC collector %s response exceeds %d bytes", path, maxResponseBodySize)
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return fmt.Errorf("decode OpenSVC collector %s response: %w", path, err)
	}
	return nil
}

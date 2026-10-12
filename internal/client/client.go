package client

import (
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
// Basic credentials sent to the collector origin only.
func NewHTTPClient(rawBaseURL string, caFile string, credentials BasicCredentials) (*http.Client, error) {
	baseURL, err := url.Parse(rawBaseURL)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, fmt.Errorf("OpenSVC collector URL must be an absolute HTTPS URL")
	}
	if credentials.Login == "" || credentials.Password == "" {
		return nil, fmt.Errorf("OpenSVC collector credentials are incomplete")
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
		// Proxy is intentionally nil: the credentials travel directly.
		TLSClientConfig:     tlsConfig,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
	}
	return &http.Client{
		Transport:     basicAuthTransport{base: transport, origin: baseURL.Scheme + "://" + baseURL.Host, credentials: credentials},
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

type basicAuthTransport struct {
	base        http.RoundTripper
	origin      string
	credentials BasicCredentials
}

func (t basicAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme+"://"+request.URL.Host != t.origin {
		return nil, fmt.Errorf("send OpenSVC collector request: destination differs from the configured collector origin")
	}
	clone := request.Clone(request.Context())
	clone.SetBasicAuth(t.credentials.Login, t.credentials.Password)
	return t.base.RoundTrip(clone)
}

// GetJSON reads a bounded JSON response. path is relative to the base URL,
// starts with "/" and has its segments already escaped by the caller.
func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, output any) error {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return fmt.Errorf("OpenSVC collector path %q is invalid", path)
	}
	endpoint, err := url.Parse(c.baseURL.String() + path)
	if err != nil {
		return fmt.Errorf("build OpenSVC collector URL: %w", err)
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create OpenSVC collector GET request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request OpenSVC collector %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return newAPIError(http.MethodGet, path, response)
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

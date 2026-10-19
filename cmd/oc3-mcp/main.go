package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/auth"
	"github.com/opensvc/oc3-mcp/internal/client"
	"github.com/opensvc/oc3-mcp/internal/config"
	"github.com/opensvc/oc3-mcp/internal/core"
	"github.com/opensvc/oc3-mcp/internal/tools"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

const (
	serverName         = "oc3-mcp"
	resourceName       = "OpenSVC Collector MCP"
	maxHTTPHeaderBytes = 64 << 10
	// readTimeout bounds the reading of a whole request, its body included:
	// a client sending it slowly (slowloris) is cut off.
	readTimeout = 10 * time.Second
	// writeTimeout bounds the processing and the response of a request, so
	// that a client reading it slowly is cut off. It stays above the longest
	// tool: a token exchange of up to 1 min (request_timeout), then collector
	// requests of up to 30 s each, two for get_service.
	writeTimeout    = 3 * time.Minute
	maxMCPBodyBytes = 1 << 20
	shutdownTimeout = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	collector, err := newCollectorClient(cfg)
	if err != nil {
		return err
	}
	var servers []*http.Server
	serveErrors := make(chan error, 2)
	if cfg.HTTPSEnabled() {
		handler, err := newMCPHandler(cfg, collector)
		if err != nil {
			return err
		}
		listener, tlsConfig, err := listenMCP(cfg)
		if err != nil {
			return fmt.Errorf("listen for MCP HTTPS: %w", err)
		}
		server := newHTTPServer(handler)
		server.TLSConfig = tlsConfig
		servers = append(servers, server)
		go func() { serveErrors <- server.ServeTLS(listener, "", "") }()
		log.Printf("%s %s listening on https://%s/mcp", serverName, serverVersion, listener.Addr())
	}
	if cfg.DelegatedSocket != "" {
		delegator, err := newDelegator(cfg)
		if err != nil {
			return err
		}
		handler, err := newHandler(collector, delegator, cfg.Actions)
		if err != nil {
			return err
		}
		listener, err := listenDelegated(cfg.DelegatedSocket)
		if err != nil {
			return err
		}
		server := newHTTPServer(handler)
		servers = append(servers, server)
		go func() { serveErrors <- server.Serve(listener) }()
		log.Printf("%s %s listening for delegated tokens on unix:%s", serverName, serverVersion, cfg.DelegatedSocket)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve MCP: %w", err)
		}
		return nil
	case <-ctx.Done():
		stop()
	}
	log.Printf("%s shutting down", serverName)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	var shutdownErr error
	for _, server := range servers {
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown MCP: %w", err))
		}
	}
	for range servers {
		if err := <-serveErrors; !errors.Is(err, http.ErrServerClosed) {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("serve MCP during shutdown: %w", err))
		}
	}
	return shutdownErr
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    maxHTTPHeaderBytes,
	}
}

// newCollectorClient prepares the collector client without contacting the
// collector. It holds no account: each request carries its caller's token.
func newCollectorClient(cfg config.Config) (*client.Client, error) {
	httpClient, err := client.NewHTTPClient(cfg.CollectorURL, cfg.CollectorCAFile)
	if err != nil {
		return nil, err
	}
	return client.New(cfg.CollectorURL, httpClient)
}

// newMCPHandler serves the HTTPS listener. OAuth authenticates the external
// client locally; each tool call, within the limits of its identity, then
// uses a token exchanged for the collector. Without exchange configuration,
// tool calls are blocked.
func newMCPHandler(cfg config.Config, collector *client.Client) (http.Handler, error) {
	if cfg.OAuth.ResourceName == "" {
		cfg.OAuth.ResourceName = resourceName
	}
	verifier, err := mcpauth.NewOAuthVerifier(cfg.OAuth)
	if err != nil {
		return nil, fmt.Errorf("configure MCP OAuth: %w", err)
	}
	route := blockToolCalls
	if cfg.ExchangeEnabled() {
		route = exchangeToolCalls(cfg.Exchange, cfg.ExchangeProfile, cfg.CollectorAudience)
	}
	limiter := mcpauth.NewCallLimiter(cfg.MaxConcurrentCalls, cfg.MaxCallsPerMinute)
	handler, err := newToolsHandler(collector, cfg.Actions, limitToolCalls(limiter), route)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", getHealth)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", verifier.Metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", verifier.Metadata)
	mux.Handle("/mcp", verifier.Middleware(handler))
	return http.MaxBytesHandler(mux, maxMCPBodyBytes), nil
}

// newDelegator verifies the tokens of the Unix socket with the configured
// OpenID provider of the collector. Its keys are fetched trusting
// OPENSVC_MCP_DELEGATED_CA_FILE, else the OAuth trust bundle of the HTTPS
// listener, else the system roots.
func newDelegator(cfg config.Config) (*auth.Delegator, error) {
	caFile := cfg.DelegatedCAFile
	if caFile == "" {
		caFile = cfg.OAuth.CAFile
	}
	verifier, err := mcpauth.NewJWTVerifier(caFile)
	if err != nil {
		return nil, fmt.Errorf("configure delegated token verification: %w", err)
	}
	return auth.NewDelegator(cfg.DelegatedIssuer, cfg.DelegatedAudience, verifier)
}

// newHandler serves the Unix socket. Every route but /health requires the
// OpenID Connect access token of the caller, verified by delegator before it
// reaches the collector, which decides the access.
func newHandler(collector *client.Client, delegator *auth.Delegator, actions bool) (http.Handler, error) {
	handler, err := newToolsHandler(collector, actions)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", getHealth)
	mux.Handle("/mcp", delegator.Middleware(handler))
	mux.Handle("GET /mcp/auth/whoami", delegator.Middleware(serveWhoAmI(collector)))
	return withRequestID(http.MaxBytesHandler(mux, maxMCPBodyBytes)), nil
}

// newToolsHandler serves the collector tools as stateless Streamable HTTP,
// with the action tools when actions is set. The audit line wraps route,
// which prepares the credential of each tool call.
func newToolsHandler(collector *client.Client, actions bool, route ...mcp.Middleware) (http.Handler, error) {
	service := core.New(collector)
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	registrar, err := tools.NewRegistrar(server)
	if err != nil {
		return nil, err
	}
	server.AddReceivingMiddleware(append([]mcp.Middleware{auditToolCalls}, route...)...)
	for _, register := range []func(*tools.Registrar, *core.Service) error{
		tools.RegisterClusterTools, tools.RegisterNodeTools, tools.RegisterServiceTools, tools.RegisterServiceActionTools, tools.RegisterAlertTools, tools.RegisterCheckTools, tools.RegisterPackageTools, tools.RegisterCollectorEventTools, tools.RegisterComplianceTools, tools.RegisterComplianceModulesetTools, tools.RegisterComplianceRulesetTools, tools.RegisterFiltersetTools, tools.RegisterStorageTools, tools.RegisterIPTools, tools.RegisterTagTools, tools.RegisterAppTools, tools.RegisterFindTools, tools.RegisterRequestTools, tools.RegisterFormTools, tools.RegisterSysreportTools, tools.RegisterGroupTools,
	} {
		if err := register(registrar, service); err != nil {
			return nil, err
		}
	}
	if actions {
		if err := tools.RegisterNodeActionTools(registrar, service); err != nil {
			return nil, err
		}
	}
	// Cross-origin browser requests are refused with 403, as the MCP SDK
	// recommends, by the protection of net/http around its handler.
	return http.NewCrossOriginProtection().Handler(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
	})), nil
}

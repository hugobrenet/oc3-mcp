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
)

const (
	serverName         = "oc3-mcp"
	serverVersion      = "v0.1.0"
	maxHTTPHeaderBytes = 64 << 10
	maxMCPBodyBytes    = 1 << 20
	shutdownTimeout    = 30 * time.Second
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
		handler, err := newHandler(collector)
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
// client locally; each tool call then exchanges its token for one intended
// for the collector. Without exchange configuration, tool calls are blocked.
func newMCPHandler(cfg config.Config, collector *client.Client) (http.Handler, error) {
	verifier, err := auth.NewOAuthVerifier(cfg.OAuth)
	if err != nil {
		return nil, fmt.Errorf("configure MCP OAuth: %w", err)
	}
	route := blockToolCalls
	if cfg.ExchangeEnabled() {
		route = exchangeToolCalls(cfg.Exchange, cfg.ExchangeProfile, cfg.CollectorAudience)
	}
	handler, err := newToolsHandler(collector, route)
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

// newHandler serves the Unix socket. Every route but /health requires the
// OpenID Connect access token of the caller, which the collector verifies.
func newHandler(collector *client.Client) (http.Handler, error) {
	handler, err := newToolsHandler(collector)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", getHealth)
	mux.Handle("/mcp", auth.Middleware(handler))
	mux.Handle("GET /mcp/auth/whoami", auth.Middleware(serveWhoAmI(collector)))
	return withRequestID(http.MaxBytesHandler(mux, maxMCPBodyBytes)), nil
}

// newToolsHandler serves the collector tools as stateless Streamable HTTP.
// The audit line wraps route, which prepares the credential of each tool call.
func newToolsHandler(collector *client.Client, route ...mcp.Middleware) (http.Handler, error) {
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
		tools.RegisterClusterTools, tools.RegisterNodeTools, tools.RegisterServiceTools, tools.RegisterServiceActionTools, tools.RegisterAlertTools, tools.RegisterCheckTools, tools.RegisterPackageTools, tools.RegisterCollectorEventTools, tools.RegisterComplianceTools,
	} {
		if err := register(registrar, service); err != nil {
			return nil, err
		}
	}
	// Cross-origin browser requests are refused with 403, as the MCP SDK
	// recommends, by the protection of net/http around its handler.
	return http.NewCrossOriginProtection().Handler(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
	})), nil
}

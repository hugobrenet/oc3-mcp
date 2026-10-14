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
	serverName    = "oc3-mcp"
	serverVersion = "v0.1.0"
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
	handler, err := newHandler(collector)
	if err != nil {
		return err
	}
	listener, err := listenDelegated(cfg.DelegatedSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	log.Printf("%s %s listening for delegated tokens on unix:%s", serverName, serverVersion, cfg.DelegatedSocket)
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown MCP: %w", err)
	}
	if err := <-serveErrors; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve MCP during shutdown: %w", err)
	}
	return nil
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

// newHandler serves the Unix socket. Every route but /health requires the
// OpenID Connect access token of the caller, which the collector verifies.
func newHandler(collector *client.Client) (http.Handler, error) {
	service := core.New(collector)
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	registrar, err := tools.NewRegistrar(server)
	if err != nil {
		return nil, err
	}
	server.AddReceivingMiddleware(auditToolCalls)
	for _, register := range []func(*tools.Registrar, *core.Service) error{
		tools.RegisterClusterTools, tools.RegisterNodeTools, tools.RegisterServiceTools, tools.RegisterServiceActionTools, tools.RegisterAlertTools, tools.RegisterCheckTools, tools.RegisterPackageTools, tools.RegisterCollectorEventTools, tools.RegisterComplianceTools,
	} {
		if err := register(registrar, service); err != nil {
			return nil, err
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", getHealth)
	// Cross-origin browser requests are refused with 403, as the MCP SDK
	// recommends, by the protection of net/http around its handler.
	mux.Handle("/mcp", auth.Middleware(http.NewCrossOriginProtection().Handler(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
	}))))
	mux.Handle("GET /mcp/auth/whoami", auth.Middleware(serveWhoAmI(collector)))
	return withRequestID(http.MaxBytesHandler(mux, 1<<20)), nil
}

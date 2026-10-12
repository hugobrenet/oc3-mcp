package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	handler, err := newHandler(core.New(collector))
	if err != nil {
		return err
	}
	// Validate TLS material before binding the TCP listener.
	certificate, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return fmt.Errorf("load MCP TLS certificate and private key: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for MCP HTTPS: %w", err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler: handler,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		},
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.ServeTLS(listener, "", "") }()
	log.Printf("%s %s listening on https://%s", serverName, serverVersion, listener.Addr())
	select {
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve MCP HTTPS: %w", err)
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
		return fmt.Errorf("shutdown MCP HTTPS: %w", err)
	}
	if err := <-serveErrors; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve MCP HTTPS during shutdown: %w", err)
	}
	return nil
}

// newCollectorClient reads the collector account and prepares the client
// without contacting the collector.
func newCollectorClient(cfg config.Config) (*client.Client, error) {
	collectorURL, err := url.Parse(cfg.CollectorURL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenSVC collector URL: %w", err)
	}
	credentials, err := client.LoadBasicCredentials(cfg.CollectorCredentialsFile, collectorURL.Hostname())
	if err != nil {
		return nil, err
	}
	httpClient, err := client.NewHTTPClient(cfg.CollectorURL, cfg.CollectorCAFile, credentials)
	if err != nil {
		return nil, err
	}
	return client.New(cfg.CollectorURL, httpClient)
}

func newHandler(service *core.Service) (http.Handler, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	registrar, err := tools.NewRegistrar(server)
	if err != nil {
		return nil, err
	}
	for _, register := range []func(*tools.Registrar, *core.Service) error{
		tools.RegisterClusterTools, tools.RegisterNodeTools, tools.RegisterServiceTools, tools.RegisterServiceActionTools, tools.RegisterAlertTools, tools.RegisterCheckTools,
	} {
		if err := register(registrar, service); err != nil {
			return nil, err
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", getHealth)
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
		CrossOriginProtection: &http.CrossOriginProtection{},
	}))
	return http.MaxBytesHandler(mux, 1<<20), nil
}

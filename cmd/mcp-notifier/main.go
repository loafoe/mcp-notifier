// Command mcp-notifier runs an MCP server exposing Slack and Microsoft Teams
// webhook notification tools.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/loafoe/mcp-notifier/internal/config"
	"github.com/loafoe/mcp-notifier/internal/mcpserver"
)

func main() {
	var (
		configPath = flag.String("config", envOr("MCP_NOTIFIER_CONFIG", "/etc/mcp-notifier/config.yaml"), "path to config.yaml")
		transport  = flag.String("transport", envOr("MCP_NOTIFIER_TRANSPORT", "http"), "transport to serve: http or stdio")
		addr       = flag.String("addr", envOr("MCP_NOTIFIER_ADDR", ":8080"), "listen address for the http transport")
		mcpPath    = flag.String("path", envOr("MCP_NOTIFIER_PATH", "/mcp"), "HTTP path serving the MCP endpoint")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if len(cfg.Slack.Webhooks) == 0 && len(cfg.Teams.Webhooks) == 0 {
		logger.Error("no notification providers configured: set at least one of slack.webhooks or teams.webhooks")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch *transport {
	case "stdio":
		server := mcpserver.New(cfg, logger)
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("stdio server exited with error", "error", err)
			os.Exit(1)
		}
	case "http":
		if err := runHTTP(ctx, cfg, logger, *addr, *mcpPath); err != nil {
			logger.Error("http server exited with error", "error", err)
			os.Exit(1)
		}
	default:
		logger.Error("unknown transport", "transport", *transport)
		os.Exit(1)
	}
}

func runHTTP(ctx context.Context, cfg *config.Config, logger *slog.Logger, addr, mcpPath string) error {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpserver.New(cfg, logger)
	}, nil)

	mux := http.NewServeMux()
	mux.Handle(mcpPath, handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr, "mcp_path", mcpPath)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

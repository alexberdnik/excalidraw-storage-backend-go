// Command server runs the Excalidraw storage backend.
//
// It stores scenes, rooms and files in a single SQLite database and exposes
// the same REST API as the original NestJS implementation.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alswl/excalidraw-storage-backend-go/internal/api"
	"github.com/alswl/excalidraw-storage-backend-go/internal/config"
	"github.com/alswl/excalidraw-storage-backend-go/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	st, err := store.Open(cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer st.Close()

	listener, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr(), err)
	}

	handler := api.New(cfg, st, logger)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	prefix := cfg.GlobalPrefix
	if prefix == "" {
		prefix = "/"
	}
	logger.Info("excalidraw storage backend listening",
		"addr", listener.Addr().String(),
		"prefix", prefix,
		"database", cfg.SQLitePath,
		"body_limit_bytes", cfg.BodyLimit,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-serveErr
}

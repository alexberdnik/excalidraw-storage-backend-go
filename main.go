// Command server runs the Excalidraw storage backend.
//
// It stores scenes, rooms and files either in a single SQLite database or as
// plain files. STORE_BACKEND selects the backend.
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

	"github.com/alexberdnik/excalidraw-storage-backend-go/internal/api"
	"github.com/alexberdnik/excalidraw-storage-backend-go/internal/config"
	"github.com/alexberdnik/excalidraw-storage-backend-go/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func openStore(cfg config.Config) (store.Store, error) {
	switch cfg.Backend {
	case config.BackendFiles:
		st, err := store.OpenFiles(cfg.FilesPath)
		if err != nil {
			return nil, err
		}
		return st, nil
	case config.BackendSQLite:
		st, err := store.OpenSQLite(cfg.SQLitePath)
		if err != nil {
			return nil, err
		}
		return st, nil
	default:
		return nil, fmt.Errorf("unknown store backend %q", cfg.Backend)
	}
}

func storageLocation(cfg config.Config) string {
	if cfg.Backend == config.BackendFiles {
		return cfg.FilesPath
	}
	return cfg.SQLitePath
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

	st, err := openStore(cfg)
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
		"backend", string(cfg.Backend),
		"location", storageLocation(cfg),
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

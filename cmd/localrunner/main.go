package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"localrunner/internal/api"
	"localrunner/internal/config"
	"localrunner/internal/runner"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manager := runner.NewManager(cfg.Runner, logger)
	for _, profile := range cfg.Profiles {
		if err := manager.AddProfile(profile); err != nil {
			logger.Error("invalid model profile", "profile", profile.Name, "error", err)
			os.Exit(1)
		}
	}
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           api.NewHandlerWithConfig(manager, logger, cfg.API),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("LocalRunner listening", "address", cfg.ListenAddress)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("HTTP server failed", "error", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown failed", "error", err)
	}
}

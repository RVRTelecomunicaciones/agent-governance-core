package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/bootstrap"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/config"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/database"
	obslog "github.com/russellcxl/agent-governance-core/internal/infrastructure/obs/log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	// Wrap the JSON handler with TraceHandler so every log line emitted within
	// a request context automatically carries trace_id and span_id attributes
	// (ADR-0005 P2.2d).
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	logger := slog.New(obslog.NewTraceHandler(base))
	slog.SetDefault(logger)

	pool, err := database.NewPool(context.Background(), cfg.DB)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	app, err := bootstrap.Wire(context.Background(), pool, logger, cfg)
	if err != nil {
		return fmt.Errorf("wiring: %w", err)
	}
	defer app.OTelShutdown(context.Background())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.ServerPort),
		Handler: app.HTTPServer,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		logger.Info("server starting", "port", cfg.ServerPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
		}
	}()

	<-done
	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/history"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/notifier"
	"github.com/ko5tas/t212/internal/poller"
	"github.com/ko5tas/t212/internal/server"
	"github.com/ko5tas/t212/internal/store"
)

func runServe() error {
	apiKey := os.Getenv("T212_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("T212_API_KEY environment variable is not set")
	}
	apiSecret := os.Getenv("T212_API_SECRET")
	if apiSecret == "" {
		return fmt.Errorf("T212_API_SECRET environment variable is not set")
	}

	signalNumber := os.Getenv("SIGNAL_NUMBER")
	signalCLIPath := os.Getenv("SIGNAL_CLI_PATH")
	if signalCLIPath == "" {
		signalCLIPath = "/usr/local/bin/signal-cli"
	}
	signalCLIConfig := os.Getenv("SIGNAL_CLI_CONFIG")
	if signalCLIConfig == "" {
		signalCLIConfig = "/var/lib/t212/signal-cli"
	}

	port := os.Getenv("T212_PORT")
	if port == "" {
		port = "8080"
	}

	const threshold = 1.00

	slog.Info("t212 serve starting",
		"port", port,
		"threshold", threshold,
		"signal_enabled", signalNumber != "",
	)

	apiClient := api.NewClient(apiKey, apiSecret, "https://live.trading212.com", nil)
	if err := apiClient.LoadMetadata(context.Background()); err != nil {
		slog.Warn("failed to load instrument metadata", "err", err)
	}
	s := store.New()
	h := hub.New()

	hs := history.NewStore()
	refreshCh := make(chan string, 8)

	var n poller.Notifier
	if signalNumber != "" {
		n = notifier.New(signalCLIPath, signalNumber, signalCLIConfig)
	}

	p := poller.New(apiClient, s, h, threshold, n,
		poller.WithHistoryStore(hs),
		poller.WithRefreshChan(refreshCh),
	)
	srv := server.New(h, ":"+port, server.WithRefreshChan(refreshCh))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go p.Run(ctx)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start(ctx) }()

	select {
	case <-ctx.Done():
		slog.Info("shutting down gracefully")
		return nil
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	}
}

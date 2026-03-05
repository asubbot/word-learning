package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"word-learning/internal/bot"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := bot.LoadConfigFromEnv()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := bot.Run(ctx, cfg, logger); err != nil {
		logger.Error("run bot", "error", err)
		os.Exit(1)
	}
}

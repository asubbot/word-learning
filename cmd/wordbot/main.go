package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"word-learning-cli/internal/bot"
)

func main() {
	var dbPath string
	flag.StringVar(&dbPath, "db", "", "Path to SQLite database file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := bot.LoadConfigFromEnv(dbPath)
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

package main

import (
	"log/slog"
	"os"

	"utobot/service"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	hasDebug := false
	for _, arg := range os.Args[1:] {
		if arg == "--debug" {
			hasDebug = true
			break
		}
	}

	if hasDebug {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
		slog.SetDefault(logger)
		slog.Debug("debug mode enabled")
	}

	if err := service.Run(os.Args[1:]); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

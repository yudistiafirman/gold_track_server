package logger

import (
	"log/slog"
	"os"
)

// New builds a structured slog.Logger. Production/staging environments get
// JSON output (easy to ship to log aggregators); local gets human-readable text.
func New(env string) *slog.Logger {
	level := slog.LevelInfo
	if env == "local" {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if env == "local" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

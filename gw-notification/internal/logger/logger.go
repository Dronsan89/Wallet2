// Package logger настраивает структурированное логирование (slog, JSON).
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New создаёт JSON-логгер с указанным уровнем и постоянным полем service.
// Уровни: debug, info, warn, error (регистр не важен).
func New(level, service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     parseLevel(level),
		AddSource: false,
	})

	return slog.New(handler).With("service", service)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
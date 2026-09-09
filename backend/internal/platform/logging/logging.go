package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

func New() *slog.Logger {
	return NewWithConfig(os.Getenv("LOG_FORMAT"), os.Getenv("LOG_LEVEL"))
}

func NewWithConfig(format, levelStr string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(levelStr)}

	var handler slog.Handler
	if strings.EqualFold(strings.TrimSpace(format), "text") {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
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

type contextKey int

const loggerKey contextKey = 0

func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func FromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return fallback
}

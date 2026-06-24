package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"protean-provider/internal/config"
)

// contextKey is a private type to avoid key collisions in context values.
type contextKey struct{}

// New creates a *slog.Logger from the given LoggingConfig and sets it as the
// global default logger (slog.SetDefault). It returns the logger so callers can
// inject it explicitly if preferred.
func New(cfg config.LoggingConfig) *slog.Logger {
	level := parseLevel(cfg.Level)

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				source, ok := a.Value.Any().(*slog.Source)
				if ok && source != nil {
					const searchStr = "protean-provider/"
					if idx := strings.LastIndex(source.File, searchStr); idx != -1 {
						source.File = source.File[idx:]
					}
				}
			}
			return a
		},
	}

	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	l := slog.New(handler)
	slog.SetDefault(l)
	return l
}

// WithLogger returns a new context that carries the given logger.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext extracts the logger stored in ctx.
// If no logger is found, it returns slog.Default() so callers never get nil.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// parseLevel converts a string level name to slog.Level.
// Defaults to slog.LevelInfo for unknown strings.
func parseLevel(s string) slog.Level {
	switch s {
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
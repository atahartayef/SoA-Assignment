package platform

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey string

const correlationKey ctxKey = "correlation_id"
const orderKey ctxKey = "order_id"

// WithCorrelation puts the correlation ID on the context so every later log line
// in the same unit of work carries it without being passed it by hand.
func WithCorrelation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

func WithOrder(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, orderKey, id)
}

func Correlation(ctx context.Context) string {
	if value, ok := ctx.Value(correlationKey).(string); ok {
		return value
	}
	return "none"
}

// NewLogger returns a JSON logger writing to stdout, which is the only log
// destination a container should know about (12-factor XI).
func NewLogger(cfg Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With(
		slog.String("service", cfg.ServiceName),
		slog.String("version", cfg.Version),
		slog.String("environment", cfg.Environment),
	)
}

// Ctx returns a logger already carrying the correlation and order IDs from ctx,
// so the mandatory fields of the log schema cannot be forgotten at a call site.
func Ctx(ctx context.Context, logger *slog.Logger) *slog.Logger {
	logger = logger.With(slog.String("correlation_id", Correlation(ctx)))
	if order, ok := ctx.Value(orderKey).(string); ok && order != "" {
		logger = logger.With(slog.String("order_id", order))
	}
	return logger
}

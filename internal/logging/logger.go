package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Logger is the global logger instance configured for the application.
var Logger *slog.Logger

// InitLogger configures the global logger using a Datadog-friendly JSON handler.
func InitLogger(level string, service string) {
	handlerLevel := parseLevel(level)
	options := &slog.HandlerOptions{
		Level: handlerLevel,
	}
	jsonHandler := slog.NewJSONHandler(os.Stdout, options)
	ddHandler := &datadogHandler{
		next:    jsonHandler,
		service: service,
	}
	Logger = slog.New(ddHandler)
	slog.SetDefault(Logger)
}

// GetLogger returns the global logger instance.
func GetLogger() *slog.Logger {
	return Logger
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
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

type datadogHandler struct {
	next    slog.Handler
	service string
}

func (h *datadogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *datadogHandler) Handle(ctx context.Context, record slog.Record) error {
	clone := record.Clone()
	clone.AddAttrs(
		slog.String("service", h.service),
		slog.String("status", levelToStatus(clone.Level)),
		slog.String("dd.trace_id", ""),
		slog.String("dd.span_id", ""),
		slog.String("message", clone.Message),
	)
	return h.next.Handle(ctx, clone)
}

func (h *datadogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &datadogHandler{
		next:    h.next.WithAttrs(attrs),
		service: h.service,
	}
}

func (h *datadogHandler) WithGroup(name string) slog.Handler {
	return &datadogHandler{
		next:    h.next.WithGroup(name),
		service: h.service,
	}
}

func levelToStatus(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelWarn:
		return "warning"
	case slog.LevelError:
		return "error"
	default:
		return "info"
	}
}

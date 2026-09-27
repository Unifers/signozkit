package signozkit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
)

// newLogger builds the process logger.
//
// Local runs get human-readable text, everything else JSON. A non-nil
// provider adds the collector as a second destination; stdout keeps its copy
// either way, because when the exporter is the thing that broke, the
// container's own logs are what is left.
func newLogger(w io.Writer, cfg Config, provider *sdklog.LoggerProvider) *slog.Logger {
	threshold := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: threshold}

	var stdout slog.Handler
	if cfg.IsLocal() {
		stdout = slog.NewTextHandler(w, opts)
	} else {
		stdout = slog.NewJSONHandler(w, opts)
	}
	handlers := []slog.Handler{traceIDs{stdout}}

	if provider != nil {
		handlers = append(handlers, otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(provider)))
	}

	return slog.New(fanout{level: threshold, handlers: handlers}).With(slog.String("service", cfg.ServiceName))
}

// fanout writes every record to each of its handlers.
//
// The level is enforced here rather than left to the handlers: the OTel bridge
// asks its provider what is enabled and the provider admits everything, so
// without this gate a service set to info would still export debug records.
type fanout struct {
	level    slog.Level
	handlers []slog.Handler
}

func (f fanout) Enabled(_ context.Context, level slog.Level) bool { return level >= f.level }

func (f fanout) Handle(ctx context.Context, record slog.Record) error {
	var problems []error
	for _, h := range f.handlers {
		if err := h.Handle(ctx, record.Clone()); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return fanout{level: f.level, handlers: next}
}

func (f fanout) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithGroup(name)
	}
	return fanout{level: f.level, handlers: next}
}

// traceIDs adds trace_id and span_id to stdout records logged inside a span,
// so a line in the container's logs can be found in SigNoz. The OTLP copy
// needs no such help: the bridge carries the span context natively.
type traceIDs struct{ slog.Handler }

func (h traceIDs) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceIDs) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceIDs{h.Handler.WithAttrs(attrs)}
}

func (h traceIDs) WithGroup(name string) slog.Handler {
	return traceIDs{h.Handler.WithGroup(name)}
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

package signozkit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ShutdownTimeout bounds the final flush in [Telemetry.Shutdown]. Short on
// purpose: every record is already on stdout, and a process that will not exit
// is worse than a last batch that never reached the collector.
var ShutdownTimeout = 5 * time.Second

// Telemetry is what [Setup] installed. Call Shutdown before the process exits.
type Telemetry struct {
	// Logger is the logger Setup installed as slog.Default().
	Logger *slog.Logger

	// Config is the configuration Setup ran with.
	Config Config

	logs   *sdklog.LoggerProvider
	traces *sdktrace.TracerProvider
	once   sync.Once
	err    error
}

// Setup reads the configuration from the environment (see [FromEnv]) and
// installs logging and tracing. service is the name used when
// OTEL_SERVICE_NAME is unset.
func Setup(ctx context.Context, service string) (*Telemetry, error) {
	return SetupConfig(ctx, FromEnv(service))
}

// SetupConfig installs logging and tracing from cfg:
//
//   - slog.Default() (and the standard log package) write to stdout and, when
//     cfg.Exporting(), to the collector;
//   - otel.GetTracerProvider() exports spans when cfg.Exporting(), and is a
//     no-op otherwise;
//   - otel.GetTextMapPropagator() reads and writes W3C trace context.
//
// An invalid endpoint is an error rather than a silent no-op: a service that
// quietly exports nowhere is found out during the incident it was meant to
// explain.
func SetupConfig(ctx context.Context, cfg Config) (*Telemetry, error) {
	return setup(ctx, cfg, os.Stdout)
}

func setup(ctx context.Context, cfg Config, stdout io.Writer) (*Telemetry, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	t := &Telemetry{Config: cfg}

	if cfg.Exporting() {
		logs, err := newLoggerProvider(ctx, cfg)
		if err != nil {
			return nil, err
		}
		traces, err := newTracerProvider(ctx, cfg)
		if err != nil {
			_ = logs.Shutdown(ctx) // nothing was exported through it yet
			return nil, err
		}
		t.logs, t.traces = logs, traces
		otel.SetTracerProvider(traces)
	}

	t.Logger = newLogger(stdout, cfg, t.logs)
	slog.SetDefault(t.Logger)

	switch {
	case cfg.Exporting():
		// An export that fails after start reaches the global error handler,
		// whose default writes bare lines to stderr that look nothing like the
		// service's own logs. Routing it through the logger is the difference
		// between "SigNoz is empty" and "the collector answered 403".
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			t.Logger.Error("telemetry export failed", "error", err)
		}))

		t.Logger.Info("exporting logs and traces", "endpoint", cfg.Endpoint, "environment", cfg.Environment)
	case cfg.Endpoint != "":
		t.Logger.Info("telemetry export is off", "environment", cfg.Environment, "enabled", cfg.Enabled)
	}
	return t, nil
}

// Exporting reports whether logs and traces are being sent to the collector.
func (t *Telemetry) Exporting() bool { return t.logs != nil }

// Shutdown flushes whatever is still batched and stops both pipelines. It
// waits at most [ShutdownTimeout], is safe to call more than once, and is a
// no-op when nothing is exported. Call it after the last request has finished,
// so the lines logged while shutting down are exported too.
func (t *Telemetry) Shutdown() error {
	t.once.Do(func() {
		if t.logs == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()

		// Traces first: their shutdown may still log, and the log pipeline
		// must be alive to carry that line.
		t.err = errors.Join(t.traces.Shutdown(ctx), t.logs.Shutdown(ctx))
		if t.err != nil {
			t.Logger.Error("flushing telemetry failed", "error", t.err)
		}
	})
	return t.err
}

// Tracer returns a tracer from the global provider, for spans inside a
// request (a database call, an outbound API call). name is the
// instrumentation scope, e.g. "orders/db".
//
//	ctx, span := signozkit.Tracer("orders/db").Start(ctx, "load_order")
//	defer span.End()
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

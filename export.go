package signozkit

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// newLoggerProvider builds the OTLP/HTTP pipeline logs are exported on.
// Records leave in batches (every second, or every 512 records), so the caller
// must shut the provider down to flush the last one.
func newLoggerProvider(ctx context.Context, cfg Config) (*sdklog.LoggerProvider, error) {
	endpoint, err := signalURL(cfg.Endpoint, "/v1/logs")
	if err != nil {
		return nil, err
	}

	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(endpoint),
		otlploghttp.WithHeaders(keyValues(cfg.Headers)),
	)
	if err != nil {
		return nil, fmt.Errorf("signozkit: otlp log exporter: %w", err)
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(newResource(cfg)),
	), nil
}

// newTracerProvider builds the OTLP/HTTP pipeline spans are exported on.
// Spans are what list a service on SigNoz's Services page; logs alone never do.
func newTracerProvider(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, error) {
	endpoint, err := signalURL(cfg.Endpoint, "/v1/traces")
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithHeaders(keyValues(cfg.Headers)),
	)
	if err != nil {
		return nil, fmt.Errorf("signozkit: otlp trace exporter: %w", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(newResource(cfg)),
	), nil
}

// newResource is what every record and span is stamped with. Logs and traces
// share it, which is what files both under the same service in SigNoz.
func newResource(cfg Config) *resource.Resource {
	var attrs []attribute.KeyValue
	for k, v := range keyValues(cfg.ResourceAttributes) {
		attrs = append(attrs, attribute.String(k, v))
	}
	attrs = append(attrs,
		semconv.ServiceName(cfg.ServiceName),
		semconv.DeploymentEnvironmentNameKey.String(cfg.Environment),
	)
	return resource.NewWithAttributes(semconv.SchemaURL, attrs...)
}

// signalURL appends a signal path to a base endpoint, which is what the
// exporters want: given only a host they post to the root, and a collector
// answers that with a 404 that no log line ever explains. A trailing slash on
// the base and an already-present signal path are both tolerated.
func signalURL(raw, path string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("signozkit: endpoint %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("signozkit: endpoint %q must start with http:// or https://", raw)
	}
	base := strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(base, path) {
		base += path
	}
	u.Path = base
	u.RawPath = ""
	return u.String(), nil
}

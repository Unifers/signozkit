package signozkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// ---------- config ----------

func TestFromEnv(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("SIGNOZ_ENABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	cfg := FromEnv("Orders")
	if cfg.ServiceName != "Orders" || cfg.Environment != "local" || !cfg.Enabled || cfg.LogLevel != "info" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Exporting() {
		t.Error("local with no endpoint must not export")
	}

	t.Setenv("OTEL_SERVICE_NAME", "Billing")
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example.com")
	cfg = FromEnv("Orders")
	if cfg.ServiceName != "Billing" {
		t.Errorf("OTEL_SERVICE_NAME should win over the default, got %q", cfg.ServiceName)
	}
	if !cfg.Exporting() {
		t.Error("production with an endpoint should export")
	}

	t.Setenv("SIGNOZ_ENABLED", "false")
	if FromEnv("Orders").Exporting() {
		t.Error("SIGNOZ_ENABLED=false must stop export")
	}
}

func TestExportingIsOffForLocalEnvironments(t *testing.T) {
	for _, env := range []string{"local", "development", "Development"} {
		cfg := Config{Environment: env, Enabled: true, Endpoint: "http://x:4318", LocalEnvironments: DefaultLocalEnvironments}
		if cfg.Exporting() {
			t.Errorf("environment %q must not export", env)
		}
	}
}

func TestSignalURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://collector:4318", "http://collector:4318/v1/logs"},
		{"http://collector:4318/", "http://collector:4318/v1/logs"},
		{"https://collector.example.com", "https://collector.example.com/v1/logs"},
		{"https://example.com/otel/", "https://example.com/otel/v1/logs"},
		{"http://collector:4318/v1/logs", "http://collector:4318/v1/logs"},
	}
	for _, c := range cases {
		got, err := signalURL(c.in, "/v1/logs")
		if err != nil || got != c.want {
			t.Errorf("signalURL(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := signalURL("collector:4318", "/v1/logs"); err == nil {
		t.Error("an endpoint without http:// or https:// must be rejected")
	}
}

func TestKeyValues(t *testing.T) {
	got := keyValues(" a=1, b = 2 ,bad,=x,c=")
	if len(got) != 2 || got["a"] != "1" || got["b"] != "2" {
		t.Errorf("unexpected parse: %v", got)
	}
}

// ---------- logger ----------

func TestLocalLoggerIsTextAndGatesLevel(t *testing.T) {
	var buf bytes.Buffer
	tel, err := setup(context.Background(), Config{ServiceName: "Orders", Environment: "local", LogLevel: "info", LocalEnvironments: DefaultLocalEnvironments}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer tel.Shutdown()

	slog.Debug("hidden")
	slog.Info("shown", "k", "v")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Error("debug record logged at info level")
	}
	if !strings.Contains(out, "msg=shown") || !strings.Contains(out, "service=Orders") {
		t.Errorf("expected text output with service, got %q", out)
	}
	if tel.Exporting() {
		t.Error("local config must not export")
	}
}

// ---------- end to end against a fake collector ----------

type fakeCollector struct {
	mu     sync.Mutex
	bodies map[string][][]byte
}

func (c *fakeCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.bodies[r.URL.Path] = append(c.bodies[r.URL.Path], body)
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-protobuf")
}

func (c *fakeCollector) received(path, needle string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range c.bodies[path] {
		if bytes.Contains(b, []byte(needle)) {
			return true
		}
	}
	return false
}

func TestSetupExportsLogsAndTraces(t *testing.T) {
	collector := &fakeCollector{bodies: map[string][][]byte{}}
	srv := httptest.NewServer(collector)
	defer srv.Close()

	var stdout bytes.Buffer
	tel, err := setup(context.Background(), Config{
		ServiceName:       "Orders",
		Environment:       "production",
		Endpoint:          srv.URL + "/",
		Enabled:           true,
		LogLevel:          "info",
		LocalEnvironments: DefaultLocalEnvironments,
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !tel.Exporting() {
		t.Fatal("expected export to be on")
	}

	handler := HTTP("/orders", func(_ http.ResponseWriter, r *http.Request) {
		slog.InfoContext(r.Context(), "order_created", "order_id", "o-123")
	})
	handler(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/orders", nil))

	if err := tel.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !collector.received("/v1/logs", "order_created") || !collector.received("/v1/logs", "Orders") {
		t.Error("collector did not receive the log record with its service name")
	}
	if !collector.received("/v1/traces", "POST /orders") || !collector.received("/v1/traces", "Orders") {
		t.Error("collector did not receive the request span with its service name")
	}

	// stdout keeps a JSON copy, carrying the trace id of the request span.
	var line map[string]any
	for _, l := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if strings.Contains(l, "order_created") {
			if err := json.Unmarshal([]byte(l), &line); err != nil {
				t.Fatalf("stdout line is not JSON: %q", l)
			}
		}
	}
	if line["trace_id"] == nil || line["service"] != "Orders" {
		t.Errorf("stdout record missing trace_id/service: %v", line)
	}
}

func TestSetupRejectsBadEndpoint(t *testing.T) {
	_, err := setup(context.Background(), Config{
		ServiceName: "Orders", Environment: "production", Endpoint: "collector:4318",
		Enabled: true, LocalEnvironments: DefaultLocalEnvironments,
	}, io.Discard)
	if err == nil {
		t.Error("expected an error for an endpoint without a scheme")
	}
}

// ---------- HTTP middleware ----------

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

func TestHTTPSpanNameStatusAndErrors(t *testing.T) {
	cases := []struct {
		status    int
		wantError bool
	}{
		{http.StatusOK, false},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, true},
	}
	for _, c := range cases {
		rec := recordSpans(t)
		h := HTTP("/users/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(c.status) })
		h(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users/42", nil))

		spans := rec.Ended()
		if len(spans) != 1 {
			t.Fatalf("status %d: expected 1 span, got %d", c.status, len(spans))
		}
		s := spans[0]
		if s.Name() != "GET /users/{id}" {
			t.Errorf("span name = %q, want route pattern", s.Name())
		}
		if gotError := s.Status().Code == codes.Error; gotError != c.wantError {
			t.Errorf("status %d: error = %v, want %v", c.status, gotError, c.wantError)
		}
	}
}

func TestHTTPContinuesCallerTrace(t *testing.T) {
	_, _ = setup(context.Background(), Config{Environment: "local", LocalEnvironments: DefaultLocalEnvironments}, io.Discard) // installs the propagator
	rec := recordSpans(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/orders", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	HTTP("/orders", func(http.ResponseWriter, *http.Request) {})(httptest.NewRecorder(), req)

	s := rec.Ended()[0]
	if got := s.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id = %s, want the caller's", got)
	}
	if got := s.Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Errorf("parent span = %s, want the caller's span", got)
	}
}

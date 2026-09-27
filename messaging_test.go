package signozkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// recording installs a tracer provider that records every span, and the
// propagator Setup would have installed, so the messaging helpers can be tested
// without a collector.
func recording(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	return recorder
}

func TestProducingAndConsumingShareOneTrace(t *testing.T) {
	recorder := recording(t)

	headers := map[string]string{}
	produceCtx, producer := Producing(context.Background(), "campaign.step", headers)
	producer.End()

	if headers["traceparent"] == "" {
		t.Fatal("Producing wrote no traceparent into the carrier")
	}

	// A fresh context, as a consumer in another process would have.
	consumeCtx, consumer := Consuming(context.Background(), "campaign.step", headers)
	consumer.End()

	produced := trace.SpanContextFromContext(produceCtx)
	consumed := trace.SpanContextFromContext(consumeCtx)
	if produced.TraceID() != consumed.TraceID() {
		t.Errorf("consumer started trace %s, want the producer's %s", consumed.TraceID(), produced.TraceID())
	}

	ended := recorder.Ended()
	if len(ended) != 2 {
		t.Fatalf("recorded %d spans, want 2", len(ended))
	}
	if parent := ended[1].Parent().SpanID(); parent != produced.SpanID() {
		t.Errorf("consumer's parent is %s, want the producer's span %s", parent, produced.SpanID())
	}
}

func TestConsumingWithoutTraceContextStartsItsOwnTrace(t *testing.T) {
	recording(t)

	ctx, span := Consuming(context.Background(), "campaign.step", map[string]string{})
	defer span.End()

	if !trace.SpanContextFromContext(ctx).IsValid() {
		t.Error("a carrier with no trace context should still produce a span")
	}
}

func TestInjectExtractRoundTrip(t *testing.T) {
	recording(t)

	ctx, span := otel.Tracer("test").Start(context.Background(), "work")
	defer span.End()

	carrier := map[string]string{}
	Inject(ctx, carrier)

	got := trace.SpanContextFromContext(Extract(context.Background(), carrier))
	if got.TraceID() != trace.SpanContextFromContext(ctx).TraceID() {
		t.Errorf("extracted trace %s, want %s", got.TraceID(), trace.SpanContextFromContext(ctx).TraceID())
	}
}

func TestTransportSendsTraceHeaders(t *testing.T) {
	recording(t)

	var traceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
	}))
	defer srv.Close()

	ctx, span := otel.Tracer("test").Start(context.Background(), "call")
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if traceparent == "" {
		t.Error("outbound request carried no traceparent")
	}
}

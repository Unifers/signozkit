package signozkit

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const messagingScope = "github.com/unifers/signozkit/messaging"

// Producing starts a span for a message about to be published and writes the
// trace context into carrier, so the consumer continues this trace instead of
// starting one of its own.
//
// destination is the queue or topic the message is addressed to. The caller
// ends the span, and copies carrier into whatever the broker calls headers:
//
//	ctx, span := signozkit.Producing(ctx, "campaign.step", headers)
//	defer span.End()
//
// Nothing here knows about any particular broker — a map of strings is all the
// trace context needs, and converting it to the client's own header type is a
// few lines at the call site.
func Producing(ctx context.Context, destination string, carrier map[string]string) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(messagingScope).Start(ctx, "send "+destination,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingDestinationName(destination),
			semconv.MessagingOperationTypeSend,
		))

	Inject(ctx, carrier)
	return ctx, span
}

// Consuming continues the trace that carrier carries and starts a span for
// handling the message.
//
// The returned context is what the handler must use: it is what links the work
// to the publisher's trace, and what puts trace_id on the lines logged while
// handling it.
//
//	ctx, span := signozkit.Consuming(ctx, "campaign.step", headers)
//	defer span.End()
//
// A carrier without trace context is not an error. The span simply starts a new
// trace, which is what a message published by something uninstrumented deserves.
func Consuming(ctx context.Context, destination string, carrier map[string]string) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(messagingScope).Start(Extract(ctx, carrier), "process "+destination,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingDestinationName(destination),
			semconv.MessagingOperationTypeProcess,
		))
	return ctx, span
}

// Inject writes ctx's W3C trace context into carrier. Producing does this for
// you; reach for Inject directly only when you are not starting a span, such as
// when forwarding a trace through a transport that has no span of its own.
func Inject(ctx context.Context, carrier map[string]string) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(carrier))
}

// Extract returns ctx with the trace context found in carrier, or ctx unchanged
// when it holds none.
func Extract(ctx context.Context, carrier map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}

// Package mongo instruments the MongoDB Go driver v2, which has no upstream
// OpenTelemetry instrumentation: otelmongo covers driver v1 only.
package mongo

import (
	"context"
	"sync"

	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const scope = "github.com/unifers/signozkit/mongo"

// Monitor returns a command monitor that opens a client span per command:
//
//	opts := options.Client().ApplyURI(uri).SetMonitor(signozmongo.Monitor())
//
// Each span is a child of whatever span the calling context carried, so a query
// is attributed to the request or job that made it. Commands the driver issues
// on its own — handshakes, heartbeats, authentication — arrive with no such
// context and become roots, which is what they are.
//
// The command document is never recorded. It holds the values a query ran
// against, which for most services means customer data.
func Monitor() *event.CommandMonitor {
	t := &commandTracer{spans: make(map[commandKey]trace.Span)}
	return &event.CommandMonitor{
		Started:   t.started,
		Succeeded: t.succeeded,
		Failed:    t.failed,
	}
}

// commandKey identifies an in-flight command. A request id is unique per
// connection rather than across the client, so the connection belongs in the key.
type commandKey struct {
	connection string
	request    int64
}

// commandTracer holds the span of every command that has started and not yet
// finished, because the driver reports the two halves as separate events.
type commandTracer struct {
	mu    sync.Mutex
	spans map[commandKey]trace.Span
}

func (t *commandTracer) started(ctx context.Context, e *event.CommandStartedEvent) {
	attrs := []attribute.KeyValue{
		semconv.DBSystemNameMongoDB,
		semconv.DBNamespace(e.DatabaseName),
		semconv.DBOperationName(e.CommandName),
	}

	// A command document names its collection in the field named after the
	// command itself: {"find": "contacts", ...}. Administrative commands such
	// as ping and hello name a number there instead, hence the check.
	name := e.CommandName
	if collection, ok := e.Command.Lookup(e.CommandName).StringValueOK(); ok {
		attrs = append(attrs, semconv.DBCollectionName(collection))
		name += " " + collection
	}

	_, span := otel.Tracer(scope).Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...))

	t.mu.Lock()
	t.spans[commandKey{e.ConnectionID, e.RequestID}] = span
	t.mu.Unlock()
}

func (t *commandTracer) succeeded(_ context.Context, e *event.CommandSucceededEvent) {
	if span := t.take(e.ConnectionID, e.RequestID); span != nil {
		span.End()
	}
}

func (t *commandTracer) failed(_ context.Context, e *event.CommandFailedEvent) {
	span := t.take(e.ConnectionID, e.RequestID)
	if span == nil {
		return
	}
	span.SetStatus(codes.Error, e.Failure.Error())
	span.RecordError(e.Failure)
	span.End()
}

// take removes a command's span and returns it, or nil when the command never
// started — which happens to whatever was in flight when a monitor was installed.
func (t *commandTracer) take(connection string, request int64) trace.Span {
	key := commandKey{connection, request}

	t.mu.Lock()
	defer t.mu.Unlock()

	span, ok := t.spans[key]
	if !ok {
		return nil
	}
	delete(t.spans, key)
	return span
}

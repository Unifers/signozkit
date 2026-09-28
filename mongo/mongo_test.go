package mongo

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recording(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()

	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	return recorder
}

func command(t *testing.T, doc bson.D) bson.Raw {
	t.Helper()

	raw, err := bson.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSucceededCommandIsOneSpan(t *testing.T) {
	recorder := recording(t)
	monitor := Monitor()

	monitor.Started(context.Background(), &event.CommandStartedEvent{
		Command:      command(t, bson.D{{Key: "find", Value: "contacts"}}),
		DatabaseName: "link",
		CommandName:  "find",
		RequestID:    7,
		ConnectionID: "mongo:27017[-1]",
	})
	monitor.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			CommandName:  "find",
			DatabaseName: "link",
			RequestID:    7,
			ConnectionID: "mongo:27017[-1]",
		},
	})

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(ended))
	}

	span := ended[0]
	if span.Name() != "find contacts" {
		t.Errorf("span name %q, want %q", span.Name(), "find contacts")
	}

	want := map[string]string{
		"db.system.name":     "mongodb",
		"db.namespace":       "link",
		"db.operation.name":  "find",
		"db.collection.name": "contacts",
	}
	got := map[string]string{}
	for _, attr := range span.Attributes() {
		got[string(attr.Key)] = attr.Value.AsString()
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("attribute %s is %q, want %q", key, got[key], value)
		}
	}

	if body := span.Attributes(); len(body) != len(want) {
		t.Errorf("span carries %d attributes, want %d — the command document must never be recorded", len(body), len(want))
	}
}

func TestFailedCommandRecordsTheError(t *testing.T) {
	recorder := recording(t)
	monitor := Monitor()

	monitor.Started(context.Background(), &event.CommandStartedEvent{
		Command:      command(t, bson.D{{Key: "insert", Value: "activities"}}),
		DatabaseName: "link",
		CommandName:  "insert",
		RequestID:    9,
		ConnectionID: "mongo:27017[-1]",
	})
	monitor.Failed(context.Background(), &event.CommandFailedEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			CommandName:  "insert",
			DatabaseName: "link",
			RequestID:    9,
			ConnectionID: "mongo:27017[-1]",
		},
		Failure: errors.New("duplicate key"),
	})

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(ended))
	}
	if status := ended[0].Status(); status.Code != codes.Error || status.Description != "duplicate key" {
		t.Errorf("status is %v, want an error saying %q", status, "duplicate key")
	}
	if len(ended[0].Events()) == 0 {
		t.Error("the failure was not recorded on the span")
	}
}

func TestAdministrativeCommandHasNoCollection(t *testing.T) {
	recorder := recording(t)
	monitor := Monitor()

	// ping names a number rather than a collection: {"ping": 1}.
	monitor.Started(context.Background(), &event.CommandStartedEvent{
		Command:      command(t, bson.D{{Key: "ping", Value: 1}}),
		DatabaseName: "admin",
		CommandName:  "ping",
		RequestID:    1,
		ConnectionID: "mongo:27017[-1]",
	})
	monitor.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{CommandName: "ping", RequestID: 1, ConnectionID: "mongo:27017[-1]"},
	})

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(ended))
	}
	if name := ended[0].Name(); name != "ping" {
		t.Errorf("span name %q, want %q", name, "ping")
	}
}

func TestFinishedCommandThatNeverStartedIsIgnored(t *testing.T) {
	recorder := recording(t)
	monitor := Monitor()

	// What the monitor sees for commands already in flight when it is installed.
	monitor.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{CommandName: "find", RequestID: 404, ConnectionID: "mongo:27017[-1]"},
	})

	if ended := recorder.Ended(); len(ended) != 0 {
		t.Errorf("recorded %d spans, want none", len(ended))
	}
}

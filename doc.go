// Package signozkit wires a Go service's logs and traces into SigNoz (or any
// OpenTelemetry collector) with one call.
//
//	tel, err := signozkit.Setup(ctx, "Orders")
//	if err != nil {
//		return err
//	}
//	defer tel.Shutdown()
//
//	mux.Handle("/orders", signozkit.HTTP("/orders", ordersHandler))
//
// After Setup:
//
//   - slog.Default() writes every record to stdout (text locally, JSON when
//     deployed) and, when exporting, to the collector over OTLP/HTTP.
//   - The global OpenTelemetry tracer provider exports spans, so the service
//     appears on SigNoz's Services page, and the W3C propagator is installed
//     so traces continue across services.
//   - Records logged with a context that carries a span (slog.InfoContext and
//     friends) are linked to that trace.
//
// Outbound work is instrumented where the client is built, so a trace does not
// stop at the edge of the process:
//
//   - [Transport] for an http.Client;
//   - [Producing] and [Consuming] for a queue, which carry the trace through a
//     message and into the consumer that handles it.
//
// Databases live in their own modules, so a service that wants logs alone does
// not pull a driver in with them: signozkit/pgx, signozkit/redis and
// signozkit/mongo.
//
// Configuration comes from the standard OpenTelemetry environment variables
// plus two switches; see [FromEnv].
package signozkit

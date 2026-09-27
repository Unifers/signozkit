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
// Configuration comes from the standard OpenTelemetry environment variables
// plus two switches; see [FromEnv].
package signozkit

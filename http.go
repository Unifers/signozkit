package signozkit

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const httpScope = "github.com/unifers/signozkit/http"

// HTTP wraps a handler func in a server span named "<METHOD> <route>" — one
// trace per request, which is what SigNoz's Services page counts. It continues
// the caller's trace when the request carries W3C trace headers.
//
// route should be the pattern, not the concrete path ("/users/{id}", not
// "/users/42"), so every request to the endpoint shares one operation name.
//
//	mux.HandleFunc("/orders", signozkit.HTTP("/orders", ordersHandler))
func HTTP(route string, next http.HandlerFunc) http.HandlerFunc {
	return Handler(route, next).ServeHTTP
}

// Handler is [HTTP] for an http.Handler.
//
//	mux.Handle("/orders", signozkit.Handler("/orders", ordersHandler))
func Handler(route string, next http.Handler) http.Handler {
	tracer := otel.Tracer(httpScope)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method+" "+route, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))

		span.SetAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.HTTPRoute(route),
			semconv.HTTPResponseStatusCode(rec.status),
		)
		// Only 5xx is a server error; a 4xx is the client's mistake and must
		// not count against the service's error rate.
		if rec.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(rec.status))
		}
	})
}

// statusRecorder remembers the status code a handler wrote, for the span.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer (Flush,
// deadlines, hijacking).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

package signozkit

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Transport wraps a round tripper so every outbound request starts a client
// span and carries the W3C trace headers that let the service at the other end
// continue the trace.
//
// A nil base means http.DefaultTransport.
//
//	client := &http.Client{Transport: signozkit.Transport(nil)}
//
// The request must carry a context that holds the calling span —
// http.NewRequestWithContext, not http.NewRequest — or the span is orphaned
// from the work that caused it.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base)
}

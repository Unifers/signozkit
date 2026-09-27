// Command example is the smallest service using signozkit.
//
//	go run ./example                                            # local: text logs, nothing exported
//	ENVIRONMENT=production \
//	OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 \
//	go run ./example                                            # exports to a collector
//
// Then: curl localhost:8080/hello   (PORT changes the port)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/unifers/signozkit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := signozkit.Setup(ctx, "Example")
	if err != nil {
		slog.Error("telemetry setup failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = tel.Shutdown() }() // runs after the server has stopped

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", signozkit.HTTP("/hello", hello))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			stop()
		}
	}()
	slog.Info("listening", "addr", srv.Addr)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func hello(w http.ResponseWriter, r *http.Request) {
	// An inner span, shown nested under "GET /hello" in the trace.
	ctx, span := signozkit.Tracer("example").Start(r.Context(), "build_greeting")
	time.Sleep(5 * time.Millisecond)
	span.End()

	// Logged with the request context, so it is linked to this request's trace.
	slog.InfoContext(ctx, "greeted", "path", r.URL.Path)
	_, _ = w.Write([]byte("hello\n"))
}

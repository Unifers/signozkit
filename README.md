# signozkit

Plug-and-play SigNoz (OpenTelemetry) logs and traces for Go services — in three lines.

```go
tel, err := signozkit.Setup(ctx, "Orders")
if err != nil {
	return err
}
defer tel.Shutdown()

mux.HandleFunc("/orders", signozkit.HTTP("/orders", ordersHandler))
```

That's all. After `Setup`:

- **Logs** — `slog.Info(...)` and friends go to stdout **and** to SigNoz.
- **Traces** — every request wrapped with `signozkit.HTTP` becomes a trace, so
  the service shows up on SigNoz's **Services** page with its request rate,
  latency and error rate.
- **Logs ↔ traces** — anything logged with the request context
  (`slog.InfoContext(r.Context(), ...)`) is linked to that request's trace.
- **Across services** — W3C trace headers are read, so a request that started in
  another traced service continues as the same trace.

## Install

```sh
go get github.com/unifers/signozkit
```

Requires Go 1.25+.

## Configure

Everything comes from environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `OTEL_SERVICE_NAME` | the name passed to `Setup` | Service name in SigNoz (case-sensitive) |
| `ENVIRONMENT` | `local` | `local` / `development` → text logs, **nothing exported**. Anything else → JSON logs, exported |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | *(empty)* | Collector base URL, e.g. `https://collector.example.com` or `http://signoz-otel-collector:4318`. Empty → nothing exported |
| `OTEL_EXPORTER_OTLP_HEADERS` | *(empty)* | Extra headers, `key=value,key=value`. SigNoz Cloud: `signoz-ingestion-key=<key>` |
| `OTEL_RESOURCE_ATTRIBUTES` | *(empty)* | Extra labels on everything, `key=value,key=value` |
| `SIGNOZ_ENABLED` | `true` | `false` stops export and keeps stdout logging |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

A typical deployment:

```env
ENVIRONMENT=production
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com
OTEL_SERVICE_NAME=Orders
```

The `/v1/logs` and `/v1/traces` paths are added for you; a trailing slash on the
endpoint is fine. Export is on only when the environment is not local,
`SIGNOZ_ENABLED` is not `false` and an endpoint is set. Startup logs one line
saying which way it went: `exporting logs and traces` or `telemetry export is off`.

To configure in code instead:

```go
cfg := signozkit.FromEnv("Orders")
cfg.LocalEnvironments = []string{"dev"} // which environments count as a laptop
tel, err := signozkit.SetupConfig(ctx, cfg)
```

## Inner spans

To see where a request spends its time, open spans inside it:

```go
ctx, span := signozkit.Tracer("orders/db").Start(ctx, "load_order")
defer span.End()
```

They appear nested under the request in SigNoz's trace view.

## Shutdown

Logs and spans are sent in batches (about every second). Call
`tel.Shutdown()` **after** your HTTP server has stopped, so the last batch —
including the lines logged while shutting down — is flushed. It waits at most
`signozkit.ShutdownTimeout` (5s). Every record is on stdout too, so nothing is
lost from the container's own logs even if the collector is down.

## Finding it in SigNoz

- **Services** → your service → an operation (e.g. `POST /orders`) → a trace →
  **Logs** shows that request's log lines.
- **Logs Explorer** → filter `service.name = Orders`.

`/health` style endpoints are best left unwrapped: an uptime probe every few
seconds would bury real requests on the Services page.

## Example

[`example/main.go`](example/main.go) is a complete service:

```sh
go run ./example                       # local: text logs, nothing exported
ENVIRONMENT=production OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run ./example
curl localhost:8080/hello
```

## License

MIT

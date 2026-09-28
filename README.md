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

## Outbound calls

A trace stops at the edge of the process unless the client carries it. Wrap the
transport, and use a request that carries the calling context:

```go
client := &http.Client{Transport: signozkit.Transport(nil)}

req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
res, err := client.Do(req)
```

Each call becomes a client span, and the service at the other end continues the
same trace.

## Queues

A job published now and handled later is one trace, if the trace context travels
with the message. Nothing here knows about any particular broker: a
`map[string]string` is all the context needs, and converting it to the client's
own header type is a few lines at the call site.

```go
// Publisher
headers := map[string]string{}
ctx, span := signozkit.Producing(ctx, "campaign.step", headers)
defer span.End()

// Consumer — use the returned ctx, that is what links the work to the publisher
ctx, span := signozkit.Consuming(ctx, "campaign.step", headers)
defer span.End()
```

For RabbitMQ that is `amqp.Table` in both directions; for SQS, message
attributes. A message with no trace context is not an error — the consumer
simply starts a trace of its own.

## Databases

These live in separate modules, so a service that wants logs alone does not pull
a driver in with them.

```sh
go get github.com/unifers/signozkit/pgx
go get github.com/unifers/signozkit/redis
go get github.com/unifers/signozkit/mongo
```

```go
// Postgres (pgx v5)
cfg.ConnConfig.Tracer = signozpgx.Tracer()

// Redis (go-redis v9)
err := signozredis.Instrument(rdb)

// MongoDB (driver v2 — upstream otelmongo covers v1 only)
opts := options.Client().ApplyURI(uri).SetMonitor(signozmongo.Monitor())
```

Query spans are named after the query. With sqlc that is the name in its header
comment (`-- name: ContactsDue :many` becomes `ContactsDue`), because naming a
span from the statement's first line would label every generated query `--`.

Every query becomes a span under the request or job that ran it, provided the
call is made with a context that carries the span — the `…Context` methods on
pgx, the first argument on go-redis, and any Mongo call taking a `ctx`.

Neither query parameters nor Mongo command documents are recorded: those are the
values a statement ran against, which for most services means customer data.

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

A service with logs but no spans never appears on the **Services** page — that
page is built from traces. If a service looks missing, check Logs Explorer
before suspecting the pipeline.

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

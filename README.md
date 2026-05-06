# log

[![Go Reference](https://pkg.go.dev/badge/github.com/dio/logging.svg)](https://pkg.go.dev/github.com/dio/logging)
[![CI](https://github.com/dio/logging/actions/workflows/ci.yml/badge.svg)](https://github.com/dio/logging/actions/workflows/ci.yml)

A slog-backed [tetratelabs/telemetry](https://github.com/tetratelabs/telemetry) logger
optimized for OpenTelemetry, with one guarantee that matters in production:

> **When you silence logs, metrics still fire.**

---

## The problem it solves

In high-traffic services you suppress Info logs to cut noise and cost. The standard
pattern breaks when you do that:

```go
// Traditional: two separate calls
log.Info("request handled", "route", route)   // silenced at Error level → gone
requestCounter.Add(ctx, 1)                     // easy to forget

// What actually happens in production when level=Error:
log.Info("request handled", "route", route)   // ← silenced
// requestCounter.Add forgotten               // ← dashboard goes dark
```

This library fixes it by making log and metric inseparable:

```go
// One call: log + metric always together
logger.Metric(requests).Info("request handled", "route", route)
// level=Error: log silent, metric still fires
// level=Debug: both log and metric fire
```

The metric fires because `RecordContext` is called **before** the level check. For the full
reasoning, see [RATIONALE.md](RATIONALE.md).

---

## Install

```bash
go get github.com/dio/logging
```

---

## Usage

### 1. Wire once in main

```go
import (
    "log/slog"

    "github.com/dio/logging"
    "github.com/tetratelabs/telemetry"
    "github.com/tetratelabs/telemetry/scope"
)

// meterProvider is your existing OTel MeterProvider (Prometheus, OTLP, etc.)
sink := log.NewOTelSink(meterProvider, "myapp")
telemetry.SetGlobalMetricSink(sink)
scope.UseLogger(log.New(slog.Default()))
```

### 2. Declare metrics in library code

No implementation dependency. Library code only imports the telemetry interface:

```go
var (
    routeLabel telemetry.Label
    requests   telemetry.Metric
    errors     telemetry.Metric
)

func init() {
    telemetry.ToGlobalMetricSink(func(ms telemetry.MetricSink) {
        routeLabel = ms.NewLabel("route")
        requests   = ms.NewSum("app_requests_total",  "Total requests handled")
        errors     = ms.NewSum("app_errors_total",    "Total request errors")
    })
}

var log = scope.Register("server", "HTTP server")
```

### 3. Log and emit metrics in one call

```go
// Success path
log.Context(ctx).
    Metric(requests.With(routeLabel.Upsert("/api/v1/users"))).
    Info("request handled", "method", "GET", "status", 200)
// → slog:  level=INFO  msg="request handled" scope=server method=GET status=200
//          trace_id=abc span_id=def  (injected from active OTel span)
// → OTel:  app_requests_total{route="/api/v1/users"} += 1

// Error path
log.Context(ctx).
    Metric(errors.With(routeLabel.Upsert("/api/v1/users"))).
    Error("request failed", err, "method", "GET")
// → slog:  level=ERROR msg="request failed" ... err=context deadline exceeded
// → OTel:  app_errors_total{route="/api/v1/users"} += 1
```

### OTel trace correlation

When a context with an active OTel span is attached via `.Context(ctx)`, `trace_id`
and `span_id` are automatically injected into every log line without manual extraction:

```
level=INFO msg="request handled" scope=server trace_id=c02b2a3a... span_id=d1449529... route=/api/v1/users
```

The same `trace_id` appears in the OTel trace, making cross-signal correlation trivial.

#### How it works

`logger.Context(ctx)` calls `trace.SpanFromContext(ctx)`. If a span is active in the
context, its `TraceID` and `SpanID` are appended to the logger's key-value pairs before
any log call. If no span is active, `SpanContext.IsValid()` is false and nothing is
appended -- no allocation, no overhead.

```go
// Inside slogLogger.Context(ctx):
if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
    c.kvs = append(c.kvs, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
}
```

#### Making a span active

A span must be created by a `TracerProvider` and stored in the context. Two patterns:

**Pattern 1 -- manual span** (business logic, background jobs):

```go
tracer := tp.Tracer("myservice")
ctx, span := tracer.Start(r.Context(), "processOrder")
defer span.End()

// ctx now has an active span; trace_id and span_id appear on every log line.
logger.Context(ctx).Info("processing order", "order_id", id)
// Output: level=INFO msg="processing order" trace_id=4bf92... span_id=00f0... order_id=42
```

**Pattern 2 -- middleware** (HTTP, gRPC, Connect RPC):

For HTTP servers use `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`;
for Connect RPC use `connectrpc.com/otelconnect`. Both wrap every request with a span
and store it in the request context before your handler runs:

```go
// HTTP: otelhttp creates a span per request.
mux.Handle("/api/", otelhttp.NewHandler(apiHandler, "api"))

// Connect RPC: otelconnect creates a span per RPC.
otelInterceptor, _ := otelconnect.NewInterceptor(
    otelconnect.WithTracerProvider(tp),
)
path, handler := myv1connect.NewMyServiceHandler(svc,
    connect.WithInterceptors(otelInterceptor),
)
```

Your handler receives a `ctx` that already has a span -- pass it to `logger.Context(ctx)`
and the IDs appear automatically.

**Pattern 3 -- no span (plain context)**:

If you call `logger.Context(context.Background())`, there is no active span, so no IDs
are injected. This is correct and expected -- background goroutines without a trace
context simply produce log lines without trace correlation.

See `examples/trace/` for a runnable example of all three patterns.

---

## Cross-cutting attributes

Use `SetAttrs` or `NewAttrs` to stamp shared dimensions into a context once at the
request boundary. Every `logger.Context(ctx)` and `metric.RecordContext(ctx, …)` call
downstream picks them up automatically — zero per-call repetition.

This is the right pattern for multi-tenant services where `customer_id`, `environment`,
`service_name`, `product`, and `service_plane` need to appear on every log line and
every metric data point:

```go
// In middleware, once per request (after JWT validation):
ctx = logging.SetAttrs(ctx,
    "customer_id", claims.CustomerID,
    "environment", os.Getenv("ENV"),
    "service_name", "valet",
    "product",      claims.Product,
    "service_plane", "dp",
)

// In library code — no knowledge of the dimensions above:
logger.Context(ctx).Metric(requests).Info("request handled", "route", r.URL.Path)
// → log:    msg="request handled" customer_id=acme environment=prod service_name=valet
//           product=tare service_plane=dp trace_id=... span_id=... route=/v1/completions
// → metric: requests_total{customer_id="acme",environment="prod",service_name="valet",
//                          product="tare",service_plane="dp"} += 1
```

The builder form is cleaner when you have many dimensions:

```go
ctx = logging.NewAttrs(ctx).
    Set("customer_id", claims.CustomerID).
    Set("environment", env).
    Set("service_name", serviceName).
    Into(ctx)
```

Calls accumulate: `SetAttrs` can be called multiple times as more attributes become
known. Static service-level attributes (known at startup) belong on the logger via
`logger.With(...)` instead:

```go
// Once in main — service-level static attributes:
base := logging.New(slog.Default()).With(
    "service_name", "valet",
    "environment", os.Getenv("ENV"),
)

// Per-request — dynamic attributes from JWT claims go into context:
ctx = logging.SetAttrs(r.Context(), "customer_id", claims.CustomerID)
base.Context(ctx).Metric(requests).Info("handled")
```

---

## Runtime log level

Change the active log level without restarting the process. Wire
`HTTPLevelHandler` onto your admin mux once in `main`:

```go
// Wrap your slog handler with LevelHandler to enable runtime changes.
levelHandler := logging.NewLevelHandler(logging.LevelInfo, slog.NewTextHandler(os.Stderr, nil))
sl := slog.New(levelHandler)

adminMux.Handle("/log/level", logging.NewHTTPLevelHandler(sl))
```

```bash
# Read current level
curl http://localhost:9090/log/level
# {"level":"INFO","available_levels":["DEBUG","INFO","NOTICE","WARNING","ERROR","EMERGENCY"]}

# Change to DEBUG at runtime
curl -X PUT http://localhost:9090/log/level -d '{"level":"DEBUG"}'
# {"level":"DEBUG","previous_level":"INFO","message":"log level changed from INFO to DEBUG"}
```

Level names are case-insensitive. Aliases are accepted: `WARN` → WARNING, `ERR`/`FATAL` → ERROR.

You can also change the level programmatically:

```go
logging.SetLevel(logger, logging.LevelDebug)
```

`SetLevel` panics if the logger's handler was not wrapped with `NewLevelHandler` — the
panic is intentional and fires at startup, not in production.

---

## Sinks

| Sink | When to use |
|------|-------------|
| `log.NewOTelSink(mp, name)` | Production: backed by OTel `MeterProvider`, exports to Prometheus or OTLP |
| `log.NewMemSink()` | Tests: in-memory, inspect values with `sink.Snapshot()` |

---

## Testing

### Unit tests

```bash
go test -race ./...
```

Uses `MemSink` without external deps, runs instantly:

```go
sink := log.NewMemSink()
telemetry.SetGlobalMetricSink(sink)
// ...
assert.Equal(t, float64(1), sink.Snapshot()["app_requests_total"])
```

### E2e tests

Uses an in-process OTLP gRPC sink: no Docker, no sleep, precise assertions on
exact values and labels:

```bash
cd e2e && go test -v -tags e2e -timeout 60s ./...
```

Assertions look like:

```go
// Exact counter value + label
val, ok := sink.WaitForCounter("app_requests_total", "route", "/api/v1/users", 1, 5*time.Second)

// Log body + trace correlation
rec, ok := sink.WaitForLog("request handled", 5*time.Second)
assert.Equal(t, traceID, rec.Attrs["trace_id"])

// Span by trace ID
span, ok := sink.WaitForSpan(traceID, 5*time.Second)
assert.Equal(t, "GET /api/v1/users", span.Name)
```

### Manual verification with otel-front

For visual browsing of the full telemetry picture, run with
[otel-front](https://github.com/mesaglio/otel-front):

```bash
# Terminal 1: start otel-front
docker run --rm -p 8000:8000 -p 4317:4317 -p 4318:4318 \
    ghcr.io/mesaglio/otel-front:latest

# Terminal 2: run e2e routing to otel-front
cd e2e && E2E_OTEL_FRONT=1 go test -v -tags e2e -timeout 90s ./...

# Open http://localhost:8000
```

You will see the same `trace_id` linking the log record, the metric data point,
and the span. The three signals are correlated in one view.

---

## Discussion

### Why `logger.Context(ctx)` instead of passing ctx to every log call

Most logging libraries take `ctx` as the first argument to every call:
`logger.InfoContext(ctx, "msg", ...)`. This library takes a different approach:
attach the context once with `.Context(ctx)`, get a new logger, and use it for
all calls in that scope.

The difference matters for library code. A component declares:

```go
var log = scope.Register("payments", "payment processing")
```

and calls `log.Info(...)` throughout. When the caller wants trace correlation and
per-request dimensions, it does:

```go
reqLog := log.Context(ctx)
reqLog.Info("charging card", "amount", amount)
reqLog.Error("charge failed", err)
```

Both calls carry the same `trace_id`, `span_id`, and any `SetAttrs` dimensions
without the library knowing where they come from. The library stays clean; the
caller controls what context flows in.

### Why metrics fire before the level check

```go
func (l *slogLogger) Info(msg string, kvs ...any) {
    if l.metric != nil {
        l.metric.RecordContext(l.ctx, 1) // always, regardless of level
    }
    if l.level != telemetry.LevelNone && l.level < telemetry.LevelInfo {
        return // log silenced, metric already recorded
    }
    l.sl.Info(msg, l.args(kvs)...)
}
```

In production you commonly silence Info logs (`level=ERROR`) to cut noise and cost.
If the metric fired after the level check, silencing logs would also silence your
alerting signal. The ordering is a deliberate invariant: changing it would silently
break every alert that relies on the metric.

See [RATIONALE.md](RATIONALE.md) for the full reasoning.

### When to use `With` vs `SetAttrs`

`logger.With(kvs...)` is for static dimensions known at construction time:
`service_name`, `environment`, `version`. They never change and the resulting logger
can be stored and reused by concurrent goroutines safely.

`SetAttrs(ctx, kvs...)` is for dynamic per-request dimensions: `customer_id`,
`user_id`, `product`. These differ per request and cannot live on a shared logger
without a data race. They live in the context, which is already per-request.

Both reach both logs and metrics. `logger.Context(ctx)` merges them in order:
static `With` dims first, then context dims, then trace IDs.

```go
// Once at startup:
base := logging.New(sl).With("service", "valet", "env", "prod")

// Per request:
ctx = logging.SetAttrs(r.Context(), "customer_id", claims.CustomerID)
base.Context(ctx).Metric(requests).Info("handled")
// Output: service=valet env=prod customer_id=acme trace_id=... span_id=...
```

### The six severity levels

`log/slog` defines four levels: DEBUG(-4), INFO(0), WARN(4), ERROR(8). This library
adds NOTICE(2) and EMERGENCY(12) to match Google Cloud Logging's severity enum.
NOTICE fills the gap between INFO and WARNING for significant-but-normal events
(config reload, leadership change). EMERGENCY signals the service is unusable.

Both are available as `logging.LevelNotice` and `logging.LevelEmergency`. The `gcp/`
subpackage maps all six to the Cloud Logging severity strings that GKE and Cloud Run
expect.

---

## License

Apache 2.0. See [LICENSE](LICENSE).

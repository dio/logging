# log/gcp

[![Go Reference](https://pkg.go.dev/badge/github.com/dio/logging/gcp.svg)](https://pkg.go.dev/github.com/dio/logging/gcp)

A `slog.Handler` that formats log records for [Google Cloud Logging](https://cloud.google.com/logging/docs/structured-logging).

Part of [github.com/dio/logging](https://github.com/dio/logging). Same `go.mod`, no new dependencies.

---

## The problem

Cloud Run and GKE auto-ingest JSON written to stdout/stderr into Cloud Logging.
But Cloud Logging only promotes fields to first-class status when the keys match
its expected names. The default `slog.JSONHandler` uses the wrong ones:

```
slog default    Cloud Logging expects
─────────────────────────────────────
level        →  severity
msg          →  message
source       →  logging.googleapis.com/sourceLocation
```

Beyond key names, `slog.LevelWarn` must map to `"WARNING"` (not `"WARN"`), and
the OTel `trace_id` / `span_id` fields injected by `github.com/dio/logging` must be
reformatted into the `logging.googleapis.com/trace` and `logging.googleapis.com/spanId`
fields that Cloud Logging uses to link log entries to Cloud Trace spans.

This package fixes all of that in one call.

---

## Install

```bash
go get github.com/dio/logging/gcp
```

---

## Usage

```go
import (
    "log/slog"
    "os"

    log "github.com/dio/logging"
    "github.com/dio/logging/gcp"
    "github.com/tetratelabs/telemetry/scope"
)

scope.UseLogger(log.New(slog.New(gcp.NewHandler(os.Stderr, "my-project", nil))))
```

That is the only change needed. Everything else stays the same.

### Zero-config project detection

Pass an empty string for `projectID` and the handler resolves it from the environment:

```go
gcp.NewHandler(os.Stderr, "", nil) // auto-detect
```

Resolution order:

1. Explicit string passed to `NewHandler` (or `ReplaceAttr`)
2. `GOOGLE_CLOUD_PROJECT` env (App Engine, Cloud Functions, gcloud CLI)
3. `GCLOUD_PROJECT` env (legacy)
4. GCP metadata server via [`cloud.google.com/go/compute/metadata`](https://pkg.go.dev/cloud.google.com/go/compute/metadata). Cloud Run, GKE, GCE, App Engine flex. 200ms timeout. Cached for the process lifetime (success and failure both cached, so dev laptops do not pay the timeout repeatedly).

On Cloud Run you can drop the explicit project entirely; `GOOGLE_CLOUD_PROJECT` is set automatically. On GKE the metadata server provides it. On non-GCP environments the chain returns empty and the trace correlation rewrite is skipped (logs still emit valid Cloud Logging JSON, they just do not link to Cloud Trace).

Use `ResolveProjectID("")` directly when you want the resolved value for other purposes:

```go
project := gcp.ResolveProjectID("") // empty on non-GCP
```

---

## What it remaps

| slog field | Cloud Logging field | Notes |
|---|---|---|
| `level` | `severity` | `WARN` becomes `WARNING` |
| `msg` | `message` | |
| `source` | `logging.googleapis.com/sourceLocation` | only when `AddSource: true` |
| `trace_id` | `logging.googleapis.com/trace` | prefixed with `projects/<projectID>/traces/` |
| `span_id` | `logging.googleapis.com/spanId` | |

The `trace_id` and `span_id` fields are injected automatically by `log.New` whenever
an active OTel span is present in the context. No extra code needed in handlers.

---

## Log entry Cloud Logging receives

```json
{
  "severity": "INFO",
  "message": "request handled",
  "scope": "server",
  "route": "/api/v1/users",
  "logging.googleapis.com/trace": "projects/my-project/traces/4bf92f3577b34da6a3ce929d0e0e4736",
  "logging.googleapis.com/spanId": "00f067aa0ba902b7"
}
```

Cloud Logging promotes `severity` and `message` as first-class fields. The trace
field links the entry to the Cloud Trace span. Clicking the trace icon in Logs
Explorer opens Cloud Trace at the exact span.

---

## Structured fields

Beyond severity and trace correlation, the package ships helpers and wrapping
handlers for the four other Cloud Logging structured fields that unlock UI
features.

### `httpRequest`

Adds Method, URL, Status, Latency columns to the Cloud Logging UI plus filter
chips. Emit it on the log line that summarizes an HTTP request:

```go
logger.LogAttrs(ctx, slog.LevelInfo, "request handled",
    gcp.HTTPRequest("GET", "/api/echo", 200, 45*time.Millisecond),
)
```

For more fields (userAgent, remoteIp, referer, protocol, request/response
size), use `gcp.HTTPRequestFull`. Empty strings and zero sizes are omitted.

### `logging.googleapis.com/operation`

Groups all log lines from a single request under one expandable entry. Wrap
the handler with `NewOperationHandler` to auto-inject `{id, producer}` from
context, and emit explicit bookends:

```go
base := gcp.NewHandler(os.Stderr, "", nil)
h := gcp.NewOperationHandler(base)
logger := slog.New(h)

// In middleware:
ctx = gcp.WithOperation(ctx, gcp.Operation{ID: reqID, Producer: "auth"})
logger.LogAttrs(ctx, slog.LevelInfo, "request received",
    gcp.OperationStart(reqID, "auth"))

// In handlers downstream: operation injected automatically.
logger.InfoContext(ctx, "cache miss")

// At the end:
logger.LogAttrs(ctx, slog.LevelInfo, "request handled",
    gcp.OperationEnd(reqID, "auth"))
```

Context-derived operations carry `id` and `producer` only. The `first` and
`last` flags must be set explicitly via `OperationStart` / `OperationEnd`
so the Cloud Logging UI groups the request correctly.

### `logging.googleapis.com/labels`

Moves bounded attr keys from `jsonPayload` to indexed labels for fast
filtering. Wrap the handler with `NewLabelsHandler` and pass the keys you
want hoisted:

```go
base := gcp.NewHandler(os.Stderr, "", nil)
h := gcp.NewLabelsHandler(base, "customer", "environment", "region")
logger := slog.New(h)

logger.Info("hit",
    slog.String("customer", "acme"),
    slog.String("environment", "prod"),
    slog.Int("rows", 42),
)
// rows stays in jsonPayload; customer + environment land under
// logging.googleapis.com/labels.
```

Cap is 64 labels per entry (GCP limit). Overflow keys stay in
`jsonPayload` and a one-time stderr warning fires so you know to trim
`labelKeys`. NEVER add unbounded keys (request_id, user_id); Cloud
Logging indexes labels and bills per series regardless of cardinality.

### `logging.googleapis.com/sourceLocation`

Emits file, line, and function for the call site. Cloud Error Reporting
uses this for grouping. Enable via the standard `slog` option:

```go
gcp.NewHandler(os.Stderr, "", &slog.HandlerOptions{AddSource: true})
```

Parsing the stack on every log has measurable cost (`runtime.Caller`).
Keep `AddSource: true` for services where Cloud Error Reporting matters;
omit it for hot-path libraries.

### Composing the wrappers

The handlers compose. Innermost runs first:

```go
base := gcp.NewHandler(os.Stderr, "", &slog.HandlerOptions{AddSource: true})
h := gcp.NewLabelsHandler(
    gcp.NewOperationHandler(base),
    "customer", "environment",
)
logger := slog.New(h)
```

Order matters less than you'd think: `NewOperationHandler` only adds an
attr if one isn't already present, and `NewLabelsHandler` only hoists
attrs whose keys you nominated. Putting labels outermost is the
recommended order because it sees the final attr set including
operation.

---

## Composing with existing HandlerOptions

If you already have a `slog.HandlerOptions` (for example with `AddSource` or a
custom `ReplaceAttr`), pass it in. The GCP remapping chains before your existing
`ReplaceAttr`:

```go
opts := &slog.HandlerOptions{
    AddSource: true,
    ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
        if a.Key == "password" {
            a.Value = slog.StringValue("[redacted]")
        }
        return a
    },
}
h := gcp.NewHandler(os.Stderr, "my-project", opts)
```

Or use `gcp.ReplaceAttr` directly if you want just the function:

```go
opts := &slog.HandlerOptions{
    ReplaceAttr: gcp.ReplaceAttr("my-project"),
}
slog.New(slog.NewJSONHandler(os.Stderr, opts))
```

---

## Severity mapping

Cloud Logging defines more severity levels than slog's four. This package exposes
them as `slog.Level` constants that fit into slog's numeric space:

| Constant | Value | Cloud Logging severity | When to use |
|---|---|---|---|
| `gcp.LevelDebug` | -4 | `DEBUG` | Detailed diagnostic info |
| `gcp.LevelInfo` | 0 | `INFO` | Normal operation |
| `gcp.LevelNotice` | 2 | `NOTICE` | Normal but significant events |
| `gcp.LevelWarning` | 4 | `WARNING` | Might cause problems |
| `gcp.LevelError` | 8 | `ERROR` | Likely to cause problems |
| `gcp.LevelEmergency` | 12 | `EMERGENCY` | System is unusable |

`gcp.LevelWarning` and `gcp.LevelNotice` sit between slog's `LevelInfo` (0) and
`LevelWarn` (4), so the standard slog `LevelWarn` also maps to `WARNING`. Use the
`gcp.Level*` constants when you need `NOTICE` or `EMERGENCY`:

```go
logger.Log(ctx, gcp.LevelNotice, "quota threshold reached", "used_pct", 80)
logger.Log(ctx, gcp.LevelEmergency, "data loss detected", "table", "users")
```

---

## No new dependencies

This package uses only `log/slog` from the standard library. The `otel/trace`
package is already a dependency of `github.com/dio/logging`. Nothing new is added.

---

## License

Apache 2.0. See [LICENSE](../LICENSE).

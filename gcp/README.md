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

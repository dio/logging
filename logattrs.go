package logging

import "context"

// Log-only attrs are key-value pairs that flow into log records but NEVER
// into metric labels. Use them for unbounded values like request_id,
// user_id, IP addresses, or anything else where the value space is huge.
//
// Why two scopes:
//
// OpenTelemetry counters allocate a new time series for every unique
// combination of label values. A service at 1000 RPS for one hour with
// request_id on its counters produces ~3.6 million series. Cloud
// Monitoring, Prometheus, and Mimir all either bill per series or
// reject past a hard cap. The counter becomes useless and your
// metrics backend bills you for the storage.
//
// SetAttrs (in attrs.go) keeps its existing semantics: bounded attrs
// that DO flow into metric labels. SetLogAttrs is the new scope for
// unbounded fields that stay on log records only.
//
// Recommended pattern:
//
//	// Bounded, fine on metric labels.
//	ctx = logging.SetAttrs(ctx,
//	    "customer_id", claims.CustomerID,
//	    "environment", env,
//	    "service_name", "valet",
//	)
//
//	// Unbounded, logs only.
//	ctx = logging.SetLogAttrs(ctx,
//	    "request_id", reqID,
//	    "user_id", claims.UserID,
//	)
//
//	logger.Context(ctx).Metric(requests).Info("hit")
//	// log:    msg=hit customer_id=acme environment=prod service_name=valet
//	//         request_id=abc123 user_id=u-42 trace_id=...
//	// metric: requests_total{customer_id="acme",environment="prod",
//	//         service_name="valet"} += 1   // no request_id, no user_id

// logAttrsKey is the context key used to store log-only key-value pairs.
// We deliberately do NOT use telemetry.KeyValuesToContext (which is what
// SetAttrs uses) so the metric sinks cannot accidentally see these values.
type logAttrsKey struct{}

// SetLogAttrs attaches key-value pairs to ctx that flow into log records
// only. The pairs are NEVER attached to counters, histograms, or gauges.
//
// Multiple calls append; later calls do not replace earlier ones.
//
//	ctx = logging.SetLogAttrs(ctx,
//	    "request_id", reqID,
//	    "user_id", userID,
//	)
//
// keyValuePairs must contain an even number of strings. Odd entries are
// silently dropped to mirror SetAttrs' tolerant behavior.
func SetLogAttrs(ctx context.Context, keyValuePairs ...string) context.Context {
	if len(keyValuePairs) == 0 {
		return ctx
	}
	// Drop trailing odd entry, if any.
	if len(keyValuePairs)%2 != 0 {
		keyValuePairs = keyValuePairs[:len(keyValuePairs)-1]
	}
	existing, _ := ctx.Value(logAttrsKey{}).([]string)
	merged := make([]string, 0, len(existing)+len(keyValuePairs))
	merged = append(merged, existing...)
	merged = append(merged, keyValuePairs...)
	return context.WithValue(ctx, logAttrsKey{}, merged)
}

// GetLogAttrs returns the log-only key-value pairs stored in ctx by
// SetLogAttrs. Returns nil if none are present. The slice is in the
// interleaved key, value, key, value order used by SetLogAttrs.
//
// Each entry is a string; the result is exposed as []any to match the
// shape used by GetAttrs so callers can pass it to slog.Attr-style
// helpers without an extra conversion.
func GetLogAttrs(ctx context.Context) []any {
	kvs, ok := ctx.Value(logAttrsKey{}).([]string)
	if !ok || len(kvs) == 0 {
		return nil
	}
	out := make([]any, len(kvs))
	for i, v := range kvs {
		out[i] = v
	}
	return out
}

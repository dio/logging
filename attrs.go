package logging

import (
	"context"

	"github.com/tetratelabs/telemetry"
)

// Attrs holds a set of key-value pairs that flow through context into both
// log lines and metric attributes automatically.
//
// Attrs is the BOUNDED scope: use it for low-cardinality enums (customer
// ID, environment, service name, region) that are safe as metric labels.
// For UNBOUNDED values like request_id, user_id, raw IP, or anything else
// with a huge value space, use SetLogAttrs (in logattrs.go) instead. The
// log-only scope lets request_id decorate log records without exploding
// counter cardinality.
//
// Wire once at the edge of your service (middleware, gRPC interceptor,
// handler constructor) and every logger.Context(ctx) call downstream
// carries the same dimensions without any per-call boilerplate:
//
//	// Bounded: safe on metric labels.
//	ctx = logging.NewAttrs(ctx).
//	    Set("customer_id", claims.CustomerID).
//	    Set("environment", env).
//	    Set("service_name", "valet").
//	    Set("product", claims.Product).
//	    Into(ctx)
//
//	// Unbounded: logs only, never metric labels.
//	ctx = logging.SetLogAttrs(ctx,
//	    "request_id", reqID,
//	    "user_id", claims.UserID,
//	)
//
//	// In library code, no knowledge of the dimensions above:
//	logger.Context(ctx).Metric(requests).Info("request handled")
//	// log:    msg="request handled" customer_id=acme environment=prod
//	//         service_name=valet product=tare request_id=abc user_id=u-42
//	//         trace_id=...
//	// metric: requests_total{customer_id="acme",environment="prod",
//	//         service_name="valet",product="tare"} += 1
//	//         (no request_id or user_id on the counter)
type Attrs struct {
	ctx  context.Context
	kvps []any
}

// NewAttrs starts building an Attrs set rooted at ctx.
// Call Set for each dimension, then Into to stamp them into a new context.
func NewAttrs(ctx context.Context) *Attrs {
	return &Attrs{ctx: ctx}
}

// Set adds a key-value pair. Both key and value must be strings.
// Returns the receiver for chaining.
func (a *Attrs) Set(key, value string) *Attrs {
	a.kvps = append(a.kvps, key, value)
	return a
}

// Into stamps all accumulated key-value pairs into the context and returns it.
// The returned context can be passed to logger.Context(ctx) or
// metric.RecordContext(ctx, ...) directly; both pick up the pairs automatically.
//
// Into uses telemetry.KeyValuesToContext, which appends to any pairs already
// present in the context, so it is safe to call multiple times as request
// attributes are resolved (e.g. once for static service attrs, once for
// per-request attrs from JWT claims).
func (a *Attrs) Into(ctx context.Context) context.Context {
	if len(a.kvps) == 0 {
		return ctx
	}
	return telemetry.KeyValuesToContext(ctx, a.kvps...)
}

// SetAttrs is a convenience one-liner for when you have all key-value pairs
// ready. It is equivalent to NewAttrs(ctx).Set(k1, v1).Set(k2, v2)...Into(ctx).
//
//	ctx = logging.SetAttrs(ctx,
//	    "customer_id", claims.CustomerID,
//	    "environment", env,
//	    "service_name", "valet",
//	)
func SetAttrs(ctx context.Context, keyValuePairs ...string) context.Context {
	if len(keyValuePairs) == 0 {
		return ctx
	}
	kvps := make([]any, len(keyValuePairs))
	for i, s := range keyValuePairs {
		kvps[i] = s
	}
	return telemetry.KeyValuesToContext(ctx, kvps...)
}

// GetAttrs returns the key-value pairs stored in ctx by SetAttrs or NewAttrs.
// Returns nil if none are present. The slice is in the same interleaved
// key, value, key, value order used by SetAttrs.
func GetAttrs(ctx context.Context) []any {
	return telemetry.KeyValuesFromContext(ctx)
}

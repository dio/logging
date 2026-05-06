// Command trace demonstrates how an active OTel span attached via
// logger.Context(ctx) causes trace_id and span_id to appear automatically
// in every log line -- no manual extraction required.
//
// Three patterns are shown:
//
//  1. Manual span: call tracer.Start(ctx, "op") and pass the returned ctx.
//  2. HTTP middleware: use go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
//     to have every HTTP handler receive a ctx that already has a span.
//  3. No span: logger.Context(ctx) on a plain background context -- no IDs injected.
//
// Run:
//
//	go run .
//
// Expected output (IDs will differ):
//
//	# pattern 1 -- manual span
//	level=INFO msg="processing order" trace_id=4bf92f3577b34da6a3ce929d0e0e4736 span_id=00f067aa0ba902b7 order_id=42
//	level=ERROR msg="payment failed" trace_id=4bf92f3577b34da6a3ce929d0e0e4736 span_id=00f067aa0ba902b7 err=card declined
//
//	# pattern 2 -- via HTTP middleware (curl http://localhost:8080/)
//	level=INFO msg="handling request" trace_id=... span_id=... method=GET path=/
//
//	# pattern 3 -- no span
//	level=INFO msg="startup complete"   (no trace_id, no span_id)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/tetratelabs/telemetry"
	"github.com/tetratelabs/telemetry/scope"

	logging "github.com/dio/logging"
)

// --- metrics declared in library code (no impl dep) ---

var orderErrors telemetry.Metric

func init() {
	telemetry.ToGlobalMetricSink(func(ms telemetry.MetricSink) {
		orderErrors = ms.NewSum("app_order_errors_total", "Order processing errors")
	})
}

var log = scope.Register("orders", "order processing")

// --- main ---

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Build slog logger wrapped in dio/logging.
	sl := slog.New(logging.NewLevelHandler(logging.LevelDebug, slog.NewTextHandler(os.Stderr, nil)))

	// 2. Wire telemetry -- MemSink is enough for this example (no real OTel export).
	sink := logging.NewMemSink()
	telemetry.SetGlobalMetricSink(sink)
	scope.UseLogger(logging.New(sl))

	// 3. Build a TracerProvider. In production this would export to OTLP / Cloud Trace.
	//    Here we use a no-op exporter so spans are created but not sent anywhere.
	res, _ := resource.New(ctx, resource.WithAttributes(semconv.ServiceName("trace-example")))
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
	defer tp.Shutdown(ctx)
	otel.SetTracerProvider(tp)

	tracer := tp.Tracer("trace-example")

	// --- Pattern 3: no span ---
	// logger.Context on a plain context: trace_id and span_id are absent.
	log.Context(ctx).Info("startup complete")
	// Output: level=INFO msg="startup complete"   (no trace_id, no span_id)

	// --- Pattern 1: manual span ---
	// Create a span explicitly; pass the returned context to logger.Context.
	// slogLogger.Context() calls trace.SpanFromContext(ctx).SpanContext().IsValid()
	// which is true here, so trace_id and span_id are appended to kvs.
	processOrder(ctx, tracer, 42)

	// --- Pattern 2: HTTP middleware ---
	// otelhttp.NewHandler wraps every request with a span and stores it in the
	// request context. The handler receives that ctx and passes it to logger.Context.
	mux := http.NewServeMux()
	mux.Handle("/", otelhttp.NewHandler(http.HandlerFunc(handleRoot), "handleRoot"))

	srv := &http.Server{Addr: ":8080", Handler: mux}
	go srv.ListenAndServe()
	sl.Info("HTTP server listening", "addr", ":8080")
	sl.Info("curl http://localhost:8080/ to see trace_id in log output")

	<-ctx.Done()
	srv.Shutdown(context.Background())
}

// processOrder demonstrates pattern 1: a span created manually in business logic.
func processOrder(ctx context.Context, tracer trace.Tracer, orderID int) {
	// Start a span. The returned ctx has an active SpanContext.
	ctx, span := tracer.Start(ctx, "processOrder")
	defer span.End()

	// Pass ctx to logger.Context -- trace_id and span_id injected automatically.
	log.Context(ctx).Info("processing order", "order_id", orderID)
	// Output: level=INFO msg="processing order"
	//         trace_id=4bf92f35... span_id=00f067aa... order_id=42

	// Simulate a failure. The same trace_id and span_id appear on the error line,
	// linking it to the span in your trace backend.
	err := errors.New("card declined")
	log.Context(ctx).
		Metric(orderErrors).
		Error("payment failed", err)
	// Output: level=ERROR msg="payment failed"
	//         trace_id=4bf92f35... span_id=00f067aa... err=card declined
	// Metric: app_order_errors_total += 1 (fires even if log level were ERROR-only)
}

// handleRoot demonstrates pattern 2: span injected by HTTP middleware.
func handleRoot(w http.ResponseWriter, r *http.Request) {
	// r.Context() already has an active span because otelhttp.NewHandler
	// called tracer.Start before invoking this handler.
	log.Context(r.Context()).Info("handling request",
		"method", r.Method,
		"path", r.URL.Path,
	)
	// Output: level=INFO msg="handling request"
	//         trace_id=... span_id=... method=GET path=/
	w.WriteHeader(http.StatusOK)
}

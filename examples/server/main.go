// Command server demonstrates github.com/dio/logging in a minimal HTTP server.
//
// Every request handler calls log.Metric(...).Info/Error, one call that emits
// both a structured log line and an OTel counter. When an OTel span is active,
// trace_id and span_id are automatically injected into the log line.
//
// The admin server exposes a runtime log-level endpoint via log.HTTPLevelHandler.
//
// # Run locally (Prometheus + local spans only)
//
//	go run .
//
//	curl http://localhost:8080/hello
//	curl http://localhost:8080/fail
//	curl http://localhost:9090/metrics           # Prometheus scrape
//	curl http://localhost:9090/log/level         # GET current level
//	curl -XPUT http://localhost:9090/log/level -d '{"level":"DEBUG"}'
//
// # Run against otel-front (all three signals: metrics, traces, logs)
//
//	# Terminal 1: start otel-front
//	docker run --rm -p 8000:8000 -p 4317:4317 -p 4318:4318 \
//	    ghcr.io/mesaglio/otel-front:latest
//
//	# Terminal 2: run the server
//	OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 go run .
//
//	# Terminal 3: generate traffic
//	while true; do
//	    curl -s http://localhost:8080/hello > /dev/null
//	    curl -s http://localhost:8080/fail  > /dev/null
//	    sleep 1
//	done
//
//	# Open http://localhost:8000
//	# Metrics tab: app_requests_total, app_errors_total
//	# Traces tab:  handleHello, handleFail spans
//	# Logs tab:    log lines with trace_id linking to the spans above
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is set, all three signals flow to that
// endpoint via gRPC. The Prometheus /metrics endpoint is always available.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/dio/logging"
	"github.com/tetratelabs/telemetry"
	"github.com/tetratelabs/telemetry/scope"

	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	otlploggrpc "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otlptracegrpc "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---------------------------------------------------------------------------
// Metrics declared in library code -- no impl dependency.
// ---------------------------------------------------------------------------

var (
	routeLabel telemetry.Label
	requests   telemetry.Metric
	errors_    telemetry.Metric
)

func init() {
	telemetry.ToGlobalMetricSink(func(ms telemetry.MetricSink) {
		routeLabel = ms.NewLabel("route")
		requests   = ms.NewSum("app_requests_total", "Total requests handled")
		errors_    = ms.NewSum("app_errors_total",   "Total request errors")
	})
}

var logger = scope.Register("server", "HTTP server")

var tracer trace.Tracer

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleHello(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handleHello")
	defer span.End()

	logger.Context(ctx).
		Metric(requests.With(routeLabel.Upsert(r.URL.Path))).
		Info("request handled", "method", r.Method, "path", r.URL.Path)

	fmt.Fprintln(w, "hello")
}

func handleFail(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handleFail")
	defer span.End()

	err := errors.New("something went wrong")

	logger.Context(ctx).
		Metric(errors_.With(routeLabel.Upsert(r.URL.Path))).
		Error("request failed", err, "method", r.Method, "path", r.URL.Path)

	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	// Resource: identifies this service in every signal.
	res, _ := resource.New(ctx, resource.WithAttributes(semconv.ServiceName("example-server")))

	// --- Metrics ---
	// Always: Prometheus pull at /metrics.
	// When OTLP endpoint is set: also push to collector.
	var metricReaders []metric.Option
	promExp, err := otelprom.New()
	if err != nil {
		slog.Error("prometheus exporter", "err", err)
		os.Exit(1)
	}
	metricReaders = append(metricReaders, metric.WithReader(promExp))

	if otlpEndpoint != "" {
		metricExp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(otlpEndpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			slog.Error("otlp metric exporter", "err", err)
			os.Exit(1)
		}
		metricReaders = append(metricReaders, metric.WithReader(
			metric.NewPeriodicReader(metricExp, metric.WithInterval(2*time.Second)),
		))
	}

	mpOpts := append([]metric.Option{metric.WithResource(res)}, metricReaders...)
	mp := metric.NewMeterProvider(mpOpts...)
	defer mp.Shutdown(ctx)

	// --- Traces ---
	// Always: a TracerProvider so trace_id/span_id appear in log lines.
	// When OTLP endpoint is set: export spans to collector.
	var tpOpts []sdktrace.TracerProviderOption
	tpOpts = append(tpOpts, sdktrace.WithResource(res))

	if otlpEndpoint != "" {
		traceExp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(otlpEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			slog.Error("otlp trace exporter", "err", err)
			os.Exit(1)
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(traceExp))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	defer tp.Shutdown(ctx)
	tracer = tp.Tracer("example-server")

	// --- Logs ---
	// Always: slog text handler to stderr.
	// When OTLP endpoint is set: also bridge slog → OTel log SDK → collector.
	// This makes log lines appear in otel-front's Logs tab, with trace_id linking
	// each log record to the span that produced it.
	levelHandler := log.NewLevelHandler(log.LevelInfo,
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}),
	)
	var slogHandler slog.Handler = levelHandler

	if otlpEndpoint != "" {
		logExp, err := otlploggrpc.New(ctx,
			otlploggrpc.WithEndpoint(otlpEndpoint),
			otlploggrpc.WithInsecure(),
		)
		if err != nil {
			slog.Error("otlp log exporter", "err", err)
			os.Exit(1)
		}
		lp := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
			sdklog.WithResource(res),
		)
		defer lp.Shutdown(ctx)

		// Fan-out: write to stderr AND forward to the OTel log SDK.
		slogHandler = &fanOutHandler{
			stderr: levelHandler,
			otel:   &otelLogBridge{provider: lp},
		}

		slog.Info("OTLP export enabled",
			"endpoint", otlpEndpoint,
			"ui", "http://localhost:8000",
		)
	}

	sl := slog.New(slogHandler)

	// Wire telemetry library.
	sink := log.NewOTelSink(mp, "example")
	defer sink.Shutdown(ctx)
	telemetry.SetGlobalMetricSink(sink)
	scope.UseLogger(log.New(sl))

	// App server.
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", handleHello)
	mux.HandleFunc("/fail",  handleFail)

	appSrv := &http.Server{Addr: ":8080", Handler: mux}

	adminMux := http.NewServeMux()
	adminMux.Handle("/metrics",   promhttp.Handler())
	adminMux.Handle("/log/level", log.NewHTTPLevelHandler(sl))
	adminSrv := &http.Server{Addr: ":9090", Handler: adminMux}

	sl.Info("starting", "app", ":8080", "admin", ":9090")

	go appSrv.ListenAndServe()
	go adminSrv.ListenAndServe()

	<-ctx.Done()

	sl.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = appSrv.Shutdown(shutCtx)
	_ = adminSrv.Shutdown(shutCtx)
}

// ---------------------------------------------------------------------------
// fanOutHandler: writes to stderr and forwards to the OTel log bridge.
// ---------------------------------------------------------------------------

type fanOutHandler struct {
	stderr slog.Handler
	otel   slog.Handler
}

func (h *fanOutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.stderr.Enabled(ctx, level)
}

func (h *fanOutHandler) Handle(ctx context.Context, r slog.Record) error {
	_ = h.stderr.Handle(ctx, r)
	_ = h.otel.Handle(ctx, r)
	return nil
}

func (h *fanOutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &fanOutHandler{
		stderr: h.stderr.WithAttrs(attrs),
		otel:   h.otel.WithAttrs(attrs),
	}
}

func (h *fanOutHandler) WithGroup(name string) slog.Handler {
	return &fanOutHandler{
		stderr: h.stderr.WithGroup(name),
		otel:   h.otel.WithGroup(name),
	}
}

// ---------------------------------------------------------------------------
// otelLogBridge: slog.Handler that emits records to the OTel log SDK.
// This is what makes log lines appear in otel-front's Logs tab, with
// trace_id linking each record to the span that produced it.
// ---------------------------------------------------------------------------

type otelLogBridge struct {
	provider *sdklog.LoggerProvider
}

func (b *otelLogBridge) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (b *otelLogBridge) WithAttrs(_ []slog.Attr) slog.Handler          { return b }
func (b *otelLogBridge) WithGroup(_ string) slog.Handler                { return b }

func (b *otelLogBridge) Handle(ctx context.Context, r slog.Record) error {
	l := b.provider.Logger("example-server")

	var rec otellog.Record
	rec.SetTimestamp(r.Time)
	rec.SetSeverityText(r.Level.String())
	rec.SetBody(otellog.StringValue(r.Message))

	// Carry all slog attributes as OTel log record attributes.
	r.Attrs(func(a slog.Attr) bool {
		rec.AddAttributes(otellog.String(a.Key, fmt.Sprint(a.Value.Any())))
		return true
	})

	// Propagate the active span's trace_id and span_id so otel-front can link
	// log records to their traces. This is the same check that
	// slogLogger.Context() does on the logging side.
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		rec.AddAttributes(
			otellog.String("trace_id", sc.TraceID().String()),
			otellog.String("span_id",  sc.SpanID().String()),
		)
	}

	l.Emit(ctx, rec)
	return nil
}

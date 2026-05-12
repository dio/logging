package logging_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/tetratelabs/telemetry"
)

// ---------------------------------------------------------------------------
// Regression: SetAttrs must propagate into histogram and gauge data points,
// not only into counters.
//
// Before the fix in otelsink.go, otelHistogram.RecordContext and
// otelGauge.RecordContext used m.base.with only and ignored
// contextAttrs(ctx). Counters used m.base.attrs(ctx) which DID merge both
// sources. The split caused a silent bug where business attributes set via
// SetAttrs landed on counters but vanished from histograms / gauges.
//
// This test was added by the tilik spike (~/src/dio/tilik) which discovered
// the bug end-to-end against a real OTel pipeline.
// ---------------------------------------------------------------------------

var (
	regHist     telemetry.Metric
	regGauge    telemetry.Metric
	regCounter2 telemetry.Metric
)

func init() {
	telemetry.ToGlobalMetricSink(func(ms telemetry.MetricSink) {
		regHist = ms.NewDistribution(
			"reg_histogram_seconds",
			"Test histogram for SetAttrs regression",
			[]float64{0.01, 0.1, 1},
		)
		regGauge = ms.NewGauge(
			"reg_gauge",
			"Test gauge for SetAttrs regression",
		)
		regCounter2 = ms.NewSum(
			"reg_counter_total",
			"Test counter for SetAttrs regression baseline",
		)
	})
}

func TestSetAttrsCarriesIntoHistogram(t *testing.T) {
	ctx := telemetry.KeyValuesToContext(context.Background(),
		"customer_id", "acme",
		"environment", "test",
	)

	regHist.RecordContext(ctx, 0.05)

	rm := collect(t)
	assertHistogramHasLabel(t, rm, "reg_histogram_seconds", "customer_id", "acme")
	assertHistogramHasLabel(t, rm, "reg_histogram_seconds", "environment", "test")
}

func TestSetAttrsCarriesIntoGauge(t *testing.T) {
	ctx := telemetry.KeyValuesToContext(context.Background(),
		"customer_id", "bigco",
		"environment", "test",
	)

	regGauge.RecordContext(ctx, 7)

	rm := collect(t)
	assertGaugeHasLabel(t, rm, "reg_gauge", "customer_id", "bigco")
	assertGaugeHasLabel(t, rm, "reg_gauge", "environment", "test")
}

// Baseline: counter behaviour unchanged.
func TestSetAttrsCarriesIntoCounter(t *testing.T) {
	ctx := telemetry.KeyValuesToContext(context.Background(),
		"customer_id", "indigo",
	)

	regCounter2.RecordContext(ctx, 1)

	rm := collect(t)
	assertCounter(t, rm, "reg_counter_total", "customer_id", "indigo", 1)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertHistogramHasLabel(t *testing.T, rm metricdata.ResourceMetrics, name, key, val string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			data, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s: expected Histogram[float64], got %T", name, m.Data)
			}
			for _, dp := range data.DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key(key)); ok && v.AsString() == val {
					return
				}
			}
			t.Errorf("histogram %q has no data point with %s=%q. data points: %d", name, key, val, len(data.DataPoints))
			return
		}
	}
	t.Errorf("histogram %q not found", name)
}

func assertGaugeHasLabel(t *testing.T, rm metricdata.ResourceMetrics, name, key, val string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			// dio/logging maps Gauge → Int64UpDownCounter (Sum[int64]).
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s: expected Sum[int64] (UpDownCounter), got %T", name, m.Data)
			}
			for _, dp := range data.DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key(key)); ok && v.AsString() == val {
					return
				}
			}
			t.Errorf("gauge %q has no data point with %s=%q. data points: %d", name, key, val, len(data.DataPoints))
			return
		}
	}
	t.Errorf("gauge %q not found", name)
}

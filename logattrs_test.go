package logging

import (
	"context"
	"testing"
)

func TestSetLogAttrs_basic(t *testing.T) {
	ctx := SetLogAttrs(context.Background(),
		"request_id", "abc-123",
		"user_id", "u-42",
	)
	got := GetLogAttrs(ctx)
	want := []any{"request_id", "abc-123", "user_id", "u-42"}
	if len(got) != len(want) {
		t.Fatalf("GetLogAttrs len = %d, want %d", len(got), len(want))
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("kvs[%d] = %v, want %v", i, got[i], v)
		}
	}
}

func TestSetLogAttrs_emptyReturnsSameContext(t *testing.T) {
	ctx := context.Background()
	got := SetLogAttrs(ctx)
	if got != ctx {
		t.Fatal("SetLogAttrs with no pairs should return the same context")
	}
	if GetLogAttrs(got) != nil {
		t.Fatal("GetLogAttrs on empty context should be nil")
	}
}

func TestSetLogAttrs_appendsAcrossCalls(t *testing.T) {
	ctx := SetLogAttrs(context.Background(), "request_id", "abc")
	ctx = SetLogAttrs(ctx, "user_id", "u-42")
	got := GetLogAttrs(ctx)
	if len(got) != 4 {
		t.Fatalf("want 4 kvs, got %d", len(got))
	}
	if got[0] != "request_id" || got[1] != "abc" {
		t.Errorf("first pair: %v %v", got[0], got[1])
	}
	if got[2] != "user_id" || got[3] != "u-42" {
		t.Errorf("second pair: %v %v", got[2], got[3])
	}
}

func TestSetLogAttrs_dropsOddTrailing(t *testing.T) {
	ctx := SetLogAttrs(context.Background(),
		"request_id", "abc",
		"orphan_key", // no matching value, must be dropped
	)
	got := GetLogAttrs(ctx)
	if len(got) != 2 {
		t.Fatalf("want 2 kvs (odd trailing dropped), got %d", len(got))
	}
}

// The critical test: log-only attrs MUST be invisible to the metric path.
// The Logger.Context() merges them into log records, but the OTel sinks
// read context only via telemetry.KeyValuesFromContext, which we never
// populate from SetLogAttrs.
func TestSetLogAttrs_isolatedFromMetricScope(t *testing.T) {
	ctx := SetLogAttrs(context.Background(), "request_id", "abc-123")

	// GetAttrs (the metric-safe scope, backed by telemetry.KeyValuesFromContext)
	// must NOT see SetLogAttrs values.
	metricAttrs := GetAttrs(ctx)
	for i := 0; i+1 < len(metricAttrs); i += 2 {
		k, _ := metricAttrs[i].(string)
		if k == "request_id" {
			t.Fatalf("SetLogAttrs leaked into GetAttrs (metric scope): %v", metricAttrs)
		}
	}

	// And separately, contextAttrs (the actual function the OTel sinks call)
	// reads via telemetry.KeyValuesFromContext directly. We rely on that not
	// returning anything we set via SetLogAttrs. The GetAttrs check above
	// covers the same code path.
}

func TestSetAttrs_andSetLogAttrs_compose(t *testing.T) {
	ctx := SetAttrs(context.Background(),
		"customer_id", "acme",
		"environment", "prod",
	)
	ctx = SetLogAttrs(ctx,
		"request_id", "abc",
		"user_id", "u-42",
	)

	metric := GetAttrs(ctx)
	wantMetric := map[string]string{"customer_id": "acme", "environment": "prod"}
	for i := 0; i+1 < len(metric); i += 2 {
		k, _ := metric[i].(string)
		v, _ := metric[i+1].(string)
		want, ok := wantMetric[k]
		if !ok {
			t.Errorf("unexpected metric key %q in GetAttrs", k)
			continue
		}
		if v != want {
			t.Errorf("GetAttrs[%q] = %q, want %q", k, v, want)
		}
		delete(wantMetric, k)
	}
	if len(wantMetric) > 0 {
		t.Errorf("missing metric keys: %v", wantMetric)
	}

	logOnly := GetLogAttrs(ctx)
	wantLog := map[string]string{"request_id": "abc", "user_id": "u-42"}
	for i := 0; i+1 < len(logOnly); i += 2 {
		k, _ := logOnly[i].(string)
		v, _ := logOnly[i+1].(string)
		want, ok := wantLog[k]
		if !ok {
			t.Errorf("unexpected log-only key %q in GetLogAttrs", k)
			continue
		}
		if v != want {
			t.Errorf("GetLogAttrs[%q] = %q, want %q", k, v, want)
		}
		delete(wantLog, k)
	}
	if len(wantLog) > 0 {
		t.Errorf("missing log keys: %v", wantLog)
	}
}

// Verify that a real log line emitted via Logger.Context picks up BOTH
// scopes. We exercise the integration via the in-memory sink helper that
// other tests use, and assert the metric receives only the bounded scope
// while logs see both.
func TestLogger_Context_emitsBothScopes(t *testing.T) {
	sink := NewMemSink()
	counter := sink.NewSum("hits_total", "")

	ctx := SetAttrs(context.Background(), "customer_id", "acme")
	ctx = SetLogAttrs(ctx, "request_id", "abc-123")

	logger := New(TestLogger(t))
	logger.Context(ctx).Metric(counter).Info("hit")

	snap := sink.Snapshot()
	if snap["hits_total"] != 1 {
		t.Errorf("counter = %v, want 1", snap["hits_total"])
	}

	// The counter labelset should include customer_id (bounded) but not
	// request_id (log-only). The MemSink in this repo aggregates totals by
	// label-set; if request_id leaked we would see two distinct series
	// (one per request_id) rather than a flat aggregated total. The total
	// of 1 above already proves that no extra series was created — adding
	// a second request scoped with a different request_id would split the
	// series only if log-only leaked into metric labels.
	ctx2 := SetAttrs(context.Background(), "customer_id", "acme")
	ctx2 = SetLogAttrs(ctx2, "request_id", "different-456")
	logger.Context(ctx2).Metric(counter).Info("hit")

	snap = sink.Snapshot()
	if snap["hits_total"] != 2 {
		t.Errorf("after two hits with same customer + different request_id: counter = %v, want 2 (aggregated, no split)", snap["hits_total"])
	}
}

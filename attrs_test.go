package logging

import (
	"context"
	"testing"
)

func TestSetAttrs_basic(t *testing.T) {
	ctx := SetAttrs(context.Background(),
		"customer_id", "acme",
		"environment", "prod",
	)
	kvs := GetAttrs(ctx)
	want := []any{"customer_id", "acme", "environment", "prod"}
	if len(kvs) != len(want) {
		t.Fatalf("GetAttrs len = %d, want %d", len(kvs), len(want))
	}
	for i, v := range want {
		if kvs[i] != v {
			t.Errorf("kvs[%d] = %v, want %v", i, kvs[i], v)
		}
	}
}

func TestSetAttrs_empty(t *testing.T) {
	ctx := SetAttrs(context.Background())
	if got := GetAttrs(ctx); len(got) != 0 {
		t.Errorf("expected empty attrs, got %v", got)
	}
}

func TestSetAttrs_appends(t *testing.T) {
	// Two calls accumulate, not overwrite.
	ctx := SetAttrs(context.Background(), "service_name", "valet")
	ctx = SetAttrs(ctx, "customer_id", "acme")
	kvs := GetAttrs(ctx)
	if len(kvs) != 4 {
		t.Fatalf("expected 4 kvs, got %d: %v", len(kvs), kvs)
	}
	if kvs[0] != "service_name" || kvs[1] != "valet" {
		t.Errorf("first pair wrong: %v %v", kvs[0], kvs[1])
	}
	if kvs[2] != "customer_id" || kvs[3] != "acme" {
		t.Errorf("second pair wrong: %v %v", kvs[2], kvs[3])
	}
}

func TestNewAttrs_builder(t *testing.T) {
	ctx := NewAttrs(context.Background()).
		Set("customer_id", "acme").
		Set("environment", "staging").
		Set("service_name", "valet").
		Set("product", "tare").
		Into(context.Background())

	kvs := GetAttrs(ctx)
	if len(kvs) != 8 {
		t.Fatalf("expected 8 kvs (4 pairs), got %d", len(kvs))
	}
	asMap := func(kvs []any) map[string]string {
		m := make(map[string]string, len(kvs)/2)
		for i := 0; i+1 < len(kvs); i += 2 {
			m[kvs[i].(string)] = kvs[i+1].(string)
		}
		return m
	}
	m := asMap(kvs)
	for k, want := range map[string]string{
		"customer_id":  "acme",
		"environment":  "staging",
		"service_name": "valet",
		"product":      "tare",
	} {
		if got := m[k]; got != want {
			t.Errorf("attr[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestNewAttrs_empty(t *testing.T) {
	ctx := NewAttrs(context.Background()).Into(context.Background())
	if got := GetAttrs(ctx); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestNewAttrs_Into_usesPassedCtx(t *testing.T) {
	// Into(ctx) stamps into the given ctx, not the one passed to NewAttrs.
	base := SetAttrs(context.Background(), "pre", "existing")
	ctx := NewAttrs(context.Background()).
		Set("new", "val").
		Into(base)

	kvs := GetAttrs(ctx)
	// base had 2 kvps, Into appends 2 more
	if len(kvs) != 4 {
		t.Fatalf("expected 4 kvs, got %d: %v", len(kvs), kvs)
	}
}

// Integration: attrs flow into log lines and metrics via logger.Context(ctx).
func TestAttrs_flowsIntoLogger(t *testing.T) {
	sink := NewMemSink()
	metric := sink.NewSum("test_total", "test counter")

	ctx := SetAttrs(context.Background(),
		"customer_id", "acme",
		"environment", "prod",
	)

	logger := New(TestLogger(t))
	logger.Context(ctx).Metric(metric).Info("handled")

	snap := sink.Snapshot()
	if snap["test_total"] != 1 {
		t.Errorf("metric = %v, want 1", snap["test_total"])
	}
}

// Integration: metric picks up context attrs via RecordContext.
func TestAttrs_flowsIntoMetric(t *testing.T) {
	sink := NewMemSink()
	counter := sink.NewSum("req_total", "requests")

	ctx := SetAttrs(context.Background(), "service_name", "valet")
	counter.RecordContext(ctx, 1)

	snap := sink.Snapshot()
	if snap["req_total"] != 1 {
		t.Errorf("metric = %v, want 1", snap["req_total"])
	}
}

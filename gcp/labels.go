package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// labelsHandler is a slog.Handler that hoists certain attribute keys
// out of jsonPayload into logging.googleapis.com/labels for indexed
// filtering in the Cloud Logging UI.
//
// Cap of 64 labels per entry is enforced (GCP's limit). Overflow keys
// stay in jsonPayload and a one-time warning is logged via slog.
type labelsHandler struct {
	inner    slog.Handler
	labelSet map[string]struct{}
}

var labelOverflowOnce sync.Once

// NewLabelsHandler wraps a slog.Handler so that any attribute whose
// key matches labelKeys is emitted under logging.googleapis.com/labels
// instead of as a flat jsonPayload field.
//
// Use this for bounded, low-cardinality values you want indexed for
// fast filtering in the Cloud Logging UI: customer, environment,
// region, service_plane. Do NOT add unbounded keys (request_id,
// user_id) — Cloud Logging indexes labels and bills you per series
// regardless of cardinality. Use plain attrs (or SetLogAttrs in the
// parent package) for unbounded values.
//
//	base := gcp.NewHandler(os.Stderr, "", nil)
//	h := gcp.NewLabelsHandler(base, "customer", "environment", "region")
//	logger := slog.New(h)
//
//	logger.Info("hit",
//	    slog.String("customer", "acme"),
//	    slog.String("environment", "prod"),
//	    slog.Int("rows", 42),
//	)
//	// → {
//	//     "severity": "INFO",
//	//     "message":  "hit",
//	//     "rows":     42,
//	//     "logging.googleapis.com/labels": {
//	//         "customer":    "acme",
//	//         "environment": "prod"
//	//     }
//	// }
//
// The 64-label cap is GCP's. Past that, overflow keys stay in
// jsonPayload (still emitted, just not indexed). The handler logs one
// stderr warning when overflow first occurs so you know to trim the
// label set.
func NewLabelsHandler(inner slog.Handler, labelKeys ...string) slog.Handler {
	set := make(map[string]struct{}, len(labelKeys))
	for _, k := range labelKeys {
		if k != "" {
			set[k] = struct{}{}
		}
	}
	if len(set) == 0 {
		return inner
	}
	return &labelsHandler{inner: inner, labelSet: set}
}

func (h *labelsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *labelsHandler) Handle(ctx context.Context, r slog.Record) error {
	// Walk attrs once to extract those whose keys match labelSet.
	labels := make([]slog.Attr, 0, len(h.labelSet))
	keep := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		if _, ok := h.labelSet[a.Key]; ok {
			labels = append(labels, a)
			return true
		}
		keep = append(keep, a)
		return true
	})

	if len(labels) == 0 {
		return h.inner.Handle(ctx, r)
	}

	// Enforce GCP's 64-label cap. Overflow keys fall back to
	// jsonPayload (we put them into the keep slice) and a one-time
	// warning fires.
	if len(labels) > 64 {
		labelOverflowOnce.Do(func() {
			fmt.Fprintf(
				stderrForOverflow,
				"[dio/logging/gcp] NewLabelsHandler: %d labels exceed GCP's 64-per-entry cap; overflow attrs stay in jsonPayload. Trim labelKeys.\n",
				len(labels),
			)
		})
		// Move overflow to keep.
		keep = append(keep, labels[64:]...)
		labels = labels[:64]
	}

	// Group the labels under the canonical Cloud Logging key.
	// Cloud Logging requires label values to be strings; coerce
	// non-string slog.Values via String().
	labelAttrs := make([]any, 0, len(labels))
	for _, l := range labels {
		labelAttrs = append(labelAttrs, slog.String(l.Key, l.Value.String()))
	}

	// Rebuild the record: keep the original metadata, replace attrs.
	clone := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	for _, a := range keep {
		clone.AddAttrs(a)
	}
	clone.AddAttrs(slog.Group("logging.googleapis.com/labels", labelAttrs...))
	return h.inner.Handle(ctx, clone)
}

func (h *labelsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &labelsHandler{inner: h.inner.WithAttrs(attrs), labelSet: h.labelSet}
}

func (h *labelsHandler) WithGroup(name string) slog.Handler {
	return &labelsHandler{inner: h.inner.WithGroup(name), labelSet: h.labelSet}
}

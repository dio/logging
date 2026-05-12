package gcp

import (
	"context"
	"log/slog"
)

// Operation describes a Cloud Logging operation, used to group log
// entries that share an ID under one expandable entry in the Cloud
// Logging UI.
//
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogEntryOperation
type Operation struct {
	// ID is the operation identifier. Required.
	ID string

	// Producer is the producer of the operation, often the service name.
	// Optional but recommended.
	Producer string

	// First marks the first log entry of the operation. The Cloud Logging
	// UI uses this to position the request marker.
	First bool

	// Last marks the last log entry of the operation. The UI uses this
	// to know when to stop grouping.
	Last bool
}

type operationKey struct{}

// WithOperation attaches an Operation to ctx. Subsequent log entries
// emitted with logger.Context(ctx) carry the operation grouping field
// when the GCP handler is wrapped with NewOperationHandler.
//
// In a typical request flow:
//
//   - Middleware sets the operation on the request context once.
//   - Application code logs normally; operations are emitted automatically.
//   - The bookend log lines should set First=true and Last=true. Use
//     the helpers OperationStart and OperationEnd to emit those.
func WithOperation(ctx context.Context, op Operation) context.Context {
	return context.WithValue(ctx, operationKey{}, op)
}

// OperationFromContext returns the Operation stored in ctx by
// WithOperation, or the zero Operation if none is set.
func OperationFromContext(ctx context.Context) Operation {
	if op, ok := ctx.Value(operationKey{}).(Operation); ok {
		return op
	}
	return Operation{}
}

// OperationStart returns an slog.Attr that marks the FIRST log entry
// of an operation. Add it to the log line that records the start of a
// request:
//
//	logger.LogAttrs(ctx, slog.LevelInfo, "request received",
//	    gcp.OperationStart(reqID, "auth"))
func OperationStart(id, producer string) slog.Attr {
	return operationAttr(Operation{ID: id, Producer: producer, First: true})
}

// OperationEnd returns an slog.Attr that marks the LAST log entry of
// an operation. Add it to the log line that records the end of a
// request:
//
//	logger.LogAttrs(ctx, slog.LevelInfo, "request handled",
//	    gcp.OperationEnd(reqID, "auth"))
func OperationEnd(id, producer string) slog.Attr {
	return operationAttr(Operation{ID: id, Producer: producer, Last: true})
}

// operationAttr renders an Operation as the structured Cloud Logging
// operation field.
func operationAttr(op Operation) slog.Attr {
	if op.ID == "" {
		return slog.Attr{}
	}
	attrs := []any{slog.String("id", op.ID)}
	if op.Producer != "" {
		attrs = append(attrs, slog.String("producer", op.Producer))
	}
	if op.First {
		attrs = append(attrs, slog.Bool("first", true))
	}
	if op.Last {
		attrs = append(attrs, slog.Bool("last", true))
	}
	return slog.Group("logging.googleapis.com/operation", attrs...)
}

// operationHandler is a slog.Handler that injects the Operation from
// context into every log record it sees.
type operationHandler struct {
	inner slog.Handler
}

// NewOperationHandler wraps a slog.Handler so log records inherit the
// Operation attached to the record's context via WithOperation.
//
// Typical use:
//
//	base := gcp.NewHandler(os.Stderr, "", nil)
//	h := gcp.NewOperationHandler(base)
//	logger := slog.New(h)
//
//	// In middleware:
//	ctx = gcp.WithOperation(ctx, gcp.Operation{ID: reqID, Producer: "auth"})
//
//	// Anywhere downstream:
//	logger.InfoContext(ctx, "cache miss")
//	// → carries logging.googleapis.com/operation = {id: reqID, producer: "auth"}
//
// The handler omits First/Last on the context-derived operation; mark
// bookend entries explicitly with OperationStart and OperationEnd as
// per-call slog.Attrs so the Cloud Logging UI groups the request
// correctly.
func NewOperationHandler(inner slog.Handler) slog.Handler {
	return &operationHandler{inner: inner}
}

func (h *operationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *operationHandler) Handle(ctx context.Context, r slog.Record) error {
	op := OperationFromContext(ctx)
	if op.ID == "" {
		return h.inner.Handle(ctx, r)
	}
	// Skip injection if the record already has an operation attr.
	// This lets explicit OperationStart / OperationEnd calls win.
	already := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "logging.googleapis.com/operation" {
			already = true
			return false
		}
		return true
	})
	if already {
		return h.inner.Handle(ctx, r)
	}
	// Inject id + producer only, never first/last from context.
	clone := r.Clone()
	clone.AddAttrs(operationAttr(Operation{ID: op.ID, Producer: op.Producer}))
	return h.inner.Handle(ctx, clone)
}

func (h *operationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &operationHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *operationHandler) WithGroup(name string) slog.Handler {
	return &operationHandler{inner: h.inner.WithGroup(name)}
}

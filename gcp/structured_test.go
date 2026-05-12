package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// parseLine parses a JSON log line into a generic map.
func parseLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("parse: %v\nline: %s", err, line)
	}
	return m
}

func TestHTTPRequest_emitsCanonicalShape(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(&buf, "p1", nil))
	logger.LogAttrs(context.Background(), slog.LevelInfo, "handled",
		HTTPRequest("GET", "/api/echo", 200, 45*time.Millisecond),
	)

	entry := parseLine(t, strings.TrimSpace(buf.String()))
	hr, ok := entry["httpRequest"].(map[string]any)
	if !ok {
		t.Fatalf("httpRequest missing: %v", entry)
	}
	if hr["requestMethod"] != "GET" {
		t.Errorf("method = %v, want GET", hr["requestMethod"])
	}
	if hr["requestUrl"] != "/api/echo" {
		t.Errorf("url = %v, want /api/echo", hr["requestUrl"])
	}
	if hr["status"].(float64) != 200 {
		t.Errorf("status = %v, want 200", hr["status"])
	}
	if hr["latency"] != "0.045s" {
		t.Errorf("latency = %v, want 0.045s", hr["latency"])
	}
}

func TestHTTPRequestFull_omitsEmptyOptionals(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(&buf, "p1", nil))
	logger.LogAttrs(context.Background(), slog.LevelInfo, "handled",
		HTTPRequestFull(
			"POST", "/api/things", 201, 120*time.Millisecond,
			"curl/8", "", "", "HTTP/1.1", 0, 1234,
		),
	)
	entry := parseLine(t, strings.TrimSpace(buf.String()))
	hr := entry["httpRequest"].(map[string]any)
	if hr["userAgent"] != "curl/8" {
		t.Errorf("userAgent = %v", hr["userAgent"])
	}
	if hr["protocol"] != "HTTP/1.1" {
		t.Errorf("protocol = %v", hr["protocol"])
	}
	if hr["responseSize"] != "1234" {
		t.Errorf("responseSize = %v", hr["responseSize"])
	}
	// Empty optionals must not show up.
	for _, k := range []string{"remoteIp", "referer", "requestSize"} {
		if _, present := hr[k]; present {
			t.Errorf("empty %q should be omitted", k)
		}
	}
}

func TestOperation_startAndEndBookends(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(&buf, "p1", nil))

	logger.LogAttrs(context.Background(), slog.LevelInfo, "request received",
		OperationStart("req-abc", "auth"))
	logger.LogAttrs(context.Background(), slog.LevelInfo, "request handled",
		OperationEnd("req-abc", "auth"))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d\nout: %s", len(lines), buf.String())
	}

	first := parseLine(t, lines[0])["logging.googleapis.com/operation"].(map[string]any)
	if first["id"] != "req-abc" || first["producer"] != "auth" || first["first"] != true {
		t.Errorf("first bookend wrong: %v", first)
	}
	if _, hasLast := first["last"]; hasLast {
		t.Errorf("first bookend must not carry last=true: %v", first)
	}

	last := parseLine(t, lines[1])["logging.googleapis.com/operation"].(map[string]any)
	if last["id"] != "req-abc" || last["last"] != true {
		t.Errorf("last bookend wrong: %v", last)
	}
}

func TestOperationHandler_injectsFromContext(t *testing.T) {
	var buf bytes.Buffer
	base := NewHandler(&buf, "p1", nil)
	logger := slog.New(NewOperationHandler(base))

	ctx := WithOperation(context.Background(), Operation{ID: "req-xyz", Producer: "auth"})
	logger.InfoContext(ctx, "cache miss")

	entry := parseLine(t, strings.TrimSpace(buf.String()))
	op, ok := entry["logging.googleapis.com/operation"].(map[string]any)
	if !ok {
		t.Fatalf("operation missing: %v", entry)
	}
	if op["id"] != "req-xyz" || op["producer"] != "auth" {
		t.Errorf("operation = %v", op)
	}
	// Context-derived operation must NOT carry first/last.
	if _, has := op["first"]; has {
		t.Errorf("context-derived op should not have first: %v", op)
	}
	if _, has := op["last"]; has {
		t.Errorf("context-derived op should not have last: %v", op)
	}
}

func TestOperationHandler_explicitWinsOverContext(t *testing.T) {
	var buf bytes.Buffer
	base := NewHandler(&buf, "p1", nil)
	logger := slog.New(NewOperationHandler(base))

	ctx := WithOperation(context.Background(), Operation{ID: "from-ctx", Producer: "auth"})
	logger.LogAttrs(ctx, slog.LevelInfo, "request received",
		OperationStart("explicit-id", "auth"))

	entry := parseLine(t, strings.TrimSpace(buf.String()))
	op := entry["logging.googleapis.com/operation"].(map[string]any)
	if op["id"] != "explicit-id" {
		t.Errorf("explicit OperationStart must win, got %v", op["id"])
	}
	if op["first"] != true {
		t.Errorf("explicit first flag missing: %v", op)
	}
}

func TestOperationHandler_noOpWithoutOperation(t *testing.T) {
	var buf bytes.Buffer
	base := NewHandler(&buf, "p1", nil)
	logger := slog.New(NewOperationHandler(base))

	logger.InfoContext(context.Background(), "nothing")
	entry := parseLine(t, strings.TrimSpace(buf.String()))
	if _, has := entry["logging.googleapis.com/operation"]; has {
		t.Errorf("unexpected operation field: %v", entry)
	}
}

func TestLabelsHandler_routesMatchingKeys(t *testing.T) {
	var buf bytes.Buffer
	base := NewHandler(&buf, "p1", nil)
	logger := slog.New(NewLabelsHandler(base, "customer", "environment"))

	logger.LogAttrs(context.Background(), slog.LevelInfo, "hit",
		slog.String("customer", "acme"),
		slog.String("environment", "prod"),
		slog.Int("rows", 42),
	)

	entry := parseLine(t, strings.TrimSpace(buf.String()))
	labels, ok := entry["logging.googleapis.com/labels"].(map[string]any)
	if !ok {
		t.Fatalf("labels missing: %v", entry)
	}
	if labels["customer"] != "acme" || labels["environment"] != "prod" {
		t.Errorf("labels = %v", labels)
	}
	// Routed keys are removed from jsonPayload.
	if _, present := entry["customer"]; present {
		t.Errorf("customer leaked to jsonPayload: %v", entry)
	}
	if _, present := entry["environment"]; present {
		t.Errorf("environment leaked to jsonPayload: %v", entry)
	}
	// Non-routed key stays.
	if entry["rows"].(float64) != 42 {
		t.Errorf("rows missing or wrong: %v", entry["rows"])
	}
}

func TestLabelsHandler_coercesValuesToString(t *testing.T) {
	var buf bytes.Buffer
	base := NewHandler(&buf, "p1", nil)
	logger := slog.New(NewLabelsHandler(base, "plan_tier", "trial"))

	logger.LogAttrs(context.Background(), slog.LevelInfo, "x",
		slog.Int("plan_tier", 3),
		slog.Bool("trial", false),
	)
	entry := parseLine(t, strings.TrimSpace(buf.String()))
	labels := entry["logging.googleapis.com/labels"].(map[string]any)
	if labels["plan_tier"] != "3" {
		t.Errorf("plan_tier = %v, want \"3\"", labels["plan_tier"])
	}
	if labels["trial"] != "false" {
		t.Errorf("trial = %v, want \"false\"", labels["trial"])
	}
}

func TestLabelsHandler_skipsWhenNoMatch(t *testing.T) {
	var buf bytes.Buffer
	base := NewHandler(&buf, "p1", nil)
	logger := slog.New(NewLabelsHandler(base, "customer"))

	logger.LogAttrs(context.Background(), slog.LevelInfo, "x",
		slog.String("unrelated", "value"),
	)
	entry := parseLine(t, strings.TrimSpace(buf.String()))
	if _, has := entry["logging.googleapis.com/labels"]; has {
		t.Errorf("labels group should be skipped: %v", entry)
	}
}

func TestLabelsHandler_emptyKeysReturnsInnerUnchanged(t *testing.T) {
	var buf bytes.Buffer
	base := NewHandler(&buf, "p1", nil)
	h := NewLabelsHandler(base)
	if h != base {
		t.Errorf("NewLabelsHandler with no keys should return inner unchanged")
	}
}

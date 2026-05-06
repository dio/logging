package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPLevelHandler_GET(t *testing.T) {
	h := NewLevelHandler(LevelWarning, slog.NewTextHandler(io.Discard, nil))
	logger := slog.New(h)
	handler := NewHTTPLevelHandler(logger)

	req := httptest.NewRequest(http.MethodGet, "/log/level", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp getLevelResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Level != "WARNING" {
		t.Errorf("level = %q, want WARNING", resp.Level)
	}
	if len(resp.Available) == 0 {
		t.Error("available_levels is empty")
	}
}

func TestHTTPLevelHandler_PUT_success(t *testing.T) {
	h := NewLevelHandler(LevelInfo, slog.NewTextHandler(io.Discard, nil))
	logger := slog.New(h)
	handler := NewHTTPLevelHandler(logger)

	body, _ := json.Marshal(setLevelRequest{Level: "DEBUG"})
	req := httptest.NewRequest(http.MethodPut, "/log/level", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", w.Code)
	}

	var resp setLevelResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Level != "DEBUG" {
		t.Errorf("level = %q, want DEBUG", resp.Level)
	}
	if resp.Previous != "INFO" {
		t.Errorf("previous = %q, want INFO", resp.Previous)
	}
	if !strings.Contains(resp.Message, "DEBUG") {
		t.Errorf("message %q does not mention DEBUG", resp.Message)
	}

	// handler level was actually changed
	if h.Level() != LevelDebug {
		t.Errorf("handler level = %v, want LevelDebug", h.Level())
	}
}

func TestHTTPLevelHandler_PUT_caseInsensitive(t *testing.T) {
	h := NewLevelHandler(LevelInfo, slog.NewTextHandler(io.Discard, nil))
	handler := NewHTTPLevelHandler(slog.New(h))

	body, _ := json.Marshal(setLevelRequest{Level: "error"})
	req := httptest.NewRequest(http.MethodPut, "/log/level", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp setLevelResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Level != "ERROR" {
		t.Errorf("level = %q, want ERROR", resp.Level)
	}
}

func TestHTTPLevelHandler_PUT_invalidJSON(t *testing.T) {
	h := NewLevelHandler(LevelInfo, slog.NewTextHandler(io.Discard, nil))
	handler := NewHTTPLevelHandler(slog.New(h))

	req := httptest.NewRequest(http.MethodPut, "/log/level", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp errorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "invalid_json" {
		t.Errorf("error = %q, want invalid_json", resp.Error)
	}
}

func TestHTTPLevelHandler_PUT_missingLevel(t *testing.T) {
	h := NewLevelHandler(LevelInfo, slog.NewTextHandler(io.Discard, nil))
	handler := NewHTTPLevelHandler(slog.New(h))

	body, _ := json.Marshal(setLevelRequest{Level: ""})
	req := httptest.NewRequest(http.MethodPut, "/log/level", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp errorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "missing_level" {
		t.Errorf("error = %q, want missing_level", resp.Error)
	}
}

func TestHTTPLevelHandler_PUT_invalidLevel(t *testing.T) {
	h := NewLevelHandler(LevelInfo, slog.NewTextHandler(io.Discard, nil))
	handler := NewHTTPLevelHandler(slog.New(h))

	body, _ := json.Marshal(setLevelRequest{Level: "NONSENSE"})
	req := httptest.NewRequest(http.MethodPut, "/log/level", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp errorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "invalid_level" {
		t.Errorf("error = %q, want invalid_level", resp.Error)
	}
}

func TestHTTPLevelHandler_methodNotAllowed(t *testing.T) {
	h := NewLevelHandler(LevelInfo, slog.NewTextHandler(io.Discard, nil))
	handler := NewHTTPLevelHandler(slog.New(h))

	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(method, "/log/level", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", method, w.Code)
		}
	}
}

func TestHTTPLevelHandler_integration(t *testing.T) {
	h := NewLevelHandler(LevelWarning, slog.NewTextHandler(io.Discard, nil))
	logger := slog.New(h)
	handler := NewHTTPLevelHandler(logger)

	checkLevel := func(want string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/log/level", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var resp getLevelResponse
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp.Level != want {
			t.Errorf("GET level = %q, want %q", resp.Level, want)
		}
	}

	setLevel := func(level string) {
		t.Helper()
		body, _ := json.Marshal(setLevelRequest{Level: level})
		req := httptest.NewRequest(http.MethodPut, "/log/level", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("PUT %q status = %d", level, w.Code)
		}
	}

	checkLevel("WARNING")
	setLevel("DEBUG")
	checkLevel("DEBUG")
	setLevel("ERROR")
	checkLevel("ERROR")
}

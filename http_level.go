package logging

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// HTTPLevelHandler is an http.Handler that exposes a GET/PUT endpoint for
// reading and changing the log level at runtime without a restart.
//
// Register it on your admin/pprof mux:
//
//	mux.Handle("/log/level", logging.NewHTTPLevelHandler(logger))
//
// GET  /log/level  -> {"level":"INFO","available_levels":["DEBUG",...]}
// PUT  /log/level  <- {"level":"DEBUG"}
//
//	-> {"level":"DEBUG","previous_level":"INFO","message":"..."}
type HTTPLevelHandler struct {
	logger *slog.Logger
}

// NewHTTPLevelHandler creates an HTTPLevelHandler for logger.
// The logger's handler must implement LevelableHandler (i.e. be wrapped with
// NewLevelHandler); otherwise PUT requests will panic.
func NewHTTPLevelHandler(logger *slog.Logger) *HTTPLevelHandler {
	return &HTTPLevelHandler{logger: logger}
}

// ServeHTTP dispatches GET and PUT; all other methods return 405.
func (h *HTTPLevelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w)
	case http.MethodPut:
		h.handlePut(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			fmt.Sprintf("method %s not allowed; use GET or PUT", r.Method))
	}
}

// --- response types ---

type getLevelResponse struct {
	Level     string   `json:"level"`
	Available []string `json:"available_levels"`
}

type setLevelRequest struct {
	Level string `json:"level"`
}

type setLevelResponse struct {
	Level    string `json:"level"`
	Previous string `json:"previous_level"`
	Message  string `json:"message"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// --- handlers ---

func (h *HTTPLevelHandler) handleGet(w http.ResponseWriter) {
	cur := LevelString(h.currentLevel())
	h.writeJSON(w, http.StatusOK, getLevelResponse{
		Level:     cur,
		Available: LevelNames(),
	})
}

func (h *HTTPLevelHandler) handlePut(w http.ResponseWriter, r *http.Request) {
	var req setLevelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_json",
			`invalid JSON body; expected {"level":"INFO"}`)
		return
	}
	if req.Level == "" {
		h.writeError(w, http.StatusBadRequest, "missing_level",
			"level field is required; available: "+strings.Join(LevelNames(), ", "))
		return
	}

	prev := h.currentLevel()

	newLevel, err := LookupLevel(req.Level)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_level",
			fmt.Sprintf("invalid level %q; available: %s", req.Level, strings.Join(LevelNames(), ", ")))
		return
	}

	SetLevel(h.logger, newLevel)

	prevName := LevelString(prev)
	newName := LevelString(newLevel)

	h.logger.Info("log level changed",
		slog.String("previous_level", prevName),
		slog.String("new_level", newName),
		slog.String("remote_addr", r.RemoteAddr),
		slog.String("user_agent", r.UserAgent()),
	)

	h.writeJSON(w, http.StatusOK, setLevelResponse{
		Level:    newName,
		Previous: prevName,
		Message:  fmt.Sprintf("log level changed from %s to %s", prevName, newName),
	})
}

// currentLevel reads the active level from the logger's handler.
// Falls back to LevelInfo for handlers that don't expose their level.
func (h *HTTPLevelHandler) currentLevel() slog.Level {
	if lh, ok := h.logger.Handler().(*LevelHandler); ok {
		return lh.Level()
	}
	if lh, ok := h.logger.Handler().(interface{ Level() slog.Level }); ok {
		return lh.Level()
	}
	return LevelInfo
}

// --- helpers ---

func (h *HTTPLevelHandler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *HTTPLevelHandler) writeError(w http.ResponseWriter, status int, errType, message string) {
	h.writeJSON(w, status, errorResponse{Error: errType, Message: message})
}

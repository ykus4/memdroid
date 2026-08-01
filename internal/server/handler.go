package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"memdroid/internal/app"
	"memdroid/internal/driver/adb"
	"memdroid/internal/memory/search"
)

// handler carries the shared state every endpoint needs. Endpoints themselves
// live in the handlers_*.go files, grouped by the resource they act on.
type handler struct {
	state *app.State
	adb   *adb.ADB
	// fileRoot restricts which paths session save/load and .CT import may
	// touch. Empty means unrestricted (CLI-equivalent behaviour).
	fileRoot string
}

// --- response helpers ---

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeOK(w http.ResponseWriter, extra map[string]any) {
	out := map[string]any{"ok": true}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, out)
}

// hexAddr marshals a memory address as a "0x..." JSON string. Addresses appear
// in nearly every response; a dedicated type keeps the formatting in one place
// and out of the handlers.
type hexAddr uintptr

func (a hexAddr) MarshalJSON() ([]byte, error) {
	return []byte(`"0x` + strconv.FormatUint(uint64(a), 16) + `"`), nil
}

func (a hexAddr) String() string {
	return "0x" + strconv.FormatUint(uint64(a), 16)
}

// --- request helpers ---

// decodeJSON decodes the request body into a value of type T. On failure it
// writes a 400 response and returns ok=false; callers must return immediately.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return v, false
	}
	return v, true
}

// decodeOptionalJSON behaves like decodeJSON but treats an empty body as an
// empty value, for endpoints whose fields are all optional.
func decodeOptionalJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	err := json.NewDecoder(r.Body).Decode(&v)
	if err == nil || errors.Is(err, io.EOF) {
		return v, true
	}
	writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
	return v, false
}

func parseHexAddr(s string) (uintptr, error) {
	v, err := strconv.ParseUint(s, 0, 64)
	return uintptr(v), err
}

// requireAddr parses a hex address, writing a 400 on failure.
func requireAddr(w http.ResponseWriter, s string) (uintptr, bool) {
	if s == "" {
		writeError(w, http.StatusBadRequest, "addr required")
		return 0, false
	}
	addr, err := parseHexAddr(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid addr: "+err.Error())
		return 0, false
	}
	return addr, true
}

// requireValue parses a typed value string, writing a 400 on failure.
func requireValue(w http.ResponseWriter, s string, vt search.ValueType) ([]byte, bool) {
	val, err := search.ParseValue(s, vt)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid value: %v", err))
		return nil, false
	}
	return val, true
}

// requireSize validates a byte count against an inclusive upper bound.
func requireSize(w http.ResponseWriter, size, maxSize int) bool {
	if size <= 0 || size > maxSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("size must be 1-%d", maxSize))
		return false
	}
	return true
}

// requireDuration parses a positive millisecond count into a Duration.
func requireDuration(w http.ResponseWriter, ms int, fallback time.Duration) (time.Duration, bool) {
	if ms == 0 {
		return fallback, true
	}
	if ms < 0 {
		writeError(w, http.StatusBadRequest, "interval_ms must be positive")
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// requirePID returns the attached PID, writing a 400 when nothing is attached.
func (h *handler) requirePID(w http.ResponseWriter) (int, bool) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, http.StatusBadRequest, "not attached")
		return 0, false
	}
	return pid, true
}

// requireSession returns the active session, writing a 400 when there is none
// or it holds no candidates.
func (h *handler) requireSession(w http.ResponseWriter) (*search.Session, bool) {
	sess := h.state.GetSession()
	if sess == nil || !sess.HasCandidates() {
		writeError(w, http.StatusBadRequest, "no active search session")
		return nil, false
	}
	return sess, true
}

// queryInt parses a query parameter as an int, returning def if absent/invalid
// or below minValue.
func queryInt(r *http.Request, key string, def, minValue int) int {
	if s := r.URL.Query().Get(key); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= minValue {
			return v
		}
	}
	return def
}

// serverError writes err as a 500 with op for context.
func serverError(w http.ResponseWriter, op string, err error) {
	writeError(w, http.StatusInternalServerError, op+": "+err.Error())
}

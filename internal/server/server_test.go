package server

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"memdroid/internal/app"
	"memdroid/internal/driver/adb"
	"memdroid/internal/driver/drivertest"
)

// --- fake process layout -----------------------------------------------------
//
// A single 1 KiB "[heap]" region at 0x1000 holding a handful of known values at
// 4-byte aligned offsets. Everything else is zero, so the number of matches for
// each probe value below is exact.

const (
	heapBase = uintptr(0x1000)
	heapSize = 1024

	offInt32A = 0x10 // -> 0x1010
	offInt32B = 0x20 // -> 0x1020
	offInt32C = 0x30 // -> 0x1030
	offFloatA = 0x40 // -> 0x1040
	offFloatB = 0x50 // -> 0x1050
	offString = 0x80 // -> 0x1080

	probeInt32   = int32(1337)
	probeFloat32 = float32(2.5)
	probeString  = "MEMDROID"
)

func newHeap() []byte {
	data := make([]byte, heapSize)
	for _, off := range []int{offInt32A, offInt32B, offInt32C} {
		binary.LittleEndian.PutUint32(data[off:], uint32(probeInt32))
	}
	for _, off := range []int{offFloatA, offFloatB} {
		binary.LittleEndian.PutUint32(data[off:], math.Float32bits(probeFloat32))
	}
	copy(data[offString:], probeString)
	return data
}

// --- test harness ------------------------------------------------------------

// env is a fully wired server (real routing, real middleware, embedded static
// files) backed by the in-memory driver.
type env struct {
	t     *testing.T
	fake  *drivertest.Fake
	state *app.State
	srv   *Server
	mux   http.Handler
}

// newEnv builds a server exactly the way cmd does, so tests exercise routes(),
// only() and secure() rather than a hand-rolled mux.
//
// The adb handle is a bare *adb.ADB: it never shells out unless an adb-backed
// endpoint is called, and the tests deliberately only touch the branches of
// those handlers that run before the first exec.
func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}
	fake := drivertest.New(drivertest.Region{Start: heapBase, Name: "[heap]", Data: newHeap()})
	state := app.NewState(fake)

	srv, err := New(cfg, state, adb.New())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown() })

	return &env{t: t, fake: fake, state: state, srv: srv, mux: srv.http.Handler}
}

func (e *env) rec(method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// obj issues a request, asserts the status code and decodes a JSON object.
func (e *env) obj(method, path, body string, wantCode int) map[string]any {
	e.t.Helper()
	rec := e.rec(method, path, body)
	if rec.Code != wantCode {
		e.t.Fatalf("%s %s: status = %d, want %d (body: %s)", method, path, rec.Code, wantCode, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
	}
	return out
}

// arr is obj for endpoints that return a top-level JSON array.
func (e *env) arr(method, path, body string, wantCode int) []any {
	e.t.Helper()
	rec := e.rec(method, path, body)
	if rec.Code != wantCode {
		e.t.Fatalf("%s %s: status = %d, want %d (body: %s)", method, path, rec.Code, wantCode, rec.Body.String())
	}
	var out []any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
	}
	return out
}

// errMsg asserts a failing status and returns the "error" field.
func (e *env) errMsg(method, path, body string, wantCode int) string {
	e.t.Helper()
	return str(e.t, e.obj(method, path, body, wantCode), "error")
}

func (e *env) attach() {
	e.t.Helper()
	e.obj(http.MethodPost, "/api/process/attach", `{"pid":1,"name":"com.example.fake"}`, http.StatusOK)
}

// heapInt32 reads a 4-byte little-endian int from the fake process.
func (e *env) heapInt32(addr uintptr) int32 {
	e.t.Helper()
	b := e.fake.Bytes(addr, 4)
	if len(b) < 4 {
		e.t.Fatalf("read 0x%x: got %d bytes", addr, len(b))
	}
	return int32(binary.LittleEndian.Uint32(b))
}

// --- JSON field accessors ----------------------------------------------------

func num(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("field %q: want number, got %T (%v)", key, m[key], m[key])
	}
	return v
}

func str(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key].(string)
	if !ok {
		t.Fatalf("field %q: want string, got %T (%v)", key, m[key], m[key])
	}
	return v
}

func boolean(t *testing.T, m map[string]any, key string) bool {
	t.Helper()
	v, ok := m[key].(bool)
	if !ok {
		t.Fatalf("field %q: want bool, got %T (%v)", key, m[key], m[key])
	}
	return v
}

func slice(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("field %q: want array, got %T (%v)", key, m[key], m[key])
	}
	return v
}

func object(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want object, got %T (%v)", v, v)
	}
	return m
}

// --- method enforcement ------------------------------------------------------

func TestOnlyRejectsWrongMethod(t *testing.T) {
	e := newEnv(t, Config{})

	tests := []struct {
		name      string
		method    string
		path      string
		wantAllow string
	}{
		{"POST to GET-only route", http.MethodPost, "/api/status", "GET, HEAD"},
		{"DELETE to GET-only route", http.MethodDelete, "/api/search/candidates", "GET, HEAD"},
		{"GET to POST-only route", http.MethodGet, "/api/search/reset", http.MethodPost},
		{"PUT to POST-only route", http.MethodPut, "/api/bookmark/add", http.MethodPost},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.rec(tc.method, tc.path, "")
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405 (body: %s)", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Allow"); got != tc.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tc.wantAllow)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != "method not allowed" {
				t.Errorf("error = %q, want %q", body["error"], "method not allowed")
			}
		})
	}
}

func TestOnlyAllowsCorrectMethod(t *testing.T) {
	e := newEnv(t, Config{})

	if rec := e.rec(http.MethodGet, "/api/status", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/status: status = %d, want 200", rec.Code)
	}
	// GET handlers also serve HEAD.
	if rec := e.rec(http.MethodHead, "/api/status", ""); rec.Code != http.StatusOK {
		t.Errorf("HEAD /api/status: status = %d, want 200", rec.Code)
	}
	// HEAD must not be silently accepted by POST-only routes.
	if rec := e.rec(http.MethodHead, "/api/search/reset", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("HEAD /api/search/reset: status = %d, want 405", rec.Code)
	}
	if rec := e.rec(http.MethodPost, "/api/search/reset", ""); rec.Code != http.StatusOK {
		t.Errorf("POST /api/search/reset: status = %d, want 200", rec.Code)
	}
}

func TestRoutesAreUniqueAndWellFormed(t *testing.T) {
	h := &handler{}
	seen := make(map[string]bool)
	for _, rt := range h.routes() {
		if !strings.HasPrefix(rt.path, "/api/") {
			t.Errorf("route %q: want an /api/ prefix", rt.path)
		}
		if rt.method != http.MethodGet && rt.method != http.MethodPost {
			t.Errorf("route %q: unexpected method %q", rt.path, rt.method)
		}
		if rt.handler == nil {
			t.Errorf("route %q: nil handler", rt.path)
		}
		if seen[rt.path] {
			t.Errorf("route %q registered twice", rt.path)
		}
		seen[rt.path] = true
	}
}

// --- auth --------------------------------------------------------------------

const testToken = "s3cr3t-token"

func TestSecureRejectsMissingCredentials(t *testing.T) {
	e := newEnv(t, Config{Token: testToken})

	for _, path := range []string{"/api/status", "/api/search/candidates", "/ws/watch"} {
		t.Run(path, func(t *testing.T) {
			rec := e.rec(http.MethodGet, path, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != "unauthorized" {
				t.Errorf("error = %q, want %q", body["error"], "unauthorized")
			}
		})
	}
}

func TestSecureRejectsWrongCredentials(t *testing.T) {
	e := newEnv(t, Config{Token: testToken})

	tests := []struct {
		name string
		mod  func(*http.Request)
	}{
		{"wrong query token", func(r *http.Request) { r.URL.RawQuery = "token=nope" }},
		{"wrong cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "mdtoken", Value: "nope"}) }},
		{"wrong bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }},
		{"token as prefix", func(r *http.Request) { r.URL.RawQuery = "token=" + testToken + "x" }},
		{"empty bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") }},
		{"non-bearer scheme", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+testToken) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			tc.mod(req)
			rec := httptest.NewRecorder()
			e.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSecureAcceptsEveryCredentialForm(t *testing.T) {
	e := newEnv(t, Config{Token: testToken})

	tests := []struct {
		name string
		mod  func(*http.Request)
	}{
		{"query parameter", func(r *http.Request) { r.URL.RawQuery = "token=" + testToken }},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "mdtoken", Value: testToken}) }},
		{"bearer header", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+testToken) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			tc.mod(req)
			rec := httptest.NewRecorder()
			e.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestSecureSetsCookieFromQuery covers the hand-off that lets the browser UI
// keep working after the first ?token= navigation.
func TestSecureSetsCookieFromQuery(t *testing.T) {
	e := newEnv(t, Config{Token: testToken})

	rec := e.rec(http.MethodGet, "/api/status?token="+testToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mdtoken" {
			found = c
		}
	}
	if found == nil {
		t.Fatal("no mdtoken cookie set")
	}
	if found.Value != testToken {
		t.Errorf("cookie value = %q, want %q", found.Value, testToken)
	}
	if !found.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if found.Path != "/" {
		t.Errorf("cookie path = %q, want %q", found.Path, "/")
	}
	if found.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want Strict", found.SameSite)
	}
}

// TestSecureLeavesStaticUnprotected pins the deliberate carve-out: only /api/
// and /ws/ need a credential, so the UI shell can load and then present one.
func TestSecureLeavesStaticUnprotected(t *testing.T) {
	e := newEnv(t, Config{Token: testToken})

	t.Run("/", func(t *testing.T) {
		rec := e.rec(http.MethodGet, "/", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Error("empty body")
		}
	})

	// http.FileServer canonicalises /index.html to / with a 301. What matters
	// here is that it is served rather than challenged for a token.
	t.Run("/index.html", func(t *testing.T) {
		rec := e.rec(http.MethodGet, "/index.html", "")
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("status = %d, want 301 (FileServer redirect to /)", rec.Code)
		}
	})
	// A path that merely looks like the API prefix is still static.
	if rec := e.rec(http.MethodGet, "/apiary", ""); rec.Code == http.StatusUnauthorized {
		t.Error("/apiary was treated as protected")
	}
}

func TestSecureDisabledWithoutToken(t *testing.T) {
	e := newEnv(t, Config{})

	for _, path := range []string{"/", "/api/status", "/api/search/candidates"} {
		if rec := e.rec(http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (body: %s)", path, rec.Code, rec.Body.String())
		}
	}
}

// TestSecureCapsBodySize covers the MaxBytesReader wrapper.
func TestSecureCapsBodySize(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	huge := `{"value":"` + strings.Repeat("1", maxBodyBytes+16) + `"}`
	rec := e.rec(http.MethodPost, "/api/search/value", huge)
	if rec.Code == http.StatusOK {
		t.Fatalf("oversized body accepted (body: %s)", rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestWebSocketAuth needs a real listener: x/net/websocket hijacks the
// connection, which an httptest.ResponseRecorder cannot do.
func TestWebSocketAuth(t *testing.T) {
	e := newEnv(t, Config{Token: testToken})
	ts := httptest.NewServer(e.mux)
	t.Cleanup(ts.Close)

	get := func(t *testing.T, url string) int {
		t.Helper()
		resp, err := ts.Client().Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	if code := get(t, ts.URL+"/ws/watch"); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated /ws/watch: status = %d, want 401", code)
	}
	// With a valid token the request reaches the WebSocket handler, which
	// rejects the (non-handshake) request itself — the point is that it is no
	// longer a 401.
	if code := get(t, ts.URL+"/ws/watch?token="+testToken); code == http.StatusUnauthorized {
		t.Error("authenticated /ws/watch: still 401")
	}
}

func TestWebSocketOriginCheck(t *testing.T) {
	e := newEnv(t, Config{})
	ts := httptest.NewServer(e.mux)
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/ws/watch", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://evil.example.com")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /ws/watch: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(body["error"], "cross-origin") {
		t.Errorf("error = %q, want it to mention cross-origin", body["error"])
	}
}

// --- address helpers ---------------------------------------------------------

func TestIsLoopback(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"127.0.0.53:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{"0.0.0.0:8080", false},
		{":8080", false},
		{"192.168.1.5:8080", false},
		{"[::]:8080", false},

		// Bare hosts with no port.
		{"127.0.0.1", true},
		{"localhost", true},
		{"::1", true},
		{"192.168.1.5", false},
		{"example.com", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			if got := IsLoopback(tc.addr); got != tc.want {
				t.Errorf("IsLoopback(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestDisplayURL(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"localhost:8080", "http://localhost:8080"},
		// IPv6 hosts must keep their brackets, or the URL is unusable.
		{"[::1]:8080", "http://[::1]:8080"},
		{"[fe80::1]:8080", "http://[fe80::1]:8080"},
		{"0.0.0.0:8080", "http://localhost:8080"},
		{"[::]:8080", "http://localhost:8080"},
		{":8080", "http://localhost:8080"},
		{"192.168.1.5:8080", "http://192.168.1.5:8080"},

		// No port at all: SplitHostPort fails and the input is passed through.
		{"localhost", "http://localhost"},
		{"example.com", "http://example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			if got := DisplayURL(tc.addr); got != tc.want {
				t.Errorf("DisplayURL(%q) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		listenAddr string
		host       string
		want       bool
	}{
		{"identical host", "http://example.com:9000", "0.0.0.0:9000", "example.com:9000", true},
		{"identical loopback host", "http://127.0.0.1:8080", "127.0.0.1:8080", "127.0.0.1:8080", true},
		{"localhost alias of listen port", "http://localhost:8080", "0.0.0.0:8080", "192.168.1.5:8080", true},
		{"127.0.0.1 alias of listen port", "http://127.0.0.1:8080", "0.0.0.0:8080", "192.168.1.5:8080", true},
		{"ipv6 loopback alias of listen port", "http://[::1]:8080", ":8080", "192.168.1.5:8080", true},
		{"foreign origin", "http://evil.example.com", ":8080", "127.0.0.1:8080", false},
		{"right host wrong port", "http://127.0.0.1:9999", ":8080", "127.0.0.1:8080", false},
		{"https scheme same host", "https://127.0.0.1:8080", ":8080", "127.0.0.1:8080", true},
		{"unparseable origin", "://nope", ":8080", "127.0.0.1:8080", false},
		{"listen addr without port", "http://localhost:8080", "not-an-addr", "127.0.0.1:8080", false},
		{"empty origin host", "", ":8080", "127.0.0.1:8080", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameOrigin(tc.origin, tc.listenAddr, tc.host); got != tc.want {
				t.Errorf("sameOrigin(%q, %q, %q) = %v, want %v", tc.origin, tc.listenAddr, tc.host, got, tc.want)
			}
		})
	}
}

// --- hexAddr -----------------------------------------------------------------

func TestHexAddrMarshalJSON(t *testing.T) {
	tests := []struct {
		in   hexAddr
		want string
	}{
		{0, `"0x0"`},
		{500, `"0x1f4"`},
		{0x1000, `"0x1000"`},
		{0xdeadbeef, `"0xdeadbeef"`},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("Marshal(%d) = %s, want %s", uint64(tc.in), got, tc.want)
			}
			if tc.in.String() != strings.Trim(tc.want, `"`) {
				t.Errorf("String() = %q, want %q", tc.in.String(), strings.Trim(tc.want, `"`))
			}
		})
	}
}

func TestHexAddrMarshalsInsideStruct(t *testing.T) {
	v := struct {
		Addr hexAddr `json:"addr"`
	}{Addr: 500}
	got, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{"addr":"0x1f4"}`; string(got) != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

func TestHexAddrsEmptyIsArrayNotNull(t *testing.T) {
	got, err := json.Marshal(hexAddrs(nil))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != "[]" {
		t.Errorf("Marshal(hexAddrs(nil)) = %s, want []", got)
	}
}

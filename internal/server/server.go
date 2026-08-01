package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"memdroid/internal/app"
	"memdroid/internal/driver/adb"
	"memdroid/internal/memory/watch"
	"memdroid/internal/server/wswatch"
)

//go:embed static
var staticFiles embed.FS

const (
	maxBodyBytes      = 1 << 20 // 1 MiB request-body cap
	readHeaderTimeout = 15 * time.Second
	writeTimeout      = 120 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// Config controls how the HTTP server is exposed.
type Config struct {
	// Addr is the listen address, e.g. "127.0.0.1:8080".
	Addr string
	// Token, when non-empty, is required on all /api and /ws requests (via the
	// "token" query parameter, an "mdtoken" cookie, or an
	// "Authorization: Bearer" header).
	Token string
	// FileRoot confines the paths the file-touching endpoints may use. Empty
	// disables the restriction.
	FileRoot string
}

// Server is a configured but not-yet-listening HTTP server.
type Server struct {
	http *http.Server
	hub  *wswatch.Hub
	// unsubscribe detaches the event listeners registered at build time.
	unsubscribe []func()
}

// New wires up the API, the embedded Web UI, and the WebSocket event feed.
func New(cfg Config, state *app.State, d *adb.ADB) (*Server, error) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("embed static: %w", err)
	}

	hub := wswatch.NewHub()
	h := &handler{state: state, adb: d, fileRoot: cfg.FileRoot}

	srv := &Server{hub: hub}

	// Push watch and alert activity to connected browsers. These are
	// registrations, not assignments, so the CLI's own printers keep working.
	srv.unsubscribe = append(srv.unsubscribe,
		state.Watcher.OnChange(func(ev watch.ChangeEvent) {
			hub.Broadcast(wswatch.Event{
				Kind: "change",
				Addr: hexAddr(ev.Addr).String(),
				Prev: ev.Prev,
				Cur:  ev.Cur,
			})
		}),
		state.AlertWatcher.OnAlert(func(ev watch.AlertEvent) {
			hub.Broadcast(wswatch.Event{
				Kind:      "alert",
				Addr:      hexAddr(ev.Addr).String(),
				Cur:       ev.Value,
				Condition: ev.Condition,
				Triggered: ev.Triggered,
			})
		}),
	)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.Handle("/ws/watch", originChecked(cfg.Addr, websocket.Handler(hub.Register)))
	for _, rt := range h.routes() {
		mux.HandleFunc(rt.path, only(rt.method, rt.handler))
	}

	srv.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           secure(cfg.Token, mux),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	return srv, nil
}

// ListenAndServe runs the server until it is shut down. It returns nil on a
// clean shutdown.
func (s *Server) ListenAndServe() error {
	err := s.http.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops accepting requests, drops WebSocket clients, and unregisters
// the event listeners.
func (s *Server) Shutdown() error {
	for _, remove := range s.unsubscribe {
		remove()
	}
	s.hub.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// secure caps the request body size and, when a token is configured, enforces
// it on /api and /ws paths. Presenting ?token=<token> sets a cookie so the
// browser UI keeps working after the first navigation.
func secure(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Only persist a credential that actually checks out, so a bad
		// ?token= cannot plant a cookie that fails every later request.
		if q := r.URL.Query().Get("token"); secretEqual(q, token) {
			http.SetCookie(w, &http.Cookie{
				Name:     "mdtoken",
				Value:    q,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}

		protected := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/")
		if protected && !tokenOK(r, token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenOK compares the presented credential in constant time so a remote caller
// cannot recover the token byte-by-byte from response latency.
func tokenOK(r *http.Request, token string) bool {
	if secretEqual(r.URL.Query().Get("token"), token) {
		return true
	}
	if c, err := r.Cookie("mdtoken"); err == nil && secretEqual(c.Value, token) {
		return true
	}
	// Require the scheme rather than accepting a bare header value, so the
	// contract matches what the docs promise.
	if h, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && secretEqual(h, token) {
		return true
	}
	return false
}

func secretEqual(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// originChecked rejects cross-origin WebSocket handshakes.
//
// x/net/websocket does not validate Origin, so without this any web page the
// user visits could open a socket to a loopback memdroid and read the live
// watch feed.
func originChecked(listenAddr string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && !sameOrigin(origin, listenAddr, r.Host) {
			writeError(w, http.StatusForbidden, "cross-origin WebSocket connections are not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether origin points at this server.
func sameOrigin(origin, listenAddr, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Host == host {
		return true
	}
	// A loopback listener is reachable under several equivalent names.
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false
	}
	for _, h := range []string{"localhost:" + port, "127.0.0.1:" + port, "[::1]:" + port} {
		if u.Host == h {
			return true
		}
	}
	return false
}

// DisplayURL renders a browser-friendly URL from a listen address, turning a
// bare ":8080" or "0.0.0.0:8080" into a localhost URL.
func DisplayURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// No port. A bare IPv6 literal still needs brackets to form a URL.
		if ip := net.ParseIP(addr); ip != nil && ip.To4() == nil {
			return "http://[" + addr + "]"
		}
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// IsLoopback reports whether addr binds only to the local machine. Callers use
// it to decide whether running without a token is safe.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

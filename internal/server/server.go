package server

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
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

const maxBodyBytes = 1 << 20 // 1 MiB request-body cap

// Start launches the HTTP server on addr (e.g. "127.0.0.1:8080") in the
// foreground. If token is non-empty, all /api and /ws requests must present it
// (via the "token" query parameter, a "mdtoken" cookie, or an
// "Authorization: Bearer" header). Call Start in a goroutine from main.
func Start(addr, token string, state *app.State, d *adb.ADB) error {
	state.Watcher.OnChange = func(ev watch.ChangeEvent) {
		wswatch.Broadcast(wswatch.Event{
			Addr: fmt.Sprintf("0x%x", ev.Addr),
			Prev: ev.Prev,
			Cur:  ev.Cur,
		})
	}

	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("embed static: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	h := &handler{state: state, adb: d}

	mux.Handle("/ws/watch", websocket.Handler(func(ws *websocket.Conn) {
		wswatch.Register(ws)
	}))

	mux.HandleFunc("/api/status", get(h.status))

	// Device
	mux.HandleFunc("/api/device/list", get(h.deviceList))
	mux.HandleFunc("/api/device/select", post(h.deviceSelect))
	mux.HandleFunc("/api/device/connect-wifi", post(h.deviceConnectWifi))
	mux.HandleFunc("/api/device/disconnect-wifi", post(h.deviceDisconnectWifi))

	// Process
	mux.HandleFunc("/api/process/list", get(h.processList))
	mux.HandleFunc("/api/process/search", post(h.processSearch))
	mux.HandleFunc("/api/process/attach", post(h.processAttach))
	mux.HandleFunc("/api/process/detach", post(h.processDetach))
	mux.HandleFunc("/api/process/stop", post(h.processStop))
	mux.HandleFunc("/api/process/continue", post(h.processContinue))

	// Maps
	mux.HandleFunc("/api/maps", get(h.mapsList))

	// Search
	mux.HandleFunc("/api/search/value", post(h.searchValue))
	mux.HandleFunc("/api/search/pattern", post(h.searchPattern))
	mux.HandleFunc("/api/search/string", post(h.searchString))
	mux.HandleFunc("/api/search/filter", post(h.searchFilter))
	mux.HandleFunc("/api/search/candidates", get(h.searchCandidates))
	mux.HandleFunc("/api/search/reset", post(h.searchReset))

	// Pointer scan
	mux.HandleFunc("/api/pointer/scan", post(h.pointerScan))
	mux.HandleFunc("/api/pointer/resolve", post(h.pointerResolve))

	// Memory
	mux.HandleFunc("/api/memory/modify", post(h.memoryModify))
	mux.HandleFunc("/api/memory/undo", post(h.memoryUndo))
	mux.HandleFunc("/api/memory/freeze", post(h.memoryFreeze))
	mux.HandleFunc("/api/memory/freeze-interval", post(h.freezeSetInterval))
	mux.HandleFunc("/api/memory/freeze-all", post(h.memoryFreezeAll))
	mux.HandleFunc("/api/memory/unfreeze", post(h.memoryUnfreeze))
	mux.HandleFunc("/api/memory/frozen", get(h.memoryFrozen))
	mux.HandleFunc("/api/memory/hexdump", get(h.memoryHexdump))

	// Snapshot
	mux.HandleFunc("/api/snapshot/take", post(h.snapshotTake))
	mux.HandleFunc("/api/snapshot/diff", post(h.snapshotDiff))

	// Bookmarks
	mux.HandleFunc("/api/bookmark/list", get(h.bookmarkList))
	mux.HandleFunc("/api/bookmark/add", post(h.bookmarkAdd))
	mux.HandleFunc("/api/bookmark/remove", post(h.bookmarkRemove))
	mux.HandleFunc("/api/bookmark/modify-all", post(h.bookmarkModifyAll))

	// Import
	mux.HandleFunc("/api/import/ct", post(h.importCT))

	// Session
	mux.HandleFunc("/api/session/save", post(h.sessionSave))
	mux.HandleFunc("/api/session/load", post(h.sessionLoad))

	srv := &http.Server{
		Addr:              addr,
		Handler:           secure(token, mux),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	fmt.Printf("Web UI: %s\n", DisplayURL(addr))
	if token != "" {
		fmt.Printf("Auth token required — open %s/?token=<token>\n", DisplayURL(addr))
	}
	return srv.ListenAndServe()
}

// get wraps a handler so it only responds to GET (and HEAD).
func get(fn http.HandlerFunc) http.HandlerFunc {
	return method(http.MethodGet, fn)
}

// post wraps a handler so it only responds to POST.
func post(fn http.HandlerFunc) http.HandlerFunc {
	return method(http.MethodPost, fn)
}

func method(verb string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed := r.Method == verb || (verb == http.MethodGet && r.Method == http.MethodHead)
		if !allowed {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		fn(w, r)
	}
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

		if q := r.URL.Query().Get("token"); q != "" {
			http.SetCookie(w, &http.Cookie{Name: "mdtoken", Value: q, Path: "/", HttpOnly: true})
		}

		protected := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/")
		if protected && !tokenOK(r, token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tokenOK(r *http.Request, token string) bool {
	if r.URL.Query().Get("token") == token {
		return true
	}
	if c, err := r.Cookie("mdtoken"); err == nil && c.Value == token {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.TrimPrefix(h, "Bearer ") == token {
		return true
	}
	return false
}

// DisplayURL renders a browser-friendly URL from a listen address, turning a
// bare ":8080" or "0.0.0.0:8080" into a localhost URL.
func DisplayURL(addr string) string {
	host, port, found := strings.Cut(addr, ":")
	if !found {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

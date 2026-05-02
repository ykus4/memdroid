package server

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"golang.org/x/net/websocket"

	"memodroid/internal/app"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/watch"
	"memodroid/internal/server/wswatch"
)

//go:embed static
var staticFiles embed.FS

// Start launches the HTTP server on addr (e.g. ":8080") in the foreground.
// Call it in a goroutine from main.
func Start(addr string, state *app.State, d *adb.ADB) error {
	// Wire watch events to WebSocket broadcast.
	watch.BroadcastFunc = func(a uintptr, prev, cur string) {
		wswatch.Broadcast(wswatch.Event{
			Addr: fmt.Sprintf("0x%x", a),
			Prev: prev,
			Cur:  cur,
		})
	}

	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("embed static: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	h := &handler{state: state, adb: d}

	// WebSocket for real-time watch events.
	mux.Handle("/ws/watch", websocket.Handler(func(ws *websocket.Conn) {
		wswatch.Register(ws)
	}))

	mux.HandleFunc("/api/status", h.status)

	// Device
	mux.HandleFunc("/api/device/list", h.deviceList)
	mux.HandleFunc("/api/device/select", h.deviceSelect)
	mux.HandleFunc("/api/device/connect-wifi", h.deviceConnectWifi)
	mux.HandleFunc("/api/device/disconnect-wifi", h.deviceDisconnectWifi)

	// Process
	mux.HandleFunc("/api/process/list", h.processList)
	mux.HandleFunc("/api/process/search", h.processSearch)
	mux.HandleFunc("/api/process/attach", h.processAttach)
	mux.HandleFunc("/api/process/detach", h.processDetach)
	mux.HandleFunc("/api/process/stop", h.processStop)
	mux.HandleFunc("/api/process/continue", h.processContinue)

	// Maps (region browser)
	mux.HandleFunc("/api/maps", h.mapsList)

	// Search
	mux.HandleFunc("/api/search/value", h.searchValue)
	mux.HandleFunc("/api/search/pattern", h.searchPattern)
	mux.HandleFunc("/api/search/string", h.searchString)
	mux.HandleFunc("/api/search/filter", h.searchFilter)
	mux.HandleFunc("/api/search/candidates", h.searchCandidates)
	mux.HandleFunc("/api/search/reset", h.searchReset)

	// Pointer scan
	mux.HandleFunc("/api/pointer/scan", h.pointerScan)

	// Memory
	mux.HandleFunc("/api/memory/modify", h.memoryModify)
	mux.HandleFunc("/api/memory/undo", h.memoryUndo)
	mux.HandleFunc("/api/memory/freeze", h.memoryFreeze)
	mux.HandleFunc("/api/memory/freeze-all", h.memoryFreezeAll)
	mux.HandleFunc("/api/memory/unfreeze", h.memoryUnfreeze)
	mux.HandleFunc("/api/memory/frozen", h.memoryFrozen)

	// Bookmarks
	mux.HandleFunc("/api/bookmark/list", h.bookmarkList)
	mux.HandleFunc("/api/bookmark/add", h.bookmarkAdd)
	mux.HandleFunc("/api/bookmark/remove", h.bookmarkRemove)
	mux.HandleFunc("/api/bookmark/modify-all", h.bookmarkModifyAll)

	// Session
	mux.HandleFunc("/api/session/save", h.sessionSave)
	mux.HandleFunc("/api/session/load", h.sessionLoad)

	fmt.Printf("Web UI: http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

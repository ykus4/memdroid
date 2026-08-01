package server

import "net/http"

// route binds a path to a handler and the single HTTP method it accepts.
type route struct {
	method  string
	path    string
	handler http.HandlerFunc
}

// routes returns the full API surface. Keeping it as data rather than a run of
// mux.HandleFunc calls makes the surface reviewable at a glance and lets tests
// assert on it.
func (h *handler) routes() []route {
	get := func(path string, fn http.HandlerFunc) route {
		return route{http.MethodGet, path, fn}
	}
	post := func(path string, fn http.HandlerFunc) route {
		return route{http.MethodPost, path, fn}
	}

	return []route{
		get("/api/status", h.status),

		// Device
		get("/api/device/list", h.deviceList),
		post("/api/device/select", h.deviceSelect),
		post("/api/device/connect-wifi", h.deviceConnectWifi),
		post("/api/device/disconnect-wifi", h.deviceDisconnectWifi),

		// Process
		get("/api/process/list", h.processList),
		get("/api/process/attached", h.processAttached),
		post("/api/process/search", h.processSearch),
		post("/api/process/attach", h.processAttach),
		post("/api/process/detach", h.processDetach),
		post("/api/process/switch", h.processSwitch),
		post("/api/process/stop", h.processStop),
		post("/api/process/continue", h.processContinue),

		// Maps
		get("/api/maps", h.mapsList),

		// Search
		get("/api/search/types", h.searchTypes),
		post("/api/search/type", h.searchSetType),
		post("/api/search/value", h.searchValue),
		post("/api/search/pattern", h.searchPattern),
		post("/api/search/string", h.searchString),
		post("/api/search/filter", h.searchFilter),
		get("/api/search/candidates", h.searchCandidates),
		post("/api/search/reset", h.searchReset),

		// Pointer
		post("/api/pointer/scan", h.pointerScan),
		post("/api/pointer/resolve", h.pointerResolve),

		// Memory
		post("/api/memory/modify", h.memoryModify),
		post("/api/memory/write-string", h.memoryWriteString),
		post("/api/memory/undo", h.memoryUndo),
		post("/api/memory/freeze", h.memoryFreeze),
		post("/api/memory/freeze-interval", h.freezeSetInterval),
		post("/api/memory/freeze-all", h.memoryFreezeAll),
		post("/api/memory/unfreeze", h.memoryUnfreeze),
		get("/api/memory/frozen", h.memoryFrozen),
		get("/api/memory/hexdump", h.memoryHexdump),
		post("/api/memory/dump", h.memoryDump),

		// Watch
		post("/api/watch/add", h.watchAdd),
		post("/api/watch/remove", h.watchRemove),
		get("/api/watch/list", h.watchList),

		// Alert
		post("/api/alert/add", h.alertAdd),
		post("/api/alert/remove", h.alertRemove),
		get("/api/alert/list", h.alertList),

		// Snapshot
		post("/api/snapshot/take", h.snapshotTake),
		post("/api/snapshot/diff", h.snapshotDiff),

		// Bookmarks
		get("/api/bookmark/list", h.bookmarkList),
		post("/api/bookmark/add", h.bookmarkAdd),
		post("/api/bookmark/remove", h.bookmarkRemove),
		post("/api/bookmark/modify-all", h.bookmarkModifyAll),

		// Import
		post("/api/import/ct", h.importCT),

		// Session
		post("/api/session/save", h.sessionSave),
		post("/api/session/load", h.sessionLoad),
	}
}

// only rejects any method other than verb (GET handlers also accept HEAD).
func only(verb string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(verb, r.Method) {
			w.Header().Set("Allow", allowHeader(verb))
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		fn(w, r)
	}
}

// methodAllowed reports whether got satisfies a handler declared for verb.
func methodAllowed(verb, got string) bool {
	if got == verb {
		return true
	}
	return verb == http.MethodGet && got == http.MethodHead
}

// allowHeader renders the full method set a verb accepts, so a 405 does not
// advertise less than the route really serves.
func allowHeader(verb string) string {
	if verb == http.MethodGet {
		return "GET, HEAD"
	}
	return verb
}

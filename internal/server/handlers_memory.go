package server

import (
	"net/http"

	"memdroid/internal/memory/modify"
)

const (
	// maxHexdumpBytes bounds a single hex view request.
	maxHexdumpBytes = 4096
	// maxSnapshotBytes bounds a single snapshot/diff region.
	maxSnapshotBytes = 16 << 20
	// maxDumpBytes bounds a region dumped to a file on the host.
	maxDumpBytes = 256 << 20
)

func (h *handler) memoryModify(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	val, ok := requireValue(w, req.Value, h.state.GetValueType())
	if !ok {
		return
	}
	if err := h.state.UndoStack.WithUndo(h.state.GetDriver(), pid, addr, val); err != nil {
		serverError(w, "modify", err)
		return
	}
	writeOK(w, map[string]any{"addr": hexAddr(addr), "undo_depth": h.state.UndoStack.Depth()})
}

// memoryWriteString overwrites a string in place with raw UTF-8 bytes.
func (h *handler) memoryWriteString(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value required")
		return
	}
	if err := modify.WriteString(h.state.GetDriver(), pid, addr, req.Value); err != nil {
		serverError(w, "write string", err)
		return
	}
	writeOK(w, map[string]any{"addr": hexAddr(addr), "bytes": len(req.Value)})
}

func (h *handler) memoryUndo(w http.ResponseWriter, _ *http.Request) {
	if err := h.state.UndoStack.Undo(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, map[string]any{"undo_depth": h.state.UndoStack.Depth()})
}

func (h *handler) memoryFreeze(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	val, ok := requireValue(w, req.Value, h.state.GetValueType())
	if !ok {
		return
	}
	if err := h.state.Freezer.Freeze(h.state.GetDriver(), pid, addr, val); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, map[string]any{"addr": hexAddr(addr)})
}

func (h *handler) freezeSetInterval(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		IntervalMs int `json:"interval_ms"`
	}](w, r)
	if !ok {
		return
	}
	if req.IntervalMs <= 0 {
		writeError(w, http.StatusBadRequest, "interval_ms must be positive")
		return
	}
	interval, ok := requireDuration(w, req.IntervalMs, 0)
	if !ok {
		return
	}
	h.state.Freezer.SetInterval(interval)
	writeOK(w, map[string]any{"interval_ms": req.IntervalMs})
}

func (h *handler) memoryFreezeAll(w http.ResponseWriter, _ *http.Request) {
	sess, ok := h.requireSession(w)
	if !ok {
		return
	}
	count := h.state.Freezer.FreezeAllCandidates(h.state.GetDriver(), sess)
	writeOK(w, map[string]any{"count": count})
}

func (h *handler) memoryUnfreeze(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Addr string `json:"addr"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	if err := h.state.Freezer.Unfreeze(addr); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *handler) memoryFrozen(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, hexAddrs(h.state.Freezer.List()))
}

func (h *handler) memoryHexdump(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, r.URL.Query().Get("addr"))
	if !ok {
		return
	}
	size := queryInt(r, "size", 0, 1)
	if !requireSize(w, size, maxHexdumpBytes) {
		return
	}
	data, err := h.state.GetDriver().ReadRegion(pid, addr, size)
	if err != nil {
		serverError(w, "read region", err)
		return
	}

	type line struct {
		Offset int     `json:"offset"`
		Addr   hexAddr `json:"addr"`
		Hex    string  `json:"hex"`
		ASCII  string  `json:"ascii"`
	}
	src := modify.HexLines(addr, data)
	lines := make([]line, len(src))
	for i, l := range src {
		lines[i] = line{Offset: l.Offset, Addr: hexAddr(l.Addr), Hex: l.Hex, ASCII: l.ASCII}
	}
	writeJSON(w, map[string]any{"addr": hexAddr(addr), "size": len(data), "hex_lines": lines})
}

// memoryDump writes a region to a file on the host running memdroid.
func (h *handler) memoryDump(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr string `json:"addr"`
		Size int    `json:"size"`
		Path string `json:"path"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	if !requireSize(w, req.Size, maxDumpBytes) {
		return
	}
	path, ok := h.resolvePath(w, req.Path, "dump.hex")
	if !ok {
		return
	}
	if err := modify.DumpRegion(h.state.GetDriver(), pid, addr, req.Size, path); err != nil {
		serverError(w, "dump", err)
		return
	}
	writeOK(w, map[string]any{"path": path, "size": req.Size})
}

func (h *handler) snapshotTake(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	addr, size, ok := h.decodeRegionRequest(w, r)
	if !ok {
		return
	}
	data, err := h.state.GetDriver().ReadRegion(pid, addr, size)
	if err != nil {
		serverError(w, "read region", err)
		return
	}
	h.state.SetSnapshot(addr, data)
	writeOK(w, map[string]any{"addr": hexAddr(addr), "size": len(data)})
}

func (h *handler) snapshotDiff(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	addr, size, ok := h.decodeRegionRequest(w, r)
	if !ok {
		return
	}
	prev := h.state.GetSnapshot(addr)
	if prev == nil {
		writeError(w, http.StatusBadRequest, "no snapshot taken for this address — call /api/snapshot/take first")
		return
	}
	cur, err := h.state.GetDriver().ReadRegion(pid, addr, size)
	if err != nil {
		serverError(w, "read region", err)
		return
	}

	diffs, err := modify.DiffSnapshots(
		&modify.Snapshot{Addr: addr, Data: prev},
		&modify.Snapshot{Addr: addr, Data: cur},
	)
	if err != nil {
		serverError(w, "diff", err)
		return
	}

	type entry struct {
		Addr   hexAddr `json:"addr"`
		Offset int     `json:"offset"`
		Before int     `json:"before"`
		After  int     `json:"after"`
	}
	out := make([]entry, len(diffs))
	for i, d := range diffs {
		out[i] = entry{Addr: hexAddr(d.Addr), Offset: d.Offset, Before: int(d.Before), After: int(d.After)}
	}
	writeJSON(w, map[string]any{"total": len(out), "diffs": out})
}

// decodeRegionRequest reads the {addr, size} body shared by the snapshot
// endpoints.
func (h *handler) decodeRegionRequest(w http.ResponseWriter, r *http.Request) (uintptr, int, bool) {
	req, ok := decodeJSON[struct {
		Addr string `json:"addr"`
		Size int    `json:"size"`
	}](w, r)
	if !ok {
		return 0, 0, false
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return 0, 0, false
	}
	if !requireSize(w, req.Size, maxSnapshotBytes) {
		return 0, 0, false
	}
	return addr, req.Size, true
}

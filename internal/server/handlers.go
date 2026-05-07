package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"memodroid/internal/app"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/pointer"
	"memodroid/internal/memory/search"
	"memodroid/internal/memory/store"
	"memodroid/internal/process"
)

type handler struct {
	state *app.State
	adb   *adb.ADB
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func parseHexAddr(s string) (uintptr, error) {
	v, err := strconv.ParseUint(s, 0, 64)
	return uintptr(v), err
}

func requirePID(w http.ResponseWriter, h *handler) (int, bool) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
		return 0, false
	}
	return pid, true
}

// --- status ---

func (h *handler) status(w http.ResponseWriter, _ *http.Request) {
	pid := h.state.GetPID()
	vt := h.state.GetValueType()
	sess := h.state.GetSession()

	candidates := 0
	if sess != nil {
		candidates = sess.CandidateCount()
	}

	writeJSON(w, map[string]any{
		"pid":        pid,
		"attached":   pid != 0,
		"value_type": vt.String(),
		"candidates": candidates,
		"undo_depth": h.state.UndoStack.Depth(),
		"frozen":     h.state.Freezer.List(),
		"device":     h.adb.DeviceSerial(),
	})
}

// --- device ---

func (h *handler) deviceList(w http.ResponseWriter, _ *http.Request) {
	devices, err := h.adb.ListDevices()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type entry struct {
		Serial string `json:"serial"`
	}
	out := make([]entry, len(devices))
	for i, s := range devices {
		out[i] = entry{Serial: s}
	}
	writeJSON(w, out)
}

func (h *handler) deviceSelect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Serial string `json:"serial"`
	}
	if err := decode(r, &req); err != nil || req.Serial == "" {
		writeError(w, 400, "serial required")
		return
	}
	if err := h.adb.SelectDevice(req.Serial); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "serial": req.Serial})
}

func (h *handler) deviceConnectWifi(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr string `json:"addr"`
	}
	if err := decode(r, &req); err != nil || req.Addr == "" {
		writeError(w, 400, "addr required (host:port)")
		return
	}
	if err := h.adb.ConnectWifi(req.Addr); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "serial": req.Addr})
}

func (h *handler) deviceDisconnectWifi(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr string `json:"addr"`
	}
	if err := decode(r, &req); err != nil || req.Addr == "" {
		writeError(w, 400, "addr required")
		return
	}
	if err := h.adb.DisconnectWifi(req.Addr); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- process ---

func (h *handler) processList(w http.ResponseWriter, _ *http.Request) {
	procs, err := process.ProcessList(h.state.GetDriver())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, procs)
}

func (h *handler) processSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil || req.Name == "" {
		writeError(w, 400, "name required")
		return
	}
	matches, err := h.adb.FindProcessByName(req.Name)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type entry struct {
		PID  int    `json:"pid"`
		Name string `json:"name"`
	}
	out := make([]entry, len(matches))
	for i, p := range matches {
		out[i] = entry{PID: p.PID, Name: p.Name}
	}
	writeJSON(w, out)
}

func (h *handler) processAttach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PID int `json:"pid"`
	}
	if err := decode(r, &req); err != nil || req.PID == 0 {
		writeError(w, 400, "invalid pid")
		return
	}
	drv := h.state.GetDriver()
	if err := drv.Attach(req.PID); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	h.state.SetPID(req.PID)
	h.state.SetSession(search.NewSession(req.PID, h.state.GetValueType(), drv))
	writeJSON(w, map[string]any{"ok": true, "pid": req.PID})
}

func (h *handler) processDetach(w http.ResponseWriter, _ *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	h.state.Freezer.UnfreezeAll()
	h.state.GetDriver().Detach(pid)
	h.state.SetPID(0)
	h.state.SetSession(nil)
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) processStop(w http.ResponseWriter, _ *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	if err := h.state.GetDriver().Stop(pid); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) processContinue(w http.ResponseWriter, _ *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	if err := h.state.GetDriver().Continue(pid); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- maps ---

func (h *handler) mapsList(w http.ResponseWriter, _ *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	regions, err := h.state.GetDriver().ReadMaps(pid)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type entry struct {
		Start string `json:"start"`
		End   string `json:"end"`
		Size  int    `json:"size"`
		Name  string `json:"name"`
	}
	out := make([]entry, len(regions))
	for i, r := range regions {
		out[i] = entry{
			Start: fmt.Sprintf("0x%x", r.Start),
			End:   fmt.Sprintf("0x%x", r.End),
			Size:  int(r.End - r.Start),
			Name:  r.Name,
		}
	}
	writeJSON(w, out)
}

// --- search ---

func (h *handler) searchValue(w http.ResponseWriter, r *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	_ = pid
	var req struct {
		Value string `json:"value"`
		Type  string `json:"type"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	vt := h.state.GetValueType()
	if req.Type != "" {
		if t, err := search.ParseValueType(req.Type); err == nil {
			vt = t
			h.state.SetValueType(vt)
		}
	}
	val, err := search.ParseValue(req.Value, vt)
	if err != nil {
		writeError(w, 400, fmt.Sprintf("invalid value: %v", err))
		return
	}
	sess := h.state.EnsureSession()
	if err := sess.Search(val); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"candidates": sess.CandidateCount()})
}

func (h *handler) searchPattern(w http.ResponseWriter, r *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	var req struct {
		Pattern string `json:"pattern"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	pat, err := search.ParsePattern(req.Pattern)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	results, err := search.SearchPattern(h.state.GetDriver(), pid, pat)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"count": len(results)})
}

func (h *handler) searchString(w http.ResponseWriter, r *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	var req struct {
		Value    string `json:"value"`
		Encoding string `json:"encoding"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	drv := h.state.GetDriver()
	var results []uintptr
	var err error
	if req.Encoding == "utf16" {
		results, err = search.SearchStringUTF16(drv, pid, req.Value)
	} else {
		results, err = search.SearchStringUTF8(drv, pid, req.Value)
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"count": len(results)})
}

func (h *handler) searchFilter(w http.ResponseWriter, r *http.Request) {
	sess := h.state.GetSession()
	if sess == nil || !sess.HasCandidates() {
		writeError(w, 400, "no active session")
		return
	}
	var req struct {
		Mode  string `json:"mode"`
		Value string `json:"value"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	mode, err := search.ParseFilterMode(req.Mode)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	var target []byte
	if mode == search.FilterValue {
		target, err = search.ParseValue(req.Value, h.state.GetValueType())
		if err != nil {
			writeError(w, 400, fmt.Sprintf("invalid value: %v", err))
			return
		}
	}
	if err := sess.Filter(mode, target); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"candidates": sess.CandidateCount()})
}

func (h *handler) searchCandidates(w http.ResponseWriter, r *http.Request) {
	sess := h.state.GetSession()
	vt := h.state.GetValueType()
	if sess == nil {
		writeJSON(w, map[string]any{"total": 0, "page": 0, "page_size": 100, "items": []any{}})
		return
	}

	pageSize := 100
	page := 0
	if s := r.URL.Query().Get("page_size"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			pageSize = v
		}
	}
	if s := r.URL.Query().Get("page"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 {
			page = v
		}
	}

	snap := sess.Snapshot()
	addrs := make([]uintptr, 0, len(snap))
	for addr := range snap {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i] < addrs[j] })

	total := len(addrs)
	start := page * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	type entry struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}
	items := make([]entry, 0, end-start)
	for _, addr := range addrs[start:end] {
		items = append(items, entry{
			Addr:  fmt.Sprintf("0x%x", addr),
			Value: search.FormatValue(snap[addr], vt),
		})
	}

	writeJSON(w, map[string]any{
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"items":     items,
	})
}

func (h *handler) searchReset(w http.ResponseWriter, _ *http.Request) {
	if sess := h.state.GetSession(); sess != nil {
		sess.Reset()
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- pointer scan ---

func (h *handler) pointerScan(w http.ResponseWriter, r *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	var req struct {
		Addr      string `json:"addr"`
		MaxDepth  int    `json:"max_depth"`
		MaxOffset int    `json:"max_offset"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	addr, err := parseHexAddr(req.Addr)
	if err != nil {
		writeError(w, 400, "invalid addr")
		return
	}
	result, err := pointer.Scan(h.state.GetDriver(), pid, addr, req.MaxDepth, uintptr(req.MaxOffset))
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	type chainJSON struct {
		Base    string  `json:"base"`
		Label   string  `json:"label"`
		Offsets []int64 `json:"offsets"`
		Path    string  `json:"path"`
	}
	out := make([]chainJSON, len(result.Chains))
	for i, c := range result.Chains {
		out[i] = chainJSON{
			Base:    fmt.Sprintf("0x%x", c.BaseAddr),
			Label:   c.BaseLabel,
			Offsets: c.Offsets,
			Path:    pointer.FormatChain(c),
		}
	}
	writeJSON(w, map[string]any{"target": fmt.Sprintf("0x%x", addr), "chains": out})
}

// --- memory ---

func (h *handler) memoryModify(w http.ResponseWriter, r *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	var req struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	addr, err := parseHexAddr(req.Addr)
	if err != nil {
		writeError(w, 400, "invalid addr")
		return
	}
	vt := h.state.GetValueType()
	val, err := search.ParseValue(req.Value, vt)
	if err != nil {
		writeError(w, 400, fmt.Sprintf("invalid value: %v", err))
		return
	}
	if err := h.state.UndoStack.WithUndo(h.state.GetDriver(), pid, addr, val, vt); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) memoryUndo(w http.ResponseWriter, _ *http.Request) {
	if err := h.state.UndoStack.Undo(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "undo_depth": h.state.UndoStack.Depth()})
}

func (h *handler) memoryFreeze(w http.ResponseWriter, r *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	var req struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	addr, err := parseHexAddr(req.Addr)
	if err != nil {
		writeError(w, 400, "invalid addr")
		return
	}
	val, err := search.ParseValue(req.Value, h.state.GetValueType())
	if err != nil {
		writeError(w, 400, fmt.Sprintf("invalid value: %v", err))
		return
	}
	if err := h.state.Freezer.Freeze(h.state.GetDriver(), pid, addr, val); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) freezeSetInterval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IntervalMs int `json:"interval_ms"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.IntervalMs <= 0 {
		writeError(w, 400, "interval_ms must be positive")
		return
	}
	h.state.Freezer.SetInterval(time.Duration(req.IntervalMs) * time.Millisecond)
	writeJSON(w, map[string]any{"ok": true, "interval_ms": req.IntervalMs})
}

func (h *handler) memoryFreezeAll(w http.ResponseWriter, _ *http.Request) {
	sess := h.state.GetSession()
	if sess == nil || !sess.HasCandidates() {
		writeError(w, 400, "no candidates")
		return
	}
	count := h.state.Freezer.FreezeAllCandidates(h.state.GetDriver(), sess)
	writeJSON(w, map[string]any{"ok": true, "count": count})
}

func (h *handler) memoryUnfreeze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr string `json:"addr"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	addr, err := parseHexAddr(req.Addr)
	if err != nil {
		writeError(w, 400, "invalid addr")
		return
	}
	if err := h.state.Freezer.Unfreeze(addr); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) memoryFrozen(w http.ResponseWriter, _ *http.Request) {
	addrs := h.state.Freezer.List()
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = fmt.Sprintf("0x%x", a)
	}
	writeJSON(w, out)
}

// --- bookmarks ---

func (h *handler) bookmarkList(w http.ResponseWriter, _ *http.Request) {
	pid := h.state.GetPID()
	drv := h.state.GetDriver()
	bl := h.state.GetBookmarks()
	vals := bl.Values(drv, pid)

	type entry struct {
		Index int    `json:"index"`
		Addr  string `json:"addr"`
		Label string `json:"label"`
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	out := make([]entry, len(bl.Entries))
	for i, b := range bl.Entries {
		out[i] = entry{
			Index: i,
			Addr:  fmt.Sprintf("0x%x", b.Addr),
			Label: b.Label,
			Type:  b.VType.String(),
			Value: vals[b.Addr],
		}
	}
	writeJSON(w, out)
}

func (h *handler) bookmarkAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr  string `json:"addr"`
		Label string `json:"label"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	addr, err := parseHexAddr(req.Addr)
	if err != nil {
		writeError(w, 400, "invalid addr")
		return
	}
	h.state.GetBookmarks().Add(addr, req.Label, h.state.GetValueType())
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) bookmarkRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Index int `json:"index"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := h.state.GetBookmarks().Remove(req.Index); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) bookmarkModifyAll(w http.ResponseWriter, r *http.Request) {
	pid, ok := requirePID(w, h)
	if !ok {
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	vt := h.state.GetValueType()
	val, err := search.ParseValue(req.Value, vt)
	if err != nil {
		writeError(w, 400, fmt.Sprintf("invalid value: %v", err))
		return
	}
	count := h.state.GetBookmarks().ModifyAll(h.state.GetDriver(), pid, val, vt)
	writeJSON(w, map[string]any{"ok": true, "count": count})
}

// --- session ---

func (h *handler) sessionSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil || req.Path == "" {
		req.Path = "memdroid.json"
	}
	if err := store.SaveState(req.Path, h.state.GetBookmarks(), h.state.GetSession()); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": req.Path})
}

func (h *handler) sessionLoad(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil || req.Path == "" {
		req.Path = "memdroid.json"
	}
	sess := h.state.GetSession()
	bl := h.state.GetBookmarks()
	if err := store.LoadState(req.Path, bl, &sess); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if sess != nil {
		sess.Driver = h.state.GetDriver()
	}
	h.state.SetSession(sess)
	writeJSON(w, map[string]any{"ok": true, "path": req.Path})
}

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"memdroid/internal/app"
	"memdroid/internal/driver"
	"memdroid/internal/driver/adb"
	"memdroid/internal/memory/pointer"
	"memdroid/internal/memory/search"
	"memdroid/internal/memory/store"
	"memdroid/internal/process"
)

const defaultStateFile = "memdroid.json"

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

func writeOK(w http.ResponseWriter, extra map[string]any) {
	out := map[string]any{"ok": true}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, out)
}

// decodeJSON decodes the request body into a value of type T. On failure it
// writes a 400 response and returns ok=false.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return v, false
	}
	return v, true
}

func parseHexAddr(s string) (uintptr, error) {
	v, err := strconv.ParseUint(s, 0, 64)
	return uintptr(v), err
}

// requireAddr parses a hex address, writing a 400 on failure.
func requireAddr(w http.ResponseWriter, s string) (uintptr, bool) {
	addr, err := parseHexAddr(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid addr")
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

func (h *handler) requirePID(w http.ResponseWriter) (int, bool) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, http.StatusBadRequest, "not attached")
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
	req, ok := decodeJSON[struct {
		Serial string `json:"serial"`
	}](w, r)
	if !ok {
		return
	}
	if req.Serial == "" {
		writeError(w, http.StatusBadRequest, "serial required")
		return
	}
	if err := h.adb.SelectDevice(req.Serial); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, map[string]any{"serial": req.Serial})
}

func (h *handler) deviceConnectWifi(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Addr string `json:"addr"`
	}](w, r)
	if !ok {
		return
	}
	if req.Addr == "" {
		writeError(w, http.StatusBadRequest, "addr required (host:port)")
		return
	}
	if err := h.adb.ConnectWifi(req.Addr); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, map[string]any{"serial": req.Addr})
}

func (h *handler) deviceDisconnectWifi(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Addr string `json:"addr"`
	}](w, r)
	if !ok {
		return
	}
	if req.Addr == "" {
		writeError(w, http.StatusBadRequest, "addr required")
		return
	}
	if err := h.adb.DisconnectWifi(req.Addr); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

// --- process ---

func (h *handler) processList(w http.ResponseWriter, _ *http.Request) {
	procs, err := process.ProcessList(h.state.GetDriver())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, procs)
}

func (h *handler) processSearch(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	matches, err := h.adb.FindProcessByName(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	req, ok := decodeJSON[struct {
		PID int `json:"pid"`
	}](w, r)
	if !ok {
		return
	}
	if req.PID == 0 {
		writeError(w, http.StatusBadRequest, "invalid pid")
		return
	}
	drv := h.state.GetDriver()
	if err := drv.Attach(req.PID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.state.SetPID(req.PID)
	h.state.SetSession(search.NewSession(req.PID, h.state.GetValueType(), drv))
	writeOK(w, map[string]any{"pid": req.PID})
}

func (h *handler) processDetach(w http.ResponseWriter, _ *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	h.state.Freezer.UnfreezeAll()
	h.state.Watcher.UnwatchAll()
	h.state.AlertWatcher.RemoveAll()
	h.state.GetDriver().Detach(pid)
	h.state.SetPID(0)
	h.state.SetSession(nil)
	writeOK(w, nil)
}

func (h *handler) processStop(w http.ResponseWriter, _ *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	if err := h.state.GetDriver().Stop(pid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *handler) processContinue(w http.ResponseWriter, _ *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	if err := h.state.GetDriver().Continue(pid); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
}

// --- maps ---

func (h *handler) mapsList(w http.ResponseWriter, _ *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	regions, err := h.state.GetDriver().ReadMaps(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
	if _, ok := h.requirePID(w); !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Value string `json:"value"`
		Type  string `json:"type"`
	}](w, r)
	if !ok {
		return
	}
	vt := h.state.GetValueType()
	if req.Type != "" {
		if t, err := search.ParseValueType(req.Type); err == nil {
			vt = t
			h.state.SetValueType(vt)
		}
	}
	val, ok := requireValue(w, req.Value, vt)
	if !ok {
		return
	}
	sess := h.state.EnsureSession()
	if err := sess.Search(val); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"candidates": sess.CandidateCount()})
}

func (h *handler) searchPattern(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Pattern string `json:"pattern"`
	}](w, r)
	if !ok {
		return
	}
	pat, err := search.ParsePattern(req.Pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	drv := h.state.GetDriver()
	results, err := search.SearchPattern(drv, pid, pat)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cands := h.candidatesFromAddrs(drv, pid, results, len(pat))
	h.storeByteCandidates(cands)
	writeJSON(w, map[string]any{
		"count":      len(results),
		"candidates": len(cands),
		"truncated":  len(results) >= search.PatternMaxResults,
	})
}

func (h *handler) searchString(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Value    string `json:"value"`
		Encoding string `json:"encoding"`
	}](w, r)
	if !ok {
		return
	}
	drv := h.state.GetDriver()
	utf16 := req.Encoding == "utf16"
	var results []uintptr
	var err error
	if utf16 {
		results, err = search.SearchStringUTF16(drv, pid, req.Value)
	} else {
		results, err = search.SearchStringUTF8(drv, pid, req.Value)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	valBytes := search.StringBytes(req.Value, utf16)
	cands := make(map[uintptr][]byte, len(results))
	for _, a := range results {
		b := make([]byte, len(valBytes))
		copy(b, valBytes)
		cands[a] = b
	}
	h.storeByteCandidates(cands)
	writeJSON(w, map[string]any{
		"count":      len(results),
		"candidates": len(cands),
		"truncated":  len(results) >= search.PatternMaxResults,
	})
}

// candidatesFromAddrs reads width bytes at each address to record the matched
// value alongside its address.
func (h *handler) candidatesFromAddrs(drv driver.Driver, pid int, addrs []uintptr, width int) map[uintptr][]byte {
	cands := make(map[uintptr][]byte, len(addrs))
	for _, a := range addrs {
		if b, err := drv.Peek(pid, a, width); err == nil {
			cands[a] = b
		}
	}
	return cands
}

// storeByteCandidates switches the active session/value type to bytes and loads
// the given candidates so they can be paged, filtered and frozen.
func (h *handler) storeByteCandidates(cands map[uintptr][]byte) {
	h.state.SetValueType(search.TypeBytes)
	sess := h.state.EnsureSession()
	sess.ValueType = search.TypeBytes
	sess.SetCandidates(cands)
}

func (h *handler) searchFilter(w http.ResponseWriter, r *http.Request) {
	sess := h.state.GetSession()
	if sess == nil || !sess.HasCandidates() {
		writeError(w, http.StatusBadRequest, "no active session")
		return
	}
	req, ok := decodeJSON[struct {
		Mode  string `json:"mode"`
		Value string `json:"value"`
	}](w, r)
	if !ok {
		return
	}
	mode, err := search.ParseFilterMode(req.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var target []byte
	if mode == search.FilterValue {
		target, ok = requireValue(w, req.Value, h.state.GetValueType())
		if !ok {
			return
		}
	}
	if err := sess.Filter(mode, target); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	pageSize := queryInt(r, "page_size", 100, 1)
	page := queryInt(r, "page", 0, 0)

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

// queryInt parses a query parameter as an int, returning def if absent/invalid
// or below min.
func queryInt(r *http.Request, key string, def, min int) int {
	if s := r.URL.Query().Get(key); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= min {
			return v
		}
	}
	return def
}

func (h *handler) searchReset(w http.ResponseWriter, _ *http.Request) {
	if sess := h.state.GetSession(); sess != nil {
		sess.Reset()
	}
	writeOK(w, nil)
}

// --- pointer scan ---

func (h *handler) pointerScan(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr      string `json:"addr"`
		MaxDepth  int    `json:"max_depth"`
		MaxOffset int    `json:"max_offset"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	result, err := pointer.Scan(h.state.GetDriver(), pid, addr, req.MaxDepth, uintptr(req.MaxOffset))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type chainJSON struct {
		Base       string  `json:"base"`
		Label      string  `json:"label"`
		BaseOffset string  `json:"base_offset"`
		Offsets    []int64 `json:"offsets"`
		Path       string  `json:"path"`
	}
	out := make([]chainJSON, len(result.Chains))
	for i, c := range result.Chains {
		out[i] = chainJSON{
			Base:       fmt.Sprintf("0x%x", c.BaseAddr),
			Label:      c.BaseLabel,
			BaseOffset: fmt.Sprintf("0x%x", c.BaseOffset),
			Offsets:    c.Offsets,
			Path:       pointer.FormatChain(c),
		}
	}
	writeJSON(w, map[string]any{"target": fmt.Sprintf("0x%x", addr), "chains": out})
}

// --- pointer resolve ---

func (h *handler) pointerResolve(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requirePID(w); !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Label      string  `json:"label"`
		BaseOffset string  `json:"base_offset"`
		Offsets    []int64 `json:"offsets"`
	}](w, r)
	if !ok {
		return
	}
	if req.Label == "" || len(req.Offsets) == 0 {
		writeError(w, http.StatusBadRequest, "label and offsets required")
		return
	}
	var baseOffset uintptr
	if req.BaseOffset != "" {
		baseOffset, ok = requireAddr(w, req.BaseOffset)
		if !ok {
			return
		}
	}
	chain := pointer.Chain{
		BaseLabel:  req.Label,
		BaseOffset: baseOffset,
		Offsets:    req.Offsets,
	}
	resolved, err := pointer.ResolveChain(h.state.GetDriver(), h.state.GetPID(), chain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"resolved":    fmt.Sprintf("0x%x", resolved),
		"label":       req.Label,
		"base_offset": req.BaseOffset,
		"offsets":     req.Offsets,
	})
}

// --- memory ---

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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, nil)
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
	writeOK(w, nil)
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
	h.state.Freezer.SetInterval(time.Duration(req.IntervalMs) * time.Millisecond)
	writeOK(w, map[string]any{"interval_ms": req.IntervalMs})
}

func (h *handler) memoryFreezeAll(w http.ResponseWriter, _ *http.Request) {
	sess := h.state.GetSession()
	if sess == nil || !sess.HasCandidates() {
		writeError(w, http.StatusBadRequest, "no candidates")
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
	addrs := h.state.Freezer.List()
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = fmt.Sprintf("0x%x", a)
	}
	writeJSON(w, out)
}

// --- hexdump ---

func (h *handler) memoryHexdump(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	addrStr := r.URL.Query().Get("addr")
	sizeStr := r.URL.Query().Get("size")
	if addrStr == "" || sizeStr == "" {
		writeError(w, http.StatusBadRequest, "addr and size required")
		return
	}
	addr, ok := requireAddr(w, addrStr)
	if !ok {
		return
	}
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size <= 0 || size > 4096 {
		writeError(w, http.StatusBadRequest, "size must be 1-4096")
		return
	}
	data, err := h.state.GetDriver().ReadRegion(pid, addr, size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type hexLine struct {
		Offset int    `json:"offset"`
		Hex    string `json:"hex"`
		ASCII  string `json:"ascii"`
	}
	var lines []hexLine
	for i := 0; i < len(data); i += 16 {
		end := i + 16
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]

		hexParts := make([]string, len(chunk))
		ascii := make([]byte, len(chunk))
		for j, b := range chunk {
			hexParts[j] = fmt.Sprintf("%02x", b)
			if b >= 0x20 && b <= 0x7e {
				ascii[j] = b
			} else {
				ascii[j] = '.'
			}
		}
		lines = append(lines, hexLine{
			Offset: i,
			Hex:    strings.Join(hexParts, " "),
			ASCII:  string(ascii),
		})
	}
	writeJSON(w, map[string]any{
		"addr":      fmt.Sprintf("0x%x", addr),
		"hex_lines": lines,
	})
}

// --- snapshot ---

func (h *handler) snapshotTake(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr string `json:"addr"`
		Size int    `json:"size"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	if req.Size <= 0 {
		writeError(w, http.StatusBadRequest, "size must be positive")
		return
	}
	data, err := h.state.GetDriver().ReadRegion(pid, addr, req.Size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.state.SetSnapshot(addr, data)
	writeOK(w, map[string]any{"addr": fmt.Sprintf("0x%x", addr), "size": len(data)})
}

func (h *handler) snapshotDiff(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr string `json:"addr"`
		Size int    `json:"size"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	if req.Size <= 0 {
		writeError(w, http.StatusBadRequest, "size must be positive")
		return
	}
	prev := h.state.GetSnapshot(addr)
	if prev == nil {
		writeError(w, http.StatusBadRequest, "no snapshot taken for this address — call /api/snapshot/take first")
		return
	}
	cur, err := h.state.GetDriver().ReadRegion(pid, addr, req.Size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	minLen := len(prev)
	if len(cur) < minLen {
		minLen = len(cur)
	}
	type diffEntry struct {
		Addr   string `json:"addr"`
		Offset int    `json:"offset"`
		Before int    `json:"before"`
		After  int    `json:"after"`
	}
	var diffs []diffEntry
	for i := 0; i < minLen; i++ {
		if prev[i] != cur[i] {
			diffs = append(diffs, diffEntry{
				Addr:   fmt.Sprintf("0x%x", addr+uintptr(i)),
				Offset: i,
				Before: int(prev[i]),
				After:  int(cur[i]),
			})
		}
	}
	writeJSON(w, map[string]any{"total": len(diffs), "diffs": diffs})
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
	req, ok := decodeJSON[struct {
		Addr  string `json:"addr"`
		Label string `json:"label"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	h.state.GetBookmarks().Add(addr, req.Label, h.state.GetValueType())
	writeOK(w, nil)
}

func (h *handler) bookmarkRemove(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Index int `json:"index"`
	}](w, r)
	if !ok {
		return
	}
	if err := h.state.GetBookmarks().Remove(req.Index); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *handler) bookmarkModifyAll(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Value string `json:"value"`
	}](w, r)
	if !ok {
		return
	}
	vt := h.state.GetValueType()
	val, ok := requireValue(w, req.Value, vt)
	if !ok {
		return
	}
	count := h.state.GetBookmarks().ModifyAll(h.state.GetDriver(), pid, val, vt)
	writeOK(w, map[string]any{"count": count})
}

// --- import ---

func (h *handler) importCT(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Path string `json:"path"`
	}](w, r)
	if !ok {
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	bookmarks, err := store.ImportCT(req.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bl := h.state.GetBookmarks()
	for _, b := range bookmarks {
		bl.Add(b.Addr, b.Label, b.VType)
	}
	writeOK(w, map[string]any{"imported": len(bookmarks)})
}

// --- session ---

func (h *handler) sessionSave(w http.ResponseWriter, r *http.Request) {
	req, _ := decodeJSON[struct {
		Path string `json:"path"`
	}](w, r)
	if req.Path == "" {
		req.Path = defaultStateFile
	}
	if err := store.SaveState(req.Path, h.state.GetBookmarks(), h.state.GetSession()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, map[string]any{"path": req.Path})
}

func (h *handler) sessionLoad(w http.ResponseWriter, r *http.Request) {
	req, _ := decodeJSON[struct {
		Path string `json:"path"`
	}](w, r)
	if req.Path == "" {
		req.Path = defaultStateFile
	}
	loaded, err := store.LoadState(req.Path, h.state.GetBookmarks())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if loaded != nil {
		loaded.Driver = h.state.GetDriver()
	}
	h.state.SetSession(loaded)
	writeOK(w, map[string]any{"path": req.Path})
}

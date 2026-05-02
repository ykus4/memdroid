package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"memodroid/internal/app"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/modify"
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
		"undo_depth": modify.UndoDepth(),
		"frozen":     modify.FrozenList(),
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
		Addr string `json:"addr"` // "host:port"
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
	if err := process.Attach(drv, req.PID); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	h.state.SetPID(req.PID)
	h.state.SetSession(search.NewSession(req.PID, h.state.GetValueType(), drv))
	writeJSON(w, map[string]any{"ok": true, "pid": req.PID})
}

func (h *handler) processDetach(w http.ResponseWriter, _ *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
		return
	}
	modify.UnfreezeAll()
	process.Detach(h.state.GetDriver(), pid)
	h.state.SetPID(0)
	h.state.SetSession(nil)
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) processStop(w http.ResponseWriter, _ *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
		return
	}
	if err := process.Stop(h.state.GetDriver(), pid); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) processContinue(w http.ResponseWriter, _ *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
		return
	}
	if err := process.Continue(h.state.GetDriver(), pid); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- maps (region browser) ---

func (h *handler) mapsList(w http.ResponseWriter, _ *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
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
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
		return
	}
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
		if t, err := parseValueType(req.Type); err == nil {
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
	sess.Search(val)
	writeJSON(w, map[string]any{"candidates": sess.CandidateCount()})
}

func (h *handler) searchPattern(w http.ResponseWriter, r *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
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
	search.SearchPattern(h.state.GetDriver(), pid, pat)
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) searchString(w http.ResponseWriter, r *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
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
	if req.Encoding == "utf16" {
		search.SearchStringUTF16(drv, pid, req.Value)
	} else {
		search.SearchStringUTF8(drv, pid, req.Value)
	}
	writeJSON(w, map[string]any{"ok": true})
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
	mode, err := parseFilterMode(req.Mode)
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
	sess.Filter(mode, target)
	writeJSON(w, map[string]any{"candidates": sess.CandidateCount()})
}

func (h *handler) searchCandidates(w http.ResponseWriter, _ *http.Request) {
	sess := h.state.GetSession()
	vt := h.state.GetValueType()
	if sess == nil {
		writeJSON(w, []any{})
		return
	}
	snap := sess.Snapshot()
	type entry struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}
	out := make([]entry, 0, len(snap))
	for addr, val := range snap {
		out = append(out, entry{
			Addr:  fmt.Sprintf("0x%x", addr),
			Value: search.FormatValue(val, vt),
		})
	}
	writeJSON(w, out)
}

func (h *handler) searchReset(w http.ResponseWriter, _ *http.Request) {
	if sess := h.state.GetSession(); sess != nil {
		sess.Reset()
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- pointer scan ---

func (h *handler) pointerScan(w http.ResponseWriter, r *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
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
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
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
	if err := modify.WithUndo(h.state.GetDriver(), pid, addr, val, vt); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) memoryUndo(w http.ResponseWriter, _ *http.Request) {
	if err := modify.Undo(); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "undo_depth": modify.UndoDepth()})
}

func (h *handler) memoryFreeze(w http.ResponseWriter, r *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
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
	modify.Freeze(h.state.GetDriver(), pid, addr, val)
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) memoryFreezeAll(w http.ResponseWriter, _ *http.Request) {
	sess := h.state.GetSession()
	if sess == nil || !sess.HasCandidates() {
		writeError(w, 400, "no candidates")
		return
	}
	modify.FreezeAllCandidates(h.state.GetDriver(), sess)
	writeJSON(w, map[string]any{"ok": true})
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
	modify.Unfreeze(addr)
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) memoryFrozen(w http.ResponseWriter, _ *http.Request) {
	addrs := modify.FrozenList()
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
	type entry struct {
		Index int    `json:"index"`
		Addr  string `json:"addr"`
		Label string `json:"label"`
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	out := make([]entry, len(bl.Entries))
	for i, b := range bl.Entries {
		val := "?"
		if pid != 0 {
			if cur, err := drv.Peek(pid, b.Addr, b.VType.Size()); err == nil {
				val = search.FormatValue(cur, b.VType)
			}
		}
		out[i] = entry{
			Index: i,
			Addr:  fmt.Sprintf("0x%x", b.Addr),
			Label: b.Label,
			Type:  b.VType.String(),
			Value: val,
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
	h.state.GetBookmarks().Remove(req.Index)
	writeJSON(w, map[string]any{"ok": true})
}

func (h *handler) bookmarkModifyAll(w http.ResponseWriter, r *http.Request) {
	pid := h.state.GetPID()
	if pid == 0 {
		writeError(w, 400, "not attached")
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
	h.state.GetBookmarks().ModifyAll(h.state.GetDriver(), pid, val, vt)
	writeJSON(w, map[string]any{"ok": true})
}

// --- session ---

func (h *handler) sessionSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil || req.Path == "" {
		req.Path = "memodroid.json"
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
		req.Path = "memodroid.json"
	}
	sess := h.state.GetSession()
	bl := h.state.GetBookmarks()
	if err := store.LoadState(req.Path, bl, &sess); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	h.state.SetSession(sess)
	writeJSON(w, map[string]any{"ok": true, "path": req.Path})
}

// --- parse helpers ---

func parseValueType(s string) (search.ValueType, error) {
	switch s {
	case "int32":
		return search.TypeInt32, nil
	case "int64":
		return search.TypeInt64, nil
	case "float32":
		return search.TypeFloat32, nil
	case "float64":
		return search.TypeFloat64, nil
	case "uint32":
		return search.TypeUint32, nil
	case "uint64":
		return search.TypeUint64, nil
	case "bytes":
		return search.TypeBytes, nil
	}
	return 0, fmt.Errorf("unknown type %q", s)
}

func parseFilterMode(s string) (search.FilterMode, error) {
	switch s {
	case "changed":
		return search.FilterChanged, nil
	case "unchanged":
		return search.FilterUnchanged, nil
	case "increased":
		return search.FilterIncreased, nil
	case "decreased":
		return search.FilterDecreased, nil
	case "value":
		return search.FilterValue, nil
	}
	return 0, fmt.Errorf("unknown filter mode %q", s)
}

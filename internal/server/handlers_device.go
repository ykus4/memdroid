package server

import (
	"net/http"
)

func (h *handler) status(w http.ResponseWriter, _ *http.Request) {
	pid := h.state.GetPID()
	sess := h.state.GetSession()

	candidates := 0
	if sess != nil {
		candidates = sess.CandidateCount()
	}

	writeJSON(w, map[string]any{
		"pid":        pid,
		"attached":   pid != 0,
		"value_type": h.state.GetValueType().String(),
		"candidates": candidates,
		"undo_depth": h.state.UndoStack.Depth(),
		"frozen":     hexAddrs(h.state.Freezer.List()),
		"watched":    hexAddrs(h.state.Watcher.List()),
		"alerts":     hexAddrs(h.state.AlertWatcher.List()),
		"device":     h.adb.DeviceSerial(),
	})
}

// hexAddrs converts a list of addresses for JSON output.
func hexAddrs(addrs []uintptr) []hexAddr {
	out := make([]hexAddr, len(addrs))
	for i, a := range addrs {
		out[i] = hexAddr(a)
	}
	return out
}

func (h *handler) deviceList(w http.ResponseWriter, _ *http.Request) {
	devices, err := h.adb.ListDevices()
	if err != nil {
		serverError(w, "list devices", err)
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
		serverError(w, "select device", err)
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
		serverError(w, "connect wifi", err)
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
		serverError(w, "disconnect wifi", err)
		return
	}
	writeOK(w, nil)
}

func (h *handler) mapsList(w http.ResponseWriter, _ *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	regions, err := h.state.GetDriver().ReadMaps(pid)
	if err != nil {
		serverError(w, "read maps", err)
		return
	}
	type entry struct {
		Start hexAddr `json:"start"`
		End   hexAddr `json:"end"`
		Size  int     `json:"size"`
		Name  string  `json:"name"`
	}
	out := make([]entry, len(regions))
	for i, r := range regions {
		out[i] = entry{
			Start: hexAddr(r.Start),
			End:   hexAddr(r.End),
			Size:  int(r.End - r.Start),
			Name:  r.Name,
		}
	}
	writeJSON(w, out)
}

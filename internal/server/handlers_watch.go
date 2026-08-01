package server

import (
	"net/http"
	"time"

	"memdroid/internal/memory/watch"
)

// defaultPollInterval matches the CLI default for watches and alerts.
const defaultPollInterval = 500 * time.Millisecond

func (h *handler) watchAdd(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr       string `json:"addr"`
		IntervalMs int    `json:"interval_ms"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	interval, ok := requireDuration(w, req.IntervalMs, defaultPollInterval)
	if !ok {
		return
	}
	vt := h.state.GetValueType()
	if err := h.state.Watcher.Watch(h.state.GetDriver(), pid, addr, vt, interval); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, map[string]any{
		"addr":        hexAddr(addr),
		"value_type":  vt.String(),
		"interval_ms": interval.Milliseconds(),
	})
}

func (h *handler) watchRemove(w http.ResponseWriter, r *http.Request) {
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
	if err := h.state.Watcher.Unwatch(addr); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *handler) watchList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, hexAddrs(h.state.Watcher.List()))
}

func (h *handler) alertAdd(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Addr       string `json:"addr"`
		Condition  string `json:"condition"`
		Threshold  string `json:"threshold"`
		Action     string `json:"action"`
		WriteValue string `json:"write_value"`
		IntervalMs int    `json:"interval_ms"`
	}](w, r)
	if !ok {
		return
	}
	addr, ok := requireAddr(w, req.Addr)
	if !ok {
		return
	}
	cond, err := watch.ParseAlertCondition(req.Condition)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	action, err := watch.ParseAlertAction(req.Action)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	interval, ok := requireDuration(w, req.IntervalMs, defaultPollInterval)
	if !ok {
		return
	}

	vt := h.state.GetValueType()
	cfg := watch.AlertConfig{Addr: addr, Condition: cond, Action: action}
	if cond != watch.AlertChanged {
		if cfg.Threshold, ok = requireValue(w, req.Threshold, vt); !ok {
			return
		}
	}
	if action == watch.ActionWrite {
		if cfg.WriteVal, ok = requireValue(w, req.WriteValue, vt); !ok {
			return
		}
	}

	if err := h.state.AlertWatcher.WatchWithAlert(h.state.GetDriver(), pid, vt, cfg, interval); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, map[string]any{
		"addr":        hexAddr(addr),
		"condition":   cond.String(),
		"action":      action.String(),
		"interval_ms": interval.Milliseconds(),
	})
}

func (h *handler) alertRemove(w http.ResponseWriter, r *http.Request) {
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
	if err := h.state.AlertWatcher.RemoveAlert(addr); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *handler) alertList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, hexAddrs(h.state.AlertWatcher.List()))
}

package server

import (
	"net/http"

	"memdroid/internal/driver"
)

// processEntry is the wire shape for a process, shared by list and search.
type processEntry struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

func processEntries(procs []driver.ProcessInfo) []processEntry {
	out := make([]processEntry, len(procs))
	for i, p := range procs {
		out[i] = processEntry{PID: p.PID, Name: p.Name}
	}
	return out
}

func (h *handler) processList(w http.ResponseWriter, _ *http.Request) {
	procs, err := h.state.GetDriver().ListProcesses()
	if err != nil {
		serverError(w, "list processes", err)
		return
	}
	writeJSON(w, processEntries(procs))
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
		serverError(w, "search processes", err)
		return
	}
	writeJSON(w, processEntries(matches))
}

func (h *handler) processAttach(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		PID  int    `json:"pid"`
		Name string `json:"name"`
	}](w, r)
	if !ok {
		return
	}
	if req.PID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid pid")
		return
	}
	if err := h.state.GetDriver().Attach(req.PID); err != nil {
		serverError(w, "attach", err)
		return
	}
	h.state.SetPID(req.PID)
	h.state.AddAttached(req.PID, req.Name)
	h.state.NewSession(req.PID)
	writeOK(w, map[string]any{"pid": req.PID})
}

func (h *handler) processDetach(w http.ResponseWriter, _ *http.Request) {
	if _, ok := h.requirePID(w); !ok {
		return
	}
	detached, next := h.state.Detach()
	out := map[string]any{"detached": detached}
	if next.PID != 0 {
		out["active_pid"] = next.PID
		out["active_name"] = next.Name
	}
	writeOK(w, out)
}

func (h *handler) processAttached(w http.ResponseWriter, _ *http.Request) {
	procs := h.state.ListAttached()
	active := h.state.GetPID()
	type entry struct {
		PID    int    `json:"pid"`
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	out := make([]entry, len(procs))
	for i, p := range procs {
		out[i] = entry{PID: p.PID, Name: p.Name, Active: p.PID == active}
	}
	writeJSON(w, out)
}

// processSwitch makes an already-attached process the active one.
func (h *handler) processSwitch(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		PID int `json:"pid"`
	}](w, r)
	if !ok {
		return
	}
	found := false
	for _, p := range h.state.ListAttached() {
		if p.PID == req.PID {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusBadRequest, "pid is not attached")
		return
	}
	h.state.SetPID(req.PID)
	h.state.NewSession(req.PID)
	writeOK(w, map[string]any{"pid": req.PID})
}

func (h *handler) processStop(w http.ResponseWriter, _ *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
		return
	}
	if err := h.state.GetDriver().Stop(pid); err != nil {
		serverError(w, "stop", err)
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
		serverError(w, "continue", err)
		return
	}
	writeOK(w, nil)
}

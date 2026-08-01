package server

import (
	"net/http"

	"memdroid/internal/memory/store"
)

func (h *handler) bookmarkList(w http.ResponseWriter, _ *http.Request) {
	bl := h.state.GetBookmarks()
	entries := bl.Entries()
	vals := bl.Values(h.state.GetDriver(), h.state.GetPID())

	type entry struct {
		Index int     `json:"index"`
		Addr  hexAddr `json:"addr"`
		Label string  `json:"label"`
		Type  string  `json:"type"`
		Value string  `json:"value"`
	}
	out := make([]entry, len(entries))
	for i, b := range entries {
		out[i] = entry{
			Index: i,
			Addr:  hexAddr(b.Addr),
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
	writeOK(w, map[string]any{"addr": hexAddr(addr)})
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
	path, ok := h.resolvePath(w, req.Path, "")
	if !ok {
		return
	}
	bookmarks, err := store.ImportCT(path)
	if err != nil {
		serverError(w, "import CT", err)
		return
	}
	bl := h.state.GetBookmarks()
	for _, b := range bookmarks {
		bl.Add(b.Addr, b.Label, b.VType)
	}
	writeOK(w, map[string]any{"imported": len(bookmarks)})
}

func (h *handler) sessionSave(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeOptionalJSON[struct {
		Path string `json:"path"`
	}](w, r)
	if !ok {
		return
	}
	path, ok := h.resolvePath(w, req.Path, store.DefaultStateFile)
	if !ok {
		return
	}
	if err := store.SaveState(path, h.state.GetBookmarks(), h.state.GetSession()); err != nil {
		serverError(w, "save state", err)
		return
	}
	writeOK(w, map[string]any{"path": path})
}

func (h *handler) sessionLoad(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeOptionalJSON[struct {
		Path string `json:"path"`
	}](w, r)
	if !ok {
		return
	}
	path, ok := h.resolvePath(w, req.Path, store.DefaultStateFile)
	if !ok {
		return
	}
	loaded, err := store.LoadState(path, h.state.GetBookmarks())
	if err != nil {
		serverError(w, "load state", err)
		return
	}
	candidates := 0
	if loaded != nil {
		// A saved session carries no driver; rebind it to the live one.
		loaded.SetDriver(h.state.GetDriver())
		candidates = loaded.CandidateCount()
	}
	h.state.SetSession(loaded)
	writeOK(w, map[string]any{"path": path, "candidates": candidates})
}

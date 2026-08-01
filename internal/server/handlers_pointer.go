package server

import (
	"net/http"

	"memdroid/internal/memory/pointer"
)

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
	if req.MaxOffset < 0 {
		writeError(w, http.StatusBadRequest, "max_offset must not be negative")
		return
	}
	result, err := pointer.Scan(h.state.GetDriver(), pid, addr, req.MaxDepth, uintptr(req.MaxOffset))
	if err != nil {
		serverError(w, "pointer scan", err)
		return
	}

	type chainJSON struct {
		Base       hexAddr `json:"base"`
		Label      string  `json:"label"`
		BaseOffset hexAddr `json:"base_offset"`
		Offsets    []int64 `json:"offsets"`
		Path       string  `json:"path"`
	}
	out := make([]chainJSON, len(result.Chains))
	for i, c := range result.Chains {
		out[i] = chainJSON{
			Base:       hexAddr(c.BaseAddr),
			Label:      c.BaseLabel,
			BaseOffset: hexAddr(c.BaseOffset),
			Offsets:    c.Offsets,
			Path:       pointer.FormatChain(c),
		}
	}
	writeJSON(w, map[string]any{"target": hexAddr(addr), "chains": out})
}

func (h *handler) pointerResolve(w http.ResponseWriter, r *http.Request) {
	pid, ok := h.requirePID(w)
	if !ok {
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
		if baseOffset, ok = requireAddr(w, req.BaseOffset); !ok {
			return
		}
	}
	chain := pointer.Chain{
		BaseLabel:  req.Label,
		BaseOffset: baseOffset,
		Offsets:    req.Offsets,
	}
	resolved, err := pointer.ResolveChain(h.state.GetDriver(), pid, chain)
	if err != nil {
		serverError(w, "resolve chain", err)
		return
	}
	writeJSON(w, map[string]any{
		"resolved":    hexAddr(resolved),
		"label":       req.Label,
		"base_offset": hexAddr(baseOffset),
		"offsets":     req.Offsets,
		"path":        pointer.FormatChain(chain),
	})
}

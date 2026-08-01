package server

import (
	"net/http"

	"memdroid/internal/driver"
	"memdroid/internal/memory/search"
)

const defaultPageSize = 100

// applyValueType switches the active type when the request names one. The type
// lives on the session, so this keeps a scan and the formatting of its results
// in agreement.
func (h *handler) applyValueType(w http.ResponseWriter, name string) (search.ValueType, bool) {
	if name == "" {
		return h.state.GetValueType(), true
	}
	vt, err := search.ParseValueType(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return 0, false
	}
	h.state.SetValueType(vt)
	return vt, true
}

// parseRegionFilter maps the optional region selector in a search request.
func parseRegionFilter(w http.ResponseWriter, name, startStr, endStr string) (driver.RegionFilter, uintptr, uintptr, bool) {
	var start, end uintptr
	var filter driver.RegionFilter
	switch name {
	case "", "all":
		filter = driver.RegionAll
	case "heap":
		filter = driver.RegionHeap
	case "stack":
		filter = driver.RegionStack
	case "anon":
		filter = driver.RegionAnon
	case "custom":
		filter = driver.RegionCustom
		var ok bool
		if start, ok = requireAddr(w, startStr); !ok {
			return 0, 0, 0, false
		}
		if end, ok = requireAddr(w, endStr); !ok {
			return 0, 0, 0, false
		}
		if end <= start {
			writeError(w, http.StatusBadRequest, "region end must be greater than start")
			return 0, 0, 0, false
		}
	default:
		writeError(w, http.StatusBadRequest, "unknown region "+name+" (use: all, heap, stack, anon, custom)")
		return 0, 0, 0, false
	}
	return filter, start, end, true
}

func (h *handler) searchValue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requirePID(w); !ok {
		return
	}
	req, ok := decodeJSON[struct {
		Value       string `json:"value"`
		Type        string `json:"type"`
		Region      string `json:"region"`
		RegionStart string `json:"region_start"`
		RegionEnd   string `json:"region_end"`
	}](w, r)
	if !ok {
		return
	}

	vt, ok := h.applyValueType(w, req.Type)
	if !ok {
		return
	}
	val, ok := requireValue(w, req.Value, vt)
	if !ok {
		return
	}
	filter, start, end, ok := parseRegionFilter(w, req.Region, req.RegionStart, req.RegionEnd)
	if !ok {
		return
	}

	sess := h.state.EnsureSession()
	if err := sess.SearchFiltered(val, filter, start, end); err != nil {
		serverError(w, "search", err)
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
	result, err := search.SearchPattern(h.state.GetDriver(), pid, pat)
	if err != nil {
		serverError(w, "pattern search", err)
		return
	}
	h.writeScanResult(w, result)
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
	enc, err := search.ParseStringEncoding(req.Encoding)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := search.SearchString(h.state.GetDriver(), pid, req.Value, enc)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeScanResult(w, result)
}

// writeScanResult loads a pattern/string scan into the session as byte
// candidates so they can be paged, filtered and frozen like any other result.
// The scan already returns the matched bytes, so no re-read is needed.
func (h *handler) writeScanResult(w http.ResponseWriter, result search.PatternResult) {
	sess := h.state.EnsureSession()
	sess.SetCandidatesAs(search.TypeBytes, result.CandidateMap())
	writeJSON(w, map[string]any{
		"count":      len(result.Matches),
		"candidates": sess.CandidateCount(),
		"truncated":  result.Truncated,
	})
}

func (h *handler) searchFilter(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.requireSession(w)
	if !ok {
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
		if target, ok = requireValue(w, req.Value, sess.Type()); !ok {
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
	pageSize := queryInt(r, "page_size", defaultPageSize, 1)
	page := queryInt(r, "page", 0, 0)

	sess := h.state.GetSession()
	if sess == nil {
		writeJSON(w, map[string]any{"total": 0, "page": page, "page_size": pageSize, "items": []any{}})
		return
	}

	vt := sess.Type()
	// Page copies only the requested slice, so this stays cheap even when the
	// candidate set has millions of entries.
	candidates, total := sess.Page(page*pageSize, pageSize)

	type entry struct {
		Addr  hexAddr `json:"addr"`
		Value string  `json:"value"`
	}
	items := make([]entry, len(candidates))
	for i, c := range candidates {
		items[i] = entry{Addr: hexAddr(c.Addr), Value: search.FormatValue(c.Value, vt)}
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
	writeOK(w, nil)
}

func (h *handler) searchTypes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"types":   search.ValueTypeNames(),
		"current": h.state.GetValueType().String(),
	})
}

func (h *handler) searchSetType(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Type string `json:"type"`
	}](w, r)
	if !ok {
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type required")
		return
	}
	vt, ok := h.applyValueType(w, req.Type)
	if !ok {
		return
	}
	writeOK(w, map[string]any{"value_type": vt.String()})
}

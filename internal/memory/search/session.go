package search

import (
	"bytes"
	"slices"
	"sync"

	"memdroid/internal/driver"
)

// Session holds the current search state for a single attached process.
//
// Every field is guarded by mu; nothing is exported directly. The session owns
// the active ValueType, so callers cannot end up searching with one width while
// formatting results with another. Long-running scans (Search/Filter) do their
// ADB I/O without holding the lock and only take it to swap in the finished
// result, so concurrent CLI/HTTP access never observes a half-built candidate
// map.
type Session struct {
	mu         sync.Mutex
	pid        int
	valueType  ValueType
	drv        driver.Driver
	candidates map[uintptr][]byte
	active     bool
}

func NewSession(pid int, vt ValueType, drv driver.Driver) *Session {
	return &Session{pid: pid, valueType: vt, drv: drv}
}

// PID returns the process this session searches.
func (s *Session) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pid
}

// Type returns the value type all candidates in this session are stored as.
func (s *Session) Type() ValueType {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.valueType
}

// SetType changes the active value type. Candidates recorded under a different
// byte width are meaningless afterwards, so they are discarded — a type change
// always starts a fresh search.
func (s *Session) SetType(vt ValueType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.valueType == vt {
		return
	}
	s.valueType = vt
	s.candidates = nil
	s.active = false
}

// SetDriver rebinds the session to a driver. Used after loading a session from
// disk, which cannot serialize a live connection.
func (s *Session) SetDriver(drv driver.Driver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drv = drv
}

// HasCandidates reports whether a scan has run and left at least one match.
func (s *Session) HasCandidates() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && len(s.candidates) > 0
}

// Searched reports whether a scan has run at all, regardless of how many
// matches it found. A search that returned nothing is a different situation
// from never having searched.
func (s *Session) Searched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *Session) CandidateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.candidates)
}

func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = nil
	s.active = false
}

// SetCandidates replaces the candidate map with c and marks the session active.
// Used after loading from disk or a pattern/string scan.
func (s *Session) SetCandidates(c map[uintptr][]byte) {
	s.replace(c)
}

// SetCandidatesAs replaces both the value type and the candidate map in one
// atomic step. Pattern and string scans need this: they produce TypeBytes
// candidates, and setting the type first would discard them.
func (s *Session) SetCandidatesAs(vt ValueType, c map[uintptr][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.valueType = vt
	s.candidates = c
	s.active = true
}

// replace swaps in a freshly built candidate map and marks the session active.
func (s *Session) replace(found map[uintptr][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = found
	s.active = true
}

// scanContext captures everything a scan needs from the session in one locked
// read, so the scan itself runs lock-free.
func (s *Session) scanContext() (pid int, vt ValueType, drv driver.Driver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pid, s.valueType, s.drv
}

// candidateWidth returns the byte width of the stored candidates: the type's
// fixed size, or for TypeBytes the length of an arbitrary candidate (all
// candidates from a single scan share a length). Returns 1 when empty.
func (s *Session) candidateWidth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if size := s.valueType.Size(); size != 0 {
		return size
	}
	for _, v := range s.candidates {
		return len(v)
	}
	return 1
}

// Snapshot returns a deep copy of the current address -> value map.
func (s *Session) Snapshot() map[uintptr][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uintptr][]byte, len(s.candidates))
	for addr, val := range s.candidates {
		out[addr] = bytes.Clone(val)
	}
	return out
}

// Candidate is one address/value pair from a search result.
type Candidate struct {
	Addr  uintptr
	Value []byte
}

// Page returns candidates sorted by address, starting at offset and at most
// limit entries, together with the total candidate count. Unlike Snapshot it
// copies only the requested page, so paging a multi-million-entry result set
// stays cheap.
func (s *Session) Page(offset, limit int) (page []Candidate, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	total = len(s.candidates)
	if offset < 0 {
		offset = 0
	}
	if offset >= total || limit <= 0 {
		return nil, total
	}

	addrs := make([]uintptr, 0, total)
	for addr := range s.candidates {
		addrs = append(addrs, addr)
	}
	slices.Sort(addrs)

	end := min(offset+limit, total)
	page = make([]Candidate, 0, end-offset)
	for _, addr := range addrs[offset:end] {
		page = append(page, Candidate{Addr: addr, Value: bytes.Clone(s.candidates[addr])})
	}
	return page, total
}

package search

import (
	"sync"

	"memdroid/internal/driver"
)

// Session holds the current search state for a single attached process.
//
// Candidates is guarded by mu. Long-running scans (Search/Filter) do their ADB
// I/O without holding the lock and only take it to swap in the finished result,
// so concurrent CLI/HTTP access never observes a half-built candidate map.
type Session struct {
	PID       int
	ValueType ValueType
	Driver    driver.Driver

	mu         sync.Mutex
	Candidates map[uintptr][]byte
	active     bool
}

func NewSession(pid int, vt ValueType, drv driver.Driver) *Session {
	return &Session{PID: pid, ValueType: vt, Driver: drv}
}

func (s *Session) HasCandidates() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && len(s.Candidates) > 0
}

func (s *Session) CandidateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Candidates)
}

func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Candidates = nil
	s.active = false
}

// SetCandidates replaces the candidate map with c and marks the session active.
// Used after loading from disk or a pattern/string scan.
func (s *Session) SetCandidates(c map[uintptr][]byte) {
	s.replace(c)
}

// replace swaps in a freshly built candidate map and marks the session active.
func (s *Session) replace(found map[uintptr][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Candidates = found
	s.active = true
}

// firstCandidateLen returns the byte length of an arbitrary candidate, or 1 if
// the session is empty. Used for TypeBytes where the width is data-defined.
func (s *Session) firstCandidateLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.Candidates {
		return len(v)
	}
	return 1
}

// Snapshot returns a deep copy of the current address -> value map.
func (s *Session) Snapshot() map[uintptr][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uintptr][]byte, len(s.Candidates))
	for addr, val := range s.Candidates {
		cp := make([]byte, len(val))
		copy(cp, val)
		out[addr] = cp
	}
	return out
}

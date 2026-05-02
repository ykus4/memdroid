package search

import "memodroid/internal/driver"

// Session holds the current search state for a single attached process.
type Session struct {
	PID        int
	ValueType  ValueType
	Driver     driver.Driver
	Candidates map[uintptr][]byte
	active     bool
}

func NewSession(pid int, vt ValueType, drv driver.Driver) *Session {
	return &Session{PID: pid, ValueType: vt, Driver: drv}
}

func (s *Session) HasCandidates() bool {
	return s.active && len(s.Candidates) > 0
}

func (s *Session) CandidateCount() int {
	return len(s.Candidates)
}

func (s *Session) Reset() {
	s.Candidates = nil
	s.active = false
}

// SetActive marks the session as having been populated (used after loading from disk).
func (s *Session) SetActive() {
	s.active = true
}

// firstCandidateLen returns the byte length of an arbitrary candidate (for TypeBytes).
func (s *Session) firstCandidateLen() int {
	for _, v := range s.Candidates {
		return len(v)
	}
	return 1
}

// Snapshot returns a copy of the current address -> value map.
func (s *Session) Snapshot() map[uintptr][]byte {
	out := make(map[uintptr][]byte, len(s.Candidates))
	for addr, val := range s.Candidates {
		cp := make([]byte, len(val))
		copy(cp, val)
		out[addr] = cp
	}
	return out
}

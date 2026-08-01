package search

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"memdroid/internal/driver"
)

// PatternMaxResults caps how many pattern/string matches are collected before
// the scan stops early. Results report Truncated when the cap was hit.
const PatternMaxResults = 200

// PatternByte represents one byte in a search pattern; Wildcard=true matches any byte.
type PatternByte struct {
	Value    byte
	Wildcard bool
}

// Pattern is a sequence of literal and wildcard bytes to search for.
type Pattern []PatternByte

// ParsePattern parses a hex pattern string like "FF 00 ?? 01" into a Pattern.
func ParsePattern(s string) (Pattern, error) {
	tokens := strings.Fields(s)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty pattern")
	}
	out := make(Pattern, len(tokens))
	for i, t := range tokens {
		if t == "??" || t == "?" {
			out[i] = PatternByte{Wildcard: true}
			continue
		}
		v, err := strconv.ParseUint(t, 16, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid token %q: %w", t, err)
		}
		out[i] = PatternByte{Value: byte(v)}
	}
	return out, nil
}

// match reports whether buf starts with this pattern. buf must be at least
// len(p) bytes long.
func (p Pattern) match(buf []byte) bool {
	for i, pb := range p {
		if !pb.Wildcard && buf[i] != pb.Value {
			return false
		}
	}
	return true
}

// PatternResult holds the matches from a pattern or string scan.
type PatternResult struct {
	// Matches are sorted by address and carry the bytes actually found there,
	// so wildcard positions can be inspected without re-reading memory.
	Matches []Candidate
	// Truncated reports that the scan stopped at PatternMaxResults and more
	// matches likely exist.
	Truncated bool
}

// Addrs returns just the matched addresses, in order.
func (r PatternResult) Addrs() []uintptr {
	out := make([]uintptr, len(r.Matches))
	for i, m := range r.Matches {
		out[i] = m.Addr
	}
	return out
}

// CandidateMap returns the matches as an address -> value map suitable for
// seeding a Session.
func (r PatternResult) CandidateMap() map[uintptr][]byte {
	out := make(map[uintptr][]byte, len(r.Matches))
	for _, m := range r.Matches {
		out[m.Addr] = m.Value
	}
	return out
}

// SearchPattern scans all rw memory regions for the byte pattern.
//
// It reads each region in bulk through the shared scan engine rather than
// byte-poking it; a pattern scan over a large mapping is one adb round-trip per
// 32 MiB instead of one per 256 bytes.
func SearchPattern(drv driver.Driver, pid int, pattern Pattern) (PatternResult, error) {
	if len(pattern) == 0 {
		return PatternResult{}, fmt.Errorf("empty pattern")
	}

	regions, err := drv.ReadMaps(pid)
	if err != nil {
		return PatternResult{}, fmt.Errorf("pattern search: read maps: %w", err)
	}

	plen := len(pattern)
	// Collect one past the cap so a run of exactly PatternMaxResults matches
	// can be reported complete rather than as a truncated result.
	limit := newScanLimit(PatternMaxResults + 1)
	found := scanRegions(drv, pid, regions, plen, func(buf []byte, base uintptr, emit emitFunc) {
		for i := 0; i+plen <= len(buf); i++ {
			if pattern.match(buf[i:]) {
				emit(base+uintptr(i), buf[i:i+plen])
			}
		}
	}, limit)

	matches := make([]Candidate, 0, len(found))
	for addr, val := range found {
		matches = append(matches, Candidate{Addr: addr, Value: val})
	}
	slices.SortFunc(matches, func(a, b Candidate) int { return cmp.Compare(a.Addr, b.Addr) })

	truncated := len(matches) > PatternMaxResults
	if truncated {
		matches = matches[:PatternMaxResults]
	}
	return PatternResult{Matches: matches, Truncated: truncated}, nil
}

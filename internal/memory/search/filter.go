package search

import (
	"fmt"
	"sort"
	"sync"

	"memodroid/internal/driver"
)

type FilterMode int

const (
	FilterChanged FilterMode = iota
	FilterUnchanged
	FilterIncreased
	FilterDecreased
	FilterValue
)

// Filter narrows the candidate list by re-reading current values and applying mode.
// For FilterValue, pass the target bytes; otherwise target may be nil.
func (s *Session) Filter(mode FilterMode, target []byte) error {
	if !s.HasCandidates() {
		return fmt.Errorf("no active search session")
	}

	fixedSize := s.ValueType.Size()
	if fixedSize == 0 {
		fixedSize = s.firstCandidateLen()
	}

	regions, err := s.Driver.ReadMaps(s.PID)
	if err != nil {
		return fmt.Errorf("filter: read maps: %w", err)
	}

	type regionData struct {
		start uintptr
		buf   []byte
	}

	// Determine which regions contain candidates so we only read those.
	type candEntry struct {
		addr uintptr
		prev []byte
	}
	entries := make([]candEntry, 0, len(s.Candidates))
	for addr, prev := range s.Candidates {
		entries = append(entries, candEntry{addr, prev})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].addr < entries[j].addr })

	regionFor := func(addr uintptr) (int, *driver.Region) {
		for i := range regions {
			if addr >= regions[i].Start && addr < regions[i].End {
				return i, &regions[i]
			}
		}
		return -1, nil
	}

	// Identify needed regions.
	needed := make(map[int]struct{})
	for _, e := range entries {
		if idx, _ := regionFor(e.addr); idx >= 0 {
			needed[idx] = struct{}{}
		}
	}

	// Pre-load needed regions in parallel.
	cache := make(map[int]*regionData)
	var cacheMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanWorkers)

	for idx := range needed {
		idx := idx
		r := &regions[idx]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			buf, readErr := s.Driver.ReadRegion(s.PID, r.Start, int(r.End-r.Start))
			cacheMu.Lock()
			if readErr != nil {
				cache[idx] = nil
			} else {
				cache[idx] = &regionData{start: r.Start, buf: buf}
			}
			cacheMu.Unlock()
		}()
	}
	wg.Wait()

	next := make(map[uintptr][]byte)
	for _, e := range entries {
		sz := fixedSize
		if s.ValueType == TypeBytes {
			sz = len(e.prev)
		}

		idx, _ := regionFor(e.addr)
		var cur []byte
		if idx >= 0 {
			if d := cache[idx]; d != nil {
				off := int(e.addr - d.start)
				if off >= 0 && off+sz <= len(d.buf) {
					cur = d.buf[off : off+sz]
				}
			}
		}
		if cur == nil {
			cur, err = s.Driver.Peek(s.PID, e.addr, sz)
			if err != nil {
				continue
			}
		}

		var keep bool
		switch mode {
		case FilterChanged:
			keep = !EqualBytes(cur, e.prev)
		case FilterUnchanged:
			keep = EqualBytes(cur, e.prev)
		case FilterIncreased:
			keep = CompareValues(cur, e.prev, s.ValueType) > 0
		case FilterDecreased:
			keep = CompareValues(cur, e.prev, s.ValueType) < 0
		case FilterValue:
			keep = EqualBytes(cur, target)
		}

		if keep {
			cp := make([]byte, sz)
			copy(cp, cur)
			next[e.addr] = cp
		}
	}

	s.Candidates = next
	return nil
}

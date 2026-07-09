package search

import (
	"fmt"
	"sort"
	"sync"

	"memdroid/internal/driver"
)

type FilterMode int

const (
	FilterChanged FilterMode = iota
	FilterUnchanged
	FilterIncreased
	FilterDecreased
	FilterValue
)

func (m FilterMode) valid() bool {
	return m >= FilterChanged && m <= FilterValue
}

// Filter narrows the candidate list by re-reading current values and applying mode.
// For FilterValue, pass the target bytes; otherwise target may be nil.
func (s *Session) Filter(mode FilterMode, target []byte) error {
	if !mode.valid() {
		return fmt.Errorf("invalid filter mode %d", mode)
	}
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
	sort.Slice(regions, func(i, j int) bool { return regions[i].Start < regions[j].Start })

	// regionFor binary-searches the sorted region list for the region containing addr.
	regionFor := func(addr uintptr) int {
		i := sort.Search(len(regions), func(k int) bool { return regions[k].Start > addr }) - 1
		if i >= 0 && addr < regions[i].End {
			return i
		}
		return -1
	}

	// Gather candidates in address order and record which regions to read.
	type candEntry struct {
		addr   uintptr
		prev   []byte
		regIdx int
	}
	snap := s.Snapshot()
	entries := make([]candEntry, 0, len(snap))
	needed := make(map[int]struct{})
	for addr, prev := range snap {
		idx := regionFor(addr)
		entries = append(entries, candEntry{addr: addr, prev: prev, regIdx: idx})
		if idx >= 0 {
			needed[idx] = struct{}{}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].addr < entries[j].addr })

	// Pre-load needed regions in parallel.
	cache := make(map[int][]byte)
	var cacheMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanWorkers)
	for idx := range needed {
		idx := idx
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r := regions[idx]
			buf, readErr := s.Driver.ReadRegion(s.PID, r.Start, int(r.End-r.Start))
			if readErr != nil {
				return
			}
			cacheMu.Lock()
			cache[idx] = buf
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

		cur := sliceFromCache(cache, regions, e.regIdx, e.addr, sz)
		if cur == nil {
			c, readErr := s.Driver.Peek(s.PID, e.addr, sz)
			if readErr != nil {
				continue
			}
			cur = c
		}

		if filterKeep(mode, cur, e.prev, target, s.ValueType) {
			next[e.addr] = cloneBytes(cur)
		}
	}

	s.replace(next)
	return nil
}

// sliceFromCache returns the sz bytes at addr from the cached region buffer, or
// nil if the region was not cached or the slice would fall out of bounds.
func sliceFromCache(cache map[int][]byte, regions []driver.Region, idx int, addr uintptr, sz int) []byte {
	if idx < 0 {
		return nil
	}
	buf, ok := cache[idx]
	if !ok {
		return nil
	}
	off := int(addr - regions[idx].Start)
	if off < 0 || off+sz > len(buf) {
		return nil
	}
	return buf[off : off+sz]
}

func filterKeep(mode FilterMode, cur, prev, target []byte, vt ValueType) bool {
	switch mode {
	case FilterChanged:
		return !EqualBytes(cur, prev)
	case FilterUnchanged:
		return EqualBytes(cur, prev)
	case FilterIncreased:
		return CompareValues(cur, prev, vt) > 0
	case FilterDecreased:
		return CompareValues(cur, prev, vt) < 0
	case FilterValue:
		return EqualBytes(cur, target)
	}
	return false
}

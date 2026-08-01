package search

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
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
	// Distinguish "never searched" from "searched and found nothing" — both
	// leave HasCandidates false, but they call for different fixes.
	if !s.Searched() {
		return fmt.Errorf("no active search session")
	}
	if !s.HasCandidates() {
		return fmt.Errorf("no candidates remain to filter")
	}
	if mode == FilterValue && len(target) == 0 {
		return fmt.Errorf("filter %q requires a target value", mode)
	}

	pid, vt, drv := s.scanContext()
	width := s.candidateWidth()

	regions, err := drv.ReadMaps(pid)
	if err != nil {
		return fmt.Errorf("filter: read maps: %w", err)
	}
	slices.SortFunc(regions, func(a, b driver.Region) int { return cmp.Compare(a.Start, b.Start) })

	snap := s.Snapshot()
	entries, needed := planFilterReads(snap, regions)
	cache := readRegions(drv, pid, regions, needed)

	next := make(map[uintptr][]byte, len(entries))
	for _, e := range entries {
		sz := width
		if vt == TypeBytes {
			sz = len(e.prev)
		}

		cur := sliceFromCache(cache, regions, e.regIdx, e.addr, sz)
		if cur == nil {
			// Address fell outside every cached region (unmapped since the last
			// scan, or its region failed to read) — fall back to a point read.
			c, readErr := drv.Peek(pid, e.addr, sz)
			if readErr != nil {
				continue
			}
			cur = c
		}

		if filterKeep(mode, cur, e.prev, target, vt) {
			next[e.addr] = bytes.Clone(cur)
		}
	}

	s.replace(next)
	return nil
}

// candEntry pairs a candidate with the index of the region containing it.
type candEntry struct {
	addr   uintptr
	prev   []byte
	regIdx int
}

// planFilterReads pairs every candidate with its containing region (via binary
// search over the sorted region list) and reports which regions must be read.
func planFilterReads(snap map[uintptr][]byte, regions []driver.Region) (entries []candEntry, needed map[int]struct{}) {
	regionFor := func(addr uintptr) int {
		i, _ := slices.BinarySearchFunc(regions, addr, func(r driver.Region, a uintptr) int {
			return cmp.Compare(r.Start, a)
		})
		// BinarySearchFunc returns the first region with Start >= addr; the
		// containing region is that one (exact hit) or the one before it.
		if i < len(regions) && regions[i].Start == addr {
			return i
		}
		i--
		if i >= 0 && addr < regions[i].End {
			return i
		}
		return -1
	}

	entries = make([]candEntry, 0, len(snap))
	needed = make(map[int]struct{})
	for addr, prev := range snap {
		idx := regionFor(addr)
		entries = append(entries, candEntry{addr: addr, prev: prev, regIdx: idx})
		if idx >= 0 {
			needed[idx] = struct{}{}
		}
	}
	slices.SortFunc(entries, func(a, b candEntry) int { return cmp.Compare(a.addr, b.addr) })
	return entries, needed
}

// readRegions bulk-loads the requested regions in parallel. Regions that fail
// to read are simply absent from the result.
func readRegions(drv driver.Driver, pid int, regions []driver.Region, needed map[int]struct{}) map[int][]byte {
	cache := make(map[int][]byte, len(needed))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanWorkers)

	for idx := range needed {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r := regions[idx]
			buf, err := drv.ReadRegion(pid, r.Start, int(r.End-r.Start))
			if err != nil {
				return
			}
			mu.Lock()
			cache[idx] = buf
			mu.Unlock()
		}()
	}
	wg.Wait()
	return cache
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
		return !bytes.Equal(cur, prev)
	case FilterUnchanged:
		return bytes.Equal(cur, prev)
	case FilterIncreased:
		return CompareValues(cur, prev, vt) > 0
	case FilterDecreased:
		return CompareValues(cur, prev, vt) < 0
	case FilterValue:
		return bytes.Equal(cur, target)
	}
	return false
}

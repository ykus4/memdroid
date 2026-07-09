package search

import (
	"sync"

	"memdroid/internal/driver"
)

// scanWorkers limits concurrent ADB calls during parallel memory reads.
const scanWorkers = 8

// Search scans all rw memory regions for target and stores results in the session.
func (s *Session) Search(target []byte) error {
	return s.SearchFiltered(target, driver.RegionAll, 0, 0)
}

// SearchFiltered scans memory with a region filter applied.
func (s *Session) SearchFiltered(target []byte, filter driver.RegionFilter, customStart, customEnd uintptr) error {
	regions, err := s.Driver.ReadMapsFiltered(s.PID, filter, customStart, customEnd)
	if err != nil {
		return err
	}

	var found map[uintptr][]byte
	if s.ValueType == TypeBytes {
		tlen := len(target)
		found = scanRegions(s.Driver, s.PID, regions, tlen, func(buf []byte, base uintptr, out map[uintptr][]byte) {
			for i := 0; i+tlen <= len(buf); i++ {
				if EqualBytes(buf[i:i+tlen], target) {
					out[base+uintptr(i)] = cloneBytes(buf[i : i+tlen])
				}
			}
		})
	} else {
		size := s.ValueType.Size()
		found = scanRegions(s.Driver, s.PID, regions, size, func(buf []byte, base uintptr, out map[uintptr][]byte) {
			for i := 0; i+size <= len(buf); i += size {
				if EqualBytes(buf[i:i+size], target) {
					out[base+uintptr(i)] = cloneBytes(buf[i : i+size])
				}
			}
		})
	}

	s.replace(found)
	return nil
}

// scanRegions reads each region once (in parallel, bounded by scanWorkers) and
// invokes scan on the resulting buffer to collect matches. Regions smaller than
// minSize are skipped. It is the shared engine for value and byte-sequence scans.
func scanRegions(drv driver.Driver, pid int, regions []driver.Region, minSize int, scan func(buf []byte, base uintptr, out map[uintptr][]byte)) map[uintptr][]byte {
	found := make(map[uintptr][]byte)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanWorkers)

	for _, r := range regions {
		size := int(r.End - r.Start)
		if size <= 0 || size < minSize {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(r driver.Region, size int) {
			defer wg.Done()
			defer func() { <-sem }()

			buf, err := drv.ReadRegion(pid, r.Start, size)
			if err != nil {
				return
			}
			local := make(map[uintptr][]byte)
			scan(buf, r.Start, local)
			if len(local) > 0 {
				mu.Lock()
				for addr, val := range local {
					found[addr] = val
				}
				mu.Unlock()
			}
		}(r, size)
	}
	wg.Wait()
	return found
}

func cloneBytes(b []byte) []byte {
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

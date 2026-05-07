package search

import (
	"sync"

	"memodroid/internal/driver"
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

	found := make(map[uintptr][]byte)

	if s.ValueType == TypeBytes {
		searchBytesInRegions(s.Driver, s.PID, regions, target, found)
	} else {
		size := s.ValueType.Size()
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, scanWorkers)

		for _, r := range regions {
			regionSize := int(r.End - r.Start)
			if regionSize <= 0 {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(r driver.Region, regionSize int) {
				defer wg.Done()
				defer func() { <-sem }()

				buf, err := s.Driver.ReadRegion(s.PID, r.Start, regionSize)
				if err != nil {
					return
				}
				local := make(map[uintptr][]byte)
				for i := 0; i+size <= len(buf); i += size {
					if EqualBytes(buf[i:i+size], target) {
						cp := make([]byte, size)
						copy(cp, buf[i:i+size])
						local[r.Start+uintptr(i)] = cp
					}
				}
				if len(local) > 0 {
					mu.Lock()
					for addr, val := range local {
						found[addr] = val
					}
					mu.Unlock()
				}
			}(r, regionSize)
		}
		wg.Wait()
	}

	s.Candidates = found
	s.active = true
	return nil
}

func searchBytesInRegions(drv driver.Driver, pid int, regions []driver.Region, target []byte, found map[uintptr][]byte) {
	tlen := len(target)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, scanWorkers)

	for _, r := range regions {
		size := int(r.End - r.Start)
		if size <= 0 || size < tlen {
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
			for i := 0; i <= len(buf)-tlen; i++ {
				if EqualBytes(buf[i:i+tlen], target) {
					cp := make([]byte, tlen)
					copy(cp, target)
					local[r.Start+uintptr(i)] = cp
				}
			}
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
}

// Package pointer implements pointer chain scanning.
//
// A pointer scan finds stable base addresses that, when followed through a
// chain of offsets, reliably reach a target address across process restarts.
// This is necessary because heap allocations change every run.
//
// Algorithm:
//  1. Collect all pointer-aligned values in rw regions that fall within the
//     mapped address space ("candidate pointers").
//  2. Starting from targetAddr, walk backwards up to maxDepth levels:
//     find all stored pointers that point within [targetAddr-maxOffset, targetAddr].
//  3. Recursively repeat for each found pointer address until we reach a
//     pointer stored in a static region (module base, [bss], etc.).
//  4. Return the resulting chains as []Chain.
package pointer

import (
	"encoding/binary"
	"fmt"

	"memodroid/internal/driver"
)

const (
	DefaultMaxDepth  = 5
	DefaultMaxOffset = 0x800 // max offset between pointer and pointed-to address
	pointerSize      = 8     // 64-bit Android
	maxResults       = 500
)

// Chain represents one resolved pointer path.
// Offsets[0] is applied to BaseAddr to get the first pointer,
// Offsets[1] to dereference that, etc., until FinalAddr is reached.
type Chain struct {
	BaseAddr  uintptr // address in a static region (module/stack base)
	BaseLabel string  // name of the region (e.g. "libil2cpp.so")
	Offsets   []int64 // signed offsets at each level
	FinalAddr uintptr // the target address this chain resolves to
}

// ScanResult holds all chains found for a target address.
type ScanResult struct {
	Target uintptr
	Chains []Chain
}

// Scan performs a pointer scan for targetAddr in the process identified by pid.
func Scan(drv driver.Driver, pid int, targetAddr uintptr, maxDepth int, maxOffset uintptr) (*ScanResult, error) {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if maxOffset == 0 {
		maxOffset = DefaultMaxOffset
	}

	regions, err := drv.ReadMaps(pid)
	if err != nil {
		return nil, fmt.Errorf("read maps: %w", err)
	}

	// Build a map: stored_value -> []address_where_stored
	// Only store values that point somewhere inside our mapped regions.
	addrSet := buildAddrSet(regions)
	ptrMap, err := buildPtrMap(drv, pid, regions, addrSet)
	if err != nil {
		return nil, fmt.Errorf("build pointer map: %w", err)
	}

	result := &ScanResult{Target: targetAddr}
	var walk func(addr uintptr, depth int, offsets []int64)
	walk = func(addr uintptr, depth int, offsets []int64) {
		if len(result.Chains) >= maxResults {
			return
		}
		// Find all pointers that point to [addr-maxOffset, addr]
		for delta := uintptr(0); delta <= maxOffset; delta += pointerSize {
			target := addr - delta
			sources, ok := ptrMap[target]
			if !ok {
				continue
			}
			offset := int64(delta)
			newOffsets := make([]int64, len(offsets)+1)
			copy(newOffsets, offsets)
			newOffsets[len(offsets)] = offset

			for _, src := range sources {
				if depth+1 < maxDepth {
					walk(src, depth+1, newOffsets)
				}
				// Check if src lives in a static region.
				if label, ok := staticRegion(src, regions); ok {
					result.Chains = append(result.Chains, Chain{
						BaseAddr:  src,
						BaseLabel: label,
						Offsets:   newOffsets,
						FinalAddr: targetAddr,
					})
				}
			}
		}
	}
	walk(targetAddr, 0, nil)
	return result, nil
}

// buildAddrSet returns a set of all addresses within mapped regions.
func buildAddrSet(regions []driver.Region) map[uintptr]struct{} {
	s := make(map[uintptr]struct{})
	for _, r := range regions {
		s[r.Start] = struct{}{}
	}
	return s
}

// buildPtrMap scans all rw regions and maps each pointer-valued address to the
// locations that store it.
func buildPtrMap(drv driver.Driver, pid int, regions []driver.Region, addrSet map[uintptr]struct{}) (map[uintptr][]uintptr, error) {
	// Determine the full mapped range so we can validate pointer values quickly.
	var mapMin, mapMax uintptr
	for i, r := range regions {
		if i == 0 || r.Start < mapMin {
			mapMin = r.Start
		}
		if r.End > mapMax {
			mapMax = r.End
		}
	}

	ptrMap := make(map[uintptr][]uintptr)
	const chunkSize = 4096

	for _, r := range regions {
		size := int(r.End - r.Start)
		for offset := 0; offset < size-pointerSize+1; offset += chunkSize {
			end := offset + chunkSize
			if end > size {
				end = size
			}
			chunk, err := drv.ReadBytes(pid, r.Start+uintptr(offset), end-offset)
			if err != nil {
				continue
			}
			for i := 0; i+pointerSize <= len(chunk); i += pointerSize {
				val := uintptr(binary.LittleEndian.Uint64(chunk[i:]))
				if val >= mapMin && val < mapMax {
					srcAddr := r.Start + uintptr(offset+i)
					ptrMap[val] = append(ptrMap[val], srcAddr)
				}
			}
		}
	}
	return ptrMap, nil
}

// staticRegion returns the region name if addr lies in a non-anonymous region
// (i.e. a mapped file like a .so), which is considered a stable base.
func staticRegion(addr uintptr, regions []driver.Region) (string, bool) {
	for _, r := range regions {
		if addr >= r.Start && addr < r.End && r.Name != "" &&
			r.Name != "[heap]" && r.Name != "[stack]" && r.Name != "[anon]" {
			return r.Name, true
		}
	}
	return "", false
}

// FormatChain returns a human-readable pointer path string.
func FormatChain(c Chain) string {
	s := fmt.Sprintf("[%s+0x%x]", c.BaseLabel, c.BaseAddr)
	for _, off := range c.Offsets {
		if off >= 0 {
			s += fmt.Sprintf("+0x%x", off)
		} else {
			s += fmt.Sprintf("-0x%x", -off)
		}
	}
	return s
}

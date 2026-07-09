// Package pointer implements pointer chain scanning.
//
// A pointer scan finds stable base addresses that, when followed through a
// chain of offsets, reliably reach a target address across process restarts.
// This is necessary because heap allocations change every run.
//
// Algorithm:
//  1. Collect all pointer-aligned values in rw regions that fall within the
//     mapped address space ("candidate pointers"), mapping each pointed-to
//     value to the addresses that store it.
//  2. Starting from targetAddr, walk backwards up to maxDepth levels:
//     find all stored pointers that point within [targetAddr-maxOffset, targetAddr].
//  3. Recursively repeat for each found pointer address until we reach a
//     pointer stored in a static region (module base, [bss], etc.).
//  4. Return the resulting chains as []Chain.
//
// Chain offsets are stored in application order (base -> final), i.e. the order
// ResolveChain applies them, matching the Cheat Engine "[[base+o0]+o1]+..."
// convention.
package pointer

import (
	"encoding/binary"
	"fmt"

	"memdroid/internal/driver"
)

const (
	DefaultMaxDepth  = 5
	DefaultMaxOffset = 0x800 // max offset between pointer and pointed-to address
	pointerSize      = 8     // 64-bit Android
	maxResults       = 500
)

// Chain represents one resolved pointer path.
//
// The base pointer is stored at moduleBase(BaseLabel)+BaseOffset. Offsets are
// applied in order: read the pointer at the current address, add the offset,
// repeat, until FinalAddr is reached.
type Chain struct {
	BaseAddr   uintptr // absolute address of the base pointer at scan time (informational)
	BaseLabel  string  // name of the region (e.g. "libil2cpp.so")
	BaseOffset uintptr // offset of the base pointer within its module
	Offsets    []int64 // signed offsets, base -> final order
	FinalAddr  uintptr // the target address this chain resolves to
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

	// Map: pointed-to value -> addresses that store it (only values inside our maps).
	ptrMap, err := buildPtrMap(drv, pid, regions)
	if err != nil {
		return nil, fmt.Errorf("build pointer map: %w", err)
	}

	result := &ScanResult{Target: targetAddr}
	onPath := map[uintptr]bool{targetAddr: true}

	var walk func(addr uintptr, depth int, offsets []int64)
	walk = func(addr uintptr, depth int, offsets []int64) {
		if len(result.Chains) >= maxResults {
			return
		}
		// Find all pointers that point to [addr-maxOffset, addr].
		for delta := uintptr(0); delta <= maxOffset; delta += pointerSize {
			sources, ok := ptrMap[addr-delta]
			if !ok {
				continue
			}
			// Discovery order accumulates target-side first; prepend so Offsets
			// stay in base -> final application order.
			newOffsets := make([]int64, 0, len(offsets)+1)
			newOffsets = append(newOffsets, int64(delta))
			newOffsets = append(newOffsets, offsets...)

			for _, src := range sources {
				if onPath[src] {
					continue // cycle
				}
				if label, base, ok := staticRegion(src, regions); ok {
					result.Chains = append(result.Chains, Chain{
						BaseAddr:   src,
						BaseLabel:  label,
						BaseOffset: src - base,
						Offsets:    newOffsets,
						FinalAddr:  targetAddr,
					})
					if len(result.Chains) >= maxResults {
						return
					}
				}
				if depth+1 < maxDepth {
					onPath[src] = true
					walk(src, depth+1, newOffsets)
					delete(onPath, src)
				}
			}
		}
	}
	walk(targetAddr, 0, nil)
	return result, nil
}

// buildPtrMap scans all rw regions and maps each pointer-valued address to the
// locations that store it. Each region is read in a single bulk transfer so no
// pointer is missed at a chunk boundary.
func buildPtrMap(drv driver.Driver, pid int, regions []driver.Region) (map[uintptr][]uintptr, error) {
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
	for _, r := range regions {
		size := int(r.End - r.Start)
		if size < pointerSize {
			continue
		}
		buf, err := drv.ReadRegion(pid, r.Start, size)
		if err != nil {
			continue
		}
		for i := 0; i+pointerSize <= len(buf); i += pointerSize {
			val := uintptr(binary.LittleEndian.Uint64(buf[i:]))
			if val >= mapMin && val < mapMax {
				ptrMap[val] = append(ptrMap[val], r.Start+uintptr(i))
			}
		}
	}
	return ptrMap, nil
}

// staticRegion returns the region name and module base if addr lies in a
// non-anonymous region (a mapped file like a .so), which is a stable base.
func staticRegion(addr uintptr, regions []driver.Region) (string, uintptr, bool) {
	for _, r := range regions {
		if addr >= r.Start && addr < r.End && isStaticName(r.Name) {
			return r.Name, moduleBase(regions, r.Name), true
		}
	}
	return "", 0, false
}

func isStaticName(name string) bool {
	return name != "" && name != "[heap]" && name != "[stack]" && name != "[anon]"
}

// moduleBase returns the lowest start address of any region named name.
func moduleBase(regions []driver.Region, name string) uintptr {
	var base uintptr
	found := false
	for _, r := range regions {
		if r.Name == name && (!found || r.Start < base) {
			base = r.Start
			found = true
		}
	}
	return base
}

// FormatChain returns a human-readable pointer path string.
func FormatChain(c Chain) string {
	s := fmt.Sprintf("[%s+0x%x]", c.BaseLabel, c.BaseOffset)
	for _, off := range c.Offsets {
		if off >= 0 {
			s += fmt.Sprintf("+0x%x", off)
		} else {
			s += fmt.Sprintf("-0x%x", -off)
		}
	}
	return s
}

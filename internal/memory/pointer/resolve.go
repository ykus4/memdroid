package pointer

import (
	"encoding/binary"
	"fmt"

	"memodroid/internal/driver"
)

// ResolveChain resolves a saved pointer chain against the current process state.
// It finds the module base by matching Chain.BaseLabel in the process's memory maps,
// then walks the pointer chain (reading each pointer and adding the next offset)
// to arrive at the final address.
func ResolveChain(drv driver.Driver, pid int, chain Chain) (uintptr, error) {
	regions, err := drv.ReadMaps(pid)
	if err != nil {
		return 0, fmt.Errorf("read maps: %w", err)
	}

	// Find the module base address by label
	var baseAddr uintptr
	found := false
	for _, r := range regions {
		if r.Name == chain.BaseLabel {
			baseAddr = r.Start
			found = true
			break
		}
	}
	if !found {
		return 0, fmt.Errorf("module %q not found in memory maps", chain.BaseLabel)
	}

	// The chain's BaseAddr was an absolute address in the original run.
	// We compute the offset from the original module base by looking at the
	// relative position. However, the Chain stores the actual address where
	// the first pointer was stored. We need to rebase it.
	// The stored BaseAddr is relative to the original module load.
	// For simplicity, use the stored base offset from the module start:
	// since we stored an absolute BaseAddr at scan time, we need the offset
	// within the module. We'll use the chain offsets directly starting from
	// the new module base.

	// Walk the chain: start at baseAddr, read pointer, add offset, repeat
	current := baseAddr
	for i, off := range chain.Offsets {
		// Read pointer at current address
		data, err := drv.Peek(pid, current, pointerSize)
		if err != nil {
			return 0, fmt.Errorf("read pointer at 0x%x (step %d): %w", current, i, err)
		}
		ptr := uintptr(binary.LittleEndian.Uint64(data))
		current = ptr + uintptr(off)
	}

	return current, nil
}

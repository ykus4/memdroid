package pointer

import (
	"encoding/binary"
	"fmt"

	"memdroid/internal/driver"
)

// ResolveChain resolves a saved pointer chain against the current process state.
// It finds the module base by matching Chain.BaseLabel in the process's memory
// maps, rebases the base pointer to moduleBase+BaseOffset, then walks the chain
// (read pointer, add offset) in base->final order to reach the final address.
func ResolveChain(drv driver.Driver, pid int, chain Chain) (uintptr, error) {
	regions, err := drv.ReadMaps(pid)
	if err != nil {
		return 0, fmt.Errorf("read maps: %w", err)
	}

	base := moduleBase(regions, chain.BaseLabel)
	if base == 0 {
		return 0, fmt.Errorf("module %q not found in memory maps", chain.BaseLabel)
	}

	current := base + chain.BaseOffset
	for i, off := range chain.Offsets {
		data, err := drv.Peek(pid, current, pointerSize)
		if err != nil {
			return 0, fmt.Errorf("read pointer at 0x%x (step %d): %w", current, i, err)
		}
		ptr := int64(binary.LittleEndian.Uint64(data))
		current = uintptr(ptr + off)
	}

	return current, nil
}

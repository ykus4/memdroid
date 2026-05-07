package modify

import (
	"fmt"
	"os"

	"memodroid/internal/driver"
)

// Snapshot holds a captured memory region for comparison.
type Snapshot struct {
	Addr uintptr
	Data []byte
}

// TakeSnapshot reads size bytes from addr and returns a Snapshot.
func TakeSnapshot(drv driver.Driver, pid int, addr uintptr, size int) (*Snapshot, error) {
	data, err := drv.ReadRegion(pid, addr, size)
	if err != nil {
		return nil, fmt.Errorf("snapshot 0x%x: %w", addr, err)
	}
	return &Snapshot{Addr: addr, Data: data}, nil
}

// DiffEntry represents a single byte difference between two snapshots.
type DiffEntry struct {
	Offset int
	Addr   uintptr
	Before byte
	After  byte
}

// DiffSnapshots compares two snapshots and returns all differing bytes.
// Both snapshots must have the same base address and length.
func DiffSnapshots(a, b *Snapshot) ([]DiffEntry, error) {
	if a.Addr != b.Addr {
		return nil, fmt.Errorf("diff: base address mismatch (0x%x vs 0x%x)", a.Addr, b.Addr)
	}
	minLen := len(a.Data)
	if len(b.Data) < minLen {
		minLen = len(b.Data)
	}

	var diffs []DiffEntry
	for i := 0; i < minLen; i++ {
		if a.Data[i] != b.Data[i] {
			diffs = append(diffs, DiffEntry{
				Offset: i,
				Addr:   a.Addr + uintptr(i),
				Before: a.Data[i],
				After:  b.Data[i],
			})
		}
	}
	return diffs, nil
}

// WriteDiff writes a human-readable diff report to the given path.
func WriteDiff(diffs []DiffEntry, baseAddr uintptr, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, _ = fmt.Fprintf(f, "Snapshot diff: %d bytes changed (base: 0x%x)\n", len(diffs), baseAddr)
	_, _ = fmt.Fprintf(f, "%-18s  %-8s  %-6s  %-6s\n", "Address", "Offset", "Before", "After")
	_, _ = fmt.Fprintf(f, "%-18s  %-8s  %-6s  %-6s\n", "──────────────────", "────────", "──────", "──────")
	for _, d := range diffs {
		_, _ = fmt.Fprintf(f, "0x%016x  +0x%-5x  0x%02x    0x%02x\n", d.Addr, d.Offset, d.Before, d.After)
	}
	return nil
}

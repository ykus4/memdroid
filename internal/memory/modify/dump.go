package modify

import (
	"bufio"
	"fmt"
	"os"

	"memdroid/internal/driver"
)

const (
	dumpLineWidth = 16
	dumpASCIIMin  = 0x20
	dumpASCIIMax  = 0x7e
)

// DumpRegion reads size bytes from addr in one bulk transfer and writes a
// classic 16-byte-per-line hex dump to path.
func DumpRegion(drv driver.Driver, pid int, addr uintptr, size int, path string) error {
	if size <= 0 {
		return fmt.Errorf("dump size must be positive, got %d", size)
	}

	data, err := drv.ReadRegion(pid, addr, size)
	if err != nil {
		return fmt.Errorf("dump 0x%x: %w", addr, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	w := bufio.NewWriter(f)
	for offset := 0; offset < len(data); offset += dumpLineWidth {
		end := offset + dumpLineWidth
		if end > len(data) {
			end = len(data)
		}
		writeHexLine(w, addr+uintptr(offset), data[offset:end])
	}
	return w.Flush()
}

func writeHexLine(w *bufio.Writer, addr uintptr, chunk []byte) {
	_, _ = fmt.Fprintf(w, "%016x  ", addr)
	for i := 0; i < dumpLineWidth; i++ {
		if i < len(chunk) {
			_, _ = fmt.Fprintf(w, "%02x ", chunk[i])
		} else {
			_, _ = w.WriteString("   ")
		}
		if i == dumpLineWidth/2-1 {
			_, _ = w.WriteString(" ")
		}
	}
	_, _ = w.WriteString(" |")
	for _, b := range chunk {
		if b >= dumpASCIIMin && b <= dumpASCIIMax {
			_ = w.WriteByte(b)
		} else {
			_ = w.WriteByte('.')
		}
	}
	_, _ = w.WriteString("|\n")
}

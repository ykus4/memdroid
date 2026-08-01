package modify

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"memdroid/internal/driver"
)

const (
	dumpLineWidth = 16
	dumpASCIIMin  = 0x20
	dumpASCIIMax  = 0x7e
)

// HexLine is one row of a hex dump.
type HexLine struct {
	Offset int     // byte offset from the start of the dump
	Addr   uintptr // absolute address of the first byte
	Hex    string  // space-separated hex bytes, e.g. "de ad be ef"
	ASCII  string  // printable rendering, non-printables as '.'
}

// HexLines renders data as dumpLineWidth-byte rows starting at base. It is the
// shared model behind both the on-disk dump and the Web UI's hex view.
func HexLines(base uintptr, data []byte) []HexLine {
	lines := make([]HexLine, 0, (len(data)+dumpLineWidth-1)/dumpLineWidth)
	for off := 0; off < len(data); off += dumpLineWidth {
		chunk := data[off:min(off+dumpLineWidth, len(data))]

		hexParts := make([]string, len(chunk))
		ascii := make([]byte, len(chunk))
		for i, b := range chunk {
			hexParts[i] = fmt.Sprintf("%02x", b)
			ascii[i] = asciiByte(b)
		}
		lines = append(lines, HexLine{
			Offset: off,
			Addr:   base + uintptr(off),
			Hex:    strings.Join(hexParts, " "),
			ASCII:  string(ascii),
		})
	}
	return lines
}

// asciiByte maps b to its printable representation, or '.' if it has none.
func asciiByte(b byte) byte {
	if b >= dumpASCIIMin && b <= dumpASCIIMax {
		return b
	}
	return '.'
}

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
		end := min(offset+dumpLineWidth, len(data))
		writeHexLine(w, addr+uintptr(offset), data[offset:end])
	}
	return w.Flush()
}

// writeHexLine renders one padded, column-aligned dump row. The padding and
// mid-line gutter are specific to the file format, so this does not reuse
// HexLines.
func writeHexLine(w *bufio.Writer, addr uintptr, chunk []byte) {
	_, _ = fmt.Fprintf(w, "%016x  ", addr)
	for i := range dumpLineWidth {
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
		_ = w.WriteByte(asciiByte(b))
	}
	_, _ = w.WriteString("|\n")
}

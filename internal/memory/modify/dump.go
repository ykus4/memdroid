package modify

import (
	"fmt"
	"os"

	"memodroid/internal/driver"
)

const (
	dumpWordSize = 8
	dumpASCIIMin = 0x20
	dumpASCIIMax = 0x7e
)

// DumpRegion reads size bytes from addr and writes a hex dump to path.
func DumpRegion(drv driver.Driver, pid int, addr uintptr, size int, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for offset := 0; offset < size; offset += dumpWordSize {
		remaining := size - offset
		if remaining > dumpWordSize {
			remaining = dumpWordSize
		}

		data, err := drv.Peek(pid, addr+uintptr(offset), remaining)
		if err != nil {
			fmt.Fprintf(f, "%016x  !! read error: %v\n", addr+uintptr(offset), err)
			continue
		}

		fmt.Fprintf(f, "%016x  ", addr+uintptr(offset))
		for i, b := range data {
			fmt.Fprintf(f, "%02x ", b)
			if i == dumpWordSize/2-1 {
				fmt.Fprintf(f, " ")
			}
		}
		for i := len(data); i < dumpWordSize; i++ {
			fmt.Fprintf(f, "   ")
		}
		fmt.Fprintf(f, " |")
		for _, b := range data {
			if b >= dumpASCIIMin && b <= dumpASCIIMax {
				fmt.Fprintf(f, "%c", b)
			} else {
				fmt.Fprintf(f, ".")
			}
		}
		fmt.Fprintf(f, "|\n")
	}

	fmt.Printf("Dumped %d bytes from 0x%x to %s\n", size, addr, path)
	return nil
}

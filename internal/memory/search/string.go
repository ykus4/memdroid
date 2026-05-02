package search

import (
	"fmt"
	"unicode/utf16"

	"memodroid/internal/driver"
)

// SearchStringUTF8 scans memory for an exact UTF-8 string match.
func SearchStringUTF8(drv driver.Driver, pid int, s string) {
	if s == "" {
		fmt.Println("Empty string")
		return
	}
	fmt.Printf("Searching UTF-8 %q (%d bytes)...\n", s, len(s))
	SearchPattern(drv, pid, bytesToPattern([]byte(s)))
}

// SearchStringUTF16 scans memory for a UTF-16LE encoded string match.
func SearchStringUTF16(drv driver.Driver, pid int, s string) {
	if s == "" {
		fmt.Println("Empty string")
		return
	}
	encoded := utf16.Encode([]rune(s))
	buf := make([]byte, len(encoded)*2)
	for i, u := range encoded {
		buf[i*2] = byte(u)
		buf[i*2+1] = byte(u >> 8)
	}
	fmt.Printf("Searching UTF-16LE %q (%d bytes)...\n", s, len(buf))
	SearchPattern(drv, pid, bytesToPattern(buf))
}

func bytesToPattern(b []byte) []PatternByte {
	p := make([]PatternByte, len(b))
	for i, v := range b {
		p[i] = PatternByte{Value: v}
	}
	return p
}

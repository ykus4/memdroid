package search

import (
	"fmt"
	"unicode/utf16"

	"memdroid/internal/driver"
)

// SearchStringUTF8 scans memory for an exact UTF-8 string match.
func SearchStringUTF8(drv driver.Driver, pid int, s string) ([]uintptr, error) {
	if s == "" {
		return nil, fmt.Errorf("empty string")
	}
	return SearchPattern(drv, pid, bytesToPattern([]byte(s)))
}

// SearchStringUTF16 scans memory for a UTF-16LE encoded string match.
func SearchStringUTF16(drv driver.Driver, pid int, s string) ([]uintptr, error) {
	if s == "" {
		return nil, fmt.Errorf("empty string")
	}
	return SearchPattern(drv, pid, bytesToPattern(StringBytes(s, true)))
}

// StringBytes encodes s as UTF-8 (utf16le=false) or UTF-16LE (utf16le=true).
func StringBytes(s string, utf16le bool) []byte {
	if !utf16le {
		return []byte(s)
	}
	encoded := utf16.Encode([]rune(s))
	buf := make([]byte, len(encoded)*2)
	for i, u := range encoded {
		buf[i*2] = byte(u)
		buf[i*2+1] = byte(u >> 8)
	}
	return buf
}

func bytesToPattern(b []byte) []PatternByte {
	p := make([]PatternByte, len(b))
	for i, v := range b {
		p[i] = PatternByte{Value: v}
	}
	return p
}

package search

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"

	"memdroid/internal/driver"
)

// StringEncoding selects how a string is laid out in target memory.
type StringEncoding int

const (
	EncodingUTF8 StringEncoding = iota
	EncodingUTF16LE
)

// ParseStringEncoding converts an API/CLI name to a StringEncoding. The empty
// string defaults to UTF-8.
func ParseStringEncoding(s string) (StringEncoding, error) {
	switch s {
	case "", "utf8", "utf-8":
		return EncodingUTF8, nil
	case "utf16", "utf-16", "utf16le", "utf-16le":
		return EncodingUTF16LE, nil
	}
	return 0, fmt.Errorf("unknown string encoding %q (use utf8 or utf16)", s)
}

func (e StringEncoding) String() string {
	if e == EncodingUTF16LE {
		return "utf16le"
	}
	return "utf8"
}

// StringBytes encodes s in the given encoding.
func StringBytes(s string, enc StringEncoding) []byte {
	if enc != EncodingUTF16LE {
		return []byte(s)
	}
	units := utf16.Encode([]rune(s))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	return buf
}

// SearchString scans memory for an exact string match in the given encoding.
func SearchString(drv driver.Driver, pid int, s string, enc StringEncoding) (PatternResult, error) {
	if s == "" {
		return PatternResult{}, fmt.Errorf("empty string")
	}
	return SearchPattern(drv, pid, literalPattern(StringBytes(s, enc)))
}

// literalPattern turns raw bytes into a wildcard-free Pattern.
func literalPattern(b []byte) Pattern {
	p := make(Pattern, len(b))
	for i, v := range b {
		p[i] = PatternByte{Value: v}
	}
	return p
}

package search

import (
	"fmt"
	"strconv"
	"strings"

	"memodroid/internal/driver"
)

const (
	patternMaxRegionBytes = 256 * 1024 * 1024
	patternChunkSize      = 4096
	patternMaxResults     = 200
)

// PatternByte represents one byte in a search pattern; Wildcard=true matches any byte.
type PatternByte struct {
	Value    byte
	Wildcard bool
}

// ParsePattern parses a hex pattern string like "FF 00 ?? 01" into []PatternByte.
func ParsePattern(s string) ([]PatternByte, error) {
	tokens := strings.Fields(s)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty pattern")
	}
	out := make([]PatternByte, len(tokens))
	for i, t := range tokens {
		if t == "??" || t == "?" {
			out[i] = PatternByte{Wildcard: true}
		} else {
			v, err := strconv.ParseUint(t, 16, 8)
			if err != nil {
				return nil, fmt.Errorf("invalid token %q: %w", t, err)
			}
			out[i] = PatternByte{Value: byte(v)}
		}
	}
	return out, nil
}

// SearchPattern scans all rw memory regions for the byte pattern and prints matches.
func SearchPattern(drv driver.Driver, pid int, pattern []PatternByte) {
	if len(pattern) == 0 {
		fmt.Println("Empty pattern")
		return
	}

	regions, err := drv.ReadMaps(pid)
	if err != nil {
		fmt.Printf("Failed to read memory maps: %v\n", err)
		return
	}

	found := 0
	plen := len(pattern)
	overlap := plen - 1

	for _, r := range regions {
		size := int(r.End - r.Start)
		if size <= 0 || size > patternMaxRegionBytes {
			continue
		}

		var prev []byte
		for offset := 0; offset < size; offset += patternChunkSize {
			end := offset + patternChunkSize
			if end > size {
				end = size
			}

			chunk, err := drv.ReadBytes(pid, r.Start+uintptr(offset), end-offset)
			if err != nil {
				prev = nil
				continue
			}

			buf := append(prev, chunk...)
			for i := 0; i <= len(buf)-plen; i++ {
				if matchPattern(buf[i:], pattern) {
					addr := r.Start + uintptr(offset) - uintptr(len(prev)) + uintptr(i)
					fmt.Printf("Found at 0x%x\n", addr)
					found++
					if found >= patternMaxResults {
						fmt.Println("(limit reached, stopping)")
						return
					}
				}
			}

			if len(chunk) >= overlap {
				prev = chunk[len(chunk)-overlap:]
			} else {
				prev = chunk
			}
		}
	}

	if found == 0 {
		fmt.Println("Not found")
	} else {
		fmt.Printf("Total: %d results\n", found)
	}
}

func matchPattern(buf []byte, pattern []PatternByte) bool {
	for i, p := range pattern {
		if !p.Wildcard && buf[i] != p.Value {
			return false
		}
	}
	return true
}

package store

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"

	"memdroid/internal/memory/search"
)

// ctFile represents the top-level CheatEngine .CT XML structure.
type ctFile struct {
	XMLName      xml.Name  `xml:"CheatTable"`
	CheatEntries []ctEntry `xml:"CheatEntries>CheatEntry"`
}

// ctEntry represents a single cheat entry in a .CT file.
type ctEntry struct {
	Description  string `xml:"Description"`
	Address      string `xml:"Address"`
	VariableType string `xml:"VariableType"`
}

// ImportCT parses a CheatEngine .CT file and returns bookmarks.
// It extracts address and description from each <CheatEntry> element.
func ImportCT(path string) ([]Bookmark, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CT file: %w", err)
	}

	var ct ctFile
	if err := xml.Unmarshal(data, &ct); err != nil {
		return nil, fmt.Errorf("parse CT XML: %w", err)
	}

	var bookmarks []Bookmark
	for _, entry := range ct.CheatEntries {
		addr, err := parseCTAddress(entry.Address)
		if err != nil {
			continue // skip entries with unparseable addresses
		}
		label := strings.Trim(entry.Description, "\"")
		if label == "" {
			label = fmt.Sprintf("0x%x", addr)
		}
		vt := ctVariableType(entry.VariableType)
		bookmarks = append(bookmarks, Bookmark{
			Addr:  addr,
			Label: label,
			VType: vt,
		})
	}

	if len(bookmarks) == 0 {
		return nil, fmt.Errorf("no valid cheat entries found in %s", path)
	}
	return bookmarks, nil
}

// parseCTAddress parses an address from a CT file.
// Addresses may be hex (with or without 0x prefix) or module+offset notation.
func parseCTAddress(s string) (uintptr, error) {
	s = strings.TrimSpace(s)
	// Remove quotes if present
	s = strings.Trim(s, "\"")

	// Handle module+offset like "game.exe+1A2B" — keep only the offset.
	// (A module-relative address can't be represented as an absolute bookmark
	// without rebasing; this is a known lossy import.)
	if idx := strings.LastIndex(s, "+"); idx > 0 {
		if !isHexString(s[:idx]) {
			s = s[idx+1:]
		}
	}

	// Strip 0x prefix
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")

	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid address %q: %w", s, err)
	}
	return uintptr(v), nil
}

// isHexString reports whether s is non-empty and consists only of hex digits.
func isHexString(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// ctVariableType maps CE variable type strings to our ValueType.
func ctVariableType(s string) search.ValueType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "4 bytes", "4bytes":
		return search.TypeInt32
	case "8 bytes", "8bytes":
		return search.TypeInt64
	case "float":
		return search.TypeFloat32
	case "double":
		return search.TypeFloat64
	default:
		return search.TypeInt32
	}
}

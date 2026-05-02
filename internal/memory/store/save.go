package store

import (
	"encoding/json"
	"fmt"
	"os"

	"memodroid/internal/memory/search"
)

const stateFilePerms = 0o600

type savedBookmark struct {
	Addr  uint64 `json:"addr"`
	Label string `json:"label"`
	VType int    `json:"vtype"`
}

type savedSession struct {
	PID        int               `json:"pid"`
	ValueType  int               `json:"value_type"`
	Candidates map[string][]byte `json:"candidates"`
}

type saveFile struct {
	Bookmarks []savedBookmark `json:"bookmarks"`
	Session   *savedSession   `json:"session,omitempty"`
}

// SaveState serializes bookmarks and the active search session to a JSON file.
func SaveState(path string, bl *BookmarkList, s *search.Session) error {
	sf := saveFile{}

	for _, b := range bl.Entries {
		sf.Bookmarks = append(sf.Bookmarks, savedBookmark{
			Addr:  uint64(b.Addr),
			Label: b.Label,
			VType: int(b.VType),
		})
	}

	if s != nil && s.HasCandidates() {
		sc := &savedSession{
			PID:        s.PID,
			ValueType:  int(s.ValueType),
			Candidates: make(map[string][]byte),
		}
		for addr, val := range s.Candidates {
			sc.Candidates[fmt.Sprintf("0x%x", addr)] = val
		}
		sf.Session = sc
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, stateFilePerms)
}

// LoadState deserializes bookmarks and session from a JSON file.
// The loaded session has a nil Driver; the caller must set it before searching.
func LoadState(path string, bl *BookmarkList, s **search.Session) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var sf saveFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return err
	}

	bl.Entries = nil
	for _, b := range sf.Bookmarks {
		bl.Entries = append(bl.Entries, Bookmark{
			Addr:  uintptr(b.Addr),
			Label: b.Label,
			VType: search.ValueType(b.VType),
		})
	}

	if sf.Session != nil {
		loaded := search.NewSession(sf.Session.PID, search.ValueType(sf.Session.ValueType), nil)
		loaded.Candidates = make(map[uintptr][]byte)
		for key, val := range sf.Session.Candidates {
			var addr uint64
			if _, err := fmt.Sscanf(key, "0x%x", &addr); err != nil {
				continue
			}
			loaded.Candidates[uintptr(addr)] = val
		}
		loaded.SetActive()
		*s = loaded
	}

	return nil
}

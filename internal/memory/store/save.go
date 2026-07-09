package store

import (
	"encoding/json"
	"fmt"
	"os"

	"memdroid/internal/memory/search"
)

const (
	stateFilePerms = 0o600
	stateVersion   = 1
)

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
	Version   int             `json:"version"`
	Bookmarks []savedBookmark `json:"bookmarks"`
	Session   *savedSession   `json:"session,omitempty"`
}

// SaveState serializes bookmarks and the active search session to a JSON file.
func SaveState(path string, bl *BookmarkList, s *search.Session) error {
	sf := saveFile{Version: stateVersion}

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
		for addr, val := range s.Snapshot() {
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

// LoadState deserializes bookmarks (into bl, replacing its contents) and returns
// the saved search session if present. The returned session has a nil Driver;
// the caller must set it before searching.
func LoadState(path string, bl *BookmarkList) (*search.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sf saveFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	if sf.Version != 0 && sf.Version != stateVersion {
		return nil, fmt.Errorf("unsupported state file version %d (expected %d)", sf.Version, stateVersion)
	}

	entries := make([]Bookmark, 0, len(sf.Bookmarks))
	for _, b := range sf.Bookmarks {
		entries = append(entries, Bookmark{
			Addr:  uintptr(b.Addr),
			Label: b.Label,
			VType: search.ValueType(b.VType),
		})
	}
	bl.Entries = entries

	if sf.Session == nil {
		return nil, nil
	}

	loaded := search.NewSession(sf.Session.PID, search.ValueType(sf.Session.ValueType), nil)
	cands := make(map[uintptr][]byte, len(sf.Session.Candidates))
	for key, val := range sf.Session.Candidates {
		var addr uint64
		if _, err := fmt.Sscanf(key, "0x%x", &addr); err != nil {
			continue
		}
		cands[uintptr(addr)] = val
	}
	loaded.SetCandidates(cands)
	return loaded, nil
}

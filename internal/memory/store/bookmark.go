package store

import (
	"fmt"
	"slices"
	"sync"

	"memdroid/internal/driver"
	"memdroid/internal/memory/search"
)

// Bookmark is a named address the user wants to keep track of.
type Bookmark struct {
	Addr  uintptr
	Label string
	VType search.ValueType
}

// BookmarkList is the user's saved addresses. The CLI menu loop and the HTTP
// handlers both mutate it concurrently, so every access goes through the mutex
// and callers only ever see copies of the entry slice.
type BookmarkList struct {
	mu      sync.RWMutex
	entries []Bookmark
}

func NewBookmarkList() *BookmarkList {
	return &BookmarkList{}
}

func (bl *BookmarkList) Add(addr uintptr, label string, vt search.ValueType) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.entries = append(bl.entries, Bookmark{Addr: addr, Label: label, VType: vt})
}

func (bl *BookmarkList) Remove(index int) error {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	if index < 0 || index >= len(bl.entries) {
		return fmt.Errorf("invalid index %d", index)
	}
	bl.entries = slices.Delete(bl.entries, index, index+1)
	return nil
}

// Entries returns a copy of the bookmark list, safe to iterate while other
// goroutines mutate the original.
func (bl *BookmarkList) Entries() []Bookmark {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return slices.Clone(bl.entries)
}

// Replace swaps in a whole new set of bookmarks, as when loading from disk.
func (bl *BookmarkList) Replace(entries []Bookmark) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.entries = slices.Clone(entries)
}

func (bl *BookmarkList) Get(index int) (Bookmark, bool) {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	if index < 0 || index >= len(bl.entries) {
		return Bookmark{}, false
	}
	return bl.entries[index], true
}

func (bl *BookmarkList) Len() int {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return len(bl.entries)
}

// Values reads the current value of each bookmark. Returns addr -> formatted value.
func (bl *BookmarkList) Values(drv driver.Driver, pid int) map[uintptr]string {
	entries := bl.Entries()
	out := make(map[uintptr]string, len(entries))
	for _, b := range entries {
		val := "?"
		if pid != 0 {
			if cur, err := drv.Peek(pid, b.Addr, b.VType.Size()); err == nil {
				val = search.FormatValue(cur, b.VType)
			}
		}
		out[b.Addr] = val
	}
	return out
}

// ModifyAll writes value to every bookmarked address that matches vt.
// Returns the number of successfully modified addresses.
//
// A value narrower than the type would write a partial number and leave the
// high bytes stale, so a mismatched width modifies nothing rather than
// silently corrupting every bookmark.
func (bl *BookmarkList) ModifyAll(drv driver.Driver, pid int, value []byte, vt search.ValueType) int {
	if size := vt.Size(); size != 0 && len(value) != size {
		return 0
	}
	count := 0
	for _, b := range bl.Entries() {
		if b.VType != vt {
			continue
		}
		if err := drv.Poke(pid, b.Addr, value); err == nil {
			count++
		}
	}
	return count
}

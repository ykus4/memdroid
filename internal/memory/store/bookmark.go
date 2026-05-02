package store

import (
	"fmt"

	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

type Bookmark struct {
	Addr  uintptr
	Label string
	VType search.ValueType
}

type BookmarkList struct {
	Entries []Bookmark
}

func NewBookmarkList() *BookmarkList {
	return &BookmarkList{}
}

func (bl *BookmarkList) Add(addr uintptr, label string, vt search.ValueType) {
	bl.Entries = append(bl.Entries, Bookmark{Addr: addr, Label: label, VType: vt})
}

func (bl *BookmarkList) Remove(index int) error {
	if index < 0 || index >= len(bl.Entries) {
		return fmt.Errorf("invalid index %d", index)
	}
	bl.Entries = append(bl.Entries[:index], bl.Entries[index+1:]...)
	return nil
}

// Values reads the current value of each bookmark. Returns addr -> formatted value.
func (bl *BookmarkList) Values(drv driver.Driver, pid int) map[uintptr]string {
	out := make(map[uintptr]string, len(bl.Entries))
	for _, b := range bl.Entries {
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
func (bl *BookmarkList) ModifyAll(drv driver.Driver, pid int, value []byte, vt search.ValueType) int {
	count := 0
	for _, b := range bl.Entries {
		if b.VType != vt {
			continue
		}
		if err := drv.Poke(pid, b.Addr, value); err == nil {
			count++
		}
	}
	return count
}

func (bl *BookmarkList) Get(index int) (Bookmark, bool) {
	if index < 0 || index >= len(bl.Entries) {
		return Bookmark{}, false
	}
	return bl.Entries[index], true
}

func (bl *BookmarkList) Len() int {
	return len(bl.Entries)
}

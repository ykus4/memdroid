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
	fmt.Printf("Bookmarked 0x%x (%s)\n", addr, label)
}

func (bl *BookmarkList) Remove(index int) {
	if index < 0 || index >= len(bl.Entries) {
		fmt.Println("Invalid index")
		return
	}
	bl.Entries = append(bl.Entries[:index], bl.Entries[index+1:]...)
}

func (bl *BookmarkList) List(drv driver.Driver, pid int) {
	if len(bl.Entries) == 0 {
		fmt.Println("No bookmarks")
		return
	}
	for i, b := range bl.Entries {
		cur, err := drv.Peek(pid, b.Addr, b.VType.Size())
		val := "?"
		if err == nil {
			val = search.FormatValue(cur, b.VType)
		}
		fmt.Printf("[%d] 0x%x  %-20s  %s = %s\n", i, b.Addr, b.Label, b.VType, val)
	}
}

// ModifyAll writes value to every bookmarked address that matches vt.
func (bl *BookmarkList) ModifyAll(drv driver.Driver, pid int, value []byte, vt search.ValueType) {
	count := 0
	for _, b := range bl.Entries {
		if b.VType != vt {
			continue
		}
		if err := drv.Poke(pid, b.Addr, value); err == nil {
			count++
		}
	}
	fmt.Printf("Modified %d bookmarks\n", count)
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

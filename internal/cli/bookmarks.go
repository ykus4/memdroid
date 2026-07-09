package cli

import (
	"fmt"

	"memdroid/internal/app"
	"memdroid/internal/memory/store"
)

func BookmarkList(st *app.State) {
	bl := st.GetBookmarks()
	if bl.Len() == 0 {
		fmt.Println("No bookmarks")
		return
	}
	vals := bl.Values(st.GetDriver(), st.GetPID())
	for i, b := range bl.Entries {
		fmt.Printf("[%d] 0x%x  %-20s  %s = %s\n", i, b.Addr, b.Label, b.VType, vals[b.Addr])
	}
}

func ImportCT(st *app.State) {
	path := Prompt("CT file path: ")
	if path == "" {
		fmt.Println("Path required")
		return
	}
	bookmarks, err := store.ImportCT(path)
	if err != nil {
		fmt.Printf("Import failed: %v\n", err)
		return
	}
	bl := st.GetBookmarks()
	for _, b := range bookmarks {
		bl.Add(b.Addr, b.Label, b.VType)
	}
	fmt.Printf("Imported %d bookmarks from %s\n", len(bookmarks), path)
}

package cli

import (
	"fmt"
	"os"
	"strconv"

	"memodroid/internal/app"
	"memodroid/internal/driver"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/modify"
	"memodroid/internal/memory/search"
	"memodroid/internal/memory/store"
	"memodroid/internal/process"
)

// DefaultStateFile is the default filename used by Save/Load State.
const DefaultStateFile = "memdroid.json"

// Run drives the interactive menu loop until the user selects Exit.
func Run(st *app.State, d *adb.ADB) {
	for {
		PrintMenu(st, d)
		if !dispatch(st, d, Prompt("")) {
			return
		}
	}
}

// dispatch handles a single menu choice. Returns false when the user chose Exit.
func dispatch(st *app.State, d *adb.ADB, choice string) bool {
	drv := st.GetDriver()
	pid := st.GetPID()
	vt := st.GetValueType()
	sess := st.GetSession()

	switch choice {
	// --- Device ---
	case "d":
		SelectDevice(d)
	case "dw":
		ConnectWifi(d)
	case "dd":
		DisconnectWifi(d)

	// --- Process ---
	case "1":
		if err := listProcesses(drv); err != nil {
			fmt.Printf("List failed: %v\n", err)
		}
	case "1s":
		AttachByName(st, d)
	case "2":
		Attach(st)
	case "3":
		Detach(st)
	case "3s":
		SwitchProcess(st)
	case "3l":
		ListAttached(st)
	case "4":
		if RequireAttached(pid) {
			if err := drv.Stop(pid); err != nil {
				fmt.Printf("Stop failed: %v\n", err)
			}
		}
	case "5":
		if RequireAttached(pid) {
			if err := drv.Continue(pid); err != nil {
				fmt.Printf("Continue failed: %v\n", err)
			}
		}

	// --- Search ---
	case "6":
		SetValueType(st)
	case "7":
		if !RequireAttached(pid) {
			return true
		}
		val, ok := ParseValue("Value: ", vt)
		if !ok {
			return true
		}
		if err := st.EnsureSession().Search(val); err != nil {
			fmt.Printf("Search failed: %v\n", err)
			return true
		}
		fmt.Printf("Found %d addresses\n", st.GetSession().CandidateCount())
	case "7r":
		if RequireAttached(pid) {
			SearchFiltered(st)
		}
	case "8", "9", "10", "11":
		if !RequireSession(sess) {
			return true
		}
		modes := map[string]search.FilterMode{
			"8": search.FilterChanged, "9": search.FilterUnchanged,
			"10": search.FilterIncreased, "11": search.FilterDecreased,
		}
		if err := sess.Filter(modes[choice], nil); err != nil {
			fmt.Printf("Filter failed: %v\n", err)
			return true
		}
		fmt.Printf("Remaining: %d addresses\n", sess.CandidateCount())
	case "12":
		if !RequireSession(sess) {
			return true
		}
		val, ok := ParseValue("Value: ", vt)
		if !ok {
			return true
		}
		if err := sess.Filter(search.FilterValue, val); err != nil {
			fmt.Printf("Filter failed: %v\n", err)
			return true
		}
		fmt.Printf("Remaining: %d addresses\n", sess.CandidateCount())
	case "13":
		if RequireSession(sess) {
			ShowCandidates(st)
		}
	case "14":
		if sess != nil {
			sess.Reset()
			fmt.Println("Search session reset")
		}

	// --- Pattern / String ---
	case "p":
		if RequireAttached(pid) {
			searchByPattern(drv, pid)
		}
	case "s8":
		if RequireAttached(pid) {
			searchByString(drv, pid, false)
		}
	case "s16":
		if RequireAttached(pid) {
			searchByString(drv, pid, true)
		}
	case "sw":
		if RequireAttached(pid) {
			modifyStringAt(drv, pid)
		}

	// --- Memory ---
	case "15":
		if RequireAttached(pid) {
			modifyAt(st, drv, pid, vt)
		}
	case "16":
		if err := st.UndoStack.Undo(); err != nil {
			fmt.Printf("Undo: %v\n", err)
		} else {
			fmt.Printf("Undone (remaining depth: %d)\n", st.UndoStack.Depth())
		}
	case "17":
		if RequireAttached(pid) {
			freezeAt(st, drv, pid, vt)
		}
	case "17i":
		SetFreezeInterval(st)
	case "17a":
		if RequireSession(sess) {
			count := st.Freezer.FreezeAllCandidates(drv, sess)
			fmt.Printf("Freezing %d addresses\n", count)
		}
	case "18":
		if addr, ok := ParseAddr("Address (hex): "); ok {
			if err := st.Freezer.Unfreeze(addr); err != nil {
				fmt.Printf("Unfreeze: %v\n", err)
			} else {
				fmt.Printf("Unfrozen 0x%x\n", addr)
			}
		}
	case "19":
		printAddrList(st.Freezer.List(), "No frozen addresses")
	case "20":
		if RequireAttached(pid) {
			Watch(st)
		}
	case "21":
		if addr, ok := ParseAddr("Address (hex): "); ok {
			if err := st.Watcher.Unwatch(addr); err != nil {
				fmt.Printf("Unwatch: %v\n", err)
			} else {
				fmt.Printf("Stopped watching 0x%x\n", addr)
			}
		}
	case "22":
		printAddrList(st.Watcher.List(), "No watched addresses")
	case "22a":
		if RequireAttached(pid) {
			SetAlert(st)
		}
	case "22r":
		RemoveAlert(st)
	case "23":
		if RequireAttached(pid) {
			Dump(st)
		}
	case "23d":
		if RequireAttached(pid) {
			SnapshotDiff(st)
		}
	case "23m":
		if RequireAttached(pid) {
			ShowMaps(st)
		}

	// --- Pointer ---
	case "pt":
		if RequireAttached(pid) {
			PointerScan(st)
		}
	case "pr":
		if RequireAttached(pid) {
			PointerResolve(st)
		}

	// --- Bookmarks ---
	case "24":
		if RequireAttached(pid) {
			addBookmark(st, vt)
		}
	case "25":
		BookmarkList(st)
	case "26":
		if RequireAttached(pid) {
			modifyAllBookmarks(st, drv, pid, vt)
		}
	case "27":
		BookmarkList(st)
		idx, err := strconv.Atoi(Prompt("Index to remove: "))
		if err != nil {
			fmt.Println("Invalid index")
			return true
		}
		if err := st.GetBookmarks().Remove(idx); err != nil {
			fmt.Printf("Remove: %v\n", err)
		}

	// --- Import ---
	case "ct":
		ImportCT(st)

	// --- Session ---
	case "28":
		saveSession(st)
	case "29":
		loadSession(st)

	case "0":
		shutdown(st, drv, pid)
		os.Exit(0)

	default:
		fmt.Println("Invalid choice")
	}
	return true
}

// --- inline helpers kept out of the switch for readability ---

func listProcesses(drv driver.Driver) error {
	return process.List(drv)
}

func searchByPattern(drv driver.Driver, pid int) {
	pat, err := search.ParsePattern(Prompt("Pattern (e.g. FF 00 ?? 01): "))
	if err != nil {
		fmt.Printf("Invalid pattern: %v\n", err)
		return
	}
	results, err := search.SearchPattern(drv, pid, pat)
	if err != nil {
		fmt.Printf("Pattern search failed: %v\n", err)
		return
	}
	printAddrResults(results)
}

func searchByString(drv driver.Driver, pid int, utf16 bool) {
	label := "String (UTF-8): "
	if utf16 {
		label = "String (UTF-16LE): "
	}
	var (
		results []uintptr
		err     error
	)
	if utf16 {
		results, err = search.SearchStringUTF16(drv, pid, Prompt(label))
	} else {
		results, err = search.SearchStringUTF8(drv, pid, Prompt(label))
	}
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}
	printAddrResults(results)
}

func modifyStringAt(drv driver.Driver, pid int) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	if err := modify.WriteString(drv, pid, addr, Prompt("New string (UTF-8): ")); err != nil {
		fmt.Printf("Modify failed: %v\n", err)
	} else {
		fmt.Println("Modified successfully")
	}
}

func modifyAt(st *app.State, drv driver.Driver, pid int, vt search.ValueType) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	val, ok := ParseValue("New value: ", vt)
	if !ok {
		return
	}
	if err := st.UndoStack.WithUndo(drv, pid, addr, val, vt); err != nil {
		fmt.Printf("Modify failed: %v\n", err)
	} else {
		fmt.Printf("Modified 0x%x (undo available, depth: %d)\n", addr, st.UndoStack.Depth())
	}
}

func freezeAt(st *app.State, drv driver.Driver, pid int, vt search.ValueType) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	val, ok := ParseValue("Value to freeze: ", vt)
	if !ok {
		return
	}
	if err := st.Freezer.Freeze(drv, pid, addr, val); err != nil {
		fmt.Printf("Freeze failed: %v\n", err)
	} else {
		fmt.Printf("Freezing 0x%x\n", addr)
	}
}

func addBookmark(st *app.State, vt search.ValueType) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	st.GetBookmarks().Add(addr, Prompt("Label: "), vt)
	fmt.Printf("Bookmarked 0x%x\n", addr)
}

func modifyAllBookmarks(st *app.State, drv driver.Driver, pid int, vt search.ValueType) {
	val, ok := ParseValue("Value: ", vt)
	if !ok {
		return
	}
	count := st.GetBookmarks().ModifyAll(drv, pid, val, vt)
	fmt.Printf("Modified %d bookmarks\n", count)
}

func saveSession(st *app.State) {
	path := Prompt(fmt.Sprintf("Save file [default: %s]: ", DefaultStateFile))
	if path == "" {
		path = DefaultStateFile
	}
	if err := store.SaveState(path, st.GetBookmarks(), st.GetSession()); err != nil {
		fmt.Printf("Save failed: %v\n", err)
	} else {
		fmt.Printf("Saved to %s\n", path)
	}
}

func loadSession(st *app.State) {
	path := Prompt(fmt.Sprintf("Load file [default: %s]: ", DefaultStateFile))
	if path == "" {
		path = DefaultStateFile
	}
	loaded := st.GetSession()
	if err := store.LoadState(path, st.GetBookmarks(), &loaded); err != nil {
		fmt.Printf("Load failed: %v\n", err)
		return
	}
	if loaded != nil {
		loaded.Driver = st.GetDriver()
	}
	st.SetSession(loaded)
	fmt.Printf("Loaded from %s\n", path)
}

func shutdown(st *app.State, drv driver.Driver, pid int) {
	st.Freezer.UnfreezeAll()
	st.Watcher.UnwatchAll()
	if pid != 0 {
		drv.Detach(pid)
	}
	fmt.Println("Bye!")
}

func printAddrList(addrs []uintptr, emptyMsg string) {
	if len(addrs) == 0 {
		fmt.Println(emptyMsg)
		return
	}
	for _, addr := range addrs {
		fmt.Printf("  0x%x\n", addr)
	}
}

func printAddrResults(addrs []uintptr) {
	for _, addr := range addrs {
		fmt.Printf("  Found at 0x%x\n", addr)
	}
	fmt.Printf("Total: %d results\n", len(addrs))
}

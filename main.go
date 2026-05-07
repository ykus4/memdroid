package main

import (
	"fmt"
	"os"
	"strconv"

	"memodroid/internal/app"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/modify"
	"memodroid/internal/memory/search"
	"memodroid/internal/memory/store"
	"memodroid/internal/memory/watch"
	"memodroid/internal/process"
	"memodroid/internal/server"
)

const (
	defaultWatchInterval = "500ms"
	defaultDumpFile      = "dump.hex"
	defaultStateFile     = "memdroid.json"
	defaultServerAddr    = ":8080"
	maxCandidatesDisplay = 50
)

func main() {
	d := adb.New()

	if devices, err := d.ListDevices(); err == nil {
		switch len(devices) {
		case 0:
			fmt.Println("Warning: no ADB devices connected.")
		case 1:
			if err := d.SelectDevice(devices[0]); err == nil {
				fmt.Printf("Auto-selected device: %s\n", devices[0])
			}
		default:
			handleSelectDevice(d)
		}
	}

	st := app.NewState(d)

	st.Watcher.OnChange = func(ev watch.ChangeEvent) {
		fmt.Printf("[Watch] 0x%x: %s -> %s\n", ev.Addr, ev.Prev, ev.Cur)
	}

	st.AlertWatcher.OnAlert = func(ev watch.AlertEvent) {
		action := "notify"
		if ev.Triggered {
			action = "WRITE"
		}
		fmt.Printf("[Alert] 0x%x: condition=%s value=%s action=%s\n", ev.Addr, ev.Condition, ev.Value, action)
	}

	go func() {
		if err := server.Start(defaultServerAddr, st, d); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		}
	}()

	for {
		printMenu(st, d)
		choice := prompt("")

		drv := st.GetDriver()
		pid := st.GetPID()
		vt := st.GetValueType()
		sess := st.GetSession()

		switch choice {
		// --- Device ---
		case "d":
			handleSelectDevice(d)
		case "dw":
			handleConnectWifi(d)
		case "dd":
			handleDisconnectWifi(d)

		// --- Process ---
		case "1":
			if err := process.List(drv); err != nil {
				fmt.Printf("List failed: %v\n", err)
			}
		case "1s":
			handleAttachByName(st, d)
		case "2":
			handleAttach(st)
		case "3":
			handleDetach(st)
		case "4":
			if requireAttached(pid) {
				if err := drv.Stop(pid); err != nil {
					fmt.Printf("Stop failed: %v\n", err)
				}
			}
		case "5":
			if requireAttached(pid) {
				if err := drv.Continue(pid); err != nil {
					fmt.Printf("Continue failed: %v\n", err)
				}
			}

		// --- Search ---
		case "6":
			handleSetValueType(st)
		case "7":
			if !requireAttached(pid) {
				continue
			}
			val, ok := parseValue("Value: ", vt)
			if !ok {
				continue
			}
			if err := st.EnsureSession().Search(val); err != nil {
				fmt.Printf("Search failed: %v\n", err)
				continue
			}
			fmt.Printf("Found %d addresses\n", st.GetSession().CandidateCount())
		case "7r":
			if !requireAttached(pid) {
				continue
			}
			handleSearchFiltered(st)
		case "8", "9", "10", "11":
			if !requireSession(sess) {
				continue
			}
			modes := map[string]search.FilterMode{
				"8": search.FilterChanged, "9": search.FilterUnchanged,
				"10": search.FilterIncreased, "11": search.FilterDecreased,
			}
			if err := sess.Filter(modes[choice], nil); err != nil {
				fmt.Printf("Filter failed: %v\n", err)
				continue
			}
			fmt.Printf("Remaining: %d addresses\n", sess.CandidateCount())
		case "12":
			if !requireSession(sess) {
				continue
			}
			val, ok := parseValue("Value: ", vt)
			if !ok {
				continue
			}
			if err := sess.Filter(search.FilterValue, val); err != nil {
				fmt.Printf("Filter failed: %v\n", err)
				continue
			}
			fmt.Printf("Remaining: %d addresses\n", sess.CandidateCount())
		case "13":
			if requireSession(sess) {
				handleShowCandidates(st)
			}
		case "14":
			if sess != nil {
				sess.Reset()
				fmt.Println("Search session reset")
			}

		// --- Pattern / String ---
		case "p":
			if !requireAttached(pid) {
				continue
			}
			pat, err := search.ParsePattern(prompt("Pattern (e.g. FF 00 ?? 01): "))
			if err != nil {
				fmt.Printf("Invalid pattern: %v\n", err)
				continue
			}
			results, err := search.SearchPattern(drv, pid, pat)
			if err != nil {
				fmt.Printf("Pattern search failed: %v\n", err)
				continue
			}
			for _, addr := range results {
				fmt.Printf("  Found at 0x%x\n", addr)
			}
			fmt.Printf("Total: %d results\n", len(results))
		case "s8":
			if !requireAttached(pid) {
				continue
			}
			results, err := search.SearchStringUTF8(drv, pid, prompt("String (UTF-8): "))
			if err != nil {
				fmt.Printf("Search failed: %v\n", err)
				continue
			}
			for _, addr := range results {
				fmt.Printf("  Found at 0x%x\n", addr)
			}
			fmt.Printf("Total: %d results\n", len(results))
		case "s16":
			if !requireAttached(pid) {
				continue
			}
			results, err := search.SearchStringUTF16(drv, pid, prompt("String (UTF-16LE): "))
			if err != nil {
				fmt.Printf("Search failed: %v\n", err)
				continue
			}
			for _, addr := range results {
				fmt.Printf("  Found at 0x%x\n", addr)
			}
			fmt.Printf("Total: %d results\n", len(results))
		case "sw":
			if !requireAttached(pid) {
				continue
			}
			addr, ok := parseAddr("Address (hex): ")
			if !ok {
				continue
			}
			if err := modify.WriteString(drv, pid, addr, prompt("New string (UTF-8): ")); err != nil {
				fmt.Printf("Modify failed: %v\n", err)
			} else {
				fmt.Println("Modified successfully")
			}

		// --- Memory ---
		case "15":
			if !requireAttached(pid) {
				continue
			}
			addr, ok := parseAddr("Address (hex): ")
			if !ok {
				continue
			}
			val, ok := parseValue("New value: ", vt)
			if !ok {
				continue
			}
			if err := st.UndoStack.WithUndo(drv, pid, addr, val, vt); err != nil {
				fmt.Printf("Modify failed: %v\n", err)
			} else {
				fmt.Printf("Modified 0x%x (undo available, depth: %d)\n", addr, st.UndoStack.Depth())
			}
		case "16":
			if err := st.UndoStack.Undo(); err != nil {
				fmt.Printf("Undo: %v\n", err)
			} else {
				fmt.Printf("Undone (remaining depth: %d)\n", st.UndoStack.Depth())
			}
		case "17":
			if !requireAttached(pid) {
				continue
			}
			addr, ok := parseAddr("Address (hex): ")
			if !ok {
				continue
			}
			val, ok := parseValue("Value to freeze: ", vt)
			if !ok {
				continue
			}
			if err := st.Freezer.Freeze(drv, pid, addr, val); err != nil {
				fmt.Printf("Freeze failed: %v\n", err)
			} else {
				fmt.Printf("Freezing 0x%x\n", addr)
			}
		case "17i":
			handleSetFreezeInterval(st)
		case "17a":
			if requireSession(sess) {
				count := st.Freezer.FreezeAllCandidates(drv, sess)
				fmt.Printf("Freezing %d addresses\n", count)
			}
		case "18":
			if addr, ok := parseAddr("Address (hex): "); ok {
				if err := st.Freezer.Unfreeze(addr); err != nil {
					fmt.Printf("Unfreeze: %v\n", err)
				} else {
					fmt.Printf("Unfrozen 0x%x\n", addr)
				}
			}
		case "19":
			addrs := st.Freezer.List()
			if len(addrs) == 0 {
				fmt.Println("No frozen addresses")
				continue
			}
			for _, addr := range addrs {
				fmt.Printf("  0x%x\n", addr)
			}
		case "20":
			if requireAttached(pid) {
				handleWatch(st)
			}
		case "21":
			if addr, ok := parseAddr("Address (hex): "); ok {
				if err := st.Watcher.Unwatch(addr); err != nil {
					fmt.Printf("Unwatch: %v\n", err)
				} else {
					fmt.Printf("Stopped watching 0x%x\n", addr)
				}
			}
		case "22":
			addrs := st.Watcher.List()
			if len(addrs) == 0 {
				fmt.Println("No watched addresses")
				continue
			}
			for _, addr := range addrs {
				fmt.Printf("  0x%x\n", addr)
			}
		case "22a":
			if requireAttached(pid) {
				handleSetAlert(st)
			}
		case "22r":
			handleRemoveAlert(st)
		case "23":
			if requireAttached(pid) {
				handleDump(st)
			}
		case "23d":
			if requireAttached(pid) {
				handleSnapshotDiff(st)
			}
		case "23m":
			if requireAttached(pid) {
				handleShowMaps(st)
			}

		// --- Pointer ---
		case "pt":
			if requireAttached(pid) {
				handlePointerScan(st)
			}
		case "pr":
			if requireAttached(pid) {
				handlePointerResolve(st)
			}

		// --- Bookmarks ---
		case "24":
			if !requireAttached(pid) {
				continue
			}
			addr, ok := parseAddr("Address (hex): ")
			if !ok {
				continue
			}
			st.GetBookmarks().Add(addr, prompt("Label: "), vt)
			fmt.Printf("Bookmarked 0x%x\n", addr)
		case "25":
			handleBookmarkList(st)
		case "26":
			if !requireAttached(pid) {
				continue
			}
			val, ok := parseValue("Value: ", vt)
			if !ok {
				continue
			}
			count := st.GetBookmarks().ModifyAll(drv, pid, val, vt)
			fmt.Printf("Modified %d bookmarks\n", count)
		case "27":
			handleBookmarkList(st)
			idx, err := strconv.Atoi(prompt("Index to remove: "))
			if err != nil {
				fmt.Println("Invalid index")
				continue
			}
			if err := st.GetBookmarks().Remove(idx); err != nil {
				fmt.Printf("Remove: %v\n", err)
			}

		// --- Session ---
		case "28":
			path := prompt(fmt.Sprintf("Save file [default: %s]: ", defaultStateFile))
			if path == "" {
				path = defaultStateFile
			}
			if err := store.SaveState(path, st.GetBookmarks(), st.GetSession()); err != nil {
				fmt.Printf("Save failed: %v\n", err)
			} else {
				fmt.Printf("Saved to %s\n", path)
			}
		case "29":
			path := prompt(fmt.Sprintf("Load file [default: %s]: ", defaultStateFile))
			if path == "" {
				path = defaultStateFile
			}
			loaded := st.GetSession()
			if err := store.LoadState(path, st.GetBookmarks(), &loaded); err != nil {
				fmt.Printf("Load failed: %v\n", err)
			} else {
				if loaded != nil {
					loaded.Driver = st.GetDriver()
				}
				st.SetSession(loaded)
				fmt.Printf("Loaded from %s\n", path)
			}

		case "0":
			st.Freezer.UnfreezeAll()
			st.Watcher.UnwatchAll()
			if pid != 0 {
				drv.Detach(pid)
			}
			fmt.Println("Bye!")
			os.Exit(0)

		default:
			fmt.Println("Invalid choice")
		}
	}
}

func printMenu(st *app.State, d *adb.ADB) {
	pid := st.GetPID()
	vt := st.GetValueType()
	sess := st.GetSession()

	fmt.Println("\n=== MemoDroid ===")
	serial := d.DeviceSerial()
	if serial == "" {
		serial = "(none)"
	}
	fmt.Printf("Device: %s\n", serial)
	if pid != 0 {
		fmt.Printf("PID: %d  Type: %s", pid, vt)
		if sess != nil && sess.HasCandidates() {
			fmt.Printf("  Candidates: %d", sess.CandidateCount())
		}
		if st.UndoStack.Depth() > 0 {
			fmt.Printf("  Undo: %d", st.UndoStack.Depth())
		}
		fmt.Println()
	}
	fmt.Printf("Web UI: http://localhost%s\n", defaultServerAddr)
	fmt.Println("--- Device ---")
	fmt.Println("  d. Select ADB Device")
	fmt.Println(" dw. Connect Wi-Fi (host:port)")
	fmt.Println(" dd. Disconnect Wi-Fi")
	fmt.Println("--- Process ---")
	fmt.Println("  1. Process List")
	fmt.Println(" 1s. Attach by Name")
	fmt.Println("  2. Attach by PID")
	fmt.Println("  3. Detach")
	fmt.Println("  4. Stop Process")
	fmt.Println("  5. Continue Process")
	fmt.Println("--- Search ---")
	fmt.Println("  6. Set Value Type  (current:", vt, ")")
	fmt.Println("  7. Search Value")
	fmt.Println(" 7r. Search Value (Region filtered)")
	fmt.Println("  8. Filter: Changed")
	fmt.Println("  9. Filter: Unchanged")
	fmt.Println(" 10. Filter: Increased")
	fmt.Println(" 11. Filter: Decreased")
	fmt.Println(" 12. Filter: Specific Value")
	fmt.Println(" 13. Show Candidates")
	fmt.Println(" 14. Reset Search")
	fmt.Println("--- Pattern / String ---")
	fmt.Println("  p.  Byte Pattern Search")
	fmt.Println("  s8. String Search UTF-8")
	fmt.Println(" s16. String Search UTF-16LE")
	fmt.Println("  sw. Modify String at Address")
	fmt.Println("--- Memory ---")
	fmt.Println(" 15. Modify Address")
	fmt.Println(" 16. Undo Last Modify")
	fmt.Println(" 17. Freeze Address")
	fmt.Println("17i. Set Freeze Interval")
	fmt.Println("17a. Freeze All Candidates")
	fmt.Println(" 18. Unfreeze Address")
	fmt.Println(" 19. List Frozen")
	fmt.Println(" 20. Watch Address")
	fmt.Println(" 21. Unwatch Address")
	fmt.Println(" 22. List Watched")
	fmt.Println("22a. Set Alert (conditional watch)")
	fmt.Println("22r. Remove Alert")
	fmt.Println(" 23. Dump Memory Region")
	fmt.Println("23d. Snapshot Diff")
	fmt.Println("23m. Show Memory Maps")
	fmt.Println("--- Pointer ---")
	fmt.Println(" pt. Pointer Scan")
	fmt.Println(" pr. Resolve Pointer Chain")
	fmt.Println("--- Bookmarks ---")
	fmt.Println(" 24. Add Bookmark")
	fmt.Println(" 25. List Bookmarks")
	fmt.Println(" 26. Modify All Bookmarks")
	fmt.Println(" 27. Remove Bookmark")
	fmt.Println("--- Session ---")
	fmt.Println(" 28. Save State")
	fmt.Println(" 29. Load State")
	fmt.Println("--- ---")
	fmt.Println("  0. Exit")
	fmt.Print("> ")
}

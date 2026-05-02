package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"memodroid/internal/app"
	"memodroid/internal/driver"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/modify"
	"memodroid/internal/memory/pointer"
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

var stdinReader = bufio.NewReader(os.Stdin)

func prompt(label string) string {
	fmt.Print(label)
	s, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(s)
}

func parseAddr(label string) (uintptr, bool) {
	v, err := strconv.ParseUint(prompt(label), 16, 64)
	if err != nil {
		fmt.Println("Invalid address")
		return 0, false
	}
	return uintptr(v), true
}

func parseValue(label string, vt search.ValueType) ([]byte, bool) {
	val, err := search.ParseValue(prompt(label), vt)
	if err != nil {
		fmt.Println("Invalid value")
		return nil, false
	}
	return val, true
}

func requireAttached(pid int) bool {
	if pid == 0 {
		fmt.Println("No process attached")
		return false
	}
	return true
}

func requireSession(s *search.Session) bool {
	if s == nil || !s.HasCandidates() {
		fmt.Println("No active search session. Run Search first.")
		return false
	}
	return true
}

// --- device ---

func handleSelectDevice(d *adb.ADB) {
	devices, err := d.ListDevices()
	if err != nil {
		fmt.Printf("Failed to list devices: %v\n", err)
		return
	}
	if len(devices) == 0 {
		fmt.Println("No devices connected")
		return
	}
	fmt.Println("Connected devices:")
	for i, s := range devices {
		fmt.Printf("  %d. %s\n", i+1, s)
	}
	idx, err := strconv.Atoi(prompt("Select device number: "))
	if err != nil || idx < 1 || idx > len(devices) {
		fmt.Println("Invalid selection")
		return
	}
	serial := devices[idx-1]
	if err := d.SelectDevice(serial); err != nil {
		fmt.Printf("Select device failed: %v\n", err)
		return
	}
	fmt.Printf("Using device: %s\n", serial)
}

func handleConnectWifi(d *adb.ADB) {
	addr := prompt("Host:port (e.g. 192.168.1.5:5555): ")
	if err := d.ConnectWifi(addr); err != nil {
		fmt.Printf("Connect failed: %v\n", err)
		return
	}
	fmt.Printf("Connected to %s\n", addr)
}

func handleDisconnectWifi(d *adb.ADB) {
	addr := prompt("Host:port to disconnect: ")
	if err := d.DisconnectWifi(addr); err != nil {
		fmt.Printf("Disconnect failed: %v\n", err)
		return
	}
	fmt.Printf("Disconnected from %s\n", addr)
}

// --- attach helpers ---

func doAttach(st *app.State, pid int, name string) {
	drv := st.GetDriver()
	if err := drv.Attach(pid); err != nil {
		fmt.Printf("Attach failed: %v\n", err)
		return
	}
	st.SetPID(pid)
	st.SetSession(search.NewSession(pid, st.GetValueType(), drv))
	if name != "" {
		fmt.Printf("Attached to %s (PID %d)\n", name, pid)
	} else {
		fmt.Printf("Attached to PID %d\n", pid)
	}
}

func handleAttach(st *app.State) {
	pid, err := strconv.Atoi(prompt("PID: "))
	if err != nil {
		fmt.Println("Invalid PID")
		return
	}
	doAttach(st, pid, "")
}

func handleAttachByName(st *app.State, d *adb.ADB) {
	name := prompt("Process name (partial match): ")
	matches, err := d.FindProcessByName(name)
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}
	if len(matches) == 0 {
		fmt.Println("No matching process found")
		return
	}
	if len(matches) == 1 {
		doAttach(st, matches[0].PID, matches[0].Name)
		return
	}
	for i, p := range matches {
		fmt.Printf("  %d. [%d] %s\n", i+1, p.PID, p.Name)
	}
	idx, err := strconv.Atoi(prompt("Select: "))
	if err != nil || idx < 1 || idx > len(matches) {
		fmt.Println("Invalid selection")
		return
	}
	doAttach(st, matches[idx-1].PID, matches[idx-1].Name)
}

func handleDetach(st *app.State) {
	pid := st.GetPID()
	if !requireAttached(pid) {
		return
	}
	st.Freezer.UnfreezeAll()
	st.Watcher.UnwatchAll()
	st.GetDriver().Detach(pid)
	fmt.Printf("Detached from PID %d\n", pid)
	st.SetPID(0)
	st.SetSession(nil)
}

func handleSetValueType(st *app.State) {
	fmt.Println("Types: 1=int32  2=int64  3=float32  4=float64  5=uint32  6=uint64  7=bytes")
	var vt search.ValueType
	switch prompt("Type: ") {
	case "1":
		vt = search.TypeInt32
	case "2":
		vt = search.TypeInt64
	case "3":
		vt = search.TypeFloat32
	case "4":
		vt = search.TypeFloat64
	case "5":
		vt = search.TypeUint32
	case "6":
		vt = search.TypeUint64
	case "7":
		vt = search.TypeBytes
	default:
		fmt.Println("Invalid type")
		return
	}
	st.SetValueType(vt)
	if sess := st.GetSession(); sess != nil {
		st.SetSession(search.NewSession(st.GetPID(), vt, st.GetDriver()))
		fmt.Println("Search session reset for new type")
	}
}

func handleSearchFiltered(st *app.State) {
	fmt.Println("Region: 1=all  2=heap  3=stack  4=anon  5=custom range")
	var filter driver.RegionFilter
	var customStart, customEnd uintptr
	switch prompt("Region: ") {
	case "2":
		filter = driver.RegionHeap
	case "3":
		filter = driver.RegionStack
	case "4":
		filter = driver.RegionAnon
	case "5":
		filter = driver.RegionCustom
		var ok bool
		if customStart, ok = parseAddr("Start address (hex): "); !ok {
			return
		}
		if customEnd, ok = parseAddr("End address (hex): "); !ok {
			return
		}
	default:
		filter = driver.RegionAll
	}
	val, ok := parseValue("Value: ", st.GetValueType())
	if !ok {
		return
	}
	if err := st.EnsureSession().SearchFiltered(val, filter, customStart, customEnd); err != nil {
		fmt.Printf("Search failed: %v\n", err)
	}
}

func handleShowCandidates(st *app.State) {
	sess := st.GetSession()
	vt := st.GetValueType()
	candidates := sess.Snapshot()
	if len(candidates) == 0 {
		fmt.Println("No candidates")
		return
	}
	shown := 0
	for addr, val := range candidates {
		fmt.Printf("  0x%x = %s\n", addr, search.FormatValue(val, vt))
		shown++
		if shown >= maxCandidatesDisplay {
			fmt.Printf("  ... (%d total)\n", len(candidates))
			break
		}
	}
}

func handleWatch(st *app.State) {
	addr, ok := parseAddr("Address (hex): ")
	if !ok {
		return
	}
	intervalStr := prompt(fmt.Sprintf("Interval [default: %s]: ", defaultWatchInterval))
	if intervalStr == "" {
		intervalStr = defaultWatchInterval
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		fmt.Println("Invalid interval")
		return
	}
	if err := st.Watcher.Watch(st.GetDriver(), st.GetPID(), addr, st.GetValueType(), interval); err != nil {
		fmt.Printf("Watch failed: %v\n", err)
		return
	}
	fmt.Printf("Watching 0x%x (%s) every %v\n", addr, st.GetValueType(), interval)
}

func handleDump(st *app.State) {
	addr, ok := parseAddr("Start address (hex): ")
	if !ok {
		return
	}
	size, err := strconv.Atoi(prompt("Size (bytes, decimal): "))
	if err != nil || size <= 0 {
		fmt.Println("Invalid size")
		return
	}
	path := prompt(fmt.Sprintf("Output file [default: %s]: ", defaultDumpFile))
	if path == "" {
		path = defaultDumpFile
	}
	if err := modify.DumpRegion(st.GetDriver(), st.GetPID(), addr, size, path); err != nil {
		fmt.Printf("Dump failed: %v\n", err)
	} else {
		fmt.Printf("Dumped %d bytes from 0x%x to %s\n", size, addr, path)
	}
}

func handlePointerScan(st *app.State) {
	addr, ok := parseAddr("Target address (hex): ")
	if !ok {
		return
	}
	depthStr := prompt("Max depth [default: 5]: ")
	maxDepth := pointer.DefaultMaxDepth
	if depthStr != "" {
		if v, err := strconv.Atoi(depthStr); err == nil && v > 0 {
			maxDepth = v
		}
	}
	offsetStr := prompt(fmt.Sprintf("Max offset [default: 0x%x]: ", pointer.DefaultMaxOffset))
	maxOffset := uintptr(pointer.DefaultMaxOffset)
	if offsetStr != "" {
		if v, err := strconv.ParseUint(offsetStr, 0, 64); err == nil {
			maxOffset = uintptr(v)
		}
	}
	fmt.Println("Scanning... (this may take a while)")
	result, err := pointer.Scan(st.GetDriver(), st.GetPID(), addr, maxDepth, maxOffset)
	if err != nil {
		fmt.Printf("Pointer scan failed: %v\n", err)
		return
	}
	if len(result.Chains) == 0 {
		fmt.Println("No pointer chains found")
		return
	}
	fmt.Printf("Found %d chains:\n", len(result.Chains))
	for i, c := range result.Chains {
		fmt.Printf("  [%d] %s\n", i+1, pointer.FormatChain(c))
	}
}

func handleShowMaps(st *app.State) {
	regions, err := st.GetDriver().ReadMaps(st.GetPID())
	if err != nil {
		fmt.Printf("Failed to read maps: %v\n", err)
		return
	}
	for _, r := range regions {
		name := r.Name
		if name == "" {
			name = "[anon]"
		}
		fmt.Printf("  0x%x - 0x%x  (%d KB)  %s\n", r.Start, r.End, (r.End-r.Start)/1024, name)
	}
}

func handleBookmarkList(st *app.State) {
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

// --- main ---

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
		case "23":
			if requireAttached(pid) {
				handleDump(st)
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
	fmt.Println("17a. Freeze All Candidates")
	fmt.Println(" 18. Unfreeze Address")
	fmt.Println(" 19. List Frozen")
	fmt.Println(" 20. Watch Address")
	fmt.Println(" 21. Unwatch Address")
	fmt.Println(" 22. List Watched")
	fmt.Println(" 23. Dump Memory Region")
	fmt.Println("23m. Show Memory Maps")
	fmt.Println("--- Pointer ---")
	fmt.Println(" pt. Pointer Scan")
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

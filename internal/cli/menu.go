package cli

import (
	"fmt"

	"memdroid/internal/app"
	"memdroid/internal/driver"
	"memdroid/internal/driver/adb"
	"memdroid/internal/memory/search"
)

// ServerURL is displayed in the menu header. Set by the caller before rendering.
var ServerURL = "http://localhost:8080"

// cmd is the context handed to a menu action. Every field is resolved fresh
// when the key is pressed, so an action never sees state left over from an
// earlier iteration of the menu loop.
type cmd struct {
	st   *app.State
	d    *adb.ADB
	drv  driver.Driver
	pid  int
	vt   search.ValueType
	sess *search.Session
}

func newCmd(st *app.State, d *adb.ADB) *cmd {
	return &cmd{
		st:   st,
		d:    d,
		drv:  st.GetDriver(),
		pid:  st.GetPID(),
		vt:   st.GetValueType(),
		sess: st.GetSession(),
	}
}

type action func(*cmd)

// attached runs fn only when a process is attached.
func attached(fn action) action {
	return func(c *cmd) {
		if c.pid == 0 {
			fmt.Println("No process attached")
			return
		}
		fn(c)
	}
}

// withSession runs fn only when there is a search session holding candidates.
func withSession(fn action) action {
	return func(c *cmd) {
		if c.sess == nil || !c.sess.HasCandidates() {
			fmt.Println("No active search session. Run Search first.")
			return
		}
		fn(c)
	}
}

type menuItem struct {
	key    string
	label  string
	action action
}

type menuSection struct {
	title string
	items []menuItem
}

// menu is both the rendered menu and the dispatch table. Keeping the label and
// its behaviour on the same line means a key can never appear in one and be
// missing from the other.
var menu = []menuSection{
	{"Device", []menuItem{
		{"d", "Select ADB Device", func(c *cmd) { SelectDevice(c.d) }},
		{"dw", "Connect Wi-Fi (host:port)", func(c *cmd) { ConnectWifi(c.d) }},
		{"dd", "Disconnect Wi-Fi", func(c *cmd) { DisconnectWifi(c.d) }},
	}},
	{"Process", []menuItem{
		{"1", "Process List", func(c *cmd) { listProcesses(c.drv) }},
		{"1s", "Attach by Name", func(c *cmd) { AttachByName(c.st, c.d) }},
		{"2", "Attach by PID", func(c *cmd) { Attach(c.st) }},
		{"3", "Detach", func(c *cmd) { Detach(c.st) }},
		{"3s", "Switch Active Process", func(c *cmd) { SwitchProcess(c.st) }},
		{"3l", "List Attached Processes", func(c *cmd) { ListAttached(c.st) }},
		{"4", "Stop Process", attached(func(c *cmd) { report("Stop", c.drv.Stop(c.pid)) })},
		{"5", "Continue Process", attached(func(c *cmd) { report("Continue", c.drv.Continue(c.pid)) })},
	}},
	{"Search", []menuItem{
		{"6", "Set Value Type", func(c *cmd) { SetValueType(c.st) }},
		{"7", "Search Value", attached(searchValue)},
		{"7r", "Search Value (Region filtered)", attached(func(c *cmd) { SearchFiltered(c.st) })},
		{"8", "Filter: Changed", withSession(filterAction(search.FilterChanged))},
		{"9", "Filter: Unchanged", withSession(filterAction(search.FilterUnchanged))},
		{"10", "Filter: Increased", withSession(filterAction(search.FilterIncreased))},
		{"11", "Filter: Decreased", withSession(filterAction(search.FilterDecreased))},
		{"12", "Filter: Specific Value", withSession(filterByValue)},
		{"13", "Show Candidates", withSession(func(c *cmd) { ShowCandidates(c.st) })},
		{"14", "Reset Search", resetSearch},
	}},
	{"Pattern / String", []menuItem{
		{"p", "Byte Pattern Search", attached(searchByPattern)},
		{"s8", "String Search UTF-8", attached(searchByString(search.EncodingUTF8))},
		{"s16", "String Search UTF-16LE", attached(searchByString(search.EncodingUTF16LE))},
		{"sw", "Modify String at Address", attached(modifyStringAt)},
	}},
	{"Memory", []menuItem{
		{"15", "Modify Address", attached(modifyAt)},
		{"16", "Undo Last Modify", undoLast},
		{"17", "Freeze Address", attached(freezeAt)},
		{"17i", "Set Freeze Interval", func(c *cmd) { SetFreezeInterval(c.st) }},
		{"17a", "Freeze All Candidates", withSession(freezeAllCandidates)},
		{"18", "Unfreeze Address", unfreezeAt},
		{"19", "List Frozen", func(c *cmd) { printAddrList(c.st.Freezer.List(), "No frozen addresses") }},
		{"20", "Watch Address", attached(func(c *cmd) { Watch(c.st) })},
		{"21", "Unwatch Address", unwatchAt},
		{"22", "List Watched", func(c *cmd) { printAddrList(c.st.Watcher.List(), "No watched addresses") }},
		{"22a", "Set Alert (conditional watch)", attached(func(c *cmd) { SetAlert(c.st) })},
		{"22r", "Remove Alert", func(c *cmd) { RemoveAlert(c.st) }},
		{"22l", "List Alerts", func(c *cmd) { printAddrList(c.st.AlertWatcher.List(), "No alerts set") }},
		{"23", "Dump Memory Region", attached(func(c *cmd) { Dump(c.st) })},
		{"23d", "Snapshot Diff", attached(func(c *cmd) { SnapshotDiff(c.st) })},
		{"23m", "Show Memory Maps", attached(func(c *cmd) { ShowMaps(c.st) })},
	}},
	{"Pointer", []menuItem{
		{"pt", "Pointer Scan", attached(func(c *cmd) { PointerScan(c.st) })},
		{"pr", "Resolve Pointer Chain", attached(func(c *cmd) { PointerResolve(c.st) })},
	}},
	{"Import", []menuItem{
		{"ct", "Import CheatEngine .CT file", func(c *cmd) { ImportCT(c.st) }},
	}},
	{"Bookmarks", []menuItem{
		{"24", "Add Bookmark", attached(addBookmark)},
		{"25", "List Bookmarks", func(c *cmd) { BookmarkList(c.st) }},
		{"26", "Modify All Bookmarks", attached(modifyAllBookmarks)},
		{"27", "Remove Bookmark", removeBookmark},
	}},
	{"Session", []menuItem{
		{"28", "Save State", func(c *cmd) { saveSession(c.st) }},
		{"29", "Load State", func(c *cmd) { loadSession(c.st) }},
	}},
}

// actions indexes menu by key for dispatch. Built once at startup, which also
// makes a duplicate key impossible to miss.
var actions = buildActions()

func buildActions() map[string]action {
	out := make(map[string]action)
	for _, section := range menu {
		for _, it := range section.items {
			if _, dup := out[it.key]; dup {
				panic("cli: duplicate menu key " + it.key)
			}
			out[it.key] = it.action
		}
	}
	return out
}

// PrintMenu renders the interactive menu and status header.
func PrintMenu(st *app.State, d *adb.ADB) {
	pid := st.GetPID()
	vt := st.GetValueType()
	sess := st.GetSession()

	fmt.Println("\n=== memdroid ===")
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
		if depth := st.UndoStack.Depth(); depth > 0 {
			fmt.Printf("  Undo: %d", depth)
		}
		fmt.Println()
	}
	fmt.Printf("Web UI: %s\n", ServerURL)
	for _, section := range menu {
		fmt.Printf("--- %s ---\n", section.title)
		for _, it := range section.items {
			label := it.label
			if it.key == "6" {
				label = fmt.Sprintf("%s  (current: %s)", label, vt)
			}
			fmt.Printf("%4s. %s\n", it.key, label)
		}
	}
	fmt.Println("--- ---")
	fmt.Println("   0. Exit")
	fmt.Print("> ")
}

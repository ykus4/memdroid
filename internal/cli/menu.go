package cli

import (
	"fmt"

	"memodroid/internal/app"
	"memodroid/internal/driver/adb"
)

// ServerAddr is displayed in the menu header. Set by the caller before rendering.
var ServerAddr = ":8080"

type menuSection struct {
	title string
	items []menuItem
}

type menuItem struct {
	key   string
	label string
}

var menu = []menuSection{
	{"Device", []menuItem{
		{"d", "Select ADB Device"},
		{"dw", "Connect Wi-Fi (host:port)"},
		{"dd", "Disconnect Wi-Fi"},
	}},
	{"Process", []menuItem{
		{"1", "Process List"},
		{"1s", "Attach by Name"},
		{"2", "Attach by PID"},
		{"3", "Detach"},
		{"3s", "Switch Active Process"},
		{"3l", "List Attached Processes"},
		{"4", "Stop Process"},
		{"5", "Continue Process"},
	}},
	{"Search", []menuItem{
		{"6", "Set Value Type"},
		{"7", "Search Value"},
		{"7r", "Search Value (Region filtered)"},
		{"8", "Filter: Changed"},
		{"9", "Filter: Unchanged"},
		{"10", "Filter: Increased"},
		{"11", "Filter: Decreased"},
		{"12", "Filter: Specific Value"},
		{"13", "Show Candidates"},
		{"14", "Reset Search"},
	}},
	{"Pattern / String", []menuItem{
		{"p", "Byte Pattern Search"},
		{"s8", "String Search UTF-8"},
		{"s16", "String Search UTF-16LE"},
		{"sw", "Modify String at Address"},
	}},
	{"Memory", []menuItem{
		{"15", "Modify Address"},
		{"16", "Undo Last Modify"},
		{"17", "Freeze Address"},
		{"17i", "Set Freeze Interval"},
		{"17a", "Freeze All Candidates"},
		{"18", "Unfreeze Address"},
		{"19", "List Frozen"},
		{"20", "Watch Address"},
		{"21", "Unwatch Address"},
		{"22", "List Watched"},
		{"22a", "Set Alert (conditional watch)"},
		{"22r", "Remove Alert"},
		{"23", "Dump Memory Region"},
		{"23d", "Snapshot Diff"},
		{"23m", "Show Memory Maps"},
	}},
	{"Pointer", []menuItem{
		{"pt", "Pointer Scan"},
		{"pr", "Resolve Pointer Chain"},
	}},
	{"Import", []menuItem{
		{"ct", "Import CheatEngine .CT file"},
	}},
	{"Bookmarks", []menuItem{
		{"24", "Add Bookmark"},
		{"25", "List Bookmarks"},
		{"26", "Modify All Bookmarks"},
		{"27", "Remove Bookmark"},
	}},
	{"Session", []menuItem{
		{"28", "Save State"},
		{"29", "Load State"},
	}},
}

// PrintMenu renders the interactive menu and status header.
func PrintMenu(st *app.State, d *adb.ADB) {
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
	fmt.Printf("Web UI: http://localhost%s\n", ServerAddr)
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

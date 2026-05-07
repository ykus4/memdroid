package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"memodroid/internal/app"
	"memodroid/internal/memory/modify"
	"memodroid/internal/memory/pointer"
	"memodroid/internal/memory/search"
	"memodroid/internal/memory/store"
	"memodroid/internal/memory/watch"
)

func handleSetFreezeInterval(st *app.State) {
	s := prompt(fmt.Sprintf("Freeze interval [current: %v]: ", st.Freezer.GetInterval()))
	if s == "" {
		return
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		fmt.Println("Invalid duration (e.g. 50ms, 200ms, 1s)")
		return
	}
	if d <= 0 {
		fmt.Println("Interval must be positive")
		return
	}
	st.Freezer.SetInterval(d)
	fmt.Printf("Freeze interval set to %v\n", d)
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

func handleSnapshotDiff(st *app.State) {
	addr, ok := parseAddr("Start address (hex): ")
	if !ok {
		return
	}
	size, err := strconv.Atoi(prompt("Size (bytes, decimal): "))
	if err != nil || size <= 0 {
		fmt.Println("Invalid size")
		return
	}
	fmt.Println("Taking snapshot A...")
	snapA, err := modify.TakeSnapshot(st.GetDriver(), st.GetPID(), addr, size)
	if err != nil {
		fmt.Printf("Snapshot failed: %v\n", err)
		return
	}
	fmt.Printf("Snapshot A: %d bytes at 0x%x\n", len(snapA.Data), addr)
	prompt("Make changes in the target process, then press Enter...")
	fmt.Println("Taking snapshot B...")
	snapB, err := modify.TakeSnapshot(st.GetDriver(), st.GetPID(), addr, size)
	if err != nil {
		fmt.Printf("Snapshot failed: %v\n", err)
		return
	}
	diffs, err := modify.DiffSnapshots(snapA, snapB)
	if err != nil {
		fmt.Printf("Diff failed: %v\n", err)
		return
	}
	if len(diffs) == 0 {
		fmt.Println("No differences found")
		return
	}
	fmt.Printf("Found %d changed bytes:\n", len(diffs))
	shown := 0
	for _, d := range diffs {
		fmt.Printf("  0x%x (+0x%x): 0x%02x -> 0x%02x\n", d.Addr, d.Offset, d.Before, d.After)
		shown++
		if shown >= 50 {
			fmt.Printf("  ... (%d total)\n", len(diffs))
			break
		}
	}
	path := prompt("Save diff to file [empty = skip]: ")
	if path != "" {
		if err := modify.WriteDiff(diffs, addr, path); err != nil {
			fmt.Printf("Write failed: %v\n", err)
		} else {
			fmt.Printf("Diff saved to %s\n", path)
		}
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

func handlePointerResolve(st *app.State) {
	label := prompt("Module name (e.g. libil2cpp.so): ")
	if label == "" {
		fmt.Println("Module name required")
		return
	}
	offsetsStr := prompt("Offsets (comma-separated hex, e.g. 0x10,0x20,0x8): ")
	if offsetsStr == "" {
		fmt.Println("Offsets required")
		return
	}
	parts := splitOffsets(offsetsStr)
	offsets := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(p, 0, 64)
		if err != nil {
			fmt.Printf("Invalid offset %q: %v\n", p, err)
			return
		}
		offsets = append(offsets, v)
	}
	chain := pointer.Chain{
		BaseLabel: label,
		Offsets:   offsets,
	}
	resolved, err := pointer.ResolveChain(st.GetDriver(), st.GetPID(), chain)
	if err != nil {
		fmt.Printf("Resolve failed: %v\n", err)
		return
	}
	fmt.Printf("Resolved address: 0x%x\n", resolved)
}

func splitOffsets(s string) []string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func handleImportCT(st *app.State) {
	path := prompt("CT file path: ")
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

func handleSetAlert(st *app.State) {
	addr, ok := parseAddr("Address (hex): ")
	if !ok {
		return
	}
	fmt.Println("Condition: above, below, changed")
	cond, err := watch.ParseAlertCondition(prompt("Condition: "))
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	vt := st.GetValueType()
	cfg := watch.AlertConfig{
		Addr:      addr,
		Condition: cond,
		Action:    watch.ActionNotify,
	}
	if cond != watch.AlertChanged {
		threshold, ok := parseValue("Threshold value: ", vt)
		if !ok {
			return
		}
		cfg.Threshold = threshold
	}
	action := prompt("Action (notify / write) [default: notify]: ")
	if action == "write" {
		cfg.Action = watch.ActionWrite
		writeVal, ok := parseValue("Value to write when triggered: ", vt)
		if !ok {
			return
		}
		cfg.WriteVal = writeVal
	}
	intervalStr := prompt("Poll interval [default: 500ms]: ")
	if intervalStr == "" {
		intervalStr = "500ms"
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		fmt.Println("Invalid interval")
		return
	}
	if err := st.AlertWatcher.WatchWithAlert(st.GetDriver(), st.GetPID(), vt, cfg, interval); err != nil {
		fmt.Printf("Alert failed: %v\n", err)
		return
	}
	fmt.Printf("Alert set on 0x%x: condition=%s action=%s\n", addr, cond, action)
}

func handleRemoveAlert(st *app.State) {
	addr, ok := parseAddr("Address (hex): ")
	if !ok {
		return
	}
	if err := st.AlertWatcher.RemoveAlert(addr); err != nil {
		fmt.Printf("Remove alert: %v\n", err)
	} else {
		fmt.Printf("Alert removed for 0x%x\n", addr)
	}
}

// suppress unused import warnings
var _ = search.TypeInt32
var _ = watch.AlertChanged

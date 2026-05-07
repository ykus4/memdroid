package main

import (
	"fmt"
	"strconv"
	"time"

	"memodroid/internal/app"
	"memodroid/internal/memory/modify"
	"memodroid/internal/memory/pointer"
)

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

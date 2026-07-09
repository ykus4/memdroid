package cli

import (
	"fmt"
	"strconv"
	"time"

	"memdroid/internal/app"
	"memdroid/internal/memory/modify"
)

// DefaultWatchInterval is the default polling interval when watching an address.
const DefaultWatchInterval = "500ms"

// DefaultDumpFile is the default output filename for memory region dumps.
const DefaultDumpFile = "dump.hex"

func SetFreezeInterval(st *app.State) {
	s := Prompt(fmt.Sprintf("Freeze interval [current: %v]: ", st.Freezer.GetInterval()))
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

func Watch(st *app.State) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	intervalStr := Prompt(fmt.Sprintf("Interval [default: %s]: ", DefaultWatchInterval))
	if intervalStr == "" {
		intervalStr = DefaultWatchInterval
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

func Dump(st *app.State) {
	addr, ok := ParseAddr("Start address (hex): ")
	if !ok {
		return
	}
	size, err := strconv.Atoi(Prompt("Size (bytes, decimal): "))
	if err != nil || size <= 0 {
		fmt.Println("Invalid size")
		return
	}
	path := Prompt(fmt.Sprintf("Output file [default: %s]: ", DefaultDumpFile))
	if path == "" {
		path = DefaultDumpFile
	}
	if err := modify.DumpRegion(st.GetDriver(), st.GetPID(), addr, size, path); err != nil {
		fmt.Printf("Dump failed: %v\n", err)
	} else {
		fmt.Printf("Dumped %d bytes from 0x%x to %s\n", size, addr, path)
	}
}

func SnapshotDiff(st *app.State) {
	addr, ok := ParseAddr("Start address (hex): ")
	if !ok {
		return
	}
	size, err := strconv.Atoi(Prompt("Size (bytes, decimal): "))
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
	Prompt("Make changes in the target process, then press Enter...")
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
		if shown >= MaxCandidatesDisplay {
			fmt.Printf("  ... (%d total)\n", len(diffs))
			break
		}
	}
	path := Prompt("Save diff to file [empty = skip]: ")
	if path != "" {
		if err := modify.WriteDiff(diffs, addr, path); err != nil {
			fmt.Printf("Write failed: %v\n", err)
		} else {
			fmt.Printf("Diff saved to %s\n", path)
		}
	}
}

func ShowMaps(st *app.State) {
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

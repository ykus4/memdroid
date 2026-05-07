package main

import (
	"fmt"
	"strconv"

	"memodroid/internal/app"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/search"
)

func doAttach(st *app.State, pid int, name string) {
	drv := st.GetDriver()
	if err := drv.Attach(pid); err != nil {
		fmt.Printf("Attach failed: %v\n", err)
		return
	}
	st.SetPID(pid)
	st.AddAttached(pid, name)
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
	st.AlertWatcher.RemoveAll()
	st.GetDriver().Detach(pid)
	st.RemoveAttached(pid)
	fmt.Printf("Detached from PID %d\n", pid)
	// Switch to another attached process if available
	remaining := st.ListAttached()
	if len(remaining) > 0 {
		next := remaining[0]
		st.SetPID(next.PID)
		st.SetSession(search.NewSession(next.PID, st.GetValueType(), st.GetDriver()))
		fmt.Printf("Switched to PID %d (%s)\n", next.PID, next.Name)
	} else {
		st.SetPID(0)
		st.SetSession(nil)
	}
}

func handleSwitchProcess(st *app.State) {
	procs := st.ListAttached()
	if len(procs) == 0 {
		fmt.Println("No attached processes")
		return
	}
	current := st.GetPID()
	fmt.Println("Attached processes:")
	for i, p := range procs {
		marker := "  "
		if p.PID == current {
			marker = "* "
		}
		name := p.Name
		if name == "" {
			name = "(unknown)"
		}
		fmt.Printf("  %s%d. [%d] %s\n", marker, i+1, p.PID, name)
	}
	idx, err := strconv.Atoi(prompt("Switch to: "))
	if err != nil || idx < 1 || idx > len(procs) {
		fmt.Println("Invalid selection")
		return
	}
	target := procs[idx-1]
	st.SetPID(target.PID)
	st.SetSession(search.NewSession(target.PID, st.GetValueType(), st.GetDriver()))
	fmt.Printf("Active process: PID %d (%s)\n", target.PID, target.Name)
}

func handleListAttached(st *app.State) {
	procs := st.ListAttached()
	if len(procs) == 0 {
		fmt.Println("No attached processes")
		return
	}
	current := st.GetPID()
	for _, p := range procs {
		marker := "  "
		if p.PID == current {
			marker = "* "
		}
		name := p.Name
		if name == "" {
			name = "(unknown)"
		}
		fmt.Printf("  %s[%d] %s\n", marker, p.PID, name)
	}
}

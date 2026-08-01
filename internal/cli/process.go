package cli

import (
	"fmt"
	"strconv"

	"memdroid/internal/app"
	"memdroid/internal/driver/adb"
)

func doAttach(st *app.State, pid int, name string) {
	if err := st.GetDriver().Attach(pid); err != nil {
		fmt.Printf("Attach failed: %v\n", err)
		return
	}
	st.SetPID(pid)
	st.AddAttached(pid, name)
	st.NewSession(pid)
	if name != "" {
		fmt.Printf("Attached to %s (PID %d)\n", name, pid)
	} else {
		fmt.Printf("Attached to PID %d\n", pid)
	}
}

func Attach(st *app.State) {
	pid, err := strconv.Atoi(Prompt("PID: "))
	if err != nil {
		fmt.Println("Invalid PID")
		return
	}
	doAttach(st, pid, "")
}

func AttachByName(st *app.State, d *adb.ADB) {
	name := Prompt("Process name (partial match): ")
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
	idx, err := strconv.Atoi(Prompt("Select: "))
	if err != nil || idx < 1 || idx > len(matches) {
		fmt.Println("Invalid selection")
		return
	}
	doAttach(st, matches[idx-1].PID, matches[idx-1].Name)
}

func Detach(st *app.State) {
	if !RequireAttached(st.GetPID()) {
		return
	}
	detached, next := st.Detach()
	fmt.Printf("Detached from PID %d\n", detached)
	if next.PID != 0 {
		fmt.Printf("Switched to PID %d (%s)\n", next.PID, next.Name)
	}
}

func SwitchProcess(st *app.State) {
	procs := st.ListAttached()
	if len(procs) == 0 {
		fmt.Println("No attached processes")
		return
	}
	current := st.GetPID()
	fmt.Println("Attached processes:")
	for i, p := range procs {
		fmt.Printf("  %s%d. [%d] %s\n", activeMarker(p.PID, current), i+1, p.PID, processName(p.Name))
	}
	idx, err := strconv.Atoi(Prompt("Switch to: "))
	if err != nil || idx < 1 || idx > len(procs) {
		fmt.Println("Invalid selection")
		return
	}
	target := procs[idx-1]
	st.SetPID(target.PID)
	st.NewSession(target.PID)
	fmt.Printf("Active process: PID %d (%s)\n", target.PID, processName(target.Name))
}

func ListAttached(st *app.State) {
	procs := st.ListAttached()
	if len(procs) == 0 {
		fmt.Println("No attached processes")
		return
	}
	current := st.GetPID()
	for _, p := range procs {
		fmt.Printf("  %s[%d] %s\n", activeMarker(p.PID, current), p.PID, processName(p.Name))
	}
}

// activeMarker flags the currently active process in a listing.
func activeMarker(pid, active int) string {
	if pid == active {
		return "* "
	}
	return "  "
}

// processName renders an unknown process name as a placeholder. A PID attached
// by number has no name recorded.
func processName(name string) string {
	if name == "" {
		return "(unknown)"
	}
	return name
}

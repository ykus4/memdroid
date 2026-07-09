package process

import (
	"fmt"

	"memdroid/internal/driver"
)

// ProcessInfo mirrors driver.ProcessInfo with JSON tags for the HTTP API.
type ProcessInfo struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// List prints all processes to stdout using drv.
func List(drv driver.Driver) error {
	procs, err := ProcessList(drv)
	if err != nil {
		return err
	}
	for _, p := range procs {
		fmt.Printf("PID: %5d  Name: %s\n", p.PID, p.Name)
	}
	return nil
}

// ProcessList returns all running processes via drv.
func ProcessList(drv driver.Driver) ([]ProcessInfo, error) {
	raw, err := drv.ListProcesses()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	out := make([]ProcessInfo, len(raw))
	for i, p := range raw {
		out[i] = ProcessInfo{PID: p.PID, Name: p.Name}
	}
	return out, nil
}

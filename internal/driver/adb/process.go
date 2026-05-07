package adb

import (
	"fmt"
	"strconv"
	"strings"

	"memodroid/internal/driver"
)

// ListProcesses returns all running processes via "adb shell ps -A".
func (a *ADB) ListProcesses() ([]driver.ProcessInfo, error) {
	out, err := a.shell("ps", "-A")
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	var procs []driver.ProcessInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// ps -A columns: USER PID PPID VSZ RSS WCHAN ADDR S NAME
		if len(fields) < 9 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		name := fields[len(fields)-1]
		procs = append(procs, driver.ProcessInfo{PID: pid, Name: name})
	}
	return procs, nil
}

// FindProcessByName returns the PIDs of all processes whose name contains substr.
func (a *ADB) FindProcessByName(substr string) ([]driver.ProcessInfo, error) {
	all, err := a.ListProcesses()
	if err != nil {
		return nil, err
	}
	var out []driver.ProcessInfo
	lower := strings.ToLower(substr)
	for _, p := range all {
		if strings.Contains(strings.ToLower(p.Name), lower) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Attach sends SIGSTOP to the process to pause it before memory operations.
// On Android we don't use ptrace; instead we stop the process with kill -STOP
// which requires root.
func (a *ADB) Attach(pid int) error {
	_, err := a.shellRoot(fmt.Sprintf("kill -STOP %d", pid))
	if err != nil {
		return fmt.Errorf("attach (SIGSTOP) pid %d: %w", pid, err)
	}
	return nil
}

// Detach resumes the process with SIGCONT.
func (a *ADB) Detach(pid int) {
	_, _ = a.shellRoot(fmt.Sprintf("kill -CONT %d", pid))
}

// Stop sends SIGSTOP (same as Attach, exposed as an explicit control command).
func (a *ADB) Stop(pid int) error {
	_, err := a.shellRoot(fmt.Sprintf("kill -STOP %d", pid))
	if err != nil {
		return fmt.Errorf("stop pid %d: %w", pid, err)
	}
	return nil
}

// Continue sends SIGCONT to resume a stopped process.
func (a *ADB) Continue(pid int) error {
	_, err := a.shellRoot(fmt.Sprintf("kill -CONT %d", pid))
	if err != nil {
		return fmt.Errorf("continue pid %d: %w", pid, err)
	}
	return nil
}

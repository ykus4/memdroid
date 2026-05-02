package watch

import (
	"fmt"
	"sync"
	"time"

	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

// BroadcastFunc is called when a watched value changes.
// Set this to wswatch.Broadcast to push events to the Web UI.
var BroadcastFunc func(addr uintptr, prev, cur string)

type watchEntry struct {
	drv   driver.Driver
	pid   int
	addr  uintptr
	vtype search.ValueType
	last  []byte
	stop  chan struct{}
}

var (
	mu      sync.Mutex
	entries = map[uintptr]*watchEntry{}
)

// Watch polls addr every interval and prints when the value changes.
func Watch(drv driver.Driver, pid int, addr uintptr, vt search.ValueType, interval time.Duration) {
	mu.Lock()
	defer mu.Unlock()

	if _, exists := entries[addr]; exists {
		fmt.Printf("0x%x is already being watched\n", addr)
		return
	}

	initial, err := drv.Peek(pid, addr, vt.Size())
	if err != nil {
		fmt.Printf("Watch failed: %v\n", err)
		return
	}
	last := make([]byte, len(initial))
	copy(last, initial)

	e := &watchEntry{drv: drv, pid: pid, addr: addr, vtype: vt, last: last, stop: make(chan struct{})}
	entries[addr] = e

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cur, err := e.drv.Peek(e.pid, e.addr, e.vtype.Size())
				if err != nil {
					continue
				}
				if !search.EqualBytes(cur, e.last) {
					prev := search.FormatValue(e.last, e.vtype)
					next := search.FormatValue(cur, e.vtype)
					fmt.Printf("[Watch] 0x%x: %s -> %s\n", e.addr, prev, next)
					if BroadcastFunc != nil {
						BroadcastFunc(e.addr, prev, next)
					}
					copy(e.last, cur)
				}
			case <-e.stop:
				return
			}
		}
	}()

	fmt.Printf("Watching 0x%x (%s) every %v\n", addr, vt, interval)
}

// Unwatch stops watching addr.
func Unwatch(addr uintptr) {
	mu.Lock()
	defer mu.Unlock()

	e, ok := entries[addr]
	if !ok {
		fmt.Printf("0x%x is not being watched\n", addr)
		return
	}
	close(e.stop)
	delete(entries, addr)
	fmt.Printf("Stopped watching 0x%x\n", addr)
}

// UnwatchAll stops all watchers.
func UnwatchAll() {
	mu.Lock()
	defer mu.Unlock()

	for addr, e := range entries {
		close(e.stop)
		delete(entries, addr)
	}
}

// List returns all currently watched addresses.
func List() []uintptr {
	mu.Lock()
	defer mu.Unlock()

	addrs := make([]uintptr, 0, len(entries))
	for addr := range entries {
		addrs = append(addrs, addr)
	}
	return addrs
}

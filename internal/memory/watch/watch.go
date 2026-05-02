package watch

import (
	"fmt"
	"sync"
	"time"

	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

// ChangeEvent is emitted when a watched value changes.
type ChangeEvent struct {
	Addr uintptr
	Prev string
	Cur  string
}

type watchEntry struct {
	drv   driver.Driver
	pid   int
	addr  uintptr
	vtype search.ValueType
	last  []byte
	stop  chan struct{}
}

// Watcher monitors a set of addresses and fires OnChange when a value changes.
type Watcher struct {
	mu       sync.Mutex
	entries  map[uintptr]*watchEntry
	OnChange func(ChangeEvent)
}

func NewWatcher() *Watcher {
	return &Watcher{entries: make(map[uintptr]*watchEntry)}
}

// Watch polls addr every interval and calls OnChange when the value changes.
// Returns an error if addr is already being watched or initial read fails.
func (w *Watcher) Watch(drv driver.Driver, pid int, addr uintptr, vt search.ValueType, interval time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.entries[addr]; exists {
		return fmt.Errorf("0x%x is already being watched", addr)
	}

	initial, err := drv.Peek(pid, addr, vt.Size())
	if err != nil {
		return fmt.Errorf("watch: initial read failed: %w", err)
	}
	last := make([]byte, len(initial))
	copy(last, initial)

	e := &watchEntry{drv: drv, pid: pid, addr: addr, vtype: vt, last: last, stop: make(chan struct{})}
	w.entries[addr] = e

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
					if w.OnChange != nil {
						w.OnChange(ChangeEvent{
							Addr: e.addr,
							Prev: search.FormatValue(e.last, e.vtype),
							Cur:  search.FormatValue(cur, e.vtype),
						})
					}
					copy(e.last, cur)
				}
			case <-e.stop:
				return
			}
		}
	}()

	return nil
}

// Unwatch stops watching addr. Returns an error if addr was not watched.
func (w *Watcher) Unwatch(addr uintptr) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.entries[addr]
	if !ok {
		return fmt.Errorf("0x%x is not being watched", addr)
	}
	close(e.stop)
	delete(w.entries, addr)
	return nil
}

// UnwatchAll stops all watchers.
func (w *Watcher) UnwatchAll() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for addr, e := range w.entries {
		close(e.stop)
		delete(w.entries, addr)
	}
}

// List returns all currently watched addresses.
func (w *Watcher) List() []uintptr {
	w.mu.Lock()
	defer w.mu.Unlock()

	addrs := make([]uintptr, 0, len(w.entries))
	for addr := range w.entries {
		addrs = append(addrs, addr)
	}
	return addrs
}

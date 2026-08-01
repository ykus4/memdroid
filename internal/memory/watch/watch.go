package watch

import (
	"bytes"
	"fmt"
	"time"

	"memdroid/internal/driver"
	"memdroid/internal/memory/search"
	"memdroid/internal/poller"
)

// ChangeEvent is emitted when a watched value changes.
type ChangeEvent struct {
	Addr uintptr
	Prev string
	Cur  string
}

// Watcher monitors a set of addresses and notifies its listeners when a value
// changes. Listeners are invoked from watcher goroutines and must be safe for
// concurrent use.
type Watcher struct {
	pool     *poller.Pool
	onChange listeners[ChangeEvent]
}

func NewWatcher() *Watcher {
	return &Watcher{pool: poller.New()}
}

// OnChange registers fn to receive every change event and returns a function
// that unregisters it. Multiple consumers (CLI output, WebSocket push) can
// subscribe at once.
func (w *Watcher) OnChange(fn func(ChangeEvent)) (remove func()) {
	return w.onChange.add(fn)
}

// Watch polls addr every interval and calls OnChange when the value changes.
// Returns an error if addr is already being watched, the type has no fixed
// size, or the initial read fails.
func (w *Watcher) Watch(drv driver.Driver, pid int, addr uintptr, vt search.ValueType, interval time.Duration) error {
	size := vt.Size()
	if size == 0 {
		return fmt.Errorf("watch does not support the %s type", vt)
	}

	initial, err := drv.Peek(pid, addr, size)
	if err != nil {
		return fmt.Errorf("watch: initial read failed: %w", err)
	}
	last := bytes.Clone(initial)

	return w.pool.Start(addr, func(stop <-chan struct{}) {
		poller.EveryTick(interval, stop, func() {
			cur, err := drv.Peek(pid, addr, size)
			if err != nil {
				return
			}
			if bytes.Equal(cur, last) {
				return
			}
			w.onChange.emit(ChangeEvent{
				Addr: addr,
				Prev: search.FormatValue(last, vt),
				Cur:  search.FormatValue(cur, vt),
			})
			// Clone rather than copy: a short read at a remapped boundary
			// returns fewer bytes, and copy would leave stale trailing bytes
			// in last, so it could never compare equal again.
			last = bytes.Clone(cur)
		})
	})
}

// Unwatch stops watching addr.
func (w *Watcher) Unwatch(addr uintptr) error { return w.pool.Stop(addr) }

// UnwatchAll stops all watchers.
func (w *Watcher) UnwatchAll() { w.pool.StopAll() }

// List returns all currently watched addresses.
func (w *Watcher) List() []uintptr { return w.pool.List() }

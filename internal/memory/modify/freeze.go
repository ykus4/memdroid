package modify

import (
	"fmt"
	"sync"
	"time"

	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

const freezeInterval = 100 * time.Millisecond

type freezeEntry struct {
	drv   driver.Driver
	pid   int
	addr  uintptr
	value []byte
	stop  chan struct{}
}

// Freezer manages a set of frozen addresses.
type Freezer struct {
	mu      sync.Mutex
	entries map[uintptr]*freezeEntry
}

func NewFreezer() *Freezer {
	return &Freezer{entries: make(map[uintptr]*freezeEntry)}
}

// Freeze starts a goroutine that repeatedly writes value to addr every 100ms.
// Returns an error if addr is already frozen.
func (f *Freezer) Freeze(drv driver.Driver, pid int, addr uintptr, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.entries[addr]; exists {
		return fmt.Errorf("0x%x is already frozen", addr)
	}

	val := make([]byte, len(value))
	copy(val, value)

	e := &freezeEntry{drv: drv, pid: pid, addr: addr, value: val, stop: make(chan struct{})}
	f.entries[addr] = e

	go func() {
		ticker := time.NewTicker(freezeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = e.drv.Poke(e.pid, e.addr, e.value)
			case <-e.stop:
				return
			}
		}
	}()

	return nil
}

// FreezeAllCandidates freezes every address in the session with its last-seen value.
func (f *Freezer) FreezeAllCandidates(drv driver.Driver, s *search.Session) int {
	count := 0
	for addr, val := range s.Candidates {
		if f.Freeze(drv, s.PID, addr, val) == nil {
			count++
		}
	}
	return count
}

// Unfreeze stops the freeze goroutine for addr.
// Returns an error if addr was not frozen.
func (f *Freezer) Unfreeze(addr uintptr) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	e, ok := f.entries[addr]
	if !ok {
		return fmt.Errorf("0x%x is not frozen", addr)
	}
	close(e.stop)
	delete(f.entries, addr)
	return nil
}

// UnfreezeAll stops all freeze goroutines.
func (f *Freezer) UnfreezeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for addr, e := range f.entries {
		close(e.stop)
		delete(f.entries, addr)
	}
}

// List returns all currently frozen addresses.
func (f *Freezer) List() []uintptr {
	f.mu.Lock()
	defer f.mu.Unlock()

	addrs := make([]uintptr, 0, len(f.entries))
	for addr := range f.entries {
		addrs = append(addrs, addr)
	}
	return addrs
}

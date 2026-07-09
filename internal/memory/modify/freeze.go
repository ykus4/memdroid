package modify

import (
	"sync"
	"time"

	"memdroid/internal/driver"
	"memdroid/internal/memory/search"
	"memdroid/internal/poller"
)

const defaultFreezeInterval = 100 * time.Millisecond

// Freezer manages a set of frozen addresses, each periodically re-written to
// hold its value. Task lifecycle is delegated to poller.Pool.
type Freezer struct {
	pool *poller.Pool

	mu       sync.Mutex
	interval time.Duration
}

func NewFreezer() *Freezer {
	return &Freezer{
		pool:     poller.New(),
		interval: defaultFreezeInterval,
	}
}

// SetInterval changes the freeze write interval for newly frozen addresses.
func (f *Freezer) SetInterval(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interval = d
}

// GetInterval returns the current freeze interval.
func (f *Freezer) GetInterval() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.interval
}

// Freeze starts a goroutine that repeatedly writes value to addr at the
// configured interval. Returns an error if addr is already frozen.
func (f *Freezer) Freeze(drv driver.Driver, pid int, addr uintptr, value []byte) error {
	val := make([]byte, len(value))
	copy(val, value)
	iv := f.GetInterval()

	return f.pool.Start(addr, func(stop <-chan struct{}) {
		poller.EveryTick(iv, stop, func() {
			_ = drv.Poke(pid, addr, val)
		})
	})
}

// FreezeAllCandidates freezes every address in the session with its last-seen value.
func (f *Freezer) FreezeAllCandidates(drv driver.Driver, s *search.Session) int {
	count := 0
	for addr, val := range s.Snapshot() {
		if f.Freeze(drv, s.PID, addr, val) == nil {
			count++
		}
	}
	return count
}

// Unfreeze stops the freeze goroutine for addr.
func (f *Freezer) Unfreeze(addr uintptr) error { return f.pool.Stop(addr) }

// UnfreezeAll stops all freeze goroutines.
func (f *Freezer) UnfreezeAll() { f.pool.StopAll() }

// List returns all currently frozen addresses.
func (f *Freezer) List() []uintptr { return f.pool.List() }

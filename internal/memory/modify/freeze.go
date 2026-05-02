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

var (
	freezeMu      sync.Mutex
	frozenEntries = map[uintptr]*freezeEntry{}
)

// Freeze starts a goroutine that repeatedly writes value to addr every 100ms.
func Freeze(drv driver.Driver, pid int, addr uintptr, value []byte) {
	freezeMu.Lock()
	defer freezeMu.Unlock()

	if _, exists := frozenEntries[addr]; exists {
		fmt.Printf("0x%x is already frozen\n", addr)
		return
	}

	val := make([]byte, len(value))
	copy(val, value)

	e := &freezeEntry{drv: drv, pid: pid, addr: addr, value: val, stop: make(chan struct{})}
	frozenEntries[addr] = e

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

	fmt.Printf("Freezing 0x%x\n", addr)
}

// FreezeAllCandidates freezes every address in the session with its last-seen value.
func FreezeAllCandidates(drv driver.Driver, s *search.Session) {
	if !s.HasCandidates() {
		fmt.Println("No candidates to freeze")
		return
	}
	count := 0
	for addr, val := range s.Candidates {
		Freeze(drv, s.PID, addr, val)
		count++
	}
	fmt.Printf("Freezing %d addresses\n", count)
}

// Unfreeze stops the freeze goroutine for addr.
func Unfreeze(addr uintptr) {
	freezeMu.Lock()
	defer freezeMu.Unlock()

	e, ok := frozenEntries[addr]
	if !ok {
		fmt.Printf("0x%x is not frozen\n", addr)
		return
	}
	close(e.stop)
	delete(frozenEntries, addr)
	fmt.Printf("Unfrozen 0x%x\n", addr)
}

// UnfreezeAll stops all freeze goroutines.
func UnfreezeAll() {
	freezeMu.Lock()
	defer freezeMu.Unlock()

	for addr, e := range frozenEntries {
		close(e.stop)
		delete(frozenEntries, addr)
	}
}

// FrozenList returns all currently frozen addresses.
func FrozenList() []uintptr {
	freezeMu.Lock()
	defer freezeMu.Unlock()

	addrs := make([]uintptr, 0, len(frozenEntries))
	for addr := range frozenEntries {
		addrs = append(addrs, addr)
	}
	return addrs
}

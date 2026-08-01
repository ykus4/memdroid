// Package drivertest provides an in-memory driver.Driver for tests.
//
// Everything above the driver layer — search, filter, pointer scanning, the
// HTTP handlers — is pure logic over a byte-addressed process. Fake lets that
// logic be tested without an Android device or an adb binary.
package drivertest

import (
	"fmt"
	"sync"

	"memdroid/internal/driver"
)

// Region is one mapped range in a fake process.
type Region struct {
	Start uintptr
	Name  string
	Data  []byte
}

// Fake is an in-memory driver.Driver.
//
// The zero value is not usable; call New. All methods are safe for concurrent
// use, matching the real driver, so the parallel scan engine can be exercised
// under -race.
type Fake struct {
	mu       sync.RWMutex
	regions  []Region
	serial   string
	devices  []string
	attached map[int]bool
	procs    []driver.ProcessInfo

	// PeekErr, PokeErr and ReadErr, when set, make the matching operation fail.
	// Used to cover error paths.
	PeekErr error
	PokeErr error
	ReadErr error

	// Counters record how often each transport operation ran, so tests can
	// assert that a change actually reduced round-trips.
	Peeks       int
	Pokes       int
	RegionReads int
}

// New returns a Fake with a single process (pid 1) and the given regions.
func New(regions ...Region) *Fake {
	return &Fake{
		regions:  regions,
		devices:  []string{"fake-device"},
		attached: make(map[int]bool),
		procs:    []driver.ProcessInfo{{PID: 1, Name: "com.example.fake"}},
	}
}

// WithProcesses replaces the fake process table.
func (f *Fake) WithProcesses(procs ...driver.ProcessInfo) *Fake {
	f.procs = procs
	return f
}

// Bytes returns a copy of the current contents of the region containing addr.
func (f *Fake) Bytes(addr uintptr, n int) []byte {
	b, err := f.ReadRegion(1, addr, n)
	if err != nil {
		return nil
	}
	return b
}

// --- device management ---

func (f *Fake) ListDevices() ([]string, error) { return f.devices, nil }

func (f *Fake) SelectDevice(serial string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serial = serial
	return nil
}

func (f *Fake) DeviceSerial() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.serial
}

// --- process management ---

func (f *Fake) ListProcesses() ([]driver.ProcessInfo, error) { return f.procs, nil }

func (f *Fake) Attach(pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attached[pid] = true
	return nil
}

func (f *Fake) Detach(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.attached, pid)
}

// Attached reports whether pid is currently attached.
func (f *Fake) Attached(pid int) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.attached[pid]
}

func (f *Fake) Stop(int) error     { return nil }
func (f *Fake) Continue(int) error { return nil }

// --- memory access ---

func (f *Fake) Peek(_ int, addr uintptr, size int) ([]byte, error) {
	f.mu.Lock()
	f.Peeks++
	err := f.PeekErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.read(addr, size)
}

func (f *Fake) Poke(_ int, addr uintptr, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Pokes++
	if f.PokeErr != nil {
		return f.PokeErr
	}
	for i := range f.regions {
		r := &f.regions[i]
		off := int(addr) - int(r.Start)
		if off < 0 || off+len(data) > len(r.Data) {
			continue
		}
		copy(r.Data[off:], data)
		return nil
	}
	return fmt.Errorf("poke 0x%x: unmapped", addr)
}

func (f *Fake) ReadBytes(pid int, addr uintptr, n int) ([]byte, error) {
	return f.Peek(pid, addr, n)
}

func (f *Fake) ReadRegion(_ int, addr uintptr, size int) ([]byte, error) {
	f.mu.Lock()
	f.RegionReads++
	err := f.ReadErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.read(addr, size)
}

// read returns up to size bytes at addr, truncating at the end of the region
// (as a real short read at a mapping boundary would).
func (f *Fake) read(addr uintptr, size int) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, r := range f.regions {
		off := int(addr) - int(r.Start)
		if off < 0 || off >= len(r.Data) {
			continue
		}
		end := min(off+size, len(r.Data))
		out := make([]byte, end-off)
		copy(out, r.Data[off:end])
		return out, nil
	}
	return nil, fmt.Errorf("read 0x%x: unmapped", addr)
}

func (f *Fake) ReadMaps(pid int) ([]driver.Region, error) {
	return f.ReadMapsFiltered(pid, driver.RegionAll, 0, 0)
}

func (f *Fake) ReadMapsFiltered(_ int, filter driver.RegionFilter, customStart, customEnd uintptr) ([]driver.Region, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []driver.Region
	for _, r := range f.regions {
		dr := driver.Region{Start: r.Start, End: r.Start + uintptr(len(r.Data)), Name: r.Name}
		if driver.MatchFilter(dr, filter, customStart, customEnd) {
			out = append(out, dr)
		}
	}
	return out, nil
}

// Compile-time check.
var _ driver.Driver = (*Fake)(nil)

package pointer

import (
	"encoding/binary"
	"fmt"
	"testing"

	"memdroid/internal/driver"
)

// fakeDriver is an in-memory driver used to exercise Scan/ResolveChain without
// a real device. It only implements the methods the pointer package uses.
type fakeDriver struct {
	regions []driver.Region
	mem     map[uintptr][]byte // region.Start -> full region bytes
}

func newFakeDriver(regions []driver.Region) *fakeDriver {
	f := &fakeDriver{regions: regions, mem: make(map[uintptr][]byte)}
	for _, r := range regions {
		f.mem[r.Start] = make([]byte, r.End-r.Start)
	}
	return f
}

// putPtr writes a little-endian 64-bit pointer value at addr.
func (f *fakeDriver) putPtr(addr, val uintptr) {
	for _, r := range f.regions {
		if addr >= r.Start && addr < r.End {
			binary.LittleEndian.PutUint64(f.mem[r.Start][addr-r.Start:], uint64(val))
			return
		}
	}
	panic(fmt.Sprintf("putPtr: addr 0x%x not in any region", addr))
}

func (f *fakeDriver) ReadMaps(int) ([]driver.Region, error) { return f.regions, nil }

func (f *fakeDriver) ReadMapsFiltered(_ int, _ driver.RegionFilter, _, _ uintptr) ([]driver.Region, error) {
	return f.regions, nil
}

func (f *fakeDriver) ReadRegion(_ int, addr uintptr, size int) ([]byte, error) {
	for _, r := range f.regions {
		if addr >= r.Start && addr < r.End {
			buf := f.mem[r.Start]
			off := int(addr - r.Start)
			end := off + size
			if end > len(buf) {
				end = len(buf)
			}
			return buf[off:end], nil
		}
	}
	return nil, fmt.Errorf("unmapped 0x%x", addr)
}

func (f *fakeDriver) Peek(pid int, addr uintptr, size int) ([]byte, error) {
	return f.ReadRegion(pid, addr, size)
}

// Unused interface methods.
func (f *fakeDriver) ListDevices() ([]string, error)               { return nil, nil }
func (f *fakeDriver) SelectDevice(string) error                    { return nil }
func (f *fakeDriver) DeviceSerial() string                         { return "" }
func (f *fakeDriver) ListProcesses() ([]driver.ProcessInfo, error) { return nil, nil }
func (f *fakeDriver) Attach(int) error                             { return nil }
func (f *fakeDriver) Detach(int)                                   {}
func (f *fakeDriver) Stop(int) error                               { return nil }
func (f *fakeDriver) Continue(int) error                           { return nil }
func (f *fakeDriver) Poke(int, uintptr, []byte) error              { return nil }
func (f *fakeDriver) ReadBytes(pid int, addr uintptr, n int) ([]byte, error) {
	return f.ReadRegion(pid, addr, n)
}

// TestScanResolveRoundTrip builds a two-level pointer chain and verifies that
// Scan discovers it and ResolveChain follows it back to the target.
func TestScanResolveRoundTrip(t *testing.T) {
	const (
		moduleStart = uintptr(0x1000)
		heapStart   = uintptr(0x40000000)
		target      = uintptr(0x40001100) // within DefaultMaxOffset of p2

		base = moduleStart + 0x10 // 0x1010, base pointer slot in module
		p1   = heapStart          // 0x40000000
		p2   = uintptr(0x40001000)
	)

	f := newFakeDriver([]driver.Region{
		{Start: moduleStart, End: moduleStart + 0x1000, Name: "libtest.so"},
		{Start: heapStart, End: heapStart + 0x10000, Name: "[heap]"},
	})
	// base -> p1 ; (p1+0x8) -> p2 ; p2 + 0x1000 == target
	f.putPtr(base, p1)
	f.putPtr(p1+0x8, p2)

	res, err := Scan(f, 1, target, DefaultMaxDepth, DefaultMaxOffset)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Chains) == 0 {
		t.Fatalf("Scan found no chains")
	}

	// Every discovered chain must resolve back to the target.
	foundModuleChain := false
	for _, c := range res.Chains {
		if c.BaseLabel == "libtest.so" {
			foundModuleChain = true
		}
		got, err := ResolveChain(f, 1, c)
		if err != nil {
			t.Fatalf("ResolveChain(%s): %v", FormatChain(c), err)
		}
		if got != target {
			t.Errorf("chain %s resolved to 0x%x, want 0x%x", FormatChain(c), got, target)
		}
	}
	if !foundModuleChain {
		t.Errorf("expected a chain based in libtest.so")
	}
}

func TestResolveChainUnknownModule(t *testing.T) {
	f := newFakeDriver([]driver.Region{{Start: 0x1000, End: 0x2000, Name: "libtest.so"}})
	_, err := ResolveChain(f, 1, Chain{BaseLabel: "missing.so", Offsets: []int64{0x8}})
	if err == nil {
		t.Errorf("expected error for unknown module")
	}
}

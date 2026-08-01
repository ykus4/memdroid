package search

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"testing"

	"memdroid/internal/driver"
	"memdroid/internal/driver/drivertest"
)

// --- shared test helpers -----------------------------------------------------

// errFake forces a driver operation to fail.
var errFake = errors.New("drivertest: forced failure")

// le32 returns the little-endian encoding of v, as ParseValue would produce.
func le32(v int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

// le64 returns the little-endian encoding of v.
func le64(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

// lef32 returns the little-endian encoding of a float32.
func lef32(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

// regionData returns size zero bytes with writes applied at the given offsets.
func regionData(size int, writes map[int][]byte) []byte {
	buf := make([]byte, size)
	for off, val := range writes {
		copy(buf[off:], val)
	}
	return buf
}

// cloneCands returns a deep copy of m, so each subtest gets its own candidates.
func cloneCands(m map[uintptr][]byte) map[uintptr][]byte {
	out := make(map[uintptr][]byte, len(m))
	for k, v := range m {
		out[k] = bytes.Clone(v)
	}
	return out
}

// sortedAddrs returns the keys of m in ascending order.
func sortedAddrs(m map[uintptr][]byte) []uintptr {
	out := make([]uintptr, 0, len(m))
	for a := range m {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// --- Search ------------------------------------------------------------------

func TestSearchFixedWidthAlignment(t *testing.T) {
	const base = 0x1000
	target := le32(0x11223344)

	cases := []struct {
		name      string
		vt        ValueType
		offset    int // where the value is planted inside the region
		target    []byte
		wantAddrs []uintptr
	}{
		{
			name:      "int32 at an aligned offset is found",
			vt:        TypeInt32,
			offset:    8,
			target:    target,
			wantAddrs: []uintptr{base + 8},
		},
		{
			name:      "int32 at an unaligned offset is skipped",
			vt:        TypeInt32,
			offset:    2,
			target:    target,
			wantAddrs: nil,
		},
		{
			name:      "bytes scan finds the same unaligned value",
			vt:        TypeBytes,
			offset:    2,
			target:    target,
			wantAddrs: []uintptr{base + 2},
		},
		{
			name:      "bytes scan finds an aligned value too",
			vt:        TypeBytes,
			offset:    8,
			target:    target,
			wantAddrs: []uintptr{base + 8},
		},
		{
			name:      "int64 at an 8-aligned offset is found",
			vt:        TypeInt64,
			offset:    16,
			target:    le64(-2),
			wantAddrs: []uintptr{base + 16},
		},
		{
			name:      "int64 at a 4-aligned but not 8-aligned offset is skipped",
			vt:        TypeInt64,
			offset:    12,
			target:    le64(-2),
			wantAddrs: nil,
		},
		{
			name:      "float32 is found",
			vt:        TypeFloat32,
			offset:    4,
			target:    lef32(1.5),
			wantAddrs: []uintptr{base + 4},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := drivertest.New(drivertest.Region{
				Start: base,
				Name:  "[heap]",
				Data:  regionData(64, map[int][]byte{c.offset: c.target}),
			})
			s := NewSession(1, c.vt, f)

			if err := s.Search(c.target); err != nil {
				t.Fatalf("Search: %v", err)
			}
			got := sortedAddrs(s.Snapshot())
			if !slices.Equal(got, c.wantAddrs) {
				t.Errorf("Search addrs = %#x, want %#x", got, c.wantAddrs)
			}
		})
	}
}

func TestSearchMultipleMatchesAndValues(t *testing.T) {
	const base = 0x2000
	f := drivertest.New(drivertest.Region{
		Start: base,
		Name:  "[heap]",
		Data: regionData(32, map[int][]byte{
			0:  le32(7),
			8:  le32(7),
			12: le32(9),
			20: le32(7),
		}),
	})
	s := NewSession(1, TypeInt32, f)

	if err := s.Search(le32(7)); err != nil {
		t.Fatalf("Search: %v", err)
	}

	want := []uintptr{base, base + 8, base + 20}
	snap := s.Snapshot()
	if got := sortedAddrs(snap); !slices.Equal(got, want) {
		t.Fatalf("Search addrs = %#x, want %#x", got, want)
	}
	for _, addr := range want {
		if got, want := FormatValue(snap[addr], TypeInt32), "7"; got != want {
			t.Errorf("value at 0x%x = %s, want %s", addr, got, want)
		}
	}
	if !s.HasCandidates() {
		t.Errorf("HasCandidates() = false after a successful search")
	}
	if got, want := s.CandidateCount(), 3; got != want {
		t.Errorf("CandidateCount() = %d, want %d", got, want)
	}
}

func TestSearchUsesBulkRegionReads(t *testing.T) {
	// A scan must be one ReadRegion per (small) region, never a Peek per address.
	f := drivertest.New(
		drivertest.Region{Start: 0x1000, Name: "[heap]", Data: regionData(64, map[int][]byte{0: le32(5)})},
		drivertest.Region{Start: 0x2000, Name: "[stack]", Data: regionData(64, nil)},
	)
	s := NewSession(1, TypeInt32, f)
	if err := s.Search(le32(5)); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got, want := f.RegionReads, 2; got != want {
		t.Errorf("RegionReads = %d, want %d (one bulk read per region)", got, want)
	}
	if got := f.Peeks; got != 0 {
		t.Errorf("Peeks = %d, want 0 during a bulk scan", got)
	}
}

func TestSearchSkipsRegionsSmallerThanTheType(t *testing.T) {
	f := drivertest.New(
		drivertest.Region{Start: 0x1000, Name: "[heap]", Data: []byte{1, 2}},
		drivertest.Region{Start: 0x2000, Name: "[heap]", Data: regionData(8, map[int][]byte{0: le64(3)})},
	)
	s := NewSession(1, TypeInt64, f)
	if err := s.Search(le64(3)); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got, want := f.RegionReads, 1; got != want {
		t.Errorf("RegionReads = %d, want %d (the 2-byte region must be skipped)", got, want)
	}
	if got, want := sortedAddrs(s.Snapshot()), []uintptr{0x2000}; !slices.Equal(got, want) {
		t.Errorf("Search addrs = %#x, want %#x", got, want)
	}
}

func TestSearchReadErrorYieldsNoCandidates(t *testing.T) {
	f := drivertest.New(drivertest.Region{
		Start: 0x1000, Name: "[heap]", Data: regionData(64, map[int][]byte{0: le32(5)}),
	})
	f.ReadErr = errFake
	s := NewSession(1, TypeInt32, f)

	// A region that cannot be read is skipped, not fatal.
	if err := s.Search(le32(5)); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := s.CandidateCount(); got != 0 {
		t.Errorf("CandidateCount() = %d, want 0 when every region read fails", got)
	}
}

func TestSearchEmptyByteTarget(t *testing.T) {
	f := drivertest.New(drivertest.Region{Start: 0x1000, Name: "[heap]", Data: regionData(16, nil)})
	s := NewSession(1, TypeBytes, f)
	if err := s.Search(nil); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := s.CandidateCount(); got != 0 {
		t.Errorf("CandidateCount() = %d, want 0 for a zero-width target", got)
	}
}

// --- SearchFiltered ----------------------------------------------------------

func TestSearchFilteredRegions(t *testing.T) {
	const size = 64
	newFake := func() *drivertest.Fake {
		return drivertest.New(
			drivertest.Region{Start: 0x1000, Name: "[heap]", Data: regionData(size, map[int][]byte{0: le32(11)})},
			drivertest.Region{Start: 0x2000, Name: "[stack]", Data: regionData(size, map[int][]byte{0: le32(11)})},
			drivertest.Region{Start: 0x3000, Name: "", Data: regionData(size, map[int][]byte{0: le32(11)})},
			drivertest.Region{Start: 0x4000, Name: "/system/lib/libc.so", Data: regionData(size, map[int][]byte{0: le32(11)})},
		)
	}

	cases := []struct {
		name        string
		filter      driver.RegionFilter
		start, end  uintptr
		wantAddrs   []uintptr
		wantRegions int
	}{
		{"all", driver.RegionAll, 0, 0, []uintptr{0x1000, 0x2000, 0x3000, 0x4000}, 4},
		{"heap", driver.RegionHeap, 0, 0, []uintptr{0x1000}, 1},
		{"stack", driver.RegionStack, 0, 0, []uintptr{0x2000}, 1},
		{"anon", driver.RegionAnon, 0, 0, []uintptr{0x3000}, 1},
		{"custom range covers stack and anon", driver.RegionCustom, 0x2000, 0x3040, []uintptr{0x2000, 0x3000}, 2},
		{"custom range covers nothing", driver.RegionCustom, 0x9000, 0x9100, nil, 0},
		{"custom range must fully contain the region", driver.RegionCustom, 0x1000, 0x1020, nil, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake()
			s := NewSession(1, TypeInt32, f)

			if err := s.SearchFiltered(le32(11), c.filter, c.start, c.end); err != nil {
				t.Fatalf("SearchFiltered: %v", err)
			}
			if got := sortedAddrs(s.Snapshot()); !slices.Equal(got, c.wantAddrs) {
				t.Errorf("addrs = %#x, want %#x", got, c.wantAddrs)
			}
			if got := f.RegionReads; got != c.wantRegions {
				t.Errorf("RegionReads = %d, want %d (only matching regions may be read)", got, c.wantRegions)
			}
		})
	}
}

// --- equalScan ---------------------------------------------------------------

func TestEqualScan(t *testing.T) {
	target := []byte{0xAA, 0xBB}

	cases := []struct {
		name    string
		buf     []byte
		base    uintptr
		width   int
		stride  int
		wantHit []uintptr
	}{
		{
			name:    "stride 1 finds unaligned matches",
			buf:     []byte{0x00, 0xAA, 0xBB, 0x00, 0xAA, 0xBB},
			base:    0x100,
			width:   2,
			stride:  1,
			wantHit: []uintptr{0x101, 0x104},
		},
		{
			name:    "stride 2 from an aligned base skips odd offsets",
			buf:     []byte{0x00, 0xAA, 0xBB, 0x00, 0xAA, 0xBB},
			base:    0x100,
			width:   2,
			stride:  2,
			wantHit: []uintptr{0x104},
		},
		{
			// Alignment is computed from the absolute address, so an odd base
			// shifts the scanned offsets by one and a different match is seen.
			name:    "stride is measured against the absolute address, not the buffer",
			buf:     []byte{0x00, 0xAA, 0xBB, 0x00, 0xAA, 0xBB},
			base:    0x101,
			width:   2,
			stride:  2,
			wantHit: []uintptr{0x102},
		},
		{
			name:    "match at the very end of the buffer",
			buf:     []byte{0x00, 0x00, 0xAA, 0xBB},
			base:    0x200,
			width:   2,
			stride:  2,
			wantHit: []uintptr{0x202},
		},
		{
			name:    "buffer shorter than the target yields nothing",
			buf:     []byte{0xAA},
			base:    0x300,
			width:   2,
			stride:  1,
			wantHit: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []uintptr
			equalScan(target, c.width, c.stride)(c.buf, c.base, func(addr uintptr, val []byte) {
				if !bytes.Equal(val, target) {
					t.Errorf("emitted value %x at 0x%x, want %x", val, addr, target)
				}
				got = append(got, addr)
			})
			slices.Sort(got)
			if !slices.Equal(got, c.wantHit) {
				t.Errorf("equalScan hits = %#x, want %#x", got, c.wantHit)
			}
		})
	}
}

// --- chunked scanning --------------------------------------------------------

// indexScan is a cheap scanFunc used to exercise scanRegionChunked's overlap
// handling over a region larger than scanChunkBytes without paying for a
// byte-by-byte comparison of 32 MiB.
func indexScan(target []byte) scanFunc {
	return func(buf []byte, base uintptr, emit emitFunc) {
		for off := 0; off+len(target) <= len(buf); {
			i := bytes.Index(buf[off:], target)
			if i < 0 {
				return
			}
			emit(base+uintptr(off+i), buf[off+i:off+i+len(target)])
			off += i + 1
		}
	}
}

func TestScanRegionChunkedFindsStraddlingMatch(t *testing.T) {
	const (
		base   = uintptr(0x40000000)
		extra  = 4096
		size   = scanChunkBytes + extra
		marker = "STRADDLE"
	)
	target := []byte(marker)

	cases := []struct {
		name   string
		offset int
	}{
		{"before the first chunk boundary", scanChunkBytes - 2*len(marker)},
		{"straddling the first chunk boundary", scanChunkBytes - len(marker)/2},
		{"ending exactly on the chunk boundary", scanChunkBytes - len(marker)},
		{"starting exactly on the chunk boundary", scanChunkBytes},
		{"in the trailing chunk", scanChunkBytes + extra/2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := make([]byte, size)
			copy(data[c.offset:], target)
			f := drivertest.New(drivertest.Region{Start: base, Name: "[heap]", Data: data})
			regions, err := f.ReadMaps(1)
			if err != nil {
				t.Fatalf("ReadMaps: %v", err)
			}

			found := scanRegions(f, 1, regions, len(target), indexScan(target), nil)

			want := []uintptr{base + uintptr(c.offset)}
			if got := sortedAddrs(found); !slices.Equal(got, want) {
				t.Fatalf("match addrs = %#x, want %#x", got, want)
			}
			if got := found[want[0]]; !bytes.Equal(got, target) {
				t.Errorf("match value = %q, want %q", got, target)
			}
			if f.RegionReads < 2 {
				t.Errorf("RegionReads = %d, want at least 2 chunk reads for a %d-byte region", f.RegionReads, size)
			}
		})
	}
}

func TestScanRegionChunkedStopsOnReadError(t *testing.T) {
	f := drivertest.New(drivertest.Region{
		Start: 0x1000, Name: "[heap]", Data: regionData(64, map[int][]byte{0: le32(1)}),
	})
	f.ReadErr = errFake

	emitted := 0
	scanRegionChunked(f, 1, driver.Region{Start: 0x1000, End: 0x1040}, 64, 4,
		equalScan(le32(1), 4, 4),
		func(uintptr, []byte) { emitted++ },
		nil,
	)
	if emitted != 0 {
		t.Errorf("emitted %d matches after a read error, want 0", emitted)
	}
	if got, want := f.RegionReads, 1; got != want {
		t.Errorf("RegionReads = %d, want %d (the region must be abandoned)", got, want)
	}
}

func TestScanRegionsZeroWidth(t *testing.T) {
	f := drivertest.New(drivertest.Region{Start: 0x1000, Name: "[heap]", Data: regionData(16, nil)})
	regions, _ := f.ReadMaps(1)
	got := scanRegions(f, 1, regions, 0, equalScan(nil, 0, 1), nil)
	if len(got) != 0 {
		t.Errorf("scanRegions with width 0 = %v, want empty", got)
	}
	if f.RegionReads != 0 {
		t.Errorf("RegionReads = %d, want 0 for a zero-width scan", f.RegionReads)
	}
}

func TestScanLimit(t *testing.T) {
	t.Run("nil limit is unlimited", func(t *testing.T) {
		var l *scanLimit
		l.add(100)
		if l.reached() {
			t.Errorf("nil scanLimit.reached() = true, want false")
		}
	})
	t.Run("zero max is unlimited", func(t *testing.T) {
		l := newScanLimit(0)
		l.add(1000)
		if l.reached() {
			t.Errorf("scanLimit{max:0}.reached() = true, want false")
		}
	})
	t.Run("reaches max", func(t *testing.T) {
		l := newScanLimit(3)
		if l.reached() {
			t.Errorf("reached() = true before adding anything")
		}
		l.add(2)
		if l.reached() {
			t.Errorf("reached() = true at 2/3")
		}
		l.add(1)
		if !l.reached() {
			t.Errorf("reached() = false at 3/3")
		}
	})
}

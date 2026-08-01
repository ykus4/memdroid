package search

import (
	"slices"
	"testing"

	"memdroid/internal/driver/drivertest"
)

func TestNewSessionAccessors(t *testing.T) {
	f := drivertest.New()
	s := NewSession(42, TypeInt64, f)

	if got, want := s.PID(), 42; got != want {
		t.Errorf("PID() = %d, want %d", got, want)
	}
	if got, want := s.Type(), TypeInt64; got != want {
		t.Errorf("Type() = %v, want %v", got, want)
	}
	if s.HasCandidates() {
		t.Errorf("HasCandidates() = true on a fresh session, want false")
	}
	if got := s.CandidateCount(); got != 0 {
		t.Errorf("CandidateCount() = %d on a fresh session, want 0", got)
	}
	if got := s.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot() = %v on a fresh session, want empty", got)
	}
}

func TestSetType(t *testing.T) {
	cases := []struct {
		name     string
		from, to ValueType
		wantKept bool
	}{
		{"same type keeps candidates", TypeInt32, TypeInt32, true},
		{"wider type discards", TypeInt32, TypeInt64, false},
		{"same width, different signedness discards", TypeInt32, TypeUint32, false},
		{"float of same width discards", TypeUint32, TypeFloat32, false},
		{"switch to bytes discards", TypeInt64, TypeBytes, false},
		{"switch away from bytes discards", TypeBytes, TypeInt32, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewSession(1, c.from, drivertest.New())
			s.SetCandidates(map[uintptr][]byte{0x1000: {1, 2, 3, 4}})
			if !s.HasCandidates() {
				t.Fatalf("setup: HasCandidates() = false, want true")
			}

			s.SetType(c.to)

			if got := s.Type(); got != c.to {
				t.Errorf("Type() = %v, want %v", got, c.to)
			}
			if got := s.HasCandidates(); got != c.wantKept {
				t.Errorf("HasCandidates() after SetType(%v) = %v, want %v", c.to, got, c.wantKept)
			}
			wantCount := 0
			if c.wantKept {
				wantCount = 1
			}
			if got := s.CandidateCount(); got != wantCount {
				t.Errorf("CandidateCount() after SetType(%v) = %d, want %d", c.to, got, wantCount)
			}
		})
	}
}

func TestSetCandidatesAs(t *testing.T) {
	s := NewSession(1, TypeInt32, drivertest.New())
	s.SetCandidates(map[uintptr][]byte{0x1000: le32(1)})

	// SetCandidatesAs must swap type and candidates atomically: setting the type
	// first would have discarded the new candidates.
	s.SetCandidatesAs(TypeBytes, map[uintptr][]byte{0x2000: {0xde, 0xad, 0xbe}})

	if got, want := s.Type(), TypeBytes; got != want {
		t.Errorf("Type() = %v, want %v", got, want)
	}
	if got, want := s.CandidateCount(), 1; got != want {
		t.Errorf("CandidateCount() = %d, want %d", got, want)
	}
	if !s.HasCandidates() {
		t.Errorf("HasCandidates() = false, want true")
	}
	snap := s.Snapshot()
	if _, ok := snap[0x2000]; !ok {
		t.Errorf("Snapshot() = %v, want key 0x2000", snap)
	}
	if _, ok := snap[0x1000]; ok {
		t.Errorf("Snapshot() still holds the old candidate at 0x1000")
	}
}

func TestReset(t *testing.T) {
	s := NewSession(1, TypeInt32, drivertest.New())
	s.SetCandidates(map[uintptr][]byte{0x1000: le32(7), 0x1004: le32(7)})

	s.Reset()

	if s.HasCandidates() {
		t.Errorf("HasCandidates() after Reset = true, want false")
	}
	if got := s.CandidateCount(); got != 0 {
		t.Errorf("CandidateCount() after Reset = %d, want 0", got)
	}
	if got, want := s.Type(), TypeInt32; got != want {
		t.Errorf("Reset must not change the type: got %v, want %v", got, want)
	}
}

func TestSetDriver(t *testing.T) {
	// A session built without a driver (as when loaded from disk) becomes
	// usable once a driver is bound.
	s := NewSession(1, TypeInt32, nil)
	f := drivertest.New(drivertest.Region{
		Start: 0x1000,
		Name:  "[heap]",
		Data:  regionData(16, map[int][]byte{0: le32(99)}),
	})
	s.SetDriver(f)

	if err := s.Search(le32(99)); err != nil {
		t.Fatalf("Search after SetDriver: %v", err)
	}
	if got, want := s.CandidateCount(), 1; got != want {
		t.Errorf("CandidateCount() = %d, want %d", got, want)
	}
}

func TestSnapshotDeepCopies(t *testing.T) {
	s := NewSession(1, TypeInt32, drivertest.New())
	s.SetCandidates(map[uintptr][]byte{
		0x1000: le32(1),
		0x1004: le32(2),
	})

	snap := s.Snapshot()
	if got, want := len(snap), 2; got != want {
		t.Fatalf("len(Snapshot()) = %d, want %d", got, want)
	}

	// Mutating the returned map and its values must not touch the session.
	snap[0x1000][0] = 0xff
	delete(snap, 0x1004)
	snap[0x9999] = le32(3)

	again := s.Snapshot()
	if got, want := len(again), 2; got != want {
		t.Fatalf("len(Snapshot()) after mutating the copy = %d, want %d", got, want)
	}
	if got, want := FormatValue(again[0x1000], TypeInt32), "1"; got != want {
		t.Errorf("value at 0x1000 = %s, want %s", got, want)
	}
	if _, ok := again[0x1004]; !ok {
		t.Errorf("candidate 0x1004 vanished after deleting it from a snapshot")
	}
	if _, ok := again[0x9999]; ok {
		t.Errorf("candidate 0x9999 appeared after adding it to a snapshot")
	}
}

func TestPage(t *testing.T) {
	// Insert out of order so a correct implementation must sort.
	cands := map[uintptr][]byte{
		0x30: le32(3),
		0x10: le32(1),
		0x50: le32(5),
		0x20: le32(2),
		0x40: le32(4),
	}

	cases := []struct {
		name          string
		offset, limit int
		wantAddrs     []uintptr
	}{
		{"first page", 0, 2, []uintptr{0x10, 0x20}},
		{"middle page", 2, 2, []uintptr{0x30, 0x40}},
		{"last partial page", 3, 10, []uintptr{0x40, 0x50}},
		{"limit larger than total", 0, 100, []uintptr{0x10, 0x20, 0x30, 0x40, 0x50}},
		{"offset at total", 5, 2, nil},
		{"offset past end", 99, 2, nil},
		{"negative offset clamps to zero", -3, 2, []uintptr{0x10, 0x20}},
		{"zero limit", 0, 0, nil},
		{"negative limit", 0, -1, nil},
		{"single entry", 4, 1, []uintptr{0x50}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewSession(1, TypeInt32, drivertest.New())
			s.SetCandidates(cloneCands(cands))

			page, total := s.Page(c.offset, c.limit)

			if total != len(cands) {
				t.Errorf("Page(%d, %d) total = %d, want %d", c.offset, c.limit, total, len(cands))
			}
			got := make([]uintptr, len(page))
			for i, p := range page {
				got[i] = p.Addr
			}
			if !slices.Equal(got, c.wantAddrs) {
				t.Errorf("Page(%d, %d) addrs = %#x, want %#x", c.offset, c.limit, got, c.wantAddrs)
			}
			if c.wantAddrs == nil && page != nil {
				t.Errorf("Page(%d, %d) = %v, want nil page", c.offset, c.limit, page)
			}
			// Values must line up with their addresses.
			for _, p := range page {
				want := FormatValue(cands[p.Addr], TypeInt32)
				if got := FormatValue(p.Value, TypeInt32); got != want {
					t.Errorf("value at 0x%x = %s, want %s", p.Addr, got, want)
				}
			}
		})
	}
}

func TestPageEmptySession(t *testing.T) {
	s := NewSession(1, TypeInt32, drivertest.New())
	page, total := s.Page(0, 10)
	if page != nil {
		t.Errorf("Page on empty session = %v, want nil", page)
	}
	if total != 0 {
		t.Errorf("Page on empty session total = %d, want 0", total)
	}
}

func TestPageCopiesValues(t *testing.T) {
	s := NewSession(1, TypeInt32, drivertest.New())
	s.SetCandidates(map[uintptr][]byte{0x10: le32(1)})

	page, _ := s.Page(0, 1)
	if len(page) != 1 {
		t.Fatalf("len(page) = %d, want 1", len(page))
	}
	page[0].Value[0] = 0xff

	again, _ := s.Page(0, 1)
	if got, want := FormatValue(again[0].Value, TypeInt32), "1"; got != want {
		t.Errorf("value after mutating a page copy = %s, want %s", got, want)
	}
}

func TestCandidateWidth(t *testing.T) {
	cases := []struct {
		name  string
		vt    ValueType
		cands map[uintptr][]byte
		want  int
	}{
		{"int32", TypeInt32, nil, 4},
		{"int64", TypeInt64, nil, 8},
		{"float32", TypeFloat32, nil, 4},
		{"float64", TypeFloat64, nil, 8},
		{"uint32", TypeUint32, nil, 4},
		{"uint64", TypeUint64, nil, 8},
		{"bytes uses candidate length", TypeBytes, map[uintptr][]byte{0x10: {1, 2, 3}}, 3},
		{"bytes with no candidates falls back to 1", TypeBytes, nil, 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewSession(1, c.vt, drivertest.New())
			if c.cands != nil {
				s.SetCandidates(c.cands)
			}
			if got := s.candidateWidth(); got != c.want {
				t.Errorf("candidateWidth() = %d, want %d", got, c.want)
			}
		})
	}
}

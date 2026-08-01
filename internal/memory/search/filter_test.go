package search

import (
	"slices"
	"strings"
	"testing"

	"memdroid/internal/driver/drivertest"
)

// filterBase is the start of the single heap region used by the filter tests.
const filterBase = uintptr(0x1000)

// newFilterSession builds a session whose four int32 candidates all hold 100,
// then rewrites memory so each candidate takes a different direction:
//
//	0x1000 -> 100 (unchanged)
//	0x1004 -> 150 (increased)
//	0x1008 ->  50 (decreased)
//	0x100c -> 100 (unchanged)
func newFilterSession(t *testing.T) (*Session, *drivertest.Fake) {
	t.Helper()

	f := drivertest.New(drivertest.Region{
		Start: filterBase,
		Name:  "[heap]",
		Data: regionData(32, map[int][]byte{
			0:  le32(100),
			4:  le32(100),
			8:  le32(100),
			12: le32(100),
			16: le32(7), // decoy: never a candidate
		}),
	})

	s := NewSession(1, TypeInt32, f)
	if err := s.Search(le32(100)); err != nil {
		t.Fatalf("setup search: %v", err)
	}
	if got, want := s.CandidateCount(), 4; got != want {
		t.Fatalf("setup: CandidateCount() = %d, want %d", got, want)
	}

	for addr, v := range map[uintptr]int32{
		filterBase + 4: 150,
		filterBase + 8: 50,
	} {
		if err := f.Poke(1, addr, le32(v)); err != nil {
			t.Fatalf("setup poke 0x%x: %v", addr, err)
		}
	}
	return s, f
}

func TestFilterModes(t *testing.T) {
	cases := []struct {
		name      string
		mode      FilterMode
		target    []byte
		wantAddrs []uintptr
	}{
		{"changed", FilterChanged, nil, []uintptr{filterBase + 4, filterBase + 8}},
		{"unchanged", FilterUnchanged, nil, []uintptr{filterBase, filterBase + 12}},
		{"increased", FilterIncreased, nil, []uintptr{filterBase + 4}},
		{"decreased", FilterDecreased, nil, []uintptr{filterBase + 8}},
		{"value hits the increased slot", FilterValue, le32(150), []uintptr{filterBase + 4}},
		{"value hits the untouched slots", FilterValue, le32(100), []uintptr{filterBase, filterBase + 12}},
		{"value matches nothing", FilterValue, le32(4242), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := newFilterSession(t)

			if err := s.Filter(c.mode, c.target); err != nil {
				t.Fatalf("Filter(%v): %v", c.mode, err)
			}

			snap := s.Snapshot()
			if got := sortedAddrs(snap); !slices.Equal(got, c.wantAddrs) {
				t.Errorf("Filter(%v) addrs = %#x, want %#x", c.mode, got, c.wantAddrs)
			}
			if got, want := s.CandidateCount(), len(c.wantAddrs); got != want {
				t.Errorf("CandidateCount() = %d, want %d", got, want)
			}
			// Surviving candidates must carry the freshly read value.
			for addr, val := range snap {
				want := FormatValue(memAt(t, s, addr, 4), TypeInt32)
				if got := FormatValue(val, TypeInt32); got != want {
					t.Errorf("stored value at 0x%x = %s, want the current %s", addr, got, want)
				}
			}
		})
	}
}

// memAt reads sz bytes at addr through the session's driver.
func memAt(t *testing.T, s *Session, addr uintptr, sz int) []byte {
	t.Helper()
	_, _, drv := s.scanContext()
	b, err := drv.Peek(1, addr, sz)
	if err != nil {
		t.Fatalf("peek 0x%x: %v", addr, err)
	}
	return b
}

func TestFilterChainNarrows(t *testing.T) {
	s, f := newFilterSession(t)

	if err := s.Filter(FilterUnchanged, nil); err != nil {
		t.Fatalf("Filter(unchanged): %v", err)
	}
	if got, want := s.CandidateCount(), 2; got != want {
		t.Fatalf("after unchanged: CandidateCount() = %d, want %d", got, want)
	}

	// Move only one of the two survivors.
	if err := f.Poke(1, filterBase+12, le32(101)); err != nil {
		t.Fatalf("poke: %v", err)
	}
	if err := s.Filter(FilterIncreased, nil); err != nil {
		t.Fatalf("Filter(increased): %v", err)
	}

	want := []uintptr{filterBase + 12}
	if got := sortedAddrs(s.Snapshot()); !slices.Equal(got, want) {
		t.Errorf("after increased: addrs = %#x, want %#x", got, want)
	}
}

func TestFilterErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T) *Session
		mode    FilterMode
		target  []byte
		wantErr string
	}{
		{
			name:    "empty session",
			setup:   func(*testing.T) *Session { return NewSession(1, TypeInt32, drivertest.New()) },
			mode:    FilterChanged,
			wantErr: "no active search session",
		},
		{
			name: "reset session",
			setup: func(t *testing.T) *Session {
				s, _ := newFilterSession(t)
				s.Reset()
				return s
			},
			mode:    FilterUnchanged,
			wantErr: "no active search session",
		},
		{
			name:    "value mode without a target",
			setup:   func(t *testing.T) *Session { s, _ := newFilterSession(t); return s },
			mode:    FilterValue,
			target:  nil,
			wantErr: "requires a target value",
		},
		{
			name:    "value mode with an empty target",
			setup:   func(t *testing.T) *Session { s, _ := newFilterSession(t); return s },
			mode:    FilterValue,
			target:  []byte{},
			wantErr: "requires a target value",
		},
		{
			name:    "mode above the range",
			setup:   func(t *testing.T) *Session { s, _ := newFilterSession(t); return s },
			mode:    FilterValue + 1,
			wantErr: "invalid filter mode",
		},
		{
			name:    "mode below the range",
			setup:   func(t *testing.T) *Session { s, _ := newFilterSession(t); return s },
			mode:    FilterChanged - 1,
			wantErr: "invalid filter mode",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := c.setup(t)
			before := s.CandidateCount()

			err := s.Filter(c.mode, c.target)
			if err == nil {
				t.Fatalf("Filter(%d) = nil, want error containing %q", c.mode, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Filter(%d) error = %q, want it to contain %q", c.mode, err, c.wantErr)
			}
			if got := s.CandidateCount(); got != before {
				t.Errorf("a failed filter changed the candidate count: %d -> %d", before, got)
			}
		})
	}
}

func TestFilterFallsBackToPeekWhenBulkReadFails(t *testing.T) {
	s, f := newFilterSession(t)
	f.ReadErr = errFake
	f.RegionReads, f.Peeks = 0, 0

	if err := s.Filter(FilterUnchanged, nil); err != nil {
		t.Fatalf("Filter: %v", err)
	}

	want := []uintptr{filterBase, filterBase + 12}
	if got := sortedAddrs(s.Snapshot()); !slices.Equal(got, want) {
		t.Errorf("addrs = %#x, want %#x", got, want)
	}
	if got, want := f.Peeks, 4; got != want {
		t.Errorf("Peeks = %d, want %d (one point read per candidate)", got, want)
	}
}

func TestFilterDropsUnmappedCandidates(t *testing.T) {
	s, _ := newFilterSession(t)
	snap := s.Snapshot()
	snap[0x99000000] = le32(100) // never mapped in the fake
	s.SetCandidates(snap)

	if err := s.Filter(FilterUnchanged, nil); err != nil {
		t.Fatalf("Filter: %v", err)
	}

	want := []uintptr{filterBase, filterBase + 12}
	if got := sortedAddrs(s.Snapshot()); !slices.Equal(got, want) {
		t.Errorf("addrs = %#x, want %#x (the unmapped candidate must be dropped)", got, want)
	}
}

func TestFilterBytesUsesCandidateLength(t *testing.T) {
	f := drivertest.New(drivertest.Region{
		Start: filterBase,
		Name:  "[heap]",
		Data:  regionData(16, map[int][]byte{0: {0xde, 0xad, 0xbe}, 8: {0xde, 0xad, 0xbe}}),
	})
	s := NewSession(1, TypeBytes, f)
	if err := s.Search([]byte{0xde, 0xad, 0xbe}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got, want := s.CandidateCount(), 2; got != want {
		t.Fatalf("CandidateCount() = %d, want %d", got, want)
	}

	if err := f.Poke(1, filterBase+8, []byte{0x00}); err != nil {
		t.Fatalf("poke: %v", err)
	}
	if err := s.Filter(FilterUnchanged, nil); err != nil {
		t.Fatalf("Filter: %v", err)
	}

	want := []uintptr{filterBase}
	if got := sortedAddrs(s.Snapshot()); !slices.Equal(got, want) {
		t.Errorf("addrs = %#x, want %#x", got, want)
	}
}

func TestFilterKeep(t *testing.T) {
	cases := []struct {
		name            string
		mode            FilterMode
		cur, prev, targ []byte
		vt              ValueType
		want            bool
	}{
		{name: "changed: differing bytes", mode: FilterChanged, cur: le32(2), prev: le32(1), vt: TypeInt32, want: true},
		{name: "changed: identical bytes", mode: FilterChanged, cur: le32(1), prev: le32(1), vt: TypeInt32, want: false},
		{name: "unchanged: identical bytes", mode: FilterUnchanged, cur: le32(1), prev: le32(1), vt: TypeInt32, want: true},
		{name: "increased", mode: FilterIncreased, cur: le32(2), prev: le32(1), vt: TypeInt32, want: true},
		{name: "increased: equal is not an increase", mode: FilterIncreased, cur: le32(1), prev: le32(1), vt: TypeInt32, want: false},
		{name: "increased: signed -1 -> 0", mode: FilterIncreased, cur: le32(0), prev: le32(-1), vt: TypeInt32, want: true},
		{name: "increased: unsigned view of -1 is huge", mode: FilterIncreased, cur: le32(0), prev: le32(-1), vt: TypeUint32, want: false},
		{name: "decreased", mode: FilterDecreased, cur: le32(1), prev: le32(2), vt: TypeInt32, want: true},
		{name: "decreased: float", mode: FilterDecreased, cur: lef32(-2.5), prev: lef32(1.5), vt: TypeFloat32, want: true},
		{name: "value: exact match", mode: FilterValue, cur: le32(5), prev: le32(1), targ: le32(5), vt: TypeInt32, want: true},
		{name: "value: mismatch", mode: FilterValue, cur: le32(6), prev: le32(1), targ: le32(5), vt: TypeInt32, want: false},
		{name: "bytes are unordered so increased never keeps", mode: FilterIncreased, cur: []byte{9}, prev: []byte{1}, vt: TypeBytes, want: false},
		{name: "unknown mode keeps nothing", mode: FilterMode(99), cur: le32(1), prev: le32(1), vt: TypeInt32, want: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := filterKeep(c.mode, c.cur, c.prev, c.targ, c.vt); got != c.want {
				t.Errorf("filterKeep(%v) = %v, want %v", c.mode, got, c.want)
			}
		})
	}
}

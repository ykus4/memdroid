package search

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"memdroid/internal/driver/drivertest"
)

func TestParsePattern(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Pattern
		wantErr string
	}{
		{
			name: "literals only",
			in:   "FF 00 AB",
			want: Pattern{{Value: 0xFF}, {Value: 0x00}, {Value: 0xAB}},
		},
		{
			name: "lowercase hex",
			in:   "de ad be ef",
			want: Pattern{{Value: 0xDE}, {Value: 0xAD}, {Value: 0xBE}, {Value: 0xEF}},
		},
		{
			name: "double-question wildcard",
			in:   "FF ?? 01",
			want: Pattern{{Value: 0xFF}, {Wildcard: true}, {Value: 0x01}},
		},
		{
			name: "single-question wildcard",
			in:   "FF ? 01",
			want: Pattern{{Value: 0xFF}, {Wildcard: true}, {Value: 0x01}},
		},
		{
			name: "single-digit hex token",
			in:   "F 0",
			want: Pattern{{Value: 0x0F}, {Value: 0x00}},
		},
		{
			name: "extra whitespace is ignored",
			in:   "  FF\t00\n?? ",
			want: Pattern{{Value: 0xFF}, {Value: 0x00}, {Wildcard: true}},
		},
		{
			name: "all wildcards",
			in:   "?? ??",
			want: Pattern{{Wildcard: true}, {Wildcard: true}},
		},
		{name: "empty input", in: "", wantErr: "empty pattern"},
		{name: "whitespace only", in: "   \t\n ", wantErr: "empty pattern"},
		{name: "non-hex token", in: "FF GG", wantErr: `invalid token "GG"`},
		{name: "token too wide for a byte", in: "1FF", wantErr: `invalid token "1FF"`},
		{name: "negative token", in: "-1", wantErr: `invalid token "-1"`},
		{name: "0x prefix is not accepted", in: "0xFF", wantErr: `invalid token "0xFF"`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParsePattern(c.in)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("ParsePattern(%q) = %v, nil; want error containing %q", c.in, got, c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("ParsePattern(%q) error = %q, want it to contain %q", c.in, err, c.wantErr)
				}
				if got != nil {
					t.Errorf("ParsePattern(%q) = %v on error, want nil", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePattern(%q): %v", c.in, err)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("ParsePattern(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestPatternMatch(t *testing.T) {
	p := Pattern{{Value: 0xFF}, {Wildcard: true}, {Value: 0x01}}
	cases := []struct {
		buf  []byte
		want bool
	}{
		{[]byte{0xFF, 0x00, 0x01}, true},
		{[]byte{0xFF, 0xAB, 0x01}, true},
		{[]byte{0xFF, 0xAB, 0x01, 0x99}, true}, // trailing bytes are ignored
		{[]byte{0xFE, 0xAB, 0x01}, false},
		{[]byte{0xFF, 0xAB, 0x02}, false},
	}
	for _, c := range cases {
		if got := p.match(c.buf); got != c.want {
			t.Errorf("match(%x) = %v, want %v", c.buf, got, c.want)
		}
	}
}

func TestSearchPattern(t *testing.T) {
	const base = 0x1000
	// Layout: two "DE AD BE" runs plus a near miss that only a wildcard matches.
	data := regionData(32, map[int][]byte{
		0:  {0xDE, 0xAD, 0xBE},
		10: {0xDE, 0x11, 0xBE},
		20: {0xDE, 0xAD, 0xBE},
	})

	cases := []struct {
		name      string
		pattern   string
		wantAddrs []uintptr
		wantVals  [][]byte
	}{
		{
			name:      "literal pattern",
			pattern:   "DE AD BE",
			wantAddrs: []uintptr{base, base + 20},
			wantVals:  [][]byte{{0xDE, 0xAD, 0xBE}, {0xDE, 0xAD, 0xBE}},
		},
		{
			name:      "wildcard widens the match",
			pattern:   "DE ?? BE",
			wantAddrs: []uintptr{base, base + 10, base + 20},
			wantVals:  [][]byte{{0xDE, 0xAD, 0xBE}, {0xDE, 0x11, 0xBE}, {0xDE, 0xAD, 0xBE}},
		},
		{
			name:      "single byte pattern",
			pattern:   "AD",
			wantAddrs: []uintptr{base + 1, base + 21},
			wantVals:  [][]byte{{0xAD}, {0xAD}},
		},
		{
			name:      "no match",
			pattern:   "CA FE",
			wantAddrs: nil,
			wantVals:  nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := drivertest.New(drivertest.Region{Start: base, Name: "[heap]", Data: bytes.Clone(data)})
			p, err := ParsePattern(c.pattern)
			if err != nil {
				t.Fatalf("ParsePattern(%q): %v", c.pattern, err)
			}

			res, err := SearchPattern(f, 1, p)
			if err != nil {
				t.Fatalf("SearchPattern: %v", err)
			}
			if res.Truncated {
				t.Errorf("Truncated = true, want false for %d matches", len(res.Matches))
			}
			if got := res.Addrs(); !slices.Equal(got, c.wantAddrs) {
				t.Fatalf("Addrs() = %#x, want %#x", got, c.wantAddrs)
			}
			for i, m := range res.Matches {
				if !bytes.Equal(m.Value, c.wantVals[i]) {
					t.Errorf("Matches[%d].Value = %x, want %x", i, m.Value, c.wantVals[i])
				}
			}

			cm := res.CandidateMap()
			if got, want := len(cm), len(c.wantAddrs); got != want {
				t.Errorf("len(CandidateMap()) = %d, want %d", got, want)
			}
			for i, addr := range c.wantAddrs {
				v, ok := cm[addr]
				if !ok {
					t.Errorf("CandidateMap() is missing 0x%x", addr)
					continue
				}
				if !bytes.Equal(v, c.wantVals[i]) {
					t.Errorf("CandidateMap()[0x%x] = %x, want %x", addr, v, c.wantVals[i])
				}
			}
		})
	}
}

func TestSearchPatternEmpty(t *testing.T) {
	f := drivertest.New(drivertest.Region{Start: 0x1000, Name: "[heap]", Data: regionData(16, nil)})
	for _, p := range []Pattern{nil, {}} {
		res, err := SearchPattern(f, 1, p)
		if err == nil {
			t.Errorf("SearchPattern(%v) = %v, nil; want an error", p, res)
		}
	}
}

func TestSearchPatternAddrsAndCandidateMapOnEmptyResult(t *testing.T) {
	var res PatternResult
	if got := res.Addrs(); len(got) != 0 {
		t.Errorf("Addrs() = %v, want empty", got)
	}
	if got := res.CandidateMap(); len(got) != 0 {
		t.Errorf("CandidateMap() = %v, want empty", got)
	}
}

func TestSearchPatternTruncates(t *testing.T) {
	// A run of identical bytes yields far more than PatternMaxResults matches
	// for a 2-byte pattern.
	const (
		base = uintptr(0x5000)
		size = 1000
	)
	data := bytes.Repeat([]byte{0xAA}, size)
	f := drivertest.New(drivertest.Region{Start: base, Name: "[heap]", Data: data})

	p, err := ParsePattern("AA AA")
	if err != nil {
		t.Fatalf("ParsePattern: %v", err)
	}
	res, err := SearchPattern(f, 1, p)
	if err != nil {
		t.Fatalf("SearchPattern: %v", err)
	}

	if !res.Truncated {
		t.Errorf("Truncated = false, want true (%d raw matches exist)", size-1)
	}
	if got, want := len(res.Matches), PatternMaxResults; got != want {
		t.Fatalf("len(Matches) = %d, want %d", got, want)
	}
	// The kept matches must be the lowest addresses, in order.
	addrs := res.Addrs()
	if !slices.IsSorted(addrs) {
		t.Errorf("Addrs() is not sorted: %#x", addrs)
	}
	if got, want := addrs[0], base; got != want {
		t.Errorf("Addrs()[0] = %#x, want %#x", got, want)
	}
	if got, want := addrs[len(addrs)-1], base+uintptr(PatternMaxResults-1); got != want {
		t.Errorf("last addr = %#x, want %#x", got, want)
	}
}

func TestSearchPatternSeedsSession(t *testing.T) {
	const base = 0x1000
	f := drivertest.New(drivertest.Region{
		Start: base, Name: "[heap]", Data: regionData(16, map[int][]byte{4: {0xCA, 0xFE}}),
	})
	p, err := ParsePattern("CA FE")
	if err != nil {
		t.Fatalf("ParsePattern: %v", err)
	}
	res, err := SearchPattern(f, 1, p)
	if err != nil {
		t.Fatalf("SearchPattern: %v", err)
	}

	s := NewSession(1, TypeInt32, f)
	s.SetCandidatesAs(TypeBytes, res.CandidateMap())

	if got, want := s.Type(), TypeBytes; got != want {
		t.Errorf("Type() = %v, want %v", got, want)
	}
	if got := sortedAddrs(s.Snapshot()); !slices.Equal(got, []uintptr{base + 4}) {
		t.Errorf("session addrs = %#x, want %#x", got, []uintptr{base + 4})
	}
	if got, want := s.candidateWidth(), 2; got != want {
		t.Errorf("candidateWidth() = %d, want %d", got, want)
	}
}

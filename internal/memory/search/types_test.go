package search

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseFormatRoundTrip(t *testing.T) {
	cases := []struct {
		vt  ValueType
		in  string
		out string
	}{
		{TypeInt32, "-5", "-5"},
		{TypeInt64, "9000000000", "9000000000"},
		{TypeUint32, "4294967295", "4294967295"},
		{TypeUint64, "18446744073709551615", "18446744073709551615"},
		{TypeFloat32, "1.5", "1.5"},
		{TypeFloat64, "3.25", "3.25"},
		{TypeBytes, "FF00AB", "ff00ab"},
	}
	for _, c := range cases {
		b, err := ParseValue(c.in, c.vt)
		if err != nil {
			t.Fatalf("ParseValue(%q, %v): %v", c.in, c.vt, err)
		}
		if got := FormatValue(b, c.vt); got != c.out {
			t.Errorf("round-trip %v %q: got %q want %q", c.vt, c.in, got, c.out)
		}
	}
}

func TestCompareValues(t *testing.T) {
	a, _ := ParseValue("10", TypeInt32)
	b, _ := ParseValue("20", TypeInt32)
	if CompareValues(a, b, TypeInt32) != -1 {
		t.Errorf("10 < 20 should be -1")
	}
	if CompareValues(b, a, TypeInt32) != 1 {
		t.Errorf("20 > 10 should be 1")
	}
	if CompareValues(a, a, TypeInt32) != 0 {
		t.Errorf("10 == 10 should be 0")
	}

	neg, _ := ParseValue("-1", TypeInt32)
	if CompareValues(neg, a, TypeInt32) != -1 {
		t.Errorf("signed compare: -1 < 10")
	}
	big, _ := ParseValue("4294967295", TypeUint32) // 0xFFFFFFFF
	if CompareValues(big, a, TypeUint32) != 1 {
		t.Errorf("unsigned compare: max > 10")
	}
}

func TestCompareValuesGuards(t *testing.T) {
	// Short slices must not panic and must return 0.
	if CompareValues([]byte{1}, []byte{2}, TypeInt32) != 0 {
		t.Errorf("short slice should compare as 0")
	}
	// TypeBytes is not ordered → 0.
	if CompareValues([]byte{1, 2, 3}, []byte{4, 5, 6}, TypeBytes) != 0 {
		t.Errorf("TypeBytes should compare as 0")
	}
}

func TestParseValueTypeAndFilterMode(t *testing.T) {
	if vt, err := ParseValueType("uint64"); err != nil || vt != TypeUint64 {
		t.Errorf("ParseValueType(uint64) = %v, %v", vt, err)
	}
	if _, err := ParseValueType("nope"); err == nil {
		t.Errorf("expected error for unknown type")
	}
	if m, err := ParseFilterMode("increased"); err != nil || m != FilterIncreased {
		t.Errorf("ParseFilterMode(increased) = %v, %v", m, err)
	}
}

// TestParseFormatRoundTripAllTypes covers every ValueType, including the
// negative and boundary values TestParseFormatRoundTrip leaves out.
func TestParseFormatRoundTripAllTypes(t *testing.T) {
	cases := []struct {
		name     string
		vt       ValueType
		in       string
		out      string // FormatValue output; defaults to in when empty
		wantSize int    // expected len(ParseValue) result
		wantRaw  []byte // optional exact little-endian encoding
	}{
		{name: "int32 zero", vt: TypeInt32, in: "0", wantSize: 4, wantRaw: []byte{0, 0, 0, 0}},
		{name: "int32 negative", vt: TypeInt32, in: "-1", wantSize: 4, wantRaw: []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{name: "int32 min", vt: TypeInt32, in: "-2147483648", wantSize: 4, wantRaw: []byte{0x00, 0x00, 0x00, 0x80}},
		{name: "int32 max", vt: TypeInt32, in: "2147483647", wantSize: 4, wantRaw: []byte{0xFF, 0xFF, 0xFF, 0x7F}},
		{name: "int64 negative", vt: TypeInt64, in: "-9000000000", wantSize: 8},
		{name: "int64 min", vt: TypeInt64, in: "-9223372036854775808", wantSize: 8},
		{name: "int64 max", vt: TypeInt64, in: "9223372036854775807", wantSize: 8},
		{name: "uint32 zero", vt: TypeUint32, in: "0", wantSize: 4},
		{name: "uint32 max", vt: TypeUint32, in: "4294967295", wantSize: 4},
		{name: "uint64 max", vt: TypeUint64, in: "18446744073709551615", wantSize: 8},
		{name: "float32 negative", vt: TypeFloat32, in: "-1.5", wantSize: 4, wantRaw: []byte{0x00, 0x00, 0xC0, 0xBF}},
		{name: "float32 fraction", vt: TypeFloat32, in: "0.1", wantSize: 4},
		{name: "float32 exponent", vt: TypeFloat32, in: "1e10", out: "1e+10", wantSize: 4},
		{name: "float64 negative", vt: TypeFloat64, in: "-3.25", wantSize: 8, wantRaw: []byte{0, 0, 0, 0, 0, 0, 0x0A, 0xC0}},
		{name: "float64 fraction", vt: TypeFloat64, in: "0.1", wantSize: 8},
		{name: "float64 exponent", vt: TypeFloat64, in: "-2.5e-7", out: "-2.5e-07", wantSize: 8},
		{name: "bytes packed hex", vt: TypeBytes, in: "FF00AB", out: "ff00ab", wantSize: 3, wantRaw: []byte{0xFF, 0x00, 0xAB}},
		{name: "bytes spaced hex", vt: TypeBytes, in: "FF 00 AB", out: "ff00ab", wantSize: 3, wantRaw: []byte{0xFF, 0x00, 0xAB}},
		{name: "bytes single", vt: TypeBytes, in: "00", out: "00", wantSize: 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := c.out
			if want == "" {
				want = c.in
			}
			b, err := ParseValue(c.in, c.vt)
			if err != nil {
				t.Fatalf("ParseValue(%q, %v): %v", c.in, c.vt, err)
			}
			if len(b) != c.wantSize {
				t.Errorf("len(ParseValue(%q, %v)) = %d, want %d", c.in, c.vt, len(b), c.wantSize)
			}
			if c.wantRaw != nil && !bytes.Equal(b, c.wantRaw) {
				t.Errorf("ParseValue(%q, %v) = % x, want % x", c.in, c.vt, b, c.wantRaw)
			}
			if got := FormatValue(b, c.vt); got != want {
				t.Errorf("round-trip %v %q: got %q want %q", c.vt, c.in, got, want)
			}
		})
	}
}

func TestParseValueErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		vt   ValueType
	}{
		{"int32 not a number", "abc", TypeInt32},
		{"int32 overflow", "2147483648", TypeInt32},
		{"int32 empty", "", TypeInt32},
		{"int64 overflow", "9223372036854775808", TypeInt64},
		{"uint32 negative", "-1", TypeUint32},
		{"uint64 negative", "-1", TypeUint64},
		{"float32 not a number", "x", TypeFloat32},
		{"float64 not a number", "x", TypeFloat64},
		{"bytes odd length", "F0F", TypeBytes},
		{"bytes non-hex", "ZZ", TypeBytes},
		{"bytes empty", "", TypeBytes},
		{"bytes spaces only", "   ", TypeBytes},
		{"unknown value type", "1", ValueType(99)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseValue(c.in, c.vt)
			if err == nil {
				t.Fatalf("ParseValue(%q, %v) = % x, nil; want an error", c.in, c.vt, got)
			}
			if got != nil {
				t.Errorf("ParseValue(%q, %v) = % x on error, want nil", c.in, c.vt, got)
			}
		})
	}
}

func TestFormatValueTruncatedAndUnknown(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		vt   ValueType
		want string
	}{
		{"int32 short read", []byte{1, 2, 3}, TypeInt32, "?"},
		{"int64 short read", []byte{1, 2, 3, 4}, TypeInt64, "?"},
		{"float64 short read", nil, TypeFloat64, "?"},
		{"unknown type", []byte{1, 2, 3, 4}, ValueType(99), "?"},
		{"bytes never truncates", nil, TypeBytes, ""},
		{"bytes hex lowercases", []byte{0xAB, 0xCD}, TypeBytes, "abcd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatValue(c.b, c.vt); got != c.want {
				t.Errorf("FormatValue(% x, %v) = %q, want %q", c.b, c.vt, got, c.want)
			}
		})
	}
}

func TestValueTypeNamesRoundTrip(t *testing.T) {
	names := ValueTypeNames()
	want := []string{"int32", "int64", "float32", "float64", "uint32", "uint64", "bytes"}
	if len(names) != len(want) {
		t.Fatalf("ValueTypeNames() = %v, want %v", names, want)
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("ValueTypeNames()[%d] = %q, want %q", i, n, want[i])
		}
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			vt, err := ParseValueType(name)
			if err != nil {
				t.Fatalf("ParseValueType(%q): %v", name, err)
			}
			if got := vt.String(); got != name {
				t.Errorf("round-trip %q -> %d -> %q", name, vt, got)
			}
		})
	}

	// ValueTypeNames must hand out a fresh slice each call.
	names[0] = "clobbered"
	if again := ValueTypeNames(); again[0] != "int32" {
		t.Errorf("ValueTypeNames() shares backing storage: got %q after mutation", again[0])
	}

	if got, want := ValueType(99).String(), "unknown"; got != want {
		t.Errorf("ValueType(99).String() = %q, want %q", got, want)
	}
	for _, bad := range []string{"", "nope", "Int32", "int 32"} {
		if _, err := ParseValueType(bad); err == nil {
			t.Errorf("ParseValueType(%q) = nil error, want an error", bad)
		} else if !strings.Contains(err.Error(), "unknown value type") {
			t.Errorf("ParseValueType(%q) error = %q, want it to mention the unknown type", bad, err)
		}
	}
}

func TestValueTypeSizeAndIsFixedSize(t *testing.T) {
	cases := []struct {
		vt        ValueType
		size      int
		fixedSize bool
	}{
		{TypeInt32, 4, true},
		{TypeInt64, 8, true},
		{TypeFloat32, 4, true},
		{TypeFloat64, 8, true},
		{TypeUint32, 4, true},
		{TypeUint64, 8, true},
		{TypeBytes, 0, false},
		{ValueType(99), 0, false},
	}
	for _, c := range cases {
		t.Run(c.vt.String(), func(t *testing.T) {
			if got := c.vt.Size(); got != c.size {
				t.Errorf("Size() = %d, want %d", got, c.size)
			}
			if got := c.vt.IsFixedSize(); got != c.fixedSize {
				t.Errorf("IsFixedSize() = %v, want %v", got, c.fixedSize)
			}
		})
	}
}

func TestFilterModeNamesRoundTrip(t *testing.T) {
	for _, name := range []string{"changed", "unchanged", "increased", "decreased", "value"} {
		t.Run(name, func(t *testing.T) {
			m, err := ParseFilterMode(name)
			if err != nil {
				t.Fatalf("ParseFilterMode(%q): %v", name, err)
			}
			if got := m.String(); got != name {
				t.Errorf("round-trip %q -> %d -> %q", name, m, got)
			}
			if !m.valid() {
				t.Errorf("%q parsed to an invalid mode %d", name, m)
			}
		})
	}

	if got, want := FilterMode(99).String(), "unknown"; got != want {
		t.Errorf("FilterMode(99).String() = %q, want %q", got, want)
	}
	if FilterMode(99).valid() || FilterMode(-1).valid() {
		t.Errorf("out-of-range FilterMode reported as valid")
	}
	for _, bad := range []string{"", "nope", "Changed"} {
		if _, err := ParseFilterMode(bad); err == nil {
			t.Errorf("ParseFilterMode(%q) = nil error, want an error", bad)
		}
	}
}

func TestCompareValuesAllTypes(t *testing.T) {
	cases := []struct {
		name string
		vt   ValueType
		lo   string
		hi   string
	}{
		{"int32", TypeInt32, "-2147483648", "2147483647"},
		{"int64", TypeInt64, "-9223372036854775808", "9223372036854775807"},
		{"float32", TypeFloat32, "-1.5", "0.25"},
		{"float64", TypeFloat64, "-1e300", "1e300"},
		{"uint32", TypeUint32, "0", "4294967295"},
		{"uint64", TypeUint64, "0", "18446744073709551615"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lo, err := ParseValue(c.lo, c.vt)
			if err != nil {
				t.Fatalf("ParseValue(%q, %v): %v", c.lo, c.vt, err)
			}
			hi, err := ParseValue(c.hi, c.vt)
			if err != nil {
				t.Fatalf("ParseValue(%q, %v): %v", c.hi, c.vt, err)
			}
			if got := CompareValues(lo, hi, c.vt); got != -1 {
				t.Errorf("CompareValues(%s, %s, %v) = %d, want -1", c.lo, c.hi, c.vt, got)
			}
			if got := CompareValues(hi, lo, c.vt); got != 1 {
				t.Errorf("CompareValues(%s, %s, %v) = %d, want 1", c.hi, c.lo, c.vt, got)
			}
			if got := CompareValues(lo, lo, c.vt); got != 0 {
				t.Errorf("CompareValues(%s, %s, %v) = %d, want 0", c.lo, c.lo, c.vt, got)
			}
		})
	}

	t.Run("unknown type", func(t *testing.T) {
		a := []byte{1, 2, 3, 4, 5, 6, 7, 8}
		b := []byte{8, 7, 6, 5, 4, 3, 2, 1}
		if got := CompareValues(a, b, ValueType(99)); got != 0 {
			t.Errorf("CompareValues with an unknown type = %d, want 0", got)
		}
	})
}

func TestEqualBytes(t *testing.T) {
	cases := []struct {
		name string
		a, b []byte
		want bool
	}{
		{"identical", []byte{1, 2}, []byte{1, 2}, true},
		{"different", []byte{1, 2}, []byte{1, 3}, false},
		{"different lengths", []byte{1, 2}, []byte{1, 2, 3}, false},
		{"both empty", nil, []byte{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EqualBytes(c.a, c.b); got != c.want {
				t.Errorf("EqualBytes(% x, % x) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestParseHexBytes(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []byte
		wantErr string
	}{
		{name: "packed", in: "FF00AB", want: []byte{0xFF, 0x00, 0xAB}},
		{name: "spaced", in: "FF 00 AB", want: []byte{0xFF, 0x00, 0xAB}},
		{name: "lowercase", in: "ff00ab", want: []byte{0xFF, 0x00, 0xAB}},
		{name: "leading and trailing spaces", in: " FF 00 ", want: []byte{0xFF, 0x00}},
		{name: "empty", in: "", wantErr: "empty byte sequence"},
		{name: "spaces only", in: "  ", wantErr: "empty byte sequence"},
		{name: "odd digit count", in: "FFA", wantErr: "invalid hex bytes"},
		{name: "non-hex", in: "GG", wantErr: "invalid hex bytes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseHexBytes(c.in)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("parseHexBytes(%q) = % x, nil; want error containing %q", c.in, got, c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("parseHexBytes(%q) error = %q, want it to contain %q", c.in, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHexBytes(%q): %v", c.in, err)
			}
			if !bytes.Equal(got, c.want) {
				t.Errorf("parseHexBytes(%q) = % x, want % x", c.in, got, c.want)
			}
		})
	}
}

package search

import "testing"

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

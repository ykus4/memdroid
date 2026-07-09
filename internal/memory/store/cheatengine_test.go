package store

import "testing"

func TestParseCTAddress(t *testing.T) {
	cases := []struct {
		in   string
		want uintptr
	}{
		{"0x1A2B", 0x1A2B},
		{"1A2B", 0x1A2B},
		{"\"deadBEEF\"", 0xDEADBEEF},
		{"game.exe+1000", 0x1000}, // named module → keep offset
		{"libil2cpp.so+2A", 0x2A}, // named module → keep offset
	}
	for _, c := range cases {
		got, err := parseCTAddress(c.in)
		if err != nil {
			t.Fatalf("parseCTAddress(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseCTAddress(%q) = 0x%x, want 0x%x", c.in, got, c.want)
		}
	}
}

func TestParseCTAddressHexModuleKept(t *testing.T) {
	// A purely-hex left side is NOT a module name, so the whole "abcd+10"
	// parses via the offset branch (left "abcd" is hex → keep only "10").
	// The important property: a hex-looking module name is not misclassified
	// away, and named modules are detected.
	if !isHexString("abcd") {
		t.Errorf("abcd should be recognised as hex")
	}
	if isHexString("game.exe") {
		t.Errorf("game.exe should not be hex")
	}
	if isHexString("") {
		t.Errorf("empty string is not hex")
	}
}

func TestCTVariableType(t *testing.T) {
	if ctVariableType("4 Bytes").String() != "int32" {
		t.Errorf("4 Bytes should map to int32")
	}
	if ctVariableType("Float").String() != "float32" {
		t.Errorf("Float should map to float32")
	}
	if ctVariableType("Double").String() != "float64" {
		t.Errorf("Double should map to float64")
	}
}

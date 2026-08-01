package search

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"memdroid/internal/driver/drivertest"
)

func TestParseStringEncoding(t *testing.T) {
	cases := []struct {
		in      string
		want    StringEncoding
		wantErr bool
	}{
		{in: "", want: EncodingUTF8},
		{in: "utf8", want: EncodingUTF8},
		{in: "utf-8", want: EncodingUTF8},
		{in: "utf16", want: EncodingUTF16LE},
		{in: "utf-16", want: EncodingUTF16LE},
		{in: "utf16le", want: EncodingUTF16LE},
		{in: "utf-16le", want: EncodingUTF16LE},
		{in: "UTF8", wantErr: true},
		{in: "utf-16be", wantErr: true},
		{in: "ascii", wantErr: true},
		{in: " utf8", wantErr: true},
	}

	for _, c := range cases {
		t.Run("in="+c.in, func(t *testing.T) {
			got, err := ParseStringEncoding(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseStringEncoding(%q) = %v, nil; want an error", c.in, got)
				}
				if !strings.Contains(err.Error(), "unknown string encoding") {
					t.Errorf("error = %q, want it to mention the unknown encoding", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseStringEncoding(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("ParseStringEncoding(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestStringEncodingString(t *testing.T) {
	cases := []struct {
		enc  StringEncoding
		want string
	}{
		{EncodingUTF8, "utf8"},
		{EncodingUTF16LE, "utf16le"},
		{StringEncoding(99), "utf8"}, // anything not UTF-16LE reads back as utf8
	}
	for _, c := range cases {
		if got := c.enc.String(); got != c.want {
			t.Errorf("StringEncoding(%d).String() = %q, want %q", c.enc, got, c.want)
		}
	}
	// Every accepted spelling of a canonical name must round-trip.
	for _, name := range []string{"utf8", "utf16le"} {
		enc, err := ParseStringEncoding(name)
		if err != nil {
			t.Fatalf("ParseStringEncoding(%q): %v", name, err)
		}
		if got := enc.String(); got != name {
			t.Errorf("round-trip %q -> %v -> %q", name, enc, got)
		}
	}
}

func TestStringBytes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		enc  StringEncoding
		want []byte
	}{
		{"ascii utf8", "Hi", EncodingUTF8, []byte{'H', 'i'}},
		{"ascii utf16le", "Hi", EncodingUTF16LE, []byte{'H', 0x00, 'i', 0x00}},
		{"empty utf8", "", EncodingUTF8, []byte{}},
		{"empty utf16le", "", EncodingUTF16LE, []byte{}},
		{"multibyte utf8", "あ", EncodingUTF8, []byte{0xE3, 0x81, 0x82}},
		{"multibyte utf16le", "あ", EncodingUTF16LE, []byte{0x42, 0x30}},
		{
			// U+1D11E needs a surrogate pair: D834 DD1E, each stored little-endian.
			name: "astral plane utf16le surrogate pair",
			in:   "\U0001D11E",
			enc:  EncodingUTF16LE,
			want: []byte{0x34, 0xD8, 0x1E, 0xDD},
		},
		{"mixed utf16le", "Aé", EncodingUTF16LE, []byte{0x41, 0x00, 0xE9, 0x00}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := StringBytes(c.in, c.enc)
			if !bytes.Equal(got, c.want) {
				t.Errorf("StringBytes(%q, %v) = % x, want % x", c.in, c.enc, got, c.want)
			}
		})
	}
}

func TestSearchString(t *testing.T) {
	const base = 0x1000
	utf8Payload := StringBytes("SCORE", EncodingUTF8)
	utf16Payload := StringBytes("SCORE", EncodingUTF16LE)

	// Both encodings live in the same region so each search must pick exactly one.
	data := regionData(64, map[int][]byte{
		4:  utf8Payload,
		16: utf16Payload,
		40: utf8Payload,
	})

	cases := []struct {
		name      string
		needle    string
		enc       StringEncoding
		wantAddrs []uintptr
		wantVal   []byte
	}{
		{
			name:      "utf8",
			needle:    "SCORE",
			enc:       EncodingUTF8,
			wantAddrs: []uintptr{base + 4, base + 40},
			wantVal:   utf8Payload,
		},
		{
			name:      "utf16le",
			needle:    "SCORE",
			enc:       EncodingUTF16LE,
			wantAddrs: []uintptr{base + 16},
			wantVal:   utf16Payload,
		},
		{
			name:      "substring",
			needle:    "COR",
			enc:       EncodingUTF8,
			wantAddrs: []uintptr{base + 5, base + 41},
			wantVal:   []byte("COR"),
		},
		{
			name:      "absent",
			needle:    "MANA",
			enc:       EncodingUTF8,
			wantAddrs: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := drivertest.New(drivertest.Region{Start: base, Name: "[heap]", Data: bytes.Clone(data)})

			res, err := SearchString(f, 1, c.needle, c.enc)
			if err != nil {
				t.Fatalf("SearchString(%q, %v): %v", c.needle, c.enc, err)
			}
			if got := res.Addrs(); !slices.Equal(got, c.wantAddrs) {
				t.Fatalf("Addrs() = %#x, want %#x", got, c.wantAddrs)
			}
			for _, m := range res.Matches {
				if !bytes.Equal(m.Value, c.wantVal) {
					t.Errorf("Matches value at 0x%x = % x, want % x", m.Addr, m.Value, c.wantVal)
				}
			}
		})
	}
}

func TestSearchStringEmpty(t *testing.T) {
	f := drivertest.New(drivertest.Region{Start: 0x1000, Name: "[heap]", Data: regionData(16, nil)})
	for _, enc := range []StringEncoding{EncodingUTF8, EncodingUTF16LE} {
		if _, err := SearchString(f, 1, "", enc); err == nil {
			t.Errorf("SearchString(%q, %v) = nil error, want %q", "", enc, "empty string")
		} else if !strings.Contains(err.Error(), "empty string") {
			t.Errorf("error = %q, want it to contain %q", err, "empty string")
		}
	}
}

func TestLiteralPattern(t *testing.T) {
	got := literalPattern([]byte{0x00, 0xFF})
	want := Pattern{{Value: 0x00}, {Value: 0xFF}}
	if !slices.Equal(got, want) {
		t.Errorf("literalPattern = %v, want %v", got, want)
	}
	if got := literalPattern(nil); len(got) != 0 {
		t.Errorf("literalPattern(nil) = %v, want empty", got)
	}
}

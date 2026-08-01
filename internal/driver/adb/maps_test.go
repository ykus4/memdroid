package adb

import (
	"bufio"
	"strings"
	"testing"

	"memdroid/internal/driver"
)

// These tests exercise the pure parsing half of the adb driver only. Nothing
// here shells out to a real adb binary or touches a device.

func TestParseMapsLine(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantOK    bool
		wantStart uintptr
		wantEnd   uintptr
		wantName  string
	}{
		{
			name:      "rw-p with name",
			line:      "7f8a1b2000-7f8a1b3000 rw-p 00000000 00:00 0                          [heap]",
			wantOK:    true,
			wantStart: 0x7f8a1b2000,
			wantEnd:   0x7f8a1b3000,
			wantName:  "[heap]",
		},
		{
			name:      "rw-p anonymous (no name field)",
			line:      "12c00000-12d00000 rw-p 00000000 00:00 0",
			wantOK:    true,
			wantStart: 0x12c00000,
			wantEnd:   0x12d00000,
			wantName:  "",
		},
		{
			name:      "rw-s shared mapping is writable",
			line:      "7000000000-7000001000 rw-s 00000000 00:10 12345 /dev/ashmem/dalvik",
			wantOK:    true,
			wantStart: 0x7000000000,
			wantEnd:   0x7000001000,
			wantName:  "/dev/ashmem/dalvik",
		},
		{
			name:   "read-only r--p rejected",
			line:   "7f8a1b0000-7f8a1b2000 r--p 00000000 fd:00 1234 /system/lib64/libc.so",
			wantOK: false,
		},
		{
			name:   "executable r-xp rejected (perms[1] != 'w')",
			line:   "7f8a000000-7f8a1b0000 r-xp 00000000 fd:00 1234 /system/lib64/libc.so",
			wantOK: false,
		},
		{
			name:   "no read permission rejected",
			line:   "7f8a1b2000-7f8a1b3000 ---p 00000000 00:00 0",
			wantOK: false,
		},
		{
			name:   "write-only w--p rejected (perms[0] != 'r')",
			line:   "7f8a1b2000-7f8a1b3000 -w-p 00000000 00:00 0",
			wantOK: false,
		},
		{
			name:   "perms field too short",
			line:   "7f8a1b2000-7f8a1b3000 r 00000000 00:00 0",
			wantOK: false,
		},
		{
			name:   "fewer than five fields",
			line:   "7f8a1b2000-7f8a1b3000 rw-p 00000000 00:00",
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
		{
			name:   "whitespace only",
			line:   "   \t  ",
			wantOK: false,
		},
		{
			name:   "garbage address field",
			line:   "notanaddress rw-p 00000000 00:00 0 [heap]",
			wantOK: false,
		},
		{
			name:   "address without range separator",
			line:   "7f8a1b2000 rw-p 00000000 00:00 0 [heap]",
			wantOK: false,
		},
		{
			name:      "pathname with a space is kept whole",
			line:      "aaaa-bbbb rw-p 00000000 00:00 0 /data/app/base.apk (deleted)",
			wantOK:    true,
			wantStart: 0xaaaa,
			wantEnd:   0xbbbb,
			wantName:  "/data/app/base.apk (deleted)",
		},
		{
			name:      "uppercase hex addresses",
			line:      "7FFF0000-7FFF1000 rw-p 00000000 00:00 0 [stack]",
			wantOK:    true,
			wantStart: 0x7FFF0000,
			wantEnd:   0x7FFF1000,
			wantName:  "[stack]",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseMapsLine(c.line)
			if ok != c.wantOK {
				t.Fatalf("parseMapsLine(%q) ok = %v, want %v (region %+v)", c.line, ok, c.wantOK, got)
			}
			if !c.wantOK {
				if got != (driver.Region{}) {
					t.Errorf("rejected line returned non-zero region %+v", got)
				}
				return
			}
			if got.Start != c.wantStart {
				t.Errorf("Start = 0x%x, want 0x%x", got.Start, c.wantStart)
			}
			if got.End != c.wantEnd {
				t.Errorf("End = 0x%x, want 0x%x", got.End, c.wantEnd)
			}
			if got.Name != c.wantName {
				t.Errorf("Name = %q, want %q", got.Name, c.wantName)
			}
		})
	}
}

// mapsText is a trimmed but realistic /proc/<pid>/maps excerpt.
const mapsText = `12c00000-12d00000 rw-p 00000000 00:00 0                                  [anon:dalvik-main space]
6f9f4000-6fa00000 r--p 00000000 fd:00 1024                               /system/framework/boot.art
7f8a000000-7f8a1b0000 r-xp 00000000 fd:00 1234                           /system/lib64/libc.so
7f8a1b0000-7f8a1b2000 r--p 001b0000 fd:00 1234                           /system/lib64/libc.so
7f8a1b2000-7f8a1b3000 rw-p 001b2000 fd:00 1234                           /system/lib64/libc.so
7fabc00000-7fabc21000 rw-p 00000000 00:00 0                              [heap]
7fdd000000-7fdd001000 rw-p 00000000 00:00 0
7ffc12345000-7ffc12366000 rw-p 00000000 00:00 0                          [stack]
ffffffffff600000-ffffffffff601000 --xp 00000000 00:00 0                  [vsyscall]
`

// scanMaps mirrors the loop in ReadMapsFiltered without any adb involvement.
func scanMaps(t *testing.T, text string, filter driver.RegionFilter, customStart, customEnd uintptr) []driver.Region {
	t.Helper()
	var regions []driver.Region
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		if r, ok := parseMapsLine(sc.Text()); ok && driver.MatchFilter(r, filter, customStart, customEnd) {
			regions = append(regions, r)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return regions
}

func TestParseMapsTextKeepsOnlyWritableRegions(t *testing.T) {
	got := scanMaps(t, mapsText, driver.RegionAll, 0, 0)

	want := []driver.Region{
		{Start: 0x12c00000, End: 0x12d00000, Name: "[anon:dalvik-main space]"},
		{Start: 0x7f8a1b2000, End: 0x7f8a1b3000, Name: "/system/lib64/libc.so"},
		{Start: 0x7fabc00000, End: 0x7fabc21000, Name: "[heap]"},
		{Start: 0x7fdd000000, End: 0x7fdd001000, Name: ""},
		{Start: 0x7ffc12345000, End: 0x7ffc12366000, Name: "[stack]"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d regions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("region %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseMapsTextWithFilters(t *testing.T) {
	cases := []struct {
		name        string
		filter      driver.RegionFilter
		customStart uintptr
		customEnd   uintptr
		wantStarts  []uintptr
	}{
		{"heap", driver.RegionHeap, 0, 0, []uintptr{0x7fabc00000}},
		{"stack", driver.RegionStack, 0, 0, []uintptr{0x7ffc12345000}},
		{"anon", driver.RegionAnon, 0, 0, []uintptr{0x7fdd000000}},
		{"custom range", driver.RegionCustom, 0x7f8a000000, 0x7fac000000, []uintptr{0x7f8a1b2000, 0x7fabc00000}},
		{"custom range matching nothing", driver.RegionCustom, 0x1, 0x2, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scanMaps(t, mapsText, c.filter, c.customStart, c.customEnd)
			if len(got) != len(c.wantStarts) {
				t.Fatalf("got %d regions, want %d: %+v", len(got), len(c.wantStarts), got)
			}
			for i, want := range c.wantStarts {
				if got[i].Start != want {
					t.Errorf("region %d start = 0x%x, want 0x%x", i, got[i].Start, want)
				}
			}
		})
	}
}

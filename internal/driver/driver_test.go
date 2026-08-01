package driver

import "testing"

func TestMatchFilter(t *testing.T) {
	heap := Region{Start: 0x1000, End: 0x2000, Name: "[heap]"}
	stack := Region{Start: 0x2000, End: 0x3000, Name: "[stack]"}
	anon := Region{Start: 0x3000, End: 0x4000, Name: ""}
	lib := Region{Start: 0x4000, End: 0x5000, Name: "/system/lib64/libc.so"}

	cases := []struct {
		name        string
		region      Region
		filter      RegionFilter
		customStart uintptr
		customEnd   uintptr
		want        bool
	}{
		{"all accepts heap", heap, RegionAll, 0, 0, true},
		{"all accepts anon", anon, RegionAll, 0, 0, true},
		{"all accepts named lib", lib, RegionAll, 0, 0, true},
		{"all ignores custom bounds", lib, RegionAll, 0x9000, 0x9001, true},

		{"heap accepts [heap]", heap, RegionHeap, 0, 0, true},
		{"heap rejects [stack]", stack, RegionHeap, 0, 0, false},
		{"heap rejects anon", anon, RegionHeap, 0, 0, false},

		{"stack accepts [stack]", stack, RegionStack, 0, 0, true},
		{"stack rejects [heap]", heap, RegionStack, 0, 0, false},

		{"anon accepts unnamed", anon, RegionAnon, 0, 0, true},
		{"anon rejects [heap]", heap, RegionAnon, 0, 0, false},
		{"anon rejects named lib", lib, RegionAnon, 0, 0, false},

		// RegionCustom keeps regions fully contained in [customStart, customEnd].
		{"custom strictly inside", heap, RegionCustom, 0x0, 0xFFFF, true},
		{"custom exact bounds are inclusive", heap, RegionCustom, 0x1000, 0x2000, true},
		{"custom start one byte too high", heap, RegionCustom, 0x1001, 0x2000, false},
		{"custom end one byte too low", heap, RegionCustom, 0x1000, 0x1FFF, false},
		{"custom region entirely below range", heap, RegionCustom, 0x5000, 0x6000, false},
		{"custom region entirely above range", lib, RegionCustom, 0x0, 0x100, false},
		{"custom partial overlap at the start", heap, RegionCustom, 0x1800, 0x2800, false},
		{"custom partial overlap at the end", heap, RegionCustom, 0x0800, 0x1800, false},
		{"custom empty range rejects everything", heap, RegionCustom, 0, 0, false},
		{"custom inverted range rejects", heap, RegionCustom, 0x3000, 0x1000, false},
		{"custom zero-size region at start bound", Region{Start: 0x1000, End: 0x1000}, RegionCustom, 0x1000, 0x1000, true},

		{"unknown filter value falls through to accept-all", lib, RegionFilter(99), 0, 0, true},
		{"negative filter value falls through to accept-all", lib, RegionFilter(-1), 0, 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MatchFilter(c.region, c.filter, c.customStart, c.customEnd)
			if got != c.want {
				t.Errorf("MatchFilter(%+v, %d, 0x%x, 0x%x) = %v, want %v",
					c.region, c.filter, c.customStart, c.customEnd, got, c.want)
			}
		})
	}
}

func TestRegionFilterConstantsAreDistinct(t *testing.T) {
	all := []RegionFilter{RegionAll, RegionHeap, RegionStack, RegionAnon, RegionCustom}
	seen := make(map[RegionFilter]bool, len(all))
	for _, f := range all {
		if seen[f] {
			t.Fatalf("duplicate RegionFilter value %d", f)
		}
		seen[f] = true
	}
	if RegionAll != 0 {
		t.Errorf("RegionAll = %d, want 0 so the zero value means unfiltered", RegionAll)
	}
}

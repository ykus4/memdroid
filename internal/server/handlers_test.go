package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- status ------------------------------------------------------------------

func TestStatusDetached(t *testing.T) {
	e := newEnv(t, Config{})

	got := e.obj(http.MethodGet, "/api/status", "", http.StatusOK)

	if pid := num(t, got, "pid"); pid != 0 {
		t.Errorf("pid = %v, want 0", pid)
	}
	if boolean(t, got, "attached") {
		t.Error("attached = true, want false")
	}
	if vt := str(t, got, "value_type"); vt != "int32" {
		t.Errorf("value_type = %q, want %q", vt, "int32")
	}
	if c := num(t, got, "candidates"); c != 0 {
		t.Errorf("candidates = %v, want 0", c)
	}
	if d := num(t, got, "undo_depth"); d != 0 {
		t.Errorf("undo_depth = %v, want 0", d)
	}
	for _, key := range []string{"frozen", "watched", "alerts"} {
		if l := slice(t, got, key); len(l) != 0 {
			t.Errorf("%s = %v, want empty", key, l)
		}
	}
	if dev := str(t, got, "device"); dev != "" {
		t.Errorf("device = %q, want empty", dev)
	}
}

func TestStatusAttached(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	if !e.fake.Attached(1) {
		t.Fatal("driver was not asked to attach")
	}

	got := e.obj(http.MethodGet, "/api/status", "", http.StatusOK)
	if pid := num(t, got, "pid"); pid != 1 {
		t.Errorf("pid = %v, want 1", pid)
	}
	if !boolean(t, got, "attached") {
		t.Error("attached = false, want true")
	}
	if c := num(t, got, "candidates"); c != 0 {
		t.Errorf("candidates = %v, want 0", c)
	}
}

// TestNotAttached checks the guard on every endpoint that needs a live process.
func TestNotAttached(t *testing.T) {
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/maps", ""},
		{http.MethodGet, "/api/memory/hexdump?addr=0x1000&size=16", ""},
		{http.MethodPost, "/api/search/value", `{"value":"1337"}`},
		{http.MethodPost, "/api/search/pattern", `{"pattern":"39 05"}`},
		{http.MethodPost, "/api/search/string", `{"value":"hi"}`},
		{http.MethodPost, "/api/memory/modify", `{"addr":"0x1000","value":"1"}`},
		{http.MethodPost, "/api/memory/write-string", `{"addr":"0x1000","value":"hi"}`},
		{http.MethodPost, "/api/memory/freeze", `{"addr":"0x1000","value":"1"}`},
		{http.MethodPost, "/api/memory/dump", `{"addr":"0x1000","size":16}`},
		{http.MethodPost, "/api/snapshot/take", `{"addr":"0x1000","size":16}`},
		{http.MethodPost, "/api/snapshot/diff", `{"addr":"0x1000","size":16}`},
		{http.MethodPost, "/api/watch/add", `{"addr":"0x1000"}`},
		{http.MethodPost, "/api/alert/add", `{"addr":"0x1000","condition":"changed","action":"notify"}`},
		{http.MethodPost, "/api/pointer/scan", `{"addr":"0x1000"}`},
		{http.MethodPost, "/api/pointer/resolve", `{"label":"[heap]","offsets":[0]}`},
		{http.MethodPost, "/api/bookmark/modify-all", `{"value":"1"}`},
		{http.MethodPost, "/api/process/stop", ""},
		{http.MethodPost, "/api/process/continue", ""},
		{http.MethodPost, "/api/process/detach", ""},
	}

	e := newEnv(t, Config{})
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if msg := e.errMsg(tc.method, tc.path, tc.body, http.StatusBadRequest); msg != "not attached" {
				t.Errorf("error = %q, want %q", msg, "not attached")
			}
		})
	}
}

// --- search / modify / undo happy path ---------------------------------------

func TestSearchFilterModifyUndoFlow(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	// 1. Initial scan finds the three planted int32 values.
	got := e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK)
	if c := num(t, got, "candidates"); c != 3 {
		t.Fatalf("candidates = %v, want 3", c)
	}

	// 2. Paging returns addresses in ascending order across pages.
	page0 := e.obj(http.MethodGet, "/api/search/candidates?page_size=2&page=0", "", http.StatusOK)
	if total := num(t, page0, "total"); total != 3 {
		t.Errorf("page 0 total = %v, want 3", total)
	}
	if ps := num(t, page0, "page_size"); ps != 2 {
		t.Errorf("page 0 page_size = %v, want 2", ps)
	}
	items0 := slice(t, page0, "items")
	if len(items0) != 2 {
		t.Fatalf("page 0 items = %d, want 2", len(items0))
	}
	assertCandidate(t, items0[0], "0x1010", "1337")
	assertCandidate(t, items0[1], "0x1020", "1337")

	page1 := e.obj(http.MethodGet, "/api/search/candidates?page_size=2&page=1", "", http.StatusOK)
	items1 := slice(t, page1, "items")
	if len(items1) != 1 {
		t.Fatalf("page 1 items = %d, want 1", len(items1))
	}
	assertCandidate(t, items1[0], "0x1030", "1337")

	// A page past the end is empty but still reports the total.
	page9 := e.obj(http.MethodGet, "/api/search/candidates?page_size=2&page=9", "", http.StatusOK)
	if total := num(t, page9, "total"); total != 3 {
		t.Errorf("page 9 total = %v, want 3", total)
	}
	if items := page9["items"]; items != nil {
		if l, ok := items.([]any); ok && len(l) != 0 {
			t.Errorf("page 9 items = %v, want empty", l)
		}
	}

	// 3. Nothing has moved, so an "unchanged" filter keeps all three.
	got = e.obj(http.MethodPost, "/api/search/filter", `{"mode":"unchanged"}`, http.StatusOK)
	if c := num(t, got, "candidates"); c != 3 {
		t.Fatalf("candidates after unchanged filter = %v, want 3", c)
	}

	// 4. Modify one of them and confirm the fake process actually changed.
	got = e.obj(http.MethodPost, "/api/memory/modify", `{"addr":"0x1010","value":"4242"}`, http.StatusOK)
	if !boolean(t, got, "ok") {
		t.Error("ok = false")
	}
	if addr := str(t, got, "addr"); addr != "0x1010" {
		t.Errorf("addr = %q, want %q", addr, "0x1010")
	}
	if d := num(t, got, "undo_depth"); d != 1 {
		t.Errorf("undo_depth = %v, want 1", d)
	}
	if v := e.heapInt32(0x1010); v != 4242 {
		t.Fatalf("memory at 0x1010 = %d, want 4242", v)
	}
	if v := e.heapInt32(0x1020); v != 1337 {
		t.Errorf("memory at 0x1020 = %d, want it untouched at 1337", v)
	}

	// 5. A "changed" filter now narrows to exactly the modified address.
	got = e.obj(http.MethodPost, "/api/search/filter", `{"mode":"changed"}`, http.StatusOK)
	if c := num(t, got, "candidates"); c != 1 {
		t.Fatalf("candidates after changed filter = %v, want 1", c)
	}
	items := slice(t, e.obj(http.MethodGet, "/api/search/candidates", "", http.StatusOK), "items")
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	assertCandidate(t, items[0], "0x1010", "4242")

	// 6. Undo restores the original bytes and empties the stack.
	got = e.obj(http.MethodPost, "/api/memory/undo", "", http.StatusOK)
	if d := num(t, got, "undo_depth"); d != 0 {
		t.Errorf("undo_depth = %v, want 0", d)
	}
	if v := e.heapInt32(0x1010); v != 1337 {
		t.Fatalf("memory at 0x1010 after undo = %d, want 1337", v)
	}

	// Undoing an empty stack is a client error, not a crash.
	if msg := e.errMsg(http.MethodPost, "/api/memory/undo", "", http.StatusBadRequest); msg == "" {
		t.Error("undo on an empty stack returned no error message")
	}
}

// TestSearchValueRetypesAndRescans is a regression test.
//
// Naming a type in a /api/search/value request must switch the session's type
// *before* the scan runs. A previous version kept the session's original type,
// so a float32 request after an int32 search silently scanned and formatted at
// the stale type.
func TestSearchValueRetypesAndRescans(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	// Establish a session at the default int32 type.
	got := e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK)
	if c := num(t, got, "candidates"); c != 3 {
		t.Fatalf("int32 candidates = %v, want 3", c)
	}
	status := e.obj(http.MethodGet, "/api/status", "", http.StatusOK)
	if vt := str(t, status, "value_type"); vt != "int32" {
		t.Fatalf("value_type = %q, want %q", vt, "int32")
	}

	// Now search at a different type in the same request.
	got = e.obj(http.MethodPost, "/api/search/value", `{"value":"2.5","type":"float32"}`, http.StatusOK)
	if c := num(t, got, "candidates"); c != 2 {
		t.Fatalf("float32 candidates = %v, want 2 (a stale int32 session would not re-scan)", c)
	}

	// The switch is visible in the status, and the candidate count proves the
	// scan re-ran rather than reusing the int32 result set.
	status = e.obj(http.MethodGet, "/api/status", "", http.StatusOK)
	if vt := str(t, status, "value_type"); vt != "float32" {
		t.Errorf("value_type = %q, want %q", vt, "float32")
	}
	if c := num(t, status, "candidates"); c != 2 {
		t.Errorf("status candidates = %v, want 2", c)
	}

	// Candidates are the float addresses, formatted with the new type.
	items := slice(t, e.obj(http.MethodGet, "/api/search/candidates", "", http.StatusOK), "items")
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	assertCandidate(t, items[0], "0x1040", "2.5")
	assertCandidate(t, items[1], "0x1050", "2.5")

	// And the int32 addresses are gone.
	for _, it := range items {
		switch addr := str(t, object(t, it), "addr"); addr {
		case "0x1010", "0x1020", "0x1030":
			t.Errorf("stale int32 candidate %s survived the type switch", addr)
		}
	}
}

func TestSearchValueRejectsBadInput(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	tests := []struct {
		name    string
		body    string
		wantSub string
	}{
		{"unknown type", `{"value":"1","type":"int128"}`, `unknown value type "int128"`},
		{"value not parseable as the type", `{"value":"2.5"}`, "invalid value"},
		{"unknown region", `{"value":"1","region":"nowhere"}`, "unknown region"},
		{"custom region without bounds", `{"value":"1","region":"custom"}`, "addr required"},
		{"custom region end below start", `{"value":"1","region":"custom","region_start":"0x2000","region_end":"0x1000"}`, "region end must be greater than start"},
		{"invalid JSON", `{`, "invalid JSON body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.errMsg(http.MethodPost, "/api/search/value", tc.body, http.StatusBadRequest)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantSub)
			}
		})
	}
}

func TestSearchCandidatesWithoutSession(t *testing.T) {
	e := newEnv(t, Config{})

	got := e.obj(http.MethodGet, "/api/search/candidates", "", http.StatusOK)
	if total := num(t, got, "total"); total != 0 {
		t.Errorf("total = %v, want 0", total)
	}
	if ps := num(t, got, "page_size"); ps != defaultPageSize {
		t.Errorf("page_size = %v, want %d", ps, defaultPageSize)
	}
	if items := slice(t, got, "items"); len(items) != 0 {
		t.Errorf("items = %v, want empty", items)
	}
}

func TestSearchFilterWithoutSession(t *testing.T) {
	e := newEnv(t, Config{})
	if msg := e.errMsg(http.MethodPost, "/api/search/filter", `{"mode":"changed"}`, http.StatusBadRequest); msg != "no active search session" {
		t.Errorf("error = %q, want %q", msg, "no active search session")
	}
}

func TestSearchFilterRejectsUnknownMode(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()
	e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK)

	msg := e.errMsg(http.MethodPost, "/api/search/filter", `{"mode":"sideways"}`, http.StatusBadRequest)
	if !strings.Contains(msg, "unknown filter mode") {
		t.Errorf("error = %q, want it to mention the filter mode", msg)
	}
}

func TestSearchReset(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()
	e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK)

	e.obj(http.MethodPost, "/api/search/reset", "", http.StatusOK)

	if c := num(t, e.obj(http.MethodGet, "/api/status", "", http.StatusOK), "candidates"); c != 0 {
		t.Errorf("candidates after reset = %v, want 0", c)
	}
	// Resetting again with no session is still a no-op success.
	e.obj(http.MethodPost, "/api/search/reset", "", http.StatusOK)
}

// --- value types -------------------------------------------------------------

func TestSearchTypes(t *testing.T) {
	e := newEnv(t, Config{})

	got := e.obj(http.MethodGet, "/api/search/types", "", http.StatusOK)
	if cur := str(t, got, "current"); cur != "int32" {
		t.Errorf("current = %q, want %q", cur, "int32")
	}
	types := slice(t, got, "types")
	want := []string{"int32", "int64", "float32", "float64", "uint32", "uint64", "bytes"}
	if len(types) != len(want) {
		t.Fatalf("types = %v, want %d entries", types, len(want))
	}
	for i, w := range want {
		if types[i] != w {
			t.Errorf("types[%d] = %v, want %q", i, types[i], w)
		}
	}
}

func TestSearchSetType(t *testing.T) {
	e := newEnv(t, Config{})

	got := e.obj(http.MethodPost, "/api/search/type", `{"type":"uint64"}`, http.StatusOK)
	if !boolean(t, got, "ok") {
		t.Error("ok = false")
	}
	if vt := str(t, got, "value_type"); vt != "uint64" {
		t.Errorf("value_type = %q, want %q", vt, "uint64")
	}
	if cur := str(t, e.obj(http.MethodGet, "/api/search/types", "", http.StatusOK), "current"); cur != "uint64" {
		t.Errorf("current = %q, want %q", cur, "uint64")
	}
	if vt := str(t, e.obj(http.MethodGet, "/api/status", "", http.StatusOK), "value_type"); vt != "uint64" {
		t.Errorf("status value_type = %q, want %q", vt, "uint64")
	}
}

func TestSearchSetTypeRejectsBadInput(t *testing.T) {
	e := newEnv(t, Config{})

	tests := []struct {
		name    string
		body    string
		wantSub string
	}{
		{"missing type", `{}`, "type required"},
		{"empty type", `{"type":""}`, "type required"},
		{"unknown type", `{"type":"decimal"}`, `unknown value type "decimal"`},
		{"invalid JSON", `not json`, "invalid JSON body"},
		{"empty body", "", "invalid JSON body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.errMsg(http.MethodPost, "/api/search/type", tc.body, http.StatusBadRequest)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantSub)
			}
		})
	}
}

// TestSetTypeDiscardsCandidates covers the other half of the type/session
// contract: candidates recorded at one width must not survive a type change.
func TestSetTypeDiscardsCandidates(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()
	e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK)

	e.obj(http.MethodPost, "/api/search/type", `{"type":"int64"}`, http.StatusOK)

	status := e.obj(http.MethodGet, "/api/status", "", http.StatusOK)
	if vt := str(t, status, "value_type"); vt != "int64" {
		t.Errorf("value_type = %q, want %q", vt, "int64")
	}
	if c := num(t, status, "candidates"); c != 0 {
		t.Errorf("candidates = %v, want 0 after a type change", c)
	}
}

// --- hexdump -----------------------------------------------------------------

func TestMemoryHexdump(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	got := e.obj(http.MethodGet, "/api/memory/hexdump?addr=0x1000&size=32", "", http.StatusOK)
	if addr := str(t, got, "addr"); addr != "0x1000" {
		t.Errorf("addr = %q, want %q", addr, "0x1000")
	}
	if size := num(t, got, "size"); size != 32 {
		t.Errorf("size = %v, want 32", size)
	}

	lines := slice(t, got, "hex_lines")
	if len(lines) != 2 {
		t.Fatalf("hex_lines = %d, want 2", len(lines))
	}

	first := object(t, lines[0])
	if off := num(t, first, "offset"); off != 0 {
		t.Errorf("line 0 offset = %v, want 0", off)
	}
	if addr := str(t, first, "addr"); addr != "0x1000" {
		t.Errorf("line 0 addr = %q, want %q", addr, "0x1000")
	}
	if hex := str(t, first, "hex"); hex != strings.TrimSpace(strings.Repeat("00 ", 16)) {
		t.Errorf("line 0 hex = %q, want 16 zero bytes", hex)
	}
	if ascii := str(t, first, "ascii"); ascii != strings.Repeat(".", 16) {
		t.Errorf("line 0 ascii = %q, want 16 dots", ascii)
	}

	// The second line starts at 0x1010, where the first int32 1337 lives.
	second := object(t, lines[1])
	if off := num(t, second, "offset"); off != 16 {
		t.Errorf("line 1 offset = %v, want 16", off)
	}
	if addr := str(t, second, "addr"); addr != "0x1010" {
		t.Errorf("line 1 addr = %q, want %q", addr, "0x1010")
	}
	if hex := str(t, second, "hex"); !strings.HasPrefix(hex, "39 05 00 00") {
		t.Errorf("line 1 hex = %q, want it to start with the little-endian 1337", hex)
	}
}

func TestMemoryHexdumpBounds(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	tests := []struct {
		name    string
		query   string
		wantSub string
	}{
		{"missing addr", "?size=16", "addr required"},
		{"malformed addr", "?addr=0xzz&size=16", "invalid addr"},
		{"size zero", "?addr=0x1000&size=0", "size must be 1-4096"},
		{"size negative", "?addr=0x1000&size=-1", "size must be 1-4096"},
		{"size absent", "?addr=0x1000", "size must be 1-4096"},
		{"size above the cap", "?addr=0x1000&size=4097", "size must be 1-4096"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.errMsg(http.MethodGet, "/api/memory/hexdump"+tc.query, "", http.StatusBadRequest)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantSub)
			}
		})
	}

	// The upper bound itself is accepted (the region short-reads, which is fine).
	if rec := e.rec(http.MethodGet, "/api/memory/hexdump?addr=0x1000&size=4096", ""); rec.Code != http.StatusOK {
		t.Errorf("size=4096: status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

// --- snapshots ---------------------------------------------------------------

func TestSnapshotTakeAndDiff(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	// A diff with no prior take is a client error.
	msg := e.errMsg(http.MethodPost, "/api/snapshot/diff", `{"addr":"0x1200","size":16}`, http.StatusBadRequest)
	if !strings.Contains(msg, "no snapshot taken") {
		t.Errorf("error = %q, want it to mention the missing snapshot", msg)
	}

	got := e.obj(http.MethodPost, "/api/snapshot/take", `{"addr":"0x1000","size":16}`, http.StatusOK)
	if !boolean(t, got, "ok") {
		t.Error("ok = false")
	}
	if size := num(t, got, "size"); size != 16 {
		t.Errorf("size = %v, want 16", size)
	}

	// An unchanged region diffs to nothing.
	got = e.obj(http.MethodPost, "/api/snapshot/diff", `{"addr":"0x1000","size":16}`, http.StatusOK)
	if total := num(t, got, "total"); total != 0 {
		t.Fatalf("total = %v, want 0 before any change", total)
	}

	// 9999 == 0x270f, so exactly the low two bytes of the region change.
	e.obj(http.MethodPost, "/api/memory/modify", `{"addr":"0x1000","value":"9999"}`, http.StatusOK)

	got = e.obj(http.MethodPost, "/api/snapshot/diff", `{"addr":"0x1000","size":16}`, http.StatusOK)
	if total := num(t, got, "total"); total != 2 {
		t.Fatalf("total = %v, want 2 (body: %v)", total, got)
	}
	diffs := slice(t, got, "diffs")
	if len(diffs) != 2 {
		t.Fatalf("diffs = %d, want 2", len(diffs))
	}
	wantDiffs := []struct {
		addr   string
		offset float64
		before float64
		after  float64
	}{
		{"0x1000", 0, 0, 0x0f},
		{"0x1001", 1, 0, 0x27},
	}
	for i, want := range wantDiffs {
		d := object(t, diffs[i])
		if addr := str(t, d, "addr"); addr != want.addr {
			t.Errorf("diff %d addr = %q, want %q", i, addr, want.addr)
		}
		if off := num(t, d, "offset"); off != want.offset {
			t.Errorf("diff %d offset = %v, want %v", i, off, want.offset)
		}
		if b := num(t, d, "before"); b != want.before {
			t.Errorf("diff %d before = %v, want %v", i, b, want.before)
		}
		if a := num(t, d, "after"); a != want.after {
			t.Errorf("diff %d after = %v, want %v", i, a, want.after)
		}
	}
}

func TestSnapshotRejectsBadRegion(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	tests := []struct {
		name    string
		body    string
		wantSub string
	}{
		{"missing addr", `{"size":16}`, "addr required"},
		{"malformed addr", `{"addr":"nope","size":16}`, "invalid addr"},
		{"size zero", `{"addr":"0x1000","size":0}`, "size must be 1-"},
		{"size above the cap", `{"addr":"0x1000","size":16777217}`, "size must be 1-"},
		{"invalid JSON", `{"addr":`, "invalid JSON body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.errMsg(http.MethodPost, "/api/snapshot/take", tc.body, http.StatusBadRequest)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantSub)
			}
		})
	}
}

// --- bookmarks ---------------------------------------------------------------

func TestBookmarkAddListRemove(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	if got := e.arr(http.MethodGet, "/api/bookmark/list", "", http.StatusOK); len(got) != 0 {
		t.Fatalf("initial bookmarks = %v, want empty", got)
	}

	got := e.obj(http.MethodPost, "/api/bookmark/add", `{"addr":"0x1010","label":"hp"}`, http.StatusOK)
	if addr := str(t, got, "addr"); addr != "0x1010" {
		t.Errorf("addr = %q, want %q", addr, "0x1010")
	}
	e.obj(http.MethodPost, "/api/bookmark/add", `{"addr":"0x1020","label":"mp"}`, http.StatusOK)

	list := e.arr(http.MethodGet, "/api/bookmark/list", "", http.StatusOK)
	if len(list) != 2 {
		t.Fatalf("bookmarks = %d, want 2", len(list))
	}
	first := object(t, list[0])
	if idx := num(t, first, "index"); idx != 0 {
		t.Errorf("index = %v, want 0", idx)
	}
	if addr := str(t, first, "addr"); addr != "0x1010" {
		t.Errorf("addr = %q, want %q", addr, "0x1010")
	}
	if label := str(t, first, "label"); label != "hp" {
		t.Errorf("label = %q, want %q", label, "hp")
	}
	if vt := str(t, first, "type"); vt != "int32" {
		t.Errorf("type = %q, want %q", vt, "int32")
	}
	// The live value is read back through the driver.
	if v := str(t, first, "value"); v != "1337" {
		t.Errorf("value = %q, want %q", v, "1337")
	}

	e.obj(http.MethodPost, "/api/bookmark/remove", `{"index":0}`, http.StatusOK)

	list = e.arr(http.MethodGet, "/api/bookmark/list", "", http.StatusOK)
	if len(list) != 1 {
		t.Fatalf("bookmarks after remove = %d, want 1", len(list))
	}
	if addr := str(t, object(t, list[0]), "addr"); addr != "0x1020" {
		t.Errorf("remaining addr = %q, want %q", addr, "0x1020")
	}
}

func TestBookmarkRejectsBadInput(t *testing.T) {
	e := newEnv(t, Config{})

	tests := []struct {
		name    string
		path    string
		body    string
		wantSub string
	}{
		{"missing addr", "/api/bookmark/add", `{"label":"hp"}`, "addr required"},
		{"malformed addr", "/api/bookmark/add", `{"addr":"0xzz"}`, "invalid addr"},
		{"invalid JSON", "/api/bookmark/add", `{"addr":}`, "invalid JSON body"},
		{"remove out of range", "/api/bookmark/remove", `{"index":7}`, "index"},
		{"remove negative index", "/api/bookmark/remove", `{"index":-1}`, "index"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.errMsg(http.MethodPost, tc.path, tc.body, http.StatusBadRequest)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantSub)
			}
		})
	}
}

func TestBookmarkModifyAll(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()
	e.obj(http.MethodPost, "/api/bookmark/add", `{"addr":"0x1010","label":"a"}`, http.StatusOK)
	e.obj(http.MethodPost, "/api/bookmark/add", `{"addr":"0x1020","label":"b"}`, http.StatusOK)

	got := e.obj(http.MethodPost, "/api/bookmark/modify-all", `{"value":"7"}`, http.StatusOK)
	if c := num(t, got, "count"); c != 2 {
		t.Fatalf("count = %v, want 2", c)
	}
	for _, addr := range []uintptr{0x1010, 0x1020} {
		if v := e.heapInt32(addr); v != 7 {
			t.Errorf("memory at 0x%x = %d, want 7", addr, v)
		}
	}
}

// --- session save / load -----------------------------------------------------

func TestSessionSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	e := newEnv(t, Config{FileRoot: root})
	e.attach()

	e.obj(http.MethodPost, "/api/bookmark/add", `{"addr":"0x1010","label":"hp"}`, http.StatusOK)
	if c := num(t, e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK), "candidates"); c != 3 {
		t.Fatalf("candidates = %v, want 3", c)
	}

	saved := e.obj(http.MethodPost, "/api/session/save", `{"path":"state.json"}`, http.StatusOK)
	wantPath := filepath.Join(mustAbs(t, root), "state.json")
	if got := str(t, saved, "path"); got != wantPath {
		t.Fatalf("saved path = %q, want %q", got, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("state file: %v", err)
	}

	// Throw away everything that was saved.
	e.obj(http.MethodPost, "/api/bookmark/remove", `{"index":0}`, http.StatusOK)
	e.obj(http.MethodPost, "/api/search/reset", "", http.StatusOK)
	if c := num(t, e.obj(http.MethodGet, "/api/status", "", http.StatusOK), "candidates"); c != 0 {
		t.Fatal("candidates were not cleared before the load")
	}

	loaded := e.obj(http.MethodPost, "/api/session/load", `{"path":"state.json"}`, http.StatusOK)
	if got := str(t, loaded, "path"); got != wantPath {
		t.Errorf("loaded path = %q, want %q", got, wantPath)
	}
	if c := num(t, loaded, "candidates"); c != 3 {
		t.Fatalf("loaded candidates = %v, want 3", c)
	}

	if c := num(t, e.obj(http.MethodGet, "/api/status", "", http.StatusOK), "candidates"); c != 3 {
		t.Errorf("status candidates after load = %v, want 3", c)
	}
	list := e.arr(http.MethodGet, "/api/bookmark/list", "", http.StatusOK)
	if len(list) != 1 {
		t.Fatalf("bookmarks after load = %d, want 1", len(list))
	}
	if label := str(t, object(t, list[0]), "label"); label != "hp" {
		t.Errorf("label = %q, want %q", label, "hp")
	}

	// The restored session is rebound to the live driver, so it can still filter.
	if c := num(t, e.obj(http.MethodPost, "/api/search/filter", `{"mode":"unchanged"}`, http.StatusOK), "candidates"); c != 3 {
		t.Errorf("candidates after filtering a restored session = %v, want 3", c)
	}
}

func TestSessionSaveDefaultsToTheStateFileName(t *testing.T) {
	root := t.TempDir()
	e := newEnv(t, Config{FileRoot: root})

	// An empty body is allowed: every field is optional.
	got := e.obj(http.MethodPost, "/api/session/save", "", http.StatusOK)
	wantPath := filepath.Join(mustAbs(t, root), "memdroid.json")
	if p := str(t, got, "path"); p != wantPath {
		t.Errorf("path = %q, want %q", p, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("state file: %v", err)
	}
}

func TestSessionPathsAreConfined(t *testing.T) {
	root := t.TempDir()
	e := newEnv(t, Config{FileRoot: root})

	for _, path := range []string{"/api/session/save", "/api/session/load"} {
		t.Run(path, func(t *testing.T) {
			msg := e.errMsg(http.MethodPost, path, `{"path":"/etc/passwd"}`, http.StatusBadRequest)
			if !strings.Contains(msg, "absolute paths are not allowed") {
				t.Errorf("error = %q, want it to reject the absolute path", msg)
			}
		})
	}

	// Traversal is rejected outright, and nothing is written either inside or
	// outside the root.
	msg := e.errMsg(http.MethodPost, "/api/session/save", `{"path":"../escape.json"}`, http.StatusBadRequest)
	if !strings.Contains(msg, "escapes") {
		t.Errorf("error = %q, want it to report the escape", msg)
	}
	for _, dir := range []string{mustAbs(t, root), filepath.Dir(mustAbs(t, root))} {
		if _, err := os.Stat(filepath.Join(dir, "escape.json")); err == nil {
			t.Errorf("escape.json was written to %s", dir)
		}
	}
}

// TestSessionPathsRejectSymlinkEscape covers the case a pure lexical check
// misses: a link that lives inside the root but points out of it.
func TestSessionPathsRejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	e := newEnv(t, Config{FileRoot: root})

	msg := e.errMsg(http.MethodPost, "/api/session/save", `{"path":"link/state.json"}`, http.StatusBadRequest)
	if !strings.Contains(msg, "escapes") {
		t.Errorf("error = %q, want it to report the escape", msg)
	}
	if _, err := os.Stat(filepath.Join(outside, "state.json")); err == nil {
		t.Error("a file escaped the file root through a symlink")
	}
}

func TestSessionLoadMissingFile(t *testing.T) {
	e := newEnv(t, Config{FileRoot: t.TempDir()})
	msg := e.errMsg(http.MethodPost, "/api/session/load", `{"path":"absent.json"}`, http.StatusInternalServerError)
	if !strings.Contains(msg, "load state") {
		t.Errorf("error = %q, want it to name the operation", msg)
	}
}

// --- maps / processes --------------------------------------------------------

func TestMapsList(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	got := e.arr(http.MethodGet, "/api/maps", "", http.StatusOK)
	if len(got) != 1 {
		t.Fatalf("maps = %d, want 1", len(got))
	}
	m := object(t, got[0])
	if start := str(t, m, "start"); start != "0x1000" {
		t.Errorf("start = %q, want %q", start, "0x1000")
	}
	if end := str(t, m, "end"); end != "0x1400" {
		t.Errorf("end = %q, want %q", end, "0x1400")
	}
	if size := num(t, m, "size"); size != heapSize {
		t.Errorf("size = %v, want %d", size, heapSize)
	}
	if name := str(t, m, "name"); name != "[heap]" {
		t.Errorf("name = %q, want %q", name, "[heap]")
	}
}

func TestProcessAttachSwitchDetach(t *testing.T) {
	e := newEnv(t, Config{})

	list := e.arr(http.MethodGet, "/api/process/list", "", http.StatusOK)
	if len(list) != 1 {
		t.Fatalf("processes = %d, want 1", len(list))
	}
	p := object(t, list[0])
	if pid := num(t, p, "pid"); pid != 1 {
		t.Errorf("pid = %v, want 1", pid)
	}
	if name := str(t, p, "name"); name != "com.example.fake" {
		t.Errorf("name = %q, want %q", name, "com.example.fake")
	}

	if msg := e.errMsg(http.MethodPost, "/api/process/attach", `{"pid":0}`, http.StatusBadRequest); msg != "invalid pid" {
		t.Errorf("error = %q, want %q", msg, "invalid pid")
	}
	if msg := e.errMsg(http.MethodPost, "/api/process/attach", `{"pid":-3}`, http.StatusBadRequest); msg != "invalid pid" {
		t.Errorf("error = %q, want %q", msg, "invalid pid")
	}

	e.attach()
	if !e.fake.Attached(1) {
		t.Fatal("fake driver reports pid 1 as not attached")
	}

	attached := e.arr(http.MethodGet, "/api/process/attached", "", http.StatusOK)
	if len(attached) != 1 {
		t.Fatalf("attached = %d, want 1", len(attached))
	}
	a := object(t, attached[0])
	if pid := num(t, a, "pid"); pid != 1 {
		t.Errorf("pid = %v, want 1", pid)
	}
	if active, ok := a["active"].(bool); !ok || !active {
		t.Errorf("active = %v, want true", a["active"])
	}

	if msg := e.errMsg(http.MethodPost, "/api/process/switch", `{"pid":99}`, http.StatusBadRequest); msg != "pid is not attached" {
		t.Errorf("error = %q, want %q", msg, "pid is not attached")
	}
	e.obj(http.MethodPost, "/api/process/switch", `{"pid":1}`, http.StatusOK)

	got := e.obj(http.MethodPost, "/api/process/detach", "", http.StatusOK)
	if d := num(t, got, "detached"); d != 1 {
		t.Errorf("detached = %v, want 1", d)
	}
	if e.fake.Attached(1) {
		t.Error("fake driver still reports pid 1 as attached")
	}
	if boolean(t, e.obj(http.MethodGet, "/api/status", "", http.StatusOK), "attached") {
		t.Error("status still reports attached")
	}
}

func TestProcessStopContinue(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	for _, path := range []string{"/api/process/stop", "/api/process/continue"} {
		if !boolean(t, e.obj(http.MethodPost, path, "", http.StatusOK), "ok") {
			t.Errorf("%s: ok = false", path)
		}
	}
}

// --- watches and alerts ------------------------------------------------------

func TestWatchAddListRemove(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	// A long interval keeps the background poller from doing real work during
	// the test; the endpoint contract is what is under test here.
	got := e.obj(http.MethodPost, "/api/watch/add", `{"addr":"0x1010","interval_ms":3600000}`, http.StatusOK)
	if addr := str(t, got, "addr"); addr != "0x1010" {
		t.Errorf("addr = %q, want %q", addr, "0x1010")
	}
	if vt := str(t, got, "value_type"); vt != "int32" {
		t.Errorf("value_type = %q, want %q", vt, "int32")
	}
	if ms := num(t, got, "interval_ms"); ms != 3600000 {
		t.Errorf("interval_ms = %v, want 3600000", ms)
	}

	list := e.arr(http.MethodGet, "/api/watch/list", "", http.StatusOK)
	if len(list) != 1 || list[0] != "0x1010" {
		t.Fatalf("watch list = %v, want [0x1010]", list)
	}
	if watched := slice(t, e.obj(http.MethodGet, "/api/status", "", http.StatusOK), "watched"); len(watched) != 1 {
		t.Errorf("status watched = %v, want one entry", watched)
	}

	e.obj(http.MethodPost, "/api/watch/remove", `{"addr":"0x1010"}`, http.StatusOK)
	if list := e.arr(http.MethodGet, "/api/watch/list", "", http.StatusOK); len(list) != 0 {
		t.Errorf("watch list after remove = %v, want empty", list)
	}
	// Removing an address that is not watched is a client error.
	if msg := e.errMsg(http.MethodPost, "/api/watch/remove", `{"addr":"0x1010"}`, http.StatusBadRequest); msg == "" {
		t.Error("removing an unwatched address returned no error")
	}
}

func TestWatchAddRejectsNegativeInterval(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	msg := e.errMsg(http.MethodPost, "/api/watch/add", `{"addr":"0x1010","interval_ms":-5}`, http.StatusBadRequest)
	if msg != "interval_ms must be positive" {
		t.Errorf("error = %q, want %q", msg, "interval_ms must be positive")
	}
}

func TestAlertAddListRemove(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	body := `{"addr":"0x1010","condition":"above","threshold":"9000","action":"notify","interval_ms":3600000}`
	got := e.obj(http.MethodPost, "/api/alert/add", body, http.StatusOK)
	if addr := str(t, got, "addr"); addr != "0x1010" {
		t.Errorf("addr = %q, want %q", addr, "0x1010")
	}

	if list := e.arr(http.MethodGet, "/api/alert/list", "", http.StatusOK); len(list) != 1 {
		t.Fatalf("alert list = %v, want one entry", list)
	}

	e.obj(http.MethodPost, "/api/alert/remove", `{"addr":"0x1010"}`, http.StatusOK)
	if list := e.arr(http.MethodGet, "/api/alert/list", "", http.StatusOK); len(list) != 0 {
		t.Errorf("alert list after remove = %v, want empty", list)
	}
}

func TestAlertAddRejectsBadInput(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	tests := []struct {
		name string
		body string
	}{
		{"unknown condition", `{"addr":"0x1010","condition":"sideways","action":"notify"}`},
		{"unknown action", `{"addr":"0x1010","condition":"changed","action":"explode"}`},
		{"threshold not parseable", `{"addr":"0x1010","condition":"above","threshold":"abc","action":"notify"}`},
		{"malformed addr", `{"addr":"nope","condition":"changed","action":"notify"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if msg := e.errMsg(http.MethodPost, "/api/alert/add", tc.body, http.StatusBadRequest); msg == "" {
				t.Error("no error message")
			}
		})
	}
}

// --- freeze ------------------------------------------------------------------

func TestFreezeUnfreeze(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	e.obj(http.MethodPost, "/api/memory/freeze-interval", `{"interval_ms":3600000}`, http.StatusOK)
	if msg := e.errMsg(http.MethodPost, "/api/memory/freeze-interval", `{"interval_ms":0}`, http.StatusBadRequest); msg != "interval_ms must be positive" {
		t.Errorf("error = %q, want %q", msg, "interval_ms must be positive")
	}

	e.obj(http.MethodPost, "/api/memory/freeze", `{"addr":"0x1010","value":"55"}`, http.StatusOK)
	if list := e.arr(http.MethodGet, "/api/memory/frozen", "", http.StatusOK); len(list) != 1 || list[0] != "0x1010" {
		t.Fatalf("frozen = %v, want [0x1010]", list)
	}

	e.obj(http.MethodPost, "/api/memory/unfreeze", `{"addr":"0x1010"}`, http.StatusOK)
	if list := e.arr(http.MethodGet, "/api/memory/frozen", "", http.StatusOK); len(list) != 0 {
		t.Errorf("frozen after unfreeze = %v, want empty", list)
	}
	if msg := e.errMsg(http.MethodPost, "/api/memory/unfreeze", `{"addr":"0x1010"}`, http.StatusBadRequest); msg == "" {
		t.Error("unfreezing an unfrozen address returned no error")
	}
}

// --- adb-backed endpoints ----------------------------------------------------

// TestADBEndpointsValidateBeforeShellingOut covers only the branches that run
// before the first exec, so the suite never depends on a real adb binary or a
// connected device.
func TestADBEndpointsValidateBeforeShellingOut(t *testing.T) {
	e := newEnv(t, Config{})

	tests := []struct {
		name    string
		path    string
		body    string
		wantSub string
	}{
		{"device select without serial", "/api/device/select", `{}`, "serial required"},
		{"device select with empty serial", "/api/device/select", `{"serial":""}`, "serial required"},
		{"device select with invalid JSON", "/api/device/select", `{`, "invalid JSON body"},
		{"connect wifi without addr", "/api/device/connect-wifi", `{}`, "addr required"},
		{"disconnect wifi without addr", "/api/device/disconnect-wifi", `{}`, "addr required"},
		{"process search without name", "/api/process/search", `{}`, "name required"},
		{"process search with invalid JSON", "/api/process/search", `nope`, "invalid JSON body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.errMsg(http.MethodPost, tc.path, tc.body, http.StatusBadRequest)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantSub)
			}
		})
	}
}

// --- generic request-decoding failures ---------------------------------------

func TestInvalidJSONBodies(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()
	// /api/search/filter checks for a session before it decodes, so give it one.
	e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK)

	paths := []string{
		"/api/search/type",
		"/api/search/filter",
		"/api/memory/modify",
		"/api/memory/unfreeze",
		"/api/bookmark/add",
		"/api/bookmark/remove",
		"/api/watch/remove",
		"/api/snapshot/take",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := e.rec(http.MethodPost, path, `{"addr": `)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if !strings.HasPrefix(body["error"], "invalid JSON body") {
				t.Errorf("error = %q, want an invalid-JSON message", body["error"])
			}
		})
	}
}

// TestErrorResponsesAreJSON pins the content type callers rely on.
func TestErrorResponsesAreJSON(t *testing.T) {
	e := newEnv(t, Config{})

	rec := e.rec(http.MethodGet, "/api/maps", "")
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("error Content-Type = %q, want application/json", ct)
	}
	rec = e.rec(http.MethodGet, "/api/status", "")
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("success Content-Type = %q, want application/json", ct)
	}
}

// --- pattern and string scans ------------------------------------------------

func TestSearchString(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	got := e.obj(http.MethodPost, "/api/search/string", `{"value":"MEMDROID"}`, http.StatusOK)
	if c := num(t, got, "count"); c != 1 {
		t.Fatalf("count = %v, want 1", c)
	}
	if c := num(t, got, "candidates"); c != 1 {
		t.Errorf("candidates = %v, want 1", c)
	}
	if truncated, ok := got["truncated"].(bool); !ok || truncated {
		t.Errorf("truncated = %v, want false", got["truncated"])
	}

	// A string scan seeds the session with raw byte candidates, so both the
	// type and the value formatting switch to "bytes".
	if vt := str(t, e.obj(http.MethodGet, "/api/status", "", http.StatusOK), "value_type"); vt != "bytes" {
		t.Errorf("value_type = %q, want %q", vt, "bytes")
	}
	items := slice(t, e.obj(http.MethodGet, "/api/search/candidates", "", http.StatusOK), "items")
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	assertCandidate(t, items[0], "0x1080", "4d454d44524f4944")

	// UTF-16 finds nothing in this heap, which is a success with zero matches.
	got = e.obj(http.MethodPost, "/api/search/string", `{"value":"MEMDROID","encoding":"utf16"}`, http.StatusOK)
	if c := num(t, got, "count"); c != 0 {
		t.Errorf("utf16 count = %v, want 0", c)
	}
}

func TestSearchStringRejectsBadInput(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	if msg := e.errMsg(http.MethodPost, "/api/search/string", `{"value":""}`, http.StatusBadRequest); !strings.Contains(msg, "empty string") {
		t.Errorf("error = %q, want it to mention the empty string", msg)
	}
	msg := e.errMsg(http.MethodPost, "/api/search/string", `{"value":"x","encoding":"rot13"}`, http.StatusBadRequest)
	if !strings.Contains(msg, "unknown string encoding") {
		t.Errorf("error = %q, want it to mention the encoding", msg)
	}
}

func TestSearchPattern(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	// "MEMD" with the third byte wildcarded.
	got := e.obj(http.MethodPost, "/api/search/pattern", `{"pattern":"4d 45 ?? 44 52"}`, http.StatusOK)
	if c := num(t, got, "count"); c != 1 {
		t.Fatalf("count = %v, want 1 (body: %v)", c, got)
	}
	items := slice(t, e.obj(http.MethodGet, "/api/search/candidates", "", http.StatusOK), "items")
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	assertCandidate(t, items[0], "0x1080", "4d454d4452")
}

func TestSearchPatternRejectsBadInput(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	if msg := e.errMsg(http.MethodPost, "/api/search/pattern", `{"pattern":""}`, http.StatusBadRequest); !strings.Contains(msg, "empty pattern") {
		t.Errorf("error = %q, want it to mention the empty pattern", msg)
	}
	if msg := e.errMsg(http.MethodPost, "/api/search/pattern", `{"pattern":"zz"}`, http.StatusBadRequest); !strings.Contains(msg, "invalid token") {
		t.Errorf("error = %q, want it to name the bad token", msg)
	}
}

// --- write-string, dump, freeze-all ------------------------------------------

func TestMemoryWriteString(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	got := e.obj(http.MethodPost, "/api/memory/write-string", `{"addr":"0x1080","value":"ZZZZ"}`, http.StatusOK)
	if n := num(t, got, "bytes"); n != 4 {
		t.Errorf("bytes = %v, want 4", n)
	}
	if s := string(e.fake.Bytes(0x1080, 8)); s != "ZZZZROID" {
		t.Errorf("memory at 0x1080 = %q, want %q", s, "ZZZZROID")
	}

	if msg := e.errMsg(http.MethodPost, "/api/memory/write-string", `{"addr":"0x1080","value":""}`, http.StatusBadRequest); msg != "value required" {
		t.Errorf("error = %q, want %q", msg, "value required")
	}
}

func TestMemoryDump(t *testing.T) {
	root := t.TempDir()
	e := newEnv(t, Config{FileRoot: root})
	e.attach()

	got := e.obj(http.MethodPost, "/api/memory/dump", `{"addr":"0x1010","size":16,"path":"dump.hex"}`, http.StatusOK)
	wantPath := filepath.Join(mustAbs(t, root), "dump.hex")
	if p := str(t, got, "path"); p != wantPath {
		t.Fatalf("path = %q, want %q", p, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if !strings.Contains(string(data), "0000000000001010") {
		t.Errorf("dump = %q, want it to contain the base address", data)
	}
	if !strings.Contains(string(data), "39 05 00 00") {
		t.Errorf("dump = %q, want it to contain the dumped bytes", data)
	}

	if msg := e.errMsg(http.MethodPost, "/api/memory/dump", `{"addr":"0x1010","size":0}`, http.StatusBadRequest); !strings.Contains(msg, "size must be 1-") {
		t.Errorf("error = %q, want a size complaint", msg)
	}
	if msg := e.errMsg(http.MethodPost, "/api/memory/dump", `{"addr":"0x1010","size":16,"path":"/tmp/escape.hex"}`, http.StatusBadRequest); !strings.Contains(msg, "absolute paths are not allowed") {
		t.Errorf("error = %q, want the path to be rejected", msg)
	}
}

func TestMemoryFreezeAll(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	// No session yet.
	if msg := e.errMsg(http.MethodPost, "/api/memory/freeze-all", "", http.StatusBadRequest); msg != "no active search session" {
		t.Errorf("error = %q, want %q", msg, "no active search session")
	}

	// A long interval keeps the freeze pollers idle for the duration of the test.
	e.obj(http.MethodPost, "/api/memory/freeze-interval", `{"interval_ms":3600000}`, http.StatusOK)
	e.obj(http.MethodPost, "/api/search/value", `{"value":"1337"}`, http.StatusOK)

	got := e.obj(http.MethodPost, "/api/memory/freeze-all", "", http.StatusOK)
	if c := num(t, got, "count"); c != 3 {
		t.Fatalf("count = %v, want 3", c)
	}
	if list := e.arr(http.MethodGet, "/api/memory/frozen", "", http.StatusOK); len(list) != 3 {
		t.Errorf("frozen = %v, want 3 entries", list)
	}

	// Detaching stops every freeze poller.
	e.obj(http.MethodPost, "/api/process/detach", "", http.StatusOK)
	if list := e.arr(http.MethodGet, "/api/memory/frozen", "", http.StatusOK); len(list) != 0 {
		t.Errorf("frozen after detach = %v, want empty", list)
	}
}

// --- pointer -----------------------------------------------------------------

func TestPointerRejectsBadInput(t *testing.T) {
	e := newEnv(t, Config{})
	e.attach()

	tests := []struct {
		name    string
		path    string
		body    string
		wantSub string
	}{
		{"scan without addr", "/api/pointer/scan", `{}`, "addr required"},
		{"scan with negative max_offset", "/api/pointer/scan", `{"addr":"0x1010","max_offset":-1}`, "max_offset must not be negative"},
		{"resolve without a label", "/api/pointer/resolve", `{"offsets":[0]}`, "label and offsets required"},
		{"resolve without offsets", "/api/pointer/resolve", `{"label":"[heap]"}`, "label and offsets required"},
		{"resolve with a bad base offset", "/api/pointer/resolve", `{"label":"[heap]","base_offset":"zz","offsets":[0]}`, "invalid addr"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := e.errMsg(http.MethodPost, tc.path, tc.body, http.StatusBadRequest)
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantSub)
			}
		})
	}
}

// --- .CT import ---------------------------------------------------------------

const ctFixture = `<?xml version="1.0" encoding="utf-8"?>
<CheatTable>
  <CheatEntries>
    <CheatEntry>
      <Description>"Health"</Description>
      <VariableType>4 Bytes</VariableType>
      <Address>1010</Address>
    </CheatEntry>
    <CheatEntry>
      <Description>"Mana"</Description>
      <VariableType>Float</VariableType>
      <Address>1020</Address>
    </CheatEntry>
  </CheatEntries>
</CheatTable>`

func TestImportCT(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "table.CT"), []byte(ctFixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	e := newEnv(t, Config{FileRoot: root})

	got := e.obj(http.MethodPost, "/api/import/ct", `{"path":"table.CT"}`, http.StatusOK)
	if n := num(t, got, "imported"); n != 2 {
		t.Fatalf("imported = %v, want 2", n)
	}

	list := e.arr(http.MethodGet, "/api/bookmark/list", "", http.StatusOK)
	if len(list) != 2 {
		t.Fatalf("bookmarks = %d, want 2", len(list))
	}
	first := object(t, list[0])
	if addr := str(t, first, "addr"); addr != "0x1010" {
		t.Errorf("addr = %q, want %q", addr, "0x1010")
	}
	if label := str(t, first, "label"); label != "Health" {
		t.Errorf("label = %q, want %q", label, "Health")
	}
}

func TestImportCTRejectsBadInput(t *testing.T) {
	root := t.TempDir()
	e := newEnv(t, Config{FileRoot: root})

	if msg := e.errMsg(http.MethodPost, "/api/import/ct", `{}`, http.StatusBadRequest); msg != "path required" {
		t.Errorf("error = %q, want %q", msg, "path required")
	}
	if msg := e.errMsg(http.MethodPost, "/api/import/ct", `{"path":"/etc/passwd"}`, http.StatusBadRequest); !strings.Contains(msg, "absolute paths are not allowed") {
		t.Errorf("error = %q, want the path to be rejected", msg)
	}
	if msg := e.errMsg(http.MethodPost, "/api/import/ct", `{"path":"absent.CT"}`, http.StatusInternalServerError); !strings.Contains(msg, "import CT") {
		t.Errorf("error = %q, want it to name the operation", msg)
	}
}

// --- shared assertions -------------------------------------------------------

func assertCandidate(t *testing.T, item any, wantAddr, wantValue string) {
	t.Helper()
	m := object(t, item)
	if addr := str(t, m, "addr"); addr != wantAddr {
		t.Errorf("candidate addr = %q, want %q", addr, wantAddr)
	}
	if v := str(t, m, "value"); v != wantValue {
		t.Errorf("candidate %s value = %q, want %q", wantAddr, v, wantValue)
	}
}

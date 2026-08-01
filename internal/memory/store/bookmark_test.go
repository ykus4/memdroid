package store

import (
	"encoding/binary"
	"sync"
	"testing"

	"memdroid/internal/driver/drivertest"
	"memdroid/internal/memory/search"
)

const bmBase = uintptr(0x1000)

func bmLE32(v int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

func bmLE64(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

// bmFake returns a fake process with a 64-byte writable region at bmBase.
func bmFake() *drivertest.Fake {
	return drivertest.New(drivertest.Region{Start: bmBase, Name: "[heap]", Data: make([]byte, 64)})
}

func TestBookmarkAddGetLen(t *testing.T) {
	bl := NewBookmarkList()

	if got := bl.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
	if _, ok := bl.Get(0); ok {
		t.Error("Get(0) on an empty list must report not-found")
	}

	bl.Add(0x1000, "hp", search.TypeInt32)
	bl.Add(0x2000, "mp", search.TypeFloat32)

	if got := bl.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}

	got, ok := bl.Get(1)
	if !ok {
		t.Fatal("Get(1) reported not-found")
	}
	want := Bookmark{Addr: 0x2000, Label: "mp", VType: search.TypeFloat32}
	if got != want {
		t.Errorf("Get(1) = %+v, want %+v", got, want)
	}
}

func TestBookmarkGetOutOfRange(t *testing.T) {
	bl := NewBookmarkList()
	bl.Add(0x1000, "hp", search.TypeInt32)

	for _, idx := range []int{-1, 1, 100} {
		if b, ok := bl.Get(idx); ok {
			t.Errorf("Get(%d) = %+v, true; want not-found", idx, b)
		}
	}
}

func TestBookmarkAddPreservesInsertionOrder(t *testing.T) {
	bl := NewBookmarkList()
	bl.Add(0x3000, "c", search.TypeInt32)
	bl.Add(0x1000, "a", search.TypeInt32)
	bl.Add(0x2000, "b", search.TypeInt32)

	want := []uintptr{0x3000, 0x1000, 0x2000}
	for i, addr := range want {
		b, ok := bl.Get(i)
		if !ok || b.Addr != addr {
			t.Errorf("entry %d = %+v, want addr 0x%x", i, b, addr)
		}
	}
}

func TestBookmarkRemove(t *testing.T) {
	bl := NewBookmarkList()
	bl.Add(0x1000, "a", search.TypeInt32)
	bl.Add(0x2000, "b", search.TypeInt32)
	bl.Add(0x3000, "c", search.TypeInt32)

	if err := bl.Remove(1); err != nil {
		t.Fatalf("Remove(1): %v", err)
	}
	if got := bl.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}

	entries := bl.Entries()
	if entries[0].Label != "a" || entries[1].Label != "c" {
		t.Errorf("after Remove(1) entries = %+v, want a then c", entries)
	}
}

func TestBookmarkRemoveOutOfRange(t *testing.T) {
	bl := NewBookmarkList()
	bl.Add(0x1000, "a", search.TypeInt32)

	for _, idx := range []int{-1, 1, 99} {
		if err := bl.Remove(idx); err == nil {
			t.Errorf("Remove(%d) must fail", idx)
		}
	}
	if got := bl.Len(); got != 1 {
		t.Errorf("Len() = %d, want 1 — a failed Remove must not mutate", got)
	}

	empty := NewBookmarkList()
	if err := empty.Remove(0); err == nil {
		t.Error("Remove(0) on an empty list must fail")
	}
}

// Entries hands out a copy; the CLI iterates it while HTTP handlers mutate the
// original.
func TestBookmarkEntriesReturnsCopy(t *testing.T) {
	bl := NewBookmarkList()
	bl.Add(0x1000, "hp", search.TypeInt32)
	bl.Add(0x2000, "mp", search.TypeInt32)

	entries := bl.Entries()
	entries[0].Label = "mutated"
	entries[1].Addr = 0xDEAD

	got := bl.Entries()
	if got[0].Label != "hp" {
		t.Errorf("entry 0 label = %q, want %q — Entries() aliases the internal slice", got[0].Label, "hp")
	}
	if got[1].Addr != 0x2000 {
		t.Errorf("entry 1 addr = 0x%x, want 0x2000 — Entries() aliases the internal slice", got[1].Addr)
	}

	// Appending to the returned slice must not affect the list either.
	entries = append(entries, Bookmark{Addr: 0x3000})
	_ = entries
	if got := bl.Len(); got != 2 {
		t.Errorf("Len() = %d, want 2", got)
	}
}

func TestBookmarkEntriesOnEmptyList(t *testing.T) {
	bl := NewBookmarkList()
	if got := bl.Entries(); len(got) != 0 {
		t.Errorf("Entries() = %+v, want empty", got)
	}
}

func TestBookmarkReplace(t *testing.T) {
	bl := NewBookmarkList()
	bl.Add(0x1000, "old", search.TypeInt32)

	fresh := []Bookmark{
		{Addr: 0xA000, Label: "one", VType: search.TypeInt64},
		{Addr: 0xB000, Label: "two", VType: search.TypeUint32},
	}
	bl.Replace(fresh)

	if got := bl.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	got := bl.Entries()
	for i := range fresh {
		if got[i] != fresh[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], fresh[i])
		}
	}

	// Replace must clone its input, so later caller mutations do not leak in.
	fresh[0].Label = "hijacked"
	if bl.Entries()[0].Label != "one" {
		t.Error("Replace aliases the caller's slice")
	}

	bl.Replace(nil)
	if got := bl.Len(); got != 0 {
		t.Errorf("Len() after Replace(nil) = %d, want 0", got)
	}
}

// Regression: BookmarkList had no mutex, so the CLI loop and the HTTP handlers
// could race on the entry slice.
func TestBookmarkConcurrentAddAndEntries(t *testing.T) {
	bl := NewBookmarkList()

	const writers = 8
	const perWriter = 200

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				bl.Add(uintptr(w*perWriter+i), "b", search.TypeInt32)
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				for _, e := range bl.Entries() {
					_ = e.Addr
				}
				_ = bl.Len()
				_, _ = bl.Get(0)
			}
		}()
	}
	wg.Wait()

	if got := bl.Len(); got != writers*perWriter {
		t.Errorf("Len() = %d, want %d", got, writers*perWriter)
	}
}

func TestBookmarkConcurrentMutation(t *testing.T) {
	bl := NewBookmarkList()
	fake := bmFake()

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				bl.Add(bmBase, "b", search.TypeInt32)
				_ = bl.Remove(0)
				bl.Replace([]Bookmark{{Addr: bmBase, Label: "r", VType: search.TypeInt32}})
				_ = bl.Values(fake, 1)
				_ = bl.ModifyAll(fake, 1, bmLE32(1), search.TypeInt32)
			}
		}()
	}
	wg.Wait()
}

// --- driver-backed helpers ---

func TestBookmarkValues(t *testing.T) {
	fake := bmFake()
	if err := fake.Poke(1, bmBase, bmLE32(1234)); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if err := fake.Poke(1, bmBase+8, bmLE64(-9)); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	bl := NewBookmarkList()
	bl.Add(bmBase, "hp", search.TypeInt32)
	bl.Add(bmBase+8, "score", search.TypeInt64)
	bl.Add(0xDEAD0000, "unmapped", search.TypeInt32)

	got := bl.Values(fake, 1)

	if len(got) != 3 {
		t.Fatalf("Values() = %v, want 3 entries", got)
	}
	if got[bmBase] != "1234" {
		t.Errorf("value at 0x%x = %q, want %q", bmBase, got[bmBase], "1234")
	}
	if got[bmBase+8] != "-9" {
		t.Errorf("value at 0x%x = %q, want %q", bmBase+8, got[bmBase+8], "-9")
	}
	if got[0xDEAD0000] != "?" {
		t.Errorf("value at an unmapped address = %q, want %q", got[0xDEAD0000], "?")
	}
}

func TestBookmarkValuesWithoutPID(t *testing.T) {
	fake := bmFake()
	bl := NewBookmarkList()
	bl.Add(bmBase, "hp", search.TypeInt32)

	got := bl.Values(fake, 0)

	if got[bmBase] != "?" {
		t.Errorf("value with pid 0 = %q, want %q", got[bmBase], "?")
	}
	if fake.Peeks != 0 {
		t.Errorf("the driver was read %d times with no attached process, want 0", fake.Peeks)
	}
}

func TestBookmarkValuesEmptyList(t *testing.T) {
	bl := NewBookmarkList()
	if got := bl.Values(bmFake(), 1); len(got) != 0 {
		t.Errorf("Values() = %v, want empty", got)
	}
}

func TestBookmarkModifyAllSkipsOtherTypes(t *testing.T) {
	fake := bmFake()
	bl := NewBookmarkList()
	bl.Add(bmBase, "a", search.TypeInt32)
	bl.Add(bmBase+4, "b", search.TypeInt32)
	bl.Add(bmBase+8, "c", search.TypeInt64) // different type: must be skipped

	n := bl.ModifyAll(fake, 1, bmLE32(7), search.TypeInt32)

	if n != 2 {
		t.Errorf("ModifyAll modified %d addresses, want 2", n)
	}
	if got := search.FormatValue(fake.Bytes(bmBase, 4), search.TypeInt32); got != "7" {
		t.Errorf("memory at 0x%x = %s, want 7", bmBase, got)
	}
	if got := search.FormatValue(fake.Bytes(bmBase+4, 4), search.TypeInt32); got != "7" {
		t.Errorf("memory at 0x%x = %s, want 7", bmBase+4, got)
	}
	if got := search.FormatValue(fake.Bytes(bmBase+8, 8), search.TypeInt64); got != "0" {
		t.Errorf("the int64 bookmark was written despite the type mismatch: %s", got)
	}
}

func TestBookmarkModifyAllCountsOnlySuccesses(t *testing.T) {
	fake := bmFake()
	bl := NewBookmarkList()
	bl.Add(bmBase, "ok", search.TypeInt32)
	bl.Add(0xDEAD0000, "unmapped", search.TypeInt32)

	n := bl.ModifyAll(fake, 1, bmLE32(3), search.TypeInt32)

	if n != 1 {
		t.Errorf("ModifyAll = %d, want 1 (the unmapped write fails)", n)
	}
}

func TestBookmarkModifyAllNoMatches(t *testing.T) {
	fake := bmFake()
	bl := NewBookmarkList()
	bl.Add(bmBase, "a", search.TypeFloat64)

	if n := bl.ModifyAll(fake, 1, bmLE32(1), search.TypeInt32); n != 0 {
		t.Errorf("ModifyAll = %d, want 0", n)
	}
	if fake.Pokes != 0 {
		t.Errorf("the driver was written %d times, want 0", fake.Pokes)
	}
}

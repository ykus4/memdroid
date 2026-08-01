package watch

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"memdroid/internal/driver/drivertest"
	"memdroid/internal/memory/search"
)

const (
	testPID      = 1
	base         = uintptr(0x1000)
	tick         = 5 * time.Millisecond
	eventTimeout = 3 * time.Second
)

func le32(v int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

// newFake returns a fake process with a 64-byte writable region at base.
func newFake() *drivertest.Fake {
	return drivertest.New(drivertest.Region{Start: base, Name: "[heap]", Data: make([]byte, 64)})
}

// recv waits for one value on ch, failing the test on timeout.
func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(eventTimeout):
		var zero T
		t.Fatalf("timed out waiting for %s", what)
		return zero
	}
}

// changeSink returns a listener that forwards events on a buffered channel,
// dropping extras so a repeated event can never block the watcher goroutine.
func changeSink(n int) (func(ChangeEvent), <-chan ChangeEvent) {
	ch := make(chan ChangeEvent, n)
	return func(ev ChangeEvent) {
		select {
		case ch <- ev:
		default:
		}
	}, ch
}

func TestWatcherEmitsChangeEvent(t *testing.T) {
	fake := newFake()
	if err := fake.Poke(testPID, base, le32(7)); err != nil {
		t.Fatalf("seed Poke: %v", err)
	}

	w := NewWatcher()
	defer w.UnwatchAll()

	fn, events := changeSink(8)
	w.OnChange(fn)

	if err := w.Watch(fake, testPID, base, search.TypeInt32, tick); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := fake.Poke(testPID, base, le32(1234)); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	ev := recv(t, events, "change event")
	if ev.Addr != base {
		t.Errorf("Addr = 0x%x, want 0x%x", ev.Addr, base)
	}
	if ev.Prev != "7" {
		t.Errorf("Prev = %q, want %q", ev.Prev, "7")
	}
	if ev.Cur != "1234" {
		t.Errorf("Cur = %q, want %q", ev.Cur, "1234")
	}
}

// Regression: the watcher used to hold a single assignable OnChange field, so a
// second registration silently replaced the first. Both must now fire.
func TestWatcherTwoListenersBothFire(t *testing.T) {
	fake := newFake()

	w := NewWatcher()
	defer w.UnwatchAll()

	fnA, eventsA := changeSink(8)
	fnB, eventsB := changeSink(8)
	w.OnChange(fnA)
	w.OnChange(fnB)

	if err := w.Watch(fake, testPID, base, search.TypeInt32, tick); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := fake.Poke(testPID, base, le32(99)); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	a := recv(t, eventsA, "the first listener's event")
	b := recv(t, eventsB, "the second listener's event")

	if a.Cur != "99" {
		t.Errorf("listener A Cur = %q, want %q", a.Cur, "99")
	}
	if b.Cur != "99" {
		t.Errorf("listener B Cur = %q, want %q", b.Cur, "99")
	}
}

func TestWatcherOnChangeRemove(t *testing.T) {
	fake := newFake()

	w := NewWatcher()
	defer w.UnwatchAll()

	fnA, eventsA := changeSink(8)
	fnB, eventsB := changeSink(8)
	removeA := w.OnChange(fnA)
	w.OnChange(fnB)
	removeA()

	if err := w.Watch(fake, testPID, base, search.TypeInt32, tick); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := fake.Poke(testPID, base, le32(5)); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	recv(t, eventsB, "the surviving listener's event")

	select {
	case ev := <-eventsA:
		t.Errorf("the removed listener still fired: %+v", ev)
	default:
	}
}

func TestWatcherReportsSuccessiveChanges(t *testing.T) {
	fake := newFake()

	w := NewWatcher()
	defer w.UnwatchAll()

	fn, events := changeSink(64)
	w.OnChange(fn)

	if err := w.Watch(fake, testPID, base, search.TypeInt32, tick); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := fake.Poke(testPID, base, le32(1)); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	first := recv(t, events, "the first change")
	if first.Prev != "0" || first.Cur != "1" {
		t.Errorf("first change = %q -> %q, want 0 -> 1", first.Prev, first.Cur)
	}

	if err := fake.Poke(testPID, base, le32(2)); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	second := recv(t, events, "the second change")
	if second.Prev != "1" || second.Cur != "2" {
		t.Errorf("second change = %q -> %q, want 1 -> 2", second.Prev, second.Cur)
	}
}

func TestWatcherFormatsFloatValues(t *testing.T) {
	fake := newFake()

	w := NewWatcher()
	defer w.UnwatchAll()

	fn, events := changeSink(8)
	w.OnChange(fn)

	if err := w.Watch(fake, testPID, base, search.TypeFloat32, tick); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	val, err := search.ParseValue("1.5", search.TypeFloat32)
	if err != nil {
		t.Fatalf("ParseValue: %v", err)
	}
	if err := fake.Poke(testPID, base, val); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	ev := recv(t, events, "float change event")
	if ev.Cur != "1.5" {
		t.Errorf("Cur = %q, want %q", ev.Cur, "1.5")
	}
}

func TestWatchRejectsVariableWidthType(t *testing.T) {
	w := NewWatcher()
	defer w.UnwatchAll()

	err := w.Watch(newFake(), testPID, base, search.TypeBytes, tick)
	if err == nil {
		t.Fatal("Watch(TypeBytes) must fail: the type has no fixed size")
	}
	if len(w.List()) != 0 {
		t.Errorf("a rejected Watch must not register anything: %v", w.List())
	}
}

func TestWatchRejectsDuplicateAddress(t *testing.T) {
	fake := newFake()
	w := NewWatcher()
	defer w.UnwatchAll()

	if err := w.Watch(fake, testPID, base, search.TypeInt32, time.Second); err != nil {
		t.Fatalf("first Watch: %v", err)
	}
	if err := w.Watch(fake, testPID, base, search.TypeInt32, time.Second); err == nil {
		t.Fatal("watching the same address twice must fail")
	}
	if got := len(w.List()); got != 1 {
		t.Errorf("List() has %d entries, want 1", got)
	}
}

func TestWatchPropagatesInitialReadError(t *testing.T) {
	fake := newFake()
	sentinel := errors.New("boom")
	fake.PeekErr = sentinel

	w := NewWatcher()
	defer w.UnwatchAll()

	err := w.Watch(fake, testPID, base, search.TypeInt32, tick)
	if err == nil {
		t.Fatal("Watch must fail when the initial read fails")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap %v", err, sentinel)
	}
	if len(w.List()) != 0 {
		t.Errorf("a failed Watch must not register anything: %v", w.List())
	}
}

func TestUnwatch(t *testing.T) {
	fake := newFake()
	w := NewWatcher()
	defer w.UnwatchAll()

	if err := w.Unwatch(base); err == nil {
		t.Error("Unwatch of an address that is not watched must fail")
	}

	if err := w.Watch(fake, testPID, base, search.TypeInt32, time.Second); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := w.Unwatch(base); err != nil {
		t.Fatalf("Unwatch: %v", err)
	}
	if got := w.List(); len(got) != 0 {
		t.Errorf("List() = %v, want empty", got)
	}
	if err := w.Unwatch(base); err == nil {
		t.Error("a second Unwatch must fail")
	}
}

func TestWatcherListIsSorted(t *testing.T) {
	fake := newFake()
	w := NewWatcher()
	defer w.UnwatchAll()

	for _, off := range []uintptr{0x18, 0x00, 0x10, 0x08} {
		if err := w.Watch(fake, testPID, base+off, search.TypeInt32, time.Second); err != nil {
			t.Fatalf("Watch(0x%x): %v", base+off, err)
		}
	}

	want := []uintptr{base, base + 0x08, base + 0x10, base + 0x18}
	got := w.List()
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List() = %v, want %v", got, want)
		}
	}
}

func TestUnwatchAll(t *testing.T) {
	fake := newFake()
	w := NewWatcher()

	for _, off := range []uintptr{0x00, 0x08, 0x10} {
		if err := w.Watch(fake, testPID, base+off, search.TypeInt32, time.Second); err != nil {
			t.Fatalf("Watch: %v", err)
		}
	}
	w.UnwatchAll()

	if got := w.List(); len(got) != 0 {
		t.Errorf("List() = %v, want empty", got)
	}
	// Addresses become watchable again after a full teardown.
	if err := w.Watch(fake, testPID, base, search.TypeInt32, time.Second); err != nil {
		t.Errorf("re-Watch after UnwatchAll: %v", err)
	}
	w.UnwatchAll()
}

// A read failure mid-flight must be skipped, not crash the poller, and the
// watcher must recover once reads succeed again.
func TestWatcherSurvivesReadErrors(t *testing.T) {
	fake := newFake()
	w := NewWatcher()
	defer w.UnwatchAll()

	fn, events := changeSink(8)
	w.OnChange(fn)

	if err := w.Watch(fake, testPID, base, search.TypeInt32, tick); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Unmapped address reads fail inside the poll loop.
	w2 := NewWatcher()
	defer w2.UnwatchAll()
	if err := w2.Watch(fake, testPID, 0xDEAD0000, search.TypeInt32, tick); err == nil {
		t.Error("watching an unmapped address must fail on the initial read")
	}

	if err := fake.Poke(testPID, base, le32(3)); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	if ev := recv(t, events, "change event"); ev.Cur != "3" {
		t.Errorf("Cur = %q, want %q", ev.Cur, "3")
	}
}

package watch

import (
	"testing"
	"time"

	"memdroid/internal/driver"
	"memdroid/internal/driver/drivertest"
	"memdroid/internal/memory/search"
)

// --- parsing ---

func TestParseAlertCondition(t *testing.T) {
	cases := []struct {
		in   string
		want AlertCondition
	}{
		{"above", AlertAbove},
		{">", AlertAbove},
		{"below", AlertBelow},
		{"<", AlertBelow},
		{"changed", AlertChanged},
		{"!=", AlertChanged},
	}
	for _, c := range cases {
		got, err := ParseAlertCondition(c.in)
		if err != nil {
			t.Errorf("ParseAlertCondition(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseAlertCondition(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseAlertConditionErrors(t *testing.T) {
	for _, in := range []string{"", "ABOVE", "eq", "==", ">=", "unknown"} {
		if got, err := ParseAlertCondition(in); err == nil {
			t.Errorf("ParseAlertCondition(%q) = %v, want an error", in, got)
		}
	}
}

func TestAlertConditionString(t *testing.T) {
	cases := map[AlertCondition]string{
		AlertAbove:          "above",
		AlertBelow:          "below",
		AlertChanged:        "changed",
		AlertCondition(99):  "unknown",
		AlertCondition(-1):  "unknown",
		AlertCondition(123): "unknown",
	}
	for c, want := range cases {
		if got := c.String(); got != want {
			t.Errorf("AlertCondition(%d).String() = %q, want %q", c, got, want)
		}
	}
}

func TestAlertConditionRoundTrip(t *testing.T) {
	for _, c := range []AlertCondition{AlertAbove, AlertBelow, AlertChanged} {
		got, err := ParseAlertCondition(c.String())
		if err != nil {
			t.Errorf("ParseAlertCondition(%q): %v", c.String(), err)
			continue
		}
		if got != c {
			t.Errorf("round trip of %v produced %v", c, got)
		}
	}
}

func TestParseAlertAction(t *testing.T) {
	cases := []struct {
		in   string
		want AlertAction
	}{
		{"", ActionNotify}, // empty defaults to notify-only
		{"notify", ActionNotify},
		{"write", ActionWrite},
	}
	for _, c := range cases {
		got, err := ParseAlertAction(c.in)
		if err != nil {
			t.Errorf("ParseAlertAction(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseAlertAction(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseAlertActionErrors(t *testing.T) {
	for _, in := range []string{"NOTIFY", "Write", "poke", "freeze"} {
		if got, err := ParseAlertAction(in); err == nil {
			t.Errorf("ParseAlertAction(%q) = %v, want an error", in, got)
		}
	}
}

func TestAlertActionString(t *testing.T) {
	if got := ActionNotify.String(); got != "notify" {
		t.Errorf("ActionNotify.String() = %q, want %q", got, "notify")
	}
	if got := ActionWrite.String(); got != "write" {
		t.Errorf("ActionWrite.String() = %q, want %q", got, "write")
	}
	// Any unrecognised value degrades to the safe notify-only behaviour.
	if got := AlertAction(42).String(); got != "notify" {
		t.Errorf("AlertAction(42).String() = %q, want %q", got, "notify")
	}
}

func TestAlertActionRoundTrip(t *testing.T) {
	for _, a := range []AlertAction{ActionNotify, ActionWrite} {
		got, err := ParseAlertAction(a.String())
		if err != nil {
			t.Errorf("ParseAlertAction(%q): %v", a.String(), err)
			continue
		}
		if got != a {
			t.Errorf("round trip of %v produced %v", a, got)
		}
	}
}

// --- WatchWithAlert ---

// peekSpy wraps the fake driver and signals every completed read, so a test can
// wait for a poll to happen instead of sleeping and hoping.
type peekSpy struct {
	*drivertest.Fake
	peeks chan struct{}
}

func newPeekSpy(f *drivertest.Fake) *peekSpy {
	return &peekSpy{Fake: f, peeks: make(chan struct{}, 64)}
}

func (p *peekSpy) Peek(pid int, addr uintptr, size int) ([]byte, error) {
	b, err := p.Fake.Peek(pid, addr, size)
	select {
	case p.peeks <- struct{}{}:
	default:
	}
	return b, err
}

var _ driver.Driver = (*peekSpy)(nil)

// alertSink returns a listener that forwards events on a buffered channel,
// dropping extras so a repeatedly firing alert cannot block the poller.
func alertSink(n int) (func(AlertEvent), <-chan AlertEvent) {
	ch := make(chan AlertEvent, n)
	return func(ev AlertEvent) {
		select {
		case ch <- ev:
		default:
		}
	}, ch
}

func TestWatchWithAlertAboveFires(t *testing.T) {
	fake := newFake()
	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	fn, alerts := alertSink(8)
	aw.OnAlert(fn)

	cfg := AlertConfig{
		Addr:      base,
		Condition: AlertAbove,
		Threshold: le32(100),
		Action:    ActionNotify,
	}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, tick); err != nil {
		t.Fatalf("WatchWithAlert: %v", err)
	}

	// Below the threshold: nothing should fire.
	if err := fake.Poke(testPID, base, le32(50)); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	select {
	case ev := <-alerts:
		t.Fatalf("alert fired below the threshold: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	if err := fake.Poke(testPID, base, le32(500)); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	ev := recv(t, alerts, "an above-threshold alert")
	if ev.Addr != base {
		t.Errorf("Addr = 0x%x, want 0x%x", ev.Addr, base)
	}
	if ev.Condition != "above" {
		t.Errorf("Condition = %q, want %q", ev.Condition, "above")
	}
	if ev.Value != "500" {
		t.Errorf("Value = %q, want %q", ev.Value, "500")
	}
	if ev.Triggered {
		t.Error("Triggered must be false for a notify-only alert")
	}
}

func TestWatchWithAlertBelowFires(t *testing.T) {
	fake := newFake()
	if err := fake.Poke(testPID, base, le32(100)); err != nil {
		t.Fatalf("seed Poke: %v", err)
	}

	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	fn, alerts := alertSink(8)
	aw.OnAlert(fn)

	cfg := AlertConfig{Addr: base, Condition: AlertBelow, Threshold: le32(10)}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, tick); err != nil {
		t.Fatalf("WatchWithAlert: %v", err)
	}

	if err := fake.Poke(testPID, base, le32(-5)); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	ev := recv(t, alerts, "a below-threshold alert")
	if ev.Condition != "below" {
		t.Errorf("Condition = %q, want %q", ev.Condition, "below")
	}
	if ev.Value != "-5" {
		t.Errorf("Value = %q, want %q", ev.Value, "-5")
	}
}

func TestWatchWithAlertChangedFires(t *testing.T) {
	fake := newFake()
	spy := newPeekSpy(fake)
	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	fn, alerts := alertSink(8)
	aw.OnAlert(fn)

	// AlertChanged needs no threshold.
	cfg := AlertConfig{Addr: base, Condition: AlertChanged}
	if err := aw.WatchWithAlert(spy, testPID, search.TypeInt32, cfg, tick); err != nil {
		t.Fatalf("WatchWithAlert: %v", err)
	}

	// AlertChanged has no baseline until the first poll completes, so wait for
	// one before mutating memory instead of racing it.
	recv(t, spy.peeks, "the first poll")

	if err := fake.Poke(testPID, base, le32(77)); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	ev := recv(t, alerts, "a changed alert")
	if ev.Condition != "changed" {
		t.Errorf("Condition = %q, want %q", ev.Condition, "changed")
	}
	if ev.Value != "77" {
		t.Errorf("Value = %q, want %q", ev.Value, "77")
	}
}

func TestWatchWithAlertActionWritePokesValue(t *testing.T) {
	fake := newFake()
	if err := fake.Poke(testPID, base, le32(9999)); err != nil {
		t.Fatalf("seed Poke: %v", err)
	}

	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	fn, alerts := alertSink(8)
	aw.OnAlert(fn)

	cfg := AlertConfig{
		Addr:      base,
		Condition: AlertAbove,
		Threshold: le32(100),
		Action:    ActionWrite,
		WriteVal:  le32(42),
	}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, tick); err != nil {
		t.Fatalf("WatchWithAlert: %v", err)
	}

	ev := recv(t, alerts, "an alert with a write action")
	if !ev.Triggered {
		t.Error("Triggered must be true when an action ran")
	}
	if ev.Value != "9999" {
		t.Errorf("Value = %q, want the pre-write value %q", ev.Value, "9999")
	}

	// The write must have landed in the fake process' memory. It is clamped
	// below the threshold, so no further write can happen.
	if got := search.FormatValue(fake.Bytes(base, 4), search.TypeInt32); got != "42" {
		t.Errorf("memory at 0x%x = %s, want 42", base, got)
	}
}

func TestWatchWithAlertActionWriteWithoutValueDoesNotTrigger(t *testing.T) {
	fake := newFake()
	if err := fake.Poke(testPID, base, le32(9999)); err != nil {
		t.Fatalf("seed Poke: %v", err)
	}

	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	fn, alerts := alertSink(8)
	aw.OnAlert(fn)

	cfg := AlertConfig{
		Addr:      base,
		Condition: AlertAbove,
		Threshold: le32(100),
		Action:    ActionWrite, // no WriteVal
	}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, tick); err != nil {
		t.Fatalf("WatchWithAlert: %v", err)
	}

	ev := recv(t, alerts, "an alert")
	if ev.Triggered {
		t.Error("Triggered must be false when there is no value to write")
	}
	if got := search.FormatValue(fake.Bytes(base, 4), search.TypeInt32); got != "9999" {
		t.Errorf("memory at 0x%x = %s, want it untouched (9999)", base, got)
	}
}

// Regression: both alert listeners must fire, not just the last registered one.
func TestAlertTwoListenersBothFire(t *testing.T) {
	fake := newFake()
	if err := fake.Poke(testPID, base, le32(500)); err != nil {
		t.Fatalf("seed Poke: %v", err)
	}

	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	fnA, alertsA := alertSink(8)
	fnB, alertsB := alertSink(8)
	aw.OnAlert(fnA)
	aw.OnAlert(fnB)

	cfg := AlertConfig{Addr: base, Condition: AlertAbove, Threshold: le32(100)}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, tick); err != nil {
		t.Fatalf("WatchWithAlert: %v", err)
	}

	a := recv(t, alertsA, "the first listener's alert")
	b := recv(t, alertsB, "the second listener's alert")
	if a.Value != "500" || b.Value != "500" {
		t.Errorf("listener values = %q and %q, want both %q", a.Value, b.Value, "500")
	}
}

func TestAlertOnAlertRemove(t *testing.T) {
	fake := newFake()
	if err := fake.Poke(testPID, base, le32(500)); err != nil {
		t.Fatalf("seed Poke: %v", err)
	}

	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	fnA, alertsA := alertSink(8)
	fnB, alertsB := alertSink(8)
	removeA := aw.OnAlert(fnA)
	aw.OnAlert(fnB)
	removeA()

	cfg := AlertConfig{Addr: base, Condition: AlertAbove, Threshold: le32(100)}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, tick); err != nil {
		t.Fatalf("WatchWithAlert: %v", err)
	}

	recv(t, alertsB, "the surviving listener's alert")
	select {
	case ev := <-alertsA:
		t.Errorf("the removed listener still fired: %+v", ev)
	default:
	}
}

func TestWatchWithAlertRejectsShortThreshold(t *testing.T) {
	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	for _, cond := range []AlertCondition{AlertAbove, AlertBelow} {
		cfg := AlertConfig{Addr: base, Condition: cond, Threshold: []byte{1, 2}}
		if err := aw.WatchWithAlert(newFake(), testPID, search.TypeInt32, cfg, tick); err == nil {
			t.Errorf("%v with a 2-byte threshold must fail for int32", cond)
		}
	}

	// A nil threshold is equally short.
	cfg := AlertConfig{Addr: base, Condition: AlertAbove}
	if err := aw.WatchWithAlert(newFake(), testPID, search.TypeInt32, cfg, tick); err == nil {
		t.Error("a nil threshold must fail for a comparison condition")
	}
	if got := len(aw.List()); got != 0 {
		t.Errorf("rejected configs must not register: %v", aw.List())
	}
}

func TestWatchWithAlertRejectsVariableWidthType(t *testing.T) {
	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	cfg := AlertConfig{Addr: base, Condition: AlertChanged}
	if err := aw.WatchWithAlert(newFake(), testPID, search.TypeBytes, cfg, tick); err == nil {
		t.Fatal("WatchWithAlert(TypeBytes) must fail: the type has no fixed size")
	}
	if len(aw.List()) != 0 {
		t.Errorf("a rejected alert must not register anything: %v", aw.List())
	}
}

func TestWatchWithAlertRejectsDuplicateAddress(t *testing.T) {
	fake := newFake()
	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	cfg := AlertConfig{Addr: base, Condition: AlertChanged}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, time.Second); err != nil {
		t.Fatalf("first WatchWithAlert: %v", err)
	}
	if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, time.Second); err == nil {
		t.Fatal("alerting on the same address twice must fail")
	}
}

func TestRemoveAlertAndList(t *testing.T) {
	fake := newFake()
	aw := NewAlertWatcher()
	defer aw.RemoveAll()

	if err := aw.RemoveAlert(base); err == nil {
		t.Error("RemoveAlert of an unknown address must fail")
	}

	for _, off := range []uintptr{0x10, 0x00, 0x08} {
		cfg := AlertConfig{Addr: base + off, Condition: AlertChanged}
		if err := aw.WatchWithAlert(fake, testPID, search.TypeInt32, cfg, time.Second); err != nil {
			t.Fatalf("WatchWithAlert(0x%x): %v", base+off, err)
		}
	}

	want := []uintptr{base, base + 0x08, base + 0x10}
	got := aw.List()
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List() = %v, want %v (sorted)", got, want)
		}
	}

	if err := aw.RemoveAlert(base + 0x08); err != nil {
		t.Fatalf("RemoveAlert: %v", err)
	}
	if got := len(aw.List()); got != 2 {
		t.Errorf("List() has %d entries, want 2", got)
	}

	aw.RemoveAll()
	if got := aw.List(); len(got) != 0 {
		t.Errorf("List() = %v, want empty after RemoveAll", got)
	}
}

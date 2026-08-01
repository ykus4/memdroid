package app

import (
	"encoding/binary"
	"sync"
	"testing"

	"memdroid/internal/driver"
	"memdroid/internal/driver/drivertest"
	"memdroid/internal/memory/search"
)

func newTestState() (*State, *drivertest.Fake) {
	data := make([]byte, 64)
	fake := drivertest.New(drivertest.Region{Start: 0x1000, Name: "[heap]", Data: data})
	return NewState(fake), fake
}

func TestNewStateDefaults(t *testing.T) {
	s, fake := newTestState()

	if got := s.GetDriver(); got != driver.Driver(fake) {
		t.Errorf("GetDriver() = %v, want the fake driver", got)
	}
	if got := s.GetPID(); got != 0 {
		t.Errorf("GetPID() = %d, want 0", got)
	}
	if got := s.GetValueType(); got != search.TypeInt32 {
		t.Errorf("GetValueType() = %v, want int32", got)
	}
	if s.GetSession() != nil {
		t.Error("GetSession() should be nil before any search")
	}
	if s.GetBookmarks() == nil {
		t.Error("GetBookmarks() must not be nil")
	}
	if s.Freezer == nil || s.UndoStack == nil || s.Watcher == nil || s.AlertWatcher == nil {
		t.Error("NewState must initialise every subsystem")
	}
	if got := s.ListAttached(); len(got) != 0 {
		t.Errorf("ListAttached() = %+v, want empty", got)
	}
}

func TestSetGetPID(t *testing.T) {
	s, _ := newTestState()
	s.SetPID(4242)
	if got := s.GetPID(); got != 4242 {
		t.Errorf("GetPID() = %d, want 4242", got)
	}
}

// --- value type / session interplay ---

func TestValueTypeWithoutSession(t *testing.T) {
	s, _ := newTestState()

	if got := s.GetValueType(); got != search.TypeInt32 {
		t.Fatalf("default GetValueType() = %v, want int32", got)
	}
	s.SetValueType(search.TypeFloat64)
	if got := s.GetValueType(); got != search.TypeFloat64 {
		t.Errorf("GetValueType() = %v, want float64", got)
	}
}

// Regression: SetValueType must reach the live session. Previously the state
// kept its own copy while the session searched at the old width, so a scan and
// the formatting of its results could disagree about the byte size.
func TestSetValueTypePropagatesIntoLiveSession(t *testing.T) {
	s, _ := newTestState()
	s.SetPID(1)
	sess := s.EnsureSession()

	if got := sess.Type(); got != search.TypeInt32 {
		t.Fatalf("new session type = %v, want int32", got)
	}

	s.SetValueType(search.TypeInt64)

	if got := sess.Type(); got != search.TypeInt64 {
		t.Errorf("session type = %v, want int64 — SetValueType did not propagate", got)
	}
	if got := s.GetValueType(); got != search.TypeInt64 {
		t.Errorf("GetValueType() = %v, want int64", got)
	}
}

// Regression: changing the type must discard candidates recorded at the old
// byte width.
func TestSetValueTypeDiscardsCandidates(t *testing.T) {
	s, _ := newTestState()
	s.SetPID(1)
	sess := s.EnsureSession()
	sess.SetCandidates(map[uintptr][]byte{0x1000: {1, 0, 0, 0}, 0x1004: {2, 0, 0, 0}})

	if !sess.HasCandidates() {
		t.Fatal("session should have candidates before the type change")
	}

	s.SetValueType(search.TypeFloat64)

	if sess.HasCandidates() {
		t.Error("candidates recorded at the old width must be discarded on a type change")
	}
	if got := sess.CandidateCount(); got != 0 {
		t.Errorf("CandidateCount() = %d, want 0", got)
	}
}

func TestSetValueTypeToSameTypeKeepsCandidates(t *testing.T) {
	s, _ := newTestState()
	sess := s.EnsureSession()
	sess.SetCandidates(map[uintptr][]byte{0x1000: {1, 0, 0, 0}})

	s.SetValueType(search.TypeInt32) // already int32

	if !sess.HasCandidates() {
		t.Error("a no-op type change must not discard candidates")
	}
}

// Once a session exists it owns the type, so a type set directly on the session
// is what GetValueType reports.
func TestSessionOwnsValueType(t *testing.T) {
	s, _ := newTestState()
	sess := s.EnsureSession()

	sess.SetType(search.TypeUint64)

	if got := s.GetValueType(); got != search.TypeUint64 {
		t.Errorf("GetValueType() = %v, want uint64 (session owns the type)", got)
	}
}

func TestSetSessionSyncsFallbackType(t *testing.T) {
	s, _ := newTestState()
	sess := search.NewSession(7, search.TypeFloat32, s.GetDriver())

	s.SetSession(sess)
	if got := s.GetSession(); got != sess {
		t.Fatalf("GetSession() = %v, want the session just set", got)
	}
	if got := s.GetValueType(); got != search.TypeFloat32 {
		t.Errorf("GetValueType() = %v, want float32", got)
	}

	// Clearing the session falls back to the synced type, not the original one.
	s.SetSession(nil)
	if s.GetSession() != nil {
		t.Error("SetSession(nil) must clear the session")
	}
	if got := s.GetValueType(); got != search.TypeFloat32 {
		t.Errorf("after clearing, GetValueType() = %v, want float32", got)
	}
}

func TestNewSessionReplacesAndUsesCurrentType(t *testing.T) {
	s, _ := newTestState()
	first := s.EnsureSession()
	s.SetValueType(search.TypeUint32)

	second := s.NewSession(99)

	if second == first {
		t.Error("NewSession must replace the existing session")
	}
	if got := second.PID(); got != 99 {
		t.Errorf("session PID = %d, want 99", got)
	}
	if got := second.Type(); got != search.TypeUint32 {
		t.Errorf("session type = %v, want uint32", got)
	}
	if got := s.GetSession(); got != second {
		t.Error("NewSession must install the new session on the state")
	}
}

func TestEnsureSessionIsIdempotent(t *testing.T) {
	s, _ := newTestState()
	s.SetPID(31337)

	first := s.EnsureSession()
	if first == nil {
		t.Fatal("EnsureSession returned nil")
	}
	if got := first.PID(); got != 31337 {
		t.Errorf("session PID = %d, want 31337", got)
	}

	second := s.EnsureSession()
	if second != first {
		t.Error("EnsureSession must reuse the existing session")
	}
}

// --- multi-attach ---

func TestAttachedListIsSortedByPID(t *testing.T) {
	s, _ := newTestState()
	s.AddAttached(300, "c")
	s.AddAttached(100, "a")
	s.AddAttached(200, "b")

	want := []AttachedProcess{{100, "a"}, {200, "b"}, {300, "c"}}
	got := s.ListAttached()
	if len(got) != len(want) {
		t.Fatalf("ListAttached() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The order must be stable across repeated calls; the CLI numbers this list.
	for i := 0; i < 20; i++ {
		again := s.ListAttached()
		for j := range want {
			if again[j] != want[j] {
				t.Fatalf("call %d entry %d = %+v, want %+v", i, j, again[j], want[j])
			}
		}
	}
}

func TestAddAttachedOverwritesName(t *testing.T) {
	s, _ := newTestState()
	s.AddAttached(1, "old")
	s.AddAttached(1, "new")

	got := s.ListAttached()
	if len(got) != 1 || got[0].Name != "new" {
		t.Errorf("ListAttached() = %+v, want a single entry named %q", got, "new")
	}
}

func TestRemoveAttached(t *testing.T) {
	s, _ := newTestState()
	s.AddAttached(10, "a")
	s.AddAttached(20, "b")

	s.RemoveAttached(10)
	got := s.ListAttached()
	if len(got) != 1 || got[0].PID != 20 {
		t.Errorf("ListAttached() = %+v, want only pid 20", got)
	}

	// Removing an unknown pid is a no-op.
	s.RemoveAttached(999)
	if len(s.ListAttached()) != 1 {
		t.Errorf("removing an unknown pid changed the list: %+v", s.ListAttached())
	}
}

// --- detach ---

func TestDetachPromotesNextProcess(t *testing.T) {
	s, fake := newTestState()
	if err := fake.Attach(100); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := fake.Attach(200); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	s.AddAttached(100, "first")
	s.AddAttached(200, "second")
	s.SetPID(100)
	s.NewSession(100)

	detached, next := s.Detach()

	if detached != 100 {
		t.Errorf("detached = %d, want 100", detached)
	}
	if next != (AttachedProcess{PID: 200, Name: "second"}) {
		t.Errorf("next = %+v, want {200 second}", next)
	}
	if got := s.GetPID(); got != 200 {
		t.Errorf("GetPID() = %d, want 200", got)
	}
	sess := s.GetSession()
	if sess == nil {
		t.Fatal("a fresh session must be created for the promoted process")
	}
	if got := sess.PID(); got != 200 {
		t.Errorf("session PID = %d, want 200", got)
	}
	if fake.Attached(100) {
		t.Error("the driver did not see the Detach for pid 100")
	}
	if !fake.Attached(200) {
		t.Error("pid 200 must stay attached at the driver level")
	}
	if got := s.ListAttached(); len(got) != 1 || got[0].PID != 200 {
		t.Errorf("ListAttached() = %+v, want only pid 200", got)
	}
}

func TestDetachLastProcessClearsState(t *testing.T) {
	s, fake := newTestState()
	if err := fake.Attach(100); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	s.AddAttached(100, "only")
	s.SetPID(100)
	s.NewSession(100)

	detached, next := s.Detach()

	if detached != 100 {
		t.Errorf("detached = %d, want 100", detached)
	}
	if next != (AttachedProcess{}) {
		t.Errorf("next = %+v, want the zero value", next)
	}
	if got := s.GetPID(); got != 0 {
		t.Errorf("GetPID() = %d, want 0", got)
	}
	if s.GetSession() != nil {
		t.Error("GetSession() must be nil after detaching the last process")
	}
	if fake.Attached(100) {
		t.Error("the driver did not see the Detach")
	}
	if got := s.ListAttached(); len(got) != 0 {
		t.Errorf("ListAttached() = %+v, want empty", got)
	}
}

func TestDetachWithNothingAttached(t *testing.T) {
	s, _ := newTestState()

	detached, next := s.Detach()

	if detached != 0 {
		t.Errorf("detached = %d, want 0", detached)
	}
	if next != (AttachedProcess{}) {
		t.Errorf("next = %+v, want the zero value", next)
	}
	if s.GetSession() != nil {
		t.Error("Detach with no PID must not create a session")
	}
}

func TestDetachStopsBackgroundWork(t *testing.T) {
	s, fake := newTestState()
	s.AddAttached(1, "proc")
	s.SetPID(1)

	// A freeze and a watch on the same address exercise all three teardown paths.
	if err := s.Freezer.Freeze(fake, 1, 0x1000, []byte{1, 0, 0, 0}); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if got := len(s.Freezer.List()); got != 1 {
		t.Fatalf("frozen count = %d, want 1", got)
	}

	s.Detach()

	if got := s.Freezer.List(); len(got) != 0 {
		t.Errorf("Freezer.List() = %+v, want empty after Detach", got)
	}
	if got := s.Watcher.List(); len(got) != 0 {
		t.Errorf("Watcher.List() = %+v, want empty after Detach", got)
	}
	if got := s.AlertWatcher.List(); len(got) != 0 {
		t.Errorf("AlertWatcher.List() = %+v, want empty after Detach", got)
	}
}

// --- snapshots ---

func TestStateSnapshotRoundTrip(t *testing.T) {
	s, _ := newTestState()

	if got := s.GetSnapshot(0x1000); got != nil {
		t.Errorf("GetSnapshot of an unknown address = %v, want nil", got)
	}

	s.SetSnapshot(0x1000, []byte{1, 2, 3, 4})
	got := s.GetSnapshot(0x1000)
	if string(got) != string([]byte{1, 2, 3, 4}) {
		t.Errorf("GetSnapshot() = %v, want [1 2 3 4]", got)
	}
}

func TestStateSnapshotCopiesInput(t *testing.T) {
	s, _ := newTestState()

	src := []byte{1, 2, 3, 4}
	s.SetSnapshot(0x2000, src)
	src[0] = 0xFF

	if got := s.GetSnapshot(0x2000); got[0] != 1 {
		t.Errorf("stored snapshot aliases the caller's slice: got %v", got)
	}
}

func TestSnapshotStoreOverwriteReplaces(t *testing.T) {
	st := newSnapshotStore(1024)

	st.set(0x10, []byte{1, 2, 3, 4, 5, 6})
	st.set(0x10, []byte{9, 9})

	got := st.get(0x10)
	if len(got) != 2 || got[0] != 9 {
		t.Errorf("get(0x10) = %v, want [9 9]", got)
	}
	if len(st.data) != 1 {
		t.Errorf("data has %d entries, want 1", len(st.data))
	}
	if len(st.order) != 1 {
		t.Errorf("order has %d entries, want 1 (overwrite must not accumulate)", len(st.order))
	}
	if st.bytes != 2 {
		t.Errorf("bytes = %d, want 2 (the old size must be subtracted)", st.bytes)
	}
}

func TestSnapshotStoreEvictsOldestOverCap(t *testing.T) {
	// Small cap so the test stays cheap; production uses maxSnapshotBytes.
	st := newSnapshotStore(10)

	st.set(0x1, make([]byte, 6))
	st.set(0x2, make([]byte, 6)) // 12 > 10 -> evict 0x1

	if got := st.get(0x1); got != nil {
		t.Errorf("get(0x1) = %v, want nil (evicted)", got)
	}
	if got := st.get(0x2); len(got) != 6 {
		t.Errorf("get(0x2) has %d bytes, want 6", len(got))
	}
	if st.bytes != 6 {
		t.Errorf("bytes = %d, want 6", st.bytes)
	}

	// Insertion order is what decides eviction: 0x2 is now the oldest.
	st.set(0x3, make([]byte, 6))
	if got := st.get(0x2); got != nil {
		t.Errorf("get(0x2) = %v, want nil (evicted)", got)
	}
	if got := st.get(0x3); len(got) != 6 {
		t.Errorf("get(0x3) has %d bytes, want 6", len(got))
	}
}

func TestSnapshotStoreKeepsSingleOversizedEntry(t *testing.T) {
	st := newSnapshotStore(4)

	st.set(0x1, make([]byte, 100))

	if got := st.get(0x1); len(got) != 100 {
		t.Errorf("get(0x1) has %d bytes, want 100 — the only entry must survive", len(got))
	}
}

func TestSnapshotStoreReinsertRefreshesAge(t *testing.T) {
	st := newSnapshotStore(10)

	st.set(0x1, make([]byte, 4))
	st.set(0x2, make([]byte, 4))
	st.set(0x1, make([]byte, 4)) // 0x1 becomes the newest
	st.set(0x3, make([]byte, 4)) // 12 > 10 -> evict the oldest, which is 0x2

	if got := st.get(0x2); got != nil {
		t.Errorf("get(0x2) = %v, want nil (oldest after 0x1 was refreshed)", got)
	}
	if st.get(0x1) == nil || st.get(0x3) == nil {
		t.Error("0x1 and 0x3 must both be retained")
	}
}

func TestSnapshotStoreEmptyValue(t *testing.T) {
	st := newSnapshotStore(10)
	st.set(0x1, nil)
	if got := st.get(0x1); len(got) != 0 {
		t.Errorf("get(0x1) = %v, want empty", got)
	}
	if st.bytes != 0 {
		t.Errorf("bytes = %d, want 0", st.bytes)
	}
}

// --- concurrency ---

// TestStateConcurrentAccess exercises every mutating and reading path from many
// goroutines so `go test -race` catches a dropped lock.
func TestStateConcurrentAccess(t *testing.T) {
	s, _ := newTestState()

	const workers = 8
	const iterations = 200

	types := []search.ValueType{search.TypeInt32, search.TypeInt64, search.TypeFloat32, search.TypeUint64}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			buf := make([]byte, 8)
			for i := 0; i < iterations; i++ {
				pid := w*1000 + i%7 + 1

				s.SetPID(pid)
				_ = s.GetPID()

				s.SetValueType(types[i%len(types)])
				_ = s.GetValueType()

				s.AddAttached(pid, "proc")
				_ = s.ListAttached()
				s.RemoveAttached(pid)

				switch i % 3 {
				case 0:
					_ = s.EnsureSession()
				case 1:
					_ = s.NewSession(pid)
				default:
					if sess := s.GetSession(); sess != nil {
						_ = sess.CandidateCount()
					}
				}

				binary.LittleEndian.PutUint64(buf, uint64(i))
				s.SetSnapshot(uintptr(w), buf)
				_ = s.GetSnapshot(uintptr(w))

				s.GetBookmarks().Add(uintptr(i), "b", search.TypeInt32)
				_ = s.GetBookmarks().Len()

				_ = s.GetDriver()
			}
		}(w)
	}
	wg.Wait()

	if got := s.GetBookmarks().Len(); got != workers*iterations {
		t.Errorf("bookmark count = %d, want %d", got, workers*iterations)
	}
}

func TestStateConcurrentDetach(t *testing.T) {
	s, _ := newTestState()
	for pid := 1; pid <= 50; pid++ {
		s.AddAttached(pid, "proc")
	}
	s.SetPID(1)

	// Detach is a sequence of individually locked steps, so two callers can
	// race for the same victim and one of them becomes a no-op. Each worker
	// therefore keeps going until the state reports nothing is attached; the
	// point of the test is that -race stays quiet and the drain terminates.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500 && s.GetPID() != 0; j++ {
				s.Detach()
				_ = s.ListAttached()
			}
		}()
	}
	wg.Wait()

	if got := s.ListAttached(); len(got) != 0 {
		t.Errorf("ListAttached() = %+v, want empty after detaching everything", got)
	}
	if got := s.GetPID(); got != 0 {
		t.Errorf("GetPID() = %d, want 0", got)
	}
}

// Guard against the bookmark accessor handing out something other than the
// state's own list.
func TestGetBookmarksReturnsSharedList(t *testing.T) {
	s, _ := newTestState()
	s.GetBookmarks().Add(0x1000, "hp", search.TypeInt32)

	if got := s.GetBookmarks().Len(); got != 1 {
		t.Errorf("Len() = %d, want 1", got)
	}
}

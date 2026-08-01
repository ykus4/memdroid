package app

import (
	"slices"
	"sync"

	"memdroid/internal/driver"
	"memdroid/internal/memory/modify"
	"memdroid/internal/memory/search"
	"memdroid/internal/memory/store"
	"memdroid/internal/memory/watch"
)

// maxSnapshotBytes bounds the total size of retained region snapshots. Each
// /api/snapshot/take keeps its buffer for a later diff; without a cap a long
// session could pin gigabytes.
const maxSnapshotBytes = 64 << 20

// AttachedProcess holds info about an attached process.
type AttachedProcess struct {
	PID  int
	Name string
}

// State is the single shared mutable state of the tool.
// Both the CLI and HTTP server read/write through this struct.
type State struct {
	mu        sync.RWMutex
	drv       driver.Driver
	pid       int
	valueType search.ValueType
	session   *search.Session
	bookmarks *store.BookmarkList
	snapshots *snapshotStore
	attached  map[int]string // pid -> name, all currently attached processes

	Freezer      *modify.Freezer
	UndoStack    *modify.UndoStack
	Watcher      *watch.Watcher
	AlertWatcher *watch.AlertWatcher
}

func NewState(drv driver.Driver) *State {
	return &State{
		drv:          drv,
		valueType:    search.TypeInt32,
		bookmarks:    store.NewBookmarkList(),
		snapshots:    newSnapshotStore(maxSnapshotBytes),
		attached:     make(map[int]string),
		Freezer:      modify.NewFreezer(),
		UndoStack:    modify.NewUndoStack(),
		Watcher:      watch.NewWatcher(),
		AlertWatcher: watch.NewAlertWatcher(),
	}
}

// --- Driver ---

func (s *State) GetDriver() driver.Driver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.drv
}

// --- PID ---

func (s *State) GetPID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pid
}

func (s *State) SetPID(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pid = pid
}

// --- ValueType ---

// GetValueType returns the active value type. Once a search session exists the
// session owns the type, so that a scan and the formatting of its results can
// never disagree about the byte width.
func (s *State) GetValueType() search.ValueType {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session != nil {
		return s.session.Type()
	}
	return s.valueType
}

// SetValueType changes the active value type, propagating it to the live
// session. Changing the type discards existing candidates, since they were
// recorded at a different width.
func (s *State) SetValueType(vt search.ValueType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.valueType = vt
	if s.session != nil {
		s.session.SetType(vt)
	}
}

// --- Session ---

func (s *State) GetSession() *search.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.session
}

func (s *State) SetSession(sess *search.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = sess
	if sess != nil {
		// The session becomes the owner of the type; keep the fallback in sync
		// so it is correct again once the session is cleared.
		s.valueType = sess.Type()
	}
}

// NewSession replaces the session with a fresh one for pid at the current
// value type.
func (s *State) NewSession(pid int) *search.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = search.NewSession(pid, s.valueType, s.drv)
	return s.session
}

// EnsureSession returns the active session, creating one for the current PID
// and value type if none exists.
func (s *State) EnsureSession() *search.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		s.session = search.NewSession(s.pid, s.valueType, s.drv)
	}
	return s.session
}

// --- Bookmarks ---

func (s *State) GetBookmarks() *store.BookmarkList {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bookmarks
}

// --- Multi-attach ---

func (s *State) AddAttached(pid int, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached[pid] = name
}

func (s *State) RemoveAttached(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attached, pid)
}

// ListAttached returns every attached process, ordered by PID so the CLI's
// numbered selection list is stable between renders.
func (s *State) ListAttached() []AttachedProcess {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedAttachedLocked()
}

// Detach stops all background activity for the active process, detaches it, and
// promotes the next attached process (if any) to active. It returns the PID
// that was detached and the process now active, or 0 when none remains.
//
// The CLI and the HTTP API both need exactly this sequence; keeping it here
// stops the two paths from drifting apart.
func (s *State) Detach() (detached int, next AttachedProcess) {
	// Claim the PID under the lock and drop it from the attached set in the
	// same critical section. Doing this as separate locked steps would let two
	// concurrent callers claim the same PID and both promote a successor.
	s.mu.Lock()
	pid := s.pid
	if pid == 0 {
		s.mu.Unlock()
		return 0, AttachedProcess{}
	}
	delete(s.attached, pid)
	drv := s.drv
	s.mu.Unlock()

	s.Freezer.UnfreezeAll()
	s.Watcher.UnwatchAll()
	s.AlertWatcher.RemoveAll()
	drv.Detach(pid)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Another Detach may have run in between; only promote if we are still the
	// process being stood down.
	if s.pid != pid {
		return pid, AttachedProcess{PID: s.pid, Name: s.attached[s.pid]}
	}
	for _, p := range s.sortedAttachedLocked() {
		s.pid = p.PID
		s.session = search.NewSession(p.PID, s.valueType, s.drv)
		return pid, p
	}
	s.pid = 0
	s.session = nil
	return pid, AttachedProcess{}
}

// --- Snapshots ---

func (s *State) SetSnapshot(addr uintptr, data []byte) {
	s.snapshots.set(addr, data)
}

func (s *State) GetSnapshot(addr uintptr) []byte {
	return s.snapshots.get(addr)
}

// snapshotStore keeps region snapshots for later diffing, evicting the oldest
// once the retained bytes exceed maxBytes.
type snapshotStore struct {
	mu       sync.Mutex
	maxBytes int
	bytes    int
	data     map[uintptr][]byte
	order    []uintptr // insertion order, oldest first
}

func newSnapshotStore(maxBytes int) *snapshotStore {
	return &snapshotStore{maxBytes: maxBytes, data: make(map[uintptr][]byte)}
}

func (st *snapshotStore) set(addr uintptr, data []byte) {
	cp := slices.Clone(data)

	st.mu.Lock()
	defer st.mu.Unlock()

	if old, ok := st.data[addr]; ok {
		st.bytes -= len(old)
		st.order = slices.DeleteFunc(st.order, func(a uintptr) bool { return a == addr })
	}
	st.data[addr] = cp
	st.order = append(st.order, addr)
	st.bytes += len(cp)

	for st.bytes > st.maxBytes && len(st.order) > 1 {
		oldest := st.order[0]
		st.order = st.order[1:]
		st.bytes -= len(st.data[oldest])
		delete(st.data, oldest)
	}
}

func (st *snapshotStore) get(addr uintptr) []byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.data[addr]
}

// sortedAttachedLocked returns the attached processes ordered by PID. The
// caller must hold s.mu.
func (s *State) sortedAttachedLocked() []AttachedProcess {
	procs := make([]AttachedProcess, 0, len(s.attached))
	for pid, name := range s.attached {
		procs = append(procs, AttachedProcess{PID: pid, Name: name})
	}
	slices.SortFunc(procs, func(a, b AttachedProcess) int { return a.PID - b.PID })
	return procs
}

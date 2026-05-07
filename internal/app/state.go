package app

import (
	"sync"

	"memodroid/internal/driver"
	"memodroid/internal/memory/modify"
	"memodroid/internal/memory/search"
	"memodroid/internal/memory/store"
	"memodroid/internal/memory/watch"
)

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
	snapshots map[uintptr][]byte
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
		snapshots:    make(map[uintptr][]byte),
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

func (s *State) GetValueType() search.ValueType {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.valueType
}

func (s *State) SetValueType(vt search.ValueType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.valueType = vt
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
}

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

func (s *State) ListAttached() []AttachedProcess {
	s.mu.RLock()
	defer s.mu.RUnlock()
	procs := make([]AttachedProcess, 0, len(s.attached))
	for pid, name := range s.attached {
		procs = append(procs, AttachedProcess{PID: pid, Name: name})
	}
	return procs
}

// --- Snapshots ---

func (s *State) SetSnapshot(addr uintptr, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	s.snapshots[addr] = cp
}

func (s *State) GetSnapshot(addr uintptr) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshots[addr]
}

// WithLock runs f while holding the write lock. Use for multi-step mutations.
func (s *State) WithLock(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

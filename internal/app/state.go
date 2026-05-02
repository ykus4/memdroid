package app

import (
	"sync"

	"memodroid/internal/driver"
	"memodroid/internal/memory/modify"
	"memodroid/internal/memory/search"
	"memodroid/internal/memory/store"
	"memodroid/internal/memory/watch"
)

// State is the single shared mutable state of the tool.
// Both the CLI and HTTP server read/write through this struct.
type State struct {
	mu        sync.RWMutex
	drv       driver.Driver
	pid       int
	valueType search.ValueType
	session   *search.Session
	bookmarks *store.BookmarkList

	Freezer   *modify.Freezer
	UndoStack *modify.UndoStack
	Watcher   *watch.Watcher
}

func NewState(drv driver.Driver) *State {
	return &State{
		drv:       drv,
		valueType: search.TypeInt32,
		bookmarks: store.NewBookmarkList(),
		Freezer:   modify.NewFreezer(),
		UndoStack: modify.NewUndoStack(),
		Watcher:   watch.NewWatcher(),
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

// WithLock runs f while holding the write lock. Use for multi-step mutations.
func (s *State) WithLock(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

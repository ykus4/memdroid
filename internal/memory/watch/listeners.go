package watch

import "sync"

// listeners[T] is a concurrency-safe fan-out registry.
//
// Watch events have more than one consumer — the CLI prints them and the web UI
// pushes them over a WebSocket — and both are wired up from different
// goroutines at startup. A single assignable callback field would let whichever
// side registered last silently win, and would be raced on by the polling
// goroutines that invoke it.
type listeners[T any] struct {
	mu   sync.RWMutex
	next int
	fns  map[int]func(T)
}

// add registers fn and returns a function that unregisters it.
func (l *listeners[T]) add(fn func(T)) (remove func()) {
	if fn == nil {
		return func() {}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fns == nil {
		l.fns = make(map[int]func(T))
	}
	id := l.next
	l.next++
	l.fns[id] = fn

	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.fns, id)
	}
}

// emit delivers ev to every registered listener. Listeners are invoked outside
// the lock so a slow one cannot block registration.
func (l *listeners[T]) emit(ev T) {
	l.mu.RLock()
	fns := make([]func(T), 0, len(l.fns))
	for _, fn := range l.fns {
		fns = append(fns, fn)
	}
	l.mu.RUnlock()

	for _, fn := range fns {
		fn(ev)
	}
}

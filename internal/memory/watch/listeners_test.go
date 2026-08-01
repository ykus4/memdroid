package watch

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestListenersAddAndEmit(t *testing.T) {
	var l listeners[int]

	var got []int
	remove := l.add(func(v int) { got = append(got, v) })
	if remove == nil {
		t.Fatal("add returned a nil remove func")
	}

	l.emit(1)
	l.emit(2)

	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("got %v, want [1 2]", got)
	}
}

func TestListenersRemove(t *testing.T) {
	var l listeners[int]

	count := 0
	remove := l.add(func(int) { count++ })

	l.emit(0)
	remove()
	l.emit(0)

	if count != 1 {
		t.Errorf("listener fired %d times, want 1 (remove did not unregister)", count)
	}

	// Removing twice must be safe.
	remove()
	l.emit(0)
	if count != 1 {
		t.Errorf("listener fired %d times after a double remove, want 1", count)
	}
}

// Regression: multiple consumers (CLI + WebSocket) subscribe at once. A single
// assignable callback field would let the last registration silently win.
func TestListenersFanOutToAll(t *testing.T) {
	var l listeners[string]

	const n = 5
	hits := make([]int, n)
	for i := 0; i < n; i++ {
		l.add(func(string) { hits[i]++ })
	}

	l.emit("ev")

	for i, h := range hits {
		if h != 1 {
			t.Errorf("listener %d fired %d times, want 1", i, h)
		}
	}
}

func TestListenersRemoveOneLeavesOthers(t *testing.T) {
	var l listeners[int]

	a, b, c := 0, 0, 0
	l.add(func(int) { a++ })
	removeB := l.add(func(int) { b++ })
	l.add(func(int) { c++ })

	removeB()
	l.emit(0)

	if a != 1 || c != 1 {
		t.Errorf("surviving listeners fired a=%d c=%d, want 1 and 1", a, c)
	}
	if b != 0 {
		t.Errorf("removed listener fired %d times, want 0", b)
	}
}

func TestListenersNilFnIsSafe(t *testing.T) {
	var l listeners[int]

	remove := l.add(nil)
	if remove == nil {
		t.Fatal("add(nil) returned a nil remove func")
	}
	remove() // must not panic

	fired := 0
	l.add(func(int) { fired++ })
	l.add(nil)
	l.emit(0)

	if fired != 1 {
		t.Errorf("real listener fired %d times, want 1", fired)
	}
}

func TestListenersEmitWithNoListeners(t *testing.T) {
	var l listeners[int]
	l.emit(42) // must not panic on the nil map

	remove := l.add(func(int) {})
	remove()
	l.emit(42) // and must not panic once emptied again
}

func TestListenersConcurrentAddEmitRemove(t *testing.T) {
	var l listeners[int]

	var fired atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				remove := l.add(func(int) { fired.Add(1) })
				l.emit(j)
				remove()
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.emit(j)
			}
		}()
	}
	wg.Wait()

	// Exact counts are timing dependent; each emit-after-add must fire at least
	// its own listener.
	if fired.Load() < 800 {
		t.Errorf("listeners fired %d times, want at least 800", fired.Load())
	}
}

// A slow listener must not block registration, since emit runs outside the lock.
func TestListenersEmitRunsOutsideLock(t *testing.T) {
	var l listeners[int]

	entered := make(chan struct{})
	release := make(chan struct{})
	l.add(func(int) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
	})

	done := make(chan struct{})
	go func() {
		l.emit(1)
		close(done)
	}()

	<-entered
	// Registration must succeed while the slow listener is still running.
	l.add(func(int) {})
	close(release)
	<-done
}

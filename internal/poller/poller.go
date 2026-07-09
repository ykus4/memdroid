// Package poller provides a small keyed goroutine manager shared by the
// freeze/watch/alert subsystems. Each of those used to carry its own copy of
// "map[uintptr]*entry + stop channel + List/Stop/StopAll" boilerplate; they now
// delegate that lifecycle management to a single Pool.
package poller

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Pool manages a set of background tasks keyed by address. Each task runs on its
// own goroutine and stops when its stop channel is closed.
type Pool struct {
	mu    sync.Mutex
	tasks map[uintptr]chan struct{}
}

// New returns an empty Pool.
func New() *Pool {
	return &Pool{tasks: make(map[uintptr]chan struct{})}
}

// Start launches run on a new goroutine associated with key. run must return
// when its stop channel is closed. Start returns an error if key is already
// running.
func (p *Pool) Start(key uintptr, run func(stop <-chan struct{})) error {
	p.mu.Lock()
	if _, exists := p.tasks[key]; exists {
		p.mu.Unlock()
		return fmt.Errorf("0x%x is already active", key)
	}
	stop := make(chan struct{})
	p.tasks[key] = stop
	p.mu.Unlock()

	go run(stop)
	return nil
}

// Stop stops the task for key. Returns an error if key is not running.
func (p *Pool) Stop(key uintptr) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	stop, ok := p.tasks[key]
	if !ok {
		return fmt.Errorf("0x%x is not active", key)
	}
	close(stop)
	delete(p.tasks, key)
	return nil
}

// StopAll stops every running task.
func (p *Pool) StopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, stop := range p.tasks {
		close(stop)
		delete(p.tasks, key)
	}
}

// Has reports whether key is currently running.
func (p *Pool) Has(key uintptr) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.tasks[key]
	return ok
}

// List returns all active keys, sorted ascending.
func (p *Pool) List() []uintptr {
	p.mu.Lock()
	defer p.mu.Unlock()
	keys := make([]uintptr, 0, len(p.tasks))
	for key := range p.tasks {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// EveryTick invokes onTick on each interval until stop is closed. It is a helper
// for building run functions passed to Start.
func EveryTick(interval time.Duration, stop <-chan struct{}, onTick func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			onTick()
		case <-stop:
			return
		}
	}
}

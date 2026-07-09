package modify

import (
	"fmt"
	"sync"

	"memdroid/internal/driver"
)

const maxUndoDepth = 50

type undoEntry struct {
	drv   driver.Driver
	pid   int
	addr  uintptr
	value []byte
}

// UndoStack records previous values so writes can be reverted. It is safe for
// concurrent use.
type UndoStack struct {
	mu      sync.Mutex
	entries []undoEntry
}

func NewUndoStack() *UndoStack {
	return &UndoStack{}
}

// WithUndo writes value to addr, saving the previous value for Undo.
func (u *UndoStack) WithUndo(drv driver.Driver, pid int, addr uintptr, value []byte) error {
	prev, err := drv.Peek(pid, addr, len(value))
	if err != nil {
		return err
	}
	if err := drv.Poke(pid, addr, value); err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.entries = append(u.entries, undoEntry{drv: drv, pid: pid, addr: addr, value: prev})
	if len(u.entries) > maxUndoDepth {
		u.entries = u.entries[len(u.entries)-maxUndoDepth:]
	}
	return nil
}

// Undo reverts the most recent WithUndo. Returns an error if the stack is empty.
func (u *UndoStack) Undo() error {
	u.mu.Lock()
	if len(u.entries) == 0 {
		u.mu.Unlock()
		return fmt.Errorf("nothing to undo")
	}
	e := u.entries[len(u.entries)-1]
	u.entries = u.entries[:len(u.entries)-1]
	u.mu.Unlock()

	return e.drv.Poke(e.pid, e.addr, e.value)
}

// Depth returns how many undo steps are available.
func (u *UndoStack) Depth() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.entries)
}

// Clear discards the undo history.
func (u *UndoStack) Clear() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.entries = nil
}

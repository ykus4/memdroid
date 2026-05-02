package modify

import (
	"fmt"

	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

const maxUndoDepth = 50

type undoEntry struct {
	drv   driver.Driver
	pid   int
	addr  uintptr
	value []byte
	vtype search.ValueType
}

// UndoStack records previous values so writes can be reverted.
type UndoStack struct {
	entries []undoEntry
}

func NewUndoStack() *UndoStack {
	return &UndoStack{}
}

// WithUndo writes value to addr, saving the previous value for Undo.
func (u *UndoStack) WithUndo(drv driver.Driver, pid int, addr uintptr, value []byte, vt search.ValueType) error {
	prev, err := drv.Peek(pid, addr, len(value))
	if err != nil {
		return err
	}
	if err := drv.Poke(pid, addr, value); err != nil {
		return err
	}
	u.entries = append(u.entries, undoEntry{drv: drv, pid: pid, addr: addr, value: prev, vtype: vt})
	if len(u.entries) > maxUndoDepth {
		u.entries = u.entries[len(u.entries)-maxUndoDepth:]
	}
	return nil
}

// Undo reverts the most recent WithUndo. Returns an error if the stack is empty.
func (u *UndoStack) Undo() error {
	if len(u.entries) == 0 {
		return fmt.Errorf("nothing to undo")
	}
	e := u.entries[len(u.entries)-1]
	u.entries = u.entries[:len(u.entries)-1]
	if err := e.drv.Poke(e.pid, e.addr, e.value); err != nil {
		return err
	}
	return nil
}

// Depth returns how many undo steps are available.
func (u *UndoStack) Depth() int {
	return len(u.entries)
}

// Clear discards the undo history.
func (u *UndoStack) Clear() {
	u.entries = nil
}

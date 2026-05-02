package modify

import (
	"fmt"

	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

type undoEntry struct {
	drv   driver.Driver
	pid   int
	addr  uintptr
	value []byte
	vtype search.ValueType
}

var undoStack []undoEntry

// WithUndo writes value to addr, saving the previous value for Undo.
func WithUndo(drv driver.Driver, pid int, addr uintptr, value []byte, vt search.ValueType) error {
	prev, err := drv.Peek(pid, addr, len(value))
	if err != nil {
		return err
	}
	if err := drv.Poke(pid, addr, value); err != nil {
		return err
	}
	undoStack = append(undoStack, undoEntry{drv: drv, pid: pid, addr: addr, value: prev, vtype: vt})
	fmt.Printf("Modified 0x%x (undo available)\n", addr)
	return nil
}

// Undo reverts the most recent WithUndo.
func Undo() error {
	if len(undoStack) == 0 {
		fmt.Println("Nothing to undo")
		return nil
	}
	e := undoStack[len(undoStack)-1]
	undoStack = undoStack[:len(undoStack)-1]
	if err := e.drv.Poke(e.pid, e.addr, e.value); err != nil {
		return err
	}
	fmt.Printf("Undid 0x%x -> %s\n", e.addr, search.FormatValue(e.value, e.vtype))
	return nil
}

// UndoDepth returns how many undo steps are available.
func UndoDepth() int {
	return len(undoStack)
}

// ClearUndo discards the undo history.
func ClearUndo() {
	undoStack = nil
}

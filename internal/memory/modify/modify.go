package modify

import "memodroid/internal/driver"

// Write writes value bytes to addr in the target process.
func Write(drv driver.Driver, pid int, addr uintptr, value []byte) error {
	return drv.Poke(pid, addr, value)
}

// WriteString overwrites a string at addr with newStr (UTF-8 bytes).
func WriteString(drv driver.Driver, pid int, addr uintptr, newStr string) error {
	return drv.Poke(pid, addr, []byte(newStr))
}

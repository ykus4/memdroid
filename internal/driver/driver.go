package driver

// Region represents a readable/writable memory-mapped region.
type Region struct {
	Start, End uintptr
	Name       string
}

// RegionFilter restricts which memory regions are scanned.
type RegionFilter int

const (
	RegionAll    RegionFilter = iota
	RegionHeap                // [heap]
	RegionStack               // [stack]
	RegionAnon                // anonymous (no name)
	RegionCustom              // custom address range
)

// ProcessInfo describes a running process.
type ProcessInfo struct {
	PID  int
	Name string
}

// Driver abstracts all low-level device access so that higher-level packages
// (search, modify, watch, store) work identically over ADB or any future backend.
type Driver interface {
	// Device management
	ListDevices() ([]string, error) // serial numbers
	SelectDevice(serial string) error
	DeviceSerial() string

	// Process management
	ListProcesses() ([]ProcessInfo, error)
	Attach(pid int) error
	Detach(pid int)
	Stop(pid int) error
	Continue(pid int) error

	// Memory access
	Peek(pid int, addr uintptr, size int) ([]byte, error)
	Poke(pid int, addr uintptr, data []byte) error
	ReadMaps(pid int) ([]Region, error)
	ReadMapsFiltered(pid int, filter RegionFilter, customStart, customEnd uintptr) ([]Region, error)
	ReadBytes(pid int, addr uintptr, n int) ([]byte, error)
	// ReadRegion reads the entire region [addr, addr+size) in a single transport
	// call. This is orders of magnitude faster than Peek per address for bulk scans.
	ReadRegion(pid int, addr uintptr, size int) ([]byte, error)
}

// MatchFilter returns true when region r satisfies filter.
func MatchFilter(r Region, filter RegionFilter, customStart, customEnd uintptr) bool {
	switch filter {
	case RegionHeap:
		return r.Name == "[heap]"
	case RegionStack:
		return r.Name == "[stack]"
	case RegionAnon:
		return r.Name == ""
	case RegionCustom:
		return r.Start >= customStart && r.End <= customEnd
	default:
		return true
	}
}

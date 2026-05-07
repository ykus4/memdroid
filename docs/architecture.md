# Architecture

## Overview

```
./memodroid
  ├── CLI (main goroutine)           — interactive menu
  ├── HTTP server (:8080)            — Web UI + REST API + WebSocket
  └── app.State (shared, mutex)
        └── driver.Driver            — ADB backend
              ├── ListProcesses()    — adb shell ps -A
              ├── Attach/Detach()    — kill -STOP / -CONT via su
              ├── Peek/Poke()        — /proc/<pid>/mem via dd + base64
              └── ReadMaps()         — adb shell cat /proc/<pid>/maps
```

## Package Structure

```
internal/
├── app/
│   └── state.go           # Thread-safe shared state (Driver, PID, Session, Bookmarks)
├── driver/
│   ├── driver.go          # Driver interface + Region/RegionFilter types
│   └── adb/
│       ├── adb.go         # Device selection, shell helpers, Wi-Fi connect/disconnect
│       ├── process.go     # ListProcesses, FindProcessByName, Attach, Detach, Stop, Continue
│       ├── maps.go        # ReadMaps, ReadMapsFiltered — /proc/<pid>/maps parser
│       └── mem.go         # Peek, Poke, ReadBytes — /proc/<pid>/mem via dd + base64
├── process/
│   ├── list.go            # ProcessList() — delegates to Driver
│   └── control.go         # Attach/Detach/Stop/Continue — thin wrappers
├── server/
│   ├── server.go          # HTTP server, route registration, WebSocket wire-up
│   ├── handlers.go        # REST API handlers
│   ├── wswatch/
│   │   └── wswatch.go     # WebSocket broadcast hub for watch events
│   └── static/
│       └── index.html     # Single-page Web UI
└── memory/
    ├── search/
    │   ├── types.go       # ValueType (int32/64/float32/64/uint32/64/bytes), ParseValue, FormatValue
    │   ├── session.go     # Session: candidate address → bytes map + Driver ref
    │   ├── search.go      # Full scan (exact value, region-filtered, byte-sequence)
    │   ├── filter.go      # Narrow candidates (changed/unchanged/increased/decreased/value)
    │   ├── pattern.go     # Byte pattern search with ?? wildcard, chunked reads
    │   └── string.go      # UTF-8 / UTF-16LE string search via SearchPattern
    ├── pointer/
    │   └── pointer.go     # Pointer chain scan — find stable base+offset paths
    ├── modify/
    │   ├── modify.go      # Write value / string to address
    │   ├── undo.go        # Save previous value before modify, revert stack
    │   ├── freeze.go      # Background goroutine to hold values + FreezeAll
    │   └── dump.go        # Hex dump memory region to file
    ├── watch/
    │   └── watch.go       # Background goroutine; notifies on value change via BroadcastFunc
    └── store/
        ├── bookmark.go    # Named address bookmarks with bulk modify
        └── save.go        # Save / Load JSON state (bookmarks + candidates)
```

## CLI Source Layout

The root-level `package main` is split into focused files:

```
main.go            # main() entry point, REPL dispatch loop, printMenu()
cli_helpers.go     # prompt(), parseAddr(), parseValue(), require* guards
cli_device.go      # handleSelectDevice, handleConnectWifi, handleDisconnectWifi
cli_process.go     # doAttach, handleAttach, handleAttachByName, handleDetach
cli_search.go      # handleSetValueType, handleSearchFiltered, handleShowCandidates
cli_memory.go      # handleWatch, handleDump, handlePointerScan, handleShowMaps, handleBookmarkList
```

## Dependency Graph

```
main (cli_*.go)
 ├── app.State
 ├── driver/adb              (no internal deps — exec.Command("adb", ...) only)
 ├── process                 → driver
 ├── server                  → app, driver/adb, memory/*, server/wswatch
 ├── memory/search           → driver
 ├── memory/pointer          → driver
 ├── memory/modify           → driver, memory/search
 ├── memory/watch            → driver, memory/search
 └── memory/store            → memory/search
```

No circular imports.

## Key Design Decisions

### Driver Interface

All device I/O is behind `driver.Driver`. Higher-level packages (search, modify, etc.)
never call `adb` directly. This decouples the logic from the transport and makes it
straightforward to add a local ptrace backend in the future.

```go
type Driver interface {
    ListProcesses() ([]ProcessInfo, error)
    Attach(pid int) error
    Detach(pid int)
    Peek(pid int, addr uintptr, size int) ([]byte, error)
    Poke(pid int, addr uintptr, data []byte) error
    ReadMaps(pid int) ([]Region, error)
    ReadMapsFiltered(pid int, filter RegionFilter, ...) ([]Region, error)
    ReadBytes(pid int, addr uintptr, n int) ([]byte, error)
    ReadRegion(pid int, addr uintptr, size int) ([]byte, error)
    ...
}
```

### ADB Memory Access

`/proc/<pid>/mem` gives direct access to a process's virtual address space.
ADB transmits data as text, so raw bytes are wrapped in base64:

```
Peek: su -c 'dd if=/proc/<pid>/mem ... | base64'  → base64.Decode()
Poke: echo <base64> | base64 -d | dd of=/proc/<pid>/mem ...
```

Attach/Detach use `kill -STOP` / `kill -CONT` because ptrace is not available
from the host side over ADB.

### Shared State (app.State)

`app.State` holds the single source of truth protected by `sync.RWMutex`.
Both the CLI goroutine and all HTTP handler goroutines read/write through it.

```
State.GetDriver()   → current ADB driver
State.GetSession()  → current search session (nil if not attached)
State.GetPID()      → attached PID (0 if none)
State.EnsureSession() → creates session if nil
```

### Search Session

`Session` holds `map[uintptr][]byte` (address → last-seen value bytes).

```
Session.Search(target)               → full scan, reset candidate map
Session.SearchFiltered(target, ...)  → restricted to a region type
Session.Filter(mode, ...)            → re-read each candidate, drop non-matching
Session.Snapshot()                   → safe copy for concurrent reads
Session.Reset()                      → clear all candidates
```

### Pointer Scan Algorithm

1. Read all rw memory regions via ReadMaps
2. Scan every pointer-aligned 8-byte value in every region → build `ptrMap[value] = []srcAddr`
3. Walk backwards from `targetAddr` up to `maxDepth` levels:
   - For each delta in `[0, maxOffset]` (step 8), look up `ptrMap[targetAddr - delta]`
   - For each source address found, check if it lies in a static region (named .so / module)
   - If yes, record the chain; if not, recurse deeper
4. Return all found chains as `[]Chain{BaseAddr, BaseLabel, Offsets, FinalAddr}`

### WebSocket Watch Events

`watch.BroadcastFunc` is a package-level function variable set by `server.Start`.
When a watched value changes, `watch.go` calls `BroadcastFunc(addr, prev, cur)`,
which calls `wswatch.Broadcast`. The `wswatch` hub fans the JSON event out to all
connected `WebSocket` clients. The Web UI Watch panel receives and displays these.

### Freeze / Watch Goroutines

Both use the ticker+stop-channel pattern:

```go
ticker := time.NewTicker(interval)
for {
    select {
    case <-ticker.C:  /* do work */
    case <-e.stop:    return
    }
}
```

`UnfreezeAll` / `UnwatchAll` close all stop channels. These are called on detach and exit.

### Bulk Region Reads (Search & Filter)

`SearchFiltered` and `Filter` call `ReadRegion` once per memory region rather than
`Peek` once per candidate address. A full scan issues ~50 `adb shell` round-trips
(one per rw region) instead of millions, bringing scan time from hours to seconds.

`ReadRegion` splits large regions into 32 MB chunks internally so the base64 payload
stays within shell buffer limits; callers see a single contiguous byte slice.

Filter builds a region → `[]byte` cache from `ReadMaps`, slices out each candidate's
bytes in-memory, and falls back to `Peek` only for addresses outside the snapshot.

### Pattern Search

`SearchPattern` (byte-pattern with `??` wildcard) also uses `ReadRegion` per region
so pattern matches spanning chunk boundaries are handled without overlap bookkeeping.

## API Reference

See [docs/api.md](api.md) for the full REST + WebSocket endpoint list.

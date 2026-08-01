# Architecture

## Overview

```
./memdroid
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
├── driver/drivertest/
│   └── fake.go            # In-memory driver.Driver for tests (no device needed)
├── server/
│   ├── server.go          # Server lifecycle, auth, origin checks, URL helpers
│   ├── routes.go          # The route table (path → method → handler)
│   ├── handler.go         # Shared handler plumbing: JSON, hexAddr, validation
│   ├── paths.go           # Confines client-supplied file paths to -file-root
│   ├── handlers_*.go      # Endpoints, one file per resource
│   ├── wswatch/
│   │   └── wswatch.go     # WebSocket broadcast Hub for watch/alert events
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
    │   ├── pointer.go     # Pointer chain scan — find stable base+offset paths
    │   └── resolve.go     # Re-resolve a saved chain against a new run
    ├── modify/
    │   ├── modify.go      # Write value / string to address
    │   ├── undo.go        # Save previous value before modify, revert stack (mutex-guarded)
    │   ├── freeze.go      # Freezer — periodic re-write, backed by poller.Pool
    │   ├── snapshot.go    # Capture / diff a memory region
    │   └── dump.go        # Hex dump memory region to file (bulk ReadRegion)
    ├── watch/
    │   ├── watch.go       # Watcher — poll for value change, backed by poller.Pool
    │   ├── alert.go       # AlertWatcher — conditional watch + auto-write
    │   └── listeners.go   # Generic fan-out registry for watch/alert events
    └── store/
        ├── bookmark.go    # Named address bookmarks with bulk modify
        ├── cheatengine.go # Import CheatEngine .CT tables as bookmarks
        └── save.go        # Save / Load versioned JSON state (bookmarks + candidates)

internal/poller/
└── poller.go             # Keyed goroutine manager shared by Freezer/Watcher/AlertWatcher
```

## CLI Source Layout

The interactive menu lives in `package cli` under `internal/cli/`, one file per
concern (`run.go` actions, `prompt.go` input helpers, `device.go`,
`process.go`, `search.go`, `memory.go`, `pointer.go`, `alert.go`,
`bookmarks.go`, `menu.go`). `main.go` wires flags, the ADB driver, shared
state, and the HTTP server, then hands off to `cli.Run`.

`menu.go` holds the menu as a single table where each entry carries its key,
its label **and** its action. Dispatch indexes that same table, so a menu entry
can never exist without a handler (or vice versa), and a duplicate key panics
at startup rather than silently shadowing. Guards like `attached(...)` and
`withSession(...)` wrap an action instead of each action re-checking
preconditions.

## Dependency Graph

```
main
 ├── app.State
 ├── cli
 ├── driver/adb              (no internal deps — exec.Command("adb", ...) only)
 ├── driver/drivertest       → driver               (test fake)
 ├── server                  → app, driver, driver/adb, memory/*, server/wswatch
 ├── memory/search           → driver
 ├── memory/pointer          → driver
 ├── memory/modify           → driver, memory/search, poller
 ├── memory/watch            → driver, memory/search, poller
 └── memory/store            → driver, memory/search
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
State.GetDriver()     → current ADB driver
State.GetSession()    → current search session (nil if not attached)
State.GetPID()        → attached PID (0 if none)
State.NewSession(pid) → replace the session with a fresh one
State.EnsureSession() → creates session if nil
State.Detach()        → unfreeze/unwatch, detach, promote the next process
```

`State.Detach()` exists because the CLI and the API both need exactly that
sequence; keeping it in one place stops the two paths from drifting.

### Value Type Ownership

The active value type has exactly one owner at a time. Before a session exists,
`State` holds it; once a session exists, the **session** owns it and
`State.GetValueType()` delegates. `State.SetValueType` propagates into the live
session, and `Session.SetType` discards existing candidates because they were
recorded at a different byte width.

This matters because a scan and the formatting of its results must agree on the
width. Storing the type in two places let them disagree: setting the type over
the API updated `State` while the session kept scanning at the old width.

### Search Session

`Session` holds `map[uintptr][]byte` (address → last-seen value bytes). All
fields are unexported and mutex-guarded; long scans do their I/O without
holding the lock and take it only to swap in the finished result.

```
Session.Search(target)               → full scan, reset candidate map
Session.SearchFiltered(target, ...)  → restricted to a region type
Session.Filter(mode, ...)            → re-read each candidate, drop non-matching
Session.Page(offset, limit)          → one sorted page, without copying the rest
Session.Snapshot()                   → deep copy for concurrent reads
Session.Reset()                      → clear all candidates
```

`Page` exists so the API can serve `/api/search/candidates` without deep-copying
a multi-million-entry candidate map on every request.

### Pointer Scan Algorithm

1. Read all rw memory regions via ReadMaps
2. Scan every pointer-aligned 8-byte value in every region → build `ptrMap[value] = []srcAddr`
3. Walk backwards from `targetAddr` up to `maxDepth` levels:
   - For each delta in `[0, maxOffset]` (step 8), look up `ptrMap[targetAddr - delta]`
   - For each source address found, check if it lies in a static region (named .so / module)
   - If yes, record the chain; if not, recurse deeper
4. Return all found chains as `[]Chain{BaseAddr, BaseLabel, Offsets, FinalAddr}`

### Watch Event Fan-out

`Watcher` and `AlertWatcher` expose `OnChange` / `OnAlert` as **registrations**
that return an unsubscribe func, backed by the generic `listeners[T]` registry:

```go
remove := state.Watcher.OnChange(func(ev watch.ChangeEvent) { ... })
```

Watch events have two consumers — the CLI prints them, and the server forwards
them to `wswatch.Hub` for the browser — and they are wired up from different
goroutines at startup. A single assignable callback field let whichever side
registered last silently win (the server's registration clobbered the CLI's),
and was raced on by the polling goroutines that invoked it.

`wswatch.Hub` is a value rather than package state, so a test — or a second
server — gets its own client set.

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

### The Shared Scan Engine

Value, byte-sequence, pattern and string scans all run through one function,
`search.scanRegions`. It reads each region via `ReadRegion` — one `adb shell`
round-trip per 32 MiB rather than one per address — fans regions out across
`scanWorkers` goroutines, and merges the per-worker match maps at the end.

A full scan issues ~50 round-trips (one per rw region) instead of millions,
bringing scan time from hours to seconds.

Two details the engine handles so callers don't have to:

- **Chunking.** A region larger than `scanChunkBytes` (32 MiB) is read in
  pieces that overlap by `width-1` bytes, so a match straddling a chunk
  boundary is still found. Without the cap, one multi-gigabyte mapping times
  `scanWorkers` would exhaust RAM.
- **Alignment.** Fixed-width types are scanned on their natural alignment (as
  Cheat Engine does); `bytes` uses stride 1. The stride offset is computed from
  the *region* base, not the chunk base, so chunking cannot shift which offsets
  a scan visits.

`Filter` works the other way round: it builds a region → `[]byte` cache from
`ReadMaps`, binary-searches each candidate into its region, slices the current
bytes out in memory, and falls back to `Peek` only for addresses whose region
is missing (unmapped since the scan, or failed to read).

### Pattern & String Search

`SearchPattern` (byte pattern with `??` wildcards) runs on the same engine, and
`SearchString` compiles a UTF-8 or UTF-16LE string into a wildcard-free pattern.

Both cap results at `PatternMaxResults` and report `Truncated` rather than
silently returning a prefix. Results carry the matched **bytes** alongside each
address, so the API can seed the search session directly instead of re-reading
every hit.

## Testing

`internal/driver/drivertest` provides an in-memory `driver.Driver`. Everything
above the driver layer is pure logic over a byte-addressed process, so search,
filter, pointer scanning and the HTTP handlers are all tested without an
Android device or an `adb` binary:

```go
f := drivertest.New(drivertest.Region{Start: 0x1000, Name: "[heap]", Data: data})
st := app.NewState(f)
```

The fake records `Peeks` / `Pokes` / `RegionReads`, so a test can assert that a
change actually reduced transport round-trips. CI runs `go test -race`; the
scan engine, the poller pool and `app.State` are all concurrent, which is the
point of that flag.

## API Reference

See [docs/api.md](api.md) for the full REST + WebSocket endpoint list.

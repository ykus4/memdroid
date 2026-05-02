# memdroid

A memory modification tool for Android processes, operated from your PC via ADB.
Supports both an interactive CLI and a browser-based Web UI running simultaneously.

## Requirements

- Go 1.22+
- `adb` in PATH (`brew install android-platform-tools` on macOS)
- Android device with root (e.g. Magisk) and USB debugging enabled

## Build

```bash
go build -o memdroid .
./memdroid
```

No root on the host PC is required — all privileged operations run on the device via `adb shell su`.

## Quick Start

1. Connect your Android device via USB (or Wi-Fi ADB)
2. Run `./memdroid` — it auto-selects if only one device is connected
3. Open `http://localhost:8080` in your browser **or** use the CLI menu
4. Search for a value, narrow it down, modify or freeze it

**Typical workflow:**

1. `1s` — Attach by process name (partial match)
2. `7` — Search for a value (e.g. HP = 100)
3. Take damage in-game, then `11` Filter: Decreased
4. Repeat until 1-5 candidates remain
5. `15` Modify / `17` Freeze
6. `pt` Pointer Scan to find a stable address for next session
7. `28` Save State

## Features

| Category       | Feature                                                                   |
|----------------|---------------------------------------------------------------------------|
| Device         | USB device selection, Wi-Fi ADB connect/disconnect                        |
| Process        | List, Search by name, Attach, Detach, Stop, Continue                      |
| Search         | Exact value (int32/64/uint32/64/float32/64/bytes), region-filtered        |
| Pattern/String | Byte pattern with `??` wildcard, UTF-8 / UTF-16LE string search           |
| Filter         | Changed / Unchanged / Increased / Decreased / Specific value              |
| Pointer Scan   | Find stable pointer chains (base+offset path) to a target address         |
| Memory         | Modify (with Undo), Freeze, Freeze All, Unfreeze, Dump, Region browser    |
| Watch          | Real-time value change monitoring; events pushed to Web UI via WebSocket  |
| Bookmarks      | Save named addresses, bulk modify                                          |
| Session        | Save / Load state (bookmarks + candidates) as JSON                        |
| Web UI         | Full feature parity with CLI, accessible at `:8080`                       |

## Architecture

```
./memdroid
  ├── CLI (main goroutine)          — interactive menu
  ├── HTTP server (:8080)           — Web UI + REST API + WebSocket
  └── app.State (mutex-protected)
        └── driver.Driver
              ├── ListProcesses()   — adb shell ps -A
              ├── Attach/Detach()   — kill -STOP / -CONT via su
              ├── Peek/Poke()       — /proc/<pid>/mem via dd + base64
              └── ReadMaps()        — adb shell cat /proc/<pid>/maps
```

## Project Structure

```
memdroid/
├── main.go
├── go.mod
└── internal/
    ├── app/state.go                 # Thread-safe shared state
    ├── driver/
    │   ├── driver.go                # Driver interface + Region types
    │   └── adb/                     # ADB implementation
    │       ├── adb.go               # Device selection, Wi-Fi, shell helpers
    │       ├── process.go           # ps -A, FindByName, attach/detach
    │       ├── maps.go              # /proc/<pid>/maps parser
    │       └── mem.go               # Peek/Poke via dd+base64, ReadBytes
    ├── process/
    │   ├── list.go                  # ProcessList() via Driver
    │   └── control.go               # Attach/Detach/Stop/Continue wrappers
    ├── server/
    │   ├── server.go                # HTTP routing + WebSocket wire-up
    │   ├── handlers.go              # REST API handlers
    │   ├── wswatch/wswatch.go       # WebSocket broadcast hub
    │   └── static/index.html        # Single-page Web UI
    └── memory/
        ├── pointer/pointer.go       # Pointer chain scan
        ├── search/                  # Scan & filter (all value types)
        ├── modify/                  # Write, Undo, Freeze, Dump
        ├── watch/                   # Background value monitor
        └── store/                   # Bookmarks, Save/Load JSON
```

## Documentation

| Doc | Contents |
|-----|----------|
| [docs/usage.md](docs/usage.md) | Full CLI menu, workflows, value types, Wi-Fi ADB |
| [docs/api.md](docs/api.md) | REST + WebSocket API reference |
| [docs/architecture.md](docs/architecture.md) | Package structure, design decisions, algorithms |
| [docs/development.md](docs/development.md) | Setup, pre-commit hooks, contribution guide |

## Notes

- Requires root on the Android device (`su` must be available in `adb shell`)
- Pointer scan reads all rw memory; may take 30-60 s on large processes
- Value search reads each memory region in one `adb shell` call (~50 round-trips total) and scans in-memory on the PC — full scans typically complete in seconds
- Memory reads use `dd if=/proc/<pid>/mem` piped through base64 to survive ADB text transport

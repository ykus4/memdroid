# memdroid

<div align="center">

**ADB-based Android memory modification toolkit — single binary with CLI + Web UI**

[![CI](https://github.com/ykus4/memdroid/actions/workflows/ci.yml/badge.svg)](https://github.com/ykus4/memdroid/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ykus4/memdroid)](https://github.com/ykus4/memdroid/releases/latest)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/ykus4/memdroid)](LICENSE)

<!-- TODO: add demo GIF here -->

</div>

---

## What is memdroid?

memdroid lets you inspect and modify memory of Android processes directly from your PC — no PC root required. Connect via USB or Wi-Fi ADB, attach to any process, and start searching for values in seconds.

- **CLI + Web UI** run simultaneously — use whichever you prefer
- **No PC root** — all privileged operations run on the device via `adb shell su`
- **Single binary** — download, run, done

---

## Install

### Download (recommended)

Grab the latest binary for your platform from the [Releases](https://github.com/ykus4/memdroid/releases/latest) page:

| Platform | File |
|----------|------|
| macOS (Apple Silicon) | `memdroid-darwin-arm64` |
| macOS (Intel) | `memdroid-darwin-amd64` |
| Linux x86_64 | `memdroid-linux-amd64` |
| Linux ARM64 | `memdroid-linux-arm64` |
| Windows x86_64 | `memdroid-windows-amd64.exe` |

### Build from source

```bash
git clone https://github.com/ykus4/memdroid.git
cd memdroid
go build -o memdroid .
```

---

## Requirements

- `adb` in PATH
  - macOS: `brew install android-platform-tools`
  - Linux: `sudo apt install adb`
- Android device with **root** (e.g. Magisk) and **USB debugging** enabled

---

## Quick Start

```
1. Connect your Android device via USB (or Wi-Fi ADB)
2. Run ./memdroid
3. Open http://localhost:8080 in your browser  ← Web UI
   or use the interactive CLI menu             ← CLI
```

### Typical workflow — modify a game value (e.g. HP)

```
1s  Attach to process  →  search "com.example.game"
 7  Search value       →  enter current HP (e.g. 100)
    Take damage in-game...
11  Filter: Decreased  →  narrow candidates
    Repeat until 1–5 remain
15  Modify             →  set HP to 9999
17  Freeze             →  lock it there
pt  Pointer Scan       →  find stable address for next session
28  Save State         →  persist bookmarks + candidates
```

---

## Features

| Category | Feature |
|----------|---------|
| **Device** | USB device selection, Wi-Fi ADB connect/disconnect |
| **Process** | List, search by name, attach, detach, stop, continue |
| **Search** | Exact value — `int32/64` `uint32/64` `float32/64` `bytes` |
| **Pattern** | Byte pattern with `??` wildcard (e.g. `FF 00 ?? 01`) |
| **String** | UTF-8 and UTF-16LE string search & in-place edit |
| **Filter** | Changed / Unchanged / Increased / Decreased / Exact value |
| **Pointer Scan** | Find stable pointer chains across process restarts |
| **Modify** | Write value with Undo, Freeze (100 ms loop), Dump to file |
| **Watch** | Real-time value change monitor — streamed to Web UI via WebSocket |
| **Bookmarks** | Named addresses, bulk modify |
| **Session** | Save / load state (bookmarks + candidates) as JSON |
| **Web UI** | Full feature parity with CLI at `http://localhost:8080` |

---

## Architecture

```
memdroid
├── CLI (main goroutine)
│     ├── main.go          — entry point + REPL loop
│     ├── cli_helpers.go   — prompt, parse, guards
│     ├── cli_device.go    — device handlers
│     ├── cli_process.go   — process handlers
│     ├── cli_search.go    — search/filter handlers
│     └── cli_memory.go    — modify, watch, pointer, maps, bookmarks
├── HTTP Server :8080 — Web UI + REST API + WebSocket
└── app.State (mutex-protected)
      └── driver.Driver
            ├── ListProcesses  — adb shell ps -A
            ├── Attach/Detach  — kill -STOP / -CONT via su
            ├── Peek/Poke      — /proc/<pid>/mem via dd + base64
            └── ReadMaps       — /proc/<pid>/maps
```

---

## Documentation

| Doc | Contents |
|-----|----------|
| [docs/usage.md](docs/usage.md) | Full CLI menu reference, workflows, value types, Wi-Fi ADB |
| [docs/api.md](docs/api.md) | REST + WebSocket API reference |
| [docs/architecture.md](docs/architecture.md) | Package structure, design decisions, algorithms |
| [docs/development.md](docs/development.md) | Setup, pre-commit hooks, contribution guide |

---

## Disclaimer

This tool is intended for **security research, CTF challenges, and personal educational use on devices you own**.

- Only use memdroid on devices and applications you have explicit permission to analyze
- Do not use this tool to cheat in online multiplayer games or circumvent anti-cheat systems
- The authors are not responsible for any misuse or damage caused by this software

---

## Notes

- Requires **root on the Android device** (`su` must be available in `adb shell`)
- Pointer scan reads all `rw` memory regions — may take 30–60 s on large processes
- Memory reads use `dd if=/proc/<pid>/mem` piped through `base64` to survive ADB text transport
- Full scans typically complete in seconds — each region is read in a single `adb shell` call

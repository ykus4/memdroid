# memdroid

<p align="center">
  <img src="assets/logo.png" alt="memdroid" width="480">
</p>

**ADB-based Android memory modification toolkit — single binary with CLI + Web UI**

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
| **Process** | List, search by name, multi-attach, switch active process |
| **Search** | Exact value — `int32/64` `uint32/64` `float32/64` `bytes`, parallel scanning |
| **Pattern** | Byte pattern with `??` wildcard (e.g. `FF 00 ?? 01`) |
| **String** | UTF-8 and UTF-16LE string search & in-place edit |
| **Filter** | Changed / Unchanged / Increased / Decreased / Exact value |
| **Pointer Scan** | Find stable pointer chains, auto-resolve after ASLR rebasing |
| **Modify** | Write value with Undo, Freeze (configurable interval), Dump to file |
| **Snapshot Diff** | Compare two memory snapshots to find changed bytes |
| **Watch** | Real-time value change monitor — streamed to Web UI via WebSocket |
| **Alerts** | Conditional watch — auto-write or notify when value crosses threshold |
| **Bookmarks** | Named addresses, bulk modify, CheatEngine .CT import |
| **Session** | Save / load state (bookmarks + candidates) as JSON |
| **Web UI** | Hex viewer, paginated candidates, pointer tree view at `http://localhost:8080` |

---

## Documentation

| Section | Contents |
|---------|----------|
| [Usage](usage.md) | Full CLI menu reference, workflows, value types, Wi-Fi ADB |
| [API Reference](api.md) | REST + WebSocket API reference |
| [Architecture](architecture.md) | Package structure, design decisions, algorithms |
| [Development](development.md) | Setup, pre-commit hooks, contribution guide |

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

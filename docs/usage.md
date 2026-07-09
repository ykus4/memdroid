# Usage Guide

## Prerequisites

- `adb` in PATH (`brew install android-platform-tools` on macOS)
- Android device with root (Magisk) and USB debugging enabled

## Build & Run

```bash
go build -o memdroid .
./memdroid
```

No root on the host is required. All privileged operations run on the device via `adb shell su`.

## Connecting a Device

### USB
Connect the device and run memdroid. It auto-selects if only one device is found.
If multiple devices are connected, you will be prompted to select one.

### Wi-Fi ADB
```
dw → Connect Wi-Fi → 192.168.1.5:5555
```
Or in the Web UI: Device panel → Wi-Fi Connection field → Connect.

To enable Wi-Fi ADB on the device:
```bash
adb tcpip 5555
adb connect 192.168.1.5:5555
```

## Menu Overview

```
--- Device ---           --- Pattern / String ---
  d. Select Device        p.  Byte Pattern Search  (e.g. FF 00 ?? 01)
 dw. Connect Wi-Fi        s8. String Search UTF-8
 dd. Disconnect Wi-Fi    s16. String Search UTF-16LE
                          sw. Modify String at Address
--- Process ---
  1. Process List        --- Memory ---
 1s. Attach by Name      15. Modify Address
  2. Attach by PID       16. Undo Last Modify
  3. Detach              17. Freeze Address
  4. Stop Process       17a. Freeze All Candidates
  5. Continue Process    18. Unfreeze Address
                         19. List Frozen
--- Search ---           20. Watch Address
  6. Set Value Type      21. Unwatch Address
  7. Search Value        22. List Watched
 7r. Search (Region)     23. Dump Memory Region
  8. Filter: Changed    23m. Show Memory Maps
  9. Filter: Unchanged
 10. Filter: Increased   --- Pointer ---
 11. Filter: Decreased    pt. Pointer Scan
 12. Filter: Exact Val
 13. Show Candidates     --- Bookmarks ---
 14. Reset Search        24. Add Bookmark
                         25. List Bookmarks
--- Session ---          26. Modify All Bookmarks
 28. Save State          27. Remove Bookmark
 29. Load State
```

## Typical Workflows

### Numeric value cheat (e.g. HP)

```
1s → Attach by Name → "com.example.game"
 4 → Stop process
 7 → Search: 100           (current HP)
 5 → Continue process
   → Take damage in-game
 4 → Stop process
11 → Filter: Decreased     (HP went down)
   → Repeat until ~5 candidates remain
15 → Modify the address    (set HP to 9999)
17 → Freeze the address    (hold at 9999)
24 → Add Bookmark "HP"
```

### Pointer scan (stable address across restarts)

```
7  → Search current HP value, narrow to 1 candidate
pt → Pointer Scan → target address (from candidate)
   → Wait 30-60 s for results
   → Note a chain like [libil2cpp.so+0x1234]+0x20+0x8
   → Next session: follow that chain to find HP again
```

### String / name edit

```
s8  → Search string "Player"
sw  → Modify string at found address → "Hacker"
```

### Byte pattern search

```
p → Pattern: FF 00 ?? 01
    (?? matches any byte)
15 → Modify the found address
```

### Region-filtered scan (faster)

```
7r → Search (Region filtered)
     Region: 2 = heap only
     Value: 100
```

### Byte sequence search

```
6  → Set Type → 7 (bytes)
7  → Search: FF00AB12     (hex, spaces optional)
```

### Session save & restore

```
28 → Save State → memdroid.json
--- next session ---
29 → Load State → memdroid.json
```

## Value Types

| Type    | Size    | Example use                        |
|---------|---------|------------------------------------|
| int32   | 4 bytes | HP, ammo, score (signed)           |
| int64   | 8 bytes | currency, large counters (signed)  |
| float32 | 4 bytes | position X/Y, speed                |
| float64 | 8 bytes | high-precision floats              |
| uint32  | 4 bytes | unsigned integer counters          |
| uint64  | 8 bytes | large unsigned values              |
| bytes   | variable| arbitrary byte sequences           |

Switch type with menu `6`. Changing type resets the search session.

## Region Filters (`7r`)

| Option | Scans          | Best for                      |
|--------|---------------|-------------------------------|
| all    | all rw regions | thorough first scan           |
| heap   | `[heap]`       | most game object values       |
| stack  | `[stack]`      | local variables               |
| anon   | anonymous maps | JIT / runtime allocations     |
| custom | address range  | known library segment         |

## Web UI

Open `http://localhost:8080` while memdroid is running. All features are available.

Special Web UI features:
- **Watch panel**: real-time value change stream via WebSocket
- **Maps panel**: interactive memory region browser with filter
- **Candidates**: "Ptr" button pre-fills the Pointer Scan target
- **Device panel**: Wi-Fi connect / disconnect without CLI

## Notes

- **Freeze** writes the value every 100 ms in the background. Use `18` or `17a` to stop.
- **Watch** prints to stdout (and Web UI Watch panel) when a value changes.
- **Undo** reverts only the most recent `Modify`. Depth shown in status bar.
- **Pointer scan** may take 30-60 s on large processes; it reads all mapped memory.
- **Dump** output is standard hex dump format compatible with any hex editor.

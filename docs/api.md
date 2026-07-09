# API Reference

All REST endpoints accept and return JSON. POST endpoints read the request body as JSON.
The WebSocket endpoint is at `/ws/watch`. Each endpoint is gated to a single
HTTP method (GET for reads, POST for mutations); other methods return `405`.
Request bodies are capped at 1 MiB.

Base URL: `http://localhost:8080` (the server binds to loopback by default;
use `-addr` to change it).

## Authentication

By default there is no auth and the server listens only on `127.0.0.1`. To
expose it safely, start with `-token <secret>` (or set `MEMDROID_TOKEN`). All
`/api` and `/ws` requests must then present the token via a `token` query
parameter, an `Authorization: Bearer <token>` header, or the `mdtoken` cookie.
Opening `http://<host>:<port>/?token=<secret>` once sets the cookie so the Web
UI keeps working.

## Status

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/status` | PID, value_type, candidates, frozen list, undo_depth, device serial |

## Device

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET | `/api/device/list` | — | List connected ADB devices |
| POST | `/api/device/select` | `{"serial":"..."}` | Select active device |
| POST | `/api/device/connect-wifi` | `{"addr":"host:port"}` | Connect via Wi-Fi ADB |
| POST | `/api/device/disconnect-wifi` | `{"addr":"host:port"}` | Disconnect Wi-Fi ADB |

## Process

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET | `/api/process/list` | — | List all running processes |
| POST | `/api/process/search` | `{"name":"substr"}` | Find processes by name |
| POST | `/api/process/attach` | `{"pid":1234}` | Attach and create search session |
| POST | `/api/process/detach` | `{}` | Detach, unfreeze all, clear session |
| POST | `/api/process/stop` | `{}` | Send SIGSTOP |
| POST | `/api/process/continue` | `{}` | Send SIGCONT |

## Maps

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/maps` | List rw memory regions (start, end, size, name) |

## Search

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/search/value` | `{"value":"100","type":"int32"}` | Full value scan |
| POST | `/api/search/filter` | `{"mode":"decreased"}` | Narrow candidates |
| GET | `/api/search/candidates` | — | List current candidates |
| POST | `/api/search/reset` | `{}` | Clear session candidates |
| POST | `/api/search/pattern` | `{"pattern":"FF ?? 01"}` | Byte pattern scan |
| POST | `/api/search/string` | `{"value":"HP","encoding":"utf8"}` | String scan |

Filter modes: `changed`, `unchanged`, `increased`, `decreased`, `value`
Value types: `int32`, `int64`, `float32`, `float64`, `uint32`, `uint64`, `bytes`

## Pointer Scan

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/pointer/scan` | `{"addr":"0x7f...","max_depth":5,"max_offset":2048}` | Find pointer chains to address |

Response: `{"target":"0x...","chains":[{"base":"0x...","label":"libXX.so","base_offset":"0x1234","offsets":[32,8],"path":"[libXX.so+0x1234]+0x20+0x8"}]}`

Offsets are in base→final application order. `/api/pointer/resolve` takes
`{"label":"libXX.so","base_offset":"0x1234","offsets":[32,8]}` and walks the
chain in the current process, returning `{"resolved":"0x..."}`.

## Memory

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/memory/modify` | `{"addr":"0x...","value":"9999"}` | Write value (with undo) |
| POST | `/api/memory/undo` | `{}` | Revert last write |
| POST | `/api/memory/freeze` | `{"addr":"0x...","value":"9999"}` | Start freeze goroutine |
| POST | `/api/memory/freeze-all` | `{}` | Freeze all current candidates |
| POST | `/api/memory/unfreeze` | `{"addr":"0x..."}` | Stop freeze for address |
| GET | `/api/memory/frozen` | — | List frozen addresses |

## Bookmarks

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET | `/api/bookmark/list` | — | List bookmarks with current values |
| POST | `/api/bookmark/add` | `{"addr":"0x...","label":"HP"}` | Add bookmark |
| POST | `/api/bookmark/remove` | `{"index":0}` | Remove bookmark by index |
| POST | `/api/bookmark/modify-all` | `{"value":"9999"}` | Write to all matching-type bookmarks |

## Session

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/session/save` | `{"path":"memdroid.json"}` | Save bookmarks + candidates |
| POST | `/api/session/load` | `{"path":"memdroid.json"}` | Load bookmarks + candidates |

## WebSocket

| Path | Direction | Description |
|------|-----------|-------------|
| `/ws/watch` | Server → Client | JSON events when a watched value changes |

Event format:
```json
{"addr":"0x7f1234","prev":"100","cur":"95"}
```

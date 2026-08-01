# API Reference

All REST endpoints accept and return JSON. POST endpoints read the request body as JSON.
The WebSocket endpoint is at `/ws/watch`. Each endpoint is gated to a single
HTTP method (GET for reads, POST for mutations); other methods return `405`
with an `Allow` header. Request bodies are capped at 1 MiB.

Base URL: `http://localhost:8080` (the server binds to loopback by default;
use `-addr` to change it).

Addresses are always rendered as `"0x..."` strings, and accepted in any form
`strconv.ParseUint(s, 0, 64)` understands (`0x1f4`, `1f4` is *not* accepted —
use the `0x` prefix, or plain decimal).

## Authentication

By default there is no auth and the server listens only on `127.0.0.1`. To
expose it safely, start with `-token <secret>` (or set `MEMDROID_TOKEN`). All
`/api` and `/ws` requests must then present the token via a `token` query
parameter, an `Authorization: Bearer <token>` header, or the `mdtoken` cookie.
Opening `http://<host>:<port>/?token=<secret>` once sets the cookie so the Web
UI keeps working.

Tokens are compared in constant time. The cookie is `HttpOnly` and
`SameSite=Strict`, and WebSocket handshakes from a foreign `Origin` are
rejected.

## File access

Endpoints that read or write files on the machine running memdroid
(`/api/session/save`, `/api/session/load`, `/api/import/ct`,
`/api/memory/dump`) are confined to the `-file-root` directory, which defaults
to the working directory. Absolute paths and `../` traversal are rejected.
Pass `-file-root ""` to disable the restriction.

## Status

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/status` | PID, value_type, candidates, frozen/watched/alert lists, undo_depth, device serial |

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
| GET | `/api/process/attached` | — | List attached processes, flagging the active one |
| POST | `/api/process/search` | `{"name":"substr"}` | Find processes by name |
| POST | `/api/process/attach` | `{"pid":1234,"name":"com.x"}` | Attach and create search session (`name` optional) |
| POST | `/api/process/switch` | `{"pid":1234}` | Make an already-attached process active |
| POST | `/api/process/detach` | `{}` | Detach, unfreeze/unwatch all, promote the next attached process |
| POST | `/api/process/stop` | `{}` | Send SIGSTOP |
| POST | `/api/process/continue` | `{}` | Send SIGCONT |

## Maps

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/maps` | List rw memory regions (start, end, size, name) |

## Search

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET | `/api/search/types` | — | Supported value types and the current one |
| POST | `/api/search/type` | `{"type":"float32"}` | Set the value type (discards candidates) |
| POST | `/api/search/value` | `{"value":"100","type":"int32"}` | Full value scan |
| POST | `/api/search/filter` | `{"mode":"decreased"}` | Narrow candidates |
| GET | `/api/search/candidates` | `?page=0&page_size=100` | Page through current candidates |
| POST | `/api/search/reset` | `{}` | Clear session candidates |
| POST | `/api/search/pattern` | `{"pattern":"FF ?? 01"}` | Byte pattern scan |
| POST | `/api/search/string` | `{"value":"HP","encoding":"utf8"}` | String scan |

Filter modes: `changed`, `unchanged`, `increased`, `decreased`, `value`
Value types: `int32`, `int64`, `float32`, `float64`, `uint32`, `uint64`, `bytes`
String encodings: `utf8` (default), `utf16`

`/api/search/value` also accepts an optional region filter:
`{"region":"heap"}` — one of `all` (default), `heap`, `stack`, `anon`, or
`custom` with `{"region":"custom","region_start":"0x...","region_end":"0x..."}`.

The value type is owned by the search session, so passing `"type"` to
`/api/search/value` switches the type and rescans at the new width; existing
candidates are discarded because they were recorded at the old width.

Pattern and string scans stop at 200 matches and set `"truncated": true` when
they do. Their results become `bytes`-typed candidates in the session, so they
can be filtered, paged and frozen like any other result.

## Pointer Scan

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/pointer/scan` | `{"addr":"0x7f...","max_depth":5,"max_offset":2048}` | Find pointer chains to address |
| POST | `/api/pointer/resolve` | `{"label":"libXX.so","base_offset":"0x1234","offsets":[32,8]}` | Walk a saved chain in the current process |

Scan response: `{"target":"0x...","chains":[{"base":"0x...","label":"libXX.so","base_offset":"0x1234","offsets":[32,8],"path":"[libXX.so+0x1234]+0x20+0x8"}]}`

Offsets are in base→final application order. Resolve returns
`{"resolved":"0x...","path":"[libXX.so+0x1234]+0x20+0x8"}`.

## Memory

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/memory/modify` | `{"addr":"0x...","value":"9999"}` | Write value (with undo) |
| POST | `/api/memory/write-string` | `{"addr":"0x...","value":"text"}` | Overwrite a string in place (UTF-8) |
| POST | `/api/memory/undo` | `{}` | Revert last write |
| POST | `/api/memory/freeze` | `{"addr":"0x...","value":"9999"}` | Start freeze goroutine |
| POST | `/api/memory/freeze-interval` | `{"interval_ms":100}` | Set the freeze rewrite interval |
| POST | `/api/memory/freeze-all` | `{}` | Freeze all current candidates |
| POST | `/api/memory/unfreeze` | `{"addr":"0x..."}` | Stop freeze for address |
| GET | `/api/memory/frozen` | — | List frozen addresses |
| GET | `/api/memory/hexdump` | `?addr=0x...&size=256` | Hex view (max 4096 bytes) |
| POST | `/api/memory/dump` | `{"addr":"0x...","size":65536,"path":"dump.hex"}` | Write a region to a file on the host |

## Watch

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/watch/add` | `{"addr":"0x...","interval_ms":500}` | Poll an address and stream changes over `/ws/watch` |
| POST | `/api/watch/remove` | `{"addr":"0x..."}` | Stop watching |
| GET | `/api/watch/list` | — | List watched addresses |

## Alert

Conditional watches that can write a value automatically when they fire.

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/alert/add` | see below | Add a conditional watch |
| POST | `/api/alert/remove` | `{"addr":"0x..."}` | Remove an alert |
| GET | `/api/alert/list` | — | List alert addresses |

```json
{
  "addr": "0x7f1234",
  "condition": "below",
  "threshold": "50",
  "action": "write",
  "write_value": "9999",
  "interval_ms": 500
}
```

Conditions: `above` (`>`), `below` (`<`), `changed` (`!=`). `threshold` is
required except for `changed`. Actions: `notify` (default), `write` — which
also requires `write_value`.

## Snapshot

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/snapshot/take` | `{"addr":"0x...","size":4096}` | Capture a region for later comparison |
| POST | `/api/snapshot/diff` | `{"addr":"0x...","size":4096}` | Re-read and report changed bytes |

## Bookmarks

| Method | Path | Body | Description |
|--------|------|------|-------------|
| GET | `/api/bookmark/list` | — | List bookmarks with current values |
| POST | `/api/bookmark/add` | `{"addr":"0x...","label":"HP"}` | Add bookmark |
| POST | `/api/bookmark/remove` | `{"index":0}` | Remove bookmark by index |
| POST | `/api/bookmark/modify-all` | `{"value":"9999"}` | Write to all matching-type bookmarks |

## Import

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/import/ct` | `{"path":"table.CT"}` | Import a CheatEngine table as bookmarks |

## Session

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/session/save` | `{"path":"memdroid.json"}` | Save bookmarks + candidates |
| POST | `/api/session/load` | `{"path":"memdroid.json"}` | Load bookmarks + candidates |

`path` is optional and defaults to `memdroid.json`.

## WebSocket

| Path | Direction | Description |
|------|-----------|-------------|
| `/ws/watch` | Server → Client | JSON events for watch changes and alerts |

Watch event:

```json
{"kind":"change","addr":"0x7f1234","prev":"100","cur":"95"}
```

Alert event:

```json
{"kind":"alert","addr":"0x7f1234","cur":"42","condition":"below","triggered":true}
```

`triggered` is `true` when the alert's `write` action ran.

## Errors

Failures return the matching status code and `{"error":"..."}`:

| Status | Meaning |
|--------|---------|
| 400 | Bad input, no process attached, or no active search session |
| 401 | Missing or invalid token |
| 403 | Cross-origin WebSocket handshake |
| 405 | Wrong HTTP method for the path |
| 500 | The device operation itself failed |

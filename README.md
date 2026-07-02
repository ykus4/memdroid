<div align="center">

<img src="docs/assets/logo.png" alt="memdroid" width="480">

**ADB-based Android memory modification toolkit — single binary with CLI + Web UI**

[![CI](https://github.com/ykus4/memdroid/actions/workflows/ci.yml/badge.svg)](https://github.com/ykus4/memdroid/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ykus4/memdroid)](https://github.com/ykus4/memdroid/releases/latest)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/ykus4/memdroid)](LICENSE)

</div>

Inspect and modify memory of Android processes from your PC — no PC root required.

## Quick Start

```bash
# Download from Releases, or build from source:
go build -o memdroid .

# Connect device via USB (or Wi-Fi ADB), then:
./memdroid
# → open http://localhost:8080 for Web UI, or use the CLI menu
```

Requires `adb` in PATH and an Android device with root (Magisk) + USB debugging.

## Documentation

**Full documentation:** <https://ykus4.github.io/memdroid>

- [Usage](https://ykus4.github.io/memdroid/usage/) — CLI menu, workflows, value types
- [API Reference](https://ykus4.github.io/memdroid/api/) — REST + WebSocket
- [Architecture](https://ykus4.github.io/memdroid/architecture/) — package structure, design decisions
- [Development](https://ykus4.github.io/memdroid/development/) — setup, contributing

## License

[MIT](LICENSE) — for security research, CTF, and personal educational use on devices you own.

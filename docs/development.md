# Development Guide

## Requirements

- Go 1.26+
- macOS, Linux, or Windows (host PC)
- [pre-commit](https://pre-commit.com/) for local hooks

## Setup

```bash
git clone https://github.com/ykus4/memdroid.git
cd memdroid
go mod download

# Install pre-commit hooks
brew install pre-commit   # or: pip install pre-commit
pre-commit install
```

## Build

```bash
go build -o memdroid .
```

## Pre-commit Hooks

Configured in [.pre-commit-config.yaml](../.pre-commit-config.yaml):

| Hook           | What it does                              |
|----------------|-------------------------------------------|
| `go-fmt`       | Formats all `.go` files with `gofmt`      |
| `go-vet`       | Runs `go vet ./...`                       |
| `go-imports`   | Cleans up import blocks with `goimports`  |
| `golangci-lint`| Runs linters defined in `.golangci.yml`   |

Run manually at any time:

```bash
pre-commit run --all-files
```

Linter config: [.golangci.yml](../.golangci.yml)  
Enabled: `gofmt`, `goimports`, `govet`, `errcheck`, `staticcheck`

## Adding a New Feature

1. Decide which package it belongs to:
   - `internal/memory/search/` — scanning and filtering logic
   - `internal/memory/modify/` — write-side operations
   - `internal/memory/watch/` — background monitoring
   - `internal/memory/store/` — persistence (bookmarks, JSON)
   - `internal/process/` — process lifecycle
   - `internal/driver/adb/` — ADB-level operations

2. Add the CLI handler to the appropriate `cli_*.go` file (or create a new one for a distinct category). Add a menu entry in `printMenu` and a dispatch case in `main.go`.

3. Register any new REST endpoint in `internal/server/server.go`.

4. Update [docs/usage.md](usage.md) and [docs/architecture.md](architecture.md).

## Commit Convention

Write descriptive commit messages. Format:

```
<Package>: <short summary>

- detail 1
- detail 2
```

Example:
```
search/filter.go: add fuzzy float comparison

- Treat float32/64 values within 1e-6 as equal to handle
  floating-point drift in game engines.
```

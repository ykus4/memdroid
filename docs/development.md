# Development Guide

## Requirements

- Go 1.22+
- Linux (build tags require Linux for ptrace; non-Linux builds compile but all ptrace calls return `errNotLinux`)
- [pre-commit](https://pre-commit.com/) for local hooks

## Setup

```bash
git clone <repo>
cd MeMoDroid
go mod download

# Install pre-commit hooks
brew install pre-commit   # or: pip install pre-commit
pre-commit install
```

## Build

```bash
# Standard build
go build -o memodroid .

# Verify it compiles on non-Linux too (stub paths)
GOOS=darwin go build ./...
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
Enabled: `gofmt`, `goimports`, `govet`, `errcheck`, `staticcheck`, `unused`, `gosimple`

## Adding a New Feature

1. Decide which package it belongs to:
   - `internal/memory/ptrace/` — low-level syscall I/O
   - `internal/memory/search/` — scanning and filtering logic
   - `internal/memory/modify/` — write-side operations
   - `internal/memory/watch/` — background monitoring
   - `internal/memory/store/` — persistence (bookmarks, JSON)
   - `internal/process/` — process lifecycle

2. If the feature is Linux-only, add a `//go:build linux` tag and a matching `_stub.go` with `//go:build !linux`.

3. Wire up the new function in `main.go` and add a menu entry to `printMenu`.

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

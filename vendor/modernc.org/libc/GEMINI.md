# GEMINI.md

This file provides guidance for the Gemini AI assistant when working with the `modernc.org/libc` repository.

## Overview
`modernc.org/libc` is a partial reimplementation of C libc in pure Go. It acts as the runtime for C programs transpiled to Go by the `modernc.org/ccgo` transpiler (used notably by `modernc.org/sqlite`). It is not intended as a general-purpose standalone library.

**Key Rule:** The API tracks the needs of `ccgo`-generated code. Callers must use the libc version matching their generated code. Do not bump the libc version of downstream consumers without re-translating their C sources.

## Architecture
The codebase is split into two completely separate code paths, selected by build constraints:

1. **musl-derived (Linux)**
   - Targeted via: `//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm)`
   - Generated files: `ccgo_linux_*.go` (~4MB each), per-arch `musl_*.go`, and assembly stubs `abi0_linux_amd64.{go,s}`. **Do not hand-edit generated files**.
   - Hand-written glue files: `libc_musl.go`, `etc_musl.go`, `mem_brk_musl.go`, `mem_musl.go`, `memgrind_musl.go`, `pthread_musl.go`, `syscall_musl.go`, `aliases.go`, `atomic.go`, `atomic{32,64}.go`, `builtin{32,64}.go`, `libc_linux_statfs.go`.

2. **Hand-written (non-Linux / mips64le)**
   - Targeted via negated constraints.
   - Main files: `libc.go`, `libc_unix.go`, `libc_<goos>.go`, `libc_windows*.go`, `pthread.go`, `mem.go`.
   - These platforms use handwritten implementations combined with some older `ccgo/v3`-generated `musl_<goos>_<goarch>.go` pieces.

**Important:** When making a change, you usually need to update *both* code paths (the musl path and the hand-written path).

## Common Commands
- **Build/Lint**: 
  - `go build ./...`
  - `make editor` (fast iteration)
  - `make build_all_targets` (full cross-compile, required before submitting)
- **Tests**:
  - `make short-test`
  - `make test` (full test)
  - `make libc-test` (musl libc-test suite)
  - `go test -v -run <TestName>`
- **Generate** (Linux only):
  - `make generate` (downloads musl, translates via `ccgo`, rebuilds, and tests)

## Build Tags
- `libc.membrk`: Sbrk-style allocator for detecting use-after-free.
- `libc.memgrind`: Allocator audit table for leak detection.
- `libc.dmesg`: Per-pid logging to `/tmp/libc.log`.
- `libc.strace`: Traces C function entries.
- *Note: `libc.membrk` and `libc.memgrind` are mutually exclusive.*

## Symbol Naming Conventions
- `Xfoo`: Externally visible C function `foo`.
- `Yfoo`: abi0-wrapped entry to `Xfoo` (linux/amd64).
- `Tfoo_t`: Typedef'd C type `foo_t`.
- `Sfoo` / `Ufoo`: Struct / Union tags.
- `Ffield`: Struct field.

If you add a new `Xfoo` symbol in hand-written files, ensure it is added to the relevant `capi_<goos>_<goarch>.go` files.

## Thread Local State (TLS)
- `*TLS` represents a C "thread". It is passed as the first argument to every `Xfoo` function.
- `TLS` is **not safe for concurrent use**. Use one goroutine per TLS.

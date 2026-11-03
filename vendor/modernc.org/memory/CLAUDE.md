# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`modernc.org/memory` is a single-package, dependency-light Go library implementing a C-style `malloc`/`free` allocator on top of raw OS mappings (`mmap` on unix, `VirtualAlloc` on Windows). It exists to serve the modernc C-to-Go stack (`modernc.org/libc` and everything above it), which needs memory the Go GC neither moves nor scans, and addresses that stay valid when held only as `uintptr`.

The repo is flat: ten Go files, one test file. `internal/autogen/` is an empty placeholder for the modernc builder infrastructure described by `builder.json`.

## Commands

```sh
go test                                 # full suite, ~2m15s (each test churns a 128 MiB quota)
go test -tags=memory.counters           # same, with the accounting assertions actually live — see Build tags
go test -run TestUMalloc                # a single test
go test -run @ -bench . -benchmem       # benchmarks only ("@" matches no test name)
make editor                             # gofmt -l -s -w *.go — the pre-commit step
make build_all_targets                  # go build + staticcheck across every supported GOOS/GOARCH
make cpu | make mem                     # bench under a profiler, then open pprof
make todo                               # grep TODO/BUG/println/unused-assignment markers
```

`make all` is the author's personal pipeline: it needs `misspell`, `unconvert`, `maligned` and `golint` installed and greps a file named `log` that it does not create (the convention is `go test ... |& tee log`). It usually fails locally — reach for the individual targets instead.

**`go vet`**: the standalone command reports dozens of `possible misuse of unsafe.Pointer` across `memory.go` and `all_test.go` and exits 1. These are inherent to the design — the allocator deliberately round-trips addresses through `uintptr` into memory the GC does not manage — and must not be "fixed". For a meaningful signal use `go vet -unsafeptr=false ./...` or `staticcheck`; both are clean.

## Build tags

- **`memory.counters`** (`counters.go` / `nocounters.go`) — enables the `Allocator.Allocs/Bytes/Mmaps` bookkeeping. Without it those fields stay 0, which silently makes the `alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0` assertion ending nearly every test vacuous; only the `len(alloc.regs) != 0` leak check survives. **Any change to mmap/unmap accounting must be tested with `-tags=memory.counters`.**
- **`memory.trace`** (`trace_enabled.go` / `trace_disabled.go`) — logs every Malloc/Free/Realloc/UsableSize call to stderr.

Both constants are compile-time `false` by default, so the guarded code costs nothing in normal builds.

## Architecture

Everything hangs off one invariant: **every mapping is `pageSize`-aligned (64 KiB, `pageSizeLog = 16`), so any allocated address masks down to its page header with `p &^ pageMask`.** There are no per-allocation headers and no address→metadata lookup on the hot path — `Free` and `UsableSize` recover everything from that single AND.

`page` (`memory.go:99`) is the four-word header at the start of every mapping: `brk` (bump index), `log` (size class), `size` (bytes actually mapped), `used` (live slots). `init()` asserts `sizeof(page) % mallocAllign == 0`.

Two allocation paths, discriminated by `page.log`:

- **`log != 0` — shared page**, for `size <= maxSlotSize` (`1 << (pageSizeLog-2)` = 16 KiB). The request rounds up to a power-of-two slot no smaller than `mallocAllign` (`2*sizeof(uintptr)`); `log` is that exponent. A page holds `cap[log] = pageAvail / (1<<log)` slots handed out by bumping `brk`; `a.pages[log]` is the current carve target and is zeroed once it fills. Freed slots go on `a.lists[log]`, an intrusive doubly-linked list whose `node{prev,next}` is written *inside the free slot* — hence the two-word minimum slot size.
- **`log == 0` — dedicated page**, for anything larger. One allocation per mapping at `pg + headerSize`, released on `Free` (see Page retention). `log == 0` is a safe sentinel precisely because `mallocAllign` rounding makes the smallest real class `log` 4 on 64-bit, 3 on 32-bit.

`a.regs` is the set of live mappings. It is touched only by `mmap`/`unmap`/`Close` (and by tests as a leak check), never on the allocation fast path.

**Page draining** (`UintptrFree`, `memory.go:340-356`): when a shared page's `used` reaches 0, all `brk` of its slots are unlinked from the global `a.lists[log]` before the page is retained or unmapped. Skipping that walk would leave free-list nodes pointing into memory that is no longer on the list — or no longer mapped at all.

**Alignment costs syscalls** (`mmap_unix.go:34`): to get a 64 KiB-aligned result, `mmap` asks for `size + pageSize` and unmaps the misaligned head and the surplus tail — up to three syscalls per page acquisition. `Allocator.mmap` amortizes this for 64 KiB regions by acquiring `slabBatch` of them in one mapping and pooling the rest (unix only — `canCarve`). If the *tail* unmap is rejected by the kernel the code keeps the enlarged mapping rather than failing (fixes bigsort on linux/s390x, cznic/sqlite#207), so `page.size` may legitimately exceed what was requested. The `TODO` at `memory.go:206` is about reusing that surplus, which requires moving `cap` out of `Allocator` and into `page` so capacity becomes per-page.

Windows gets alignment for free — `VirtualAlloc` is 64 KiB-granular — so `mmap_windows.go` just rounds up and returns.

**Platform and width splits**: `pageSizeLog` is declared per platform in `mmap_unix.go` and `mmap_windows.go` (16 in both since `e95c668`). `rawmem`, the huge array type used to fabricate slices over raw memory, is declared per pointer width in `memory32.go` / `memory64.go` — its build-tag list is what needs editing when adding an architecture.

## API surface

Four families over one core, all in `memory.go`: `Malloc`/`Calloc`/`Realloc`/`Free` returning `[]byte`, the `Unsafe*` variants returning `unsafe.Pointer`, the `Uintptr*` variants, and `UsableSize`/`UnsafeUsableSize`/`UintptrUsableSize`. **The `Uintptr*` methods are the implementation; the other two families are thin wrappers** — algorithm changes belong in `UintptrMalloc`/`UintptrFree`/`UintptrRealloc` and nowhere else.

The zero `Allocator` is ready to use and **is not goroutine-safe** — there is no locking anywhere; callers serialize. Slice-returning calls set cap to `usableSize(p)`, which is why `Free` and `Realloc` reslice to `b[:cap(b)]` to recover the block address.

## Tests

`all_test.go` is the entire suite: randomized allocate / verify / free cycles over a 128 MiB quota, in `Small` (≤ 2× OS page, shared pages only) and `Big` (≤ 2× 64 KiB, forces the dedicated-page path) variants, across all three access styles. Sizes and contents come from `mathutil.NewFC32`, a full-cycle PRNG the tests `Seek(0)` to replay — expected values are regenerated rather than stored, and failures reproduce deterministically. Every allocation is filled with its pattern and re-verified, so slot overlap or a wrong `usableSize` surfaces as "corrupted heap". Tests read unexported state (`alloc.regs`, `page.log`), so they must stay in-package.

## Page retention

`UintptrFree` does **not** return an empty region to the OS eagerly. A drained shared page is kept as its size class's carve target; every other empty region — a drained page whose class already has a carve target, or a dedicated page — goes via `release` into `a.freed`, a pool keyed by region size and bounded by `maxFreedSize` (4 MiB plus the high-water mark of the live mapping). `Allocator.mmap` serves requests from the pool before mapping fresh, with request sizes collapsed into region classes (`mmapSize`) so pool hits are likely. `Trim` releases the carve targets and the pool; `Close` releases everything.

This is why every test and benchmark calls `alloc.Trim()` before asserting `Allocs`/`Mmaps`/`Bytes`/`regs` are zero. Asserting after `Close` instead would be vacuous — `Close` ends with `*a = Allocator{}`, so every field reads zero regardless.

`HANDOFF.md` records the diagnosis, the measurements, and the design options that were weighed before picking this policy. Its "Status: open" header is historical; note also that it undercounts the affected test sites as 8 (there are 16 — 10 using `t`, 6 using `b`).

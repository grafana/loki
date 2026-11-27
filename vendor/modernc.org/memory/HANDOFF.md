# HANDOFF: no page-retention hysteresis in `Allocator.UintptrFree`

**Status:** resolved 2026-08-06. Retention is now unconditional and bounded to
one empty page per size class — design option 1 below — combined with the
explicit flush of option 3, `(*Allocator).Trim`. The tests keep their full
`Allocs`/`Mmaps`/`Bytes`/`regs` assertions by calling `Trim` first; moving them
after `Close` would have made them vacuous, since `Close` ends with
`*a = Allocator{}`. Two corrections to what follows: the invariant is asserted
at 16 sites, not 8 (`TestFree` and `TestMalloc` were missed, and 6 of the 16 are
benchmarks using `b`), and retention is per size class, since a global pool
would need the per-page capacity that the `memory.go` TODO still tracks.

Everything below is the original diagnosis, kept for its measurements and
rationale. It describes the code as of `0a6f754`.

## The ask

`UintptrFree` returns a shared page to the OS the moment its last live slot is
freed. There is no hysteresis, so an allocation pattern whose live count for a
size class repeatedly crosses zero pays a full `mmap`/`munmap` round trip per
turnover — in the degenerate case, **one `mmap` + three `munmap`s per single
alloc/free pair**. Decide whether this allocator should retain empty pages, and
under what policy.

## Root cause

`memory.go:222-241`, the tail of `UintptrFree`:

```go
	(*page)(unsafe.Pointer(pg)).used--
	if (*page)(unsafe.Pointer(pg)).used != 0 {
		return nil
	}
	... unlink every node of this page from a.lists[log] ...
	if a.pages[log] == pg {
		a.pages[log] = 0
	}
	...
	return a.unmap(pg)          // <-- unconditional, immediate
```

`used == 0` means "no live slots", which is treated as "give the page back".
`a.pages[log]` (the current carve target) is cleared too, so the next allocation
of that size class must `newSharedPage` → `mmap` again.

Two amplifiers make each turnover cost more than one syscall pair:

- `mmap(size)` (`mmap_unix.go:29`) asks the kernel for `size + pageSize` so it
  can align the result, then unmaps the misaligned head and the tail — up to
  **two trim `munmap`s per `mmap`**, on top of the one that releases the page.
  A 64 KiB page therefore costs a 128 KiB kernel request. Measured totals below
  show all three `munmap`s in the synthetic case and an average of two under the
  real workload.
- With `pageSizeLog = 16` and `maxSlotSizeLog = pageSizeLog - 2`, every size
  class up to **16 KiB** uses shared pages (`memory.go:262`), so this covers the
  whole small/medium range, not just tiny allocations.

## Why it has not bitten before

Long-lived consumers (sqlite et al.) keep a working set alive, so `used` rarely
reaches 0 for a hot size class — the page stays mapped and the free list does its
job. The pathology needs a consumer that repeatedly *drains* a size class.

## Standalone reproducer

No other module needed. Allocate one slot and free it, in a loop:

```go
a := &memory.Allocator{}
defer a.Close()
for i := 0; i < n; i++ {
	p, _ := a.UintptrMalloc(size)
	a.UintptrFree(p)
}
```

`n` = 200000, `strace -c -f -e trace=mmap,munmap`, AMD Ryzen 9 3900X, go1.26.5:

| size | build | `mmap` | `munmap` | syscall time | end state |
|---|---|---|---|---|---|
| 64 B | v1.11.0 | 200,022 | 600,000 | 4.33 s | `Mmaps=0 Bytes=0` |
| 64 B | + retention | 25 | 3 | 0.0002 s | `Mmaps=1 Bytes=65536` |
| 4096 B | v1.11.0 | 200,024 | 600,000 | 4.49 s | `Mmaps=0 Bytes=0` |
| 4096 B | + retention | 24 | 2 | ~0 | `Mmaps=1 Bytes=65536` |

That is ~21.6 µs of kernel time per alloc/free pair, and it is size-independent
across the shared-page range.

## The consumer that hit it

`modernc.org/libquickjs` v0.13.1, which tracks QuickJS release 2026-06-04
(`bellard/quickjs` `3d5e064`). That release added a small-block arena allocator:
31 size classes from 16 to 512 bytes, carved out of `JS_MALLOC_ARENA_SIZE`
= **4096**-byte arenas, and it **frees an arena as soon as its last block is
freed**. QuickJS's free-on-empty policy sits directly on top of this package's
free-on-empty policy and the two resonate: 4096 → `log` 12 → shared page path,
15 arenas per 64 KiB page, cycling constantly.

V8 `v8-v7` benchmark scores (throughput, higher is better), medians of 3
interleaved rounds on a quiet host, go1.26.5, libc v1.74.4:

| bench | libquickjs v0.12.10<br>(QuickJS 2025-09-13) | libquickjs v0.13.1<br>(QuickJS 2026-06-04) | v0.13.1 + retention |
|---|---|---|---|
| Richards | 175.5 | 243.5 | 243 |
| DeltaBlue | 208.5 | **16** | 265.5 |
| Crypto | 118.5 | 157.5 | 166 |
| RayTrace | 285.5 | **195.5** | 464.5 |
| EarleyBoyer | 465.5 | 618.5 | 622.5 |
| RegExp | 99.5 | 189 | 190 |
| Splay | 911.5 | **340.5** | 1149 |
| NavierStokes | 208.5 | 322 | 323 |
| **Score** | **240** | **187.5** | **348.5** |

Retention moves *only* the three allocation-bound benchmarks and leaves the other
five within noise (243.5→243, 189→190, 322→323), which is what makes the
attribution airtight.

Syscall counts and profile for DeltaBlue alone:

| build | `mmap` | `munmap` | syscall time |
|---|---|---|---|
| v0.12.10 (no arena allocator) | 302 | 532 | 0.0001 s |
| v0.13.1 | **1,345,702** | **2,691,328** | **21.1 s** |
| v0.13.1 + retention | 72 | 62 | 0.0008 s |

```
  10.43s 71.73%  internal/runtime/syscall/linux.Syscall6
   2.22s 15.27%  modernc.org/memory.(*Allocator).mmap        (62.79% cum)
   0.03s  0.21%  golang.org/x/sys/unix.munmap                (58.94% cum)
```

72% of a JavaScript benchmark's runtime in `mmap`/`munmap`. Note the third
column of the score table: with retention the *new* QuickJS overtakes the old one
everywhere, which is the expected outcome — the arena allocator is a win once it
stops thrashing.

## The experiment (proof of diagnosis, NOT a shippable patch)

Retain one empty page per size class by recycling it as the carve target instead
of unmapping it:

```diff
 	if a.pages[log] == pg {
 		a.pages[log] = 0
 	}
+	if a.pages[log] == 0 {
+		(*page)(unsafe.Pointer(pg)).brk = 0
+		(*page)(unsafe.Pointer(pg)).used = 0
+		a.pages[log] = pg
+		return nil
+	}
 	if counters {
 		a.Bytes -= (*page)(unsafe.Pointer(pg)).size
 	}
 	return a.unmap(pg)
```

That is the whole change behind every "+ retention" number above. Its bound is
one 64 KiB page per size class per allocator (≤ 64 classes → ≤ 4 MiB worst case,
in practice only for classes actually used).

### Why it cannot be merged as written

`all_test.go` asserts, after freeing everything, at 8 sites (`:137`, `:203`,
`:285`, `:307`, `:331`, `:401`, `:466`, `:544`):

```go
if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
```

Retention breaks `Mmaps`, `Bytes` and `len(regs)` by construction — visible in
the reproducer's end-state column, and `go test ./...` fails with the patch
applied. **This is a policy conflict, not a leak**: `Close()` still walks
`a.regs` and unmaps everything, so nothing escapes the allocator's lifetime. But
the invariant "every byte is back with the OS after the last `Free`" is real,
is asserted by those tests, and consumers may rely on it. Changing it is your
call.

## Design options

1. **Unconditional bounded retention** (the diff above). Simplest, helps every
   consumer automatically, no API change. Costs the invariant; the tests would
   need to assert after `Close`, or a flush would need exposing.
2. **Opt-in.** An `Allocator` field or constructor knob, default off, leaving
   existing behaviour and tests untouched so `libquickjs`/`libc` can turn it on.
   Safest, but each consumer has to know to ask.
3. **Explicit flush.** Always retain; add `(*Allocator).Trim()` and let callers
   decide when to give pages back.
4. **Bounded hysteresis, documented.** As (1), but with the retention count an
   explicit, documented parameter rather than incidentally "one".

Open questions worth settling at the same time:

- Per size class (as prototyped) or one global pool of empty pages? The latter
  caps total retention regardless of how many classes are hot.
- Is the align-by-overallocating in `mmap` worth revisiting? It doubles the
  syscall cost of every page acquisition, so it amplifies exactly this problem.

## Provenance

Found while investigating why `modernc.org/libquickjs` did not show the +42% that
QuickJS 2026-06-04 advertises. Native C on the same host gains +60.8% on v8-v7
(+43.5% with `DIRECT_DISPATCH 0`, the configuration the ccgo build corresponds
to); the Go build instead *regressed* 21.7%. This allocator interaction was the
sole cause: with retention the Go build gains +45.2%, matching the native +43.5%,
and five of the eight benchmarks had been gaining 32–91% all along.

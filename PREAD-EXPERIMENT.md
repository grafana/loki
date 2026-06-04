# TSDB index reader: mmap → on-demand pread (io.ReaderAt)

Status: **experiment / not ready to ship.** This branch replaces the TSDB index
reader's mmap backing with on-demand `pread` (`io.ReaderAt`). It is the
speculative half of the work; the two unconditional wins it depends on
(per-query symbol cache + bounded section reads) are being split out to ship
separately on branch `reduce-tsdb-index-size-perf`.

## What we are doing

Serve the TSDB index (`NewTSDBIndexFromFile`, used by the index-gateway and
ingester) via `os.Open` + `ReadAt` instead of `mmap`.

Changes specific to this branch (on top of the shared wins):
- `index.ReaderAtByteSlice` — a `ByteSlice` backed by an `io.ReaderAt`, reading
  on demand. Hardened for the interface's lack of an error return: bounds guard,
  short-read returns only bytes read (so the decbuf trips `ErrInvalidSize`/CRC
  rather than decoding a zero-padded tail), first-error-wins sticky error.
- `index.NewFileReaderAt` — opens an `*os.File`, surfaces the sticky read error
  after open (TOC/symbol parse can't propagate through `ByteSlice.Range`).
- `single_file_index.go: NewTSDBIndexFromFile` now uses `NewFileReaderAt`.
- Benchmarks: `reader_at_bench_test.go` (mmap vs pread × cache × concurrency,
  + read-amplification), `reader_at_prototype_test.go` (correctness + amplification),
  `index_size_analysis_test.go` (on-disk section-size breakdown).

Shared wins also present here because pread depends on them (they make pread's
read pattern viable): the per-query `SymbolCache` and bounded symbol /
postings-offset-table reads. See "Relationship to the wins branch" below.

## Why

The index-gateway exhibits an mmap-specific pathology under node memory pressure.
Observed live on `loki-ops-002` (cluster `ops-eu-south-0`, 2026-06-02):

- Major page faults (`container_memory_failures_total{failure_type="pgmajfault"}`)
  climbed to ~1000/s for ~24 min on `index-gateway-1`.
- Go P99 scheduler latency (`go_sched_latencies_seconds`) jumped **80 µs → ≥117 ms**
  (bucket-pinned, i.e. a floor) for the duration, then snapped back.
- **CPU stayed flat** (~0.2–0.3 cores) the whole time.

That is the textbook signature of mmap major faults parking goroutine-scheduler
**P**s: a major fault is a CPU exception, not a syscall, so the Go runtime never
hands the P off (no `entersyscall`), and the thread sleeps in `D` state while the
fault is serviced — runnable goroutines starve, CPU drops, and it is invisible to
the CPU profiler. The container's own cgroup was **not** memory-pressured
(`working_set` ~1.1 GB vs 8 GB limit); `mapped_file` churned 6 GB → 750 MB, so the
reclaim driving the re-faults is **node-level** page-cache pressure from
neighbours — meaning more RAM on the gateway alone would not fix it.

`pread` goes through `entersyscall`, so the runtime releases the P during the
read: cold reads no longer stall the scheduler.

## Current findings (benchmarks)

Run:

```
go test -run='^$' -bench=BenchmarkTSDBIndexRead$            -benchmem ./pkg/storage/stores/shipper/indexshipper/tsdb/
go test -run='^$' -bench=BenchmarkTSDBIndexReadAmplification -benchmem ./pkg/storage/stores/shipper/indexshipper/tsdb/
go test -run='^$' -bench=BenchmarkTSDBIndexReadConcurrent -cpu=1,8,11 ./pkg/storage/stores/shipper/indexshipper/tsdb/
```

**Read amplification is bounded and cardinality-flat** (the bounded-reads change
works): `reads/series` ≈ 9 (no cache) / ≈ 3 (cache), flat from 1k → 50k series.

**The SymbolCache is a clear win on both backings** (50k×12, single-threaded):
1.8× faster on mmap, 2.7× faster on pread, ~3× fewer allocs.

**pread's cost, in mmap's best case** (warm page cache, no pressure):

| 50k×12, cache=on | ns/op | allocs/op | B/op |
|---|---|---|---|
| mmap  | 47.6 ms | 150k | 13.5 MB |
| pread | 149 ms | 300k | 58.9 MB |

~3× slower single-threaded. **Worse under concurrency**: with `-cpu=1,8`, mmap
scales ~6.2× (47→7.6 ms) but pread only ~1.45× (130→89 ms) — so pread is ~12×
slower at 8 cores. Likely causes: per-`Range` allocation/GC churn (the ~45 MB/scan
delta) plus a `pread` syscall floor (~3 reads/series × N series) and a per-read
memcpy that mmap (zero-syscall, zero-copy subslicing) never pays.

**Critical caveat:** these benchmarks run against a warm page cache with no memory
pressure — *mmap's best case*, and exactly the scenario where its fault-stalls
never fire. They measure the *cost* of pread, not its intended *benefit*. They
cannot, by construction, show the production win. **Do not make the mmap/pread
decision from these numbers.**

## Next steps (in priority order)

1. **Decisive go/no-go: a memory-pressure + concurrency benchmark.** Run mmap vs
   pread with a cgroup memory limit well below the index size (force reclaim +
   cold faults) and concurrent scans, measuring **tail latency** and
   `go_sched_latencies` — not warm throughput. This is the only test that can
   justify or kill the migration. Everything below is conditional on pread
   winning here.
2. **Buffer pooling to kill the ~45 MB/scan alloc churn.** Preference: per-query
   *statically reusable* buffers (a `recordBuf` + `symBuf` carried on the existing
   per-query object that already threads the `SymbolCache`), **not** a
   concurrency-safe `sync.Pool` — one query's decode loop is single-goroutine, so
   no lock/Put is needed; release == overwrite. Safe targets are the series-record
   and symbol-block reads (fully copied out before reuse; `UvarintStr` copies, so
   labels don't alias). **Not** safe to reuse: postings-list buffers (the
   `bigEndianPostings` iterator aliases them past the call) and `LabelValues`
   return values (yolo'd). This should shrink pread's B/op toward mmap's and
   improve concurrency scaling — but cannot remove the syscall floor.
3. **Reduce reads/series** (lower the syscall floor): coalesce the series-record
   read (`NewDecbufUvarintAt` peek+read) from ~2 reads to 1, taking cache-warm
   reads/series from ~3 toward ~2.
4. **Resolve the `ByteSlice.Range` no-error-return wart properly** if
   productionizing (interface change rippling into `Decbuf`), rather than the
   sticky-error workaround.
5. Note: `builder.go` still uses mmap to read back just-written files — fine, it
   is not the serving path.

## Relationship to the wins branch (`reduce-tsdb-index-size-perf`)

The per-query **SymbolCache** and **bounded reads** (symbol blocks +
postings-offset-table regions) are net improvements independent of the backing
and are being shipped separately on `reduce-tsdb-index-size-perf`. They appear on
this branch too because pread depends on them. When the wins merge to `main`,
rebase this branch and drop the duplicated commits.

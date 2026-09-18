# memory-peak-bench

Measures the **peak resident set size (RSS)** of Go benchmarks, one benchmark per process, so peak
memory can be attributed to a single benchmark. It is not tied to any particular package — you pass
the package and the benchmark name(s) to measure.

The tool:

1. pre-builds the package's test binary once (so per-benchmark processes don't include compilation
   memory),
2. runs **one subprocess per (benchmark, sample)**, and
3. reads each child's peak RSS via `getrusage` (`Rusage.Maxrss`), normalizing Linux kilobytes /
   macOS bytes to bytes.

Unlike in-process `b.ReportAllocs()` numbers (which report *allocation volume*, excluding the runtime
baseline), this reports true process peak RSS — a smaller but more realistic signal, since it
includes the Go runtime floor every benchmark shares.

## Fixtures / caches

By default the tool does a **warmup run per benchmark** before measuring. This populates any on-disk
cache the benchmark builds — for example a large, lazily-generated, content-addressed fixture — so
the one-time generation memory is not counted in the measured peak. This is generic: the tool needs
no knowledge of a benchmark's fixtures, only the "run once, discard, then measure" pass. Disable it
with `-warmup=false` if a benchmark has no such cost.

## Usage

```
go run ./tools/memory-peak-bench -pkg ./pkg/logql/ -bench 'BenchmarkX' -count 3
```

Flags:

- `-pkg` (required) — Go package whose test binary contains the benchmarks (e.g. `./pkg/logql/`).
- `-bench` (required) — comma-separated benchmark name(s). Each is matched exactly: every
  `/`-separated sub-benchmark level is anchored, so pass a full leaf path to measure one leaf, or a
  parent name to measure its whole subtree.
- `-count` (default 5) — samples (subprocesses) per benchmark; the reported value is the median.
- `-benchtime` (default `1x`) — passed through to `-test.benchtime`.
- `-warmup` (default true) — warm each benchmark's caches/fixtures once before measuring.

Output is the median peak RSS per benchmark (plus a first/second ratio when exactly two are given),
and benchstat-parseable `peak-RSS-bytes/op` lines.

### Example: stream-first vs timestamp-first (logql)

```
go run ./tools/memory-peak-bench -pkg ./pkg/logql/ -count 3 -bench \
  'BenchmarkLogQLQueries/mode=per-timestamp/source=store/query=all_5m/latency=0s,BenchmarkLogQLQueries/mode=per-stream/source=store/query=all_5m/latency=0s'
```

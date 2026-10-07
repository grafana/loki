# AGENTS.md

Instructions for coding agents working in this directory.

`BenchmarkLogQLMetricQueries` compares LogQL metric-query execution across three scenarios:

- Chunks with timestamp-first sample order
- Chunks with stream-first order
- Data objects (always stream-first)

Each scenario runs the same set of query shapes with different artificial store latencies. The
benchmark pins `GOMAXPROCS` to 1, so the comparison measures CPU wall time rather than how much a
scenario benefits from parallelism.

## Running the benchmark

```bash
go test -bench BenchmarkLogQLMetricQueries ./pkg/logql/internal/logqlbench/... > bench.out
```

The first run builds the fixture into a content-addressed cache under `os.TempDir()`. Later runs
reuse it as long as the fixture parameters do not change.

Narrow to one or a few leaves with `-bench`, matching the hierarchical name
`query=<expr> (<description>)/scenario=<name>/latency=<name>`:

```bash
go test -bench 'BenchmarkLogQLMetricQueries/query=sum.*high_input.*/scenario=dataobj' \
    ./pkg/logql/internal/logqlbench/...
```

Each case reports:

- `ns/op`, `allocs/op`: the standard Go benchmark time and allocations.
- `store_reqs/op`: object-store calls made to answer the query.
- `store_bytes/op`: bytes read from the object store.
- `store_max_parallel`: peak object-store reads in flight at once, over all iterations. The results
  table reports it at `latency=250ms`. Read it only with injected latency, because a read with no
  delay can end before another starts.

All three `store_*` metrics skip index reads (keys with the prefix `index`): the TSDB index of the
chunk store and the metastore of the data-object store. Index reads also get no injected latency.

## Measuring peak memory

`ns/op` and `allocs/op` describe total work, not how much memory a scenario holds at once. For
peak RSS, use loki-toolbox's `memory-peak-bench`, which runs one benchmark leaf per process so the
peak belongs to a single leaf:

```bash
cd <path-to-loki-toolbox>
go run ./cmd/loki-toolbox pracucci/memory-peak-bench \
    -bench 'BenchmarkLogQLMetricQueries/query=.*high_input.*/scenario=dataobj-stream-first' \
    -samples 3 \
    <path-to-loki>/pkg/logql/internal/logqlbench
```

This benchmark builds each scenario's store lazily (`sync.OnceValues`) and registers its cleanup
on the top-level `*testing.B`, so selecting a single scenario with `-bench` keeps the others
unbuilt and out of the measured peak.

## Generating the PR results tables

`report/` is a separate command. It turns `go test -bench` output into the two markdown tables
showing a comparison between scenarios.

```bash
go run ./pkg/logql/internal/logqlbench/report bench.out
```

It also reads from stdin if given no argument. Values average across any duplicate
(query, scenario, latency) lines, so a `-count>1` run reports correctly too.

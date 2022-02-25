#!/usr/bin/env bash

old=$1
new=$2

echo benchmarks:
echo

benchstat \
<(LOKI_TSDB_PATH="${old}" go test github.com/grafana/loki/tools/tsdb/tsdb-map -bench=BenchmarkQuery -run '^$' -benchmem) \
<(LOKI_TSDB_PATH="${new}" go test github.com/grafana/loki/tools/tsdb/tsdb-map -bench=BenchmarkQuery -run '^$' -benchmem)

echo
echo sizing:
echo

ls -lh "${old}"
ls -lh "${new}"

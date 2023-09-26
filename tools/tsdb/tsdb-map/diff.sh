#!/usr/bin/env bash

# This can be run like:
# ./tools/tsdb/tsdb-map/diff.sh /tmp/loki-scratch/loki-ops-daily.r main $(git rev-parse --abbrev-ref HEAD) <rounds>

boltdb_base=$1
branch_a=$2
branch_b=$3
COUNT="${4:-8}"
echo running "${COUNT}" rounds

echo building from "${branch_a}"
git checkout "${branch_a}"
go run tools/tsdb/tsdb-map/main.go  -source "${boltdb_base}" -dest /tmp/loki-tsdb-a
echo benchmarking "${branch_a}"
LOKI_TSDB_PATH=/tmp/loki-tsdb-a go test github.com/grafana/loki/tools/tsdb/tsdb-map -count="${COUNT}" -bench=BenchmarkQuery -run '^$' -benchmem > /tmp/loki-tsdb-bench-a

echo building from "${branch_b}"
git checkout "${branch_b}"
go run tools/tsdb/tsdb-map/main.go  -source "${boltdb_base}" -dest /tmp/loki-tsdb-b
echo benchmarking "${branch_b}"
LOKI_TSDB_PATH=/tmp/loki-tsdb-b go test github.com/grafana/loki/tools/tsdb/tsdb-map -count="${COUNT}" -bench=BenchmarkQuery -run '^$' -benchmem > /tmp/loki-tsdb-bench-b


echo benchmarks:
echo
benchstat /tmp/loki-tsdb-bench-a /tmp/loki-tsdb-bench-b

echo
echo sizing:
echo

ls -lh /tmp/loki-tsdb-a
ls -lh /tmp/loki-tsdb-b

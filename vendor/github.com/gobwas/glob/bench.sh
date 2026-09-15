#!/bin/bash
#
# Compares the benchmarks of the current branch against a git revision:
#
#	./bench.sh v0.2.3            # all the benchmarks
#	./bench.sh master 'Match'    # the ones matching a -bench regexp
#
# The results are written to *.bench files in the current directory and
# compared with benchstat (go install golang.org/x/perf/cmd/benchstat@latest).

set -eu

prev=$1
what=${2:-.}
curr=$(git rev-parse --abbrev-ref HEAD)
rnd=$(head -c4 </dev/urandom | xxd -p)

file() {
	echo "$rnd-$1.bench" | tr "/" "_"
}

bench() {
	local rev=$1
	local out
	out=$(file "$rev")
	if [[ -e "$out" ]]; then
		echo "Already exists $out"
		return
	fi
	git checkout -q "$rev"
	echo -n "Creating $out... "
	go test ./... -run=none -benchmem -bench="$what" >"$out"
	echo "OK"
	git checkout -q "$curr"
	sleep 5
}

bench "$prev"
bench "$curr"

benchstat "$(file "$prev")" "$(file "$curr")"

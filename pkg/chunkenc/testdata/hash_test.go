package testdata

import (
	"hash/fnv"
	"hash/maphash"
	"testing"
)

var res uint64

func Benchmark_fnv64a(b *testing.B) {

	for n := 0; n < b.N; n++ {
		for i := 0; i < len(LogsBytes); i++ {
			h := fnv.New64a()
			_, _ = h.Write(LogsBytes[i])
			res = h.Sum64()
		}

	}
}

func Benchmark_hashmap(b *testing.B) {
	var h maphash.Hash
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(LogsBytes); i++ {
			_, _ = h.Write(LogsBytes[i])
			res = h.Sum64()
			h.Reset()
		}

	}
}

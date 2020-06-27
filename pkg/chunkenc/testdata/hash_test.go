package testdata

import (
	"hash/fnv"
	"hash/maphash"
	"testing"

	"github.com/segmentio/fasthash/fnv1a"
	"github.com/cespare/xxhash"
	"github.com/stretchr/testify/require"
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

func Benchmark_fnv64a_third_party(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(LogsBytes); i++ {
			res = fnv1a.HashBytes64(LogsBytes[i])
		}
	}
}

func Benchmark_xxhash(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(LogsBytes); i++ {
			res = xxhash.Sum64(LogsBytes[i])
		}
	}
}

func Benchmark_hashmap(b *testing.B) {
	// I discarded hashmap/map as it will compute different value on different binary for the same entry
	var h maphash.Hash
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(LogsBytes); i++ {
			h.SetSeed(maphash.MakeSeed())
			_, _ = h.Write(LogsBytes[i])
			res = h.Sum64()
		}
	}
}

func Test_xxhash_integrity(t *testing.T) {
	data := []uint64{}

	for i := 0; i < len(LogsBytes); i++ {
		data = append(data, xxhash.Sum64(LogsBytes[i]))
	}

	for i := 0; i < len(LogsBytes); i++ {
		require.Equal(t, data[i], xxhash.Sum64(LogsBytes[i]))
	}
}

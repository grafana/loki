package chunkenc

import (
	"hash/fnv"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/segmentio/fasthash/fnv1a"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc/testdata"
)

var res uint64

func Benchmark_fnv64a(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(testdata.LogsBytes); i++ {
			h := fnv.New64a()
			_, _ = h.Write(testdata.LogsBytes[i])
			res = h.Sum64()
		}
	}
}

func Benchmark_fnv64a_third_party(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(testdata.LogsBytes); i++ {
			res = fnv1a.HashBytes64(testdata.LogsBytes[i])
		}
	}
}

func Benchmark_xxhash(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(testdata.LogsBytes); i++ {
			res = xxhash.Sum64(testdata.LogsBytes[i])
		}
	}
}

func Test_xxhash_integrity(t *testing.T) {
	data := []uint64{}

	for i := 0; i < len(testdata.LogsBytes); i++ {
		data = append(data, xxhash.Sum64(testdata.LogsBytes[i]))
	}

	for i := 0; i < len(testdata.LogsBytes); i++ {
		require.Equal(t, data[i], xxhash.Sum64(testdata.LogsBytes[i]))
	}

	unique := map[uint64]struct{}{}
	for i := 0; i < len(testdata.LogsBytes); i++ {
		_, ok := unique[xxhash.Sum64(testdata.LogsBytes[i])]
		require.False(t, ok, string(testdata.LogsBytes[i])) // all lines have been made unique
		unique[xxhash.Sum64(testdata.LogsBytes[i])] = struct{}{}
	}

}

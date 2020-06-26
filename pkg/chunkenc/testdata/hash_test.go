package testdata

import (
	"hash/fnv"
	"hash/maphash"
	"testing"

	"github.com/minio/highwayhash"
	"github.com/segmentio/fasthash/fnv1a"
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

func Benchmark_highway(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for i := 0; i < len(LogsBytes); i++ {
			h, err := highwayhash.New64(LogsBytes[i])
			if err != nil {
				b.Fatal(b)
			}
			res = h.Sum64()
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

// func Test_HashmapIntegrity(t *testing.T) {
// 	data := []uint64{}
// 	var h maphash.Hash
// 	for i := 0; i < len(LogsBytes); i++ {
// 		_, _ = h.Write(LogsBytes[i])
// 		data = append(data, h.Sum64())
// 		h.Reset()
// 	}

// 	var f maphash.Hash
// 	for i := 0; i < len(LogsBytes); i++ {
// 		_, _ = f.Write(LogsBytes[i])
// 		require.Equal(t, data[i], f.Sum64())
// 		h.Reset()
// 	}
// }

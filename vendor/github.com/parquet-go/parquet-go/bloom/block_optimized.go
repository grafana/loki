//go:build (!amd64 || purego) && !parquet.bloom.no_unroll

package bloom

// The functions in this file are optimized versions of the algorithms described
// in https://github.com/apache/parquet-format/blob/master/BloomFilter.md
//
// The functions are manual unrolling of the loops, which yield significant
// performance improvements:
//
// goos: darwin
// goarch: amd64
// pkg: github.com/parquet-go/parquet-go/bloom
// cpu: Intel(R) Core(TM) i9-8950HK CPU @ 2.90GHz
//
// name         old time/op    new time/op      delta
// BlockInsert     327ns ± 1%        12ns ± 4%    -96.47%  (p=0.000 n=9+8)
// BlockCheck      240ns ± 4%        13ns ±28%    -94.75%  (p=0.000 n=8+10)
//
// name         old speed      new speed        delta
// BlockInsert  97.8MB/s ± 1%  2725.0MB/s ±13%  +2686.59%  (p=0.000 n=9+9)
// BlockCheck    133MB/s ± 4%    2587MB/s ±23%  +1838.46%  (p=0.000 n=8+10)
//
// The benchmarks measure throughput based on the byte size of a bloom filter
// block.

func (b *Block) Insert(x uint32) {
	b[0] |= 1 << ((x * salt0) >> 27)
	b[1] |= 1 << ((x * salt1) >> 27)
	b[2] |= 1 << ((x * salt2) >> 27)
	b[3] |= 1 << ((x * salt3) >> 27)
	b[4] |= 1 << ((x * salt4) >> 27)
	b[5] |= 1 << ((x * salt5) >> 27)
	b[6] |= 1 << ((x * salt6) >> 27)
	b[7] |= 1 << ((x * salt7) >> 27)
}

func (b *Block) Check(x uint32) bool {
	return ((b[0] & (1 << ((x * salt0) >> 27))) != 0) &&
		((b[1] & (1 << ((x * salt1) >> 27))) != 0) &&
		((b[2] & (1 << ((x * salt2) >> 27))) != 0) &&
		((b[3] & (1 << ((x * salt3) >> 27))) != 0) &&
		((b[4] & (1 << ((x * salt4) >> 27))) != 0) &&
		((b[5] & (1 << ((x * salt5) >> 27))) != 0) &&
		((b[6] & (1 << ((x * salt6) >> 27))) != 0) &&
		((b[7] & (1 << ((x * salt7) >> 27))) != 0)
}

func (f SplitBlockFilter) insertBulk(x []uint64) {
	for i := range x {
		f.Insert(x[i])
	}
}

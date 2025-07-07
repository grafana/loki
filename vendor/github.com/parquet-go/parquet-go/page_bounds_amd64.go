//go:build !purego

package parquet

// The min-max algorithms combine looking for the min and max values in a single
// pass over the data. While the behavior is the same as calling functions to
// look for the min and max values independently, doing both operations at the
// same time means that we only load the data from memory once. When working on
// large arrays the algorithms are limited by memory bandwidth, computing both
// the min and max together shrinks by half the amount of data read from memory.
//
// The following benchmarks results were highlighting the benefits of combining
// the min-max search, compared to calling the min and max functions separately:
//
// name                 old time/op    new time/op    delta
// BoundsInt64/10240KiB    590µs ±15%     330µs ±10%  -44.01%  (p=0.000 n=10+10)
//
// name                 old speed      new speed      delta
// BoundsInt64/10240KiB 17.9GB/s ±13%  31.8GB/s ±11%  +78.13%  (p=0.000 n=10+10)
//
// As expected, since the functions are memory-bound in those cases, and load
// half as much data, we see significant improvements. The gains are not 2x because
// running more AVX-512 instructions in the tight loops causes more contention
// on CPU ports.
//
// Optimizations being trade offs, using min/max functions independently appears
// to yield better throughput when the data resides in CPU caches:
//
// name             old time/op    new time/op    delta
// BoundsInt64/4KiB   52.1ns ± 0%    46.2ns ± 1%  -12.65%  (p=0.000 n=10+10)
//
// name             old speed      new speed      delta
// BoundsInt64/4KiB 78.6GB/s ± 0%  88.6GB/s ± 1%  +11.23%  (p=0.000 n=10+10)
//
// The probable explanation is that in those cases the algorithms are not
// memory-bound anymore, but limited by contention on CPU ports, and the
// individual min/max functions are able to better parallelize the work due
// to running less instructions per loop. The performance starts to equalize
// around 256KiB, and degrade beyond 1MiB, so we use this threshold to determine
// which approach to prefer.
const combinedBoundsThreshold = 1 * 1024 * 1024

//go:noescape
func combinedBoundsBool(data []bool) (min, max bool)

//go:noescape
func combinedBoundsInt32(data []int32) (min, max int32)

//go:noescape
func combinedBoundsInt64(data []int64) (min, max int64)

//go:noescape
func combinedBoundsUint32(data []uint32) (min, max uint32)

//go:noescape
func combinedBoundsUint64(data []uint64) (min, max uint64)

//go:noescape
func combinedBoundsFloat32(data []float32) (min, max float32)

//go:noescape
func combinedBoundsFloat64(data []float64) (min, max float64)

//go:noescape
func combinedBoundsBE128(data [][16]byte) (min, max []byte)

func boundsInt32(data []int32) (min, max int32) {
	if 4*len(data) >= combinedBoundsThreshold {
		return combinedBoundsInt32(data)
	}
	min = minInt32(data)
	max = maxInt32(data)
	return
}

func boundsInt64(data []int64) (min, max int64) {
	if 8*len(data) >= combinedBoundsThreshold {
		return combinedBoundsInt64(data)
	}
	min = minInt64(data)
	max = maxInt64(data)
	return
}

func boundsUint32(data []uint32) (min, max uint32) {
	if 4*len(data) >= combinedBoundsThreshold {
		return combinedBoundsUint32(data)
	}
	min = minUint32(data)
	max = maxUint32(data)
	return
}

func boundsUint64(data []uint64) (min, max uint64) {
	if 8*len(data) >= combinedBoundsThreshold {
		return combinedBoundsUint64(data)
	}
	min = minUint64(data)
	max = maxUint64(data)
	return
}

func boundsFloat32(data []float32) (min, max float32) {
	if 4*len(data) >= combinedBoundsThreshold {
		return combinedBoundsFloat32(data)
	}
	min = minFloat32(data)
	max = maxFloat32(data)
	return
}

func boundsFloat64(data []float64) (min, max float64) {
	if 8*len(data) >= combinedBoundsThreshold {
		return combinedBoundsFloat64(data)
	}
	min = minFloat64(data)
	max = maxFloat64(data)
	return
}

func boundsBE128(data [][16]byte) (min, max []byte) {
	// TODO: min/max BE128 is really complex to vectorize, and the returns
	// were barely better than doing the min and max independently, for all
	// input sizes. We should revisit if we find ways to improve the min or
	// max algorithms which can be transposed to the combined version.
	min = minBE128(data)
	max = maxBE128(data)
	return
}

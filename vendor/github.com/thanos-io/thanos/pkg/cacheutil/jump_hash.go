// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package cacheutil

// jumpHash consistently chooses a hash bucket number in the range
// [0, numBuckets) for the given key. numBuckets must be >= 1.
//
// Copied from github.com/dgryski/go-jump/blob/master/jump.go (MIT license).
func jumpHash(key uint64, numBuckets int) int32 {
	var b int64 = -1
	var j int64

	for j < int64(numBuckets) {
		b = j
		key = key*2862933555777941757 + 1
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}

	return int32(b)
}

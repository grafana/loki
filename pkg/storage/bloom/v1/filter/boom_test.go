// Original work Copyright (c) 2015 Tyler Treat
// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/tylertreat/BoomFilters/blob/master/boom_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Loki Authors.

package filter

import (
	"encoding/binary"
	"hash/fnv"
	"testing"
)

func BenchmarkHashKernel(b *testing.B) {
	hsh := fnv.New64()
	var data [4]byte

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		binary.LittleEndian.PutUint32(data[:], uint32(i))
		hashKernel(data[:], hsh)
	}
}

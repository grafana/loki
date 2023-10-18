// Original work Copyright (c) 2015 Tyler Treat
// Modified work Copyright (c) 2023 Owen Diehl
// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/tylertreat/BoomFilters/blob/master/partitioned_test.go
// Provenance-includes-location: https://github.com/owen-d/BoomFilters/blob/master/boom/partitioned_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Loki Authors.

package filter

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"strconv"
	"testing"

	"github.com/d4l3k/messagediff"
)

// Ensures that Capacity returns the number of bits, m, in the Bloom filter.
func TestPartitionedBloomCapacity(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.1)

	if capacity := f.Capacity(); capacity != 480 {
		t.Errorf("Expected 480, got %d", capacity)
	}
}

// Ensures that K returns the number of hash functions in the Bloom Filter.
func TestPartitionedBloomK(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.1)

	if k := f.K(); k != 4 {
		t.Errorf("Expected 4, got %d", k)
	}
}

// Ensures that Count returns the number of items added to the filter.
func TestPartitionedCount(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.1)
	for i := 0; i < 10; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}

	if count := f.Count(); count != 10 {
		t.Errorf("Expected 10, got %d", count)
	}
}

// Ensures that EstimatedFillRatio returns the correct approximation.
func TestPartitionedEstimatedFillRatio(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.5)
	for i := 0; i < 100; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}

	if ratio := f.EstimatedFillRatio(); ratio > 0.5 {
		t.Errorf("Expected less than or equal to 0.5, got %f", ratio)
	}
}

// Ensures that FillRatio returns the ratio of set bits.
func TestPartitionedFillRatio(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.1)
	f.Add([]byte(`a`))
	f.Add([]byte(`b`))
	f.Add([]byte(`c`))

	if ratio := f.FillRatio(); ratio != 0.025 {
		t.Errorf("Expected 0.025, got %f", ratio)
	}
}

// Ensures that Test, Add, and TestAndAdd behave correctly.
func TestPartitionedBloomTestAndAdd(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.01)

	// `a` isn't in the filter.
	if f.Test([]byte(`a`)) {
		t.Error("`a` should not be a member")
	}

	if f.Add([]byte(`a`)) != f {
		t.Error("Returned PartitionedBloomFilter should be the same instance")
	}

	// `a` is now in the filter.
	if !f.Test([]byte(`a`)) {
		t.Error("`a` should be a member")
	}

	// `a` is still in the filter.
	if !f.TestAndAdd([]byte(`a`)) {
		t.Error("`a` should be a member")
	}

	// `b` is not in the filter.
	if f.TestAndAdd([]byte(`b`)) {
		t.Error("`b` should not be a member")
	}

	// `a` is still in the filter.
	if !f.Test([]byte(`a`)) {
		t.Error("`a` should be a member")
	}

	// `b` is now in the filter.
	if !f.Test([]byte(`b`)) {
		t.Error("`b` should be a member")
	}

	// `c` is not in the filter.
	if f.Test([]byte(`c`)) {
		t.Error("`c` should not be a member")
	}

	for i := 0; i < 1000000; i++ {
		f.TestAndAdd([]byte(strconv.Itoa(i)))
	}

	// `x` should be a false positive.
	if !f.Test([]byte(`x`)) {
		t.Error("`x` should be a member")
	}
}

// Ensures that Reset sets every bit to zero.
func TestPartitionedBloomReset(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.1)
	for i := 0; i < 1000; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}

	if f.Reset() != f {
		t.Error("Returned PartitionedBloomFilter should be the same instance")
	}

	for _, partition := range f.partitions {
		for i := uint(0); i < partition.Count(); i++ {
			if partition.Get(0) != 0 {
				t.Error("Expected all bits to be unset")
			}
		}
	}
}

// Ensures that PartitionedBloomFilter can be serialized and deserialized without errors.
func TestPartitionedBloomGob(t *testing.T) {
	f := NewPartitionedBloomFilter(100, 0.1)
	for i := 0; i < 1000; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(f); err != nil {
		t.Error(err)
	}

	f2 := NewPartitionedBloomFilter(100, 0.1)
	if err := gob.NewDecoder(&buf).Decode(f2); err != nil {
		t.Error(err)
	}

	if diff, equal := messagediff.PrettyDiff(f, f2); !equal {
		t.Errorf("PartitionedBoomFilter Gob Encode and Decode = %+v; not %+v\n%s", f2, f, diff)
	}
}

func TestPartitionedBloomFilterLazyReader(t *testing.T) {
	filter := NewPartitionedBloomFilter(100, 0.1)
	for i := 0; i < 2000; i++ {
		if i%2 == 0 {
			filter.Add([]byte(strconv.Itoa(i)))
		}
	}

	// We want more than one partition to make sure that we're reading the serialized object correctly.
	if len(filter.partitions) < 2 {
		t.Errorf("Expected more than 1 partition, got %d", len(filter.partitions))
	}

	buf, err := filter.GobEncode()
	if err != nil {
		t.Error(err)
	}

	decodedFilter, n, err := DecodePartitionedBloomFilterFromBuf(buf)
	if err != nil {
		t.Error(err)
	}
	if int(n) != len(buf) {
		t.Errorf("Expected %d bytes read, got %d", len(buf), n)
	}
	reflect.DeepEqual(filter, decodedFilter)
}

func BenchmarkPartitionedBloomAdd(b *testing.B) {
	b.StopTimer()
	f := NewPartitionedBloomFilter(100000, 0.1)
	data := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		data[i] = []byte(strconv.Itoa(i))
	}
	b.StartTimer()

	for n := 0; n < b.N; n++ {
		f.Add(data[n])
	}
}

func BenchmarkPartitionedBloomTest(b *testing.B) {
	b.StopTimer()
	f := NewPartitionedBloomFilter(100000, 0.1)
	data := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		data[i] = []byte(strconv.Itoa(i))
	}
	b.StartTimer()

	for n := 0; n < b.N; n++ {
		f.Test(data[n])
	}
}

func BenchmarkPartitionedBloomTestAndAdd(b *testing.B) {
	b.StopTimer()
	f := NewPartitionedBloomFilter(100000, 0.1)
	data := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		data[i] = []byte(strconv.Itoa(i))
	}
	b.StartTimer()

	for n := 0; n < b.N; n++ {
		f.TestAndAdd(data[n])
	}
}

func BenchmarkPartitionedFillRatio(b *testing.B) {
	b.StopTimer()
	f := NewPartitionedBloomFilter(100000, 0.1)
	b.StartTimer()

	for n := 0; n < b.N; n++ {
		f.FillRatio()
	}
}

// Original work Copyright (c) 2015 Tyler Treat
// Modified work Copyright (c) 2023 Owen Diehl
// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/tylertreat/BoomFilters/blob/master/scalable_test.go
// Provenance-includes-location: https://github.com/owen-d/BoomFilters/blob/master/boom/scalable_test.go
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

// Ensures that K returns the number of hash functions used in each Bloom
// filter.
func TestScalableBloomK(t *testing.T) {
	f := NewScalableBloomFilter(10, 0.1, 0.8)

	if k := f.K(); k != 4 {
		t.Errorf("Expected 4, got %d", k)
	}
}

// Ensures that FillRatio returns the average fill ratio of the contained
// filters.
func TestScalableFillRatio(t *testing.T) {
	f := NewScalableBloomFilter(100, 0.1, 0.8)
	for i := 0; i < 200; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}

	if ratio := f.FillRatio(); ratio > 0.5 {
		t.Errorf("Expected less than or equal to 0.5, got %f", ratio)
	}
}

// Ensures that Test, Add, and TestAndAdd behave correctly.
func TestScalableBloomTestAndAdd(t *testing.T) {
	f := NewScalableBloomFilter(1000, 0.01, 0.8)

	// `a` isn't in the filter.
	if f.Test([]byte(`a`)) {
		t.Error("`a` should not be a member")
	}

	if f.Add([]byte(`a`)) != f {
		t.Error("Returned ScalableBloomFilter should be the same instance")
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

	for i := 0; i < 10000; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}

	// `x` should not be a false positive.
	if f.Test([]byte(`x`)) {
		t.Error("`x` should not be a member")
	}
}

// Ensures that ScalableBloomFilter can be serialized and deserialized without errors.
func TestScalableBloomGob(t *testing.T) {
	f := NewScalableBloomFilter(10, 0.1, 0.8)
	for i := 0; i < 1000; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(f); err != nil {
		t.Error(err)
	}

	f2 := NewScalableBloomFilter(10, 0.1, 0.8)
	if err := gob.NewDecoder(&buf).Decode(f2); err != nil {
		t.Error(err)
	}

	if diff, equal := messagediff.PrettyDiff(f, f2); !equal {
		t.Errorf("ScalableBoomFilter Gob Encode and Decode = %+v; not %+v\n%s", f2, f, diff)
	}
}

func TestScalableBloomFilterLazyReader(t *testing.T) {
	filter := NewScalableBloomFilter(10, 0.1, 0.8)
	for i := 0; i < 2000; i++ {
		if i%2 == 0 {
			filter.Add([]byte(strconv.Itoa(i)))
		}
	}

	// We want more than one filter to make sure that we're reading the serialized object correctly.
	if len(filter.filters) < 2 {
		t.Errorf("Expected more than 1 filter, got %d", len(filter.filters))
	}

	buf, err := filter.GobEncode()
	if err != nil {
		t.Error(err)
	}

	var decodedFilter ScalableBloomFilter
	n, err := decodedFilter.DecodeFrom(buf)

	if err != nil {
		t.Error(err)
	}
	if int(n) != len(buf) {
		t.Errorf("Expected %d bytes read, got %d", len(buf), n)
	}
	reflect.DeepEqual(filter, decodedFilter)
}

func BenchmarkScalableBloomAdd(b *testing.B) {
	b.StopTimer()
	f := NewScalableBloomFilter(100000, 0.1, 0.8)
	data := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		data[i] = []byte(strconv.Itoa(i))
	}
	b.StartTimer()

	for n := 0; n < b.N; n++ {
		f.Add(data[n])
	}
}

func BenchmarkScalableBloomTest(b *testing.B) {
	b.StopTimer()
	f := NewScalableBloomFilter(100000, 0.1, 0.8)
	data := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		data[i] = []byte(strconv.Itoa(i))
	}
	b.StartTimer()

	for n := 0; n < b.N; n++ {
		f.Test(data[n])
	}
}

func BenchmarkScalableBloomTestAndAdd(b *testing.B) {
	b.StopTimer()
	f := NewScalableBloomFilter(100000, 0.1, 0.8)
	data := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		data[i] = []byte(strconv.Itoa(i))
	}
	b.StartTimer()

	for n := 0; n < b.N; n++ {
		f.TestAndAdd(data[n])
	}
}

func BenchmarkRead(b *testing.B) {
	b.StopTimer()
	keys := int(1e6)
	f := NewScalableBloomFilter(1<<10, 0.1, 0.8)
	for i := 0; i < keys; i++ {
		f.Add([]byte(strconv.Itoa(i)))
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		f.Test([]byte(strconv.Itoa(i)))
	}
}

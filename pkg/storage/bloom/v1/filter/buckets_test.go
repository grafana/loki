// Original work Copyright (c) 2015 Tyler Treat
// Modified work Copyright (c) 2023 Owen Diehl
// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/tylertreat/BoomFilters/blob/master/buckets_test.go
// Provenance-includes-location: https://github.com/owen-d/BoomFilters/blob/master/boom/buckets_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Loki Authors.

package filter

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"

	"github.com/d4l3k/messagediff"
)

// Ensures that MaxBucketValue returns the correct maximum based on the bucket
// size.
func TestMaxBucketValue(t *testing.T) {
	b := NewBuckets(10, 2)

	if maxVal := b.MaxBucketValue(); maxVal != 3 {
		t.Errorf("Expected 3, got %d", maxVal)
	}
}

// Ensures that Count returns the number of buckets.
func TestBucketsCount(t *testing.T) {
	b := NewBuckets(10, 2)

	if count := b.Count(); count != 10 {
		t.Errorf("Expected 10, got %d", count)
	}
}

// Ensures that Increment increments the bucket value by the correct delta and
// clamps to zero and the maximum, Get returns the correct bucket value, and
// Set sets the bucket value correctly.
func TestBucketsIncrementAndGetAndSet(t *testing.T) {
	b := NewBuckets(5, 2)

	if b.Increment(0, 1) != b {
		t.Error("Returned Buckets should be the same instance")
	}

	if v := b.Get(0); v != 1 {
		t.Errorf("Expected 1, got %d", v)
	}

	b.Increment(1, -1)

	if v := b.Get(1); v != 0 {
		t.Errorf("Expected 0, got %d", v)
	}

	if b.Set(2, 100) != b {
		t.Error("Returned Buckets should be the same instance")
	}

	if v := b.Get(2); v != 3 {
		t.Errorf("Expected 3, got %d", v)
	}

	b.Increment(3, 2)

	if v := b.Get(3); v != 2 {
		t.Errorf("Expected 2, got %d", v)
	}
}

// Ensures that Reset restores the Buckets to the original state.
func TestBucketsReset(t *testing.T) {
	b := NewBuckets(5, 2)
	for i := 0; i < 5; i++ {
		b.Increment(uint(i), 1)
	}

	if b.Reset() != b {
		t.Error("Returned Buckets should be the same instance")
	}

	for i := 0; i < 5; i++ {
		if c := b.Get(uint(i)); c != 0 {
			t.Errorf("Expected 0, got %d", c)
		}
	}
}

// Ensures that Buckets can be serialized and deserialized without errors.
func TestBucketsGob(t *testing.T) {
	b := NewBuckets(5, 2)
	for i := 0; i < 5; i++ {
		b.Increment(uint(i), 1)
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(b); err != nil {
		t.Error(err)
	}

	b2 := NewBuckets(5, 2)
	if err := gob.NewDecoder(&buf).Decode(b2); err != nil {
		t.Error(err)
	}

	if diff, equal := messagediff.PrettyDiff(b, b2); !equal {
		t.Errorf("Buckets Gob Encode and Decode = %+v; not %+v\n%s", b2, b, diff)
	}
}

func TestBucketsLazyReader(t *testing.T) {
	filter := NewBuckets(10000, 10)
	for i := 0; i < 2000; i++ {
		if i%2 == 0 {
			filter.Set(uint(i), 1)
		}
	}

	buf, err := filter.GobEncode()
	if err != nil {
		t.Error(err)
	}

	var decodedFilter Buckets
	n, err := decodedFilter.DecodeFrom(buf)
	if err != nil {
		t.Error(err)
	}
	if int(n) != len(buf) {
		t.Errorf("Expected %d bytes read, got %d", len(buf), n)
	}
	reflect.DeepEqual(filter, decodedFilter)
}

func BenchmarkBucketsIncrement(b *testing.B) {
	buckets := NewBuckets(10000, 10)
	for n := 0; n < b.N; n++ {
		buckets.Increment(uint(n)%10000, 1)
	}
}

func BenchmarkBucketsSet(b *testing.B) {
	buckets := NewBuckets(10000, 10)
	for n := 0; n < b.N; n++ {
		buckets.Set(uint(n)%10000, 1)
	}
}

func BenchmarkBucketsGet(b *testing.B) {
	b.StopTimer()
	buckets := NewBuckets(10000, 10)
	for n := 0; n < b.N; n++ {
		buckets.Set(uint(n)%10000, 1)
	}
	b.StartTimer()
	for n := 0; n < b.N; n++ {
		buckets.Get(uint(n) % 10000)
	}
}

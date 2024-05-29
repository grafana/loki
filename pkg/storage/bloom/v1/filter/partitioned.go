/*
Original work Copyright (c) 2013 zhenjl
Modified work Copyright (c) 2015 Tyler Treat
Modified work Copyright (c) 2023 Owen Diehl
SPDX-License-Identifier: AGPL-3.0-only
Provenance-includes-location: https://github.com/tylertreat/BoomFilters/blob/master/partitioned.go
Provenance-includes-location: https://github.com/owen-d/BoomFilters/blob/master/boom/partitioned.go
Provenance-includes-license: Apache-2.0
Provenance-includes-license: MIT
Provenance-includes-copyright: The Loki Authors.

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
of the Software, and to permit persons to whom the Software is furnished to do
so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.
*/

package filter

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"math"
)

func getNewHashFunction() hash.Hash64 {
	return fnv.New64()
}

// PartitionedBloomFilter implements a variation of a classic Bloom filter as
// described by Almeida, Baquero, Preguica, and Hutchison in Scalable Bloom
// Filters:
//
// http://gsd.di.uminho.pt/members/cbm/ps/dbloom.pdf
//
// This filter works by partitioning the M-sized bit array into k slices of
// size m = M/k bits. Each hash function produces an index over m for its
// respective slice. Thus, each element is described by exactly k bits, meaning
// the distribution of false positives is uniform across all elements.
type PartitionedBloomFilter struct {
	partitions     []*Buckets  // partitioned filter data
	hash           hash.Hash64 // hash function (kernel for all k functions)
	m              uint        // filter size (divided into k partitions)
	k              uint        // number of hash functions (and partitions)
	s              uint        // partition size (m / k)
	estimatedCount uint        // number of distinct items added
	optimalCount   uint        // optimal number of distinct items that can be stored in this filter
}

// NewPartitionedBloomFilterWithCapacity creates a new partitioned Bloom filter
// with a specific capacity
func NewPartitionedBloomFilterWithCapacity(m uint, fpRate float64) *PartitionedBloomFilter {
	var (
		k = OptimalK(fpRate)
		s = uint(math.Ceil(float64(m) / float64(k)))
	)
	partitions := make([]*Buckets, k)

	for i := uint(0); i < k; i++ {
		partitions[i] = NewBuckets(s, 1)
	}

	return &PartitionedBloomFilter{
		partitions:   partitions,
		hash:         getNewHashFunction(),
		m:            m,
		k:            k,
		s:            s,
		optimalCount: estimatedCount(m, fillRatio),
	}
}

// NewPartitionedBloomFilter creates a new partitioned Bloom filter optimized
// to store n items with a specified target false-positive rate.
func NewPartitionedBloomFilter(n uint, fpRate float64) *PartitionedBloomFilter {
	m := OptimalM(n, fpRate)
	return NewPartitionedBloomFilterWithCapacity(m, fpRate)
}

// Capacity returns the Bloom filter capacity, m.
func (p *PartitionedBloomFilter) Capacity() uint {
	return p.m
}

// K returns the number of hash functions.
func (p *PartitionedBloomFilter) K() uint {
	return p.k
}

// Count returns the number of items added to the filter.
func (p *PartitionedBloomFilter) Count() uint {
	return p.estimatedCount
}

// EstimatedFillRatio returns the current estimated ratio of set bits.
func (p *PartitionedBloomFilter) EstimatedFillRatio() float64 {
	return 1 - math.Exp(-float64(p.estimatedCount)/float64(p.s))
}

// FillRatio returns the average ratio of set bits across all partitions.
func (p *PartitionedBloomFilter) FillRatio() float64 {
	t := float64(0)
	for i := uint(0); i < p.k; i++ {
		var sum int
		sum += p.partitions[i].PopCount()
		t += (float64(sum) / float64(p.s))
	}
	return t / float64(p.k)
}

// Since duplicates can be added to a bloom filter,
// we update the count via the following formula via
// https://gsd.di.uminho.pt/members/cbm/ps/dbloom.pdf
//
// n ≈ −m ln(1 − p).
// This prevents the count from being off by a large
// amount when duplicates are added.
// NOTE: this calls FillRatio which calculates the hamming weight from
// across all bits which can be relatively expensive.
// Returns current exact fill ratio
func (p *PartitionedBloomFilter) UpdateCount() float64 {
	fillRatio := p.FillRatio()
	p.estimatedCount = estimatedCount(p.m, fillRatio)
	return fillRatio
}

// OptimalCount returns the optimal number of distinct items that can be stored
// in this filter.
func (p *PartitionedBloomFilter) OptimalCount() uint {
	return p.optimalCount
}

// Test will test for membership of the data and returns true if it is a
// member, false if not. This is a probabilistic test, meaning there is a
// non-zero probability of false positives but a zero probability of false
// negatives. Due to the way the filter is partitioned, the probability of
// false positives is uniformly distributed across all elements.
func (p *PartitionedBloomFilter) Test(data []byte) bool {
	lower, upper := hashKernel(data, p.hash)

	// If any of the K partition bits are not set, then it's not a member.
	for i := uint(0); i < p.k; i++ {
		if p.partitions[i].Get((uint(lower)+uint(upper)*i)%p.s) == 0 {
			return false
		}
	}

	return true
}

// Add will add the data to the Bloom filter. It returns the filter to allow
// for chaining.
func (p *PartitionedBloomFilter) Add(data []byte) Filter {
	lower, upper := hashKernel(data, p.hash)

	// Set the K partition bits.
	for i := uint(0); i < p.k; i++ {
		p.partitions[i].Set((uint(lower)+uint(upper)*i)%p.s, 1)
	}

	p.estimatedCount++
	return p
}

// TestAndAdd is equivalent to calling Test followed by Add. It returns true if
// the data is a member, false if not.
func (p *PartitionedBloomFilter) TestAndAdd(data []byte) bool {
	lower, upper := hashKernel(data, p.hash)
	member := true

	// If any of the K partition bits are not set, then it's not a member.
	for i := uint(0); i < p.k; i++ {
		idx := (uint(lower) + uint(upper)*i) % p.s
		if p.partitions[i].Get(idx) == 0 {
			member = false
		}
		p.partitions[i].Set(idx, 1)
	}

	p.estimatedCount++
	return member
}

// Reset restores the Bloom filter to its original state. It returns the filter
// to allow for chaining.
func (p *PartitionedBloomFilter) Reset() *PartitionedBloomFilter {
	for _, partition := range p.partitions {
		partition.Reset()
	}
	return p
}

// SetHash sets the hashing function used in the filter.
// For the effect on false positive rates see: https://github.com/tylertreat/BoomFilters/pull/1
func (p *PartitionedBloomFilter) SetHash(h hash.Hash64) {
	p.hash = h
}

// WriteTo writes a binary representation of the PartitionedBloomFilter to an i/o stream.
// It returns the number of bytes written.
func (p *PartitionedBloomFilter) WriteTo(stream io.Writer) (int64, error) {
	err := binary.Write(stream, binary.BigEndian, uint64(p.m))
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, uint64(p.k))
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, uint64(p.s))
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, uint64(p.estimatedCount))
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, uint64(p.optimalCount))
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, uint64(len(p.partitions)))
	if err != nil {
		return 0, err
	}
	var numBytes int64
	for _, partition := range p.partitions {
		num, err := partition.WriteTo(stream)
		if err != nil {
			return 0, err
		}
		numBytes += num
	}
	return numBytes + int64(5*binary.Size(uint64(0))), err
}

func (p *PartitionedBloomFilter) readParams(stream io.Reader) (int64, error) {
	var m, k, s, estimatedCount, optimalCount uint64

	err := binary.Read(stream, binary.BigEndian, &m)
	if err != nil {
		return 0, err
	}
	err = binary.Read(stream, binary.BigEndian, &k)
	if err != nil {
		return 0, err
	}
	err = binary.Read(stream, binary.BigEndian, &s)
	if err != nil {
		return 0, err
	}
	err = binary.Read(stream, binary.BigEndian, &estimatedCount)
	if err != nil {
		return 0, err
	}
	err = binary.Read(stream, binary.BigEndian, &optimalCount)
	if err != nil {
		return 0, err
	}

	p.m = uint(m)
	p.k = uint(k)
	p.s = uint(s)
	p.estimatedCount = uint(estimatedCount)
	p.optimalCount = uint(optimalCount)

	// Bytes read: m, k, s, estimatedCount, optimalCount. All uint64
	return int64(5 * binary.Size(uint64(0))), nil
}

// ReadFrom reads a binary representation of PartitionedBloomFilter (such as might
// have been written by WriteTo()) from an i/o stream. It returns the number
// of bytes read.
func (p *PartitionedBloomFilter) ReadFrom(stream io.Reader) (int64, error) {
	bytesParams, err := p.readParams(stream)
	if err != nil {
		return 0, err
	}

	var len uint64 // nolint:revive
	err = binary.Read(stream, binary.BigEndian, &len)
	if err != nil {
		return 0, err
	}
	var numBytes int64
	partitions := make([]*Buckets, len)
	for i := range partitions {
		buckets := &Buckets{}
		num, err := buckets.ReadFrom(stream)
		if err != nil {
			return 0, err
		}
		numBytes += num
		partitions[i] = buckets
	}
	p.partitions = partitions
	// Bytes read: bytesParams + len (uint64), partitions (numBytes)
	return bytesParams + int64(binary.Size(uint64(0))) + numBytes, nil
}

// DecodeFrom reads a binary representation of PartitionedBloomFilter (such as might
// have been written by WriteTo()) from a buffer.
// Whereas ReadFrom() calls Buckets.ReadFrom() hence making a copy of the data,
// DecodeFrom calls Buckets.DecodeFrom which keeps a reference to the original data buffer.
func (p *PartitionedBloomFilter) DecodeFrom(data []byte) (int64, error) {
	bytesParams, err := p.readParams(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("failed to read PartitionedBloomFilter params from buffer: %w", err)
	}

	lenBuckets := int64(binary.BigEndian.Uint64(data[bytesParams:]))
	bucketStartOffset := bytesParams + int64(binary.Size(uint64(0)))

	partitions := make([]*Buckets, lenBuckets)
	for i := range partitions {
		var b Buckets
		n, err := b.DecodeFrom(data[bucketStartOffset:])
		if err != nil {
			return 0, fmt.Errorf("failed to decode bucket %d from buffer: %w", i, err)
		}
		bucketStartOffset += n
		partitions[i] = &b
	}
	p.partitions = partitions

	// The length is the bucketStartOffset since we updated it in the last
	// iteration of the loop above with the length of the last bucket.
	return bucketStartOffset, nil
}

// GobEncode implements gob.GobEncoder interface.
func (p *PartitionedBloomFilter) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	_, err := p.WriteTo(&buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder interface.
func (p *PartitionedBloomFilter) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	_, err := p.ReadFrom(buf)

	return err
}

// Original work Copyright (c) 2013 zhenjl
// Modified work Copyright (c) 2015 Tyler Treat
// Modified work Copyright (c) 2023 Owen Diehl
// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/tylertreat/BoomFilters/blob/master/buckets.go
// Provenance-includes-location: https://github.com/owen-d/BoomFilters/blob/master/boom/buckets.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Loki Authors.

package filter

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
)

// Buckets is a fast, space-efficient array of buckets where each bucket can
// store up to a configured maximum value.
type Buckets struct {
	data       []byte
	bucketSize uint8
	max        uint8
	count      uint
}

// NewBuckets creates a new Buckets with the provided number of buckets where
// each bucket is the specified number of bits.
func NewBuckets(count uint, bucketSize uint8) *Buckets {
	return &Buckets{
		count:      count,
		data:       make([]byte, (count*uint(bucketSize)+7)/8),
		bucketSize: bucketSize,
		max:        (1 << bucketSize) - 1,
	}
}

// MaxBucketValue returns the maximum value that can be stored in a bucket.
func (b *Buckets) MaxBucketValue() uint8 {
	return b.max
}

// Count returns the number of buckets.
func (b *Buckets) Count() uint {
	return b.count
}

// Increment will increment the value in the specified bucket by the provided
// delta. A bucket can be decremented by providing a negative delta. The value
// is clamped to zero and the maximum bucket value. Returns itself to allow for
// chaining.
func (b *Buckets) Increment(bucket uint, delta int32) *Buckets {
	val := int32(b.getBits(bucket*uint(b.bucketSize), uint(b.bucketSize))) + delta
	if val > int32(b.max) {
		val = int32(b.max)
	} else if val < 0 {
		val = 0
	}

	b.setBits(uint32(bucket)*uint32(b.bucketSize), uint32(b.bucketSize), uint32(val))
	return b
}

// Set will set the bucket value. The value is clamped to zero and the maximum
// bucket value. Returns itself to allow for chaining.
func (b *Buckets) Set(bucket uint, value uint8) *Buckets {
	if value > b.max {
		value = b.max
	}

	b.setBits(uint32(bucket)*uint32(b.bucketSize), uint32(b.bucketSize), uint32(value))
	return b
}

// Get returns the value in the specified bucket.
func (b *Buckets) Get(bucket uint) uint32 {
	return b.getBits(bucket*uint(b.bucketSize), uint(b.bucketSize))
}

func (b *Buckets) PopCount() (count int) {
	for _, x := range b.data {
		count += bits.OnesCount8(x)
	}
	return count
}

// Reset restores the Buckets to the original state. Returns itself to allow
// for chaining.
func (b *Buckets) Reset() *Buckets {
	b.data = make([]byte, (b.count*uint(b.bucketSize)+7)/8)
	return b
}

// getBits returns the bits at the specified offset and length.
func (b *Buckets) getBits(offset, length uint) uint32 {
	byteIndex := offset / 8
	byteOffset := offset % 8
	if byteOffset+length > 8 {
		rem := 8 - byteOffset
		return b.getBits(offset, rem) | (b.getBits(offset+rem, length-rem) << rem)
	}
	bitMask := uint32((1 << length) - 1)
	return (uint32(b.data[byteIndex]) & (bitMask << byteOffset)) >> byteOffset
}

// setBits sets bits at the specified offset and length.
func (b *Buckets) setBits(offset, length, bits uint32) {
	byteIndex := offset / 8
	byteOffset := offset % 8
	if byteOffset+length > 8 {
		rem := 8 - byteOffset
		b.setBits(offset, rem, bits)
		b.setBits(offset+rem, length-rem, bits>>rem)
		return
	}
	bitMask := uint32((1 << length) - 1)
	b.data[byteIndex] = byte(uint32(b.data[byteIndex]) & ^(bitMask << byteOffset))
	b.data[byteIndex] = byte(uint32(b.data[byteIndex]) | ((bits & bitMask) << byteOffset))
}

// WriteTo writes a binary representation of Buckets to an i/o stream.
// It returns the number of bytes written.
func (b *Buckets) WriteTo(stream io.Writer) (int64, error) {
	err := binary.Write(stream, binary.BigEndian, b.bucketSize)
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, b.max)
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, uint64(b.count))
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, uint64(len(b.data)))
	if err != nil {
		return 0, err
	}
	err = binary.Write(stream, binary.BigEndian, b.data)
	if err != nil {
		return 0, err
	}
	return int64(len(b.data) + 2*binary.Size(uint8(0)) + 2*binary.Size(uint64(0))), err
}

func (b *Buckets) readParams(stream io.Reader) (int64, error) {
	var bucketSize, maximum uint8
	var count uint64

	err := binary.Read(stream, binary.BigEndian, &bucketSize)
	if err != nil {
		return 0, err
	}
	err = binary.Read(stream, binary.BigEndian, &maximum)
	if err != nil {
		return 0, err
	}
	err = binary.Read(stream, binary.BigEndian, &count)
	if err != nil {
		return 0, err
	}

	b.bucketSize = bucketSize
	b.max = maximum
	b.count = uint(count)

	// Bytes read: bucketSize (uint8), max (uint8), count (uint64)
	return int64(2*binary.Size(uint8(0)) + binary.Size(uint64(0))), nil
}

// ReadFrom reads a binary representation of Buckets (such as might
// have been written by WriteTo()) from an i/o stream. It returns the number
// of bytes read.
func (b *Buckets) ReadFrom(stream io.Reader) (int64, error) {
	bytesParams, err := b.readParams(stream)
	if err != nil {
		return 0, err
	}

	var len uint64 // nolint:revive
	err = binary.Read(stream, binary.BigEndian, &len)
	if err != nil {
		return 0, err
	}
	data := make([]byte, len)
	err = binary.Read(stream, binary.BigEndian, &data)
	if err != nil {
		return 0, err
	}
	b.data = data

	// Bytes read: bytesParams + dataLen (uint64) + data ([]byte)
	return bytesParams + int64(binary.Size(uint64(0))) + int64(len), nil
}

// DecodeFrom reads a binary representation of Buckets (such as might
// have been written by WriteTo()) from a buffer.
// Whereas ReadFrom() reads the entire data into memory and
// makes a copy of the data buffer, DecodeFrom keeps a reference
// to the original data buffer and can only be used to Test.
func (b *Buckets) DecodeFrom(data []byte) (int64, error) {
	bytesParams, err := b.readParams(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("failed to read Buckets params from buffer: %w", err)
	}

	dataLen := int64(binary.BigEndian.Uint64(data[bytesParams:]))
	dataStart := bytesParams + int64(binary.Size(uint64(0)))
	dataEnd := dataStart + dataLen
	b.data = data[dataStart:dataEnd]

	return dataEnd, nil
}

// GobEncode implements gob.GobEncoder interface.
func (b *Buckets) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	_, err := b.WriteTo(&buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GobDecode implements gob.GobDecoder interface.
func (b *Buckets) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	_, err := b.ReadFrom(buf)

	return err
}

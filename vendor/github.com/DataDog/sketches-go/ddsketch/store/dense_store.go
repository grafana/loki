// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package store

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	enc "github.com/DataDog/sketches-go/ddsketch/encoding"
	"github.com/DataDog/sketches-go/ddsketch/pb/sketchpb"
)

const (
	arrayLengthOverhead        = 64
	arrayLengthGrowthIncrement = 0.1

	// Grow the bins with an extra growthBuffer bins to prevent growing too often
	growthBuffer = 128
)

// DenseStore is a dynamically growing contiguous (non-sparse) store. The number of bins are
// bound only by the size of the slice that can be allocated.
type DenseStore struct {
	bins     []float64
	count    float64
	offset   int
	minIndex int
	maxIndex int
}

func NewDenseStore() *DenseStore {
	return &DenseStore{minIndex: math.MaxInt32, maxIndex: math.MinInt32}
}

func (s *DenseStore) Add(index int) {
	s.AddWithCount(index, float64(1))
}

func (s *DenseStore) AddBin(bin Bin) {
	if bin.count == 0 {
		return
	}
	s.AddWithCount(bin.index, bin.count)
}

func (s *DenseStore) AddWithCount(index int, count float64) {
	if count == 0 {
		return
	}
	arrayIndex := s.normalize(index)
	s.bins[arrayIndex] += count
	s.count += count
}

// Normalize the store, if necessary, so that the counter of the specified index can be updated.
func (s *DenseStore) normalize(index int) int {
	if index < s.minIndex || index > s.maxIndex {
		s.extendRange(index, index)
	}
	return index - s.offset
}

func (s *DenseStore) getNewLength(newMinIndex, newMaxIndex int) int {
	desiredLength := newMaxIndex - newMinIndex + 1
	return int((float64(desiredLength+arrayLengthOverhead-1)/arrayLengthGrowthIncrement + 1) * arrayLengthGrowthIncrement)
}

func (s *DenseStore) extendRange(newMinIndex, newMaxIndex int) {

	newMinIndex = min(newMinIndex, s.minIndex)
	newMaxIndex = max(newMaxIndex, s.maxIndex)

	if s.IsEmpty() {
		initialLength := s.getNewLength(newMinIndex, newMaxIndex)
		s.bins = append(s.bins, make([]float64, initialLength)...)
		s.offset = newMinIndex
		s.minIndex = newMinIndex
		s.maxIndex = newMaxIndex
		s.adjust(newMinIndex, newMaxIndex)
	} else if newMinIndex >= s.offset && newMaxIndex < s.offset+len(s.bins) {
		s.minIndex = newMinIndex
		s.maxIndex = newMaxIndex
	} else {
		// To avoid shifting too often when nearing the capacity of the array,
		// we may grow it before we actually reach the capacity.
		newLength := s.getNewLength(newMinIndex, newMaxIndex)
		if newLength > len(s.bins) {
			s.bins = append(s.bins, make([]float64, newLength-len(s.bins))...)
		}
		s.adjust(newMinIndex, newMaxIndex)
	}
}

// Adjust bins, offset, minIndex and maxIndex, without resizing the bins slice in order to make it fit the
// specified range.
func (s *DenseStore) adjust(newMinIndex, newMaxIndex int) {
	s.centerCounts(newMinIndex, newMaxIndex)
}

func (s *DenseStore) centerCounts(newMinIndex, newMaxIndex int) {
	midIndex := newMinIndex + (newMaxIndex-newMinIndex+1)/2
	s.shiftCounts(s.offset + len(s.bins)/2 - midIndex)
	s.minIndex = newMinIndex
	s.maxIndex = newMaxIndex
}

func (s *DenseStore) shiftCounts(shift int) {
	minArrIndex := s.minIndex - s.offset
	maxArrIndex := s.maxIndex - s.offset
	copy(s.bins[minArrIndex+shift:], s.bins[minArrIndex:maxArrIndex+1])
	if shift > 0 {
		s.resetBins(s.minIndex, s.minIndex+shift-1)
	} else {
		s.resetBins(s.maxIndex+shift+1, s.maxIndex)
	}
	s.offset -= shift
}

func (s *DenseStore) resetBins(fromIndex, toIndex int) {
	for i := fromIndex - s.offset; i <= toIndex-s.offset; i++ {
		s.bins[i] = 0
	}
}

func (s *DenseStore) IsEmpty() bool {
	return s.count == 0
}

func (s *DenseStore) TotalCount() float64 {
	return s.count
}

func (s *DenseStore) MinIndex() (int, error) {
	if s.IsEmpty() {
		return 0, errUndefinedMinIndex
	}
	return s.minIndex, nil
}

func (s *DenseStore) MaxIndex() (int, error) {
	if s.IsEmpty() {
		return 0, errUndefinedMaxIndex
	}
	return s.maxIndex, nil
}

// Return the key for the value at rank
func (s *DenseStore) KeyAtRank(rank float64) int {
	if rank < 0 {
		rank = 0
	}
	var n float64
	for i, b := range s.bins {
		n += b
		if n > rank {
			return i + s.offset
		}
	}
	return s.maxIndex
}

func (s *DenseStore) MergeWith(other Store) {
	if other.IsEmpty() {
		return
	}
	o, ok := other.(*DenseStore)
	if !ok {
		other.ForEach(func(index int, count float64) (stop bool) {
			s.AddWithCount(index, count)
			return false
		})
		return
	}
	if o.minIndex < s.minIndex || o.maxIndex > s.maxIndex {
		s.extendRange(o.minIndex, o.maxIndex)
	}
	for idx := o.minIndex; idx <= o.maxIndex; idx++ {
		s.bins[idx-s.offset] += o.bins[idx-o.offset]
	}
	s.count += o.count
}

func (s *DenseStore) Bins() <-chan Bin {
	ch := make(chan Bin)
	go func() {
		defer close(ch)
		for idx := s.minIndex; idx <= s.maxIndex; idx++ {
			if s.bins[idx-s.offset] > 0 {
				ch <- Bin{index: idx, count: s.bins[idx-s.offset]}
			}
		}
	}()
	return ch
}

func (s *DenseStore) ForEach(f func(index int, count float64) (stop bool)) {
	for idx := s.minIndex; idx <= s.maxIndex; idx++ {
		if s.bins[idx-s.offset] > 0 {
			if f(idx, s.bins[idx-s.offset]) {
				return
			}
		}
	}
}

func (s *DenseStore) Copy() Store {
	bins := make([]float64, len(s.bins))
	copy(bins, s.bins)
	return &DenseStore{
		bins:     bins,
		count:    s.count,
		offset:   s.offset,
		minIndex: s.minIndex,
		maxIndex: s.maxIndex,
	}
}

func (s *DenseStore) Clear() {
	s.bins = s.bins[:0]
	s.count = 0
	s.minIndex = math.MaxInt32
	s.maxIndex = math.MinInt32
}

func (s *DenseStore) string() string {
	var buffer bytes.Buffer
	buffer.WriteString("{")
	for i := 0; i < len(s.bins); i++ {
		index := i + s.offset
		buffer.WriteString(fmt.Sprintf("%d: %f, ", index, s.bins[i]))
	}
	buffer.WriteString(fmt.Sprintf("count: %v, offset: %d, minIndex: %d, maxIndex: %d}", s.count, s.offset, s.minIndex, s.maxIndex))
	return buffer.String()
}

func (s *DenseStore) ToProto() *sketchpb.Store {
	if s.IsEmpty() {
		return &sketchpb.Store{ContiguousBinCounts: nil}
	}
	bins := make([]float64, s.maxIndex-s.minIndex+1)
	copy(bins, s.bins[s.minIndex-s.offset:s.maxIndex-s.offset+1])
	return &sketchpb.Store{
		ContiguousBinCounts:      bins,
		ContiguousBinIndexOffset: int32(s.minIndex),
	}
}

func (s *DenseStore) EncodeProto(builder *sketchpb.StoreBuilder) {
	if s.IsEmpty() {
		return
	}

	for i := s.minIndex - s.offset; i < s.maxIndex-s.offset+1; i++ {
		builder.AddContiguousBinCounts(s.bins[i])
	}
	builder.SetContiguousBinIndexOffset(int32(s.minIndex))
}

func (s *DenseStore) Reweight(w float64) error {
	if w <= 0 {
		return errors.New("can't reweight by a negative factor")
	}
	if w == 1 {
		return nil
	}
	s.count *= w
	for idx := s.minIndex; idx <= s.maxIndex; idx++ {
		s.bins[idx-s.offset] *= w
	}
	return nil
}

func (s *DenseStore) Encode(b *[]byte, t enc.FlagType) {
	if s.IsEmpty() {
		return
	}

	denseEncodingSize := 0
	numBins := uint64(s.maxIndex-s.minIndex) + 1
	denseEncodingSize += enc.Uvarint64Size(numBins)
	denseEncodingSize += enc.Varint64Size(int64(s.minIndex))
	denseEncodingSize += enc.Varint64Size(1)

	sparseEncodingSize := 0
	numNonEmptyBins := uint64(0)

	previousIndex := s.minIndex
	for index := s.minIndex; index <= s.maxIndex; index++ {
		count := s.bins[index-s.offset]
		countVarFloat64Size := enc.Varfloat64Size(count)
		denseEncodingSize += countVarFloat64Size
		if count != 0 {
			numNonEmptyBins++
			sparseEncodingSize += enc.Varint64Size(int64(index - previousIndex))
			sparseEncodingSize += countVarFloat64Size
			previousIndex = index
		}
	}
	sparseEncodingSize += enc.Uvarint64Size(numNonEmptyBins)

	if denseEncodingSize <= sparseEncodingSize {
		s.encodeDensely(b, t, numBins)
	} else {
		s.encodeSparsely(b, t, numNonEmptyBins)
	}
}

func (s *DenseStore) encodeDensely(b *[]byte, t enc.FlagType, numBins uint64) {
	enc.EncodeFlag(b, enc.NewFlag(t, enc.BinEncodingContiguousCounts))
	enc.EncodeUvarint64(b, numBins)
	enc.EncodeVarint64(b, int64(s.minIndex))
	enc.EncodeVarint64(b, 1)
	for index := s.minIndex; index <= s.maxIndex; index++ {
		enc.EncodeVarfloat64(b, s.bins[index-s.offset])
	}
}

func (s *DenseStore) encodeSparsely(b *[]byte, t enc.FlagType, numNonEmptyBins uint64) {
	enc.EncodeFlag(b, enc.NewFlag(t, enc.BinEncodingIndexDeltasAndCounts))
	enc.EncodeUvarint64(b, numNonEmptyBins)
	previousIndex := 0
	for index := s.minIndex; index <= s.maxIndex; index++ {
		count := s.bins[index-s.offset]
		if count != 0 {
			enc.EncodeVarint64(b, int64(index-previousIndex))
			enc.EncodeVarfloat64(b, count)
			previousIndex = index
		}
	}
}

func (s *DenseStore) DecodeAndMergeWith(b *[]byte, encodingMode enc.SubFlag) error {
	return DecodeAndMergeWith(s, b, encodingMode)
}

var _ Store = (*DenseStore)(nil)

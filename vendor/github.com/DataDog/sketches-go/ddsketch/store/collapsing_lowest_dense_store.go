// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package store

import (
	"math"

	enc "github.com/DataDog/sketches-go/ddsketch/encoding"
)

// CollapsingLowestDenseStore is a dynamically growing contiguous (non-sparse) store.
// The lower bins get combined so that the total number of bins do not exceed maxNumBins.
type CollapsingLowestDenseStore struct {
	DenseStore
	maxNumBins  int
	isCollapsed bool
}

func NewCollapsingLowestDenseStore(maxNumBins int) *CollapsingLowestDenseStore {
	// Bins are not allocated until values are added.
	// When the first value is added, a small number of bins are allocated. The number of bins will
	// grow as needed up to maxNumBins.
	return &CollapsingLowestDenseStore{
		DenseStore:  DenseStore{minIndex: math.MaxInt32, maxIndex: math.MinInt32},
		maxNumBins:  maxNumBins,
		isCollapsed: false,
	}
}

func (s *CollapsingLowestDenseStore) Add(index int) {
	s.AddWithCount(index, float64(1))
}

func (s *CollapsingLowestDenseStore) AddBin(bin Bin) {
	index := bin.Index()
	count := bin.Count()
	if count == 0 {
		return
	}
	s.AddWithCount(index, count)
}

func (s *CollapsingLowestDenseStore) AddWithCount(index int, count float64) {
	if count == 0 {
		return
	}
	arrayIndex := s.normalize(index)
	s.bins[arrayIndex] += count
	s.count += count
}

// Normalize the store, if necessary, so that the counter of the specified index can be updated.
func (s *CollapsingLowestDenseStore) normalize(index int) int {
	if index < s.minIndex {
		if s.isCollapsed {
			return 0
		} else {
			s.extendRange(index, index)
			if s.isCollapsed {
				return 0
			}
		}
	} else if index > s.maxIndex {
		s.extendRange(index, index)
	}
	return index - s.offset
}

func (s *CollapsingLowestDenseStore) getNewLength(newMinIndex, newMaxIndex int) int {
	return min(s.DenseStore.getNewLength(newMinIndex, newMaxIndex), s.maxNumBins)
}

func (s *CollapsingLowestDenseStore) extendRange(newMinIndex, newMaxIndex int) {
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
func (s *CollapsingLowestDenseStore) adjust(newMinIndex, newMaxIndex int) {
	if newMaxIndex-newMinIndex+1 > len(s.bins) {
		// The range of indices is too wide, buckets of lowest indices need to be collapsed.
		newMinIndex = newMaxIndex - len(s.bins) + 1
		if newMinIndex >= s.maxIndex {
			// There will be only one non-empty bucket.
			s.bins = make([]float64, len(s.bins))
			s.offset = newMinIndex
			s.minIndex = newMinIndex
			s.bins[0] = s.count
		} else {
			shift := s.offset - newMinIndex
			if shift < 0 {
				// Collapse the buckets.
				n := float64(0)
				for i := s.minIndex; i < newMinIndex; i++ {
					n += s.bins[i-s.offset]
				}
				s.resetBins(s.minIndex, newMinIndex-1)
				s.bins[newMinIndex-s.offset] += n
				s.minIndex = newMinIndex
				// Shift the buckets to make room for newMaxIndex.
				s.shiftCounts(shift)
			} else {
				// Shift the buckets to make room for newMinIndex.
				s.shiftCounts(shift)
				s.minIndex = newMinIndex
			}
		}
		s.maxIndex = newMaxIndex
		s.isCollapsed = true
	} else {
		s.centerCounts(newMinIndex, newMaxIndex)
	}
}

func (s *CollapsingLowestDenseStore) MergeWith(other Store) {
	if other.IsEmpty() {
		return
	}
	o, ok := other.(*CollapsingLowestDenseStore)
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
	idx := o.minIndex
	for ; idx < s.minIndex && idx <= o.maxIndex; idx++ {
		s.bins[0] += o.bins[idx-o.offset]
	}
	for ; idx < o.maxIndex; idx++ {
		s.bins[idx-s.offset] += o.bins[idx-o.offset]
	}
	// This is a separate test so that the comparison in the previous loop is strict (<) and handles
	// store.maxIndex = Integer.MAX_VALUE.
	if idx == o.maxIndex {
		s.bins[idx-s.offset] += o.bins[idx-o.offset]
	}
	s.count += o.count
}

func (s *CollapsingLowestDenseStore) Copy() Store {
	bins := make([]float64, len(s.bins))
	copy(bins, s.bins)
	return &CollapsingLowestDenseStore{
		DenseStore: DenseStore{
			bins:     bins,
			count:    s.count,
			offset:   s.offset,
			minIndex: s.minIndex,
			maxIndex: s.maxIndex,
		},
		maxNumBins:  s.maxNumBins,
		isCollapsed: s.isCollapsed,
	}
}

func (s *CollapsingLowestDenseStore) Clear() {
	s.DenseStore.Clear()
	s.isCollapsed = false
}

func (s *CollapsingLowestDenseStore) DecodeAndMergeWith(r *[]byte, encodingMode enc.SubFlag) error {
	return DecodeAndMergeWith(s, r, encodingMode)
}

var _ Store = (*CollapsingLowestDenseStore)(nil)

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

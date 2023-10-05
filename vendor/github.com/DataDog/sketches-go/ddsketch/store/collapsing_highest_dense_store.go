// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package store

import (
	"math"

	enc "github.com/DataDog/sketches-go/ddsketch/encoding"
)

type CollapsingHighestDenseStore struct {
	DenseStore
	maxNumBins  int
	isCollapsed bool
}

func NewCollapsingHighestDenseStore(maxNumBins int) *CollapsingHighestDenseStore {
	return &CollapsingHighestDenseStore{
		DenseStore:  DenseStore{minIndex: math.MaxInt32, maxIndex: math.MinInt32},
		maxNumBins:  maxNumBins,
		isCollapsed: false,
	}
}

func (s *CollapsingHighestDenseStore) Add(index int) {
	s.AddWithCount(index, float64(1))
}

func (s *CollapsingHighestDenseStore) AddBin(bin Bin) {
	index := bin.Index()
	count := bin.Count()
	if count == 0 {
		return
	}
	s.AddWithCount(index, count)
}

func (s *CollapsingHighestDenseStore) AddWithCount(index int, count float64) {
	if count == 0 {
		return
	}
	arrayIndex := s.normalize(index)
	s.bins[arrayIndex] += count
	s.count += count
}

// Normalize the store, if necessary, so that the counter of the specified index can be updated.
func (s *CollapsingHighestDenseStore) normalize(index int) int {
	if index > s.maxIndex {
		if s.isCollapsed {
			return len(s.bins) - 1
		} else {
			s.extendRange(index, index)
			if s.isCollapsed {
				return len(s.bins) - 1
			}
		}
	} else if index < s.minIndex {
		s.extendRange(index, index)
	}
	return index - s.offset
}

func (s *CollapsingHighestDenseStore) getNewLength(newMinIndex, newMaxIndex int) int {
	return min(s.DenseStore.getNewLength(newMinIndex, newMaxIndex), s.maxNumBins)
}

func (s *CollapsingHighestDenseStore) extendRange(newMinIndex, newMaxIndex int) {
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
func (s *CollapsingHighestDenseStore) adjust(newMinIndex, newMaxIndex int) {
	if newMaxIndex-newMinIndex+1 > len(s.bins) {
		// The range of indices is too wide, buckets of lowest indices need to be collapsed.
		newMaxIndex = newMinIndex + len(s.bins) - 1
		if newMaxIndex <= s.minIndex {
			// There will be only one non-empty bucket.
			s.bins = make([]float64, len(s.bins))
			s.offset = newMinIndex
			s.maxIndex = newMaxIndex
			s.bins[len(s.bins)-1] = s.count
		} else {
			shift := s.offset - newMinIndex
			if shift > 0 {
				// Collapse the buckets.
				n := float64(0)
				for i := newMaxIndex + 1; i <= s.maxIndex; i++ {
					n += s.bins[i-s.offset]
				}
				s.resetBins(newMaxIndex+1, s.maxIndex)
				s.bins[newMaxIndex-s.offset] += n
				s.maxIndex = newMaxIndex
				// Shift the buckets to make room for newMinIndex.
				s.shiftCounts(shift)
			} else {
				// Shift the buckets to make room for newMaxIndex.
				s.shiftCounts(shift)
				s.maxIndex = newMaxIndex
			}
		}
		s.minIndex = newMinIndex
		s.isCollapsed = true
	} else {
		s.centerCounts(newMinIndex, newMaxIndex)
	}
}

func (s *CollapsingHighestDenseStore) MergeWith(other Store) {
	if other.IsEmpty() {
		return
	}
	o, ok := other.(*CollapsingHighestDenseStore)
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
	idx := o.maxIndex
	for ; idx > s.maxIndex && idx >= o.minIndex; idx-- {
		s.bins[len(s.bins)-1] += o.bins[idx-o.offset]
	}
	for ; idx > o.minIndex; idx-- {
		s.bins[idx-s.offset] += o.bins[idx-o.offset]
	}
	// This is a separate test so that the comparison in the previous loop is strict (>) and handles
	// o.minIndex = Integer.MIN_VALUE.
	if idx == o.minIndex {
		s.bins[idx-s.offset] += o.bins[idx-o.offset]
	}
	s.count += o.count
}

func (s *CollapsingHighestDenseStore) Copy() Store {
	bins := make([]float64, len(s.bins))
	copy(bins, s.bins)
	return &CollapsingHighestDenseStore{
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

func (s *CollapsingHighestDenseStore) Clear() {
	s.DenseStore.Clear()
	s.isCollapsed = false
}

func (s *CollapsingHighestDenseStore) DecodeAndMergeWith(r *[]byte, encodingMode enc.SubFlag) error {
	return DecodeAndMergeWith(s, r, encodingMode)
}

var _ Store = (*CollapsingHighestDenseStore)(nil)

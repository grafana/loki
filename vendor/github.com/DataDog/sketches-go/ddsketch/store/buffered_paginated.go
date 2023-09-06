// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package store

import (
	"errors"
	"sort"

	enc "github.com/DataDog/sketches-go/ddsketch/encoding"
	"github.com/DataDog/sketches-go/ddsketch/pb/sketchpb"
)

const (
	ptrSize         = 32 << (^uintptr(0) >> 63)
	intSize         = 32 << (^uint(0) >> 63)
	float64size     = 64
	bufferEntrySize = intSize
	countSize       = float64size

	defaultPageLenLog2 = 5 // pageLen = 32
)

// BufferedPaginatedStore allocates storage for counts in aligned fixed-size
// pages, themselves stored in a dynamically-sized slice. A page encodes the
// counts for a contiguous range of indexes, and two pages that are contiguous
// in the slice encode ranges that are contiguous. In addition, input indexes
// that are added to the store with a count equal to 1 can be stored in a
// buffer.
// The store favors using the buffer and only creates pages when the memory size
// of the page is no greater than the memory space that is needed to keep in the
// buffer the indexes that could otherwise be encoded in that page. That means
// that some indexes may stay indefinitely in the buffer if, to be removed from
// the buffer, they would create a page that is almost empty. The process that
// transfers indexes from the buffer to pages is called compaction.
// This store never collapses or merges bins, therefore, it does not introduce
// any error in itself. In particular, MinIndex(), MaxIndex(), Bins() and
// KeyAtRank() return exact results.
// There is no upper bound on the memory size that this store needs to encode
// input indexes, and some input data distributions may make it reach large
// sizes. However, thanks to the buffer and the fact that only required pages
// are allocated, it can be much more space efficient than alternative stores,
// especially dense stores, in various situations, including when only few
// indexes are added (with their counts equal to 1), when the input data has a
// few outliers or when the input data distribution is multimodal.
type BufferedPaginatedStore struct {
	buffer                     []int // FIXME: in practice, int32 (even int16, depending on the accuracy parameter) is enough
	bufferCompactionTriggerLen int   // compaction happens only after this buffer length is reached

	pages        [][]float64 // len == cap, the slice is always used to its maximum capacity
	minPageIndex int         // minPageIndex == maxInt iff pages are unused (they may still be allocated)
	pageLenLog2  int
	pageLenMask  int
}

func NewBufferedPaginatedStore() *BufferedPaginatedStore {
	initialBufferCapacity := 4
	pageLenLog2 := defaultPageLenLog2
	pageLen := 1 << pageLenLog2

	return &BufferedPaginatedStore{
		buffer:                     make([]int, 0, initialBufferCapacity),
		bufferCompactionTriggerLen: 2 * pageLen,
		pages:                      nil,
		minPageIndex:               maxInt,
		pageLenLog2:                pageLenLog2,
		pageLenMask:                pageLen - 1,
	}
}

// pageIndex returns the page number the given index falls on.
func (s *BufferedPaginatedStore) pageIndex(index int) int {
	return index >> s.pageLenLog2
}

// lineIndex returns the line number within a page that the given index falls on.
func (s *BufferedPaginatedStore) lineIndex(index int) int {
	return index & s.pageLenMask
}

// index returns the store-level index for a given page number and a line within that page.
func (s *BufferedPaginatedStore) index(pageIndex, lineIndex int) int {
	return pageIndex<<s.pageLenLog2 + lineIndex
}

// page returns the page for the provided pageIndex, or nil. When unexisting,
// the page is created if and only if ensureExists is true.
func (s *BufferedPaginatedStore) page(pageIndex int, ensureExists bool) []float64 {
	pageLen := 1 << s.pageLenLog2

	if pageIndex >= s.minPageIndex && pageIndex < s.minPageIndex+len(s.pages) {
		// No need to extend s.pages.
		page := &s.pages[pageIndex-s.minPageIndex]
		if ensureExists && len(*page) == 0 {
			*page = append(*page, make([]float64, pageLen)...)
		}
		return *page
	}

	if !ensureExists {
		return nil
	}

	if pageIndex < s.minPageIndex {
		if s.minPageIndex == maxInt {
			if len(s.pages) == 0 {
				s.pages = append(s.pages, make([][]float64, s.newPagesLen(1))...)
			}
			s.minPageIndex = pageIndex - len(s.pages)/2
		} else {
			// Extends s.pages left.
			newLen := s.newPagesLen(s.minPageIndex - pageIndex + 1 + len(s.pages))
			addedLen := newLen - len(s.pages)
			s.pages = append(s.pages, make([][]float64, addedLen)...)
			copy(s.pages[addedLen:], s.pages)
			for i := 0; i < addedLen; i++ {
				s.pages[i] = nil
			}
			s.minPageIndex -= addedLen
		}
	} else {
		// Extends s.pages right.
		s.pages = append(s.pages, make([][]float64, s.newPagesLen(pageIndex-s.minPageIndex+1)-len(s.pages))...)
	}

	page := &s.pages[pageIndex-s.minPageIndex]
	if len(*page) == 0 {
		*page = append(*page, make([]float64, pageLen)...)
	}
	return *page
}

func (s *BufferedPaginatedStore) newPagesLen(required int) int {
	// Grow in size by multiples of 64 bytes
	pageGrowthIncrement := 64 * 8 / ptrSize
	return (required + pageGrowthIncrement - 1) & -pageGrowthIncrement
}

// compact transfers indexes from the buffer to the pages. It only creates new
// pages if they can encode enough buffered indexes so that it frees more space
// in the buffer than the new page takes.
func (s *BufferedPaginatedStore) compact() {
	pageLen := 1 << s.pageLenLog2

	s.sortBuffer()

	for bufferPos := 0; bufferPos < len(s.buffer); {
		bufferPageStart := bufferPos
		pageIndex := s.pageIndex(s.buffer[bufferPageStart])
		bufferPos++
		for bufferPos < len(s.buffer) && s.pageIndex(s.buffer[bufferPos]) == pageIndex {
			bufferPos++
		}
		bufferPageEnd := bufferPos

		// We avoid creating a new page if it would take more memory space than
		// what we would free in the buffer. Note that even when the page itself
		// takes less memory space than the buffered indexes that can be encoded
		// in the page, because we may have to extend s.pages, the store may end
		// up larger. However, for the sake of simplicity, we ignore the length
		// of s.pages.
		ensureExists := (bufferPageEnd-bufferPageStart)*bufferEntrySize >= pageLen*float64size
		newPage := s.page(pageIndex, ensureExists)
		if len(newPage) > 0 {
			for _, index := range s.buffer[bufferPageStart:bufferPageEnd] {
				newPage[s.lineIndex(index)]++
			}
			copy(s.buffer[bufferPageStart:], s.buffer[bufferPageEnd:])
			s.buffer = s.buffer[:len(s.buffer)+bufferPageStart-bufferPageEnd]
			bufferPos = bufferPageStart
		}
	}

	s.bufferCompactionTriggerLen = len(s.buffer) + pageLen
}

func (s *BufferedPaginatedStore) sortBuffer() {
	sort.Slice(s.buffer, func(i, j int) bool { return s.buffer[i] < s.buffer[j] })
}

func (s *BufferedPaginatedStore) Add(index int) {
	pageIndex := s.pageIndex(index)
	if pageIndex >= s.minPageIndex && pageIndex < s.minPageIndex+len(s.pages) {
		page := s.pages[pageIndex-s.minPageIndex]
		if len(page) > 0 {
			page[s.lineIndex(index)]++
			return
		}
	}

	// The page does not exist, use the buffer.
	if len(s.buffer) == cap(s.buffer) && len(s.buffer) >= s.bufferCompactionTriggerLen {
		s.compact()
	}

	s.buffer = append(s.buffer, index)
}

func (s *BufferedPaginatedStore) AddBin(bin Bin) {
	s.AddWithCount(bin.Index(), bin.Count())
}

func (s *BufferedPaginatedStore) AddWithCount(index int, count float64) {
	if count == 0 {
		return
	} else if count == 1 {
		s.Add(index)
	} else {
		s.page(s.pageIndex(index), true)[s.lineIndex(index)] += count
	}
}

func (s *BufferedPaginatedStore) IsEmpty() bool {
	if len(s.buffer) > 0 {
		return false
	}
	for _, page := range s.pages {
		for _, count := range page {
			if count > 0 {
				return false
			}
		}
	}
	return true
}

func (s *BufferedPaginatedStore) TotalCount() float64 {
	totalCount := float64(len(s.buffer))
	for _, page := range s.pages {
		for _, count := range page {
			totalCount += count
		}
	}
	return totalCount
}

func (s *BufferedPaginatedStore) MinIndex() (int, error) {
	isEmpty := true

	// Iterate over the buffer.
	var minIndex int
	for _, index := range s.buffer {
		if isEmpty || index < minIndex {
			isEmpty = false
			minIndex = index
		}
	}

	// Iterate over the pages.
	for pageIndex := s.minPageIndex; pageIndex < s.minPageIndex+len(s.pages) && (isEmpty || pageIndex <= s.pageIndex(minIndex)); pageIndex++ {
		page := s.pages[pageIndex-s.minPageIndex]
		if len(page) == 0 {
			continue
		}

		var lineIndexRangeEnd int
		if !isEmpty && pageIndex == s.pageIndex(minIndex) {
			lineIndexRangeEnd = s.lineIndex(minIndex)
		} else {
			lineIndexRangeEnd = 1 << s.pageLenLog2
		}

		for lineIndex := 0; lineIndex < lineIndexRangeEnd; lineIndex++ {
			if page[lineIndex] > 0 {
				return s.index(pageIndex, lineIndex), nil
			}
		}
	}

	if isEmpty {
		return 0, errUndefinedMinIndex
	} else {
		return minIndex, nil
	}
}

func (s *BufferedPaginatedStore) MaxIndex() (int, error) {
	isEmpty := true

	// Iterate over the buffer.
	var maxIndex int
	for _, index := range s.buffer {
		if isEmpty || index > maxIndex {
			isEmpty = false
			maxIndex = index
		}
	}

	// Iterate over the pages.
	for pageIndex := s.minPageIndex + len(s.pages) - 1; pageIndex >= s.minPageIndex && (isEmpty || pageIndex >= s.pageIndex(maxIndex)); pageIndex-- {
		page := s.pages[pageIndex-s.minPageIndex]
		if len(page) == 0 {
			continue
		}

		var lineIndexRangeStart int
		if !isEmpty && pageIndex == s.pageIndex(maxIndex) {
			lineIndexRangeStart = s.lineIndex(maxIndex)
		} else {
			lineIndexRangeStart = 0
		}

		for lineIndex := len(page) - 1; lineIndex >= lineIndexRangeStart; lineIndex-- {
			if page[lineIndex] > 0 {
				return s.index(pageIndex, lineIndex), nil
			}
		}
	}

	if isEmpty {
		return 0, errUndefinedMaxIndex
	} else {
		return maxIndex, nil
	}
}

func (s *BufferedPaginatedStore) KeyAtRank(rank float64) int {
	if rank < 0 {
		rank = 0
	}
	key, err := s.minIndexWithCumulCount(func(cumulCount float64) bool {
		return cumulCount > rank
	})

	if err != nil {
		maxIndex, err := s.MaxIndex()
		if err == nil {
			return maxIndex
		} else {
			// FIXME: make Store's KeyAtRank consistent with MinIndex and MaxIndex
			return 0
		}
	}
	return key
}

// minIndexWithCumulCount returns the minimum index whose cumulative count (that
// is, the sum of the counts associated with the indexes less than or equal to
// the index) verifies the predicate.
func (s *BufferedPaginatedStore) minIndexWithCumulCount(predicate func(float64) bool) (int, error) {
	s.sortBuffer()
	cumulCount := float64(0)

	// Iterate over the pages and the buffer simultaneously.
	bufferPos := 0
	for pageOffset, page := range s.pages {
		for lineIndex, count := range page {
			index := s.index(s.minPageIndex+pageOffset, lineIndex)

			// Iterate over the buffer until index is reached.
			for ; bufferPos < len(s.buffer) && s.buffer[bufferPos] < index; bufferPos++ {
				cumulCount++
				if predicate(cumulCount) {
					return s.buffer[bufferPos], nil
				}
			}
			cumulCount += count
			if predicate(cumulCount) {
				return index, nil
			}
		}
	}

	// Iterate over the rest of the buffer
	for ; bufferPos < len(s.buffer); bufferPos++ {
		cumulCount++
		if predicate(cumulCount) {
			return s.buffer[bufferPos], nil
		}
	}

	return 0, errors.New("the predicate on the cumulative count is never verified")
}

func (s *BufferedPaginatedStore) MergeWith(other Store) {
	o, ok := other.(*BufferedPaginatedStore)
	if ok && s.pageLenLog2 == o.pageLenLog2 {
		// Merge pages.
		for oPageOffset, oPage := range o.pages {
			if len(oPage) == 0 {
				continue
			}
			oPageIndex := o.minPageIndex + oPageOffset
			page := s.page(oPageIndex, true)
			for i, oCount := range oPage {
				page[i] += oCount
			}
		}

		// Merge buffers.
		for _, index := range o.buffer {
			s.Add(index)
		}
	} else {
		// Fallback merging.
		other.ForEach(func(index int, count float64) (stop bool) {
			s.AddWithCount(index, count)
			return false
		})
	}
}

func (s *BufferedPaginatedStore) MergeWithProto(pb *sketchpb.Store) {
	for index, count := range pb.BinCounts {
		s.AddWithCount(int(index), count)
	}
	for indexOffset, count := range pb.ContiguousBinCounts {
		s.AddWithCount(int(pb.ContiguousBinIndexOffset)+indexOffset, count)
	}
}

func (s *BufferedPaginatedStore) Bins() <-chan Bin {
	s.sortBuffer()
	ch := make(chan Bin)
	go func() {
		defer close(ch)
		bufferPos := 0

		// Iterate over the pages and the buffer simultaneously.
		for pageOffset, page := range s.pages {
			for lineIndex, count := range page {
				if count == 0 {
					continue
				}

				index := s.index(s.minPageIndex+pageOffset, lineIndex)

				// Iterate over the buffer until index is reached.
				var indexBufferStartPos int
				for {
					indexBufferStartPos = bufferPos
					if indexBufferStartPos >= len(s.buffer) || s.buffer[indexBufferStartPos] > index {
						break
					}
					bufferPos++
					for bufferPos < len(s.buffer) && s.buffer[bufferPos] == s.buffer[indexBufferStartPos] {
						bufferPos++
					}
					if s.buffer[indexBufferStartPos] == index {
						break
					}
					ch <- Bin{index: s.buffer[indexBufferStartPos], count: float64(bufferPos - indexBufferStartPos)}
				}
				ch <- Bin{index: index, count: count + float64(bufferPos-indexBufferStartPos)}
			}
		}

		// Iterate over the rest of the buffer.
		for bufferPos < len(s.buffer) {
			indexBufferStartPos := bufferPos
			bufferPos++
			for bufferPos < len(s.buffer) && s.buffer[bufferPos] == s.buffer[indexBufferStartPos] {
				bufferPos++
			}
			bin := Bin{index: s.buffer[indexBufferStartPos], count: float64(bufferPos - indexBufferStartPos)}
			ch <- bin
		}
	}()
	return ch
}

func (s *BufferedPaginatedStore) ForEach(f func(index int, count float64) (stop bool)) {
	s.sortBuffer()
	bufferPos := 0

	// Iterate over the pages and the buffer simultaneously.
	for pageOffset, page := range s.pages {
		for lineIndex, count := range page {
			if count == 0 {
				continue
			}

			index := s.index(s.minPageIndex+pageOffset, lineIndex)

			// Iterate over the buffer until index is reached.
			var indexBufferStartPos int
			for {
				indexBufferStartPos = bufferPos
				if indexBufferStartPos >= len(s.buffer) || s.buffer[indexBufferStartPos] > index {
					break
				}
				bufferPos++
				for bufferPos < len(s.buffer) && s.buffer[bufferPos] == s.buffer[indexBufferStartPos] {
					bufferPos++
				}
				if s.buffer[indexBufferStartPos] == index {
					break
				}
				if f(s.buffer[indexBufferStartPos], float64(bufferPos-indexBufferStartPos)) {
					return
				}
			}
			if f(index, count+float64(bufferPos-indexBufferStartPos)) {
				return
			}
		}
	}

	// Iterate over the rest of the buffer.
	for bufferPos < len(s.buffer) {
		indexBufferStartPos := bufferPos
		bufferPos++
		for bufferPos < len(s.buffer) && s.buffer[bufferPos] == s.buffer[indexBufferStartPos] {
			bufferPos++
		}
		if f(s.buffer[indexBufferStartPos], float64(bufferPos-indexBufferStartPos)) {
			return
		}
	}
}

func (s *BufferedPaginatedStore) Copy() Store {
	bufferCopy := make([]int, len(s.buffer))
	copy(bufferCopy, s.buffer)
	pagesCopy := make([][]float64, len(s.pages))
	for i, page := range s.pages {
		if len(page) > 0 {
			pageCopy := make([]float64, len(page))
			copy(pageCopy, page)
			pagesCopy[i] = pageCopy
		}
	}
	return &BufferedPaginatedStore{
		buffer:                     bufferCopy,
		bufferCompactionTriggerLen: s.bufferCompactionTriggerLen,
		pages:                      pagesCopy,
		minPageIndex:               s.minPageIndex,
		pageLenLog2:                s.pageLenLog2,
		pageLenMask:                s.pageLenMask,
	}
}

func (s *BufferedPaginatedStore) Clear() {
	s.buffer = s.buffer[:0]
	for i := range s.pages {
		s.pages[i] = s.pages[i][:0]
	}
	s.minPageIndex = maxInt
}

func (s *BufferedPaginatedStore) ToProto() *sketchpb.Store {
	if s.IsEmpty() {
		return &sketchpb.Store{}
	}
	// FIXME: add heuristic to use contiguousBinCounts when cheaper.
	binCounts := make(map[int32]float64)
	s.ForEach(func(index int, count float64) (stop bool) {
		binCounts[int32(index)] = count
		return false
	})
	return &sketchpb.Store{
		BinCounts: binCounts,
	}
}

func (s *BufferedPaginatedStore) Reweight(w float64) error {
	if w <= 0 {
		return errors.New("can't reweight by a negative factor")
	}
	if w == 1 {
		return nil
	}
	buffer := s.buffer
	s.buffer = s.buffer[:0]
	for _, p := range s.pages {
		for i := range p {
			p[i] *= w
		}
	}
	for _, index := range buffer {
		s.AddWithCount(index, w)
	}
	return nil
}

func (s *BufferedPaginatedStore) Encode(b *[]byte, t enc.FlagType) {
	s.compact()
	if len(s.buffer) > 0 {
		enc.EncodeFlag(b, enc.NewFlag(t, enc.BinEncodingIndexDeltas))
		enc.EncodeUvarint64(b, uint64(len(s.buffer)))
		previousIndex := 0
		for _, index := range s.buffer {
			enc.EncodeVarint64(b, int64(index-previousIndex))
			previousIndex = index
		}
	}

	for pageOffset, page := range s.pages {
		if len(page) > 0 {
			enc.EncodeFlag(b, enc.NewFlag(t, enc.BinEncodingContiguousCounts))
			enc.EncodeUvarint64(b, uint64(len(page)))
			enc.EncodeVarint64(b, int64(s.index(s.minPageIndex+pageOffset, 0)))
			enc.EncodeVarint64(b, 1)
			for _, count := range page {
				enc.EncodeVarfloat64(b, count)
			}
		}
	}
}

func (s *BufferedPaginatedStore) DecodeAndMergeWith(b *[]byte, encodingMode enc.SubFlag) error {
	switch encodingMode {

	case enc.BinEncodingIndexDeltas:
		numBins, err := enc.DecodeUvarint64(b)
		if err != nil {
			return err
		}
		remaining := int(numBins)
		index := int64(0)
		// Process indexes in batches to avoid checking after each insertion
		// whether compaction should happen.
		for {
			batchSize := min(remaining, max(cap(s.buffer), s.bufferCompactionTriggerLen)-len(s.buffer))
			for i := 0; i < batchSize; i++ {
				indexDelta, err := enc.DecodeVarint64(b)
				if err != nil {
					return err
				}
				index += indexDelta
				s.buffer = append(s.buffer, int(index))
			}
			remaining -= batchSize
			if remaining == 0 {
				return nil
			}
			s.compact()
		}

	case enc.BinEncodingContiguousCounts:
		numBins, err := enc.DecodeUvarint64(b)
		if err != nil {
			return err
		}
		indexOffset, err := enc.DecodeVarint64(b)
		if err != nil {
			return err
		}
		indexDelta, err := enc.DecodeVarint64(b)
		if err != nil {
			return err
		}
		pageLen := 1 << s.pageLenLog2
		for i := uint64(0); i < numBins; {
			page := s.page(s.pageIndex(int(indexOffset)), true)
			lineIndex := s.lineIndex(int(indexOffset))
			for lineIndex >= 0 && lineIndex < pageLen && i < numBins {
				count, err := enc.DecodeVarfloat64(b)
				if err != nil {
					return err
				}
				page[lineIndex] += count
				lineIndex += int(indexDelta)
				indexOffset += indexDelta
				i++
			}
		}
		return nil

	default:
		return DecodeAndMergeWith(s, b, encodingMode)
	}
}

var _ Store = (*BufferedPaginatedStore)(nil)

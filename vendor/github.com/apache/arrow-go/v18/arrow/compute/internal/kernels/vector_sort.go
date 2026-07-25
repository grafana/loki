// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build go1.22

package kernels

import (
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
)

// SortOrder specifies the sort order for sorting operations.
type SortOrder int8

const (
	Ascending SortOrder = iota
	Descending
)

// NullPlacement specifies where null values should be placed in the sort order.
type NullPlacement int8

const (
	NullsAtEnd NullPlacement = iota
	NullsAtStart
)

// SortOptions defines options for the sort_indices function.
type SortOptions struct {
	Order         SortOrder
	NullPlacement NullPlacement
}

func (SortOptions) TypeName() string { return "SortOptions" }

type SortState = SortOptions

// SortKey defines a column to sort by with its ordering and null placement options.
type SortKey struct {
	ColumnIndex   int
	Order         SortOrder
	NullPlacement NullPlacement
}

// Chunk-aware sort_indices: logical row IDs 0..n-1, no chunk concatenation. Structure follows
// Apache Arrow C++ vector_sort.cc / vector_sort_internal.h (see vector_sort_internal.go).
//
// Per-column data uses a dense logicalRowMap for O(1) chunk resolution under random compares.
// Each sortable physical type has a dedicated comparator struct in vector_sort_physical.go (C++
// ConcreteColumnComparator<T> shape): full monomorphization for the hot compare path; no
// value-level compare func pointer.
// compareRowsForKey implements the same ordering as C++ (null placement, NaN, Order).
//
// Single key: ChunkedArraySorter — arraySortOneColumnRange per chunk (PartitionNullsOnly /
// PartitionNullLikes + stable_sort finites), then pairwise merge (ChunkedMergeImpl-style merge
// using full row order; C++ splits null / non-null merge when the type has null-likes).
//
// Multi-key, aligned chunks: TableSorter — per-chunk RadixRecordBatchSorter or
// MultipleKeyRecordBatchSorter, then merge.
//
// Multi-key, single segment: RadixRecordBatchSorter (<= maxRadixSortKeys) or
// MultipleKeyRecordBatchSorter (> maxRadixSortKeys).

// maxRadixSortKeys matches Arrow C++ kMaxRadixSortKeys (vector_sort.cc): above this, one global
// multi-key stable sort is used instead of MSD radix.
const maxRadixSortKeys = 8

// columnComparator is the Go analogue of compute::internal::ColumnComparator (vector_sort_internal.h):
// per-column row compare + null / null-like metadata for partitioning.
type columnComparator interface {
	// compareRowsForKey returns -1 if i before j, +1 if i after j, 0 if tied on this column
	// (both null, or both non-null and equal), so the caller may advance to the next sort key.
	compareRowsForKey(i, j uint64, key SortKey) int
	// isNullAt returns true if the global row index is null.
	isNullAt(global uint64) bool
	// hasNullLikeValues returns true if the column has null-like values.
	hasNullLikeValues() bool
	// isNullLikeAt returns true if the global row index is a null-like value.
	isNullLikeAt(global uint64) bool
	// columnHasValidityNulls mirrors Array::null_count() != 0; when false, C++ skips PartitionNullsOnly.
	columnHasValidityNulls() bool
}

// multiColumnComparator compares two logical rows (global uint64 indices) lexicographically
// across every sort key. That matches C++ MultipleKeyComparator::CompareInternal(left, right, 0)
// (vector_sort_internal.h), but it is not a port of the whole MultipleKeyComparator type: C++ keeps
// ResolvedSortKey per column, uses Location (int64 batch row vs ChunkLocation on tables), builds
// virtual ColumnComparator instances, and passes start_sort_key_index for radix tails and other
// partial key ranges — in Go those suffix compares are makeTailComparator(comparators, keys, from).
type multiColumnComparator struct {
	columns []columnComparator
	keys    []SortKey
}

// compare is a three-way ordering for stable sort / merge: negative if idxA before idxB, etc.
func (m *multiColumnComparator) compare(idxA, idxB uint64) int {
	for i, key := range m.keys {
		if cmpVal := m.columns[i].compareRowsForKey(idxA, idxB, key); cmpVal != 0 {
			return cmpVal
		}
	}
	return 0
}

func extensionStorageFixedSizeBinaryChunks(chunks []arrow.Array) ([]arrow.Array, error) {
	out := make([]arrow.Array, len(chunks))
	for i, ch := range chunks {
		ext, ok := ch.(array.ExtensionArray)
		if !ok {
			return nil, fmt.Errorf("%w: extension column must implement array.ExtensionArray", arrow.ErrInvalid)
		}
		st := ext.Storage()

		// TODO: allow individual extension types to sort themselves properly

		if st.DataType().ID() != arrow.FIXED_SIZE_BINARY {
			return nil, fmt.Errorf("%w: sorting extension columns is only supported when storage is fixed_size_binary (got %s)",
				arrow.ErrNotImplemented, st.DataType())
		}
		out[i] = st
	}
	return out, nil
}

func newFixedSizeBinaryComparator(chunks []arrow.Array, numRows int, vn bool) (columnComparator, error) {
	f0, ok := chunks[0].(*array.FixedSizeBinary)
	if !ok {
		return nil, fmt.Errorf("%w: expected *array.FixedSizeBinary chunk", arrow.ErrInvalid)
	}
	w := f0.DataType().(*arrow.FixedSizeBinaryType).ByteWidth
	for _, chunk := range chunks[1:] {
		fi, ok := chunk.(*array.FixedSizeBinary)
		if !ok {
			return nil, fmt.Errorf("%w: expected *array.FixedSizeBinary chunk", arrow.ErrInvalid)
		}
		wi := fi.DataType().(*arrow.FixedSizeBinaryType).ByteWidth
		if wi != w {
			return nil, fmt.Errorf("%w: fixed_size_binary chunks must have the same byte width (%d vs %d)",
				arrow.ErrInvalid, w, wi)
		}
	}
	return newPhysicalSortFixedSizeBinaryColumn(chunks, numRows, vn), nil
}

// createChunkedComparator builds a column comparator for these chunks (one Arrow type for all chunks).
func createChunkedComparator(chunks []arrow.Array, numRows int) (columnComparator, error) {
	if len(chunks) == 0 {
		return nil, fmt.Errorf("%w: cannot create comparator for empty chunk list", arrow.ErrInvalid)
	}
	if totalChunkRows(chunks) != numRows {
		return nil, fmt.Errorf("%w: chunk row count does not match column length", arrow.ErrInvalid)
	}

	validityNulls := chunksHaveNulls(chunks)
	typeID := chunks[0].DataType().ID()
	switch typeID {
	case arrow.INT8:
		return newPhysicalSortInt8Column(chunks, numRows, validityNulls), nil
	case arrow.INT16:
		return newPhysicalSortInt16Column(chunks, numRows, validityNulls), nil
	case arrow.INT32:
		return newPhysicalSortInt32Column(chunks, numRows, validityNulls), nil
	case arrow.DATE32:
		return newPhysicalSortDate32Column(chunks, numRows, validityNulls), nil
	case arrow.TIME32:
		return newPhysicalSortTime32Column(chunks, numRows, validityNulls), nil
	case arrow.INT64:
		return newPhysicalSortInt64Column(chunks, numRows, validityNulls), nil
	case arrow.DATE64:
		return newPhysicalSortDate64Column(chunks, numRows, validityNulls), nil
	case arrow.TIME64:
		return newPhysicalSortTime64Column(chunks, numRows, validityNulls), nil
	case arrow.TIMESTAMP:
		return newPhysicalSortTimestampColumn(chunks, numRows, validityNulls), nil
	case arrow.DURATION:
		return newPhysicalSortDurationColumn(chunks, numRows, validityNulls), nil
	case arrow.UINT8:
		return newPhysicalSortUint8Column(chunks, numRows, validityNulls), nil
	case arrow.UINT16:
		return newPhysicalSortUint16Column(chunks, numRows, validityNulls), nil
	case arrow.UINT32:
		return newPhysicalSortUint32Column(chunks, numRows, validityNulls), nil
	case arrow.UINT64:
		return newPhysicalSortUint64Column(chunks, numRows, validityNulls), nil
	case arrow.FLOAT16:
		return newPhysicalSortFloat16Column(chunks, numRows, validityNulls), nil
	case arrow.FLOAT32:
		return newPhysicalSortFloat32Column(chunks, numRows, validityNulls), nil
	case arrow.FLOAT64:
		return newPhysicalSortFloat64Column(chunks, numRows, validityNulls), nil
	case arrow.DECIMAL32:
		return newPhysicalSortDecimal32Column(chunks, numRows, validityNulls), nil
	case arrow.DECIMAL64:
		return newPhysicalSortDecimal64Column(chunks, numRows, validityNulls), nil
	case arrow.DECIMAL128:
		return newPhysicalSortDecimal128Column(chunks, numRows, validityNulls), nil
	case arrow.DECIMAL256:
		return newPhysicalSortDecimal256Column(chunks, numRows, validityNulls), nil
	case arrow.INTERVAL_MONTHS:
		return newPhysicalSortMonthIntervalColumn(chunks, numRows, validityNulls), nil
	case arrow.INTERVAL_DAY_TIME:
		return newPhysicalSortDayTimeColumn(chunks, numRows, validityNulls), nil
	case arrow.INTERVAL_MONTH_DAY_NANO:
		return newPhysicalSortMonthDayNanoColumn(chunks, numRows, validityNulls), nil
	case arrow.BOOL:
		return newPhysicalSortBoolColumn(chunks, numRows, validityNulls), nil
	case arrow.STRING:
		return newPhysicalSortStringColumn(chunks, numRows, validityNulls), nil
	case arrow.LARGE_STRING:
		return newPhysicalSortLargeStringColumn(chunks, numRows, validityNulls), nil
	case arrow.BINARY:
		return newPhysicalSortBinaryColumn(chunks, numRows, validityNulls), nil
	case arrow.LARGE_BINARY:
		return newPhysicalSortLargeBinaryColumn(chunks, numRows, validityNulls), nil
	case arrow.FIXED_SIZE_BINARY:
		return newFixedSizeBinaryComparator(chunks, numRows, validityNulls)
	case arrow.EXTENSION:
		storageChunks, err := extensionStorageFixedSizeBinaryChunks(chunks)
		if err != nil {
			return nil, err
		}
		return newFixedSizeBinaryComparator(storageChunks, numRows, validityNulls)
	default:
		return nil, fmt.Errorf("%w: sorting not supported for type %s", arrow.ErrNotImplemented, typeID)
	}
}

// chunkIndexSpan represents a contiguous range of indices in the global order.
type chunkIndexSpan struct {
	lo, hi int
}

// mergeAdjacentStable merges sorted adjacent ranges [a0,a1) and [b0,b1) (a1 == b0) into indices[lo:hi]
// using a strict weak order: i is ordered before j iff less(i,j). Tie-breaking prefers the left range
// (stable merge, same as C++ std::merge with !comp(right,left)).
func mergeAdjacentStable(indices, tmp []uint64, a0, a1, b0, b1 int, less func(a, b uint64) bool) {
	i, j, k := a0, b0, a0
	for i < a1 && j < b1 {
		if !less(indices[j], indices[i]) {
			tmp[k] = indices[i]
			i++
		} else {
			tmp[k] = indices[j]
			j++
		}
		k++
	}
	for i < a1 {
		tmp[k] = indices[i]
		k++
		i++
	}
	for j < b1 {
		tmp[k] = indices[j]
		k++
		j++
	}
	copy(indices[a0:b1], tmp[a0:b1])
}

// pairwiseMergeSortedSpans merges already-sorted adjacent index spans (chunk batch rows in global
// order), matching Arrow C++ ChunkedMergeImpl / TableSorter batch merge (vector_sort.cc).
// spanScratch must have capacity >= len(spans); it ping-pongs with spans' backing during merging.
func pairwiseMergeSortedSpans(indices, tmp []uint64, spans []chunkIndexSpan, less func(a, b uint64) bool, spanScratch []chunkIndexSpan) {
	if len(spans) <= 1 {
		return
	}
	if cap(spanScratch) < len(spans) {
		panic("kernels: spanScratch cap < len(spans)")
	}
	cur := spans
	other := spanScratch[:0]
	for len(cur) > 1 {
		other = other[:0]
		for i := 0; i < len(cur); i += 2 {
			if i+1 < len(cur) {
				s0, s1 := cur[i], cur[i+1]
				mergeAdjacentStable(indices, tmp, s0.lo, s0.hi, s1.lo, s1.hi, less)
				other = append(other, chunkIndexSpan{s0.lo, s1.hi})
			} else {
				other = append(other, cur[i])
			}
		}
		cur, other = other, cur
	}
}

// alignedChunkBoundaries reports cumulative row offsets for chunk boundaries when every sort column
// has the same chunk count and matching chunk lengths (typical for Arrow tables).
func alignedChunkBoundaries(columns []*arrow.Chunked) ([]int, bool) {
	if len(columns) == 0 {
		return nil, false
	}
	ch0 := columns[0].Chunks()
	n := len(ch0)
	if n == 0 {
		return nil, false
	}
	offs := make([]int, n+1)
	for i := range n {
		chunkLength := ch0[i].Len()
		for _, col := range columns[1:] {
			cj := col.Chunks()
			if len(cj) != n || cj[i].Len() != chunkLength {
				return nil, false
			}
		}
		offs[i+1] = offs[i] + chunkLength
	}
	if offs[n] != columns[0].Len() {
		return nil, false
	}
	return offs, true
}

// sortIndicesSingleColumnChunked implements Arrow C++ ChunkedArraySorter for one logical column:
// per-chunk array sort (partition + sort finites), then pairwise merge (ChunkedMergeImpl).
func sortIndicesSingleColumnChunked(indices []uint64, chunks []arrow.Array, comp columnComparator, key SortKey, tmp []uint64, spanScratch []chunkIndexSpan) {
	lo := 0
	for _, ch := range chunks {
		hi := lo + ch.Len()
		arraySortOneColumnRange(indices, tmp, comp, key, lo, hi)
		lo = hi
	}

	nChunks := len(chunks)
	if nChunks <= 1 {
		return
	}

	less := func(a, b uint64) bool { return comp.compareRowsForKey(a, b, key) < 0 }

	spans := make([]chunkIndexSpan, nChunks)
	lo = 0
	for i, ch := range chunks {
		hi := lo + ch.Len()
		spans[i] = chunkIndexSpan{lo, hi}
		lo = hi
	}
	pairwiseMergeSortedSpans(indices, tmp, spans, less, spanScratch)
}

// sortIndicesMultiColumnAlignedChunks sorts each aligned chunk (C++ RadixRecordBatchSorter or
// MultipleKeyRecordBatchSorter), then merges like Arrow C++ TableSorter.
func sortIndicesMultiColumnAlignedChunks(indices []uint64, offs []int, comparators []columnComparator, keys []SortKey, multiComp *multiColumnComparator, tmp []uint64, spanScratch []chunkIndexSpan) {
	nChunks := len(offs) - 1
	useRadix := len(keys) <= maxRadixSortKeys
	for c := range nChunks {
		lo, hi := offs[c], offs[c+1]
		if useRadix {
			radixRecordBatchSortRange(indices, tmp, comparators, keys, 0, lo, hi)
		} else {
			multipleKeyRecordBatchSortRange(indices, tmp, comparators, keys, lo, hi, makeTailComparator(comparators, keys, 1))
		}
	}
	if nChunks <= 1 {
		return
	}
	less := func(a, b uint64) bool { return multiComp.compare(a, b) < 0 }
	spans := make([]chunkIndexSpan, nChunks)
	for c := range nChunks {
		spans[c] = chunkIndexSpan{offs[c], offs[c+1]}
	}
	pairwiseMergeSortedSpans(indices, tmp, spans, less, spanScratch)
}

// SortIndices returns a stable permutation of 0..n-1 that would lexicographically sort the given
// columns. Each *arrow.Chunked is used via its .Chunks() only—no concatenate.
//
// Important: columns[i] pairs with keys[i] for order and null placement on that column.
// This kernel expects the public API to have already extracted the relevant columns from the input batch/table.
func SortIndices(ctx *exec.KernelCtx, columns []*arrow.Chunked, keys []SortKey) (*exec.ExecResult, error) {
	if len(columns) == 0 || len(keys) == 0 {
		return nil, fmt.Errorf("%w: must have at least one column and one sort key", arrow.ErrInvalid)
	}

	if len(columns) != len(keys) {
		return nil, fmt.Errorf("%w: number of columns (%d) must match number of sort keys (%d)",
			arrow.ErrInvalid, len(columns), len(keys))
	}

	length := int64(columns[0].Len())
	for _, col := range columns {
		if int64(col.Len()) != length {
			return nil, fmt.Errorf("%w: all columns must have the same length", arrow.ErrInvalid)
		}
	}

	comparators := make([]columnComparator, len(columns))
	nRows := int(length)
	for i, col := range columns {
		comp, err := createChunkedComparator(col.Chunks(), nRows)
		if err != nil {
			return nil, err
		}
		comparators[i] = comp
	}

	multiComp := &multiColumnComparator{
		columns: comparators,
		keys:    keys,
	}

	out := &exec.ExecResult{}
	out.Len = length
	out.Type = arrow.PrimitiveTypes.Uint64
	out.Nulls = 0

	buf := ctx.Allocate(int(length) * arrow.Uint64SizeBytes)
	indices := arrow.GetData[uint64](buf.Buf())[:length]

	for i := range indices {
		indices[i] = uint64(i)
	}

	if len(keys) == 1 {
		chunks := columns[0].Chunks()
		if len(chunks) > 1 {
			tmpBuf := ctx.Allocate(nRows * arrow.Uint64SizeBytes)
			tmp := arrow.GetData[uint64](tmpBuf.Buf())[:nRows]
			spanScratch := make([]chunkIndexSpan, len(chunks))
			sortIndicesSingleColumnChunked(indices, chunks, comparators[0], keys[0], tmp, spanScratch)
		} else {
			k0 := keys[0]
			c0 := comparators[0]
			if !c0.columnHasValidityNulls() && !c0.hasNullLikeValues() {
				slices.SortStableFunc(indices, func(a, b uint64) int { return c0.compareRowsForKey(a, b, k0) })
			} else {
				tmpBuf := ctx.Allocate(nRows * arrow.Uint64SizeBytes)
				tmp := arrow.GetData[uint64](tmpBuf.Buf())[:nRows]
				arraySortOneColumnRange(indices, tmp, c0, k0, 0, nRows)
			}
		}
	} else {
		useRadix := len(keys) <= maxRadixSortKeys
		offs, aligned := alignedChunkBoundaries(columns)
		nSeg := 1
		if aligned {
			nSeg = len(offs) - 1
		}
		multiChunkMerge := aligned && nSeg > 1

		tmpBuf := ctx.Allocate(nRows * arrow.Uint64SizeBytes)
		tmp := arrow.GetData[uint64](tmpBuf.Buf())[:nRows]

		var spanScratch []chunkIndexSpan
		if multiChunkMerge {
			spanScratch = make([]chunkIndexSpan, nSeg)
		}

		if multiChunkMerge {
			sortIndicesMultiColumnAlignedChunks(indices, offs, comparators, keys, multiComp, tmp, spanScratch)
		} else if useRadix {
			radixRecordBatchSortRange(indices, tmp, comparators, keys, 0, 0, nRows)
		} else {
			multipleKeyRecordBatchSortRange(indices, tmp, comparators, keys, 0, nRows, makeTailComparator(comparators, keys, 1))
		}
	}

	out.Buffers[1].WrapBuffer(buf)

	return out, nil
}

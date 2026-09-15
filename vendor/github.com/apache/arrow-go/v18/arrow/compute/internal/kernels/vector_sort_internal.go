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
	"slices"
)

// Ported from Apache Arrow C++ vector_sort_internal.h / vector_sort.cc:
// GenericNullPartitionResult, PartitionNullsOnly, PartitionNullLikes, VisitConstantRanges,
// ConcreteRecordBatchColumnSorter::SortRange (radix / column-wise multi-key path).

// nullPartitionIndices holds inclusive-exclusive offsets into the indices buffer (like uint64_t*
// ranges in C++ GenericNullPartitionResult).
type nullPartitionIndices struct {
	nonNullsLo, nonNullsHi int
	nullsLo, nullsHi       int
}

// partitionNullsOnly mirrors PartitionNullsOnly<StablePartitioner> in vector_sort_internal.h.
func partitionNullsOnly(indices, scratch []uint64, lo, hi int, nullPlacement NullPlacement, isNull func(uint64) bool) nullPartitionIndices {
	if lo >= hi {
		return nullPartitionIndices{lo, lo, lo, lo}
	}
	hasNull := false
	for i := lo; i < hi; i++ {
		if isNull(indices[i]) {
			hasNull = true
			break
		}
	}
	if !hasNull {
		return nullPartitionNoValidityNulls(lo, hi, nullPlacement)
	}
	if nullPlacement == NullsAtStart {
		t := 0
		for i := lo; i < hi; i++ {
			if isNull(indices[i]) {
				scratch[lo+t] = indices[i]
				t++
			}
		}
		nullEnd := lo + t
		u := t
		for i := lo; i < hi; i++ {
			if !isNull(indices[i]) {
				scratch[lo+u] = indices[i]
				u++
			}
		}
		copy(indices[lo:hi], scratch[lo:hi])
		return nullPartitionIndices{nullEnd, hi, lo, nullEnd}
	}
	t := 0
	for i := lo; i < hi; i++ {
		if !isNull(indices[i]) {
			scratch[lo+t] = indices[i]
			t++
		}
	}
	nnEnd := lo + t
	u := t
	for i := lo; i < hi; i++ {
		if isNull(indices[i]) {
			scratch[lo+u] = indices[i]
			u++
		}
	}
	copy(indices[lo:hi], scratch[lo:hi])
	return nullPartitionIndices{lo, nnEnd, nnEnd, hi}
}

// partitionNullLikes mirrors PartitionNullLikes in vector_sort_internal.h (NaN for floats).
func partitionNullLikes(indices, scratch []uint64, lo, hi int, nullPlacement NullPlacement, hasNullLike bool, isNullLike func(uint64) bool) nullPartitionIndices {
	if !hasNullLike || lo >= hi {
		return nullPartitionNoValidityNulls(lo, hi, nullPlacement)
	}
	hasLike := false
	for i := lo; i < hi; i++ {
		if isNullLike(indices[i]) {
			hasLike = true
			break
		}
	}
	if !hasLike {
		return nullPartitionNoValidityNulls(lo, hi, nullPlacement)
	}
	if nullPlacement == NullsAtStart {
		t := 0
		for i := lo; i < hi; i++ {
			if isNullLike(indices[i]) {
				scratch[lo+t] = indices[i]
				t++
			}
		}
		likeEnd := lo + t
		u := t
		for i := lo; i < hi; i++ {
			if !isNullLike(indices[i]) {
				scratch[lo+u] = indices[i]
				u++
			}
		}
		copy(indices[lo:hi], scratch[lo:hi])
		return nullPartitionIndices{likeEnd, hi, lo, likeEnd}
	}
	t := 0
	for i := lo; i < hi; i++ {
		if !isNullLike(indices[i]) {
			scratch[lo+t] = indices[i]
			t++
		}
	}
	finEnd := lo + t
	u := t
	for i := lo; i < hi; i++ {
		if isNullLike(indices[i]) {
			scratch[lo+u] = indices[i]
			u++
		}
	}
	copy(indices[lo:hi], scratch[lo:hi])
	return nullPartitionIndices{lo, finEnd, finEnd, hi}
}

// nullPartitionNoValidityNulls is PartitionNullsOnly when null_count == 0 (vector_sort_internal.h).
func nullPartitionNoValidityNulls(lo, hi int, nullPlacement NullPlacement) nullPartitionIndices {
	if nullPlacement == NullsAtStart {
		return nullPartitionIndices{lo, hi, lo, lo}
	}
	return nullPartitionIndices{lo, hi, hi, hi}
}

// visitConstantRanges mirrors VisitConstantRanges in vector_sort.cc.
// seg is a view of the permutation buffer (e.g. indices[finiteLo:finiteHi]). visit(rs, re) receives
// half-open offsets relative to seg; the caller maps them into the full indices slice.
func visitConstantRanges(seg []uint64, key SortKey, comp columnComparator, visit func(rs, re int)) {
	if len(seg) <= 1 {
		return
	}
	rs := 0
	for t := 1; t <= len(seg); t++ {
		if t < len(seg) && comp.compareRowsForKey(seg[t-1], seg[t], key) == 0 {
			continue
		}
		if t-rs > 0 {
			visit(rs, t)
		}
		rs = t
	}
}

// radixRecordBatchSortRange mirrors ConcreteRecordBatchColumnSorter::SortRange in vector_sort.cc.
func radixRecordBatchSortRange(indices []uint64, scratch []uint64, comparators []columnComparator, keys []SortKey, keyIdx, lo, hi int) {
	if hi-lo <= 1 || keyIdx >= len(keys) {
		return
	}
	key := keys[keyIdx]
	comp := comparators[keyIdx]

	var p nullPartitionIndices
	if comp.columnHasValidityNulls() {
		p = partitionNullsOnly(indices, scratch, lo, hi, key.NullPlacement, comp.isNullAt)
	} else {
		p = nullPartitionNoValidityNulls(lo, hi, key.NullPlacement)
	}
	q := partitionNullLikes(indices, scratch, p.nonNullsLo, p.nonNullsHi, key.NullPlacement, comp.hasNullLikeValues(), comp.isNullLikeAt)

	nanLo, nanHi := q.nullsLo, q.nullsHi
	finiteLo, finiteHi := q.nonNullsLo, q.nonNullsHi
	nullLo, nullHi := p.nullsLo, p.nullsHi

	slices.SortStableFunc(indices[finiteLo:finiteHi], func(a, b uint64) int {
		return comp.compareRowsForKey(a, b, key)
	})

	if keyIdx == len(keys)-1 {
		return
	}
	next := keyIdx + 1

	// Same order as C++: null-likes, true nulls, then tie ranges among finite values.
	radixRecordBatchSortRange(indices, scratch, comparators, keys, next, nanLo, nanHi)
	radixRecordBatchSortRange(indices, scratch, comparators, keys, next, nullLo, nullHi)
	visitConstantRanges(indices[finiteLo:finiteHi], key, comp, func(rs, re int) {
		radixRecordBatchSortRange(indices, scratch, comparators, keys, next, finiteLo+rs, finiteLo+re)
	})
}

// multipleKeyRecordBatchSortRange mirrors MultipleKeyRecordBatchSorter::SortInternal in vector_sort.cc:
// partition nulls / null-likes on the first key, stable_sort non-null finites with tail comparator.
func multipleKeyRecordBatchSortRange(indices []uint64, scratch []uint64, comparators []columnComparator, keys []SortKey, lo, hi int, tail func(a, b uint64) int) {
	if hi-lo <= 1 {
		return
	}
	key := keys[0]
	comp := comparators[0]
	var p nullPartitionIndices
	if comp.columnHasValidityNulls() {
		p = partitionNullsOnly(indices, scratch, lo, hi, key.NullPlacement, comp.isNullAt)
	} else {
		p = nullPartitionNoValidityNulls(lo, hi, key.NullPlacement)
	}
	q := partitionNullLikes(indices, scratch, p.nonNullsLo, p.nonNullsHi, key.NullPlacement, comp.hasNullLikeValues(), comp.isNullLikeAt)
	finiteLo, finiteHi := q.nonNullsLo, q.nonNullsHi
	nanLo, nanHi := q.nullsLo, q.nullsHi
	nullLo, nullHi := p.nullsLo, p.nullsHi

	slices.SortStableFunc(indices[finiteLo:finiteHi], func(a, b uint64) int {
		va := comp.compareRowsForKey(a, b, key)
		if va != 0 {
			return va
		}
		return tail(a, b)
	})

	slices.SortStableFunc(indices[nanLo:nanHi], tail)
	slices.SortStableFunc(indices[nullLo:nullHi], tail)
}

// makeTailComparator returns lexicographic compare for keys[from:], analogous to C++
// MultipleKeyComparator::CompareInternal(left, right, from) (vector_sort_internal.h).
func makeTailComparator(comparators []columnComparator, keys []SortKey, from int) func(a, b uint64) int {
	return func(a, b uint64) int {
		for i := from; i < len(keys); i++ {
			if v := comparators[i].compareRowsForKey(a, b, keys[i]); v != 0 {
				return v
			}
		}
		return 0
	}
}

// arraySortOneColumnRange mirrors a single-column ArraySort / chunk step in ChunkedArraySorter
// (partition nulls and null-likes, then stable_sort the finite non-null-like slice only).
func arraySortOneColumnRange(indices []uint64, scratch []uint64, comp columnComparator, key SortKey, lo, hi int) {
	if hi-lo <= 1 {
		return
	}
	if !comp.columnHasValidityNulls() && !comp.hasNullLikeValues() {
		slices.SortStableFunc(indices[lo:hi], func(a, b uint64) int {
			return comp.compareRowsForKey(a, b, key)
		})
		return
	}
	var p nullPartitionIndices
	if comp.columnHasValidityNulls() {
		p = partitionNullsOnly(indices, scratch, lo, hi, key.NullPlacement, comp.isNullAt)
	} else {
		p = nullPartitionNoValidityNulls(lo, hi, key.NullPlacement)
	}
	q := partitionNullLikes(indices, scratch, p.nonNullsLo, p.nonNullsHi, key.NullPlacement, comp.hasNullLikeValues(), comp.isNullLikeAt)
	finiteLo, finiteHi := q.nonNullsLo, q.nonNullsHi
	slices.SortStableFunc(indices[finiteLo:finiteHi], func(a, b uint64) int {
		return comp.compareRowsForKey(a, b, key)
	})
}

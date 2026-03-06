/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2026 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package set

import (
	"cmp"
	"slices"
)

// Set - uses map as a set of comparable elements.
//
// Important Caveats:
//   - Sets are unordered by nature. Map iteration order is non-deterministic in Go.
//   - When converting to slices, use ToSlice() with a comparison function or
//     ToSliceOrdered() for ordered types to get deterministic, sorted results.
//   - Comparison functions must provide total ordering: if your comparison returns 0
//     for different elements, their relative order in the result is undefined.
//   - For deterministic ordering when elements may compare equal, use secondary
//     sort criteria (e.g., sort by length first, then alphabetically for ties).
type Set[T comparable] map[T]struct{}

// ToSlice - returns Set as a slice sorted using the provided comparison function.
// If cmpFn is nil, the slice order is undefined (non-deterministic).
//
// Important: The comparison function should provide total ordering. If it returns 0
// for elements that are not identical, their relative order in the result is undefined.
// For deterministic results, use secondary sort criteria for tie-breaking.
func (set Set[T]) ToSlice(cmpFn func(a, b T) int) []T {
	keys := make([]T, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	if cmpFn != nil {
		slices.SortFunc(keys, cmpFn)
	}
	return keys
}

// ToSliceOrdered - returns Set as a sorted slice for ordered types.
// This is a convenience method for types that implement cmp.Ordered.
// The result is deterministic and always sorted in ascending order.
func ToSliceOrdered[T cmp.Ordered](set Set[T]) []T {
	keys := make([]T, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// IsEmpty - returns whether the set is empty or not.
func (set Set[T]) IsEmpty() bool {
	return len(set) == 0
}

// Add - adds element to the set.
func (set Set[T]) Add(s T) {
	set[s] = struct{}{}
}

// Remove - removes element from the set. It does nothing if element does not exist in the set.
func (set Set[T]) Remove(s T) {
	delete(set, s)
}

// Contains - checks if element is in the set.
func (set Set[T]) Contains(s T) bool {
	_, ok := set[s]
	return ok
}

// FuncMatch - returns new set containing each value that passes match function.
// A 'matchFn' should accept element in a set as first argument and
// 'matchValue' as second argument. The function can do any logic to
// compare both the arguments and should return true to accept element in
// a set to include in output set else the element is ignored.
func (set Set[T]) FuncMatch(matchFn func(T, T) bool, matchValue T) Set[T] {
	nset := New[T]()
	for k := range set {
		if matchFn(k, matchValue) {
			nset.Add(k)
		}
	}
	return nset
}

// ApplyFunc - returns new set containing each value processed by 'applyFn'.
// A 'applyFn' should accept element in a set as an argument and return
// a processed value. The function can do any logic to return a processed value.
func (set Set[T]) ApplyFunc(applyFn func(T) T) Set[T] {
	nset := New[T]()
	for k := range set {
		nset.Add(applyFn(k))
	}
	return nset
}

// Equals - checks whether given set is equal to current set or not.
func (set Set[T]) Equals(sset Set[T]) bool {
	// If length of set is not equal to length of given set, the
	// set is not equal to given set.
	if len(set) != len(sset) {
		return false
	}

	// As both sets are equal in length, check each elements are equal.
	for k := range set {
		if _, ok := sset[k]; !ok {
			return false
		}
	}

	return true
}

// Intersection - returns the intersection with given set as new set.
func (set Set[T]) Intersection(sset Set[T]) Set[T] {
	nset := New[T]()
	for k := range set {
		if _, ok := sset[k]; ok {
			nset.Add(k)
		}
	}

	return nset
}

// Difference - returns the difference with given set as new set.
func (set Set[T]) Difference(sset Set[T]) Set[T] {
	nset := New[T]()
	for k := range set {
		if _, ok := sset[k]; !ok {
			nset.Add(k)
		}
	}

	return nset
}

// Union - returns the union with given set as new set.
func (set Set[T]) Union(sset Set[T]) Set[T] {
	nset := New[T]()
	for k := range set {
		nset.Add(k)
	}

	for k := range sset {
		nset.Add(k)
	}

	return nset
}

// New - creates new set.
func New[T comparable]() Set[T] {
	return make(Set[T])
}

// Create - creates new set with given values.
func Create[T comparable](sl ...T) Set[T] {
	set := make(Set[T], len(sl))
	for _, k := range sl {
		set.Add(k)
	}
	return set
}

// Copy - returns copy of given set.
func Copy[T comparable](set Set[T]) Set[T] {
	nset := make(Set[T], len(set))
	for k, v := range set {
		nset[k] = v
	}
	return nset
}

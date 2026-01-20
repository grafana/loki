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
	"encoding/json"
	"fmt"
)

// IntSet - uses map as set of ints.
// This is now implemented using the generic Set[int] type.
type IntSet Set[int]

// ToSlice - returns IntSet as int slice.
func (set IntSet) ToSlice() []int {
	return ToSliceOrdered(Set[int](set))
}

// IsEmpty - returns whether the set is empty or not.
func (set IntSet) IsEmpty() bool {
	return Set[int](set).IsEmpty()
}

// Add - adds int to the set.
func (set IntSet) Add(i int) {
	Set[int](set).Add(i)
}

// Remove - removes int in the set. It does nothing if int does not exist in the set.
func (set IntSet) Remove(i int) {
	Set[int](set).Remove(i)
}

// Contains - checks if int is in the set.
func (set IntSet) Contains(i int) bool {
	return Set[int](set).Contains(i)
}

// FuncMatch - returns new set containing each value who passes match function.
// A 'matchFn' should accept element in a set as first argument and
// 'matchInt' as second argument. The function can do any logic to
// compare both the arguments and should return true to accept element in
// a set to include in output set else the element is ignored.
func (set IntSet) FuncMatch(matchFn func(int, int) bool, matchInt int) IntSet {
	return IntSet(Set[int](set).FuncMatch(matchFn, matchInt))
}

// ApplyFunc - returns new set containing each value processed by 'applyFn'.
// A 'applyFn' should accept element in a set as a argument and return
// a processed int. The function can do any logic to return a processed
// int.
func (set IntSet) ApplyFunc(applyFn func(int) int) IntSet {
	return IntSet(Set[int](set).ApplyFunc(applyFn))
}

// Equals - checks whether given set is equal to current set or not.
func (set IntSet) Equals(iset IntSet) bool {
	return Set[int](set).Equals(Set[int](iset))
}

// Intersection - returns the intersection with given set as new set.
func (set IntSet) Intersection(iset IntSet) IntSet {
	return IntSet(Set[int](set).Intersection(Set[int](iset)))
}

// Difference - returns the difference with given set as new set.
func (set IntSet) Difference(iset IntSet) IntSet {
	return IntSet(Set[int](set).Difference(Set[int](iset)))
}

// Union - returns the union with given set as new set.
func (set IntSet) Union(iset IntSet) IntSet {
	return IntSet(Set[int](set).Union(Set[int](iset)))
}

// MarshalJSON - converts to JSON data.
func (set IntSet) MarshalJSON() ([]byte, error) {
	return json.Marshal(set.ToSlice())
}

// UnmarshalJSON - parses JSON data and creates new set with it.
func (set *IntSet) UnmarshalJSON(data []byte) error {
	sl := []int{}
	var err error
	if err = json.Unmarshal(data, &sl); err == nil {
		*set = make(IntSet)
		for _, i := range sl {
			set.Add(i)
		}
	}
	return err
}

// String - returns printable string of the set.
func (set IntSet) String() string {
	return fmt.Sprintf("%v", set.ToSlice())
}

// NewIntSet - creates new int set.
func NewIntSet() IntSet {
	return IntSet(New[int]())
}

// CreateIntSet - creates new int set with given int values.
func CreateIntSet(il ...int) IntSet {
	return IntSet(Create(il...))
}

// CopyIntSet - returns copy of given set.
func CopyIntSet(set IntSet) IntSet {
	return IntSet(Copy(Set[int](set)))
}

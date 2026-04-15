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
	"sort"
)

// StringSet - uses map as set of strings.
// This is now implemented using the generic Set[string] type.
type StringSet Set[string]

// ToSlice - returns StringSet as string slice.
func (set StringSet) ToSlice() []string {
	return ToSliceOrdered(Set[string](set))
}

// ToByteSlices - returns StringSet as a sorted
// slice of byte slices, using only one allocation.
func (set StringSet) ToByteSlices() [][]byte {
	length := 0
	for k := range set {
		length += len(k)
	}
	// Preallocate the slice with the total length of all strings
	// to avoid multiple allocations.
	dst := make([]byte, length)

	// Add keys to this...
	keys := make([][]byte, 0, len(set))
	for k := range set {
		n := copy(dst, k)
		keys = append(keys, dst[:n])
		dst = dst[n:]
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})
	return keys
}

// IsEmpty - returns whether the set is empty or not.
func (set StringSet) IsEmpty() bool {
	return Set[string](set).IsEmpty()
}

// Add - adds string to the set.
func (set StringSet) Add(s string) {
	Set[string](set).Add(s)
}

// Remove - removes string in the set.  It does nothing if string does not exist in the set.
func (set StringSet) Remove(s string) {
	Set[string](set).Remove(s)
}

// Contains - checks if string is in the set.
func (set StringSet) Contains(s string) bool {
	return Set[string](set).Contains(s)
}

// FuncMatch - returns new set containing each value who passes match function.
// A 'matchFn' should accept element in a set as first argument and
// 'matchString' as second argument.  The function can do any logic to
// compare both the arguments and should return true to accept element in
// a set to include in output set else the element is ignored.
func (set StringSet) FuncMatch(matchFn func(string, string) bool, matchString string) StringSet {
	return StringSet(Set[string](set).FuncMatch(matchFn, matchString))
}

// ApplyFunc - returns new set containing each value processed by 'applyFn'.
// A 'applyFn' should accept element in a set as a argument and return
// a processed string.  The function can do any logic to return a processed
// string.
func (set StringSet) ApplyFunc(applyFn func(string) string) StringSet {
	return StringSet(Set[string](set).ApplyFunc(applyFn))
}

// Equals - checks whether given set is equal to current set or not.
func (set StringSet) Equals(sset StringSet) bool {
	return Set[string](set).Equals(Set[string](sset))
}

// Intersection - returns the intersection with given set as new set.
func (set StringSet) Intersection(sset StringSet) StringSet {
	return StringSet(Set[string](set).Intersection(Set[string](sset)))
}

// Difference - returns the difference with given set as new set.
func (set StringSet) Difference(sset StringSet) StringSet {
	return StringSet(Set[string](set).Difference(Set[string](sset)))
}

// Union - returns the union with given set as new set.
func (set StringSet) Union(sset StringSet) StringSet {
	return StringSet(Set[string](set).Union(Set[string](sset)))
}

// MarshalJSON - converts to JSON data.
func (set StringSet) MarshalJSON() ([]byte, error) {
	return json.Marshal(set.ToSlice())
}

// UnmarshalJSON - parses JSON data and creates new set with it.
func (set *StringSet) UnmarshalJSON(data []byte) error {
	sl := []interface{}{}
	var err error
	if err = json.Unmarshal(data, &sl); err == nil {
		*set = make(StringSet)
		for _, s := range sl {
			set.Add(fmt.Sprintf("%v", s))
		}
	} else {
		var s interface{}
		if err = json.Unmarshal(data, &s); err == nil {
			*set = make(StringSet)
			set.Add(fmt.Sprintf("%v", s))
		}
	}

	return err
}

// String - returns printable string of the set.
func (set StringSet) String() string {
	return fmt.Sprintf("%s", set.ToSlice())
}

// NewStringSet - creates new string set.
func NewStringSet() StringSet {
	return StringSet(New[string]())
}

// CreateStringSet - creates new string set with given string values.
func CreateStringSet(sl ...string) StringSet {
	return StringSet(Create(sl...))
}

// CopyStringSet - returns copy of given set.
func CopyStringSet(set StringSet) StringSet {
	return StringSet(Copy(Set[string](set)))
}

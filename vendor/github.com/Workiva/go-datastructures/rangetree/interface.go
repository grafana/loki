/*
Copyright 2014 Workiva, LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Package rangetree is designed to store n-dimensional data in an easy-to-query
way.  Given this package's primary use as representing cartesian data, this
information is represented by int64s at n-dimensions.  This implementation
is not actually a tree but a sparse n-dimensional list.  This package also
includes two implementations of this sparse list, one mutable (and not threadsafe)
and another that is immutable copy-on-write which is threadsafe.  The mutable
version is obviously faster but will likely have write contention for any
consumer that needs a threadsafe rangetree.

TODO: unify both implementations with the same interface.
*/
package rangetree

// Entry defines items that can be added to the rangetree.
type Entry interface {
	// ValueAtDimension returns the value of this entry
	// at the specified dimension.
	ValueAtDimension(dimension uint64) int64
}

// Interval describes the methods required to query the rangetree.  Note that
// all ranges are inclusive.
type Interval interface {
	// LowAtDimension returns an integer representing the lower bound
	// at the requested dimension.
	LowAtDimension(dimension uint64) int64
	// HighAtDimension returns an integer representing the higher bound
	// at the request dimension.
	HighAtDimension(dimension uint64) int64
}

// RangeTree describes the methods available to the rangetree.
type RangeTree interface {
	// Add will add the provided entries to the tree.  Any entries that
	// were overwritten will be returned in the order in which they
	// were overwritten.  If an entry's addition does not overwrite, a nil
	// is returned for that entry's index in the provided cells.
	Add(entries ...Entry) Entries
	// Len returns the number of entries in the tree.
	Len() uint64
	// Delete will remove the provided entries from the tree.
	// Any entries that were deleted will be returned in the order in
	// which they were deleted.  If an entry does not exist to be deleted,
	// a nil is returned for that entry's index in the provided cells.
	Delete(entries ...Entry) Entries
	// Query will return a list of entries that fall within
	// the provided interval.  The values at dimensions are inclusive.
	Query(interval Interval) Entries
	// Apply will call the provided function with each entry that exists
	// within the provided range, in order.  Return false at any time to
	// cancel iteration.  Altering the entry in such a way that its location
	// changes will result in undefined behavior.
	Apply(interval Interval, fn func(Entry) bool)
	// Get returns any entries that exist at the addresses provided by the
	// given entries.  Entries are returned in the order in which they are
	// received.  If an entry cannot be found, a nil is returned in its
	// place.
	Get(entries ...Entry) Entries
	// InsertAtDimension will increment items at and above the given index
	// by the number provided.  Provide a negative number to to decrement.
	// Returned are two lists.  The first list is a list of entries that
	// were moved.  The second is a list entries that were deleted.  These
	// lists are exclusive.
	InsertAtDimension(dimension uint64, index, number int64) (Entries, Entries)
}

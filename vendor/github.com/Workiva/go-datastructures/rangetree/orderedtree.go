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
package rangetree

func isLastDimension(value, test uint64) bool {
	return test >= value
}

type nodeBundle struct {
	list  *orderedNodes
	index int
}

type orderedTree struct {
	top        orderedNodes
	number     uint64
	dimensions uint64
	path       []*nodeBundle
}

func (ot *orderedTree) resetPath() {
	ot.path = ot.path[:0]
}

func (ot *orderedTree) needNextDimension() bool {
	return ot.dimensions > 1
}

// add will add the provided entry to the rangetree and return an
// entry if one was overwritten.
func (ot *orderedTree) add(entry Entry) *node {
	var node *node
	list := &ot.top

	for i := uint64(1); i <= ot.dimensions; i++ {
		if isLastDimension(ot.dimensions, i) {
			overwritten := list.add(
				newNode(entry.ValueAtDimension(i), entry, false),
			)
			if overwritten == nil {
				ot.number++
			}
			return overwritten
		}
		node, _ = list.getOrAdd(entry, i, ot.dimensions)
		list = &node.orderedNodes
	}

	return nil
}

// Add will add the provided entries to the tree.  This method
// returns a list of entries that were overwritten in the order
// in which entries were received.  If an entry doesn't overwrite
// anything, a nil will be returned for that entry in the returned
// slice.
func (ot *orderedTree) Add(entries ...Entry) Entries {
	if len(entries) == 0 {
		return nil
	}

	overwrittens := make(Entries, len(entries))
	for i, entry := range entries {
		if entry == nil {
			continue
		}

		overwritten := ot.add(entry)
		if overwritten != nil {
			overwrittens[i] = overwritten.entry
		}
	}

	return overwrittens
}

func (ot *orderedTree) delete(entry Entry) *node {
	ot.resetPath()
	var index int
	var node *node
	list := &ot.top

	for i := uint64(1); i <= ot.dimensions; i++ {
		value := entry.ValueAtDimension(i)
		node, index = list.get(value)
		if node == nil { // there's nothing to delete
			return nil
		}

		nb := &nodeBundle{list: list, index: index}
		ot.path = append(ot.path, nb)

		list = &node.orderedNodes
	}

	ot.number--

	for i := len(ot.path) - 1; i >= 0; i-- {
		nb := ot.path[i]
		nb.list.deleteAt(nb.index)
		if len(*nb.list) > 0 {
			break
		}
	}

	return node
}

func (ot *orderedTree) get(entry Entry) Entry {
	on := ot.top
	for i := uint64(1); i <= ot.dimensions; i++ {
		n, _ := on.get(entry.ValueAtDimension(i))
		if n == nil {
			return nil
		}
		if i == ot.dimensions {
			return n.entry
		}
		on = n.orderedNodes
	}

	return nil
}

// Get returns any entries that exist at the addresses provided by the
// given entries.  Entries are returned in the order in which they are
// received.  If an entry cannot be found, a nil is returned in its
// place.
func (ot *orderedTree) Get(entries ...Entry) Entries {
	result := make(Entries, 0, len(entries))
	for _, entry := range entries {
		result = append(result, ot.get(entry))
	}

	return result
}

// Delete will remove the provided entries from the tree.
// Any entries that were deleted will be returned in the order in
// which they were deleted.  If an entry does not exist to be deleted,
// a nil is returned for that entry's index in the provided cells.
func (ot *orderedTree) Delete(entries ...Entry) Entries {
	if len(entries) == 0 {
		return nil
	}

	deletedEntries := make(Entries, len(entries))
	for i, entry := range entries {
		if entry == nil {
			continue
		}

		deleted := ot.delete(entry)
		if deleted != nil {
			deletedEntries[i] = deleted.entry
		}
	}

	return deletedEntries
}

// Len returns the number of items in the tree.
func (ot *orderedTree) Len() uint64 {
	return ot.number
}

func (ot *orderedTree) apply(list orderedNodes, interval Interval,
	dimension uint64, fn func(*node) bool) bool {

	low, high := interval.LowAtDimension(dimension), interval.HighAtDimension(dimension)

	if isLastDimension(ot.dimensions, dimension) {
		if !list.apply(low, high, fn) {
			return false
		}
	} else {
		if !list.apply(low, high, func(n *node) bool {
			if !ot.apply(n.orderedNodes, interval, dimension+1, fn) {
				return false
			}
			return true
		}) {
			return false
		}
		return true
	}

	return true
}

// Apply will call (in order) the provided function to every
// entry that falls within the provided interval.  Any alteration
// the the entry that would result in different answers to the
// interface methods results in undefined behavior.
func (ot *orderedTree) Apply(interval Interval, fn func(Entry) bool) {
	ot.apply(ot.top, interval, 1, func(n *node) bool {
		return fn(n.entry)
	})
}

// Query will return an ordered list of results in the given
// interval.
func (ot *orderedTree) Query(interval Interval) Entries {
	entries := NewEntries()

	ot.apply(ot.top, interval, 1, func(n *node) bool {
		entries = append(entries, n.entry)
		return true
	})

	return entries
}

// InsertAtDimension will increment items at and above the given index
// by the number provided.  Provide a negative number to to decrement.
// Returned are two lists.  The first list is a list of entries that
// were moved.  The second is a list entries that were deleted.  These
// lists are exclusive.
func (ot *orderedTree) InsertAtDimension(dimension uint64,
	index, number int64) (Entries, Entries) {

	// TODO: perhaps return an error here?
	if dimension > ot.dimensions || number == 0 {
		return nil, nil
	}

	modified := make(Entries, 0, 100)
	deleted := make(Entries, 0, 100)

	ot.top.insert(dimension, 1, ot.dimensions,
		index, number, &modified, &deleted,
	)

	ot.number -= uint64(len(deleted))

	return modified, deleted
}

func newOrderedTree(dimensions uint64) *orderedTree {
	return &orderedTree{
		dimensions: dimensions,
		path:       make([]*nodeBundle, 0, dimensions),
	}
}

// New is the constructor to create a new rangetree with
// the provided number of dimensions.
func New(dimensions uint64) RangeTree {
	return newOrderedTree(dimensions)
}

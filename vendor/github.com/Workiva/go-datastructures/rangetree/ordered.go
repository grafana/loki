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

import "sort"

// orderedNodes represents an ordered list of points living
// at the last dimension.  No duplicates can be inserted here.
type orderedNodes nodes

func (nodes orderedNodes) search(value int64) int {
	return sort.Search(
		len(nodes),
		func(i int) bool { return nodes[i].value >= value },
	)
}

// addAt will add the provided node at the provided index.  Returns
// a node if one was overwritten.
func (nodes *orderedNodes) addAt(i int, node *node) *node {
	if i == len(*nodes) {
		*nodes = append(*nodes, node)
		return nil
	}

	if (*nodes)[i].value == node.value {
		overwritten := (*nodes)[i]
		// this is a duplicate, there can't be a duplicate
		// point in the last dimension
		(*nodes)[i] = node
		return overwritten
	}

	*nodes = append(*nodes, nil)
	copy((*nodes)[i+1:], (*nodes)[i:])
	(*nodes)[i] = node
	return nil
}

func (nodes *orderedNodes) add(node *node) *node {
	i := nodes.search(node.value)
	return nodes.addAt(i, node)
}

func (nodes *orderedNodes) deleteAt(i int) *node {
	if i >= len(*nodes) { // no matching found
		return nil
	}

	deleted := (*nodes)[i]
	copy((*nodes)[i:], (*nodes)[i+1:])
	(*nodes)[len(*nodes)-1] = nil
	*nodes = (*nodes)[:len(*nodes)-1]
	return deleted
}

func (nodes *orderedNodes) delete(value int64) *node {
	i := nodes.search(value)

	if (*nodes)[i].value != value || i == len(*nodes) {
		return nil
	}

	return nodes.deleteAt(i)
}

func (nodes orderedNodes) apply(low, high int64, fn func(*node) bool) bool {
	index := nodes.search(low)
	if index == len(nodes) {
		return true
	}

	for ; index < len(nodes); index++ {
		if nodes[index].value > high {
			break
		}

		if !fn(nodes[index]) {
			return false
		}
	}

	return true
}

func (nodes orderedNodes) get(value int64) (*node, int) {
	i := nodes.search(value)
	if i == len(nodes) {
		return nil, i
	}

	if nodes[i].value == value {
		return nodes[i], i
	}

	return nil, i
}

func (nodes *orderedNodes) getOrAdd(entry Entry,
	dimension, lastDimension uint64) (*node, bool) {

	isLastDimension := isLastDimension(lastDimension, dimension)
	value := entry.ValueAtDimension(dimension)

	i := nodes.search(value)
	if i == len(*nodes) {
		node := newNode(value, entry, !isLastDimension)
		*nodes = append(*nodes, node)
		return node, true
	}

	if (*nodes)[i].value == value {
		return (*nodes)[i], false
	}

	node := newNode(value, entry, !isLastDimension)
	*nodes = append(*nodes, nil)
	copy((*nodes)[i+1:], (*nodes)[i:])
	(*nodes)[i] = node
	return node, true
}

func (nodes orderedNodes) flatten(entries *Entries) {
	for _, node := range nodes {
		if node.orderedNodes != nil {
			node.orderedNodes.flatten(entries)
		} else {
			*entries = append(*entries, node.entry)
		}
	}
}

func (nodes *orderedNodes) insert(insertDimension, dimension, maxDimension uint64,
	index, number int64, modified, deleted *Entries) {

	lastDimension := isLastDimension(maxDimension, dimension)

	if insertDimension == dimension {
		i := nodes.search(index)
		var toDelete []int

		for j := i; j < len(*nodes); j++ {
			(*nodes)[j].value += number
			if (*nodes)[j].value < index {
				toDelete = append(toDelete, j)
				if lastDimension {
					*deleted = append(*deleted, (*nodes)[j].entry)
				} else {
					(*nodes)[j].orderedNodes.flatten(deleted)
				}
				continue
			}
			if lastDimension {
				*modified = append(*modified, (*nodes)[j].entry)
			} else {
				(*nodes)[j].orderedNodes.flatten(modified)
			}
		}

		for i, index := range toDelete {
			nodes.deleteAt(index - i)
		}

		return
	}

	for _, node := range *nodes {
		node.orderedNodes.insert(
			insertDimension, dimension+1, maxDimension,
			index, number, modified, deleted,
		)
	}
}

func (nodes orderedNodes) immutableInsert(insertDimension, dimension, maxDimension uint64,
	index, number int64, modified, deleted *Entries) orderedNodes {

	lastDimension := isLastDimension(maxDimension, dimension)

	cp := make(orderedNodes, len(nodes))
	copy(cp, nodes)

	if insertDimension == dimension {
		i := cp.search(index)
		var toDelete []int

		for j := i; j < len(cp); j++ {
			nn := newNode(cp[j].value+number, cp[j].entry, !lastDimension)
			nn.orderedNodes = cp[j].orderedNodes
			cp[j] = nn
			if cp[j].value < index {
				toDelete = append(toDelete, j)
				if lastDimension {
					*deleted = append(*deleted, cp[j].entry)
				} else {
					cp[j].orderedNodes.flatten(deleted)
				}
				continue
			}
			if lastDimension {
				*modified = append(*modified, cp[j].entry)
			} else {
				cp[j].orderedNodes.flatten(modified)
			}
		}

		for _, index := range toDelete {
			cp.deleteAt(index)
		}

		return cp
	}

	for i := 0; i < len(cp); i++ {
		oldNode := nodes[i]
		nn := newNode(oldNode.value, oldNode.entry, !lastDimension)
		nn.orderedNodes = oldNode.orderedNodes.immutableInsert(
			insertDimension, dimension+1,
			maxDimension,
			index, number,
			modified, deleted,
		)
		cp[i] = nn
	}

	return cp
}

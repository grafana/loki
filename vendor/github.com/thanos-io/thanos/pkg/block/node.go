// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package block

import (
	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block/metadata"
)

// Node type represents a node of a tree.
type Node struct {
	metadata.Meta
	Children []*Node
}

// NewNode creates a new node with children as empty slice.
func NewNode(meta *metadata.Meta) *Node {
	return &Node{
		Meta:     *meta,
		Children: []*Node{},
	}
}

// getNonRootIDs returns list of ids which are not on root level.
func getNonRootIDs(root *Node) []ulid.ULID {
	var ulids []ulid.ULID
	for _, node := range root.Children {
		ulids = append(ulids, childrenToULIDs(node)...)
		ulids = remove(ulids, node.ULID)
	}
	return ulids
}

func childrenToULIDs(a *Node) []ulid.ULID {
	var ulids = []ulid.ULID{a.ULID}
	for _, childNode := range a.Children {
		ulids = append(ulids, childrenToULIDs(childNode)...)
	}
	return ulids
}

func remove(items []ulid.ULID, item ulid.ULID) []ulid.ULID {
	newitems := []ulid.ULID{}

	for _, i := range items {
		if i.Compare(item) != 0 {
			newitems = append(newitems, i)
		}
	}

	return newitems
}

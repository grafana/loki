// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	v2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// ItemsChanges represent changes found between a left (original) and right (modified) object. Items is only
// used by Swagger documents.
type ItemsChanges struct {
	*PropertyChanges
	ItemsChanges *ItemsChanges `json:"items,omitempty" yaml:"items,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Items objects
func (i *ItemsChanges) GetAllChanges() []*Change {
	if i == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, i.Changes...)
	if i.ItemsChanges != nil {
		changes = append(changes, i.ItemsChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes found between two Items objects
// This is a recursive function because Items can contain Items. Be careful!
func (i *ItemsChanges) TotalChanges() int {
	if i == nil {
		return 0
	}
	c := i.PropertyChanges.TotalChanges()
	if i.ItemsChanges != nil {
		c += i.ItemsChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes found between two Swagger Items objects
// This is a recursive method, Items are recursive, be careful!
func (i *ItemsChanges) TotalBreakingChanges() int {
	c := i.PropertyChanges.TotalBreakingChanges()
	if i.ItemsChanges != nil {
		c += i.ItemsChanges.TotalBreakingChanges()
	}
	return c
}

// CompareItems compares two sets of Swagger Item objects. If there are any changes found then a pointer to
// ItemsChanges will be returned, otherwise nil is returned.
//
// It is worth nothing that Items can contain Items. This means recursion is possible and has the potential for
// runaway code if not using the resolver's circular reference checking.
func CompareItems(l, r *v2.Items) *ItemsChanges {
	var changes []*Change
	var props []*PropertyCheck

	ic := new(ItemsChanges)

	// header is identical to items, except for a description.
	props = append(props, addSwaggerHeaderProperties(l, r, &changes)...)
	CheckProperties(props)

	if !l.Items.IsEmpty() && !r.Items.IsEmpty() {
		// inline, check hashes, if they don't match, compare.
		if l.Items.Value.Hash() != r.Items.Value.Hash() {
			// compare.
			ic.ItemsChanges = CompareItems(l.Items.Value, r.Items.Value)
		}
	}
	if l.Items.IsEmpty() && !r.Items.IsEmpty() {
		// added items
		CreateChange(&changes, PropertyAdded, v3.ItemsLabel,
			nil, r.Items.GetValueNode(), true, nil, r.Items.GetValue())
	}
	if !l.Items.IsEmpty() && r.Items.IsEmpty() {
		// removed items
		CreateChange(&changes, PropertyRemoved, v3.ItemsLabel,
			l.Items.GetValueNode(), nil, true, l.Items.GetValue(),
			nil)
	}
	ic.PropertyChanges = NewPropertyChanges(changes)
	if ic.TotalChanges() <= 0 {
		return nil
	}
	return ic
}

// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// ContactChanges Represent changes to a Contact object that is a child of Info, part of an OpenAPI document.
type ContactChanges struct {
	*PropertyChanges
}

// GetAllChanges returns a slice of all changes made between Callback objects
func (c *ContactChanges) GetAllChanges() []*Change {
	if c == nil {
		return nil
	}
	return c.Changes
}

// TotalChanges represents the total number of changes that have occurred to a Contact object
func (c *ContactChanges) TotalChanges() int {
	if c == nil {
		return 0
	}
	return c.PropertyChanges.TotalChanges()
}

// TotalBreakingChanges returns the total number of breaking changes in Contact objects.
func (c *ContactChanges) TotalBreakingChanges() int {
	if c == nil {
		return 0
	}
	return c.PropertyChanges.TotalBreakingChanges()
}

// CompareContact will check a left (original) and right (new) Contact object for any changes. If there
// were any, a pointer to a ContactChanges object is returned, otherwise if nothing changed - the function
// returns nil.
func CompareContact(l, r *base.Contact) *ContactChanges {
	var changes []*Change
	props := make([]*PropertyCheck, 0, 3)

	props = append(props,
		NewPropertyCheck(CompContact, PropURL,
			l.URL.ValueNode, r.URL.ValueNode,
			v3.URLLabel, &changes, l, r),
		NewPropertyCheck(CompContact, PropName,
			l.Name.ValueNode, r.Name.ValueNode,
			v3.NameLabel, &changes, l, r),
		NewPropertyCheck(CompContact, PropEmail,
			l.Email.ValueNode, r.Email.ValueNode,
			v3.EmailLabel, &changes, l, r),
	)

	CheckProperties(props)

	dc := new(ContactChanges)
	dc.PropertyChanges = NewPropertyChanges(changes)
	if dc.TotalChanges() <= 0 {
		return nil
	}
	return dc
}

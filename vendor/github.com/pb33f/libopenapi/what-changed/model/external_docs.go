// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// ExternalDocChanges represents changes made to any ExternalDoc object from an OpenAPI document.
type ExternalDocChanges struct {
	*PropertyChanges
	ExtensionChanges *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Example objects
func (e *ExternalDocChanges) GetAllChanges() []*Change {
	if e == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, e.Changes...)
	if e.ExtensionChanges != nil {
		changes = append(changes, e.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns a count of everything that changed
func (e *ExternalDocChanges) TotalChanges() int {
	if e == nil {
		return 0
	}
	c := e.PropertyChanges.TotalChanges()
	if e.ExtensionChanges != nil {
		c += e.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes in ExternalDoc objects.
func (e *ExternalDocChanges) TotalBreakingChanges() int {
	if e == nil {
		return 0
	}
	c := e.PropertyChanges.TotalBreakingChanges()
	if e.ExtensionChanges != nil {
		c += e.ExtensionChanges.TotalBreakingChanges()
	}
	return c
}

// CompareExternalDocs will compare a left (original) and a right (new) slice of ValueReference
// nodes for any changes between them. If there are changes, then a pointer to ExternalDocChanges
// is returned, otherwise if nothing changed - then nil is returned.
func CompareExternalDocs(l, r *base.ExternalDoc) *ExternalDocChanges {
	var changes []*Change
	props := make([]*PropertyCheck, 0, 2)

	props = append(props,
		NewPropertyCheck(CompExternalDocs, PropURL,
			l.URL.ValueNode, r.URL.ValueNode,
			v3.URLLabel, &changes, l, r),
		NewPropertyCheck(CompExternalDocs, PropDescription,
			l.Description.ValueNode, r.Description.ValueNode,
			v3.DescriptionLabel, &changes, l, r),
	)

	CheckProperties(props)

	dc := new(ExternalDocChanges)
	dc.PropertyChanges = NewPropertyChanges(changes)

	// check extensions
	dc.ExtensionChanges = CheckExtensions(l, r)
	if dc.TotalChanges() <= 0 {
		return nil
	}
	return dc
}

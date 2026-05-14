// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low/base"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// LicenseChanges represent changes to a License object that is a child of Info object. Part of an OpenAPI document
type LicenseChanges struct {
	*PropertyChanges
	ExtensionChanges *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between License objects
func (l *LicenseChanges) GetAllChanges() []*Change {
	if l == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, l.Changes...)
	if l.ExtensionChanges != nil {
		changes = append(changes, l.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges represents the total number of changes made to a License instance.
func (l *LicenseChanges) TotalChanges() int {
	if l == nil {
		return 0
	}
	c := l.PropertyChanges.TotalChanges()

	if l.ExtensionChanges != nil {
		c += l.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes in License objects.
func (l *LicenseChanges) TotalBreakingChanges() int {
	if l == nil {
		return 0
	}
	c := l.PropertyChanges.TotalBreakingChanges()
	if l.ExtensionChanges != nil {
		c += l.ExtensionChanges.TotalBreakingChanges()
	}
	return c
}

// CompareLicense will check a left (original) and right (new) License object for any changes. If there
// were any, a pointer to a LicenseChanges object is returned, otherwise if nothing changed - the function
// returns nil.
func CompareLicense(l, r *base.License) *LicenseChanges {
	var changes []*Change
	props := make([]*PropertyCheck, 0, 3)

	props = append(props,
		NewPropertyCheck(CompLicense, PropURL,
			l.URL.ValueNode, r.URL.ValueNode,
			v3.URLLabel, &changes, l, r),
		NewPropertyCheck(CompLicense, PropName,
			l.Name.ValueNode, r.Name.ValueNode,
			v3.NameLabel, &changes, l, r),
		NewPropertyCheck(CompLicense, PropIdentifier,
			l.Identifier.ValueNode, r.Identifier.ValueNode,
			v3.Identifier, &changes, l, r),
	)

	CheckProperties(props)

	lc := new(LicenseChanges)
	lc.PropertyChanges = NewPropertyChanges(changes)
	lc.ExtensionChanges = CompareExtensions(l.Extensions, r.Extensions)
	if lc.TotalChanges() <= 0 {
		return nil
	}
	return lc
}

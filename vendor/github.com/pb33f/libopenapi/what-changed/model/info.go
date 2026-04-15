// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low/base"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// InfoChanges represents the number of changes to an Info object. Part of an OpenAPI document
type InfoChanges struct {
	*PropertyChanges
	ContactChanges   *ContactChanges   `json:"contact,omitempty" yaml:"contact,omitempty"`
	LicenseChanges   *LicenseChanges   `json:"license,omitempty" yaml:"license,omitempty"`
	ExtensionChanges *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Info objects
func (i *InfoChanges) GetAllChanges() []*Change {
	if i == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, i.Changes...)
	if i.ContactChanges != nil {
		changes = append(changes, i.ContactChanges.GetAllChanges()...)
	}
	if i.LicenseChanges != nil {
		changes = append(changes, i.LicenseChanges.GetAllChanges()...)
	}
	if i.ExtensionChanges != nil {
		changes = append(changes, i.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges represents the total number of changes made to an Info object.
func (i *InfoChanges) TotalChanges() int {
	if i == nil {
		return 0
	}
	t := i.PropertyChanges.TotalChanges()
	if i.ContactChanges != nil {
		t += i.ContactChanges.TotalChanges()
	}
	if i.LicenseChanges != nil {
		t += i.LicenseChanges.TotalChanges()
	}
	if i.ExtensionChanges != nil {
		t += i.ExtensionChanges.TotalChanges()
	}
	return t
}

// TotalBreakingChanges returns the total number of breaking changes in Info objects.
func (i *InfoChanges) TotalBreakingChanges() int {
	if i == nil {
		return 0
	}
	c := i.PropertyChanges.TotalBreakingChanges()
	if i.ContactChanges != nil {
		c += i.ContactChanges.TotalBreakingChanges()
	}
	if i.LicenseChanges != nil {
		c += i.LicenseChanges.TotalBreakingChanges()
	}
	if i.ExtensionChanges != nil {
		c += i.ExtensionChanges.TotalBreakingChanges()
	}
	return c
}

// CompareInfo will compare a left (original) and a right (new) Info object. Any changes
// will be returned in a pointer to InfoChanges, otherwise if nothing is found, then nil is
// returned instead.
func CompareInfo(l, r *base.Info) *InfoChanges {
	var changes []*Change
	props := make([]*PropertyCheck, 0, 5)

	props = append(props,
		NewPropertyCheck(CompInfo, PropTitle,
			l.Title.ValueNode, r.Title.ValueNode,
			v3.TitleLabel, &changes, l, r),
		NewPropertyCheck(CompInfo, PropSummary,
			l.Summary.ValueNode, r.Summary.ValueNode,
			v3.SummaryLabel, &changes, l, r),
		NewPropertyCheck(CompInfo, PropDescription,
			l.Description.ValueNode, r.Description.ValueNode,
			v3.DescriptionLabel, &changes, l, r),
		NewPropertyCheck(CompInfo, PropTermsOfService,
			l.TermsOfService.ValueNode, r.TermsOfService.ValueNode,
			v3.TermsOfServiceLabel, &changes, l, r),
		NewPropertyCheck(CompInfo, PropVersion,
			l.Version.ValueNode, r.Version.ValueNode,
			v3.VersionLabel, &changes, l, r),
	)

	// check properties
	CheckProperties(props)

	i := new(InfoChanges)

	// compare contact.
	if l.Contact.Value != nil && r.Contact.Value != nil {
		i.ContactChanges = CompareContact(l.Contact.Value, r.Contact.Value)
	} else {
		if l.Contact.Value == nil && r.Contact.Value != nil {
			CreateChange(&changes, ObjectAdded, v3.ContactLabel,
				nil, r.Contact.ValueNode, BreakingAdded(CompInfo, PropContact), nil, r.Contact.Value)
		}
		if l.Contact.Value != nil && r.Contact.Value == nil {
			CreateChange(&changes, ObjectRemoved, v3.ContactLabel,
				l.Contact.ValueNode, nil, BreakingRemoved(CompInfo, PropContact), l.Contact.Value, nil)
		}
	}

	// compare license.
	if l.License.Value != nil && r.License.Value != nil {
		i.LicenseChanges = CompareLicense(l.License.Value, r.License.Value)
	} else {
		if l.License.Value == nil && r.License.Value != nil {
			CreateChange(&changes, ObjectAdded, v3.LicenseLabel,
				nil, r.License.ValueNode, BreakingAdded(CompInfo, PropLicense), nil, r.License.Value)
		}
		if l.License.Value != nil && r.License.Value == nil {
			CreateChange(&changes, ObjectRemoved, v3.LicenseLabel,
				l.License.ValueNode, nil, BreakingRemoved(CompInfo, PropLicense), r.License.Value, nil)
		}
	}

	// check extensions.
	i.ExtensionChanges = CompareExtensions(l.Extensions, r.Extensions)

	i.PropertyChanges = NewPropertyChanges(changes)
	if i.TotalChanges() <= 0 {
		return nil
	}
	return i
}

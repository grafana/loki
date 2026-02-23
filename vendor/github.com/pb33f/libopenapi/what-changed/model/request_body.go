// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// RequestBodyChanges represents changes made between two OpenAPI RequestBody Objects
type RequestBodyChanges struct {
	*PropertyChanges
	ContentChanges   map[string]*MediaTypeChanges `json:"content,omitempty" yaml:"content,omitempty"`
	ExtensionChanges *ExtensionChanges            `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between RequestBody objects
func (rb *RequestBodyChanges) GetAllChanges() []*Change {
	if rb == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, rb.Changes...)
	for k := range rb.ContentChanges {
		changes = append(changes, rb.ContentChanges[k].GetAllChanges()...)
	}
	if rb.ExtensionChanges != nil {
		changes = append(changes, rb.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes found between two OpenAPI RequestBody objects
func (rb *RequestBodyChanges) TotalChanges() int {
	if rb == nil {
		return 0
	}
	c := rb.PropertyChanges.TotalChanges()
	for k := range rb.ContentChanges {
		c += rb.ContentChanges[k].TotalChanges()
	}
	if rb.ExtensionChanges != nil {
		c += rb.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes found between OpenAPI RequestBody objects
func (rb *RequestBodyChanges) TotalBreakingChanges() int {
	c := rb.PropertyChanges.TotalBreakingChanges()
	for k := range rb.ContentChanges {
		c += rb.ContentChanges[k].TotalBreakingChanges()
	}
	return c
}

// CompareRequestBodies compares a left and right OpenAPI RequestBody object for changes. If found returns a pointer
// to a RequestBodyChanges instance. Returns nil if nothing was found.
func CompareRequestBodies(l, r *v3.RequestBody) *RequestBodyChanges {
	if low.AreEqual(l, r) {
		return nil
	}

	var changes []*Change
	props := make([]*PropertyCheck, 0, 2)

	props = append(props,
		NewPropertyCheck(CompRequestBody, PropDescription,
			l.Description.ValueNode, r.Description.ValueNode,
			v3.DescriptionLabel, &changes, l, r),
		NewPropertyCheck(CompRequestBody, PropRequired,
			l.Required.ValueNode, r.Required.ValueNode,
			v3.RequiredLabel, &changes, l, r),
	)

	CheckProperties(props)

	rbc := new(RequestBodyChanges)
	rbc.ContentChanges = CheckMapForChanges(l.Content.Value, r.Content.Value,
		&changes, v3.ContentLabel, CompareMediaTypes)
	rbc.ExtensionChanges = CompareExtensions(l.Extensions, r.Extensions)
	rbc.PropertyChanges = NewPropertyChanges(changes)
	return rbc
}

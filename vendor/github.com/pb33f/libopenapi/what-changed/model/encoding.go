// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// EncodingChanges represent all the changes made to an Encoding object
type EncodingChanges struct {
	*PropertyChanges
	HeaderChanges map[string]*HeaderChanges `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Encoding objects
func (e *EncodingChanges) GetAllChanges() []*Change {
	if e == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, e.Changes...)
	for k := range e.HeaderChanges {
		changes = append(changes, e.HeaderChanges[k].GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes made between two Encoding objects
func (e *EncodingChanges) TotalChanges() int {
	if e == nil {
		return 0
	}
	c := e.PropertyChanges.TotalChanges()
	if e.HeaderChanges != nil {
		for i := range e.HeaderChanges {
			c += e.HeaderChanges[i].TotalChanges()
		}
	}
	return c
}

// TotalBreakingChanges returns the number of changes made between two Encoding objects that were breaking.
func (e *EncodingChanges) TotalBreakingChanges() int {
	c := e.PropertyChanges.TotalBreakingChanges()
	if e.HeaderChanges != nil {
		for i := range e.HeaderChanges {
			c += e.HeaderChanges[i].TotalBreakingChanges()
		}
	}
	return c
}

// CompareEncoding returns a pointer to *EncodingChanges that contain all changes made between a left and right
// set of Encoding objects.
func CompareEncoding(l, r *v3.Encoding) *EncodingChanges {
	var changes []*Change
	props := make([]*PropertyCheck, 0, 4)

	props = append(props,
		NewPropertyCheck(CompEncoding, PropContentType,
			l.ContentType.ValueNode, r.ContentType.ValueNode,
			v3.ContentTypeLabel, &changes, l, r),
		NewPropertyCheck(CompEncoding, PropStyle,
			l.Style.ValueNode, r.Style.ValueNode,
			v3.StyleLabel, &changes, l, r),
		NewPropertyCheck(CompEncoding, PropExplode,
			l.Explode.ValueNode, r.Explode.ValueNode,
			v3.ExplodeLabel, &changes, l, r),
		NewPropertyCheck(CompEncoding, PropAllowReserved,
			l.AllowReserved.ValueNode, r.AllowReserved.ValueNode,
			v3.AllowReservedLabel, &changes, l, r),
	)

	// check everything.
	CheckProperties(props)
	ec := new(EncodingChanges)

	// headers
	ec.HeaderChanges = CheckMapForChanges(l.Headers.Value, r.Headers.Value, &changes, v3.HeadersLabel, CompareHeadersV3)
	ec.PropertyChanges = NewPropertyChanges(changes)
	if ec.TotalChanges() <= 0 {
		return nil
	}
	return ec
}

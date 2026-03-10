// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// MediaTypeChanges represent changes made between two OpenAPI MediaType instances.
type MediaTypeChanges struct {
	*PropertyChanges
	SchemaChanges       *SchemaChanges              `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	ItemSchemaChanges   *SchemaChanges              `json:"itemSchemas,omitempty" yaml:"itemSchemas,omitempty"`
	ExtensionChanges    *ExtensionChanges           `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	ExampleChanges      map[string]*ExampleChanges  `json:"examples,omitempty" yaml:"examples,omitempty"`
	EncodingChanges     map[string]*EncodingChanges `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	ItemEncodingChanges map[string]*EncodingChanges `json:"itemEncoding,omitempty" yaml:"itemEncoding,omitempty"`
}

// GetAllChanges returns a slice of all changes made between MediaType objects
func (m *MediaTypeChanges) GetAllChanges() []*Change {
	if m == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, m.Changes...)
	if m.SchemaChanges != nil {
		changes = append(changes, m.SchemaChanges.GetAllChanges()...)
	}
	if m.ItemSchemaChanges != nil {
		changes = append(changes, m.ItemSchemaChanges.GetAllChanges()...)
	}
	for k := range m.ExampleChanges {
		changes = append(changes, m.ExampleChanges[k].GetAllChanges()...)
	}
	for k := range m.EncodingChanges {
		changes = append(changes, m.EncodingChanges[k].GetAllChanges()...)
	}
	for k := range m.ItemEncodingChanges {
		changes = append(changes, m.ItemEncodingChanges[k].GetAllChanges()...)
	}
	if m.ExtensionChanges != nil {
		changes = append(changes, m.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes between two MediaType instances.
func (m *MediaTypeChanges) TotalChanges() int {
	if m == nil {
		return 0
	}
	c := m.PropertyChanges.TotalChanges()
	for k := range m.ExampleChanges {
		c += m.ExampleChanges[k].TotalChanges()
	}
	if m.SchemaChanges != nil {
		c += m.SchemaChanges.TotalChanges()
	}
	if m.ItemSchemaChanges != nil {
		c += m.ItemSchemaChanges.TotalChanges()
	}
	if len(m.EncodingChanges) > 0 {
		for i := range m.EncodingChanges {
			c += m.EncodingChanges[i].TotalChanges()
		}
	}
	if len(m.ItemEncodingChanges) > 0 {
		for i := range m.ItemEncodingChanges {
			c += m.ItemEncodingChanges[i].TotalChanges()
		}
	}
	if m.ExtensionChanges != nil {
		c += m.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes made between two MediaType instances.
func (m *MediaTypeChanges) TotalBreakingChanges() int {
	c := m.PropertyChanges.TotalBreakingChanges()
	for k := range m.ExampleChanges {
		c += m.ExampleChanges[k].TotalBreakingChanges()
	}
	if m.SchemaChanges != nil {
		c += m.SchemaChanges.TotalBreakingChanges()
	}
	if m.ItemSchemaChanges != nil {
		c += m.ItemSchemaChanges.TotalBreakingChanges()
	}
	if len(m.EncodingChanges) > 0 {
		for i := range m.EncodingChanges {
			c += m.EncodingChanges[i].TotalBreakingChanges()
		}
	}
	if len(m.ItemEncodingChanges) > 0 {
		for i := range m.ItemEncodingChanges {
			c += m.ItemEncodingChanges[i].TotalBreakingChanges()
		}
	}
	return c
}

// CompareMediaTypes compares a left and a right MediaType object for any changes. If found, a pointer to a
// MediaTypeChanges instance is returned; otherwise nothing is returned.
func CompareMediaTypes(l, r *v3.MediaType) *MediaTypeChanges {
	var props []*PropertyCheck
	var changes []*Change

	mc := new(MediaTypeChanges)

	if low.AreEqual(l, r) {
		return nil
	}

	// Example
	CheckPropertyAdditionOrRemovalWithEncoding(l.Example.ValueNode, r.Example.ValueNode,
		v3.ExampleLabel, &changes,
		BreakingAdded(CompMediaType, PropExample) || BreakingRemoved(CompMediaType, PropExample),
		l.Example.Value, r.Example.Value)
	CheckForModificationWithEncoding(l.Example.ValueNode, r.Example.ValueNode,
		v3.ExampleLabel, &changes, BreakingModified(CompMediaType, PropExample),
		l.Example.Value, r.Example.Value)

	CheckProperties(props)

	// schema
	if !l.Schema.IsEmpty() && !r.Schema.IsEmpty() {
		mc.SchemaChanges = CompareSchemas(l.Schema.Value, r.Schema.Value)
	}
	if !l.Schema.IsEmpty() && r.Schema.IsEmpty() {
		CreateChange(&changes, ObjectRemoved, v3.SchemaLabel, l.Schema.ValueNode,
			nil, BreakingRemoved(CompMediaType, PropSchema), l.Schema.Value, nil)
	}
	if l.Schema.IsEmpty() && !r.Schema.IsEmpty() {
		CreateChange(&changes, ObjectAdded, v3.SchemaLabel, nil,
			r.Schema.ValueNode, BreakingAdded(CompMediaType, PropSchema), nil, r.Schema.Value)
	}

	// examples - use nil-aware version so added/removed examples appear in the map for tree rendering
	mc.ExampleChanges = CheckMapForChangesWithNilSupport(l.Examples.Value, r.Examples.Value,
		&changes, v3.ExamplesLabel, CompareExamples)

	// encoding
	mc.EncodingChanges = CheckMapForChanges(l.Encoding.Value, r.Encoding.Value,
		&changes, v3.EncodingLabel, CompareEncoding)

	// itemSchema
	if !l.ItemSchema.IsEmpty() && !r.ItemSchema.IsEmpty() {
		mc.ItemSchemaChanges = CompareSchemas(l.ItemSchema.Value, r.ItemSchema.Value)
	}
	if !l.ItemSchema.IsEmpty() && r.ItemSchema.IsEmpty() {
		CreateChange(&changes, ObjectRemoved, v3.ItemSchemaLabel, l.ItemSchema.ValueNode,
			nil, BreakingRemoved(CompMediaType, PropItemSchema), l.ItemSchema.Value, nil)
	}
	if l.ItemSchema.IsEmpty() && !r.ItemSchema.IsEmpty() {
		CreateChange(&changes, ObjectAdded, v3.ItemSchemaLabel, nil,
			r.ItemSchema.ValueNode, BreakingAdded(CompMediaType, PropItemSchema), nil, r.ItemSchema.Value)
	}

	// itemEncoding
	mc.ItemEncodingChanges = CheckMapForChanges(l.ItemEncoding.Value, r.ItemEncoding.Value,
		&changes, v3.ItemEncodingLabel, CompareEncoding)

	mc.ExtensionChanges = CompareExtensions(l.Extensions, r.Extensions)
	mc.PropertyChanges = NewPropertyChanges(changes)
	return mc
}

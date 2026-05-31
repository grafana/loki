// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// GetIndex will return the index.SpecIndex instance attached to the Schema object
func (s *Schema) GetIndex() *index.SpecIndex {
	return s.index
}

// GetContext will return the context.Context instance used when building the Schema object
func (s *Schema) GetContext() context.Context {
	return s.context
}

// FindProperty will return a ValueReference pointer containing a SchemaProxy pointer
// from a property key name. if found
func (s *Schema) FindProperty(name string) *low.ValueReference[*SchemaProxy] {
	return low.FindItemInOrderedMap[*SchemaProxy](name, s.Properties.Value)
}

// FindDependentSchema will return a ValueReference pointer containing a SchemaProxy pointer
// from a dependent schema key name. if found (3.1+ only)
func (s *Schema) FindDependentSchema(name string) *low.ValueReference[*SchemaProxy] {
	return low.FindItemInOrderedMap[*SchemaProxy](name, s.DependentSchemas.Value)
}

// FindPatternProperty will return a ValueReference pointer containing a SchemaProxy pointer
// from a pattern property key name. if found (3.1+ only)
func (s *Schema) FindPatternProperty(name string) *low.ValueReference[*SchemaProxy] {
	return low.FindItemInOrderedMap[*SchemaProxy](name, s.PatternProperties.Value)
}

// GetExtensions returns all extensions for Schema
func (s *Schema) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return s.Extensions
}

// GetRootNode will return the root yaml node of the Schema object
func (s *Schema) GetRootNode() *yaml.Node {
	return s.RootNode
}

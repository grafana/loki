// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// CriterionExpressionType represents a high-level Arazzo Criterion Expression Type Object.
// https://spec.openapis.org/arazzo/v1.0.1#criterion-expression-type-object
type CriterionExpressionType struct {
	Type       string                              `json:"type,omitempty" yaml:"type,omitempty"`
	Version    string                              `json:"version,omitempty" yaml:"version,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.CriterionExpressionType
}

// NewCriterionExpressionType creates a new high-level CriterionExpressionType instance from a low-level one.
func NewCriterionExpressionType(cet *low.CriterionExpressionType) *CriterionExpressionType {
	c := new(CriterionExpressionType)
	c.low = cet
	if !cet.Type.IsEmpty() {
		c.Type = cet.Type.Value
	}
	if !cet.Version.IsEmpty() {
		c.Version = cet.Version.Value
	}
	c.Extensions = high.ExtractExtensions(cet.Extensions)
	return c
}

// GoLow returns the low-level CriterionExpressionType instance used to create the high-level one.
func (c *CriterionExpressionType) GoLow() *low.CriterionExpressionType {
	return c.low
}

// GoLowUntyped returns the low-level CriterionExpressionType instance with no type.
func (c *CriterionExpressionType) GoLowUntyped() any {
	return c.low
}

// Render returns a YAML representation of the CriterionExpressionType object as a byte slice.
func (c *CriterionExpressionType) Render() ([]byte, error) {
	return yaml.Marshal(c)
}

// MarshalYAML creates a ready to render YAML representation of the CriterionExpressionType object.
func (c *CriterionExpressionType) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if c.Type != "" {
		m.Set("type", c.Type)
	}
	if c.Version != "" {
		m.Set("version", c.Version)
	}
	marshalExtensions(m, c.Extensions)
	return m, nil
}

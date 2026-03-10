// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel/high"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Criterion represents a high-level Arazzo Criterion Object.
// https://spec.openapis.org/arazzo/v1.0.1#criterion-object
type Criterion struct {
	Context        string                              `json:"context,omitempty" yaml:"context,omitempty"`
	Condition      string                              `json:"condition,omitempty" yaml:"condition,omitempty"`
	Type           string                              `json:"-" yaml:"-"`
	ExpressionType *CriterionExpressionType            `json:"-" yaml:"-"`
	Extensions     *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low            *low.Criterion
}

// GetEffectiveType returns the effective criterion type. Returns "simple" when Type is empty,
// the string value when set as a scalar, or ExpressionType.Type when the type field is an object.
func (c *Criterion) GetEffectiveType() string {
	if c.ExpressionType != nil {
		return c.ExpressionType.Type
	}
	if c.Type != "" {
		return c.Type
	}
	return "simple"
}

// NewCriterion creates a new high-level Criterion instance from a low-level one.
func NewCriterion(criterion *low.Criterion) *Criterion {
	c := new(Criterion)
	c.low = criterion
	if !criterion.Context.IsEmpty() {
		c.Context = criterion.Context.Value
	}
	if !criterion.Condition.IsEmpty() {
		c.Condition = criterion.Condition.Value
	}
	// Type is a union: scalar string or CriterionExpressionType mapping
	if !criterion.Type.IsEmpty() && criterion.Type.Value != nil {
		node := criterion.Type.Value
		switch node.Kind {
		case yaml.ScalarNode:
			c.Type = node.Value
		case yaml.MappingNode:
			cet := &low.CriterionExpressionType{}
			if err := lowmodel.BuildModel(node, cet); err == nil {
				if err = cet.Build(context.Background(), nil, node, nil); err == nil {
					c.ExpressionType = NewCriterionExpressionType(cet)
				}
			}
		}
	}
	c.Extensions = high.ExtractExtensions(criterion.Extensions)
	return c
}

// GoLow returns the low-level Criterion instance used to create the high-level one.
func (c *Criterion) GoLow() *low.Criterion {
	return c.low
}

// GoLowUntyped returns the low-level Criterion instance with no type.
func (c *Criterion) GoLowUntyped() any {
	return c.low
}

// Render returns a YAML representation of the Criterion object as a byte slice.
func (c *Criterion) Render() ([]byte, error) {
	return yaml.Marshal(c)
}

// MarshalYAML creates a ready to render YAML representation of the Criterion object.
func (c *Criterion) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if c.Context != "" {
		m.Set(low.ContextLabel, c.Context)
	}
	if c.Condition != "" {
		m.Set(low.ConditionLabel, c.Condition)
	}
	if c.ExpressionType != nil {
		m.Set(low.TypeLabel, c.ExpressionType)
	} else if c.Type != "" {
		m.Set(low.TypeLabel, c.Type)
	}
	marshalExtensions(m, c.Extensions)
	return m, nil
}

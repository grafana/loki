// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Components represents a high-level Arazzo Components Object.
// https://spec.openapis.org/arazzo/v1.0.1#components-object
type Components struct {
	Inputs         *orderedmap.Map[string, *yaml.Node]     `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Parameters     *orderedmap.Map[string, *Parameter]     `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	SuccessActions *orderedmap.Map[string, *SuccessAction] `json:"successActions,omitempty" yaml:"successActions,omitempty"`
	FailureActions *orderedmap.Map[string, *FailureAction] `json:"failureActions,omitempty" yaml:"failureActions,omitempty"`
	Extensions     *orderedmap.Map[string, *yaml.Node]     `json:"-" yaml:"-"`
	low            *low.Components
}

// NewComponents creates a new high-level Components instance from a low-level one.
func NewComponents(comp *low.Components) *Components {
	c := new(Components)
	c.low = comp

	if !comp.Inputs.IsEmpty() && comp.Inputs.Value != nil {
		c.Inputs = lowmodel.FromReferenceMap[string, *yaml.Node](comp.Inputs.Value)
	}
	if !comp.Parameters.IsEmpty() && comp.Parameters.Value != nil {
		c.Parameters = lowmodel.FromReferenceMapWithFunc(comp.Parameters.Value, func(v *low.Parameter) *Parameter {
			return NewParameter(v)
		})
	}
	if !comp.SuccessActions.IsEmpty() && comp.SuccessActions.Value != nil {
		c.SuccessActions = lowmodel.FromReferenceMapWithFunc(comp.SuccessActions.Value, func(v *low.SuccessAction) *SuccessAction {
			return NewSuccessAction(v)
		})
	}
	if !comp.FailureActions.IsEmpty() && comp.FailureActions.Value != nil {
		c.FailureActions = lowmodel.FromReferenceMapWithFunc(comp.FailureActions.Value, func(v *low.FailureAction) *FailureAction {
			return NewFailureAction(v)
		})
	}
	c.Extensions = high.ExtractExtensions(comp.Extensions)
	return c
}

// GoLow returns the low-level Components instance used to create the high-level one.
func (c *Components) GoLow() *low.Components {
	return c.low
}

// GoLowUntyped returns the low-level Components instance with no type.
func (c *Components) GoLowUntyped() any {
	return c.low
}

// Render returns a YAML representation of the Components object as a byte slice.
func (c *Components) Render() ([]byte, error) {
	return yaml.Marshal(c)
}

// MarshalYAML creates a ready to render YAML representation of the Components object.
func (c *Components) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if c.Inputs != nil && c.Inputs.Len() > 0 {
		m.Set(low.InputsLabel, c.Inputs)
	}
	if c.Parameters != nil && c.Parameters.Len() > 0 {
		m.Set(low.ParametersLabel, c.Parameters)
	}
	if c.SuccessActions != nil && c.SuccessActions.Len() > 0 {
		m.Set(low.SuccessActionsLabel, c.SuccessActions)
	}
	if c.FailureActions != nil && c.FailureActions.Len() > 0 {
		m.Set(low.FailureActionsLabel, c.FailureActions)
	}
	marshalExtensions(m, c.Extensions)
	return m, nil
}

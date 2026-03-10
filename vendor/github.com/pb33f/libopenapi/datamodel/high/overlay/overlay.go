// Copyright 2022-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package overlay

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/overlay"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Overlay represents a high-level OpenAPI Overlay document.
// https://spec.openapis.org/overlay/v1.0.0
type Overlay struct {
	Overlay    string                              `json:"overlay,omitempty" yaml:"overlay,omitempty"`
	Info       *Info                               `json:"info,omitempty" yaml:"info,omitempty"`
	Extends    string                              `json:"extends,omitempty" yaml:"extends,omitempty"`
	Actions    []*Action                           `json:"actions,omitempty" yaml:"actions,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.Overlay
}

// NewOverlay creates a new high-level Overlay instance from a low-level one.
func NewOverlay(overlay *low.Overlay) *Overlay {
	o := new(Overlay)
	o.low = overlay
	if !overlay.Overlay.IsEmpty() {
		o.Overlay = overlay.Overlay.Value
	}
	if !overlay.Info.IsEmpty() {
		o.Info = NewInfo(overlay.Info.Value)
	}
	if !overlay.Extends.IsEmpty() {
		o.Extends = overlay.Extends.Value
	}
	if !overlay.Actions.IsEmpty() {
		actions := make([]*Action, 0, len(overlay.Actions.Value))
		for _, action := range overlay.Actions.Value {
			actions = append(actions, NewAction(action.Value))
		}
		o.Actions = actions
	}
	o.Extensions = high.ExtractExtensions(overlay.Extensions)
	return o
}

// GoLow returns the low-level Overlay instance used to create the high-level one.
func (o *Overlay) GoLow() *low.Overlay {
	return o.low
}

// GoLowUntyped returns the low-level Overlay instance with no type.
func (o *Overlay) GoLowUntyped() any {
	return o.low
}

// Render returns a YAML representation of the Overlay object as a byte slice.
func (o *Overlay) Render() ([]byte, error) {
	return yaml.Marshal(o)
}

// MarshalYAML creates a ready to render YAML representation of the Overlay object.
func (o *Overlay) MarshalYAML() (interface{}, error) {
	m := orderedmap.New[string, any]()
	if o.Overlay != "" {
		m.Set("overlay", o.Overlay)
	}
	if o.Info != nil {
		m.Set("info", o.Info)
	}
	if o.Extends != "" {
		m.Set("extends", o.Extends)
	}
	if len(o.Actions) > 0 {
		m.Set("actions", o.Actions)
	}
	for pair := o.Extensions.First(); pair != nil; pair = pair.Next() {
		m.Set(pair.Key(), pair.Value())
	}
	return m, nil
}

// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// PayloadReplacement represents a high-level Arazzo Payload Replacement Object.
// https://spec.openapis.org/arazzo/v1.0.1#payload-replacement-object
type PayloadReplacement struct {
	Target     string                              `json:"target,omitempty" yaml:"target,omitempty"`
	Value      *yaml.Node                          `json:"value,omitempty" yaml:"value,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.PayloadReplacement
}

// NewPayloadReplacement creates a new high-level PayloadReplacement instance from a low-level one.
func NewPayloadReplacement(pr *low.PayloadReplacement) *PayloadReplacement {
	p := new(PayloadReplacement)
	p.low = pr
	if !pr.Target.IsEmpty() {
		p.Target = pr.Target.Value
	}
	if !pr.Value.IsEmpty() {
		p.Value = pr.Value.Value
	}
	p.Extensions = high.ExtractExtensions(pr.Extensions)
	return p
}

// GoLow returns the low-level PayloadReplacement instance used to create the high-level one.
func (p *PayloadReplacement) GoLow() *low.PayloadReplacement {
	return p.low
}

// GoLowUntyped returns the low-level PayloadReplacement instance with no type.
func (p *PayloadReplacement) GoLowUntyped() any {
	return p.low
}

// Render returns a YAML representation of the PayloadReplacement object as a byte slice.
func (p *PayloadReplacement) Render() ([]byte, error) {
	return yaml.Marshal(p)
}

// MarshalYAML creates a ready to render YAML representation of the PayloadReplacement object.
func (p *PayloadReplacement) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if p.Target != "" {
		m.Set(low.TargetLabel, p.Target)
	}
	if p.Value != nil {
		m.Set(low.ValueLabel, p.Value)
	}
	marshalExtensions(m, p.Extensions)
	return m, nil
}

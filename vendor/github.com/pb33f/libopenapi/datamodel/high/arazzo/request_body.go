// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// RequestBody represents a high-level Arazzo Request Body Object.
// https://spec.openapis.org/arazzo/v1.0.1#request-body-object
type RequestBody struct {
	ContentType  string                              `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	Payload      *yaml.Node                          `json:"payload,omitempty" yaml:"payload,omitempty"`
	Replacements []*PayloadReplacement               `json:"replacements,omitempty" yaml:"replacements,omitempty"`
	Extensions   *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low          *low.RequestBody
}

// NewRequestBody creates a new high-level RequestBody instance from a low-level one.
func NewRequestBody(rb *low.RequestBody) *RequestBody {
	r := new(RequestBody)
	r.low = rb
	if !rb.ContentType.IsEmpty() {
		r.ContentType = rb.ContentType.Value
	}
	if !rb.Payload.IsEmpty() {
		r.Payload = rb.Payload.Value
	}
	if !rb.Replacements.IsEmpty() {
		r.Replacements = buildSlice(rb.Replacements.Value, NewPayloadReplacement)
	}
	r.Extensions = high.ExtractExtensions(rb.Extensions)
	return r
}

// GoLow returns the low-level RequestBody instance used to create the high-level one.
func (r *RequestBody) GoLow() *low.RequestBody {
	return r.low
}

// GoLowUntyped returns the low-level RequestBody instance with no type.
func (r *RequestBody) GoLowUntyped() any {
	return r.low
}

// Render returns a YAML representation of the RequestBody object as a byte slice.
func (r *RequestBody) Render() ([]byte, error) {
	return yaml.Marshal(r)
}

// MarshalYAML creates a ready to render YAML representation of the RequestBody object.
func (r *RequestBody) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if r.ContentType != "" {
		m.Set(low.ContentTypeLabel, r.ContentType)
	}
	if r.Payload != nil {
		m.Set(low.PayloadLabel, r.Payload)
	}
	if len(r.Replacements) > 0 {
		m.Set(low.ReplacementsLabel, r.Replacements)
	}
	marshalExtensions(m, r.Extensions)
	return m, nil
}

// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// SourceDescription represents a high-level Arazzo Source Description Object.
// https://spec.openapis.org/arazzo/v1.0.1#source-description-object
type SourceDescription struct {
	Name       string                              `json:"name,omitempty" yaml:"name,omitempty"`
	URL        string                              `json:"url,omitempty" yaml:"url,omitempty"`
	Type       string                              `json:"type,omitempty" yaml:"type,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.SourceDescription
}

// NewSourceDescription creates a new high-level SourceDescription instance from a low-level one.
func NewSourceDescription(sd *low.SourceDescription) *SourceDescription {
	s := new(SourceDescription)
	s.low = sd
	if !sd.Name.IsEmpty() {
		s.Name = sd.Name.Value
	}
	if !sd.URL.IsEmpty() {
		s.URL = sd.URL.Value
	}
	if !sd.Type.IsEmpty() {
		s.Type = sd.Type.Value
	}
	s.Extensions = high.ExtractExtensions(sd.Extensions)
	return s
}

// GoLow returns the low-level SourceDescription instance used to create the high-level one.
func (s *SourceDescription) GoLow() *low.SourceDescription {
	return s.low
}

// GoLowUntyped returns the low-level SourceDescription instance with no type.
func (s *SourceDescription) GoLowUntyped() any {
	return s.low
}

// Render returns a YAML representation of the SourceDescription object as a byte slice.
func (s *SourceDescription) Render() ([]byte, error) {
	return yaml.Marshal(s)
}

// MarshalYAML creates a ready to render YAML representation of the SourceDescription object.
func (s *SourceDescription) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if s.Name != "" {
		m.Set(low.NameLabel, s.Name)
	}
	if s.URL != "" {
		m.Set(low.URLLabel, s.URL)
	}
	if s.Type != "" {
		m.Set(low.TypeLabel, s.Type)
	}
	marshalExtensions(m, s.Extensions)
	return m, nil
}

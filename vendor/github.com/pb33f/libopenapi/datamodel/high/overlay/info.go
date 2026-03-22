// Copyright 2022-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package overlay

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/overlay"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Info represents a high-level Overlay Info Object.
// https://spec.openapis.org/overlay/v1.1.0#info-object
type Info struct {
	Title       string                              `json:"title,omitempty" yaml:"title,omitempty"`
	Version     string                              `json:"version,omitempty" yaml:"version,omitempty"`
	Description string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Extensions  *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low         *low.Info
}

// NewInfo creates a new high-level Info instance from a low-level one.
func NewInfo(info *low.Info) *Info {
	i := new(Info)
	i.low = info
	if !info.Title.IsEmpty() {
		i.Title = info.Title.Value
	}
	if !info.Version.IsEmpty() {
		i.Version = info.Version.Value
	}
	if !info.Description.IsEmpty() {
		i.Description = info.Description.Value
	}
	i.Extensions = high.ExtractExtensions(info.Extensions)
	return i
}

// GoLow returns the low-level Info instance used to create the high-level one.
func (i *Info) GoLow() *low.Info {
	return i.low
}

// GoLowUntyped returns the low-level Info instance with no type.
func (i *Info) GoLowUntyped() any {
	return i.low
}

// Render returns a YAML representation of the Info object as a byte slice.
func (i *Info) Render() ([]byte, error) {
	return yaml.Marshal(i)
}

// MarshalYAML creates a ready to render YAML representation of the Info object.
func (i *Info) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if i.Title != "" {
		m.Set("title", i.Title)
	}
	if i.Version != "" {
		m.Set("version", i.Version)
	}
	if i.Description != "" {
		m.Set("description", i.Description)
	}
	for pair := i.Extensions.First(); pair != nil; pair = pair.Next() {
		m.Set(pair.Key(), pair.Value())
	}
	return m, nil
}

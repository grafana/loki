// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Info represents a high-level Arazzo Info Object.
// https://spec.openapis.org/arazzo/v1.0.1#info-object
type Info struct {
	Title       string                              `json:"title,omitempty" yaml:"title,omitempty"`
	Summary     string                              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Version     string                              `json:"version,omitempty" yaml:"version,omitempty"`
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
	if !info.Summary.IsEmpty() {
		i.Summary = info.Summary.Value
	}
	if !info.Description.IsEmpty() {
		i.Description = info.Description.Value
	}
	if !info.Version.IsEmpty() {
		i.Version = info.Version.Value
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
		m.Set(low.TitleLabel, i.Title)
	}
	if i.Summary != "" {
		m.Set(low.SummaryLabel, i.Summary)
	}
	if i.Description != "" {
		m.Set(low.DescriptionLabel, i.Description)
	}
	if i.Version != "" {
		m.Set(low.VersionLabel, i.Version)
	}
	marshalExtensions(m, i.Extensions)
	return m, nil
}

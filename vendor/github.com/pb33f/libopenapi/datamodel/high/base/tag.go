// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Tag represents a high-level Tag instance that is backed by a low-level one.
//
// Adds metadata to a single tag that is used by the Operation Object. It is not mandatory to have a Tag Object per
// tag defined in the Operation Object instances.
//   - v2: https://swagger.io/specification/v2/#tagObject
//   - v3: https://swagger.io/specification/#tag-object
//   - v3.2: https://spec.openapis.org/oas/v3.2.0#tag-object
type Tag struct {
	Name         string       `json:"name,omitempty" yaml:"name,omitempty"`
	Summary      string       `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description  string       `json:"description,omitempty" yaml:"description,omitempty"`
	ExternalDocs *ExternalDoc `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Parent       string       `json:"parent,omitempty" yaml:"parent,omitempty"`
	Kind         string       `json:"kind,omitempty" yaml:"kind,omitempty"`
	Extensions   *orderedmap.Map[string, *yaml.Node]
	low          *low.Tag
}

// NewTag creates a new high-level Tag instance that is backed by a low-level one.
func NewTag(tag *low.Tag) *Tag {
	t := new(Tag)
	t.low = tag
	if !tag.Name.IsEmpty() {
		t.Name = tag.Name.Value
	}
	if !tag.Summary.IsEmpty() {
		t.Summary = tag.Summary.Value
	}
	if !tag.Description.IsEmpty() {
		t.Description = tag.Description.Value
	}
	if !tag.ExternalDocs.IsEmpty() {
		t.ExternalDocs = NewExternalDoc(tag.ExternalDocs.Value)
	}
	if !tag.Parent.IsEmpty() {
		t.Parent = tag.Parent.Value
	}
	if !tag.Kind.IsEmpty() {
		t.Kind = tag.Kind.Value
	}
	t.Extensions = high.ExtractExtensions(tag.Extensions)
	return t
}

// GoLow returns the low-level Tag instance used to create the high-level one.
func (t *Tag) GoLow() *low.Tag {
	return t.low
}

// GoLowUntyped will return the low-level Tag instance that was used to create the high-level one, with no type
func (t *Tag) GoLowUntyped() any {
	return t.low
}

// Render will return a YAML representation of the Info object as a byte slice.
func (t *Tag) Render() ([]byte, error) {
	return yaml.Marshal(t)
}

// Render will return a YAML representation of the Info object as a byte slice.
func (t *Tag) RenderInline() ([]byte, error) {
	d, _ := t.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the Info object.
func (t *Tag) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(t, t.low)
	return nb.Render(), nil
}

func (t *Tag) MarshalYAMLInline() (interface{}, error) {
	nb := high.NewNodeBuilder(t, t.low)
	nb.Resolve = true
	return nb.Render(), nil
}

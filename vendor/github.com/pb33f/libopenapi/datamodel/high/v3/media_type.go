// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// MediaType represents a high-level OpenAPI MediaType object that is backed by a low-level one.
//
// Each Media Type Object provides schema and examples for the media type identified by its key.
//   - https://spec.openapis.org/oas/v3.1.0#media-type-object
type MediaType struct {
	Schema       *base.SchemaProxy                      `json:"schema,omitempty" yaml:"schema,omitempty"`
	ItemSchema   *base.SchemaProxy                      `json:"itemSchema,omitempty" yaml:"itemSchema,omitempty"`
	Example      *yaml.Node                             `json:"example,omitempty" yaml:"example,omitempty"`
	Examples     *orderedmap.Map[string, *base.Example] `json:"examples,omitempty" yaml:"examples,omitempty"`
	Encoding     *orderedmap.Map[string, *Encoding]     `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	ItemEncoding *orderedmap.Map[string, *Encoding]     `json:"itemEncoding,omitempty" yaml:"itemEncoding,omitempty"`
	Extensions   *orderedmap.Map[string, *yaml.Node]    `json:"-" yaml:"-"`
	low          *low.MediaType
}

// NewMediaType will create a new high-level MediaType instance from a low-level one.
func NewMediaType(mediaType *low.MediaType) *MediaType {
	m := new(MediaType)
	m.low = mediaType
	if !mediaType.Schema.IsEmpty() {
		m.Schema = base.NewSchemaProxy(&mediaType.Schema)
	}
	if !mediaType.ItemSchema.IsEmpty() {
		m.ItemSchema = base.NewSchemaProxy(&mediaType.ItemSchema)
	}
	m.Example = mediaType.Example.Value
	m.Examples = base.ExtractExamples(mediaType.Examples.Value)
	m.Extensions = high.ExtractExtensions(mediaType.Extensions)
	m.Encoding = ExtractEncoding(mediaType.Encoding.Value)
	if !mediaType.ItemEncoding.IsEmpty() {
		m.ItemEncoding = ExtractEncoding(mediaType.ItemEncoding.Value)
	}
	return m
}

// GoLow will return the low-level instance of MediaType used to create the high-level one.
func (m *MediaType) GoLow() *low.MediaType {
	return m.low
}

// GoLowUntyped will return the low-level MediaType instance that was used to create the high-level one, with no type
func (m *MediaType) GoLowUntyped() any {
	return m.low
}

// Render will return a YAML representation of the MediaType object as a byte slice.
func (m *MediaType) Render() ([]byte, error) {
	return yaml.Marshal(m)
}

func (m *MediaType) RenderInline() ([]byte, error) {
	d, _ := m.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the MediaType object.
func (m *MediaType) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(m, m.low)
	return nb.Render(), nil
}

func (m *MediaType) MarshalYAMLInline() (interface{}, error) {
	nb := high.NewNodeBuilder(m, m.low)
	nb.Resolve = true
	return nb.Render(), nil
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the MediaType object,
// resolving any references inline where possible. Uses the provided context for cycle detection.
// The ctx parameter should be *base.InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (m *MediaType) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	nb := high.NewNodeBuilder(m, m.low)
	nb.Resolve = true
	nb.RenderContext = ctx
	return nb.Render(), nil
}

// ExtractContent takes in a complex and hard to navigate low-level content map, and converts it in to a much simpler
// and easier to navigate high-level one.
func ExtractContent(elements *orderedmap.Map[lowmodel.KeyReference[string], lowmodel.ValueReference[*low.MediaType]]) *orderedmap.Map[string, *MediaType] {
	extracted := orderedmap.New[string, *MediaType]()
	translateFunc := func(pair orderedmap.Pair[lowmodel.KeyReference[string], lowmodel.ValueReference[*low.MediaType]]) (asyncResult[*MediaType], error) {
		return asyncResult[*MediaType]{
			key:    pair.Key().Value,
			result: NewMediaType(pair.Value().Value),
		}, nil
	}
	resultFunc := func(value asyncResult[*MediaType]) error {
		extracted.Set(value.key, value.result)
		return nil
	}
	_ = datamodel.TranslateMapParallel(elements, translateFunc, resultFunc)
	return extracted
}

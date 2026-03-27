// Copyright 2022-2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel/high"
	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	lowv3 "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// buildLowHeader builds a low-level Header from a resolved YAML node.
func buildLowHeader(node *yaml.Node, idx *index.SpecIndex) (*lowv3.Header, error) {
	var header lowv3.Header
	lowmodel.BuildModel(node, &header)
	if err := header.Build(context.Background(), nil, node, idx); err != nil {
		return nil, err
	}
	return &header, nil
}

// Header represents a high-level OpenAPI 3+ Header object backed by a low-level one.
//   - https://spec.openapis.org/oas/v3.1.0#header-object
type Header struct {
	Reference       string                                     `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Description     string                                     `json:"description,omitempty" yaml:"description,omitempty"`
	Required        bool                                       `json:"required,omitempty" yaml:"required,omitempty"`
	Deprecated      bool                                       `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	AllowEmptyValue bool                                       `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Style           string                                     `json:"style,omitempty" yaml:"style,omitempty"`
	Explode         bool                                       `json:"explode,omitempty" yaml:"explode,omitempty"`
	AllowReserved   bool                                       `json:"allowReserved,omitempty" yaml:"allowReserved,omitempty"`
	Schema          *highbase.SchemaProxy                      `json:"schema,omitempty" yaml:"schema,omitempty"`
	Example         *yaml.Node                                 `json:"example,omitempty" yaml:"example,omitempty"`
	Examples        *orderedmap.Map[string, *highbase.Example] `json:"examples,omitempty" yaml:"examples,omitempty"`
	Content         *orderedmap.Map[string, *MediaType]        `json:"content,omitempty" yaml:"content,omitempty"`
	Extensions      *orderedmap.Map[string, *yaml.Node]        `json:"-" yaml:"-"`
	low             *lowv3.Header
}

// NewHeader creates a new high-level Header instance from a low-level one.
func NewHeader(header *lowv3.Header) *Header {
	h := new(Header)
	h.low = header
	h.Description = header.Description.Value
	h.Required = header.Required.Value
	h.Deprecated = header.Deprecated.Value
	h.AllowEmptyValue = header.AllowEmptyValue.Value
	h.Style = header.Style.Value
	h.Explode = header.Explode.Value
	h.AllowReserved = header.AllowReserved.Value
	if !header.Schema.IsEmpty() {
		h.Schema = highbase.NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			Value:     header.Schema.Value,
			KeyNode:   header.Schema.KeyNode,
			ValueNode: header.Schema.ValueNode,
		})
	}
	h.Content = ExtractContent(header.Content.Value)
	h.Example = header.Example.Value
	h.Examples = highbase.ExtractExamples(header.Examples.Value)
	h.Extensions = high.ExtractExtensions(header.Extensions)
	return h
}

// GoLow returns the low-level Header instance used to create the high-level one.
func (h *Header) GoLow() *lowv3.Header {
	return h.low
}

// GoLowUntyped will return the low-level Header instance that was used to create the high-level one, with no type
func (h *Header) GoLowUntyped() any {
	return h.low
}

// IsReference returns true if this Header is a reference to another Header definition.
func (h *Header) IsReference() bool {
	return h.Reference != ""
}

// GetReference returns the reference string if this is a reference Header.
func (h *Header) GetReference() string {
	return h.Reference
}

// ExtractHeaders will extract a hard to navigate low-level Header map, into simple high-level one.
func ExtractHeaders(elements *orderedmap.Map[lowmodel.KeyReference[string], lowmodel.ValueReference[*lowv3.Header]]) *orderedmap.Map[string, *Header] {
	return low.FromReferenceMapWithFunc(elements, NewHeader)
}

// Render will return a YAML representation of the Header object as a byte slice.
func (h *Header) Render() ([]byte, error) {
	return yaml.Marshal(h)
}

// RenderInline will return a YAML representation of the Header object as a byte slice with references resolved.
func (h *Header) RenderInline() ([]byte, error) {
	d, _ := h.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the Header object.
func (h *Header) MarshalYAML() (interface{}, error) {
	// Handle reference-only header
	if h.Reference != "" {
		return utils.CreateRefNode(h.Reference), nil
	}
	nb := high.NewNodeBuilder(h, h.low)
	return nb.Render(), nil
}

// MarshalYAMLInline will create a ready to render YAML representation of the Header object with references resolved.
func (h *Header) MarshalYAMLInline() (interface{}, error) {
	// reference-only objects render as $ref nodes
	if h.Reference != "" {
		return utils.CreateRefNode(h.Reference), nil
	}

	// resolve external reference if present
	if h.low != nil {
		rendered, err := high.RenderExternalRef(h.low, buildLowHeader, NewHeader)
		if err != nil || rendered != nil {
			return rendered, err
		}
	}

	return high.RenderInline(h, h.low)
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the Header object,
// resolving any references inline where possible. Uses the provided context for cycle detection.
// The ctx parameter should be *base.InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (h *Header) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	if h.Reference != "" {
		return utils.CreateRefNode(h.Reference), nil
	}

	// resolve external reference if present
	if h.low != nil {
		rendered, err := high.RenderExternalRefWithContext(h.low, buildLowHeader, NewHeader, ctx)
		if err != nil || rendered != nil {
			return rendered, err
		}
	}

	return high.RenderInlineWithContext(h, h.low, ctx)
}

// CreateHeaderRef creates a Header that renders as a $ref to another header definition.
// This is useful when building OpenAPI specs programmatically, and you want to reference
// a header defined in components/headers rather than inlining the full definition.
//
// Example:
//
//	header := v3.CreateHeaderRef("#/components/headers/X-Rate-Limit")
//
// Renders as:
//
//	$ref: '#/components/headers/X-Rate-Limit'
func CreateHeaderRef(ref string) *Header {
	return &Header{Reference: ref}
}

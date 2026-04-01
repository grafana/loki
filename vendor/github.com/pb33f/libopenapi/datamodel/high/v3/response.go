// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	lowv3 "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// buildLowResponse builds a low-level Response from a resolved YAML node.
func buildLowResponse(node *yaml.Node, idx *index.SpecIndex) (*lowv3.Response, error) {
	var resp lowv3.Response
	lowmodel.BuildModel(node, &resp)
	if err := resp.Build(context.Background(), nil, node, idx); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Response represents a high-level OpenAPI 3+ Response object that is backed by a low-level one.
//
// Describes a single response from an API Operation, including design-time, static links to
// operations based on the response.
//   - https://spec.openapis.org/oas/v3.1.0#response-object
type Response struct {
	Reference   string                              `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Summary     string                              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string                              `json:"description" yaml:"description"`
	Headers     *orderedmap.Map[string, *Header]    `json:"headers,omitempty" yaml:"headers,omitempty"`
	Content     *orderedmap.Map[string, *MediaType] `json:"content,omitempty" yaml:"content,omitempty"`
	Links       *orderedmap.Map[string, *Link]      `json:"links,omitempty" yaml:"links,omitempty"`
	Extensions  *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low         *lowv3.Response
}

// NewResponse creates a new high-level Response object that is backed by a low-level one.
func NewResponse(response *lowv3.Response) *Response {
	r := new(Response)
	r.low = response
	r.Summary = response.Summary.Value
	r.Description = response.Description.Value
	if !response.Headers.IsEmpty() {
		r.Headers = ExtractHeaders(response.Headers.Value)
	}
	r.Extensions = high.ExtractExtensions(response.Extensions)
	if !response.Content.IsEmpty() {
		r.Content = ExtractContent(response.Content.Value)
	}
	if !response.Links.IsEmpty() {
		r.Links = low.FromReferenceMapWithFunc(response.Links.Value, NewLink)
	}
	return r
}

// GoLow returns the low-level Response object that was used to create the high-level one.
func (r *Response) GoLow() *lowv3.Response {
	return r.low
}

// GoLowUntyped will return the low-level Response instance that was used to create the high-level one, with no type
func (r *Response) GoLowUntyped() any {
	return r.low
}

// IsReference returns true if this Response is a reference to another Response definition.
func (r *Response) IsReference() bool {
	return r.Reference != ""
}

// GetReference returns the reference string if this is a reference Response.
func (r *Response) GetReference() string {
	return r.Reference
}

// Render will return a YAML representation of the Response object as a byte slice.
func (r *Response) Render() ([]byte, error) {
	return yaml.Marshal(r)
}

func (r *Response) RenderInline() ([]byte, error) {
	d, _ := r.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the Response object.
func (r *Response) MarshalYAML() (interface{}, error) {
	// Handle reference-only response
	if r.Reference != "" {
		return utils.CreateRefNode(r.Reference), nil
	}
	nb := high.NewNodeBuilder(r, r.low)
	return nb.Render(), nil
}

// MarshalYAMLInline will create a ready to render YAML representation of the Response object,
// resolving any references inline where possible.
func (r *Response) MarshalYAMLInline() (interface{}, error) {
	// reference-only objects render as $ref nodes
	if r.Reference != "" {
		return utils.CreateRefNode(r.Reference), nil
	}

	// resolve external reference if present
	if r.low != nil {
		rendered, err := high.RenderExternalRef(r.low, buildLowResponse, NewResponse)
		if err != nil || rendered != nil {
			return rendered, err
		}
	}

	return high.RenderInline(r, r.low)
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the Response object,
// resolving any references inline where possible. Uses the provided context for cycle detection.
// The ctx parameter should be *base.InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (r *Response) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	if r.Reference != "" {
		return utils.CreateRefNode(r.Reference), nil
	}

	// resolve external reference if present
	if r.low != nil {
		rendered, err := high.RenderExternalRefWithContext(r.low, buildLowResponse, NewResponse, ctx)
		if err != nil || rendered != nil {
			return rendered, err
		}
	}

	return high.RenderInlineWithContext(r, r.low, ctx)
}

// CreateResponseRef creates a Response that renders as a $ref to another response definition.
// This is useful when building OpenAPI specs programmatically and you want to reference
// a response defined in components/responses rather than inlining the full definition.
//
// Example:
//
//	resp := v3.CreateResponseRef("#/components/responses/NotFound")
//
// Renders as:
//
//	$ref: '#/components/responses/NotFound'
func CreateResponseRef(ref string) *Response {
	return &Response{Reference: ref}
}

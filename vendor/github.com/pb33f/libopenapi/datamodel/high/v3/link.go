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

// buildLowLink builds a low-level Link from a resolved YAML node.
func buildLowLink(node *yaml.Node, idx *index.SpecIndex) (*lowv3.Link, error) {
	var link lowv3.Link
	lowmodel.BuildModel(node, &link)
	link.Build(context.Background(), nil, node, idx)
	return &link, nil
}

// Link represents a high-level OpenAPI 3+ Link object that is backed by a low-level one.
//
// The Link object represents a possible design-time link for a response. The presence of a link does not guarantee the
// caller's ability to successfully invoke it, rather it provides a known relationship and traversal mechanism between
// responses and other operations.
//
// Unlike dynamic links (i.e. links provided in the response payload), the OAS linking mechanism does not require
// link information in the runtime response.
//
// For computing links, and providing instructions to execute them, a runtime expression is used for accessing values
// in an operation and using them as parameters while invoking the linked operation.
//   - https://spec.openapis.org/oas/v3.1.0#link-object
type Link struct {
	Reference    string                              `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	OperationRef string                              `json:"operationRef,omitempty" yaml:"operationRef,omitempty"`
	OperationId  string                              `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Parameters   *orderedmap.Map[string, string]     `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody  string                              `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Description  string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Server       *Server                             `json:"server,omitempty" yaml:"server,omitempty"`
	Extensions   *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low          *lowv3.Link
}

// NewLink will create a new high-level Link instance from a low-level one.
func NewLink(link *lowv3.Link) *Link {
	l := new(Link)
	l.low = link
	l.OperationRef = link.OperationRef.Value
	l.OperationId = link.OperationId.Value
	l.Parameters = low.FromReferenceMap(link.Parameters.Value)
	l.RequestBody = link.RequestBody.Value
	l.Description = link.Description.Value
	if link.Server.Value != nil {
		l.Server = NewServer(link.Server.Value)
	}
	l.Extensions = high.ExtractExtensions(link.Extensions)
	return l
}

// GoLow will return the low-level Link instance used to create the high-level one.
func (l *Link) GoLow() *lowv3.Link {
	return l.low
}

// GoLowUntyped will return the low-level Link instance that was used to create the high-level one, with no type
func (l *Link) GoLowUntyped() any {
	return l.low
}

// IsReference returns true if this Link is a reference to another Link definition.
func (l *Link) IsReference() bool {
	return l.Reference != ""
}

// GetReference returns the reference string if this is a reference Link.
func (l *Link) GetReference() string {
	return l.Reference
}

// Render will return a YAML representation of the Link object as a byte slice.
func (l *Link) Render() ([]byte, error) {
	return yaml.Marshal(l)
}

// MarshalYAML will create a ready to render YAML representation of the Link object.
func (l *Link) MarshalYAML() (interface{}, error) {
	// Handle reference-only link
	if l.Reference != "" {
		return utils.CreateRefNode(l.Reference), nil
	}
	nb := high.NewNodeBuilder(l, l.low)
	return nb.Render(), nil
}

// MarshalYAMLInline will create a ready to render YAML representation of the Link object,
// with all references resolved inline.
func (l *Link) MarshalYAMLInline() (interface{}, error) {
	// reference-only objects render as $ref nodes
	if l.Reference != "" {
		return utils.CreateRefNode(l.Reference), nil
	}

	// resolve external reference if present
	if l.low != nil {
		// buildLowLink never returns an error, so we can ignore it
		rendered, _ := high.RenderExternalRef(l.low, buildLowLink, NewLink)
		if rendered != nil {
			return rendered, nil
		}
	}

	return high.RenderInline(l, l.low)
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the Link object,
// resolving any references inline where possible. Uses the provided context for cycle detection.
// The ctx parameter should be *base.InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (l *Link) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	if l.Reference != "" {
		return utils.CreateRefNode(l.Reference), nil
	}

	// resolve external reference if present
	if l.low != nil {
		// buildLowLink never returns an error, so we can ignore it
		rendered, _ := high.RenderExternalRefWithContext(l.low, buildLowLink, NewLink, ctx)
		if rendered != nil {
			return rendered, nil
		}
	}

	return high.RenderInlineWithContext(l, l.low, ctx)
}

// CreateLinkRef creates a Link that renders as a $ref to another link definition.
// This is useful when building OpenAPI specs programmatically, and you want to reference
// a link defined in components/links rather than inlining the full definition.
//
// Example:
//
//	link := v3.CreateLinkRef("#/components/links/GetUserByUserId")
//
// Renders as:
//
//	$ref: '#/components/links/GetUserByUserId'
func CreateLinkRef(ref string) *Link {
	return &Link{Reference: ref}
}

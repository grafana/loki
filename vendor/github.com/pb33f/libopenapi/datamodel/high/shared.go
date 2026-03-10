// Copyright 2022 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package high contains a set of high-level models that represent OpenAPI 2 and 3 documents.
// These high-level models (porcelain) are used by applications directly, rather than the low-level models
// plumbing) that are used to compose high level models.
//
// High level models are simple to navigate, strongly typed, precise representations of the OpenAPI schema
// that are created from an OpenAPI specification.
//
// All high level objects contains a 'GoLow' method. This 'GoLow' method will return the low-level model that
// was used to create it, which provides an engineer as much low level detail about the raw spec used to create
// those models, things like key/value breakdown of each value, lines, column, source comments etc.
package high

import (
	"context"
	"fmt"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// GoesLow is used to represent any high-level model. All high level models meet this interface and can be used to
// extract low-level models from any high-level model.
type GoesLow[T any] interface {
	// GoLow returns the low-level object that was used to create the high-level object. This allows consumers
	// to dive-down into the plumbing API at any point in the model.
	GoLow() T
}

// GoesLowUntyped is used to represent any high-level model. All high level models meet this interface and can be used to
// extract low-level models from any high-level model.
type GoesLowUntyped interface {
	// GoLowUntyped returns the low-level object that was used to create the high-level object. This allows consumers
	// to dive-down into the plumbing API at any point in the model.
	GoLowUntyped() any
}

// ExtractExtensions is a convenience method for converting low-level extension definitions, to a high level *orderedmap.Map[string, *yaml.Node]
// definition that is easier to consume in applications.
func ExtractExtensions(extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]) *orderedmap.Map[string, *yaml.Node] {
	return low.FromReferenceMap(extensions)
}

// RenderInline creates an inline YAML representation of a high-level object with all references resolved.
// This is a shared helper used by MarshalYAMLInline implementations across high-level types.
func RenderInline(high, low any) (interface{}, error) {
	nb := NewNodeBuilder(high, low)
	nb.Resolve = true
	return nb.Render(), nil
}

// RenderInlineWithContext creates an inline YAML representation of a high-level object with all references resolved.
// Uses the provided context for cycle detection during inline rendering.
// The ctx parameter should be *base.InlineRenderContext but is typed as any to avoid import cycles.
func RenderInlineWithContext(high, low, ctx any) (interface{}, error) {
	nb := NewNodeBuilder(high, low)
	nb.Resolve = true
	nb.RenderContext = ctx
	return nb.Render(), nil
}

// UnpackExtensions is a convenience function that makes it easy and simple to unpack an objects extensions
// into a complex type, provided as a generic. This function is for high-level models that implement `GoesLow()`
// and for low-level models that support extensions via `HasExtensions`.
//
// This feature will be upgraded at some point to hold a registry of types and extension mappings to make this
// functionality available a little more automatically.
// You can read more about the discussion here: https://github.com/pb33f/libopenapi/issues/8
//
// `T` represents the Type you want to unpack into
// `R` represents the LOW type of the object that contains the extensions (not the high)
// `low` represents the HIGH type of the object that contains the extensions.
//
// to use:
//
//	schema := schemaProxy.Schema() // any high-level object that has
//	extensions, err := UnpackExtensions[MyComplexType, low.Schema](schema)
func UnpackExtensions[T any, R low.HasExtensions[T]](low GoesLow[R]) (*orderedmap.Map[string, *T], error) {
	m := orderedmap.New[string, *T]()
	ext := low.GoLow().GetExtensions()
	for ext, value := range ext.FromOldest() {
		g := new(T)
		valueNode := value.ValueNode
		err := valueNode.Decode(g)
		if err != nil {
			return nil, err
		}
		m.Set(ext.Value, g)
	}
	return m, nil
}

// ExternalRefResolver is an interface for low-level objects that can be external references.
// This is used by ResolveExternalRef to resolve external $ref values during inline rendering.
type ExternalRefResolver interface {
	IsReference() bool
	GetReference() string
	GetIndex() *index.SpecIndex
}

// ExternalRefBuildFunc is a function that builds a low-level object from a resolved YAML node.
// It should create a new instance of the low-level type, call BuildModel and Build on it,
// and return the constructed object along with any error encountered.
type ExternalRefBuildFunc[L any] func(node *yaml.Node, idx *index.SpecIndex) (L, error)

// ExternalRefResult contains the result of resolving an external reference.
type ExternalRefResult[H any, L any] struct {
	High     H
	Low      L
	Resolved bool
}

// ResolveExternalRef attempts to resolve an external reference from a low-level object.
// If the low-level object is an external reference (IsReference() returns true), this function
// will use the index to find and resolve the referenced component, build new low and high level
// objects from the resolved content, and return them.
//
// Parameters:
//   - lowObj: the low-level object that may be an external reference
//   - buildLow: function to build a new low-level object from the resolved YAML node
//   - buildHigh: function to create a high-level object from the resolved low-level object
//
// Returns:
//   - ExternalRefResult containing the resolved high and low objects if resolution succeeded
//   - error if resolution failed (malformed YAML, build errors, etc.)
//
// If the object is not a reference or cannot be resolved, Resolved will be false and the
// caller should fall back to rendering the original object.
func ResolveExternalRef[H any, L any](
	lowObj ExternalRefResolver,
	buildLow ExternalRefBuildFunc[L],
	buildHigh func(L) H,
) (ExternalRefResult[H, L], error) {
	var result ExternalRefResult[H, L]

	// not a reference, nothing to resolve
	if lowObj == nil || !lowObj.IsReference() {
		return result, nil
	}

	idx := lowObj.GetIndex()
	if idx == nil {
		return result, nil
	}

	ref := lowObj.GetReference()
	resolved := idx.FindComponent(context.Background(), ref)
	if resolved == nil || resolved.Node == nil {
		return result, nil
	}

	// build the low-level object from the resolved node
	lowResolved, err := buildLow(resolved.Node, resolved.Index)
	if err != nil {
		return result, fmt.Errorf("failed to build resolved external reference '%s': %w", ref, err)
	}

	// build the high-level object from the resolved low-level object
	highResolved := buildHigh(lowResolved)

	result.High = highResolved
	result.Low = lowResolved
	result.Resolved = true
	return result, nil
}

// RenderExternalRef is a convenience function that resolves an external reference and renders it inline.
// This combines ResolveExternalRef with RenderInline for the common case where you want to
// resolve and immediately render an external reference.
//
// If the low-level object is not a reference or resolution fails gracefully (not found),
// this returns (nil, nil) and the caller should fall back to normal rendering.
// If resolution succeeds, returns the rendered YAML node.
// If an error occurs during resolution or rendering, returns the error.
func RenderExternalRef[H any, L any](
	lowObj ExternalRefResolver,
	buildLow ExternalRefBuildFunc[L],
	buildHigh func(L) H,
) (interface{}, error) {
	result, err := ResolveExternalRef(lowObj, buildLow, buildHigh)
	if err != nil || !result.Resolved {
		return nil, err
	}
	return RenderInline(result.High, result.Low)
}

// RenderExternalRefWithContext is like RenderExternalRef but passes a context for cycle detection.
func RenderExternalRefWithContext[H any, L any](
	lowObj ExternalRefResolver,
	buildLow ExternalRefBuildFunc[L],
	buildHigh func(L) H,
	ctx any,
) (interface{}, error) {
	result, err := ResolveExternalRef(lowObj, buildLow, buildHigh)
	if err != nil || !result.Resolved {
		return nil, err
	}
	return RenderInlineWithContext(result.High, result.Low, ctx)
}

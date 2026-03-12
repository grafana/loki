// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"encoding/json"

	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowBase "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// buildLowExample builds a low-level Example from a resolved YAML node.
func buildLowExample(node *yaml.Node, idx *index.SpecIndex) (*lowBase.Example, error) {
	var ex lowBase.Example
	low.BuildModel(node, &ex)
	ex.Build(context.Background(), nil, node, idx)
	return &ex, nil
}

// Example represents a high-level Example object as defined by OpenAPI 3+
//
//	v3 - https://spec.openapis.org/oas/v3.1.0#example-object
type Example struct {
	Reference       string                              `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Summary         string                              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description     string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Value           *yaml.Node                          `json:"value,omitempty" yaml:"value,omitempty"`
	ExternalValue   string                              `json:"externalValue,omitempty" yaml:"externalValue,omitempty"`
	DataValue       *yaml.Node                          `json:"dataValue,omitempty" yaml:"dataValue,omitempty"`             // OpenAPI 3.2+ dataValue field
	SerializedValue string                              `json:"serializedValue,omitempty" yaml:"serializedValue,omitempty"` // OpenAPI 3.2+ serializedValue field
	Extensions      *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low             *lowBase.Example
}

// NewExample will create a new instance of an Example, using a low-level Example.
func NewExample(example *lowBase.Example) *Example {
	e := new(Example)
	e.low = example
	e.Summary = example.Summary.Value
	e.Description = example.Description.Value
	e.Value = example.Value.Value
	e.ExternalValue = example.ExternalValue.Value
	e.DataValue = example.DataValue.Value
	e.SerializedValue = example.SerializedValue.Value
	e.Extensions = high.ExtractExtensions(example.Extensions)
	return e
}

// GoLow will return the low-level Example used to build the high level one.
func (e *Example) GoLow() *lowBase.Example {
	if e == nil {
		return nil
	}
	return e.low
}

// GoLowUntyped will return the low-level Example instance that was used to create the high-level one, with no type
func (e *Example) GoLowUntyped() any {
	if e == nil {
		return nil
	}
	return e.low
}

// IsReference returns true if this Example is a reference to another Example definition.
func (e *Example) IsReference() bool {
	return e.Reference != ""
}

// GetReference returns the reference string if this is a reference Example.
func (e *Example) GetReference() string {
	return e.Reference
}

// Render will return a YAML representation of the Example object as a byte slice.
func (e *Example) Render() ([]byte, error) {
	return yaml.Marshal(e)
}

// MarshalYAML will create a ready to render YAML representation of the Example object.
func (e *Example) MarshalYAML() (interface{}, error) {
	// Handle reference-only example
	if e.Reference != "" {
		return utils.CreateRefNode(e.Reference), nil
	}
	nb := high.NewNodeBuilder(e, e.low)
	return nb.Render(), nil
}

// MarshalYAMLInline will create a ready to render YAML representation of the Example object,
// with all references resolved inline.
func (e *Example) MarshalYAMLInline() (interface{}, error) {
	// reference-only objects render as $ref nodes
	if e.Reference != "" {
		return utils.CreateRefNode(e.Reference), nil
	}

	// resolve external reference if present
	if e.low != nil {
		// buildLowExample never returns an error, so we can ignore it
		rendered, err := high.RenderExternalRef(e.low, buildLowExample, NewExample)
		if rendered != nil || err != nil {
			return rendered, err
		}
	}

	return high.RenderInline(e, e.low)
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the Example object,
// resolving any references inline where possible. Uses the provided context for cycle detection.
// The ctx parameter should be *InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (e *Example) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	if e.Reference != "" {
		return utils.CreateRefNode(e.Reference), nil
	}

	// resolve external reference if present
	if e.low != nil {
		// buildLowExample never returns an error, so we can ignore it
		rendered, _ := high.RenderExternalRefWithContext(e.low, buildLowExample, NewExample, ctx)
		if rendered != nil {
			return rendered, nil
		}
	}

	return high.RenderInlineWithContext(e, e.low, ctx)
}

// CreateExampleRef creates an Example that renders as a $ref to another example definition.
// This is useful when building OpenAPI specs programmatically and you want to reference
// an example defined in components/examples rather than inlining the full definition.
//
// Example:
//
//	ex := base.CreateExampleRef("#/components/examples/UserExample")
//
// Renders as:
//
//	$ref: '#/components/examples/UserExample'
func CreateExampleRef(ref string) *Example {
	return &Example{Reference: ref}
}

// MarshalJSON will marshal this into a JSON byte slice
func (e *Example) MarshalJSON() ([]byte, error) {
	var g map[string]any
	nb := high.NewNodeBuilder(e, e.low)
	r := nb.Render()
	r.Decode(&g)
	return json.Marshal(g)
}

// ExtractExamples will convert a low-level example map, into a high level one that is simple to navigate.
// no fidelity is lost, everything is still available via GoLow()
func ExtractExamples(elements *orderedmap.Map[low.KeyReference[string], low.ValueReference[*lowBase.Example]]) *orderedmap.Map[string, *Example] {
	return low.FromReferenceMapWithFunc(elements, NewExample)
}

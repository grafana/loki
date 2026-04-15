// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// buildLowParameter builds a low-level Parameter from a resolved YAML node.
func buildLowParameter(node *yaml.Node, idx *index.SpecIndex) (*low.Parameter, error) {
	var param low.Parameter
	lowmodel.BuildModel(node, &param)
	if err := param.Build(context.Background(), nil, node, idx); err != nil {
		return nil, err
	}
	return &param, nil
}

// Parameter represents a high-level OpenAPI 3+ Parameter object, that is backed by a low-level one.
//
// A unique parameter is defined by a combination of a name and location.
//   - https://spec.openapis.org/oas/v3.1.0#parameter-object
type Parameter struct {
	Reference       string                                 `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Name            string                                 `json:"name,omitempty" yaml:"name,omitempty"`
	In              string                                 `json:"in,omitempty" yaml:"in,omitempty"`
	Description     string                                 `json:"description,omitempty" yaml:"description,omitempty"`
	Required        *bool                                  `json:"required,renderZero,omitempty" yaml:"required,renderZero,omitempty"`
	Deprecated      bool                                   `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	AllowEmptyValue bool                                   `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Style           string                                 `json:"style,omitempty" yaml:"style,omitempty"`
	Explode         *bool                                  `json:"explode,renderZero,omitempty" yaml:"explode,renderZero,omitempty"`
	AllowReserved   bool                                   `json:"allowReserved,omitempty" yaml:"allowReserved,omitempty"`
	Schema          *base.SchemaProxy                      `json:"schema,omitempty" yaml:"schema,omitempty"`
	Example         *yaml.Node                             `json:"example,omitempty" yaml:"example,omitempty"`
	Examples        *orderedmap.Map[string, *base.Example] `json:"examples,omitempty" yaml:"examples,omitempty"`
	Content         *orderedmap.Map[string, *MediaType]    `json:"content,omitempty" yaml:"content,omitempty"`
	Extensions      *orderedmap.Map[string, *yaml.Node]    `json:"-" yaml:"-"`
	low             *low.Parameter
}

// NewParameter will create a new high-level instance of a Parameter, using a low-level one.
func NewParameter(param *low.Parameter) *Parameter {
	p := new(Parameter)
	p.low = param
	p.Name = param.Name.Value
	p.In = param.In.Value
	p.Description = param.Description.Value
	p.Deprecated = param.Deprecated.Value
	p.AllowEmptyValue = param.AllowEmptyValue.Value
	p.Style = param.Style.Value
	if !param.Explode.IsEmpty() {
		p.Explode = &param.Explode.Value
	}
	p.AllowReserved = param.AllowReserved.Value
	if !param.Schema.IsEmpty() {
		p.Schema = base.NewSchemaProxy(&param.Schema)
	}
	if !param.Required.IsEmpty() {
		p.Required = &param.Required.Value
	}
	p.Example = param.Example.Value
	p.Examples = base.ExtractExamples(param.Examples.Value)
	p.Content = ExtractContent(param.Content.Value)
	p.Extensions = high.ExtractExtensions(param.Extensions)
	return p
}

// CreateParameterRef creates a Parameter that renders as a $ref to another parameter definition.
// This is useful when building OpenAPI specs programmatically and you want to reference
// a parameter defined in components/parameters rather than inlining the full definition.
//
// Example:
//
//	param := v3.CreateParameterRef("#/components/parameters/limitParam")
//
// Renders as:
//
//	$ref: '#/components/parameters/limitParam'
func CreateParameterRef(ref string) *Parameter {
	return &Parameter{Reference: ref}
}

// GoLow returns the low-level Parameter used to create the high-level one.
func (p *Parameter) GoLow() *low.Parameter {
	return p.low
}

// GoLowUntyped will return the low-level Discriminator instance that was used to create the high-level one, with no type
func (p *Parameter) GoLowUntyped() any {
	return p.low
}

// IsReference returns true if this Parameter is a reference to another Parameter definition.
func (p *Parameter) IsReference() bool {
	return p.Reference != ""
}

// GetReference returns the reference string if this is a reference Parameter.
func (p *Parameter) GetReference() string {
	return p.Reference
}

// Render will return a YAML representation of the Encoding object as a byte slice.
func (p *Parameter) Render() ([]byte, error) {
	return yaml.Marshal(p)
}

func (p *Parameter) RenderInline() ([]byte, error) {
	d, _ := p.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the Parameter object.
func (p *Parameter) MarshalYAML() (interface{}, error) {
	// Handle reference-only parameter
	if p.Reference != "" {
		return utils.CreateRefNode(p.Reference), nil
	}
	nb := high.NewNodeBuilder(p, p.low)
	return nb.Render(), nil
}

// MarshalYAMLInline will create a ready to render YAML representation of the Parameter object,
// resolving any references inline where possible.
func (p *Parameter) MarshalYAMLInline() (interface{}, error) {
	// reference-only objects render as $ref nodes
	if p.Reference != "" {
		return utils.CreateRefNode(p.Reference), nil
	}

	// resolve external reference if present
	if p.low != nil {
		rendered, err := high.RenderExternalRef(p.low, buildLowParameter, NewParameter)
		if err != nil || rendered != nil {
			return rendered, err
		}
	}

	return high.RenderInline(p, p.low)
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the Parameter object,
// resolving any references inline where possible. Uses the provided context for cycle detection.
// The ctx parameter should be *base.InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (p *Parameter) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	if p.Reference != "" {
		return utils.CreateRefNode(p.Reference), nil
	}

	// resolve external reference if present
	if p.low != nil {
		rendered, err := high.RenderExternalRefWithContext(p.low, buildLowParameter, NewParameter, ctx)
		if err != nil || rendered != nil {
			return rendered, err
		}
	}

	return high.RenderInlineWithContext(p, p.low, ctx)
}

// IsExploded will return true if the parameter is exploded, false otherwise.
func (p *Parameter) IsExploded() bool {
	if p.Explode == nil {
		return false
	}
	return *p.Explode
}

// IsDefaultFormEncoding will return true if the parameter has no exploded value, or has exploded set to true, and no style
// or a style set to form. This combination is the default encoding/serialization style for parameters for OpenAPI 3+
func (p *Parameter) IsDefaultFormEncoding() bool {
	if p.Explode == nil && (p.Style == "" || p.Style == "form") {
		return true
	}
	if p.Explode != nil && *p.Explode && (p.Style == "" || p.Style == "form") {
		return true
	}
	return false
}

// IsDefaultHeaderEncoding will return true if the parameter has no exploded value, or has exploded set to false, and no style
// or a style set to simple. This combination is the default encoding/serialization style for header parameters for OpenAPI 3+
func (p *Parameter) IsDefaultHeaderEncoding() bool {
	if p.Explode == nil && (p.Style == "" || p.Style == "simple") {
		return true
	}
	if p.Explode != nil && !*p.Explode && (p.Style == "" || p.Style == "simple") {
		return true
	}
	return false
}

// IsDefaultPathEncoding will return true if the parameter has no exploded value, or has exploded set to false, and no style
// or a style set to simple. This combination is the default encoding/serialization style for path parameters for OpenAPI 3+
func (p *Parameter) IsDefaultPathEncoding() bool {
	return p.IsDefaultHeaderEncoding() // header default encoding and path default encoding are the same
}

// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"errors"

	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowBase "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Discriminator is only used by OpenAPI 3+ documents, it represents a polymorphic discriminator used for schemas
//
// When request bodies or response payloads may be one of a number of different schemas, a discriminator object can be
// used to aid in serialization, deserialization, and validation. The discriminator is a specific object in a schema
// which is used to inform the consumer of the document of an alternative schema based on the value associated with it.
//
// When using the discriminator, inline schemas will not be considered.
//
//	v3 - https://spec.openapis.org/oas/v3.1.0#discriminator-object
type Discriminator struct {
	PropertyName   string                          `json:"propertyName,omitempty" yaml:"propertyName,omitempty"`
	Mapping        *orderedmap.Map[string, string] `json:"mapping,omitempty" yaml:"mapping,omitempty"`
	DefaultMapping string                          `json:"defaultMapping,omitempty" yaml:"defaultMapping,omitempty"` // OpenAPI 3.2+ defaultMapping for fallback schema
	low            *lowBase.Discriminator
}

// NewDiscriminator will create a new high-level Discriminator from a low-level one.
func NewDiscriminator(disc *lowBase.Discriminator) *Discriminator {
	d := new(Discriminator)
	d.low = disc
	d.PropertyName = disc.PropertyName.Value
	d.Mapping = low.FromReferenceMap(disc.Mapping.Value)
	d.DefaultMapping = disc.DefaultMapping.Value
	return d
}

// GoLow returns the low-level Discriminator used to build the high-level one.
func (d *Discriminator) GoLow() *lowBase.Discriminator {
	return d.low
}

// GoLowUntyped will return the low-level Discriminator instance that was used to create the high-level one, with no type
func (d *Discriminator) GoLowUntyped() any {
	return d.low
}

// Render will return a YAML representation of the Discriminator object as a byte slice.
func (d *Discriminator) Render() ([]byte, error) {
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the Discriminator object.
func (d *Discriminator) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(d, d.low)
	return nb.Render(), nil
}

// MarshalYAMLInlineWithContext renders discriminator mappings with replacements
// prepared for this render operation, without mutating their indexed YAML nodes.
func (d *Discriminator) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	nb := high.NewNodeBuilder(d, d.low)
	rendered := nb.Render()
	renderCtx, ok := ctx.(*InlineRenderContext)
	if !ok || renderCtx == nil || d.low == nil || d.Mapping == nil {
		return rendered, errors.Join(nb.Errors...)
	}

	mappingNode := discriminatorMappingNode(rendered)
	if mappingNode != nil {
		for pair := d.Mapping.First(); pair != nil; pair = pair.Next() {
			lowValue := d.low.FindMappingValue(pair.Key())
			if lowValue == nil {
				continue
			}
			replacement, exists := renderCtx.MappingRewrite(lowValue.ValueNode)
			if !exists {
				continue
			}
			for i := 0; i+1 < len(mappingNode.Content); i += 2 {
				if mappingNode.Content[i].Value == pair.Key() {
					mappingNode.Content[i+1].Value = replacement
					break
				}
			}
		}
	}
	return rendered, errors.Join(nb.Errors...)
}

func discriminatorMappingNode(node *yaml.Node) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "mapping" && node.Content[i+1].Kind == yaml.MappingNode {
			return node.Content[i+1]
		}
	}
	return nil
}

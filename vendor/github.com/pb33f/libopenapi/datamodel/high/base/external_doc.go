// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// ExternalDoc represents a high-level External Documentation object as defined by OpenAPI 2 and 3
//
// Allows referencing an external resource for extended documentation.
//
//	v2 - https://swagger.io/specification/v2/#externalDocumentationObject
//	v3 - https://spec.openapis.org/oas/v3.1.0#external-documentation-object
type ExternalDoc struct {
	Description string                              `json:"description,omitempty" yaml:"description,omitempty"`
	URL         string                              `json:"url,omitempty" yaml:"url,omitempty"`
	Extensions  *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low         *low.ExternalDoc
}

// NewExternalDoc will create a new high-level External Documentation object from a low-level one.
func NewExternalDoc(extDoc *low.ExternalDoc) *ExternalDoc {
	d := new(ExternalDoc)
	d.low = extDoc
	if !extDoc.Description.IsEmpty() {
		d.Description = extDoc.Description.Value
	}
	if !extDoc.URL.IsEmpty() {
		d.URL = extDoc.URL.Value
	}
	d.Extensions = high.ExtractExtensions(extDoc.Extensions)
	return d
}

// GoLow returns the low-level ExternalDoc instance used to create the high-level one.
func (e *ExternalDoc) GoLow() *low.ExternalDoc {
	return e.low
}

// GoLowUntyped will return the low-level ExternalDoc instance that was used to create the high-level one, with no type
func (e *ExternalDoc) GoLowUntyped() any {
	return e.low
}

func (e *ExternalDoc) GetExtensions() *orderedmap.Map[string, *yaml.Node] {
	return e.Extensions
}

// Render will return a YAML representation of the ExternalDoc object as a byte slice.
func (e *ExternalDoc) Render() ([]byte, error) {
	return yaml.Marshal(e)
}

// MarshalYAML will create a ready to render YAML representation of the ExternalDoc object.
func (e *ExternalDoc) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(e, e.low)
	return nb.Render(), nil
}

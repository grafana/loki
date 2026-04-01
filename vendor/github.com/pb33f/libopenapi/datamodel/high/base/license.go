// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// License is a high-level representation of a License object as defined by OpenAPI 2 and OpenAPI 3
//
//	v2 - https://swagger.io/specification/v2/#licenseObject
//	v3 - https://spec.openapis.org/oas/v3.1.0#license-object
type License struct {
	Name       string                              `json:"name,omitempty" yaml:"name,omitempty"`
	URL        string                              `json:"url,omitempty" yaml:"url,omitempty"`
	Identifier string                              `json:"identifier,omitempty" yaml:"identifier,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.License
}

// NewLicense will create a new high-level License instance from a low-level one.
func NewLicense(license *low.License) *License {
	l := new(License)
	l.low = license
	l.Extensions = high.ExtractExtensions(license.Extensions)
	if !license.URL.IsEmpty() {
		l.URL = license.URL.Value
	}
	if !license.Name.IsEmpty() {
		l.Name = license.Name.Value
	}
	if !license.Identifier.IsEmpty() {
		l.Identifier = license.Identifier.Value
	}
	return l
}

// GoLow will return the low-level License used to create the high-level one.
func (l *License) GoLow() *low.License {
	return l.low
}

// GoLowUntyped will return the low-level License instance that was used to create the high-level one, with no type
func (l *License) GoLowUntyped() any {
	return l.low
}

// Render will return a YAML representation of the License object as a byte slice.
func (l *License) Render() ([]byte, error) {
	return yaml.Marshal(l)
}

// MarshalYAML will create a ready to render YAML representation of the License object.
func (l *License) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(l, l.low)
	return nb.Render(), nil
}

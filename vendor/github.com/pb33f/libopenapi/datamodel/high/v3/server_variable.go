// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// ServerVariable represents a high-level OpenAPI 3+ ServerVariable object, that is backed by a low-level one.
//
// ServerVariable is an object representing a Server Variable for server URL template substitution.
// - https://spec.openapis.org/oas/v3.1.0#server-variable-object
type ServerVariable struct {
	Enum        []string                            `json:"enum,omitempty" yaml:"enum,omitempty"`
	Default     string                              `json:"default,omitempty" yaml:"default,omitempty"`
	Description string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Extensions  *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low         *low.ServerVariable
}

// NewServerVariable will return a new high-level instance of a ServerVariable from a low-level one.
func NewServerVariable(variable *low.ServerVariable) *ServerVariable {
	v := new(ServerVariable)
	v.low = variable
	var enums []string
	for _, enum := range variable.Enum {
		if enum.Value != "" {
			enums = append(enums, enum.Value)
		}
	}
	v.Default = variable.Default.Value
	v.Description = variable.Description.Value
	v.Enum = enums
	v.Extensions = high.ExtractExtensions(variable.Extensions)
	return v
}

// GoLow returns the low-level ServerVariable used to create the high\-level one.
func (s *ServerVariable) GoLow() *low.ServerVariable {
	return s.low
}

// GoLowUntyped will return the low-level ServerVariable instance that was used to create the high-level one, with no type
func (s *ServerVariable) GoLowUntyped() any {
	return s.low
}

// Render will return a YAML representation of the ServerVariable object as a byte slice.
func (s *ServerVariable) Render() ([]byte, error) {
	return yaml.Marshal(s)
}

// MarshalYAML will create a ready to render YAML representation of the ServerVariable object.
func (s *ServerVariable) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(s, s.low)
	return nb.Render(), nil
}

// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowv3 "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Server represents a high-level OpenAPI 3+ Server object, that is backed by a low level one.
//   - https://spec.openapis.org/oas/v3.1.0#server-object
type Server struct {
	Name        string                                   `json:"name,omitempty" yaml:"name,omitempty"` // OpenAPI 3.2+ name field for documentation
	URL         string                                   `json:"url,omitempty" yaml:"url,omitempty"`
	Description string                                   `json:"description,omitempty" yaml:"description,omitempty"`
	Variables   *orderedmap.Map[string, *ServerVariable] `json:"variables,omitempty" yaml:"variables,omitempty"`
	Extensions  *orderedmap.Map[string, *yaml.Node]      `json:"-" yaml:"-"`
	low         *lowv3.Server
}

// NewServer will create a new high-level Server instance from a low-level one.
func NewServer(server *lowv3.Server) *Server {
	s := new(Server)
	s.low = server
	s.Name = server.Name.Value
	s.Description = server.Description.Value
	s.URL = server.URL.Value
	s.Variables = low.FromReferenceMapWithFunc(server.Variables.Value, NewServerVariable)
	s.Extensions = high.ExtractExtensions(server.Extensions)
	return s
}

// GoLow returns the low-level Server instance that was used to create the high-level one
func (s *Server) GoLow() *lowv3.Server {
	return s.low
}

// GoLowUntyped will return the low-level Server instance that was used to create the high-level one, with no type
func (s *Server) GoLowUntyped() any {
	return s.low
}

// Render will return a YAML representation of the Server object as a byte slice.
func (s *Server) Render() ([]byte, error) {
	return yaml.Marshal(s)
}

// MarshalYAML will create a ready to render YAML representation of the Server object.
func (s *Server) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(s, s.low)
	return nb.Render(), nil
}

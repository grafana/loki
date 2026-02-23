// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	lowv2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
)

// Scopes is a high-level representation of a Swagger / OpenAPI 2 OAuth2 Scopes object, that is backed by a low-level one.
//
// Scopes lists the available scopes for an OAuth2 security scheme.
//   - https://swagger.io/specification/v2/#scopesObject
type Scopes struct {
	Values *orderedmap.Map[string, string]
	low    *lowv2.Scopes
}

// NewScopes creates a new high-level instance of Scopes from a low-level one.
func NewScopes(scopes *lowv2.Scopes) *Scopes {
	s := new(Scopes)
	s.low = scopes
	s.Values = low.FromReferenceMap(scopes.Values)
	return s
}

// GoLow returns the low-level instance of Scopes used to create the high-level one.
func (s *Scopes) GoLow() *lowv2.Scopes {
	return s.low
}

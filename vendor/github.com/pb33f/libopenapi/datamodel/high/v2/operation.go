// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Operation represents a high-level Swagger / OpenAPI 2 Operation object, backed by a low-level one.
// It describes a single API operation on a path.
//   - https://swagger.io/specification/v2/#operationObject
type Operation struct {
	Tags         []string
	Summary      string
	Description  string
	ExternalDocs *base.ExternalDoc
	OperationId  string
	Consumes     []string
	Produces     []string
	Parameters   []*Parameter
	Responses    *Responses
	Schemes      []string
	Deprecated   bool
	Security     []*base.SecurityRequirement
	Extensions   *orderedmap.Map[string, *yaml.Node]
	low          *low.Operation
}

// NewOperation creates a new high-level Operation instance from a low-level one.
func NewOperation(operation *low.Operation) *Operation {
	o := new(Operation)
	o.low = operation
	o.Extensions = high.ExtractExtensions(operation.Extensions)
	if !operation.Tags.IsEmpty() {
		var tags []string
		for t := range operation.Tags.Value {
			tags = append(tags, operation.Tags.Value[t].Value)
		}
		o.Tags = tags
	}
	if !operation.Summary.IsEmpty() {
		o.Summary = operation.Summary.Value
	}
	if !operation.Description.IsEmpty() {
		o.Description = operation.Description.Value
	}
	if !operation.ExternalDocs.IsEmpty() {
		o.ExternalDocs = base.NewExternalDoc(operation.ExternalDocs.Value)
	}
	if !operation.OperationId.IsEmpty() {
		o.OperationId = operation.OperationId.Value
	}
	if !operation.Consumes.IsEmpty() {
		var cons []string
		for c := range operation.Consumes.Value {
			cons = append(cons, operation.Consumes.Value[c].Value)
		}
		o.Consumes = cons
	}
	if !operation.Produces.IsEmpty() {
		var prods []string
		for p := range operation.Produces.Value {
			prods = append(prods, operation.Produces.Value[p].Value)
		}
		o.Produces = prods
	}
	if !operation.Parameters.IsEmpty() {
		var params []*Parameter
		for p := range operation.Parameters.Value {
			params = append(params, NewParameter(operation.Parameters.Value[p].Value))
		}
		o.Parameters = params
	}
	if !operation.Responses.IsEmpty() {
		o.Responses = NewResponses(operation.Responses.Value)
	}
	if !operation.Schemes.IsEmpty() {
		var schemes []string
		for s := range operation.Schemes.Value {
			schemes = append(schemes, operation.Schemes.Value[s].Value)
		}
		o.Schemes = schemes
	}
	if !operation.Deprecated.IsEmpty() {
		o.Deprecated = operation.Deprecated.Value
	}
	if !operation.Security.IsEmpty() {
		var sec []*base.SecurityRequirement
		for s := range operation.Security.Value {
			sec = append(sec, base.NewSecurityRequirement(operation.Security.Value[s].Value))
		}
		o.Security = sec
	}
	return o
}

// GoLow returns the low-level operation used to create the high-level one.
func (o *Operation) GoLow() *low.Operation {
	return o.low
}

// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowv2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Response is a representation of a high-level Swagger / OpenAPI 2 Response object, backed by a low-level one.
// Response describes a single response from an API Operation
//   - https://swagger.io/specification/v2/#responseObject
type Response struct {
	Description string
	Schema      *base.SchemaProxy
	Headers     *orderedmap.Map[string, *Header]
	Examples    *Example
	Extensions  *orderedmap.Map[string, *yaml.Node]
	low         *lowv2.Response
}

// NewResponse creates a new high-level instance of Response from a low level one.
func NewResponse(response *lowv2.Response) *Response {
	r := new(Response)
	r.low = response
	r.Extensions = high.ExtractExtensions(response.Extensions)
	if !response.Description.IsEmpty() {
		r.Description = response.Description.Value
	}
	if !response.Schema.IsEmpty() {
		r.Schema = base.NewSchemaProxy(&response.Schema)
	}
	if !response.Headers.IsEmpty() {
		r.Headers = low.FromReferenceMapWithFunc(response.Headers.Value, NewHeader)
	}
	if !response.Examples.IsEmpty() {
		r.Examples = NewExample(response.Examples.Value)
	}
	return r
}

// GoLow will return the low-level Response instance used to create the high level one.
func (r *Response) GoLow() *lowv2.Response {
	return r.low
}

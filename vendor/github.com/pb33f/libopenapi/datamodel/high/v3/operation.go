// Copyright 2022-2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowv3 "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Operation is a high-level representation of an OpenAPI 3+ Operation object, backed by a low-level one.
//
// An Operation is perhaps the most important object of the entire specification. Everything of value
// happens here. The entire being for existence of this library and the specification, is this Operation.
//   - https://spec.openapis.org/oas/v3.1.0#operation-object
type Operation struct {
	Tags         []string                            `json:"tags,omitempty" yaml:"tags,omitempty"`
	Summary      string                              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description  string                              `json:"description,omitempty" yaml:"description,omitempty"`
	ExternalDocs *base.ExternalDoc                   `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	OperationId  string                              `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Parameters   []*Parameter                        `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody  *RequestBody                        `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses    *Responses                          `json:"responses,omitempty" yaml:"responses,omitempty"`
	Callbacks    *orderedmap.Map[string, *Callback]  `json:"callbacks,omitempty" yaml:"callbacks,omitempty"`
	Deprecated   *bool                               `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Security     []*base.SecurityRequirement         `json:"security,omitempty" yaml:"security,omitempty"`
	Servers      []*Server                           `json:"servers,omitempty" yaml:"servers,omitempty"`
	Extensions   *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low          *lowv3.Operation
}

// NewOperation will create a new Operation instance from a low-level one.
func NewOperation(operation *lowv3.Operation) *Operation {
	o := new(Operation)
	o.low = operation
	var tags []string
	if !operation.Tags.IsEmpty() {
		for i := range operation.Tags.Value {
			tags = append(tags, operation.Tags.Value[i].Value)
		}
	}
	o.Tags = tags
	o.Summary = operation.Summary.Value
	if !operation.Deprecated.IsEmpty() {
		o.Deprecated = &operation.Deprecated.Value
	}
	o.Description = operation.Description.Value
	if !operation.ExternalDocs.IsEmpty() {
		o.ExternalDocs = base.NewExternalDoc(operation.ExternalDocs.Value)
	}
	o.OperationId = operation.OperationId.Value
	if !operation.Parameters.IsEmpty() {
		params := make([]*Parameter, len(operation.Parameters.Value))
		for i := range operation.Parameters.Value {
			params[i] = NewParameter(operation.Parameters.Value[i].Value)
		}
		o.Parameters = params
	}
	if !operation.RequestBody.IsEmpty() {
		o.RequestBody = NewRequestBody(operation.RequestBody.Value)
	}
	if !operation.Responses.IsEmpty() {
		o.Responses = NewResponses(operation.Responses.Value)
	}
	if !operation.Security.IsEmpty() {
		var sec []*base.SecurityRequirement
		for s := range operation.Security.Value {
			sec = append(sec, base.NewSecurityRequirement(operation.Security.Value[s].Value))
		}
		if len(sec) > 0 {
			o.Security = sec
		} else {
			o.Security = []*base.SecurityRequirement{} // security is defined, but empty.
		}
	}
	var servers []*Server
	for i := range operation.Servers.Value {
		servers = append(servers, NewServer(operation.Servers.Value[i].Value))
	}
	o.Servers = servers
	o.Extensions = high.ExtractExtensions(operation.Extensions)
	if !operation.Callbacks.IsEmpty() {
		o.Callbacks = low.FromReferenceMapWithFunc(operation.Callbacks.Value, NewCallback)
	}
	return o
}

// GoLow will return the low-level Operation instance that was used to create the high-level one.
func (o *Operation) GoLow() *lowv3.Operation {
	return o.low
}

// GoLowUntyped will return the low-level Discriminator instance that was used to create the high-level one, with no type
func (o *Operation) GoLowUntyped() any {
	return o.low
}

// Render will return a YAML representation of the Operation object as a byte slice.
func (o *Operation) Render() ([]byte, error) {
	return yaml.Marshal(o)
}

func (o *Operation) RenderInline() ([]byte, error) {
	d, _ := o.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the Operation object.
func (o *Operation) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(o, o.low)
	return nb.Render(), nil
}

func (o *Operation) MarshalYAMLInline() (interface{}, error) {
	nb := high.NewNodeBuilder(o, o.low)
	nb.Resolve = true
	return nb.Render(), nil
}

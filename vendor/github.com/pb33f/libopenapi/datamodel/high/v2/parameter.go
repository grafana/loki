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

// Parameter represents a high-level Swagger / OpenAPI 2 Parameter object, backed by a low-level one.
//
// A unique parameter is defined by a combination of a name and location.
//
// There are five possible parameter types.
//
// Path
//
//	Used together with Path Templating, where the parameter value is actually part of the operation's URL.
//	This does not include the host or base path of the API. For example, in /items/{itemId}, the path parameter is itemId.
//
// Query
//
//	Parameters that are appended to the URL. For example, in /items?id=###, the query parameter is id.
//
// Header
//
//	Custom headers that are expected as part of the request.
//
// Body
//
//	The payload that's appended to the HTTP request. Since there can only be one payload, there can only be one body parameter.
//	The name of the body parameter has no effect on the parameter itself and is used for documentation purposes only.
//	Since Form parameters are also in the payload, body and form parameters cannot exist together for the same operation.
//
// Form
//
//	Used to describe the payload of an HTTP request when either application/x-www-form-urlencoded, multipart/form-data
//	or both are used as the content type of the request (in Swagger's definition, the consumes property of an operation).
//	This is the only parameter type that can be used to send files, thus supporting the file type. Since form parameters
//	are sent in the payload, they cannot be declared together with a body parameter for the same operation. Form
//	parameters have a different format based on the content-type used (for further details,
//	consult http://www.w3.org/TR/html401/interact/forms.html#h-17.13.4):
//	  application/x-www-form-urlencoded - Similar to the format of Query parameters but as a payload. For example,
//	  foo=1&bar=swagger - both foo and bar are form parameters. This is normally used for simple parameters that are
//	                      being transferred.
//	  multipart/form-data - each parameter takes a section in the payload with an internal header. For example, for
//	                        the header Content-Disposition: form-data; name="submit-name" the name of the parameter is
//	                        submit-name. This type of form parameters is more commonly used for file transfers
//
// https://swagger.io/specification/v2/#parameterObject
type Parameter struct {
	Name             string
	In               string
	Type             string
	Format           string
	Description      string
	Required         *bool
	AllowEmptyValue  *bool
	Schema           *base.SchemaProxy
	Items            *Items
	CollectionFormat string
	Default          *yaml.Node
	Maximum          *int
	ExclusiveMaximum *bool
	Minimum          *int
	ExclusiveMinimum *bool
	MaxLength        *int
	MinLength        *int
	Pattern          string
	MaxItems         *int
	MinItems         *int
	UniqueItems      *bool
	Enum             []*yaml.Node
	MultipleOf       *int
	Extensions       *orderedmap.Map[string, *yaml.Node]
	low              *low.Parameter
}

// NewParameter creates a new high-level instance of a Parameter from a low-level one.
func NewParameter(parameter *low.Parameter) *Parameter {
	p := new(Parameter)
	p.low = parameter
	p.Extensions = high.ExtractExtensions(parameter.Extensions)
	if !parameter.Name.IsEmpty() {
		p.Name = parameter.Name.Value
	}
	if !parameter.In.IsEmpty() {
		p.In = parameter.In.Value
	}
	if !parameter.Type.IsEmpty() {
		p.Type = parameter.Type.Value
	}
	if !parameter.Format.IsEmpty() {
		p.Format = parameter.Format.Value
	}
	if !parameter.Description.IsEmpty() {
		p.Description = parameter.Description.Value
	}
	if !parameter.Required.IsEmpty() {
		p.Required = &parameter.Required.Value
	}
	if !parameter.AllowEmptyValue.IsEmpty() {
		p.AllowEmptyValue = &parameter.AllowEmptyValue.Value
	}
	if !parameter.Schema.IsEmpty() {
		p.Schema = base.NewSchemaProxy(&parameter.Schema)
	}
	if !parameter.Items.IsEmpty() {
		p.Items = NewItems(parameter.Items.Value)
	}
	if !parameter.CollectionFormat.IsEmpty() {
		p.CollectionFormat = parameter.CollectionFormat.Value
	}
	if !parameter.Default.IsEmpty() {
		p.Default = parameter.Default.Value
	}
	if !parameter.Maximum.IsEmpty() {
		p.Maximum = &parameter.Maximum.Value
	}
	if !parameter.ExclusiveMaximum.IsEmpty() {
		p.ExclusiveMaximum = &parameter.ExclusiveMaximum.Value
	}
	if !parameter.Minimum.IsEmpty() {
		p.Minimum = &parameter.Minimum.Value
	}
	if !parameter.ExclusiveMinimum.IsEmpty() {
		p.ExclusiveMinimum = &parameter.ExclusiveMinimum.Value
	}
	if !parameter.MaxLength.IsEmpty() {
		p.MaxLength = &parameter.MaxLength.Value
	}
	if !parameter.MinLength.IsEmpty() {
		p.MinLength = &parameter.MinLength.Value
	}
	if !parameter.Pattern.IsEmpty() {
		p.Pattern = parameter.Pattern.Value
	}
	if !parameter.MinItems.IsEmpty() {
		p.MinItems = &parameter.MinItems.Value
	}
	if !parameter.MaxItems.IsEmpty() {
		p.MaxItems = &parameter.MaxItems.Value
	}
	if !parameter.UniqueItems.IsEmpty() {
		p.UniqueItems = &parameter.UniqueItems.Value
	}
	if !parameter.Enum.IsEmpty() {
		var enums []*yaml.Node
		for e := range parameter.Enum.Value {
			enums = append(enums, parameter.Enum.Value[e].Value)
		}
		p.Enum = enums
	}
	if !parameter.MultipleOf.IsEmpty() {
		p.MultipleOf = &parameter.MultipleOf.Value
	}
	return p
}

// GoLow returns the low-level Parameter used to create the high-level one.
func (p *Parameter) GoLow() *low.Parameter {
	return p.low
}

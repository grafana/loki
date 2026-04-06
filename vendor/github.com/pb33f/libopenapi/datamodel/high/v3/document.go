// Copyright 2022-2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package v3 represents all OpenAPI 3+ high-level models. High-level models are easy to navigate
// and simple to extract what ever is required from an OpenAPI 3+ specification.
//
// High-level models are backed by low-level ones. There is a 'GoLow()' method available on every high level
// object. 'Going Low' allows engineers to transition from a high-level or 'porcelain' API, to a low-level 'plumbing'
// API, which provides fine grain detail to the underlying AST powering the data, lines, columns, raw nodes etc.
package v3

import (
	"bytes"

	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowv3 "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/json"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Document represents a high-level OpenAPI 3 document (both 3.0 & 3.1). A Document is the root of the specification.
type Document struct {
	// Version is the version of OpenAPI being used, extracted from the 'openapi: x.x.x' definition.
	// This is not a standard property of the OpenAPI model, it's a convenience mechanism only.
	Version string `json:"openapi,omitempty" yaml:"openapi,omitempty"`

	// Info represents a specification Info definitions
	// Provides metadata about the API. The metadata MAY be used by tooling as required.
	// - https://spec.openapis.org/oas/v3.1.0#info-object
	Info *base.Info `json:"info,omitempty" yaml:"info,omitempty"`

	// Servers is a slice of Server instances which provide connectivity information to a target server. If the servers
	// property is not provided, or is an empty array, the default value would be a Server Object with an url value of /.
	// - https://spec.openapis.org/oas/v3.1.0#server-object
	Servers []*Server `json:"servers,omitempty" yaml:"servers,omitempty"`

	// Paths contains all the PathItem definitions for the specification.
	// The available paths and operations for the API, The most important part of ths spec.
	// - https://spec.openapis.org/oas/v3.1.0#paths-object
	Paths *Paths `json:"paths,omitempty" yaml:"paths,omitempty"`

	// Components is an element to hold various schemas for the document.
	// - https://spec.openapis.org/oas/v3.1.0#components-object
	Components *Components `json:"components,omitempty" yaml:"components,omitempty"`

	// Security contains global security requirements/roles for the specification
	// A declaration of which security mechanisms can be used across the API. The list of values includes alternative
	// security requirement objects that can be used. Only one of the security requirement objects need to be satisfied
	// to authorize a request. Individual operations can override this definition. To make security optional,
	// an empty security requirement ({}) can be included in the array.
	// - https://spec.openapis.org/oas/v3.1.0#security-requirement-object
	Security []*base.SecurityRequirement `json:"security,omitempty" yaml:"security,omitempty"`
	// Security []*base.SecurityRequirement `json:"-" yaml:"-"`

	// Tags is a slice of base.Tag instances defined by the specification
	// A list of tags used by the document with additional metadata. The order of the tags can be used to reflect on
	// their order by the parsing tools. Not all tags that are used by the Operation Object must be declared.
	// The tags that are not declared MAY be organized randomly or based on the tools’ logic.
	// Each tag name in the list MUST be unique.
	// - https://spec.openapis.org/oas/v3.1.0#tag-object
	Tags []*base.Tag `json:"tags,omitempty" yaml:"tags,omitempty"`

	// ExternalDocs is an instance of base.ExternalDoc for.. well, obvious really, innit.
	// - https://spec.openapis.org/oas/v3.1.0#external-documentation-object
	ExternalDocs *base.ExternalDoc `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`

	// Extensions contains all custom extensions defined for the top-level document.
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`

	// JsonSchemaDialect is a 3.1+ property that sets the dialect to use for validating *base.Schema definitions
	// The default value for the $schema keyword within Schema Objects contained within this OAS document.
	// This MUST be in the form of a URI.
	// - https://spec.openapis.org/oas/v3.1.0#schema-object
	JsonSchemaDialect string `json:"jsonSchemaDialect,omitempty" yaml:"jsonSchemaDialect,omitempty"`

	// Self is a 3.2+ property that sets the base URI for the document for resolving relative references
	// - https://spec.openapis.org/oas/v3.2.0#openapi-object
	Self string `json:"$self,omitempty" yaml:"$self,omitempty"`

	// Webhooks is a 3.1+ property that is similar to callbacks, except, this defines incoming webhooks.
	// The incoming webhooks that MAY be received as part of this API and that the API consumer MAY choose to implement.
	// Closely related to the callbacks feature, this section describes requests initiated other than by an API call,
	// for example by an out-of-band registration. The key name is a unique string to refer to each webhook,
	// while the (optionally referenced) Path Item Object describes a request that may be initiated by the API provider
	// and the expected responses. An example is available.
	Webhooks *orderedmap.Map[string, *PathItem] `json:"webhooks,omitempty" yaml:"webhooks,omitempty"`

	// Index is a reference to the *index.SpecIndex that was created for the document and used
	// as a guide when building out the Document. Ideal if further processing is required on the model and
	// the original details are required to continue the work.
	//
	// This property is not a part of the OpenAPI schema, this is custom to libopenapi.
	Index *index.SpecIndex `json:"-" yaml:"-"`

	// Rolodex is the low-level rolodex used when creating this document.
	// This in an internal structure and not part of the OpenAPI schema.
	Rolodex *index.Rolodex `json:"-" yaml:"-"`
	low     *lowv3.Document
}

// NewDocument will create a new high-level Document from a low-level one.
func NewDocument(document *lowv3.Document) *Document {
	d := new(Document)
	d.low = document
	d.Index = document.Index
	if !document.Info.IsEmpty() {
		d.Info = base.NewInfo(document.Info.Value)
	}
	if !document.Version.IsEmpty() {
		d.Version = document.Version.Value
	}
	var servers []*Server
	for _, ser := range document.Servers.Value {
		servers = append(servers, NewServer(ser.Value))
	}
	d.Servers = servers
	var tags []*base.Tag
	for _, tag := range document.Tags.Value {
		tags = append(tags, base.NewTag(tag.Value))
	}
	d.Tags = tags
	if !document.ExternalDocs.IsEmpty() {
		d.ExternalDocs = base.NewExternalDoc(document.ExternalDocs.Value)
	}
	if orderedmap.Len(document.Extensions) > 0 {
		d.Extensions = high.ExtractExtensions(document.Extensions)
	}
	if !document.Components.IsEmpty() {
		d.Components = NewComponents(document.Components.Value)
	}
	if !document.Paths.IsEmpty() {
		d.Paths = NewPaths(document.Paths.Value)
	}
	if !document.JsonSchemaDialect.IsEmpty() {
		d.JsonSchemaDialect = document.JsonSchemaDialect.Value
	}
	if !document.Self.IsEmpty() {
		d.Self = document.Self.Value
	}
	if !document.Webhooks.IsEmpty() {
		d.Webhooks = low.FromReferenceMapWithFunc(document.Webhooks.Value, NewPathItem)
	}
	if !document.Security.IsEmpty() {
		var security []*base.SecurityRequirement
		for s := range document.Security.Value {
			security = append(security, base.NewSecurityRequirement(document.Security.Value[s].Value))
		}
		d.Security = security
	}
	return d
}

// GoLow returns the low-level Document that was used to create the high level one.
func (d *Document) GoLow() *lowv3.Document {
	return d.low
}

// GoLowUntyped returns the low-level Document that was used to create the high level one, however, it's untyped.
func (d *Document) GoLowUntyped() any {
	return d.low
}

// Render will return a YAML representation of the Document object as a byte slice.
func (d *Document) Render() ([]byte, error) {
	return yaml.Marshal(d)
}

// RenderWithIndention will return a YAML representation of the Document object as a byte slice.
// the rendering will use the original indention of the document.
func (d *Document) RenderWithIndention(indent int) []byte {
	var buf bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)
	_ = yamlEncoder.Encode(d)
	return buf.Bytes()
}

// RenderJSON will return a JSON representation of the Document object as a byte slice.
func (d *Document) RenderJSON(indention string) ([]byte, error) {
	nb := high.NewNodeBuilder(d, d.low)

	dat, err := json.YAMLNodeToJSON(nb.Render(), indention)
	if err != nil {
		return dat, err
	}
	return dat, nil
}

func (d *Document) RenderInline() ([]byte, error) {
	di, _ := d.MarshalYAMLInline()
	return yaml.Marshal(di)
}

// MarshalYAML will create a ready to render YAML representation of the Document object.
func (d *Document) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(d, d.low)
	return nb.Render(), nil
}

func (d *Document) MarshalYAMLInline() (interface{}, error) {
	nb := high.NewNodeBuilder(d, d.low)
	nb.Resolve = true
	return nb.Render(), nil
}

func (d *Document) GetIndex() *index.SpecIndex {
	return d.Index
}

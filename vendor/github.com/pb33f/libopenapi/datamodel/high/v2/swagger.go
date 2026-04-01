// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package v2 represents all Swagger / OpenAPI 2 high-level models. High-level models are easy to navigate
// and simple to extract what ever is required from a specification.
//
// High-level models are backed by low-level ones. There is a 'GoLow()' method available on every high level
// object. 'Going Low' allows engineers to transition from a high-level or 'porcelain' API, to a low-level 'plumbing'
// API, which provides fine grain detail to the underlying AST powering the data, lines, columns, raw nodes etc.
//
// IMPORTANT: As a general rule, Swagger / OpenAPI 2 should be avoided for new projects.
// VERY IMPORTANT: pb33f is no longer maintaining the v2 model. It's a commercial product (Swagger) by a company (SmartBear) and not OpenAPI.
// PLEASE DO NOT USE THIS MODEL UNLESS YOU HAVE TO. IT'S HERE FOR LEGACY SUPPORT ONLY. Upgrade to 3x!
package v2

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Swagger represents a high-level Swagger / OpenAPI 2 document. An instance of Swagger is the root of the specification.
type Swagger struct {
	// Swagger is the version of Swagger / OpenAPI being used, extracted from the 'swagger: 2.x' definition.
	Swagger string

	// Info represents a specification Info definition.
	// Provides metadata about the API. The metadata can be used by the clients if needed.
	// - https://swagger.io/specification/v2/#infoObject
	Info *base.Info

	// Host is The host (name or ip) serving the API. This MUST be the host only and does not include the scheme nor
	// sub-paths. It MAY include a port. If the host is not included, the host serving the documentation is to be used
	// (including the port). The host does not support path templating.
	Host string

	// BasePath is The base path on which the API is served, which is relative to the host. If it is not included, the API is
	// served directly under the host. The value MUST start with a leading slash (/).
	// The basePath does not support path templating.
	BasePath string

	// Schemes represents the transfer protocol of the API. Requirements MUST be from the list: "http", "https", "ws", "wss".
	// If the schemes is not included, the default scheme to be used is the one used to access
	// the Swagger definition itself.
	Schemes []string

	// Consumes is a list of MIME types the APIs can consume. This is global to all APIs but can be overridden on
	// specific API calls. Value MUST be as described under Mime Types.
	Consumes []string

	// Produces is a list of MIME types the APIs can produce. This is global to all APIs but can be overridden on
	// specific API calls. Value MUST be as described under Mime Types.
	Produces []string

	// Paths are the paths and operations for the API. Perhaps the most important part of the specification.
	//  - https://swagger.io/specification/v2/#pathsObject
	Paths *Paths

	// Definitions is an object to hold data types produced and consumed by operations. It's composed of Schema instances
	//  - https://swagger.io/specification/v2/#definitionsObject
	Definitions *Definitions

	// Parameters is an object to hold parameters that can be used across operations.
	// This property does not define global parameters for all operations.
	//  - https://swagger.io/specification/v2/#parametersDefinitionsObject
	Parameters *ParameterDefinitions

	// Responses is an object to hold responses that can be used across operations.
	// This property does not define global responses for all operations.
	//  - https://swagger.io/specification/v2/#responsesDefinitionsObject
	Responses *ResponsesDefinitions

	// SecurityDefinitions represents security scheme definitions that can be used across the specification.
	//  - https://swagger.io/specification/v2/#securityDefinitionsObject
	SecurityDefinitions *SecurityDefinitions

	// Security is a declaration of which security schemes are applied for the API as a whole. The list of values
	// describes alternative security schemes that can be used (that is, there is a logical OR between the security
	// requirements). Individual operations can override this definition.
	//  - https://swagger.io/specification/v2/#securityRequirementObject
	Security []*base.SecurityRequirement

	// Tags are A list of tags used by the specification with additional metadata.
	// The order of the tags can be used to reflect on their order by the parsing tools. Not all tags that are used
	// by the Operation Object must be declared. The tags that are not declared may be organized randomly or based
	// on the tools' logic. Each tag name in the list MUST be unique.
	//  - https://swagger.io/specification/v2/#tagObject
	Tags []*base.Tag

	// ExternalDocs is an instance of base.ExternalDoc for.. well, obvious really, innit.
	ExternalDocs *base.ExternalDoc

	// Extensions contains all custom extensions defined for the top-level document.
	Extensions *orderedmap.Map[string, *yaml.Node]
	low        *low.Swagger
}

// NewSwaggerDocument will create a new high-level Swagger document from a low-level one.
func NewSwaggerDocument(document *low.Swagger) *Swagger {
	d := new(Swagger)
	d.low = document
	d.Extensions = high.ExtractExtensions(document.Extensions)
	if !document.Info.IsEmpty() {
		d.Info = base.NewInfo(document.Info.Value)
	}
	if !document.Swagger.IsEmpty() {
		d.Swagger = document.Swagger.Value
	}
	if !document.Host.IsEmpty() {
		d.Host = document.Host.Value
	}
	if !document.BasePath.IsEmpty() {
		d.BasePath = document.BasePath.Value
	}

	if !document.Schemes.IsEmpty() {
		var schemes []string
		for s := range document.Schemes.Value {
			schemes = append(schemes, document.Schemes.Value[s].Value)
		}
		d.Schemes = schemes
	}
	if !document.Consumes.IsEmpty() {
		var consumes []string
		for c := range document.Consumes.Value {
			consumes = append(consumes, document.Consumes.Value[c].Value)
		}
		d.Consumes = consumes
	}
	if !document.Produces.IsEmpty() {
		var produces []string
		for p := range document.Produces.Value {
			produces = append(produces, document.Produces.Value[p].Value)
		}
		d.Produces = produces
	}
	if !document.Paths.IsEmpty() {
		d.Paths = NewPaths(document.Paths.Value)
	}
	if !document.Definitions.IsEmpty() {
		d.Definitions = NewDefinitions(document.Definitions.Value)
	}
	if !document.Parameters.IsEmpty() {
		d.Parameters = NewParametersDefinitions(document.Parameters.Value)
	}

	if !document.Responses.IsEmpty() {
		d.Responses = NewResponsesDefinitions(document.Responses.Value)
	}
	if !document.SecurityDefinitions.IsEmpty() {
		d.SecurityDefinitions = NewSecurityDefinitions(document.SecurityDefinitions.Value)
	}
	if !document.Security.IsEmpty() {
		var security []*base.SecurityRequirement
		for s := range document.Security.Value {
			security = append(security, base.NewSecurityRequirement(document.Security.Value[s].Value))
		}
		d.Security = security
	}
	if !document.Tags.IsEmpty() {
		var tags []*base.Tag
		for t := range document.Tags.Value {
			tags = append(tags, base.NewTag(document.Tags.Value[t].Value))
		}
		d.Tags = tags
	}
	if !document.ExternalDocs.IsEmpty() {
		d.ExternalDocs = base.NewExternalDoc(document.ExternalDocs.Value)
	}
	return d
}

// GoLow returns the low-level Swagger instance that was used to create the high-level one.
func (s *Swagger) GoLow() *low.Swagger {
	return s.low
}

// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package v3 represents all OpenAPI 3+ low-level models. Low-level models are more difficult to navigate
// than higher-level models, however they are packed with all the raw AST and node data required to perform
// any kind of analysis on the underlying data.
//
// Every property is wrapped in a NodeReference or a KeyReference or a ValueReference.
package v3

import (
	"hash/maphash"
	"sort"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

type Document struct {
	// Version is the version of OpenAPI being used, extracted from the 'openapi: x.x.x' definition.
	// This is not a standard property of the OpenAPI model, it's a convenience mechanism only.
	Version low.NodeReference[string]

	// Info represents a specification Info definitions
	// Provides metadata about the API. The metadata MAY be used by tooling as required.
	// - https://spec.openapis.org/oas/v3.1.0#info-object
	Info low.NodeReference[*base.Info]

	// JsonSchemaDialect is a 3.1+ property that sets the dialect to use for validating *base.Schema definitions
	// The default value for the $schema keyword within Schema Objects contained within this OAS document.
	// This MUST be in the form of a URI.
	// - https://spec.openapis.org/oas/v3.1.0#schema-object
	JsonSchemaDialect low.NodeReference[string] // 3.1

	// Self is a 3.2+ property that sets the base URI for the document for resolving relative references
	// - https://spec.openapis.org/oas/v3.2.0#openapi-object
	Self low.NodeReference[string] // 3.2

	// Webhooks is a 3.1+ property that is similar to callbacks, except, this defines incoming webhooks.
	// The incoming webhooks that MAY be received as part of this API and that the API consumer MAY choose to implement.
	// Closely related to the callbacks feature, this section describes requests initiated other than by an API call,
	// for example by an out-of-band registration. The key name is a unique string to refer to each webhook,
	// while the (optionally referenced) Path Item Object describes a request that may be initiated by the API provider
	// and the expected responses. An example is available.
	Webhooks low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*PathItem]]] // 3.1

	// Servers is a slice of Server instances which provide connectivity information to a target server. If the servers
	// property is not provided, or is an empty array, the default value would be a Server Object with an url value of /.
	// - https://spec.openapis.org/oas/v3.1.0#server-object
	Servers low.NodeReference[[]low.ValueReference[*Server]]

	// Paths contains all the PathItem definitions for the specification.
	// The available paths and operations for the API, The most important part of ths spec.
	// - https://spec.openapis.org/oas/v3.1.0#paths-object
	Paths low.NodeReference[*Paths]

	// Components is an element to hold various schemas for the document.
	// - https://spec.openapis.org/oas/v3.1.0#components-object
	Components low.NodeReference[*Components]

	// Security contains global security requirements/roles for the specification
	// A declaration of which security mechanisms can be used across the API. The list of values includes alternative
	// security requirement objects that can be used. Only one of the security requirement objects need to be satisfied
	// to authorize a request. Individual operations can override this definition. To make security optional,
	// an empty security requirement ({}) can be included in the array.
	// - https://spec.openapis.org/oas/v3.1.0#security-requirement-object
	Security low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]

	// Tags is a slice of base.Tag instances defined by the specification
	// A list of tags used by the document with additional metadata. The order of the tags can be used to reflect on
	// their order by the parsing tools. Not all tags that are used by the Operation Object must be declared.
	// The tags that are not declared MAY be organized randomly or based on the tools’ logic.
	// Each tag name in the list MUST be unique.
	// - https://spec.openapis.org/oas/v3.1.0#tag-object
	Tags low.NodeReference[[]low.ValueReference[*base.Tag]]

	// ExternalDocs is an instance of base.ExternalDoc for.. well, obvious really, innit.
	// - https://spec.openapis.org/oas/v3.1.0#external-documentation-object
	ExternalDocs low.NodeReference[*base.ExternalDoc]

	// Extensions contains all custom extensions defined for the top-level document.
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]

	// Index is a reference to the *index.SpecIndex that was created for the document and used
	// as a guide when building out the Document. Ideal if further processing is required on the model and
	// the original details are required to continue the work.
	//
	// This property is not a part of the OpenAPI schema, this is custom to libopenapi.
	Index *index.SpecIndex

	// Rolodex is a reference to the rolodex used when creating this document.
	Rolodex *index.Rolodex

	// StorageRoot is the root path to the storage location of the document. This has no effect on resolving references.
	// but it's used by the doctor to determine where to store the document. This is not part of the OpenAPI schema.
	StorageRoot string `json:"-" yaml:"-"`

	low.NodeMap
}

// FindSecurityRequirement will attempt to locate a security requirement string from a supplied name.
func (d *Document) FindSecurityRequirement(name string) []low.ValueReference[string] {
	for k := range d.Security.Value {
		requirements := d.Security.Value[k].Value.Requirements
		for k, v := range requirements.Value.FromOldest() {
			if k.Value == name {
				return v.Value
			}
		}
	}
	return nil
}

// GetExtensions returns all Document extensions and satisfies the low.HasExtensions interface.
func (d *Document) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return d.Extensions
}

func (d *Document) GetExternalDocs() *low.NodeReference[any] {
	return &low.NodeReference[any]{
		KeyNode:   d.ExternalDocs.KeyNode,
		ValueNode: d.ExternalDocs.ValueNode,
		Value:     d.ExternalDocs.Value,
	}
}

func (d *Document) GetIndex() *index.SpecIndex {
	return d.Index
}

// Hash will return a consistent Hash of the Document object
func (d *Document) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if d.Version.Value != "" {
			h.WriteString(d.Version.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if d.Info.Value != nil {
			h.WriteString(low.GenerateHashString(d.Info.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if d.JsonSchemaDialect.Value != "" {
			h.WriteString(d.JsonSchemaDialect.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if d.Self.Value != "" {
			h.WriteString(d.Self.Value)
			h.WriteByte(low.HASH_PIPE)
		}

		// Webhooks - pre-allocate slice
		if d.Webhooks.GetValue() != nil {
			webhookLen := d.Webhooks.GetValue().Len()
			if webhookLen > 0 {
				keys := make([]string, 0, webhookLen)
				for k, v := range d.Webhooks.GetValue().FromOldest() {
					keys = append(keys, k.Value+"-"+low.GenerateHashString(v.Value))
				}
				sort.Strings(keys)
				for _, key := range keys {
					h.WriteString(key)
					h.WriteByte(low.HASH_PIPE)
				}
			}
		}

		// Servers - pre-allocate slice
		serverLen := len(d.Servers.Value)
		if serverLen > 0 {
			keys := make([]string, 0, serverLen)
			for i := range d.Servers.Value {
				keys = append(keys, low.GenerateHashString(d.Servers.Value[i].Value))
			}
			sort.Strings(keys)
			for _, key := range keys {
				h.WriteString(key)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		if d.Paths.Value != nil {
			h.WriteString(low.GenerateHashString(d.Paths.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if d.Components.Value != nil {
			h.WriteString(low.GenerateHashString(d.Components.Value))
			h.WriteByte(low.HASH_PIPE)
		}

		// Security - pre-allocate slice
		securityLen := len(d.Security.Value)
		if securityLen > 0 {
			keys := make([]string, 0, securityLen)
			for i := range d.Security.Value {
				keys = append(keys, low.GenerateHashString(d.Security.Value[i].Value))
			}
			sort.Strings(keys)
			for _, key := range keys {
				h.WriteString(key)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		// Tags - pre-allocate slice
		tagLen := len(d.Tags.Value)
		if tagLen > 0 {
			keys := make([]string, 0, tagLen)
			for i := range d.Tags.Value {
				keys = append(keys, low.GenerateHashString(d.Tags.Value[i].Value))
			}
			sort.Strings(keys)
			for _, key := range keys {
				h.WriteString(key)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		if d.ExternalDocs.Value != nil {
			h.WriteString(low.GenerateHashString(d.ExternalDocs.Value))
			h.WriteByte(low.HASH_PIPE)
		}

		// Extensions
		for _, ext := range low.HashExtensions(d.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

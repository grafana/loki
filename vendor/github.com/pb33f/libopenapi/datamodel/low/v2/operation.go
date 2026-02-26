// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"context"
	"hash/maphash"
	"sort"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Operation represents a low-level Swagger / OpenAPI 2 Operation object.
//
// It describes a single API operation on a path.
//   - https://swagger.io/specification/v2/#operationObject
type Operation struct {
	Tags         low.NodeReference[[]low.ValueReference[string]]
	Summary      low.NodeReference[string]
	Description  low.NodeReference[string]
	ExternalDocs low.NodeReference[*base.ExternalDoc]
	OperationId  low.NodeReference[string]
	Consumes     low.NodeReference[[]low.ValueReference[string]]
	Produces     low.NodeReference[[]low.ValueReference[string]]
	Parameters   low.NodeReference[[]low.ValueReference[*Parameter]]
	Responses    low.NodeReference[*Responses]
	Schemes      low.NodeReference[[]low.ValueReference[string]]
	Deprecated   low.NodeReference[bool]
	Security     low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]
	Extensions   *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
}

// Build will extract external docs, extensions, parameters, responses and security requirements.
func (o *Operation) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	o.Extensions = low.ExtractExtensions(root)

	// extract externalDocs
	extDocs, dErr := low.ExtractObject[*base.ExternalDoc](ctx, base.ExternalDocsLabel, root, idx)
	if dErr != nil {
		return dErr
	}
	o.ExternalDocs = extDocs

	// extract parameters
	params, ln, vn, pErr := low.ExtractArray[*Parameter](ctx, ParametersLabel, root, idx)
	if pErr != nil {
		return pErr
	}
	if params != nil {
		o.Parameters = low.NodeReference[[]low.ValueReference[*Parameter]]{
			Value:     params,
			KeyNode:   ln,
			ValueNode: vn,
		}
	}

	// extract responses
	respBody, respErr := low.ExtractObject[*Responses](ctx, ResponsesLabel, root, idx)
	if respErr != nil {
		return respErr
	}
	o.Responses = respBody

	// extract security
	sec, sln, svn, sErr := low.ExtractArray[*base.SecurityRequirement](ctx, SecurityLabel, root, idx)
	if sErr != nil {
		return sErr
	}
	if sec != nil {
		o.Security = low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]{
			Value:     sec,
			KeyNode:   sln,
			ValueNode: svn,
		}
	}
	return nil
}

// Hash will return a consistent Hash of the Operation object
func (o *Operation) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !o.Summary.IsEmpty() {
			h.WriteString(o.Summary.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.Description.IsEmpty() {
			h.WriteString(o.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.OperationId.IsEmpty() {
			h.WriteString(o.OperationId.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.ExternalDocs.IsEmpty() {
			h.WriteString(low.GenerateHashString(o.ExternalDocs.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.Responses.IsEmpty() {
			h.WriteString(low.GenerateHashString(o.Responses.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !o.Deprecated.IsEmpty() {
			low.HashBool(h, o.Deprecated.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		var keys []string
		keys = make([]string, len(o.Tags.Value))
		for k := range o.Tags.Value {
			keys[k] = o.Tags.Value[k].Value
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}

		keys = make([]string, len(o.Consumes.Value))
		for k := range o.Consumes.Value {
			keys[k] = o.Consumes.Value[k].Value
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}

		keys = make([]string, len(o.Produces.Value))
		for k := range o.Produces.Value {
			keys[k] = o.Produces.Value[k].Value
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}

		keys = make([]string, len(o.Schemes.Value))
		for k := range o.Schemes.Value {
			keys[k] = o.Schemes.Value[k].Value
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}

		keys = make([]string, len(o.Parameters.Value))
		for k := range o.Parameters.Value {
			keys[k] = low.GenerateHashString(o.Parameters.Value[k].Value)
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}

		keys = make([]string, len(o.Security.Value))
		for k := range o.Security.Value {
			keys[k] = low.GenerateHashString(o.Security.Value[k].Value)
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(o.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

// methods to satisfy swagger operations interface

func (o *Operation) GetTags() low.NodeReference[[]low.ValueReference[string]] {
	return o.Tags
}

func (o *Operation) GetSummary() low.NodeReference[string] {
	return o.Summary
}

func (o *Operation) GetDescription() low.NodeReference[string] {
	return o.Description
}

func (o *Operation) GetExternalDocs() low.NodeReference[any] {
	return low.NodeReference[any]{
		ValueNode: o.ExternalDocs.ValueNode,
		KeyNode:   o.ExternalDocs.KeyNode,
		Value:     o.ExternalDocs.Value,
	}
}

func (o *Operation) GetOperationId() low.NodeReference[string] {
	return o.OperationId
}

func (o *Operation) GetDeprecated() low.NodeReference[bool] {
	return o.Deprecated
}

func (o *Operation) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return o.Extensions
}

func (o *Operation) GetResponses() low.NodeReference[any] {
	return low.NodeReference[any]{
		ValueNode: o.Responses.ValueNode,
		KeyNode:   o.Responses.KeyNode,
		Value:     o.Responses.Value,
	}
}

func (o *Operation) GetParameters() low.NodeReference[any] {
	return low.NodeReference[any]{
		ValueNode: o.Parameters.ValueNode,
		KeyNode:   o.Parameters.KeyNode,
		Value:     o.Parameters.Value,
	}
}

func (o *Operation) GetSecurity() low.NodeReference[any] {
	return low.NodeReference[any]{
		ValueNode: o.Security.ValueNode,
		KeyNode:   o.Security.KeyNode,
		Value:     o.Security.Value,
	}
}

func (o *Operation) GetSchemes() low.NodeReference[[]low.ValueReference[string]] {
	return o.Schemes
}

func (o *Operation) GetProduces() low.NodeReference[[]low.ValueReference[string]] {
	return o.Produces
}

func (o *Operation) GetConsumes() low.NodeReference[[]low.ValueReference[string]] {
	return o.Consumes
}

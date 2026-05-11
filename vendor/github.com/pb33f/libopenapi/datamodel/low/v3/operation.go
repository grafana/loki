// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

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

// Operation is a low-level representation of an OpenAPI 3+ Operation object.
//
// An Operation is perhaps the most important object of the entire specification. Everything of value
// happens here. The entire being for existence of this library and the specification, is this Operation.
//   - https://spec.openapis.org/oas/v3.1.0#operation-object
type Operation struct {
	Tags         low.NodeReference[[]low.ValueReference[string]]
	Summary      low.NodeReference[string]
	Description  low.NodeReference[string]
	ExternalDocs low.NodeReference[*base.ExternalDoc]
	OperationId  low.NodeReference[string]
	Parameters   low.NodeReference[[]low.ValueReference[*Parameter]]
	RequestBody  low.NodeReference[*RequestBody]
	Responses    low.NodeReference[*Responses]
	Callbacks    low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Callback]]]
	Deprecated   low.NodeReference[bool]
	Security     low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]
	Servers      low.NodeReference[[]low.ValueReference[*Server]]
	Extensions   *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode      *yaml.Node
	RootNode     *yaml.Node
	index        *index.SpecIndex
	context      context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Operation object.
func (o *Operation) GetIndex() *index.SpecIndex {
	return o.index
}

// GetContext returns the context.Context instance used when building the Operation object.
func (o *Operation) GetContext() context.Context {
	return o.context
}

// FindCallback will attempt to locate a Callback instance by the supplied name.
func (o *Operation) FindCallback(callback string) *low.ValueReference[*Callback] {
	return low.FindItemInOrderedMap(callback, o.Callbacks.GetValue())
}

// FindSecurityRequirement will attempt to locate a security requirement string from a supplied name.
func (o *Operation) FindSecurityRequirement(name string) []low.ValueReference[string] {
	for k := range o.Security.Value {
		requirements := o.Security.Value[k].Value.Requirements
		for k, v := range requirements.Value.FromOldest() {
			if k.Value == name {
				return v.Value
			}
		}
	}
	return nil
}

// GetRootNode returns the root yaml node of the Operation object
func (o *Operation) GetRootNode() *yaml.Node {
	return o.RootNode
}

// GetKeyNode returns the key yaml node of the Operation object
func (o *Operation) GetKeyNode() *yaml.Node {
	return o.KeyNode
}

// Build will extract external docs, parameters, request body, responses, callbacks, security and servers.
func (o *Operation) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	o.KeyNode = keyNode
	o.RootNode = root
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	o.Reference = new(low.Reference)
	o.Nodes = low.ExtractNodes(ctx, root)
	o.Extensions = low.ExtractExtensions(root)
	o.index = idx
	o.context = ctx
	low.ExtractExtensionNodes(ctx, o.Extensions, o.Nodes)

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
		o.Nodes.Store(ln.Line, ln)
	}

	// extract request body
	rBody, rErr := low.ExtractObject[*RequestBody](ctx, RequestBodyLabel, root, idx)
	if rErr != nil {
		return rErr
	}
	o.RequestBody = rBody

	// extract tags, but only extract nodes, the model has already been built
	k, v := utils.FindKeyNode(TagsLabel, root.Content)
	if k != nil && v != nil {
		o.Nodes.Store(k.Line, k)
		nm := low.ExtractNodesRecursive(ctx, v)
		nm.Range(func(key, value interface{}) bool {
			o.Nodes.Store(key, value)
			return true
		})
	}

	// extract responses
	respBody, respErr := low.ExtractObject[*Responses](ctx, ResponsesLabel, root, idx)
	if respErr != nil {
		return respErr
	}
	o.Responses = respBody

	// extract callbacks
	callbacks, cbL, cbN, cbErr := low.ExtractMap[*Callback](ctx, CallbacksLabel, root, idx)
	if cbErr != nil {
		return cbErr
	}
	if callbacks != nil {
		o.Callbacks = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Callback]]]{
			Value:     callbacks,
			KeyNode:   cbL,
			ValueNode: cbN,
		}
		o.Nodes.Store(cbL.Line, cbL)
		for k, v := range callbacks.FromOldest() {
			v.Value.Nodes.Store(k.KeyNode.Line, k.KeyNode)
		}
	}

	// extract security
	sec, sln, svn, sErr := low.ExtractArray[*base.SecurityRequirement](ctx, SecurityLabel, root, idx)
	if sErr != nil {
		return sErr
	}

	// if security is defined and requirements are provided.
	if sln != nil && len(svn.Content) > 0 && sec != nil {
		o.Security = low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]{
			Value:     sec,
			KeyNode:   sln,
			ValueNode: svn,
		}
		o.Nodes.Store(sln.Line, sln)
	}

	// if security is set, but no requirements are defined.
	// https://github.com/pb33f/libopenapi/issues/111
	if sln != nil && len(svn.Content) == 0 && sec == nil {
		o.Security = low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]{
			Value:     []low.ValueReference[*base.SecurityRequirement]{}, // empty
			KeyNode:   sln,
			ValueNode: svn,
		}
		o.Nodes.Store(sln.Line, svn)
	}

	// extract servers
	servers, sl, sn, serErr := low.ExtractArray[*Server](ctx, ServersLabel, root, idx)
	if serErr != nil {
		return serErr
	}
	if servers != nil {
		o.Servers = low.NodeReference[[]low.ValueReference[*Server]]{
			Value:     servers,
			KeyNode:   sl,
			ValueNode: sn,
		}
		o.Nodes.Store(sl.Line, sl)
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
		if !o.RequestBody.IsEmpty() {
			h.WriteString(low.GenerateHashString(o.RequestBody.Value))
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
		if !o.Security.IsEmpty() {
			// Pre-allocate keys for sorting
			secKeys := make([]string, len(o.Security.Value))
			for k := range o.Security.Value {
				secKeys[k] = low.GenerateHashString(o.Security.Value[k].Value)
			}
			sort.Strings(secKeys)
			for _, key := range secKeys {
				h.WriteString(key)
				h.WriteByte(low.HASH_PIPE)
			}
		}
		if !o.Deprecated.IsEmpty() {
			low.HashBool(h, o.Deprecated.Value)
			h.WriteByte(low.HASH_PIPE)
		}

		// Tags array - pre-allocate and sort
		if len(o.Tags.Value) > 0 {
			tags := make([]string, len(o.Tags.Value))
			for k := range o.Tags.Value {
				tags[k] = o.Tags.Value[k].Value
			}
			sort.Strings(tags)
			for _, tag := range tags {
				h.WriteString(tag)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		// Servers array - pre-allocate and sort
		if len(o.Servers.Value) > 0 {
			servers := make([]string, len(o.Servers.Value))
			for k := range o.Servers.Value {
				servers[k] = low.GenerateHashString(o.Servers.Value[k].Value)
			}
			sort.Strings(servers)
			for _, server := range servers {
				h.WriteString(server)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		// Parameters array - pre-allocate and sort
		if len(o.Parameters.Value) > 0 {
			params := make([]string, len(o.Parameters.Value))
			for k := range o.Parameters.Value {
				params[k] = low.GenerateHashString(o.Parameters.Value[k].Value)
			}
			sort.Strings(params)
			for _, param := range params {
				h.WriteString(param)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		// Callbacks
		for v := range orderedmap.SortAlpha(o.Callbacks.Value).ValuesFromOldest() {
			h.WriteString(low.GenerateHashString(v.Value))
			h.WriteByte(low.HASH_PIPE)
		}

		// Extensions
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

func (o *Operation) GetRequestBody() low.NodeReference[any] {
	return low.NodeReference[any]{
		ValueNode: o.RequestBody.ValueNode,
		KeyNode:   o.RequestBody.KeyNode,
		Value:     o.RequestBody.Value,
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

func (o *Operation) GetServers() low.NodeReference[any] {
	return low.NodeReference[any]{
		ValueNode: o.Servers.ValueNode,
		KeyNode:   o.Servers.KeyNode,
		Value:     o.Servers.Value,
	}
}

func (o *Operation) GetCallbacks() low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Callback]]] {
	return o.Callbacks
}

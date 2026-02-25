// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"fmt"
	"hash/maphash"
	"slices"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Parameter represents a high-level OpenAPI 3+ Parameter object, that is backed by a low-level one.
//
// A unique parameter is defined by a combination of a name and location.
//   - https://spec.openapis.org/oas/v3.1.0#parameter-object
type Parameter struct {
	KeyNode         *yaml.Node
	RootNode        *yaml.Node
	Name            low.NodeReference[string]
	In              low.NodeReference[string]
	Description     low.NodeReference[string]
	Required        low.NodeReference[bool]
	Deprecated      low.NodeReference[bool]
	AllowEmptyValue low.NodeReference[bool]
	Style           low.NodeReference[string]
	Explode         low.NodeReference[bool]
	AllowReserved   low.NodeReference[bool]
	Schema          low.NodeReference[*base.SchemaProxy]
	Example         low.NodeReference[*yaml.Node]
	Examples        low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.Example]]]
	Content         low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*MediaType]]]
	Extensions      *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	index           *index.SpecIndex
	context         context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Parameter object.
func (p *Parameter) GetIndex() *index.SpecIndex {
	return p.index
}

// GetContext returns the context.Context instance used when building the Parameter
func (p *Parameter) GetContext() context.Context {
	return p.context
}

// GetRootNode returns the root yaml node of the Parameter object.
func (p *Parameter) GetRootNode() *yaml.Node {
	return p.RootNode
}

// GetKeyNode returns the key yaml node of the Parameter object.
func (p *Parameter) GetKeyNode() *yaml.Node {
	return p.KeyNode
}

// FindContent will attempt to locate a MediaType instance using the specified name.
func (p *Parameter) FindContent(cType string) *low.ValueReference[*MediaType] {
	return low.FindItemInOrderedMap[*MediaType](cType, p.Content.Value)
}

// FindExample will attempt to locate a base.Example instance using the specified name.
func (p *Parameter) FindExample(eType string) *low.ValueReference[*base.Example] {
	return low.FindItemInOrderedMap[*base.Example](eType, p.Examples.Value)
}

// FindExtension attempts to locate an extension using the specified name.
func (p *Parameter) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, p.Extensions)
}

// GetExtensions returns all extensions for Parameter.
func (p *Parameter) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return p.Extensions
}

// Build will extract examples, extensions and content/media types.
func (p *Parameter) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	p.Reference = new(low.Reference)
	if ok, _, ref := utils.IsNodeRefValue(root); ok {
		p.SetReference(ref, root)
	}
	root = utils.NodeAlias(root)
	p.KeyNode = keyNode
	p.RootNode = root
	utils.CheckForMergeNodes(root)
	p.Nodes = low.ExtractNodes(ctx, root)
	p.Extensions = low.ExtractExtensions(root)
	p.index = idx
	p.context = ctx
	low.ExtractExtensionNodes(ctx, p.Extensions, p.Nodes)

	// handle example if set.
	_, expLabel, expNode := utils.FindKeyNodeFullTop(base.ExampleLabel, root.Content)
	if expNode != nil {
		p.Example = low.NodeReference[*yaml.Node]{Value: expNode, KeyNode: expLabel, ValueNode: expNode}
		p.Nodes.Store(expLabel.Line, expLabel)
	}

	// handle schema
	sch, sErr := base.ExtractSchema(ctx, root, idx)
	if sErr != nil {
		return sErr
	}
	if sch != nil {
		p.Schema = *sch
	}

	// handle examples if set.
	exps, expsL, expsN, eErr := low.ExtractMap[*base.Example](ctx, base.ExamplesLabel, root, idx)
	if eErr != nil {
		return eErr
	}
	// Only consider examples if they are defined in the root node.
	if exps != nil && slices.Contains(root.Content, expsL) {
		p.Examples = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.Example]]]{
			Value:     exps,
			KeyNode:   expsL,
			ValueNode: expsN,
		}
		p.Nodes.Store(expsL.Line, expsL)
		for k, v := range exps.FromOldest() {
			v.Value.Nodes.Store(k.KeyNode.Line, k.KeyNode)
		}
	}

	// handle content, if set.
	con, cL, cN, cErr := low.ExtractMap[*MediaType](ctx, ContentLabel, root, idx)
	if cErr != nil {
		return cErr
	}
	p.Content = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*MediaType]]]{
		Value:     con,
		KeyNode:   cL,
		ValueNode: cN,
	}
	if cL != nil {
		p.Nodes.Store(cL.Line, cL)
		for k, v := range con.FromOldest() {
			v.Value.Nodes.Store(k.KeyNode.Line, k.KeyNode)
		}
	}

	return nil
}

// Hash will return a consistent Hash of the Parameter object
func (p *Parameter) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if p.Name.Value != "" {
			h.WriteString(p.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if p.In.Value != "" {
			h.WriteString(p.In.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if p.Description.Value != "" {
			h.WriteString(p.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		low.HashBool(h, p.Required.Value)
		h.WriteByte(low.HASH_PIPE)
		low.HashBool(h, p.Deprecated.Value)
		h.WriteByte(low.HASH_PIPE)
		low.HashBool(h, p.AllowEmptyValue.Value)
		h.WriteByte(low.HASH_PIPE)
		if p.Style.Value != "" {
			h.WriteString(p.Style.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		low.HashBool(h, p.Explode.Value)
		h.WriteByte(low.HASH_PIPE)
		low.HashBool(h, p.AllowReserved.Value)
		h.WriteByte(low.HASH_PIPE)
		if p.Schema.Value != nil && p.Schema.Value.Schema() != nil {
			h.WriteString(fmt.Sprintf("%x", p.Schema.Value.Schema().Hash()))
			h.WriteByte(low.HASH_PIPE)
		}
		if p.Example.Value != nil && !p.Example.Value.IsZero() {
			h.WriteString(low.GenerateHashString(p.Example.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for v := range orderedmap.SortAlpha(p.Examples.Value).ValuesFromOldest() {
			h.WriteString(low.GenerateHashString(v.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for v := range orderedmap.SortAlpha(p.Content.Value).ValuesFromOldest() {
			h.WriteString(low.GenerateHashString(v.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(p.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

// IsParameter compliance methods.

func (p *Parameter) GetName() *low.NodeReference[string] {
	return &p.Name
}

func (p *Parameter) GetIn() *low.NodeReference[string] {
	return &p.In
}

func (p *Parameter) GetDescription() *low.NodeReference[string] {
	return &p.Description
}

func (p *Parameter) GetRequired() *low.NodeReference[bool] {
	return &p.Required
}

func (p *Parameter) GetDeprecated() *low.NodeReference[bool] {
	return &p.Deprecated
}

func (p *Parameter) GetAllowEmptyValue() *low.NodeReference[bool] {
	return &p.AllowEmptyValue
}

func (p *Parameter) GetSchema() *low.NodeReference[any] {
	i := low.NodeReference[any]{
		KeyNode:   p.Schema.KeyNode,
		ValueNode: p.Schema.ValueNode,
		Value:     p.Schema.Value,
	}
	return &i
}

func (p *Parameter) GetStyle() *low.NodeReference[string] {
	return &p.Style
}

func (p *Parameter) GetAllowReserved() *low.NodeReference[bool] {
	return &p.AllowReserved
}

func (p *Parameter) GetExplode() *low.NodeReference[bool] {
	return &p.Explode
}

func (p *Parameter) GetExample() *low.NodeReference[*yaml.Node] {
	return &p.Example
}

func (p *Parameter) GetExamples() *low.NodeReference[any] {
	i := low.NodeReference[any]{
		KeyNode:   p.Examples.KeyNode,
		ValueNode: p.Examples.ValueNode,
		Value:     p.Examples.Value,
	}
	return &i
}

func (p *Parameter) GetContent() *low.NodeReference[any] {
	c := low.NodeReference[any]{
		KeyNode:   p.Content.KeyNode,
		ValueNode: p.Content.ValueNode,
		Value:     p.Content.Value,
	}
	return &c
}

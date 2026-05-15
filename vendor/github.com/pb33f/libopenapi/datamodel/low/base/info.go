// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"go.yaml.in/yaml/v4"
)

// Info represents a low-level Info object as defined by both OpenAPI 2 and OpenAPI 3.
//
// The object provides metadata about the API. The metadata MAY be used by the clients if needed, and MAY be presented
// in editing or documentation generation tools for convenience.
//
//	v2 - https://swagger.io/specification/v2/#infoObject
//	v3 - https://spec.openapis.org/oas/v3.1.0#info-object
type Info struct {
	Title          low.NodeReference[string]
	Summary        low.NodeReference[string]
	Description    low.NodeReference[string]
	TermsOfService low.NodeReference[string]
	Contact        low.NodeReference[*Contact]
	License        low.NodeReference[*License]
	Version        low.NodeReference[string]
	Extensions     *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode        *yaml.Node
	RootNode       *yaml.Node
	index          *index.SpecIndex
	context        context.Context
	*low.Reference
	low.NodeMap
}

// FindExtension attempts to locate an extension with the supplied key
func (i *Info) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, i.Extensions)
}

// GetRootNode will return the root yaml node of the Info object
func (i *Info) GetRootNode() *yaml.Node {
	return i.RootNode
}

// GetKeyNode will return the key yaml node of the Info object
func (i *Info) GetKeyNode() *yaml.Node {
	return i.KeyNode
}

// GetExtensions returns all extensions for Info
func (i *Info) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return i.Extensions
}

// Build will extract out the Contact and Info objects from the supplied root node.
func (i *Info) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	i.KeyNode = keyNode
	root = utils.NodeAlias(root)
	i.RootNode = root
	utils.CheckForMergeNodes(root)
	i.Reference = new(low.Reference)
	i.Nodes = low.ExtractNodes(ctx, root)
	i.Extensions = low.ExtractExtensions(root)
	i.index = idx
	i.context = ctx

	// extract contact
	contact, _ := low.ExtractObject[*Contact](ctx, ContactLabel, root, idx)
	i.Contact = contact

	// extract license
	lic, _ := low.ExtractObject[*License](ctx, LicenseLabel, root, idx)
	i.License = lic
	return nil
}

// GetIndex will return the index.SpecIndex instance attached to the Info object
func (i *Info) GetIndex() *index.SpecIndex {
	return i.index
}

// GetContext will return the context.Context instance used when building the Info object
func (i *Info) GetContext() context.Context {
	return i.context
}

// Hash will return a consistent hash of the Info object
func (i *Info) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !i.Title.IsEmpty() {
			h.WriteString(i.Title.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.Summary.IsEmpty() {
			h.WriteString(i.Summary.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.Description.IsEmpty() {
			h.WriteString(i.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.TermsOfService.IsEmpty() {
			h.WriteString(i.TermsOfService.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.Contact.IsEmpty() {
			h.WriteString(low.GenerateHashString(i.Contact.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.License.IsEmpty() {
			h.WriteString(low.GenerateHashString(i.License.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !i.Version.IsEmpty() {
			h.WriteString(i.Version.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(i.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// RequestBody represents a low-level Arazzo Request Body Object.
// https://spec.openapis.org/arazzo/v1.0.1#request-body-object
type RequestBody struct {
	ContentType  low.NodeReference[string]
	Payload      low.NodeReference[*yaml.Node]
	Replacements low.NodeReference[[]low.ValueReference[*PayloadReplacement]]
	Extensions   *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode      *yaml.Node
	RootNode     *yaml.Node
	index        *index.SpecIndex
	context      context.Context
	*low.Reference
	low.NodeMap
}

var extractRequestBodyReplacements = extractArray[PayloadReplacement]

// GetIndex returns the index.SpecIndex instance attached to the RequestBody object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (r *RequestBody) GetIndex() *index.SpecIndex {
	return r.index
}

// GetContext returns the context.Context instance used when building the RequestBody object.
func (r *RequestBody) GetContext() context.Context {
	return r.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (r *RequestBody) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, r.Extensions)
}

// GetRootNode returns the root yaml node of the RequestBody object.
func (r *RequestBody) GetRootNode() *yaml.Node {
	return r.RootNode
}

// GetKeyNode returns the key yaml node of the RequestBody object.
func (r *RequestBody) GetKeyNode() *yaml.Node {
	return r.KeyNode
}

// Build will extract all properties of the RequestBody object.
func (r *RequestBody) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &r.KeyNode,
		RootNode:   &r.RootNode,
		Reference:  &r.Reference,
		NodeMap:    &r.NodeMap,
		Extensions: &r.Extensions,
		Index:      &r.index,
		Context:    &r.context,
	}, ctx, keyNode, root, idx)

	r.Payload = extractRawNode(PayloadLabel, root)

	replacements, err := extractRequestBodyReplacements(ctx, ReplacementsLabel, root, idx)
	if err != nil {
		return err
	}
	r.Replacements = replacements
	return nil
}

// GetExtensions returns all RequestBody extensions and satisfies the low.HasExtensions interface.
func (r *RequestBody) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return r.Extensions
}

// Hash will return a consistent hash of the RequestBody object.
func (r *RequestBody) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !r.ContentType.IsEmpty() {
			h.WriteString(r.ContentType.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !r.Payload.IsEmpty() {
			hashYAMLNode(h, r.Payload.Value)
		}
		if !r.Replacements.IsEmpty() {
			for _, rep := range r.Replacements.Value {
				low.HashUint64(h, rep.Value.Hash())
			}
		}
		hashExtensionsInto(h, r.Extensions)
		return h.Sum64()
	})
}

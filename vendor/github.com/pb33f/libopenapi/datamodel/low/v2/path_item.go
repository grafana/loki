// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"context"
	"hash/maphash"
	"sort"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// PathItem represents a low-level Swagger / OpenAPI 2 PathItem object.
//
// Describes the operations available on a single path. A Path Item may be empty, due to ACL constraints.
// The path itself is still exposed to the tooling, but will not know which operations and parameters
// are available.
//
//   - https://swagger.io/specification/v2/#pathItemObject
type PathItem struct {
	Ref        low.NodeReference[string]
	Get        low.NodeReference[*Operation]
	Put        low.NodeReference[*Operation]
	Post       low.NodeReference[*Operation]
	Delete     low.NodeReference[*Operation]
	Options    low.NodeReference[*Operation]
	Head       low.NodeReference[*Operation]
	Patch      low.NodeReference[*Operation]
	Parameters low.NodeReference[[]low.ValueReference[*Parameter]]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
}

// FindExtension will attempt to locate an extension given a name.
func (p *PathItem) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, p.Extensions)
}

// GetExtensions returns all PathItem extensions and satisfies the low.HasExtensions interface.
func (p *PathItem) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return p.Extensions
}

// Build will extract extensions, parameters and operations for all methods. Every method is handled
// asynchronously, in order to keep things moving quickly for complex operations.
func (p *PathItem) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	p.Extensions = low.ExtractExtensions(root)
	skip := false
	var currentNode *yaml.Node

	var wg sync.WaitGroup
	var errors []error

	var ops []low.NodeReference[*Operation]

	// extract parameters
	params, ln, vn, pErr := low.ExtractArray[*Parameter](ctx, ParametersLabel, root, idx)
	if pErr != nil {
		return pErr
	}
	if params != nil {
		p.Parameters = low.NodeReference[[]low.ValueReference[*Parameter]]{
			Value:     params,
			KeyNode:   ln,
			ValueNode: vn,
		}
	}

	for i, pathNode := range root.Content {
		if strings.HasPrefix(strings.ToLower(pathNode.Value), "x-") {
			skip = true
			continue
		}
		// because (for some reason) the spec for swagger docs allows for a '$ref' property for path items.
		// this is kinda nuts, because '$ref' is a reserved keyword for JSON references, which is ALSO used
		// in swagger. Why this choice was made, I do not know.
		if strings.Contains(strings.ToLower(pathNode.Value), "$ref") {
			rn := root.Content[i+1]
			p.Ref = low.NodeReference[string]{
				Value:     rn.Value,
				ValueNode: rn,
				KeyNode:   pathNode,
			}
			skip = true
			continue
		}
		if skip {
			skip = false
			continue
		}
		if i%2 == 0 {
			currentNode = pathNode
			continue
		}

		// the only thing we now care about is handling operations, filter out anything that's not a verb.
		switch currentNode.Value {
		case GetLabel:
		case PostLabel:
		case PutLabel:
		case PatchLabel:
		case DeleteLabel:
		case HeadLabel:
		case OptionsLabel:
		default:
			continue // ignore everything else.
		}

		var op Operation

		wg.Add(1)

		low.BuildModelAsync(pathNode, &op, &wg, &errors)

		opRef := low.NodeReference[*Operation]{
			Value:     &op,
			KeyNode:   currentNode,
			ValueNode: pathNode,
		}

		ops = append(ops, opRef)

		switch currentNode.Value {
		case GetLabel:
			p.Get = opRef
		case PostLabel:
			p.Post = opRef
		case PutLabel:
			p.Put = opRef
		case PatchLabel:
			p.Patch = opRef
		case DeleteLabel:
			p.Delete = opRef
		case HeadLabel:
			p.Head = opRef
		case OptionsLabel:
			p.Options = opRef
		}
	}

	// all operations have been superficially built,
	// now we need to build out the operation, we will do this asynchronously for speed.
	opBuildChan := make(chan struct{})
	opErrorChan := make(chan error)

	buildOpFunc := func(op low.NodeReference[*Operation], ch chan<- struct{}, errCh chan<- error) {
		er := op.Value.Build(ctx, op.KeyNode, op.ValueNode, idx)
		if er != nil {
			errCh <- er
		}
		ch <- struct{}{}
	}

	if len(ops) <= 0 {
		return nil // nothing to do.
	}

	for _, op := range ops {
		go buildOpFunc(op, opBuildChan, opErrorChan)
	}

	n := 0
	total := len(ops)
	for n < total {
		select {
		case buildError := <-opErrorChan:
			return buildError
		case <-opBuildChan:
			n++
		}
	}

	// make sure we don't exit before the path is finished building.
	if len(ops) > 0 {
		wg.Wait()
	}

	return nil
}

// Hash will return a consistent Hash of the PathItem object
func (p *PathItem) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !p.Get.IsEmpty() {
			h.WriteString(GetLabel)
			h.WriteByte('-')
			h.WriteString(low.GenerateHashString(p.Get.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Put.IsEmpty() {
			h.WriteString(PutLabel)
			h.WriteByte('-')
			h.WriteString(low.GenerateHashString(p.Put.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Post.IsEmpty() {
			h.WriteString(PostLabel)
			h.WriteByte('-')
			h.WriteString(low.GenerateHashString(p.Post.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Delete.IsEmpty() {
			h.WriteString(DeleteLabel)
			h.WriteByte('-')
			h.WriteString(low.GenerateHashString(p.Delete.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Options.IsEmpty() {
			h.WriteString(OptionsLabel)
			h.WriteByte('-')
			h.WriteString(low.GenerateHashString(p.Options.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Head.IsEmpty() {
			h.WriteString(HeadLabel)
			h.WriteByte('-')
			h.WriteString(low.GenerateHashString(p.Head.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Patch.IsEmpty() {
			h.WriteString(PatchLabel)
			h.WriteByte('-')
			h.WriteString(low.GenerateHashString(p.Patch.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		keys := make([]string, len(p.Parameters.Value))
		for k := range p.Parameters.Value {
			keys[k] = low.GenerateHashString(p.Parameters.Value[k].Value)
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(p.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

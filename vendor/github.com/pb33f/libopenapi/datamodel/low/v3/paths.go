// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"fmt"
	"hash/maphash"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Paths represents a high-level OpenAPI 3+ Paths object, that is backed by a low-level one.
//
// Holds the relative paths to the individual endpoints and their operations. The path is appended to the URL from the
// Server Object in order to construct the full URL. The Paths MAY be empty, due to Access Control List (ACL)
// constraints.
//   - https://spec.openapis.org/oas/v3.1.0#paths-object
type Paths struct {
	PathItems  *orderedmap.Map[low.KeyReference[string], low.ValueReference[*PathItem]]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the Paths object.
func (p *Paths) GetIndex() *index.SpecIndex {
	return p.index
}

// GetContext returns the context.Context instance used when building the Paths object.
func (p *Paths) GetContext() context.Context {
	return p.context
}

// GetRootNode returns the root yaml node of the Paths object.
func (p *Paths) GetRootNode() *yaml.Node {
	return p.RootNode
}

// GetKeyNode returns the key yaml node of the Paths object.
func (p *Paths) GetKeyNode() *yaml.Node {
	return p.KeyNode
}

// FindPath will attempt to locate a PathItem using the provided path string.
func (p *Paths) FindPath(path string) (result *low.ValueReference[*PathItem]) {
	for pair := orderedmap.First(p.PathItems); pair != nil; pair = pair.Next() {
		if pair.Key().Value == path {
			result = pair.ValuePtr()
			break
		}
	}
	return result
}

// FindPathAndKey attempts to locate a PathItem instance, given a path key.
func (p *Paths) FindPathAndKey(path string) (key *low.KeyReference[string], value *low.ValueReference[*PathItem]) {
	for pair := orderedmap.First(p.PathItems); pair != nil; pair = pair.Next() {
		if pair.Key().Value == path {
			key = pair.KeyPtr()
			value = pair.ValuePtr()
			break
		}
	}
	return key, value
}

// FindExtension will attempt to locate an extension using the specified string.
func (p *Paths) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, p.Extensions)
}

// GetExtensions returns all Paths extensions and satisfies the low.HasExtensions interface.
func (p *Paths) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return p.Extensions
}

// Build will extract extensions and all PathItems. This happens asynchronously for speed.
func (p *Paths) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	p.KeyNode = keyNode
	p.RootNode = root
	utils.CheckForMergeNodes(root)
	p.Reference = new(low.Reference)
	p.Nodes = low.ExtractNodes(ctx, keyNode)
	p.Extensions = low.ExtractExtensions(root)
	p.index = idx
	p.context = ctx

	low.ExtractExtensionNodes(ctx, p.Extensions, p.Nodes)

	pathsMap, err := extractPathItemsMap(ctx, root, idx)
	if err != nil {
		return err
	}

	p.PathItems = pathsMap

	for k, v := range pathsMap.FromOldest() {
		// add path as node to path item, not this path object.
		v.Value.Nodes.Store(k.KeyNode.Line, k.KeyNode)
	}

	return nil
}

// Hash will return a consistent Hash of the Paths object
func (p *Paths) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		for _, hash := range low.AppendMapHashes(nil, p.PathItems) {
			h.WriteString(hash)
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(p.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

func extractPathItemsMap(ctx context.Context, root *yaml.Node, idx *index.SpecIndex) (*orderedmap.Map[low.KeyReference[string], low.ValueReference[*PathItem]], error) {
	// Translate YAML nodes to pathsMap using `TranslatePipeline`.
	type buildResult struct {
		key   low.KeyReference[string]
		value low.ValueReference[*PathItem]
	}
	type buildInput struct {
		currentNode *yaml.Node
		pathNode    *yaml.Node
	}
	pathsMap := orderedmap.New[low.KeyReference[string], low.ValueReference[*PathItem]]()
	in := make(chan buildInput)
	out := make(chan buildResult)
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2) // input and output goroutines.

	// TranslatePipeline input.
	go func() {
		defer func() {
			close(in)
			wg.Done()
		}()
		skip := false
		var currentNode *yaml.Node
		if root != nil {
			for i, pathNode := range root.Content {
				if strings.HasPrefix(strings.ToLower(pathNode.Value), "x-") {
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

				select {
				case in <- buildInput{
					currentNode: currentNode,
					pathNode:    pathNode,
				}:
				case <-done:
					return
				}
			}
		}
	}()

	// TranslatePipeline output.
	go func() {
		for {
			result, ok := <-out
			if !ok {
				break
			}
			pathsMap.Set(result.key, result.value)
		}
		close(done)
		wg.Done()
	}()

	err := datamodel.TranslatePipeline[buildInput, buildResult](in, out,
		func(value buildInput) (buildResult, error) {
			pNode := value.pathNode
			cNode := value.currentNode

			foundContext := ctx
			var isRef bool
			var refNode *yaml.Node
			if ok, _, _ := utils.IsNodeRefValue(pNode); ok {
				isRef = true
				refNode = pNode
				r, _, err, fCtx := low.LocateRefNodeWithContext(ctx, pNode, idx)
				if r != nil {
					pNode = r
					foundContext = fCtx
					if err != nil {
						if !idx.AllowCircularReferenceResolving() {
							return buildResult{}, fmt.Errorf("path item build failed: %s", err.Error())
						}
					}
				} else {
					return buildResult{}, fmt.Errorf("path item build failed: cannot find reference: '%s' at line %d, col %d",
						pNode.Content[1].Value, pNode.Content[1].Line, pNode.Content[1].Column)
				}
			}

			path := new(PathItem)
			_ = low.BuildModel(pNode, path)
			err := path.Build(foundContext, cNode, pNode, idx)

			if isRef {
				path.SetReference(refNode.Content[1].Value, refNode)
			}

			if err != nil {
				if idx != nil && idx.GetLogger() != nil {
					idx.GetLogger().Error(fmt.Sprintf("error building path item: %s", err.Error()))
				}
				// return buildResult{}, err
			}

			return buildResult{
				key: low.KeyReference[string]{
					Value:   cNode.Value,
					KeyNode: cNode,
				},
				value: low.ValueReference[*PathItem]{
					Value:     path,
					ValueNode: pNode,
				},
			}, nil
		},
	)
	wg.Wait()
	if err != nil {
		return nil, err
	}
	return pathsMap, nil
}

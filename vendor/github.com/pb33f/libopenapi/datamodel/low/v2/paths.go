// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"context"
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

// Paths represents a low-level Swagger / OpenAPI Paths object.
type Paths struct {
	PathItems  *orderedmap.Map[low.KeyReference[string], low.ValueReference[*PathItem]]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
}

// GetExtensions returns all Paths extensions and satisfies the low.HasExtensions interface.
func (p *Paths) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return p.Extensions
}

// FindPath attempts to locate a PathItem instance, given a path key.
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

// FindExtension will attempt to locate an extension value given a name.
func (p *Paths) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, p.Extensions)
}

// Build will extract extensions and paths from node.
func (p *Paths) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	p.Extensions = low.ExtractExtensions(root)

	// Translate YAML nodes to pathsMap using `TranslatePipeline`.
	type pathBuildResult struct {
		key   low.KeyReference[string]
		value low.ValueReference[*PathItem]
	}
	type buildInput struct {
		currentNode *yaml.Node
		pathNode    *yaml.Node
	}
	pathsMap := orderedmap.New[low.KeyReference[string], low.ValueReference[*PathItem]]()
	in := make(chan buildInput)
	out := make(chan pathBuildResult)
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

	translateFunc := func(value buildInput) (pathBuildResult, error) {
		pNode := value.pathNode
		cNode := value.currentNode
		path := new(PathItem)
		_ = low.BuildModel(pNode, path)
		err := path.Build(ctx, cNode, pNode, idx)
		if err != nil {
			return pathBuildResult{}, err
		}
		return pathBuildResult{
			key: low.KeyReference[string]{
				Value:   cNode.Value,
				KeyNode: cNode,
			},
			value: low.ValueReference[*PathItem]{
				Value:     path,
				ValueNode: pNode,
			},
		}, nil
	}
	err := datamodel.TranslatePipeline[buildInput, pathBuildResult](in, out, translateFunc)
	wg.Wait()
	if err != nil {
		return err
	}

	p.PathItems = pathsMap
	return nil
}

// Hash will return a consistent Hash of the Paths object
func (p *Paths) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		for v := range orderedmap.SortAlpha(p.PathItems).ValuesFromOldest() {
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

// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"context"
	"fmt"
	"hash/maphash"
	"strconv"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// FailureAction represents a low-level Arazzo Failure Action Object.
// A failure action can be a full definition or a Reusable Object with a $components reference.
// https://spec.openapis.org/arazzo/v1.0.1#failure-action-object
type FailureAction struct {
	Name       low.NodeReference[string]
	Type       low.NodeReference[string]
	WorkflowId low.NodeReference[string]
	StepId     low.NodeReference[string]
	RetryAfter low.NodeReference[float64]
	RetryLimit low.NodeReference[int64]
	Criteria   low.NodeReference[[]low.ValueReference[*Criterion]]
	ComponentRef low.NodeReference[string]
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode    *yaml.Node
	RootNode   *yaml.Node
	index      *index.SpecIndex
	context    context.Context
	*low.Reference
	low.NodeMap
}

var extractFailureActionCriteria = extractArray[Criterion]

// IsReusable returns true if this failure action is a Reusable Object (has a reference field).
func (f *FailureAction) IsReusable() bool {
	return !f.ComponentRef.IsEmpty()
}

// GetIndex returns the index.SpecIndex instance attached to the FailureAction object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (f *FailureAction) GetIndex() *index.SpecIndex {
	return f.index
}

// GetContext returns the context.Context instance used when building the FailureAction object.
func (f *FailureAction) GetContext() context.Context {
	return f.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (f *FailureAction) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, f.Extensions)
}

// GetRootNode returns the root yaml node of the FailureAction object.
func (f *FailureAction) GetRootNode() *yaml.Node {
	return f.RootNode
}

// GetKeyNode returns the key yaml node of the FailureAction object.
func (f *FailureAction) GetKeyNode() *yaml.Node {
	return f.KeyNode
}

// Build will extract all properties of the FailureAction object.
func (f *FailureAction) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &f.KeyNode,
		RootNode:   &f.RootNode,
		Reference:  &f.Reference,
		NodeMap:    &f.NodeMap,
		Extensions: &f.Extensions,
		Index:      &f.index,
		Context:    &f.context,
	}, ctx, keyNode, root, idx)

	f.ComponentRef = extractComponentRef(ReferenceLabel, root)

	// Extract numeric fields (retryAfter, retryLimit) which need special parsing
	for i := 0; i < len(root.Content); i += 2 {
		if i+1 >= len(root.Content) {
			break
		}
		k := root.Content[i]
		v := root.Content[i+1]
		switch k.Value {
		case RetryAfterLabel:
			val, err := strconv.ParseFloat(v.Value, 64)
			if err != nil {
				return fmt.Errorf("invalid retryAfter value %q: %w", v.Value, err)
			}
			f.RetryAfter = low.NodeReference[float64]{
				Value:     val,
				KeyNode:   k,
				ValueNode: v,
			}
		case RetryLimitLabel:
			val, err := strconv.ParseInt(v.Value, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid retryLimit value %q: %w", v.Value, err)
			}
			f.RetryLimit = low.NodeReference[int64]{
				Value:     val,
				KeyNode:   k,
				ValueNode: v,
			}
		}
	}

	// Extract criteria array
	criteria, err := extractFailureActionCriteria(ctx, CriteriaLabel, root, idx)
	if err != nil {
		return err
	}
	f.Criteria = criteria
	return nil
}

// GetExtensions returns all FailureAction extensions and satisfies the low.HasExtensions interface.
func (f *FailureAction) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return f.Extensions
}

// Hash will return a consistent hash of the FailureAction object.
func (f *FailureAction) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !f.ComponentRef.IsEmpty() {
			h.WriteString(f.ComponentRef.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !f.Name.IsEmpty() {
			h.WriteString(f.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !f.Type.IsEmpty() {
			h.WriteString(f.Type.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !f.WorkflowId.IsEmpty() {
			h.WriteString(f.WorkflowId.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !f.StepId.IsEmpty() {
			h.WriteString(f.StepId.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !f.RetryAfter.IsEmpty() {
			h.WriteString(strconv.FormatFloat(f.RetryAfter.Value, 'f', -1, 64))
			h.WriteByte(low.HASH_PIPE)
		}
		if !f.RetryLimit.IsEmpty() {
			low.HashInt64(h, f.RetryLimit.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !f.Criteria.IsEmpty() {
			for _, c := range f.Criteria.Value {
				low.HashUint64(h, c.Value.Hash())
			}
		}
		hashExtensionsInto(h, f.Extensions)
		return h.Sum64()
	})
}

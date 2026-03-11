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

// Workflow represents a low-level Arazzo Workflow Object.
// https://spec.openapis.org/arazzo/v1.0.1#workflow-object
type Workflow struct {
	WorkflowId     low.NodeReference[string]
	Summary        low.NodeReference[string]
	Description    low.NodeReference[string]
	Inputs         low.NodeReference[*yaml.Node]
	DependsOn      low.NodeReference[[]low.ValueReference[string]]
	Steps          low.NodeReference[[]low.ValueReference[*Step]]
	SuccessActions low.NodeReference[[]low.ValueReference[*SuccessAction]]
	FailureActions low.NodeReference[[]low.ValueReference[*FailureAction]]
	Outputs        low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[string]]]
	Parameters     low.NodeReference[[]low.ValueReference[*Parameter]]
	Extensions     *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode        *yaml.Node
	RootNode       *yaml.Node
	index          *index.SpecIndex
	context        context.Context
	*low.Reference
	low.NodeMap
}

var extractWorkflowSuccessActions = extractArray[SuccessAction]
var extractWorkflowParameters = extractArray[Parameter]

// GetIndex returns the index.SpecIndex instance attached to the Workflow object.
// For Arazzo low models this is typically nil, because Arazzo parsing does not build a SpecIndex.
// The index parameter is still required to satisfy the shared low.Buildable interface and generic extractors.
func (w *Workflow) GetIndex() *index.SpecIndex {
	return w.index
}

// GetContext returns the context.Context instance used when building the Workflow object.
func (w *Workflow) GetContext() context.Context {
	return w.context
}

// FindExtension returns a ValueReference containing the extension value, if found.
func (w *Workflow) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, w.Extensions)
}

// GetRootNode returns the root yaml node of the Workflow object.
func (w *Workflow) GetRootNode() *yaml.Node {
	return w.RootNode
}

// GetKeyNode returns the key yaml node of the Workflow object.
func (w *Workflow) GetKeyNode() *yaml.Node {
	return w.KeyNode
}

// Build will extract all properties of the Workflow object.
func (w *Workflow) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
	root = initBuild(&arazzoBase{
		KeyNode:    &w.KeyNode,
		RootNode:   &w.RootNode,
		Reference:  &w.Reference,
		NodeMap:    &w.NodeMap,
		Extensions: &w.Extensions,
		Index:      &w.index,
		Context:    &w.context,
	}, ctx, keyNode, root, idx)

	w.Inputs = extractRawNode(InputsLabel, root) // raw node: JSON Schema
	w.DependsOn = extractStringArray(DependsOnLabel, root)

	steps, err := extractArray[Step](ctx, StepsLabel, root, idx)
	if err != nil {
		return err
	}
	w.Steps = steps

	successActions, err := extractWorkflowSuccessActions(ctx, SuccessActionsLabel, root, idx)
	if err != nil {
		return err
	}
	w.SuccessActions = successActions

	failureActions, err := extractArray[FailureAction](ctx, FailureActionsLabel, root, idx)
	if err != nil {
		return err
	}
	w.FailureActions = failureActions

	w.Outputs = extractExpressionsMap(OutputsLabel, root)

	params, err := extractWorkflowParameters(ctx, ParametersLabel, root, idx)
	if err != nil {
		return err
	}
	w.Parameters = params

	return nil
}

// GetExtensions returns all Workflow extensions and satisfies the low.HasExtensions interface.
func (w *Workflow) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return w.Extensions
}

// Hash will return a consistent hash of the Workflow object.
func (w *Workflow) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !w.WorkflowId.IsEmpty() {
			h.WriteString(w.WorkflowId.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !w.Summary.IsEmpty() {
			h.WriteString(w.Summary.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !w.Description.IsEmpty() {
			h.WriteString(w.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !w.Inputs.IsEmpty() {
			hashYAMLNode(h, w.Inputs.Value)
		}
		if !w.DependsOn.IsEmpty() {
			for _, d := range w.DependsOn.Value {
				h.WriteString(d.Value)
				h.WriteByte(low.HASH_PIPE)
			}
		}
		if !w.Steps.IsEmpty() {
			for _, s := range w.Steps.Value {
				low.HashUint64(h, s.Value.Hash())
			}
		}
		if !w.SuccessActions.IsEmpty() {
			for _, a := range w.SuccessActions.Value {
				low.HashUint64(h, a.Value.Hash())
			}
		}
		if !w.FailureActions.IsEmpty() {
			for _, a := range w.FailureActions.Value {
				low.HashUint64(h, a.Value.Hash())
			}
		}
		if !w.Outputs.IsEmpty() {
			for pair := w.Outputs.Value.First(); pair != nil; pair = pair.Next() {
				h.WriteString(pair.Key().Value)
				h.WriteByte(low.HASH_PIPE)
				h.WriteString(pair.Value().Value)
				h.WriteByte(low.HASH_PIPE)
			}
		}
		if !w.Parameters.IsEmpty() {
			for _, p := range w.Parameters.Value {
				low.HashUint64(h, p.Value.Hash())
			}
		}
		hashExtensionsInto(h, w.Extensions)
		return h.Sum64()
	})
}

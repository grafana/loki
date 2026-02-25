// Copyright 2022-2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"fmt"
	"hash/maphash"
	"sort"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// PathItem represents a low-level OpenAPI 3+ PathItem object.
//
// Describes the operations available on a single path. A Path Item MAY be empty, due to ACL constraints.
// The path itself is still exposed to the documentation viewer, but they will not know which operations and parameters
// are available.
//   - https://spec.openapis.org/oas/v3.1.0#path-item-object
type PathItem struct {
	Description          low.NodeReference[string]
	Summary              low.NodeReference[string]
	Get                  low.NodeReference[*Operation]
	Put                  low.NodeReference[*Operation]
	Post                 low.NodeReference[*Operation]
	Delete               low.NodeReference[*Operation]
	Options              low.NodeReference[*Operation]
	Head                 low.NodeReference[*Operation]
	Patch                low.NodeReference[*Operation]
	Trace                low.NodeReference[*Operation]
	Query                low.NodeReference[*Operation]
	AdditionalOperations low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.NodeReference[*Operation]]] // OpenAPI 3.2+ additional operations
	Servers              low.NodeReference[[]low.ValueReference[*Server]]
	Parameters           low.NodeReference[[]low.ValueReference[*Parameter]]
	Extensions           *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode              *yaml.Node
	RootNode             *yaml.Node
	index                *index.SpecIndex
	context              context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex returns the index.SpecIndex instance attached to the PathItem object.
func (p *PathItem) GetIndex() *index.SpecIndex {
	return p.index
}

// GetContext returns the context.Context instance used when building the PathItem object.
func (p *PathItem) GetContext() context.Context {
	return p.context
}

// Hash will return a consistent Hash of the PathItem object
func (p *PathItem) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !p.Description.IsEmpty() {
			h.WriteString(p.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Summary.IsEmpty() {
			h.WriteString(p.Summary.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Get.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", GetLabel, low.GenerateHashString(p.Get.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Put.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", PutLabel, low.GenerateHashString(p.Put.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Post.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", PostLabel, low.GenerateHashString(p.Post.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Delete.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", DeleteLabel, low.GenerateHashString(p.Delete.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Options.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", OptionsLabel, low.GenerateHashString(p.Options.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Head.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", HeadLabel, low.GenerateHashString(p.Head.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Patch.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", PatchLabel, low.GenerateHashString(p.Patch.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Trace.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", TraceLabel, low.GenerateHashString(p.Trace.Value)))
			h.WriteByte(low.HASH_PIPE)
		}
		if !p.Query.IsEmpty() {
			h.WriteString(fmt.Sprintf("%s-%s", QueryLabel, low.GenerateHashString(p.Query.Value)))
			h.WriteByte(low.HASH_PIPE)
		}

		// Process AdditionalOperations with pre-allocation and sorting
		if p.AdditionalOperations.Value != nil && p.AdditionalOperations.Value.Len() > 0 {
			keys := make([]string, 0, p.AdditionalOperations.Value.Len())
			for k, v := range p.AdditionalOperations.Value.FromOldest() {
				keys = append(keys, fmt.Sprintf("%s-%s", k.Value, low.GenerateHashString(v.Value)))
			}
			sort.Strings(keys)
			for _, key := range keys {
				h.WriteString(key)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		// Process Parameters with pre-allocation and sorting
		if len(p.Parameters.Value) > 0 {
			keys := make([]string, len(p.Parameters.Value))
			for k := range p.Parameters.Value {
				keys[k] = low.GenerateHashString(p.Parameters.Value[k].Value)
			}
			sort.Strings(keys)
			for _, key := range keys {
				h.WriteString(key)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		// Process Servers with pre-allocation and sorting
		if len(p.Servers.Value) > 0 {
			keys := make([]string, len(p.Servers.Value))
			for k := range p.Servers.Value {
				keys[k] = low.GenerateHashString(p.Servers.Value[k].Value)
			}
			sort.Strings(keys)
			for _, key := range keys {
				h.WriteString(key)
				h.WriteByte(low.HASH_PIPE)
			}
		}

		for _, ext := range low.HashExtensions(p.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

// GetRootNode returns the root yaml node of the PathItem object
func (p *PathItem) GetRootNode() *yaml.Node {
	return p.RootNode
}

// GetKeyNode returns the key yaml node of the PathItem object
func (p *PathItem) GetKeyNode() *yaml.Node {
	return p.KeyNode
}

// FindExtension attempts to find an extension
func (p *PathItem) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, p.Extensions)
}

// GetExtensions returns all PathItem extensions and satisfies the low.HasExtensions interface.
func (p *PathItem) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return p.Extensions
}

// Build extracts extensions, parameters, servers and each http method defined.
// everything is extracted asynchronously for speed.
func (p *PathItem) Build(ctx context.Context, keyNode, root *yaml.Node, idx *index.SpecIndex) error {
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
	skip := false
	var currentNode *yaml.Node

	var wg sync.WaitGroup
	var errors []error
	var ops []low.NodeReference[*Operation]
	var additionalOps *orderedmap.Map[low.KeyReference[string], low.NodeReference[*Operation]]

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
		p.Nodes.Store(ln.Line, ln)
	}

	_, ln, vn = utils.FindKeyNodeFullTop(ServersLabel, root.Content)
	if vn != nil {
		if utils.IsNodeArray(vn) {
			var servers []low.ValueReference[*Server]
			for _, srvN := range vn.Content {
				if utils.IsNodeMap(srvN) {
					srvr := new(Server)
					_ = low.BuildModel(srvN, srvr)
					srvr.Build(ctx, ln, srvN, idx)
					servers = append(servers, low.ValueReference[*Server]{
						Value:     srvr,
						ValueNode: srvN,
					})
				}
			}
			p.Servers = low.NodeReference[[]low.ValueReference[*Server]]{
				Value:     servers,
				KeyNode:   ln,
				ValueNode: vn,
			}
			p.Nodes.Store(ln.Line, ln)
		}
	}
	prevExt := false
	for i, pathNode := range root.Content {
		if strings.HasPrefix(strings.ToLower(pathNode.Value), "x-") {
			skip = true
			prevExt = true
			continue
		}
		// https://github.com/pb33f/libopenapi/issues/388
		// in the case where a user has an extension with the value 'parameters', make sure we handle
		// it correctly, by not skipping.
		if strings.HasPrefix(strings.ToLower(pathNode.Value), "parameters") {
			if !prevExt { // this
				skip = true
				continue
			} else {
				prevExt = false
			}
		}
		if skip {
			skip = false
			continue
		}
		if i%2 == 0 {
			currentNode = pathNode
			continue
		}

		// check if this is an operation (either standard or additional)
		isStandardOp := false
		isAdditionalOp := false

		switch currentNode.Value {
		case GetLabel, PostLabel, PutLabel, PatchLabel, DeleteLabel, HeadLabel, OptionsLabel, TraceLabel, QueryLabel:
			isStandardOp = true
		default:
			// check if this looks like an HTTP method (and isn't a known non-operation field)
			switch currentNode.Value {
			case ParametersLabel, ServersLabel, SummaryLabel, DescriptionLabel:
				continue // ignore known non-operation fields
			default:
				// assume it's an additional operation if it contains a mapping to an operation object
				if utils.IsNodeMap(pathNode) {
					isAdditionalOp = true
				} else {
					continue // ignore if not a map
				}
			}
		}

		foundContext, pathNode, opIsRef, opRefVal, opRefNode, err := resolveOperationReference(ctx, pathNode, idx)
		if err != nil {
			return err
		}
		var op Operation
		wg.Add(1)
		low.BuildModelAsync(pathNode, &op, &wg, &errors)

		opRef := low.NodeReference[*Operation]{
			Value:     &op,
			KeyNode:   currentNode,
			ValueNode: pathNode,
			Context:   foundContext,
		}
		if opIsRef {
			opRef.SetReference(opRefVal, opRefNode)
		}

		ops = append(ops, opRef)

		if isStandardOp {
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
			case TraceLabel:
				p.Trace = opRef
			case QueryLabel:
				p.Query = opRef
			}
		} else if isAdditionalOp {
			// initialize additionalOps map if this is the first additional operation
			if additionalOps == nil {
				additionalOps = orderedmap.New[low.KeyReference[string], low.NodeReference[*Operation]]()
			}

			// now we need to determine if these are inline additional operations, or just plonked into the root.
			if currentNode.Value == AdditionalOperationsLabel {

				for j := 0; j < len(pathNode.Content); j += 2 {
					opKeyNode := pathNode.Content[j]
					opValueNode := pathNode.Content[j+1]

					// resolve operation reference for each additional operation
					foundContext, opValueNode, opIsRef, opRefVal, opRefNode, err = resolveOperationReference(ctx, opValueNode, idx)
					if err != nil {
						return err
					}
					var addOp Operation
					wg.Add(1)
					low.BuildModelAsync(opValueNode, &addOp, &wg, &errors)

					addOpRef := low.NodeReference[*Operation]{
						Value:     &addOp,
						KeyNode:   opKeyNode,
						ValueNode: opValueNode,
						Context:   foundContext,
					}
					if opIsRef {
						addOpRef.SetReference(opRefVal, opRefNode)
					}

					additionalOps.Set(low.KeyReference[string]{
						KeyNode: opKeyNode,
						Value:   opKeyNode.Value,
					}, addOpRef)
				}
			} else {

				kv := pathNode.Value
				if kv == "" {
					kv = currentNode.Value
				}

				additionalOps.Set(low.KeyReference[string]{
					KeyNode: currentNode,
					Value:   kv,
				}, opRef)

			}
		}
	}

	// all operations have been superficially built,
	// now we need to build out the operation, we will do this asynchronously for speed.
	translateFunc := func(_ int, op low.NodeReference[*Operation]) (any, error) {
		ref := ""
		var refNode *yaml.Node
		if op.IsReference() {
			ref = op.GetReference()
			refNode = op.GetReferenceNode()
		}

		err := op.Value.Build(op.Context, op.KeyNode, op.ValueNode, op.Context.Value(index.FoundIndexKey).(*index.SpecIndex))
		if ref != "" {
			op.Value.Reference.SetReference(ref, refNode)
		}
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	err := datamodel.TranslateSliceParallel[low.NodeReference[*Operation], any](ops, translateFunc, nil)
	if err != nil {
		return err
	}

	// assign additionalOperations if any were found
	if additionalOps != nil && additionalOps.Len() > 0 {
		var extrOps []low.NodeReference[*Operation]
		// build out each additional operation
		for _, appVal := range additionalOps.FromOldest() {
			extrOps = append(extrOps, appVal)
		}

		err = datamodel.TranslateSliceParallel[low.NodeReference[*Operation], any](extrOps, translateFunc, nil)

		p.AdditionalOperations = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.NodeReference[*Operation]]]{
			Value: additionalOps,
		}
	}
	return nil
}

// resolveOperationReference handles the resolution of operation references ($ref)
// Returns: foundContext, resolvedPathNode, isRef, refValue, refNode, error
func resolveOperationReference(ctx context.Context, pathNode *yaml.Node, idx *index.SpecIndex) (
	context.Context, *yaml.Node, bool, string, *yaml.Node, error) {

	foundContext := ctx
	opIsRef := false
	var opRefVal string
	var opRefNode *yaml.Node

	if ok, _, ref := utils.IsNodeRefValue(pathNode); ok {
		// According to OpenAPI spec the only valid $ref for paths is
		// reference for the whole pathItem. Unfortunately, the internet is full of invalid specs
		// even from trusted companies like DigitalOcean where they tend to
		// use file $ref for each respective operation:
		// /endpoint/call/name:
		//   post:
		//     $ref: 'file.yaml'
		// Check if that is the case and resolve such thing properly too.

		opIsRef = true
		opRefVal = ref
		opRefNode = pathNode
		r, newIdx, err, nCtx := low.LocateRefNodeWithContext(ctx, pathNode, idx)
		if r != nil {
			if r.Kind == yaml.DocumentNode {
				r = r.Content[0]
			}
			pathNode = r
			foundContext = nCtx
			foundContext = context.WithValue(foundContext, index.FoundIndexKey, newIdx)

			if r.Tag == "" {
				// If it's a node from file, tag is empty
				pathNode = r.Content[0]
			}

			if err != nil {
				if !idx.AllowCircularReferenceResolving() {
					return nil, nil, false, "", nil, fmt.Errorf("build schema failed: %s", err.Error())
				}
			}
		} else {
			return nil, nil, false, "", nil, fmt.Errorf("path item build failed: cannot find reference: %s at line %d, col %d",
				pathNode.Content[1].Value, pathNode.Content[1].Line, pathNode.Content[1].Column)
		}
	} else {
		foundContext = context.WithValue(foundContext, index.FoundIndexKey, idx)
	}

	return foundContext, pathNode, opIsRef, opRefVal, opRefNode, nil
}

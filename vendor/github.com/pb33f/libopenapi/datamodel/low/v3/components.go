// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"fmt"
	"hash/maphash"
	"reflect"
	"sync"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Components represents a low-level OpenAPI 3+ Components Object, that is backed by a low-level one.
//
// Holds a set of reusable objects for different aspects of the OAS. All objects defined within the components object
// will have no effect on the API unless they are explicitly referenced from properties outside the components object.
//   - https://spec.openapis.org/oas/v3.1.0#components-object
type Components struct {
	Schemas         low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.SchemaProxy]]]
	Responses       low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Response]]]
	Parameters      low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Parameter]]]
	Examples        low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.Example]]]
	RequestBodies   low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*RequestBody]]]
	Headers         low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Header]]]
	SecuritySchemes low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SecurityScheme]]]
	Links           low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Link]]]
	Callbacks       low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*Callback]]]
	PathItems       low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*PathItem]]]
	MediaTypes      low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*MediaType]]] // OpenAPI 3.2+ mediaTypes section
	Extensions      *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	KeyNode         *yaml.Node
	RootNode        *yaml.Node
	index           *index.SpecIndex
	context         context.Context
	*low.Reference
	low.NodeMap
}

type componentBuildResult[T any] struct {
	key   low.KeyReference[string]
	value low.ValueReference[T]
}

type componentInput struct {
	node         *yaml.Node
	currentLabel *yaml.Node
}

// GetIndex returns the index.SpecIndex instance attached to the Components object
func (co *Components) GetIndex() *index.SpecIndex {
	return co.index
}

// GetContext returns the context.Context instance used when building the Components object
func (co *Components) GetContext() context.Context {
	return co.context
}

// GetExtensions returns all Components extensions and satisfies the low.HasExtensions interface.
func (co *Components) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return co.Extensions
}

// GetRootNode returns the root yaml node of the Components object
func (co *Components) GetRootNode() *yaml.Node {
	return co.RootNode
}

// GetKeyNode returns the key yaml node of the Components object
func (co *Components) GetKeyNode() *yaml.Node {
	return co.KeyNode
}

// Hash will return a consistent Hash of the Components object
func (co *Components) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		generateHashForObjectMap(co.Schemas.Value, h)
		generateHashForObjectMap(co.Responses.Value, h)
		generateHashForObjectMap(co.Parameters.Value, h)
		generateHashForObjectMap(co.Examples.Value, h)
		generateHashForObjectMap(co.RequestBodies.Value, h)
		generateHashForObjectMap(co.Headers.Value, h)
		generateHashForObjectMap(co.SecuritySchemes.Value, h)
		generateHashForObjectMap(co.Links.Value, h)
		generateHashForObjectMap(co.Callbacks.Value, h)
		generateHashForObjectMap(co.PathItems.Value, h)
		generateHashForObjectMap(co.MediaTypes.Value, h)
		for _, ext := range low.HashExtensions(co.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

func generateHashForObjectMap[T any](collection *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]], h *maphash.Hash) {
	for v := range orderedmap.SortAlpha(collection).ValuesFromOldest() {
		h.WriteString(low.GenerateHashString(v.Value))
		h.WriteByte(low.HASH_PIPE)
	}
}

// FindExtension attempts to locate an extension with the supplied key
func (co *Components) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, co.Extensions)
}

// FindSchema attempts to locate a SchemaProxy from 'schemas' with a specific name
func (co *Components) FindSchema(schema string) *low.ValueReference[*base.SchemaProxy] {
	return low.FindItemInOrderedMap[*base.SchemaProxy](schema, co.Schemas.Value)
}

// FindResponse attempts to locate a Response from 'responses' with a specific name
func (co *Components) FindResponse(response string) *low.ValueReference[*Response] {
	return low.FindItemInOrderedMap[*Response](response, co.Responses.Value)
}

// FindParameter attempts to locate a Parameter from 'parameters' with a specific name
func (co *Components) FindParameter(response string) *low.ValueReference[*Parameter] {
	return low.FindItemInOrderedMap[*Parameter](response, co.Parameters.Value)
}

// FindSecurityScheme attempts to locate a SecurityScheme from 'securitySchemes' with a specific name
func (co *Components) FindSecurityScheme(sScheme string) *low.ValueReference[*SecurityScheme] {
	return low.FindItemInOrderedMap[*SecurityScheme](sScheme, co.SecuritySchemes.Value)
}

// FindExample attempts tp
func (co *Components) FindExample(example string) *low.ValueReference[*base.Example] {
	return low.FindItemInOrderedMap[*base.Example](example, co.Examples.Value)
}

func (co *Components) FindRequestBody(requestBody string) *low.ValueReference[*RequestBody] {
	return low.FindItemInOrderedMap[*RequestBody](requestBody, co.RequestBodies.Value)
}

func (co *Components) FindHeader(header string) *low.ValueReference[*Header] {
	return low.FindItemInOrderedMap[*Header](header, co.Headers.Value)
}

func (co *Components) FindLink(link string) *low.ValueReference[*Link] {
	return low.FindItemInOrderedMap[*Link](link, co.Links.Value)
}

func (co *Components) FindPathItem(path string) *low.ValueReference[*PathItem] {
	return low.FindItemInOrderedMap[*PathItem](path, co.PathItems.Value)
}

func (co *Components) FindCallback(callback string) *low.ValueReference[*Callback] {
	return low.FindItemInOrderedMap[*Callback](callback, co.Callbacks.Value)
}

// FindMediaType attempts to locate a MediaType from 'mediaTypes' with a specific name
func (co *Components) FindMediaType(mediaType string) *low.ValueReference[*MediaType] {
	return low.FindItemInOrderedMap[*MediaType](mediaType, co.MediaTypes.Value)
}

// Build converts root YAML node containing components to low level model.
// Process each component in parallel.
func (co *Components) Build(ctx context.Context, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	co.Reference = new(low.Reference)
	co.Nodes = low.ExtractNodes(ctx, root)
	co.Extensions = low.ExtractExtensions(root)
	low.ExtractExtensionNodes(ctx, co.Extensions, co.Nodes)
	co.RootNode = root
	co.KeyNode = root
	co.index = idx
	co.context = ctx

	var reterr error
	var ceMutex sync.Mutex
	var wg sync.WaitGroup
	wg.Add(11)

	captureError := func(err error) {
		ceMutex.Lock()
		defer ceMutex.Unlock()
		if err != nil {
			reterr = err
		}
	}

	go func() {
		schemas, err := extractComponentValues[*base.SchemaProxy](ctx, SchemasLabel, root, idx, co)
		captureError(err)
		co.Schemas = schemas
		wg.Done()
	}()
	go func() {
		parameters, err := extractComponentValues[*Parameter](ctx, ParametersLabel, root, idx, co)
		captureError(err)
		co.Parameters = parameters
		wg.Done()
	}()
	go func() {
		responses, err := extractComponentValues[*Response](ctx, ResponsesLabel, root, idx, co)
		captureError(err)
		co.Responses = responses
		wg.Done()
	}()
	go func() {
		examples, err := extractComponentValues[*base.Example](ctx, base.ExamplesLabel, root, idx, co)
		captureError(err)
		co.Examples = examples
		wg.Done()
	}()
	go func() {
		requestBodies, err := extractComponentValues[*RequestBody](ctx, RequestBodiesLabel, root, idx, co)
		captureError(err)
		co.RequestBodies = requestBodies
		wg.Done()
	}()
	go func() {
		headers, err := extractComponentValues[*Header](ctx, HeadersLabel, root, idx, co)
		captureError(err)
		co.Headers = headers
		wg.Done()
	}()
	go func() {
		securitySchemes, err := extractComponentValues[*SecurityScheme](ctx, SecuritySchemesLabel, root, idx, co)
		captureError(err)
		co.SecuritySchemes = securitySchemes
		wg.Done()
	}()
	go func() {
		links, err := extractComponentValues[*Link](ctx, LinksLabel, root, idx, co)
		captureError(err)
		co.Links = links
		wg.Done()
	}()
	go func() {
		callbacks, err := extractComponentValues[*Callback](ctx, CallbacksLabel, root, idx, co)
		captureError(err)
		co.Callbacks = callbacks
		wg.Done()
	}()
	go func() {
		pathItems, err := extractComponentValues[*PathItem](ctx, PathItemsLabel, root, idx, co)
		captureError(err)
		co.PathItems = pathItems
		wg.Done()
	}()
	go func() {
		mediaTypes, err := extractComponentValues[*MediaType](ctx, MediaTypesLabel, root, idx, co)
		captureError(err)
		co.MediaTypes = mediaTypes
		wg.Done()
	}()

	wg.Wait()
	return reterr
}

// extractComponentValues converts all the YAML nodes of a component type to
// low level model.
// Process each node in parallel.
func extractComponentValues[T low.Buildable[N], N any](ctx context.Context, label string, root *yaml.Node, idx *index.SpecIndex, co *Components) (low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[T]]], error) {
	var emptyResult low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[T]]]
	_, nodeLabel, nodeValue := utils.FindKeyNodeFullTop(label, root.Content)
	if nodeValue == nil {
		return emptyResult, nil
	}
	co.Nodes.Store(nodeLabel.Line, nodeLabel)
	componentValues := orderedmap.New[low.KeyReference[string], low.ValueReference[T]]()
	if utils.IsNodeArray(nodeValue) {
		return emptyResult, fmt.Errorf("node is array, cannot be used in components: line %d, column %d", nodeValue.Line, nodeValue.Column)
	}

	in := make(chan componentInput)
	out := make(chan componentBuildResult[T])
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2) // input and output goroutines.

	// Send input.
	go func() {
		defer func() {
			close(in)
			wg.Done()
		}()
		var currentLabel *yaml.Node
		for i, node := range nodeValue.Content {
			// always ignore extensions
			if i%2 == 0 {
				currentLabel = node
				continue
			}

			select {
			case in <- componentInput{
				node:         node,
				currentLabel: currentLabel,
			}:
			case <-done:
				return
			}
		}
	}()

	// Collect output.
	go func() {
		for result := range out {
			componentValues.Set(result.key, result.value)
		}
		close(done)
		wg.Done()
	}()

	// Translate.
	translateFunc := func(value componentInput) (componentBuildResult[T], error) {
		var n T = new(N)
		currentLabel := value.currentLabel
		node := value.node

		// build.
		_ = low.BuildModel(node, n)
		err := n.Build(ctx, currentLabel, node, idx)
		if err != nil {
			return componentBuildResult[T]{}, err
		}

		nType := reflect.TypeOf(n)
		nValue := reflect.ValueOf(n)

		// for SchemaProxy, use the transformed node from sp.vn instead of original node
		finalValueNode := node
		if valueNodeGetter, ok := nValue.Interface().(low.HasValueNodeUntyped); ok {
			if transformedNode := valueNodeGetter.GetValueNode(); transformedNode != nil {
				finalValueNode = transformedNode
			}
		}

		// Check if the type implements low.HasKeyNode
		hasKeyNodeType := reflect.TypeOf((*low.HasKeyNode)(nil)).Elem()
		if nType.Implements(hasKeyNodeType) {
			r := nValue.Interface()
			if h, ok := r.(low.HasKeyNode); ok {
				if k, ko := r.(low.AddNodes); ko {
					k.AddNode(h.GetKeyNode().Line, h.GetKeyNode())
				}
			}

		}
		return componentBuildResult[T]{
			key: low.KeyReference[string]{
				KeyNode: currentLabel,
				Value:   currentLabel.Value,
			},
			value: low.ValueReference[T]{
				Value:     n,
				ValueNode: finalValueNode, // use transformed node if available
			},
		}, nil
	}
	err := datamodel.TranslatePipeline[componentInput, componentBuildResult[T]](in, out, translateFunc)
	wg.Wait()
	if err != nil {
		return emptyResult, err
	}

	results := low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[T]]]{
		KeyNode:   nodeLabel,
		ValueNode: nodeValue,
		Value:     componentValues,
	}
	return results, nil
}

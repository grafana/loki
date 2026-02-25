// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"log/slog"
	"sync"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// SchemaProxy exists as a stub that will create a Schema once (and only once) the Schema() method is called.
//
// Why use a Proxy design?
//
// There are three reasons.
//
// 1. Circular References and Endless Loops.
//
// JSON Schema allows for references to be used. This means references can loop around and create infinite recursive
// structures, These 'Circular references' technically mean a schema can NEVER be resolved, not without breaking the
// loop somewhere along the chain.
//
// Polymorphism in the form of 'oneOf' and 'anyOf' in version 3+ only exacerbates the problem.
//
// These circular traps can be discovered using the resolver, however it's still not enough to stop endless loops and
// endless goroutine spawning. A proxy design means that resolving occurs on demand and runs down a single level only.
// preventing any run-away loops.
//
// 2. Performance
//
// Even without circular references, Polymorphism creates large additional resolving chains that take a long time
// and slow things down when building. By preventing recursion through every polymorphic item, building models is kept
// fast and snappy, which is desired for realtime processing of specs.
//
//   - Q: Yeah, but, why not just use state to avoiding re-visiting seen polymorphic nodes?
//   - A: It's slow, takes up memory and still has runaway potential in very, very long chains.
//
// 3. Short Circuit Errors.
//
// Schemas are where things can get messy, mainly because the Schema standard changes between versions, and
// it's not actually JSONSchema until 3.1, so lots of times a bad schema will break parsing. Errors are only found
// when a schema is needed, so the rest of the document is parsed and ready to use.
type SchemaProxy struct {
	low.Reference
	kn             *yaml.Node
	vn             *yaml.Node
	idx            *index.SpecIndex
	rendered       *Schema
	buildError     error
	ctx            context.Context
	cachedHash     *uint64    // Cache computed hash to avoid recalculation
	TransformedRef *yaml.Node // Original node that contained the ref before transformation
	*low.NodeMap
}

// Build will prepare the SchemaProxy for rendering, it does not build the Schema, only sets up internal state.
// Key maybe nil if absent.
func (sp *SchemaProxy) Build(ctx context.Context, key, value *yaml.Node, idx *index.SpecIndex) error {
	sp.kn = key
	sp.idx = idx

	// transform sibling refs to allOf structure if enabled and applicable
	// this ensures sp.vn contains the pre-transformed YAML as the source of truth
	transformedValue := value
	wasTransformed := false
	if idx != nil && idx.GetConfig() != nil && idx.GetConfig().TransformSiblingRefs {
		transformer := NewSiblingRefTransformer(idx)
		if transformer.ShouldTransform(value) {
			transformed, _ := transformer.TransformSiblingRef(value)
			if transformed != nil {
				transformedValue = transformed
				wasTransformed = true
				sp.TransformedRef = value // store original node that had the ref
			}
		}
	}

	sp.vn = transformedValue
	sp.ctx = applySchemaIdScope(ctx, value, idx)

	// handle reference detection
	if !wasTransformed {
		// for non-transformed schemas, handle reference normally
		if rf, _, r := utils.IsNodeRefValue(transformedValue); rf {
			sp.SetReference(r, transformedValue)
		}
	}
	// for transformed schemas, don't set reference since it's now an allOf structure
	// the reference is embedded within the allOf, but the schema itself is not a pure reference
	var m sync.Map
	sp.NodeMap = &low.NodeMap{Nodes: &m}
	return nil
}

func applySchemaIdScope(ctx context.Context, node *yaml.Node, idx *index.SpecIndex) context.Context {
	if node == nil {
		return ctx
	}
	scope := index.GetSchemaIdScope(ctx)
	idValue := index.FindSchemaIdInNode(node)
	if idValue == "" {
		return ctx
	}
	if scope == nil {
		base := ""
		if idx != nil {
			base = idx.GetSpecAbsolutePath()
		}
		scope = index.NewSchemaIdScope(base)
		ctx = index.WithSchemaIdScope(ctx, scope)
	}
	parentBase := scope.BaseUri
	resolved, err := index.ResolveSchemaId(idValue, parentBase)
	if err != nil || resolved == "" {
		resolved = idValue
	}
	updated := scope.Copy()
	updated.PushId(resolved)
	return index.WithSchemaIdScope(ctx, updated)
}

// Schema will first check if this SchemaProxy has already rendered the schema, and return the pre-rendered version
// first.
//
// If this is the first run of Schema(), then the SchemaProxy will create a new Schema from the underlying
// yaml.Node. Once built out, the SchemaProxy will record that Schema as rendered and store it for later use,
// (this is what is we mean when we say 'pre-rendered').
//
// Schema() then returns the newly created Schema.
//
// If anything goes wrong during the build, then nothing is returned and the error that occurred can
// be retrieved by using GetBuildError()
func (sp *SchemaProxy) Schema() *Schema {
	if sp.rendered != nil {
		return sp.rendered
	}

	// If this proxy represents an unresolved external ref, return nil without error.
	if sp.IsReference() && sp.idx != nil && sp.idx.GetConfig() != nil &&
		sp.idx.GetConfig().SkipExternalRefResolution && utils.IsExternalRef(sp.GetReference()) {
		return nil
	}

	// handle property merging for references with sibling properties
	buildNode := sp.vn
	if sp.idx != nil && sp.idx.GetConfig() != nil {
		if docConfig := sp.getDocumentConfig(); docConfig != nil && docConfig.MergeReferencedProperties {
			if mergedNode := sp.attemptPropertyMerging(buildNode, docConfig); mergedNode != nil {
				buildNode = mergedNode
			}
		}
	}

	schema := new(Schema)
	utils.CheckForMergeNodes(buildNode)
	err := schema.Build(sp.ctx, buildNode, sp.idx)
	if err != nil {
		sp.buildError = err
		return nil
	}
	schema.ParentProxy = sp // https://github.com/pb33f/libopenapi/issues/29
	sp.rendered = schema

	// for all the nodes added, copy them over to the schema
	if sp.NodeMap != nil {
		sp.NodeMap.Nodes.Range(func(key, value any) bool {
			schema.AddNode(key.(int), value.(*yaml.Node))
			return true
		})
	}
	return schema
}

// GetBuildError returns the build error that was set when Schema() was called. If Schema() has not been run, or
// there were no errors during build, then nil will be returned.
func (sp *SchemaProxy) GetBuildError() error {
	return sp.buildError
}

func (sp *SchemaProxy) GetSchemaReferenceLocation() *index.NodeOrigin {
	if sp.idx != nil {
		origin := sp.idx.FindNodeOrigin(sp.vn)
		if origin != nil {
			return origin
		}
		if sp.idx.GetRolodex() != nil {
			origin = sp.idx.GetRolodex().FindNodeOrigin(sp.vn)
			return origin
		}
	}
	return nil
}

// GetKeyNode will return the yaml.Node pointer that is a key for value node.
func (sp *SchemaProxy) GetKeyNode() *yaml.Node {
	return sp.kn
}

// GetContext will return the context.Context object that was passed to the SchemaProxy during build.
func (sp *SchemaProxy) GetContext() context.Context {
	return sp.ctx
}

// GetValueNode will return the yaml.Node pointer used by the proxy to generate the Schema.
func (sp *SchemaProxy) GetValueNode() *yaml.Node {
	return sp.vn
}

// Hash will return a consistent Hash of the SchemaProxy object (it will resolve it)
func (sp *SchemaProxy) Hash() uint64 {
	if sp.cachedHash != nil {
		return *sp.cachedHash
	}

	var hash uint64

	if sp.rendered != nil {
		if !sp.IsReference() {
			hash = sp.rendered.Hash()
		} else {
			// For references, hash the reference value
			hash = low.WithHasher(func(h *maphash.Hash) uint64 {
				h.WriteString(sp.GetReference())
				return h.Sum64()
			})
		}
	} else {
		if !sp.IsReference() {
			// Only resolve this proxy if it's not a ref.
			sch := sp.Schema()
			sp.rendered = sch
			hashError := fmt.Errorf("circular reference detected: %s", sp.GetReference())
			if sch != nil {
				if sp.idx != nil && sp.idx.GetConfig() != nil && sp.idx.GetConfig().UseSchemaQuickHash {
					if !CheckSchemaProxyForCircularRefs(sp) {
						hash = sch.Hash()
					}
				} else {
					hash = sch.Hash()
				}
			} else {
				var logger *slog.Logger
				if sp.idx != nil && sp.idx.GetLogger() != nil {
					logger = sp.idx.GetLogger()
				}
				if logger != nil {
					bErr := errors.Join(sp.GetBuildError(), hashError)
					if bErr != nil {
						logger.Warn("SchemaProxy.Hash() unable to complete hash: ", "error", bErr.Error())
					}
				}
				hash = 0
			}
		} else {
			// Handle UseSchemaQuickHash case for references
			if sp.idx != nil && sp.idx.GetConfig() != nil && sp.idx.GetConfig().UseSchemaQuickHash {
				if sp.idx != nil && !CheckSchemaProxyForCircularRefs(sp) {
					if sp.rendered == nil {
						sp.rendered = sp.Schema()
					}
					hash = sp.rendered.QuickHash() // quick hash uses a cache to keep things fast.
				} else {
					hash = low.WithHasher(func(h *maphash.Hash) uint64 {
						h.WriteString(sp.GetReference())
						return h.Sum64()
					})
				}
			} else {
				// Hash reference value only, do not resolve!
				hash = low.WithHasher(func(h *maphash.Hash) uint64 {
					h.WriteString(sp.GetReference())
					return h.Sum64()
				})
			}
		}
	}

	// Cache the computed hash for future calls
	sp.cachedHash = &hash
	return hash
}

// AddNode stores nodes in the underlying schema if rendered, otherwise holds in the proxy until build.
func (sp *SchemaProxy) AddNode(key int, node *yaml.Node) {
	// Clear cached hash since content is being modified
	sp.cachedHash = nil

	if sp.rendered != nil {
		sp.rendered.AddNode(key, node)
	} else {
		sp.Nodes.Store(key, node)
	}
}

// GetIndex will return the index.SpecIndex pointer that was passed to the SchemaProxy during build.
func (sp *SchemaProxy) GetIndex() *index.SpecIndex {
	return sp.idx
}

type HasIndex interface {
	GetIndex() *index.SpecIndex
}

// getDocumentConfig retrieves the document configuration from the index
func (sp *SchemaProxy) getDocumentConfig() *datamodel.DocumentConfiguration {
	if sp.idx == nil || sp.idx.GetRolodex() == nil {
		return nil
	}
	rolodex := sp.idx.GetRolodex()
	if config := rolodex.GetConfig(); config != nil {
		return config.ToDocumentConfiguration()
	}
	return nil
}

// attemptPropertyMerging attempts to merge properties for references with siblings
func (sp *SchemaProxy) attemptPropertyMerging(node *yaml.Node, config *datamodel.DocumentConfiguration) *yaml.Node {
	if !config.MergeReferencedProperties || !utils.IsNodeMap(node) {
		return nil
	}

	// extract ref value and sibling properties
	var refValue string
	siblings := make(map[string]*yaml.Node)

	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			if node.Content[i].Value == "$ref" {
				refValue = node.Content[i+1].Value
			} else {
				siblings[node.Content[i].Value] = node.Content[i+1]
			}
		}
	}

	if refValue == "" || len(siblings) == 0 {
		return nil // no merging needed
	}

	referencedComponent := sp.idx.FindComponentInRoot(sp.ctx, refValue)
	if referencedComponent == nil || referencedComponent.Node == nil {
		return nil // cannot resolve reference
	}

	// create property merger and merge
	merger := NewPropertyMerger(config.PropertyMergeStrategy)

	// create a local node with just the sibling properties
	localNode := &yaml.Node{Kind: yaml.MappingNode}
	for key, value := range siblings {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
		localNode.Content = append(localNode.Content, keyNode, value)
	}

	// merge local properties with referenced schema
	merged, err := merger.MergeProperties(localNode, referencedComponent.Node)
	if err != nil {
		// if merging fails, return original node to preserve existing behavior
		return nil
	}

	return merged
}

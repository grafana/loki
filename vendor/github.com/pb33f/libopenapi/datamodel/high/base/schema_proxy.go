// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// inlineRenderingTracker tracks schemas during inline rendering to prevent infinite recursion.
// Uses sync.Map for lock-free concurrent access - each goroutine works on different keys,
// so sync.Map's internal sharding reduces contention compared to a single mutex.
var inlineRenderingTracker sync.Map

// bundlingModeCount tracks the number of active bundling operations.
// Uses reference counting to support concurrent BundleDocument calls safely.
//
// NOTE: This is process-wide. Any RenderInline() call made while bundling is active
// (count > 0) will also preserve local component refs. This is intentional - the bundler
// uses RenderInline internally, and concurrent bundles must all see consistent behavior.
// Direct RenderInline() calls outside of bundling are unaffected when no bundles are running.
var bundlingModeCount atomic.Int32

// SetBundlingMode increments or decrements the bundling mode reference count.
// Bundling mode is active when count > 0, supporting concurrent bundle operations.
func SetBundlingMode(enabled bool) {
	if enabled {
		bundlingModeCount.Add(1)
	} else {
		bundlingModeCount.Add(-1)
	}
}

// IsBundlingMode returns whether any bundling operation is active.
func IsBundlingMode() bool {
	return bundlingModeCount.Load() > 0
}

// RenderingMode controls how inline rendering handles discriminator $refs.
type RenderingMode int

const (
	// RenderingModeBundle is the default mode - preserves $refs in discriminator
	// oneOf/anyOf for compatibility with discriminator mappings during bundling.
	RenderingModeBundle RenderingMode = iota

	// RenderingModeValidation forces full inlining of all $refs, ignoring
	// discriminator preservation. Use this when rendering schemas for JSON
	// Schema validation where the compiler needs a self-contained schema.
	RenderingModeValidation
)

// InlineRenderContext provides isolated tracking for inline rendering operations.
// Each render call-chain should use its own context to prevent false positive
// cycle detection when multiple goroutines render the same schemas concurrently.
type InlineRenderContext struct {
	tracker       sync.Map
	Mode          RenderingMode
	preservedRefs sync.Map // tracks refs that should be preserved in this render
}

// NewInlineRenderContext creates a new isolated rendering context with default bundle mode.
func NewInlineRenderContext() *InlineRenderContext {
	return &InlineRenderContext{Mode: RenderingModeBundle}
}

// NewInlineRenderContextForValidation creates a context that fully inlines
// all refs, including discriminator oneOf/anyOf refs. Use this when rendering
// schemas for JSON Schema validation.
func NewInlineRenderContextForValidation() *InlineRenderContext {
	return &InlineRenderContext{Mode: RenderingModeValidation}
}

// StartRendering marks a key as being rendered. Returns true if already rendering (cycle detected).
// The key should be stable and unique per schema instance (e.g., filePath:$ref).
func (ctx *InlineRenderContext) StartRendering(key string) bool {
	if key == "" {
		return false
	}
	_, loaded := ctx.tracker.LoadOrStore(key, true)
	return loaded
}

// StopRendering marks a key as done rendering.
func (ctx *InlineRenderContext) StopRendering(key string) {
	if key != "" {
		ctx.tracker.Delete(key)
	}
}

// MarkRefAsPreserved marks a reference as one that should be preserved (not inlined) in this render.
// used by discriminator handling to track which refs need preservation without mutating shared state.
func (ctx *InlineRenderContext) MarkRefAsPreserved(ref string) {
	if ref != "" {
		ctx.preservedRefs.Store(ref, true)
	}
}

// ShouldPreserveRef returns true if the given reference was marked for preservation.
func (ctx *InlineRenderContext) ShouldPreserveRef(ref string) bool {
	if ref == "" {
		return false
	}
	_, ok := ctx.preservedRefs.Load(ref)
	return ok
}

// SchemaProxy exists as a stub that will create a Schema once (and only once) the Schema() method is called. An
// underlying low-level SchemaProxy backs this high-level one.
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
	schema     *low.NodeReference[*base.SchemaProxy]
	buildError error
	rendered   *Schema
	refStr     string
	lock       *sync.Mutex
}

// NewSchemaProxy creates a new high-level SchemaProxy from a low-level one.
func NewSchemaProxy(schema *low.NodeReference[*base.SchemaProxy]) *SchemaProxy {
	return &SchemaProxy{schema: schema, lock: &sync.Mutex{}}
}

// copySchemaWithParentProxy creates a shallow copy of a schema and sets the ParentProxy
func (sp *SchemaProxy) copySchemaWithParentProxy(schema *Schema) *Schema {
	schemaCopy := *schema
	schemaCopy.ParentProxy = sp
	return &schemaCopy
}

// CreateSchemaProxy will create a new high-level SchemaProxy from a high-level Schema, this acts the same
// as if the SchemaProxy is pre-rendered.
func CreateSchemaProxy(schema *Schema) *SchemaProxy {
	return &SchemaProxy{rendered: schema, lock: &sync.Mutex{}}
}

// CreateSchemaProxyRef will create a new high-level SchemaProxy from a reference string, this is used only when
// building out new models from scratch that require a reference rather than a schema implementation.
func CreateSchemaProxyRef(ref string) *SchemaProxy {
	return &SchemaProxy{refStr: ref, lock: &sync.Mutex{}}
}

// GetValueNode returns the value node of the SchemaProxy.
func (sp *SchemaProxy) GetValueNode() *yaml.Node {
	if sp.schema != nil {
		return sp.schema.ValueNode
	}
	return nil
}

// Schema will create a new Schema instance using NewSchema from the low-level SchemaProxy backing this high-level one.
// If there is a problem building the Schema, then this method will return nil. Use GetBuildError to gain access
// to that building error.
//
// It's important to note that this method will return nil on a pointer created using NewSchemaProxy or CreateSchema* methods
// there is no low-level SchemaProxy backing it, and therefore no schema to build, so this will fail. Use BuildSchema
// instead for proxies created using NewSchemaProxy or CreateSchema* methods.
// https://github.com/pb33f/libopenapi/issues/403
func (sp *SchemaProxy) Schema() *Schema {
	if sp == nil || sp.lock == nil {
		return nil
	}

	sp.lock.Lock()
	defer sp.lock.Unlock()

	if sp.rendered != nil {
		return sp.rendered
	}

	if sp.schema == nil || sp.schema.Value == nil {
		return nil
	}

	// check the high-level cache first.
	idx := sp.schema.Value.GetIndex()
	if idx != nil && sp.schema.Value != nil {
		if sp.schema.Value.IsReference() && sp.schema.Value.GetReferenceNode() != nil && sp.schema.GetValueNode() != nil {
			loc := fmt.Sprintf("%s:%d:%d", idx.GetSpecAbsolutePath(), sp.schema.GetValueNode().Line, sp.schema.GetValueNode().Column)
			if seen, ok := idx.GetHighCache().Load(loc); ok {
				idx.HighCacheHit()
				// cache locally to avoid recreating on repeated access
				sp.rendered = sp.copySchemaWithParentProxy(seen.(*Schema))
				return sp.rendered
			} else {
				idx.HighCacheMiss()
			}
		}
	}

	s := sp.schema.Value.Schema()
	if s == nil {
		sp.buildError = sp.schema.Value.GetBuildError()
		return nil
	}
	sch := NewSchema(s)

	if idx != nil {
		// only store the schema in the cache if is a reference!
		if sp.IsReference() && sp.GetReferenceNode() != nil && sp.schema != nil && sp.schema.GetValueNode() != nil {
			// if sp.schema.GetValueNode() != nil {
			loc := fmt.Sprintf("%s:%d:%d", idx.GetSpecAbsolutePath(), sp.schema.GetValueNode().Line, sp.schema.GetValueNode().Column)

			// caching is only performed on traditional $ref nodes with a reference and a value node, any 3.1 additional
			// will not be cached as libopenapi does not yet support them.
			if len(sp.GetReferenceNode().Content) == 2 {
				idx.GetHighCache().Store(loc, sch)
			}
		}
	}

	sp.rendered = sp.copySchemaWithParentProxy(sch)
	return sp.rendered
}

// IsReference returns true if the SchemaProxy is a reference to another Schema.
func (sp *SchemaProxy) IsReference() bool {
	if sp == nil {
		return false
	}

	if sp.refStr != "" {
		return true
	}
	if sp.schema != nil && sp.schema.Value != nil {
		return sp.schema.Value.IsReference()
	}
	return false
}

// GetReference returns the location of the $ref if this SchemaProxy is a reference to another Schema.
func (sp *SchemaProxy) GetReference() string {
	if sp.refStr != "" {
		return sp.refStr
	}
	return sp.schema.GetValue().GetReference()
}

func (sp *SchemaProxy) GetSchemaKeyNode() *yaml.Node {
	if sp.schema != nil {
		return sp.GoLow().GetKeyNode()
	}
	return nil
}

func (sp *SchemaProxy) GetReferenceNode() *yaml.Node {
	if sp.refStr != "" {
		return utils.CreateRefNode(sp.refStr)
	}
	return sp.schema.GetValue().GetReferenceNode()
}

// GetReferenceOrigin returns a pointer to the index.NodeOrigin of the $ref if this SchemaProxy is a reference to another Schema.
// returns nil if the origin cannot be found (which, means there is a bug, and we need to fix it).
func (sp *SchemaProxy) GetReferenceOrigin() *index.NodeOrigin {
	if sp.schema != nil {
		return sp.schema.Value.GetSchemaReferenceLocation()
	}
	return nil
}

// BuildSchema operates the same way as Schema, except it will return any error along with the *Schema. Unlike the Schema
// method, this will work on a proxy created by the NewSchemaProxy or CreateSchema* methods.
//
// It differs from Schema in that it does not require a low-level SchemaProxy to be present,
// and will build the schema from the high-level one.
func (sp *SchemaProxy) BuildSchema() (*Schema, error) {
	if sp == nil {
		return nil, nil
	}
	schema := sp.Schema()
	if sp.lock == nil {
		return schema, sp.buildError
	}
	sp.lock.Lock()
	er := sp.buildError
	sp.lock.Unlock()
	return schema, er
}

// GetBuildError returns any error that was thrown when calling Schema()
func (sp *SchemaProxy) GetBuildError() error {
	if sp == nil {
		return nil
	}
	if sp.lock == nil {
		return sp.buildError
	}
	sp.lock.Lock()
	err := sp.buildError
	sp.lock.Unlock()
	return err
}

func (sp *SchemaProxy) GoLow() *base.SchemaProxy {
	if sp.schema == nil {
		return nil
	}
	return sp.schema.Value
}

func (sp *SchemaProxy) GoLowUntyped() any {
	if sp.schema == nil {
		return nil
	}
	return sp.schema.Value
}

// Render will return a YAML representation of the Schema object as a byte slice.
func (sp *SchemaProxy) Render() ([]byte, error) {
	return yaml.Marshal(sp)
}

// MarshalYAML will create a ready to render YAML representation of the SchemaProxy object.
func (sp *SchemaProxy) MarshalYAML() (interface{}, error) {
	var s *Schema
	var err error
	// if this schema isn't a reference, then build it out.
	if !sp.IsReference() {
		s, err = sp.BuildSchema()
		if err != nil {
			return nil, err
		}
		nb := high.NewNodeBuilder(s, s.low)
		return nb.Render(), nil
	} else {
		return sp.GetReferenceNode(), nil
	}
}

// getInlineRenderKey generates a unique key for tracking this schema during inline rendering.
// This prevents infinite recursion when schemas reference each other circularly.
func (sp *SchemaProxy) getInlineRenderKey() string {
	// Check for nil schema first (sp.schema or sp.schema.Value could be nil)
	if sp.schema == nil || sp.schema.Value == nil {
		// Check for refStr-based reference
		if sp.refStr != "" {
			return sp.refStr
		}
		return ""
	}
	// Use the reference string if available
	if sp.IsReference() {
		ref := sp.GetReference()
		// Include the index path to handle cross-file references
		if sp.schema.Value != nil && sp.schema.Value.GetIndex() != nil {
			idx := sp.schema.Value.GetIndex()
			return fmt.Sprintf("%s:%s", idx.GetSpecAbsolutePath(), ref)
		}
		return ref
	}
	// For inline schemas, use the node position
	if sp.schema.ValueNode != nil {
		node := sp.schema.ValueNode
		var idx *index.SpecIndex
		if sp.schema.Value != nil {
			idx = sp.schema.Value.GetIndex()
		}
		if node.Line > 0 && node.Column > 0 {
			if idx != nil {
				return fmt.Sprintf("%s:%d:%d", idx.GetSpecAbsolutePath(), node.Line, node.Column)
			}
			return fmt.Sprintf("inline:%d:%d", node.Line, node.Column)
		}
		// Nodes created via yaml.Node.Encode() don't include line/column info.
		// Fall back to a pointer-based key to avoid false cycle detection.
		if idx != nil {
			return fmt.Sprintf("%s:inline:%p", idx.GetSpecAbsolutePath(), node)
		}
		return fmt.Sprintf("inline:%p", node)
	}
	return ""
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the SchemaProxy object
// using the provided InlineRenderContext for cycle detection. Use this when multiple goroutines may render
// the same schemas concurrently to avoid false positive cycle detection.
// The ctx parameter should be *InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (sp *SchemaProxy) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	if renderCtx, ok := ctx.(*InlineRenderContext); ok {
		return sp.marshalYAMLInlineInternal(renderCtx)
	}
	// Fallback to fresh context if wrong type passed
	return sp.marshalYAMLInlineInternal(NewInlineRenderContext())
}

// MarshalYAMLInline will create a ready to render YAML representation of the SchemaProxy object. The
// $ref values will be inlined instead of kept as is. All circular references will be ignored, regardless
// of the type of circular reference, they are all bad when rendering.
// This method creates a fresh InlineRenderContext internally. For concurrent scenarios, use
// MarshalYAMLInlineWithContext instead.
func (sp *SchemaProxy) MarshalYAMLInline() (interface{}, error) {
	ctx := NewInlineRenderContext()
	return sp.marshalYAMLInlineInternal(ctx)
}

func (sp *SchemaProxy) marshalYAMLInlineInternal(ctx *InlineRenderContext) (interface{}, error) {
	// check if this reference should be preserved (set via context by discriminator handling).
	// this avoids mutating shared SchemaProxy state and prevents race conditions.
	// need to guard against nil schema.Value which can happen with bad/incomplete proxies.
	if sp.IsReference() {
		ref := sp.GetReference()
		if ref != "" && ctx.ShouldPreserveRef(ref) {
			return sp.GetReferenceNode(), nil
		}
	}

	// In bundling mode, preserve local component refs that point to schemas in the SAME document.
	// Only inline refs that point to schemas from EXTERNAL files.
	// Outside of bundling mode (direct MarshalYAMLInline calls), inline everything.
	if IsBundlingMode() && sp.IsReference() {
		ref := sp.GetReference()
		if strings.HasPrefix(ref, "#/components/") {
			// Check if this ref points to a schema in the same root document.
			// If the low-level proxy has an index, compare it to the root index.
			if sp.schema != nil && sp.schema.Value != nil {
				lowProxy := sp.schema.Value
				schemaIdx := lowProxy.GetIndex()
				if schemaIdx != nil {
					rolodex := schemaIdx.GetRolodex()
					if rolodex != nil {
						rootIdx := rolodex.GetRootIndex()
						// If the schema is in the root index, preserve the ref
						if rootIdx != nil && schemaIdx == rootIdx {
							return sp.GetReferenceNode(), nil
						}
					}
				}
			}
		}
	}

	// Check for recursive rendering using the context's tracker.
	// This prevents infinite recursion when circular references aren't properly detected.
	// Using a scoped context instead of a global tracker prevents false positive cycle detection
	// when multiple goroutines render the same schemas concurrently.
	renderKey := sp.getInlineRenderKey()
	if ctx.StartRendering(renderKey) {
		// We're already rendering this schema in THIS call chain - return ref to break the cycle
		if sp.IsReference() {
			return sp.GetReferenceNode(),
				fmt.Errorf("schema render failure, circular reference: `%s`", sp.GetReference())
		}
		// For inline schemas, return an empty map to avoid infinite recursion
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"},
			fmt.Errorf("schema render failure, circular reference detected during inline rendering")
	}
	defer ctx.StopRendering(renderKey)

	var s *Schema
	var err error
	s, err = sp.BuildSchema()

	if s != nil && s.GoLow() != nil && s.GoLow().Index != nil {
		idx := s.GoLow().Index
		circ := idx.GetCircularReferences()

		// Extract ignored and safe circular references from rolodex if available
		if idx.GetRolodex() != nil {
			ignored := idx.GetRolodex().GetIgnoredCircularReferences()
			safe := idx.GetRolodex().GetSafeCircularReferences()
			circ = append(circ, ignored...)
			circ = append(circ, safe...)
		}

		cirError := func(str string) error {
			return fmt.Errorf("schema render failure, circular reference: `%s`", str)
		}

		for _, c := range circ {
			if sp.IsReference() {
				if sp.GetReference() == c.LoopPoint.Definition {
					// nope
					return sp.GetReferenceNode(),
						cirError((c.LoopPoint.Definition))
				}
				basePath := sp.GoLow().GetIndex().GetSpecAbsolutePath()

				if !filepath.IsAbs(basePath) && !strings.HasPrefix(basePath, "http") {
					basePath, _ = filepath.Abs(basePath)
				}

				if basePath == c.LoopPoint.FullDefinition {
					// we loop on our-self
					return sp.GetReferenceNode(),
						cirError((c.LoopPoint.Definition))
				}
				a := utils.ReplaceWindowsDriveWithLinuxPath(strings.Replace(c.LoopPoint.FullDefinition, basePath, "", 1))
				b := sp.GetReference()
				if strings.HasPrefix(b, "./") {
					b = strings.Replace(b, "./", "/", 1) // strip any leading ./ from the reference
				}
				// if loading things in remotely and references are relative.
				if strings.HasPrefix(a, "http") {
					purl, _ := url.Parse(a)
					if purl != nil {
						specPath := filepath.Dir(purl.Path)
						host := fmt.Sprintf("%s://%s", purl.Scheme, purl.Host)
						a = strings.Replace(a, host, "", 1)
						a = strings.Replace(a, specPath, "", 1)
					}
				}

				aBase, aFragment := index.SplitRefFragment(a)
				bBase, bFragment := index.SplitRefFragment(b)

				if aFragment != "" && bFragment != "" && aFragment == bFragment {
					return sp.GetReferenceNode(),
						cirError((c.LoopPoint.Definition))
				}

				if aFragment == "" && bFragment == "" {
					aNorm := strings.TrimPrefix(strings.TrimPrefix(aBase, "./"), "/")
					bNorm := strings.TrimPrefix(strings.TrimPrefix(bBase, "./"), "/")
					if aNorm != "" && bNorm != "" && aNorm == bNorm {
						// nope
						return sp.GetReferenceNode(),
							cirError((c.LoopPoint.Definition))
					}
				}
			}
		}
	}

	if err != nil {
		return nil, err
	}
	if s != nil {
		// Delegate to Schema.MarshalYAMLInlineWithContext to ensure discriminator handling is applied
		// and cycle detection context is propagated.
		// Schema.MarshalYAMLInlineWithContext sets preserveReference on OneOf/AnyOf items when
		// a discriminator is present, which is required for proper bundling.
		return s.MarshalYAMLInlineWithContext(ctx)
	}
	return nil, errors.New("unable to render schema")
}

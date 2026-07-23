// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
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

// buildCacheKey builds a "path:line:col" string without fmt.Sprintf allocations.
func buildCacheKey(path string, line, col int) string {
	var b strings.Builder
	b.Grow(len(path) + 12)
	b.WriteString(path)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(line))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(col))
	return b.String()
}

// inlineRenderingTracker tracks schemas during inline rendering to prevent infinite recursion.
// Uses sync.Map for lock-free concurrent access - each goroutine works on different keys,
// so sync.Map's internal sharding reduces contention compared to a single mutex.
var inlineRenderingTracker sync.Map

// ClearInlineRenderingTracker resets the inline rendering tracker.
// Call this between document lifecycles in long-running processes to bound memory.
func ClearInlineRenderingTracker() {
	inlineRenderingTracker.Clear()
}

// bundlingModeCount is retained for source compatibility with callers that use
// SetBundlingMode and IsBundlingMode directly. New legacy render contexts
// snapshot this state, while bundle policy is explicitly per operation.
var bundlingModeCount atomic.Int32

// SetBundlingMode increments or decrements the bundling mode reference count.
// Bundling mode is active when count > 0, supporting concurrent bundle operations.
//
// Deprecated: configure InlineRenderContext.PreserveLocalComponentRefs instead.
func SetBundlingMode(enabled bool) {
	if enabled {
		bundlingModeCount.Add(1)
	} else {
		bundlingModeCount.Add(-1)
	}
}

// IsBundlingMode returns whether any bundling operation is active.
//
// Deprecated: bundle rendering no longer consults process-global bundling mode.
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
	tracker sync.Map
	Mode    RenderingMode
	// PreserveLocalComponentRefs keeps root-local component references intact
	// while external references continue through inline rendering.
	PreserveLocalComponentRefs bool
	// RootIndex identifies the authored root document for local-reference policy.
	RootIndex *index.SpecIndex
	// StrictCircularReferenceIdentity requires circular metadata to match the
	// prepared canonical target instead of a bare authored reference string.
	StrictCircularReferenceIdentity bool
	preservedRefs                   sync.Map // scoped reference key -> struct{}
	referenceRewrites               sync.Map // scoped reference key -> root component ref
	mappingRewrites                 sync.Map // discriminator mapping value node -> root component ref
	referenceNodeSources            sync.Map // authored reference node -> source index
	referenceNodeTargets            sync.Map // authored reference node -> canonical target
	preservedReferenceNodes         sync.Map // authored reference node -> struct{}
	referenceNodeRewrites           sync.Map // authored reference node -> root component ref
}

// NewInlineRenderContext creates a new isolated rendering context with default bundle mode.
func NewInlineRenderContext() *InlineRenderContext {
	return &InlineRenderContext{
		Mode:                       RenderingModeBundle,
		PreserveLocalComponentRefs: IsBundlingMode(),
	}
}

// NewInlineRenderContextForValidation creates a context that fully inlines
// all refs, including discriminator oneOf/anyOf refs. Use this when rendering
// schemas for JSON Schema validation.
func NewInlineRenderContextForValidation() *InlineRenderContext {
	return &InlineRenderContext{Mode: RenderingModeValidation}
}

// ScopedReferenceKey identifies an authored reference in the document where it
// appears. The source qualification prevents equal local fragments in different
// external documents from colliding during one render.
func ScopedReferenceKey(source *index.SpecIndex, ref string) string {
	if source == nil {
		return scopedReferenceKey("", ref)
	}
	return scopedReferenceKey(source.GetSpecAbsolutePath(), ref)
}

func scopedReferenceKey(sourcePath, ref string) string {
	if ref == "" {
		return ""
	}
	if sourcePath == "" {
		return "\x00" + ref
	}
	return sourcePath + "\x00" + ref
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
//
// Deprecated: use MarkScopedRefAsPreserved to qualify the authored reference by source document.
func (ctx *InlineRenderContext) MarkRefAsPreserved(ref string) {
	ctx.MarkScopedRefAsPreserved(nil, ref)
}

// ShouldPreserveRef returns true if the given reference was marked for preservation.
//
// Deprecated: use ShouldPreserveScopedRef to qualify the authored reference by source document.
func (ctx *InlineRenderContext) ShouldPreserveRef(ref string) bool {
	return ctx.ShouldPreserveScopedRef(nil, ref)
}

// MarkScopedRefAsPreserved marks an authored reference in a specific source
// document as preserved for this render operation.
func (ctx *InlineRenderContext) MarkScopedRefAsPreserved(source *index.SpecIndex, ref string) {
	ctx.markRefAsPreserved(scopedReferencePath(source), ref)
}

func scopedReferencePath(source *index.SpecIndex) string {
	if source == nil {
		return ""
	}
	return source.GetSpecAbsolutePath()
}

func (ctx *InlineRenderContext) markRefAsPreserved(sourcePath, ref string) {
	if key := scopedReferenceKey(sourcePath, ref); key != "" {
		ctx.preservedRefs.Store(key, struct{}{})
	}
}

// ShouldPreserveScopedRef reports whether the source-qualified authored
// reference must remain a reference during this render operation.
func (ctx *InlineRenderContext) ShouldPreserveScopedRef(source *index.SpecIndex, ref string) bool {
	return ctx.shouldPreserveRef(scopedReferencePath(source), ref)
}

func (ctx *InlineRenderContext) shouldPreserveRef(sourcePath, ref string) bool {
	key := scopedReferenceKey(sourcePath, ref)
	if key == "" {
		return false
	}
	_, ok := ctx.preservedRefs.Load(key)
	return ok
}

// SetReferenceRewrite maps a source-qualified authored reference to a
// self-contained root component reference for this render operation.
func (ctx *InlineRenderContext) SetReferenceRewrite(source *index.SpecIndex, ref, replacement string) {
	ctx.setReferenceRewrite(scopedReferencePath(source), ref, replacement)
}

func (ctx *InlineRenderContext) setReferenceRewrite(sourcePath, ref, replacement string) {
	if key := scopedReferenceKey(sourcePath, ref); key != "" && replacement != "" {
		ctx.referenceRewrites.Store(key, replacement)
	}
}

// ReferenceRewrite returns the root component reference allocated for a
// source-qualified authored reference.
func (ctx *InlineRenderContext) ReferenceRewrite(source *index.SpecIndex, ref string) (string, bool) {
	return ctx.referenceRewrite(scopedReferencePath(source), ref)
}

func (ctx *InlineRenderContext) referenceRewrite(sourcePath, ref string) (string, bool) {
	key := scopedReferenceKey(sourcePath, ref)
	if key == "" {
		return "", false
	}
	replacement, ok := ctx.referenceRewrites.Load(key)
	if !ok {
		return "", false
	}
	return replacement.(string), true
}

// SetMappingRewrite records a replacement for one raw discriminator mapping
// value without mutating the indexed YAML node.
func (ctx *InlineRenderContext) SetMappingRewrite(node *yaml.Node, replacement string) {
	if node != nil && replacement != "" {
		ctx.mappingRewrites.Store(node, replacement)
	}
}

// MappingRewrite returns a prepared discriminator mapping replacement.
func (ctx *InlineRenderContext) MappingRewrite(node *yaml.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	replacement, ok := ctx.mappingRewrites.Load(node)
	if !ok {
		return "", false
	}
	return replacement.(string), true
}

// SetReferenceNodeIdentity records the authored source and canonical target for
// a reference node. Bundle preparation calls this once before rendering.
func (ctx *InlineRenderContext) SetReferenceNodeIdentity(node *yaml.Node, source *index.SpecIndex, target string) {
	if node == nil {
		return
	}
	if source != nil {
		ctx.referenceNodeSources.Store(node, source)
	}
	if target != "" {
		ctx.referenceNodeTargets.Store(node, target)
	}
}

// ReferenceNodeSource returns the authored source index prepared for node.
func (ctx *InlineRenderContext) ReferenceNodeSource(node *yaml.Node) (*index.SpecIndex, bool) {
	if node == nil {
		return nil, false
	}
	source, ok := ctx.referenceNodeSources.Load(node)
	if !ok {
		return nil, false
	}
	return source.(*index.SpecIndex), true
}

// ReferenceNodeTarget returns the canonical resolved target prepared for node.
func (ctx *InlineRenderContext) ReferenceNodeTarget(node *yaml.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	target, ok := ctx.referenceNodeTargets.Load(node)
	if !ok {
		return "", false
	}
	return target.(string), true
}

// MarkReferenceNodeAsPreserved marks one authored reference occurrence for
// preservation without relying on an ambiguous bare reference string.
func (ctx *InlineRenderContext) MarkReferenceNodeAsPreserved(node *yaml.Node) {
	if node != nil {
		ctx.preservedReferenceNodes.Store(node, struct{}{})
	}
}

// ShouldPreserveReferenceNode reports whether an authored occurrence is preserved.
func (ctx *InlineRenderContext) ShouldPreserveReferenceNode(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	_, ok := ctx.preservedReferenceNodes.Load(node)
	return ok
}

// SetReferenceNodeRewrite records the replacement for one authored reference occurrence.
func (ctx *InlineRenderContext) SetReferenceNodeRewrite(node *yaml.Node, replacement string) {
	if node != nil && replacement != "" {
		ctx.referenceNodeRewrites.Store(node, replacement)
	}
}

// ReferenceNodeRewrite returns the replacement for one authored reference occurrence.
func (ctx *InlineRenderContext) ReferenceNodeRewrite(node *yaml.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	replacement, ok := ctx.referenceNodeRewrites.Load(node)
	if !ok {
		return "", false
	}
	return replacement.(string), true
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
	lock       sync.Mutex
}

// NewSchemaProxy creates a new high-level SchemaProxy from a low-level one.
func NewSchemaProxy(schema *low.NodeReference[*base.SchemaProxy]) *SchemaProxy {
	return &SchemaProxy{schema: schema}
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
	return &SchemaProxy{rendered: schema}
}

// CreateSchemaProxyRef will create a new high-level SchemaProxy from a reference string, this is used only when
// building out new models from scratch that require a reference rather than a schema implementation.
func CreateSchemaProxyRef(ref string) *SchemaProxy {
	return &SchemaProxy{refStr: ref}
}

// CreateSchemaProxyRefWithSchema creates a SchemaProxy that carries both a $ref and sibling schema
// properties. This supports JSON Schema 2020-12 section 7.7.1.1 where $ref can coexist with other
// keywords. When rendered, $ref appears first followed by the schema's sibling properties.
//
// If schema is nil, the result behaves identically to CreateSchemaProxyRef.
func CreateSchemaProxyRefWithSchema(ref string, schema *Schema) *SchemaProxy {
	return &SchemaProxy{refStr: ref, rendered: schema}
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
	if sp == nil {
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

	if sp.isParsedRefWithSiblings() {
		sp.rendered = sp.buildSiblingOnlySchemaView()
		return sp.rendered
	}

	// check the high-level cache first.
	idx := sp.schema.Value.GetIndex()
	if idx != nil && sp.schema.Value != nil {
		if sp.schema.Value.IsReference() && sp.schema.Value.GetReferenceNode() != nil && sp.schema.GetValueNode() != nil {
			loc := buildCacheKey(idx.GetSpecAbsolutePath(), sp.schema.GetValueNode().Line, sp.schema.GetValueNode().Column)
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

	cached := false
	if idx != nil {
		// only store the schema in the cache if is a reference!
		if sp.IsReference() && sp.GetReferenceNode() != nil && sp.schema != nil && sp.schema.GetValueNode() != nil {
			loc := buildCacheKey(idx.GetSpecAbsolutePath(), sp.schema.GetValueNode().Line, sp.schema.GetValueNode().Column)

			// caching is only performed on traditional $ref nodes with a reference and a value node, any 3.1 additional
			// will not be cached as libopenapi does not yet support them.
			if len(sp.GetReferenceNode().Content) == 2 {
				idx.GetHighCache().Store(loc, sch)
				cached = true
			}
		}
	}

	if cached {
		// Schema was stored in shared cache — must copy to avoid races with
		// concurrent readers that will read the cached schema.
		sp.rendered = sp.copySchemaWithParentProxy(sch)
	} else {
		// Not cached — safe to set ParentProxy directly, avoiding a copy.
		sch.ParentProxy = sp
		sp.rendered = sch
	}
	return sp.rendered
}

// IsReference returns true if the SchemaProxy is a reference to another Schema.
// For parsed OpenAPI 3.1 $ref-with-siblings schemas, the low proxy is backed by
// an internal allOf node, but the high-level API reflects the authored $ref.
func (sp *SchemaProxy) IsReference() bool {
	if sp == nil {
		return false
	}

	if sp.refStr != "" {
		return true
	}
	if sp.isParsedRefWithSiblings() {
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
	if sp.isParsedRefWithSiblings() {
		return sp.schema.Value.GetTransformedRefReference()
	}
	if refNode := sp.GetReferenceNode(); refNode != nil {
		if refValNode := utils.GetRefValueNode(refNode); refValNode != nil {
			return refValNode.Value
		}
	}
	if sp.schema == nil || sp.schema.Value == nil {
		return ""
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
	if sp.isParsedRefWithSiblings() {
		return sp.schema.Value.TransformedRef
	}
	if sp.schema == nil || sp.schema.Value == nil {
		return nil
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

// isRefWithSiblings returns true when this is a programmatically-created proxy
// that carries both a $ref and sibling schema properties.
func (sp *SchemaProxy) isRefWithSiblings() bool {
	return sp.refStr != "" && sp.rendered != nil && sp.schema == nil
}

// IsTransformedRefWithSiblings reports whether this high-level proxy represents
// an authored OpenAPI 3.1 $ref with sibling schema keywords.
func (sp *SchemaProxy) IsTransformedRefWithSiblings() bool {
	return sp != nil &&
		sp.schema != nil &&
		sp.schema.Value != nil &&
		sp.schema.Value.IsTransformedRefWithSiblings() &&
		sp.shouldCollapseTransformedRefWithSiblings()
}

func (sp *SchemaProxy) isParsedRefWithSiblings() bool {
	return sp.IsTransformedRefWithSiblings()
}

func (sp *SchemaProxy) buildSiblingOnlySchemaView() *Schema {
	if sp == nil || sp.schema == nil || sp.schema.Value == nil {
		return nil
	}
	lowProxy := sp.schema.Value
	siblingNode := lowProxy.GetTransformedRefSiblingSchema()
	if siblingNode == nil {
		return nil
	}

	lowSchema := new(base.Schema)
	if err := lowSchema.Build(lowProxy.GetContext(), siblingNode, lowProxy.GetIndex()); err != nil {
		sp.buildError = err
		return nil
	}
	lowSchema.ParentProxy = lowProxy

	schema := NewSchema(lowSchema)
	schema.ParentProxy = sp
	return schema
}

// BuildTransformedRefSemanticSchema returns the internal semantic allOf view for
// an authored $ref-with-siblings proxy, using current high-level sibling values.
func (sp *SchemaProxy) BuildTransformedRefSemanticSchema(currentSibling *Schema) (*Schema, error) {
	return sp.buildSemanticAllOfSchemaView(currentSibling)
}

func (sp *SchemaProxy) buildSemanticAllOfSchemaView(currentSibling *Schema) (*Schema, error) {
	if sp == nil || sp.schema == nil || sp.schema.Value == nil {
		return nil, nil
	}
	lowSchema := sp.schema.Value.Schema()
	if lowSchema == nil {
		return nil, sp.schema.Value.GetBuildError()
	}
	schema := NewSchema(lowSchema)
	schema.ParentProxy = nil
	if currentSibling == nil {
		currentSibling = sp.Schema()
	}
	if currentSibling != nil && len(schema.AllOf) == 2 {
		siblingCopy := *currentSibling
		siblingCopy.ParentProxy = nil
		schema.AllOf[0] = CreateSchemaProxy(&siblingCopy)
	}
	return schema, nil
}

// renderRefWithSiblings builds a YAML mapping node containing $ref as the
// first key followed by all rendered schema sibling properties.
func (sp *SchemaProxy) renderRefWithSiblings() *yaml.Node {
	nb := high.NewNodeBuilder(sp.rendered, nil)
	node := nb.Render()
	refKey := utils.CreateStringNode("$ref")
	refVal := utils.CreateStringNode(sp.refStr)
	refVal.Style = yaml.SingleQuotedStyle
	content := make([]*yaml.Node, 0, len(node.Content)+2)
	content = append(content, refKey, refVal)
	content = append(content, node.Content...)
	node.Content = content
	return node
}

func (sp *SchemaProxy) renderTransformedRefWithSiblings(s *Schema) (*yaml.Node, bool, error) {
	if sp == nil || sp.schema == nil || sp.schema.Value == nil || sp.schema.Value.TransformedRef == nil || s == nil {
		return nil, false, nil
	}
	if !sp.shouldCollapseTransformedRefWithSiblings() {
		return nil, false, nil
	}

	var siblingNode *yaml.Node
	ref := sp.schema.Value.GetTransformedRefReference()

	if !sp.schemaIsTransformedSiblingView(s) {
		if len(s.AllOf) != 2 || s.AllOf[0] == nil || s.AllOf[1] == nil || !s.AllOf[1].IsReference() {
			return nil, false, nil
		}

		// Only collapse the synthetic allOf created by the sibling-ref transformer.
		// If callers add fields to the outer schema or change its composition, keep
		// the explicit allOf so no mutations are hidden.
		outerNode := high.NewNodeBuilder(s, s.low).Render()
		if len(outerNode.Content) != 2 || outerNode.Content[0].Value != "allOf" {
			return nil, false, nil
		}

		siblingRender, err := s.AllOf[0].MarshalYAML()
		if err != nil {
			return nil, true, err
		}
		var ok bool
		siblingNode, ok = yamlNodeFromRender(siblingRender)
		if !ok || !utils.IsNodeMap(siblingNode) {
			return nil, false, nil
		}
		ref = s.AllOf[1].GetReference()
	} else {
		siblingNode = high.NewNodeBuilder(s, s.low).Render()
	}

	original := sp.schema.Value.TransformedRef
	result := utils.CreateEmptyMapNode()
	consumed := make(map[string]struct{}, len(siblingNode.Content)/2)

	for i := 0; i+1 < len(original.Content); i += 2 {
		keyNode := original.Content[i]
		valueNode := original.Content[i+1]
		if keyNode == nil {
			continue
		}
		if keyNode.Value == "$ref" {
			refKey := utils.CloneYAMLNode(keyNode)
			refValue := utils.CloneYAMLNode(valueNode)
			if refValue == nil {
				refValue = utils.CreateStringNode(ref)
			}
			refValue.Value = ref
			result.Content = append(result.Content, refKey, refValue)
			continue
		}
		if _, siblingValue := findYAMLPair(siblingNode, keyNode.Value); siblingValue != nil {
			renderKey := utils.CloneYAMLNode(keyNode)
			result.Content = append(result.Content, renderKey, utils.CloneYAMLNode(siblingValue))
			consumed[keyNode.Value] = struct{}{}
		}
	}

	for i := 0; i+1 < len(siblingNode.Content); i += 2 {
		keyNode := siblingNode.Content[i]
		valueNode := siblingNode.Content[i+1]
		if _, ok := consumed[keyNode.Value]; ok {
			continue
		}
		result.Content = append(result.Content, utils.CloneYAMLNode(keyNode), utils.CloneYAMLNode(valueNode))
	}

	return result, true, nil
}

func (sp *SchemaProxy) schemaIsTransformedSiblingView(s *Schema) bool {
	if sp == nil || sp.schema == nil || sp.schema.Value == nil || s == nil {
		return false
	}
	return s.low != nil && s.low.RootNode == sp.schema.Value.GetTransformedRefSiblingSchema()
}

func (sp *SchemaProxy) shouldCollapseTransformedRefWithSiblings() bool {
	if sp == nil || sp.schema == nil || sp.schema.Value == nil {
		return false
	}
	idx := sp.schema.Value.GetIndex()
	if idx == nil || idx.GetConfig() == nil || idx.GetConfig().SpecInfo == nil {
		return true
	}
	return idx.GetConfig().SpecInfo.VersionNumeric >= 3.1
}

func yamlNodeFromRender(rendered interface{}) (*yaml.Node, bool) {
	switch node := rendered.(type) {
	case *yaml.Node:
		return node, node != nil
	case yaml.Node:
		return &node, true
	default:
		return nil, false
	}
}

func findYAMLPair(node *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if node == nil || !utils.IsNodeMap(node) {
		return nil, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i] != nil && node.Content[i].Value == key {
			return node.Content[i], node.Content[i+1]
		}
	}
	return nil, nil
}

// Render will return a YAML representation of the Schema object as a byte slice.
func (sp *SchemaProxy) Render() ([]byte, error) {
	return yaml.Marshal(sp)
}

// MarshalYAML will create a ready to render YAML representation of the SchemaProxy object.
func (sp *SchemaProxy) MarshalYAML() (interface{}, error) {
	if sp.isParsedRefWithSiblings() {
		return sp.referenceYAMLNodeForSchema(nil)
	}
	if !sp.IsReference() {
		s, err := sp.BuildSchema()
		if err != nil {
			return nil, err
		}
		if node, ok, renderErr := sp.renderTransformedRefWithSiblings(s); ok || renderErr != nil {
			return node, renderErr
		}
		nb := high.NewNodeBuilder(s, s.low)
		return nb.Render(), nil
	}
	if sp.isRefWithSiblings() {
		return sp.renderRefWithSiblings(), nil
	}
	return sp.GetReferenceNode(), nil
}

func (sp *SchemaProxy) referenceYAMLNode() (*yaml.Node, error) {
	return sp.referenceYAMLNodeForSchema(nil)
}

func (sp *SchemaProxy) sourceIndex() *index.SpecIndex {
	if sp == nil || sp.schema == nil || sp.schema.Value == nil {
		return nil
	}
	return sp.schema.Value.GetIndex()
}

func (sp *SchemaProxy) referenceSourceIndex(ctx *InlineRenderContext) *index.SpecIndex {
	if ctx != nil {
		if source, ok := ctx.ReferenceNodeSource(sp.GetReferenceNode()); ok {
			return source
		}
	}
	return sp.sourceIndex()
}

func (sp *SchemaProxy) referenceSourcePath(ctx *InlineRenderContext) string {
	if source := sp.referenceSourceIndex(ctx); source != nil {
		return source.GetSpecAbsolutePath()
	}
	return ""
}

func (sp *SchemaProxy) isRootLocalReference(ctx *InlineRenderContext) bool {
	if sp == nil || !strings.HasPrefix(sp.GetReference(), "#/") {
		return false
	}
	source := sp.referenceSourceIndex(ctx)
	if source == nil {
		return false
	}
	if ctx != nil && ctx.RootIndex != nil {
		return source == ctx.RootIndex
	}
	return source.GetRolodex() != nil && source.GetRolodex().GetRootIndex() == source
}

func (sp *SchemaProxy) referenceTargetIdentity(ctx *InlineRenderContext) string {
	if ctx != nil {
		if target, ok := ctx.ReferenceNodeTarget(sp.GetReferenceNode()); ok {
			return index.CanonicalReferenceIdentity(target)
		}
	}
	ref := sp.GetReference()
	if strings.HasPrefix(ref, "#") {
		if source := sp.sourceIndex(); source != nil {
			return index.CanonicalReferenceIdentity(source.GetSpecAbsolutePath() + ref)
		}
	}
	return ""
}

// rewrittenReferenceNode clones the authored reference node and applies the
// source-qualified replacement for this render, if one was prepared. Cloning
// preserves $ref siblings and source metadata without mutating indexed nodes.
func (sp *SchemaProxy) rewrittenReferenceNode(ctx *InlineRenderContext) (*yaml.Node, bool, error) {
	node, err := sp.referenceYAMLNode()
	if node == nil {
		return nil, false, err
	}
	replacement, ok := ctx.ReferenceNodeRewrite(node)
	if !ok {
		replacement, ok = ctx.referenceRewrite(sp.referenceSourcePath(ctx), sp.GetReference())
	}
	if !ok {
		return node, false, err
	}
	cloned := utils.CloneYAMLNode(node)
	refValue := utils.GetRefValueNode(cloned)
	if refValue == nil {
		return nil, false, errors.Join(err, errors.New("unable to rewrite schema reference: $ref value node is missing"))
	}
	refValue.Value = replacement
	return cloned, true, err
}

func (sp *SchemaProxy) circularReferenceResult(ctx *InlineRenderContext, node *yaml.Node, rewritten bool, refErr, circularErr error) (*yaml.Node, error) {
	if rewritten || sp.isRootLocalReference(ctx) {
		return node, refErr
	}
	return node, errors.Join(refErr, circularErr)
}

func (sp *SchemaProxy) referenceYAMLNodeForSchema(s *Schema) (*yaml.Node, error) {
	if sp.isRefWithSiblings() {
		return sp.renderRefWithSiblings(), nil
	}
	if sp.isParsedRefWithSiblings() {
		if s == nil {
			var err error
			s, err = sp.BuildSchema()
			if err != nil {
				return nil, err
			}
		}
		if node, ok, renderErr := sp.renderTransformedRefWithSiblings(s); ok || renderErr != nil {
			return node, renderErr
		}
	}
	return sp.GetReferenceNode(), nil
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
	if sp.isParsedRefWithSiblings() && sp.schema.ValueNode != nil {
		node := sp.schema.ValueNode
		idx := sp.schema.Value.GetIndex()
		if node.Line > 0 && node.Column > 0 {
			source := "inline"
			if idx != nil {
				source = idx.GetSpecAbsolutePath()
			}
			return fmt.Sprintf("%s:%d:%d", source, node.Line, node.Column)
		}
		source := "inline"
		if idx != nil {
			source = fmt.Sprintf("%s:inline", idx.GetSpecAbsolutePath())
		}
		return fmt.Sprintf("%s:%p", source, node)
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
	refNode := func() (*yaml.Node, error) {
		return sp.referenceYAMLNode()
	}
	rewrittenRefNode := func() (*yaml.Node, bool, error) {
		return sp.rewrittenReferenceNode(ctx)
	}

	// check if this reference should be preserved (set via context by discriminator handling).
	// this avoids mutating shared SchemaProxy state and prevents race conditions.
	// need to guard against nil schema.Value which can happen with bad/incomplete proxies.
	if sp.IsReference() {
		ref := sp.GetReference()
		refNode := sp.GetReferenceNode()
		preserve := ctx.ShouldPreserveReferenceNode(refNode)
		if !preserve {
			preserve = ctx.shouldPreserveRef(sp.referenceSourcePath(ctx), ref) || ctx.ShouldPreserveRef(ref)
		}
		if ref != "" && preserve {
			node, _, err := rewrittenRefNode()
			return node, err
		}
	}

	// In bundling mode, preserve local component refs that point to schemas in the SAME document.
	// Only inline refs that point to schemas from EXTERNAL files.
	// Outside of bundling mode (direct MarshalYAMLInline calls), inline everything.
	if ctx.PreserveLocalComponentRefs && sp.IsReference() {
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
						if rootIdx != nil && sp.isRootLocalReference(ctx) {
							return refNode()
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
			node, rewritten, refErr := rewrittenRefNode()
			return sp.circularReferenceResult(ctx, node, rewritten, refErr,
				fmt.Errorf("schema render failure, circular reference: `%s`", sp.GetReference()))
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
				if c == nil || c.LoopPoint == nil {
					continue
				}
				if ctx.StrictCircularReferenceIdentity {
					target := sp.referenceTargetIdentity(ctx)
					if target == "" || target != index.CanonicalReferenceIdentity(c.LoopPoint.FullDefinition) {
						continue
					}
					node, rewritten, refErr := rewrittenRefNode()
					return sp.circularReferenceResult(ctx, node, rewritten, refErr, cirError(c.LoopPoint.Definition))
				}
				if sp.GetReference() == c.LoopPoint.Definition {
					node, rewritten, refErr := rewrittenRefNode()
					return sp.circularReferenceResult(ctx, node, rewritten, refErr, cirError(c.LoopPoint.Definition))
				}
				basePath := idx.GetSpecAbsolutePath()

				if !filepath.IsAbs(basePath) && !strings.HasPrefix(basePath, "http") {
					basePath, _ = filepath.Abs(basePath)
				}

				if basePath == c.LoopPoint.FullDefinition {
					node, rewritten, refErr := rewrittenRefNode()
					return sp.circularReferenceResult(ctx, node, rewritten, refErr, cirError(c.LoopPoint.Definition))
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
					node, rewritten, refErr := rewrittenRefNode()
					return sp.circularReferenceResult(ctx, node, rewritten, refErr, cirError(c.LoopPoint.Definition))
				}

				if aFragment == "" && bFragment == "" {
					aNorm := strings.TrimPrefix(strings.TrimPrefix(aBase, "./"), "/")
					bNorm := strings.TrimPrefix(strings.TrimPrefix(bBase, "./"), "/")
					if aNorm != "" && bNorm != "" && aNorm == bNorm {
						node, rewritten, refErr := rewrittenRefNode()
						return sp.circularReferenceResult(ctx, node, rewritten, refErr, cirError(c.LoopPoint.Definition))
					}
				}
			}
		}
	}

	if err != nil {
		return nil, err
	}
	if s != nil {
		if sp.isParsedRefWithSiblings() {
			return sp.marshalParsedRefWithSiblingsInline(ctx, s)
		}
		// For programmatic ref+siblings proxies, render directly to avoid nil-deref
		// in Schema.MarshalYAMLInlineWithContext which assumes s.GoLow() is non-nil.
		if sp.isRefWithSiblings() {
			return sp.renderRefWithSiblings(), nil
		}
		// Delegate to Schema.MarshalYAMLInlineWithContext to ensure discriminator handling is applied
		// and cycle detection context is propagated.
		// Schema.MarshalYAMLInlineWithContext sets preserveReference on OneOf/AnyOf items when
		// a discriminator is present, which is required for proper bundling.
		return s.MarshalYAMLInlineWithContext(ctx)
	}
	return nil, errors.New("unable to render schema")
}

func (sp *SchemaProxy) marshalParsedRefWithSiblingsInline(ctx *InlineRenderContext, currentSibling *Schema) (interface{}, error) {
	s, err := sp.buildSemanticAllOfSchemaView(currentSibling)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("unable to render transformed schema reference")
	}
	return s.MarshalYAMLInlineWithContext(ctx)
}

package base

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
	"golang.org/x/sync/errgroup"
)

// SchemaDynamicValue is used to hold multiple possible types for a schema property. There are two values, a left
// value (A) and a right value (B). The A and B values represent different types that a property can have,
// not necessarily different OpenAPI versions.
//
// For example:
//   - additionalProperties: A = *SchemaProxy (when it's a schema), B = bool (when it's a boolean)
//   - items: A = *SchemaProxy (when it's a schema), B = bool (when it's a boolean in 3.1)
//   - type: A = string (single type), B = []ValueReference[string] (multiple types in 3.1)
//   - exclusiveMinimum: A = bool (in 3.0), B = float64 (in 3.1)
//
// The N value indicates which value is set (0 = A, 1 = B), preventing the need to check both values.
type SchemaDynamicValue[A any, B any] struct {
	N int // 0 == A, 1 == B
	A A
	B B
}

// IsA will return true if the 'A' or left value is set.
func (s *SchemaDynamicValue[A, B]) IsA() bool {
	return s.N == 0
}

// IsB will return true if the 'B' or right value is set.
func (s *SchemaDynamicValue[A, B]) IsB() bool {
	return s.N == 1
}

// Hash will generate a stable hash of the SchemaDynamicValue
func (s *SchemaDynamicValue[A, B]) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if s.IsA() {
			h.WriteString(low.GenerateHashString(s.A))
		} else {
			h.WriteString(low.GenerateHashString(s.B))
		}
		return h.Sum64()
	})
}

// Schema represents a JSON Schema that support Swagger, OpenAPI 3 and OpenAPI 3.1
//
// Until 3.1 OpenAPI had a strange relationship with JSON Schema. It's been a super-set/sub-set
// mix, which has been confusing. So, instead of building a bunch of different models, we have compressed
// all variations into a single model that makes it easy to support multiple spec types.
//
//   - v2 schema: https://swagger.io/specification/v2/#schemaObject
//   - v3 schema: https://swagger.io/specification/#schema-object
//   - v3.1 schema: https://spec.openapis.org/oas/v3.1.0#schema-object
type Schema struct {
	// Reference to the '$schema' dialect setting (3.1 only)
	SchemaTypeRef low.NodeReference[string]

	// In versions 2 and 3.0, this ExclusiveMaximum can only be a boolean.
	ExclusiveMaximum low.NodeReference[*SchemaDynamicValue[bool, float64]]

	// In versions 2 and 3.0, this ExclusiveMinimum can only be a boolean.
	ExclusiveMinimum low.NodeReference[*SchemaDynamicValue[bool, float64]]

	// In versions 2 and 3.0, this Type is a single value, so array will only ever have one value
	// in version 3.1, Type can be multiple values
	Type low.NodeReference[SchemaDynamicValue[string, []low.ValueReference[string]]]

	// Schemas are resolved on demand using a SchemaProxy
	AllOf low.NodeReference[[]low.ValueReference[*SchemaProxy]]

	// Polymorphic Schemas are only available in version 3+
	OneOf         low.NodeReference[[]low.ValueReference[*SchemaProxy]]
	AnyOf         low.NodeReference[[]low.ValueReference[*SchemaProxy]]
	Discriminator low.NodeReference[*Discriminator]

	// in 3.1 examples can be an array (which is recommended)
	Examples low.NodeReference[[]low.ValueReference[*yaml.Node]]
	// in 3.1 PrefixItems provides tuple validation using prefixItems.
	PrefixItems low.NodeReference[[]low.ValueReference[*SchemaProxy]]
	// in 3.1 Contains is used by arrays and points to a Schema.
	Contains    low.NodeReference[*SchemaProxy]
	MinContains low.NodeReference[int64]
	MaxContains low.NodeReference[int64]

	// items can be a schema in 2.0, 3.0 and 3.1 or a bool in 3.1
	Items low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]

	// 3.1 only
	If                low.NodeReference[*SchemaProxy]
	Else              low.NodeReference[*SchemaProxy]
	Then              low.NodeReference[*SchemaProxy]
	DependentSchemas  low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]
	DependentRequired low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]]

	PatternProperties     low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]
	PropertyNames         low.NodeReference[*SchemaProxy]
	UnevaluatedItems      low.NodeReference[*SchemaProxy]
	UnevaluatedProperties low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]
	Id                    low.NodeReference[string] // JSON Schema 2020-12 $id - schema resource identifier
	Anchor                low.NodeReference[string]
	DynamicAnchor         low.NodeReference[string]
	DynamicRef            low.NodeReference[string]

	// Compatible with all versions
	Title                low.NodeReference[string]
	MultipleOf           low.NodeReference[float64]
	Maximum              low.NodeReference[float64]
	Minimum              low.NodeReference[float64]
	MaxLength            low.NodeReference[int64]
	MinLength            low.NodeReference[int64]
	Pattern              low.NodeReference[string]
	Format               low.NodeReference[string]
	MaxItems             low.NodeReference[int64]
	MinItems             low.NodeReference[int64]
	UniqueItems          low.NodeReference[bool]
	MaxProperties        low.NodeReference[int64]
	MinProperties        low.NodeReference[int64]
	Required             low.NodeReference[[]low.ValueReference[string]]
	Enum                 low.NodeReference[[]low.ValueReference[*yaml.Node]]
	Not                  low.NodeReference[*SchemaProxy]
	Properties           low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]
	AdditionalProperties low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]
	Description          low.NodeReference[string]
	ContentEncoding      low.NodeReference[string]
	ContentMediaType     low.NodeReference[string]
	ContentSchema        low.NodeReference[*SchemaProxy]                                                        // JSON Schema 2020-12 contentSchema
	Comment              low.NodeReference[string]                                                              // JSON Schema 2020-12 $comment
	Vocabulary           low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[bool]]] // JSON Schema 2020-12 $vocabulary
	Default              low.NodeReference[*yaml.Node]
	Const                low.NodeReference[*yaml.Node]
	Nullable             low.NodeReference[bool]
	ReadOnly             low.NodeReference[bool]
	WriteOnly            low.NodeReference[bool]
	XML                  low.NodeReference[*XML]
	ExternalDocs         low.NodeReference[*ExternalDoc]
	Example              low.NodeReference[*yaml.Node]
	Deprecated           low.NodeReference[bool]
	Extensions           *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]

	// Parent Proxy refers back to the low level SchemaProxy that is proxying this schema.
	ParentProxy *SchemaProxy

	// Index is a reference to the SpecIndex that was used to build this schema.
	Index    *index.SpecIndex
	RootNode *yaml.Node
	index    *index.SpecIndex
	context  context.Context
	*low.Reference
	low.NodeMap
}

// GetIndex will return the index.SpecIndex instance attached to the Schema object
func (s *Schema) GetIndex() *index.SpecIndex {
	return s.index
}

// GetContext will return the context.Context instance used when building the Schema object
func (s *Schema) GetContext() context.Context {
	return s.context
}

// QuickHash will calculate a hash from the values of the schema, however the hash is not very deep
// and is used for quick equality checking, This method exists because a full hash could end up churning through
// thousands of polymorphic references. With a quick hash, polymorphic properties are not included.
func (s *Schema) QuickHash() uint64 {
	return s.hash(true)
}

// Hash will calculate a hash from the values of the schema, This allows equality checking against
// Schemas defined inside an OpenAPI document. The only way to know if a schema has changed, is to hash it.
func (s *Schema) Hash() uint64 {
	return s.hash(false)
}

// SchemaQuickHashMap is a sync.Map used to store quick hashes of schemas, used by quick hashing to prevent
// over rotation on the same schema. This map is automatically reset each time `CompareDocuments` is called by the
// `what-changed` package and each time a model is built via `BuildV3Model()` etc.
//
// This exists because to ensure deep equality checking when composing schemas using references. However this
// can cause an exhaustive deep hash calculation that chews up compute like crazy, particularly with polymorphic refs.
// The hash map means each schema is hashed once, and then the hash is reused for quick equality checking.
var SchemaQuickHashMap sync.Map

// ClearSchemaQuickHashMap resets the schema quick-hash cache.
// Call this between document lifecycles in long-running processes to bound memory.
func ClearSchemaQuickHashMap() {
	SchemaQuickHashMap.Clear()
}

func (s *Schema) hash(quick bool) uint64 {
	if s == nil {
		return 0
	}

	// create a key for the schema, this is used to quickly check if the schema has been hashed before, and prevent re-hashing.
	idx := s.GetIndex()
	path := ""
	if idx != nil {
		path = idx.GetSpecAbsolutePath()
	}
	cfId := "root"
	if s.Index != nil {
		if s.Index.GetRolodex() != nil {
			if s.Index.GetRolodex().GetId() != "" {
				cfId = s.Index.GetRolodex().GetId()
			}
		} else {
			cfId = s.Index.GetConfig().GetId()
		}
	}
	var keyBuf strings.Builder
	keyBuf.Grow(len(path) + len(cfId) + 16)
	keyBuf.WriteString(path)
	keyBuf.WriteByte(':')
	keyBuf.WriteString(strconv.Itoa(s.RootNode.Line))
	keyBuf.WriteByte(':')
	keyBuf.WriteString(strconv.Itoa(s.RootNode.Column))
	keyBuf.WriteByte(':')
	keyBuf.WriteString(cfId)
	key := keyBuf.String()
	if quick {
		if v, ok := SchemaQuickHashMap.Load(key); ok {
			if r, k := v.(uint64); k {
				return r
			}
		}
	}

	// Use string builder pool for efficient string concatenation
	sb := low.GetStringBuilder()
	defer low.PutStringBuilder(sb)

	// calculate a hash from every property in the schema.
	if !s.SchemaTypeRef.IsEmpty() {
		sb.WriteString(s.SchemaTypeRef.Value)
		sb.WriteByte('|')
	}
	if !s.Title.IsEmpty() {
		sb.WriteString(s.Title.Value)
		sb.WriteByte('|')
	}
	if !s.MultipleOf.IsEmpty() {
		sb.WriteString(strconv.FormatFloat(s.MultipleOf.Value, 'g', -1, 64))
		sb.WriteByte('|')
	}
	if !s.Maximum.IsEmpty() {
		sb.WriteString(strconv.FormatFloat(s.Maximum.Value, 'g', -1, 64))
		sb.WriteByte('|')
	}
	if !s.Minimum.IsEmpty() {
		sb.WriteString(strconv.FormatFloat(s.Minimum.Value, 'g', -1, 64))
		sb.WriteByte('|')
	}
	if !s.MaxLength.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MaxLength.Value, 10))
		sb.WriteByte('|')
	}
	if !s.MinLength.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MinLength.Value, 10))
		sb.WriteByte('|')
	}
	if !s.Pattern.IsEmpty() {
		sb.WriteString(s.Pattern.Value)
		sb.WriteByte('|')
	}
	if !s.Format.IsEmpty() {
		sb.WriteString(s.Format.Value)
		sb.WriteByte('|')
	}
	if !s.MaxItems.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MaxItems.Value, 10))
		sb.WriteByte('|')
	}
	if !s.MinItems.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MinItems.Value, 10))
		sb.WriteByte('|')
	}
	if !s.UniqueItems.IsEmpty() {
		sb.WriteString(strconv.FormatBool(s.UniqueItems.Value))
		sb.WriteByte('|')
	}
	if !s.MaxProperties.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MaxProperties.Value, 10))
		sb.WriteByte('|')
	}
	if !s.MinProperties.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MinProperties.Value, 10))
		sb.WriteByte('|')
	}
	if !s.AdditionalProperties.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.AdditionalProperties.Value))
		sb.WriteByte('|')
	}
	if !s.Description.IsEmpty() {
		sb.WriteString(s.Description.Value)
		sb.WriteByte('|')
	}
	if !s.ContentEncoding.IsEmpty() {
		sb.WriteString(s.ContentEncoding.Value)
		sb.WriteByte('|')
	}
	if !s.ContentMediaType.IsEmpty() {
		sb.WriteString(s.ContentMediaType.Value)
		sb.WriteByte('|')
	}
	if !s.Default.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.Default.Value))
		sb.WriteByte('|')
	}
	if !s.Const.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.Const.Value))
		sb.WriteByte('|')
	}
	if !s.Nullable.IsEmpty() {
		sb.WriteString(strconv.FormatBool(s.Nullable.Value))
		sb.WriteByte('|')
	}
	if !s.ReadOnly.IsEmpty() {
		sb.WriteString(strconv.FormatBool(s.ReadOnly.Value))
		sb.WriteByte('|')
	}
	if !s.WriteOnly.IsEmpty() {
		sb.WriteString(strconv.FormatBool(s.WriteOnly.Value))
		sb.WriteByte('|')
	}
	if !s.Deprecated.IsEmpty() {
		sb.WriteString(strconv.FormatBool(s.Deprecated.Value))
		sb.WriteByte('|')
	}
	if !s.ExclusiveMaximum.IsEmpty() && s.ExclusiveMaximum.Value.IsA() {
		sb.WriteString(strconv.FormatBool(s.ExclusiveMaximum.Value.A))
		sb.WriteByte('|')
	}
	if !s.ExclusiveMaximum.IsEmpty() && s.ExclusiveMaximum.Value.IsB() {
		sb.WriteString(strconv.FormatFloat(s.ExclusiveMaximum.Value.B, 'g', -1, 64))
		sb.WriteByte('|')
	}
	if !s.ExclusiveMinimum.IsEmpty() && s.ExclusiveMinimum.Value.IsA() {
		sb.WriteString(strconv.FormatBool(s.ExclusiveMinimum.Value.A))
		sb.WriteByte('|')
	}
	if !s.ExclusiveMinimum.IsEmpty() && s.ExclusiveMinimum.Value.IsB() {
		sb.WriteString(strconv.FormatFloat(s.ExclusiveMinimum.Value.B, 'g', -1, 64))
		sb.WriteByte('|')
	}
	if !s.Type.IsEmpty() && s.Type.Value.IsA() {
		sb.WriteString(s.Type.Value.A)
		sb.WriteByte('|')
	}
	if !s.Type.IsEmpty() && s.Type.Value.IsB() {
		// Pre-allocate slice for Type.B values
		j := make([]string, len(s.Type.Value.B))
		for h := range s.Type.Value.B {
			j[h] = s.Type.Value.B[h].Value
		}
		sort.Strings(j)
		for _, val := range j {
			sb.WriteString(val)
		}
		sb.WriteByte('|')
	}

	// Process Required values
	if len(s.Required.Value) > 0 {
		keys := make([]string, len(s.Required.Value))
		for i := range s.Required.Value {
			keys[i] = s.Required.Value[i].Value
		}
		sort.Strings(keys)
		for _, key := range keys {
			sb.WriteString(key)
			sb.WriteByte('|')
		}
	}

	// Process Enum values
	if len(s.Enum.Value) > 0 {
		keys := make([]string, len(s.Enum.Value))
		for i := range s.Enum.Value {
			keys[i] = low.ValueToString(s.Enum.Value[i].Value)
		}
		sort.Strings(keys)
		for _, key := range keys {
			sb.WriteString(key)
			sb.WriteByte('|')
		}
	}

	// Append map hashes using helper function
	for _, hash := range low.AppendMapHashes(nil, s.Properties.Value) {
		sb.WriteString(hash)
		sb.WriteByte('|')
	}

	if s.XML.Value != nil {
		sb.WriteString(low.GenerateHashString(s.XML.Value))
		sb.WriteByte('|')
	}
	if s.ExternalDocs.Value != nil {
		sb.WriteString(low.GenerateHashString(s.ExternalDocs.Value))
		sb.WriteByte('|')
	}
	if s.Discriminator.Value != nil {
		sb.WriteString(low.GenerateHashString(s.Discriminator.Value))
		sb.WriteByte('|')
	}

	// hash polymorphic data - OneOf
	if len(s.OneOf.Value) > 0 {
		oneOfKeys := make([]string, len(s.OneOf.Value))
		for i := range s.OneOf.Value {
			oneOfKeys[i] = low.GenerateHashString(s.OneOf.Value[i].Value)
		}
		sort.Strings(oneOfKeys)
		for _, key := range oneOfKeys {
			sb.WriteString(key)
			sb.WriteByte('|')
		}
	}

	// hash polymorphic data - AllOf
	if len(s.AllOf.Value) > 0 {
		allOfKeys := make([]string, len(s.AllOf.Value))
		for i := range s.AllOf.Value {
			allOfKeys[i] = low.GenerateHashString(s.AllOf.Value[i].Value)
		}
		sort.Strings(allOfKeys)
		for _, key := range allOfKeys {
			sb.WriteString(key)
			sb.WriteByte('|')
		}
	}

	// hash polymorphic data - AnyOf
	if len(s.AnyOf.Value) > 0 {
		anyOfKeys := make([]string, len(s.AnyOf.Value))
		for i := range s.AnyOf.Value {
			anyOfKeys[i] = low.GenerateHashString(s.AnyOf.Value[i].Value)
		}
		sort.Strings(anyOfKeys)
		for _, key := range anyOfKeys {
			sb.WriteString(key)
			sb.WriteByte('|')
		}
	}

	if !s.Not.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.Not.Value))
		sb.WriteByte('|')
	}

	// check if items is a schema or a bool.
	if !s.Items.IsEmpty() && s.Items.Value.IsA() {
		sb.WriteString(low.GenerateHashString(s.Items.Value.A))
		sb.WriteByte('|')
	}
	if !s.Items.IsEmpty() && s.Items.Value.IsB() {
		sb.WriteString(strconv.FormatBool(s.Items.Value.B))
		sb.WriteByte('|')
	}
	// 3.1 only props
	if !s.If.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.If.Value))
		sb.WriteByte('|')
	}
	if !s.Else.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.Else.Value))
		sb.WriteByte('|')
	}
	if !s.Then.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.Then.Value))
		sb.WriteByte('|')
	}
	if !s.PropertyNames.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.PropertyNames.Value))
		sb.WriteByte('|')
	}
	if !s.UnevaluatedProperties.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.UnevaluatedProperties.Value))
		sb.WriteByte('|')
	}
	if !s.UnevaluatedItems.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.UnevaluatedItems.Value))
		sb.WriteByte('|')
	}
	if !s.Id.IsEmpty() {
		sb.WriteString(s.Id.Value)
		sb.WriteByte('|')
	}
	if !s.Anchor.IsEmpty() {
		sb.WriteString(s.Anchor.Value)
		sb.WriteByte('|')
	}
	if !s.DynamicAnchor.IsEmpty() {
		sb.WriteString(s.DynamicAnchor.Value)
		sb.WriteByte('|')
	}
	if !s.DynamicRef.IsEmpty() {
		sb.WriteString(s.DynamicRef.Value)
		sb.WriteByte('|')
	}
	if !s.Comment.IsEmpty() {
		sb.WriteString(s.Comment.Value)
		sb.WriteByte('|')
	}
	if !s.ContentSchema.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.ContentSchema.Value))
		sb.WriteByte('|')
	}
	if s.Vocabulary.Value != nil {
		// sort vocabulary keys for deterministic hashing
		// pre-allocate with known size for better memory efficiency
		vocabSize := orderedmap.Len(s.Vocabulary.Value)
		vocabKeys := make([]string, 0, vocabSize)
		vocabMap := make(map[string]bool, vocabSize)
		for k, v := range s.Vocabulary.Value.FromOldest() {
			vocabKeys = append(vocabKeys, k.Value)
			vocabMap[k.Value] = v.Value
		}
		sort.Strings(vocabKeys)
		for _, k := range vocabKeys {
			sb.WriteString(k)
			sb.WriteByte(':')
			sb.WriteString(strconv.FormatBool(vocabMap[k]))
			sb.WriteByte('|')
		}
	}

	// Process dependent schemas and pattern properties
	for _, hash := range low.AppendMapHashes(nil, orderedmap.SortAlpha(s.DependentSchemas.Value)) {
		sb.WriteString(hash)
		sb.WriteByte('|')
	}

	// Process dependent required
	if s.DependentRequired.Value != nil {
		// Sort keys for deterministic hashing
		var depReqKeys []string
		depReqMap := make(map[string][]string)
		for prop, requiredProps := range s.DependentRequired.Value.FromOldest() {
			depReqKeys = append(depReqKeys, prop.Value)
			depReqMap[prop.Value] = requiredProps.Value
		}
		sort.Strings(depReqKeys)

		for _, prop := range depReqKeys {
			sb.WriteString(prop)
			sb.WriteByte(':')
			requiredProps := depReqMap[prop]
			for i, reqProp := range requiredProps {
				sb.WriteString(reqProp)
				if i < len(requiredProps)-1 {
					sb.WriteByte(',')
				}
			}
			sb.WriteByte('|')
		}
	}

	for _, hash := range low.AppendMapHashes(nil, orderedmap.SortAlpha(s.PatternProperties.Value)) {
		sb.WriteString(hash)
		sb.WriteByte('|')
	}

	// Process PrefixItems
	if len(s.PrefixItems.Value) > 0 {
		itemsKeys := make([]string, len(s.PrefixItems.Value))
		for i := range s.PrefixItems.Value {
			itemsKeys[i] = low.GenerateHashString(s.PrefixItems.Value[i].Value)
		}
		sort.Strings(itemsKeys)
		for _, key := range itemsKeys {
			sb.WriteString(key)
			sb.WriteByte('|')
		}
	}

	// Process extensions
	for _, ext := range low.HashExtensions(s.Extensions) {
		sb.WriteString(ext)
		sb.WriteByte('|')
	}

	if s.Example.Value != nil {
		sb.WriteString(low.GenerateHashString(s.Example.Value))
		sb.WriteByte('|')
	}

	// contains
	if !s.Contains.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.Contains.Value))
		sb.WriteByte('|')
	}
	if !s.MinContains.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MinContains.Value, 10))
		sb.WriteByte('|')
	}
	if !s.MaxContains.IsEmpty() {
		sb.WriteString(strconv.FormatInt(s.MaxContains.Value, 10))
		sb.WriteByte('|')
	}
	if !s.Examples.IsEmpty() {
		for _, ex := range s.Examples.Value {
			sb.WriteString(low.GenerateHashString(ex.Value))
			sb.WriteByte('|')
		}
	}

	h := low.WithHasher(func(hasher *maphash.Hash) uint64 {
		hasher.WriteString(sb.String())
		return hasher.Sum64()
	})
	SchemaQuickHashMap.Store(key, h)
	return h
}

// FindProperty will return a ValueReference pointer containing a SchemaProxy pointer
// from a property key name. if found
func (s *Schema) FindProperty(name string) *low.ValueReference[*SchemaProxy] {
	return low.FindItemInOrderedMap[*SchemaProxy](name, s.Properties.Value)
}

// FindDependentSchema will return a ValueReference pointer containing a SchemaProxy pointer
// from a dependent schema key name. if found (3.1+ only)
func (s *Schema) FindDependentSchema(name string) *low.ValueReference[*SchemaProxy] {
	return low.FindItemInOrderedMap[*SchemaProxy](name, s.DependentSchemas.Value)
}

// FindPatternProperty will return a ValueReference pointer containing a SchemaProxy pointer
// from a pattern property key name. if found (3.1+ only)
func (s *Schema) FindPatternProperty(name string) *low.ValueReference[*SchemaProxy] {
	return low.FindItemInOrderedMap[*SchemaProxy](name, s.PatternProperties.Value)
}

// GetExtensions returns all extensions for Schema
func (s *Schema) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return s.Extensions
}

// GetRootNode will return the root yaml node of the Schema object
func (s *Schema) GetRootNode() *yaml.Node {
	return s.RootNode
}

// Build will perform a number of operations.
// Extraction of the following happens in this method:
//   - Extensions
//   - Type
//   - ExclusiveMinimum and ExclusiveMaximum
//   - Examples
//   - AdditionalProperties
//   - Discriminator
//   - ExternalDocs
//   - XML
//   - Properties
//   - AllOf, OneOf, AnyOf
//   - Not
//   - Items
//   - PrefixItems
//   - If
//   - Else
//   - Then
//   - DependentSchemas
//   - PatternProperties
//   - PropertyNames
//   - UnevaluatedItems
//   - UnevaluatedProperties
//   - Anchor
func (s *Schema) Build(ctx context.Context, root *yaml.Node, idx *index.SpecIndex) error {
	if root == nil {
		return fmt.Errorf("cannot build schema from a nil node")
	}
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)

	// Note: sibling ref transformation now happens in SchemaProxy.Build()
	// so the root node should already be pre-transformed if needed

	s.Reference = new(low.Reference)
	no := low.ExtractNodes(ctx, root)
	s.Nodes = no
	s.Index = idx
	s.RootNode = root
	s.context = ctx
	s.index = idx

	// check if this schema was transformed from a sibling ref
	// if so, skip reference dereferencing to preserve the allOf structure
	isTransformed := false
	if s.ParentProxy != nil && s.ParentProxy.TransformedRef != nil {
		isTransformed = true
	}

	if !isTransformed {
		if h, _, _ := utils.IsNodeRefValue(root); h {
			ref, _, err, fctx := low.LocateRefNodeWithContext(ctx, root, idx)
			if ref != nil {
				root = ref
				if fctx != nil {
					ctx = fctx
				}
				if err != nil {
					if !idx.AllowCircularReferenceResolving() {
						return fmt.Errorf("build schema failed: %s", err.Error())
					}
				}
			} else {
				return fmt.Errorf("build schema failed: reference cannot be found: '%s', line %d, col %d",
					root.Content[1].Value, root.Content[1].Line, root.Content[1].Column)
			}
		}
	}

	// Build model using possibly dereferenced root
	if err := low.BuildModel(root, s); err != nil {
		return err
	}

	s.extractExtensions(root)

	// if the schema has required values, extract the nodes for them.
	if s.Required.Value != nil {
		for _, r := range s.Required.Value {
			s.AddNode(r.ValueNode.Line, r.ValueNode)
		}
	}

	// same thing with enums
	if s.Enum.Value != nil {
		for _, e := range s.Enum.Value {
			s.AddNode(e.ValueNode.Line, e.ValueNode)
		}
	}

	// determine schema type, singular (3.0) or multiple (3.1), use a variable value
	_, typeLabel, typeValue := utils.FindKeyNodeFullTop(TypeLabel, root.Content)
	if typeValue != nil {
		if utils.IsNodeStringValue(typeValue) {
			s.Type = low.NodeReference[SchemaDynamicValue[string, []low.ValueReference[string]]]{
				KeyNode:   typeLabel,
				ValueNode: typeValue,
				Value:     SchemaDynamicValue[string, []low.ValueReference[string]]{N: 0, A: typeValue.Value},
			}
		}
		if utils.IsNodeArray(typeValue) {

			var refs []low.ValueReference[string]
			for r := range typeValue.Content {
				refs = append(refs, low.ValueReference[string]{
					Value:     typeValue.Content[r].Value,
					ValueNode: typeValue.Content[r],
				})
			}
			s.Type = low.NodeReference[SchemaDynamicValue[string, []low.ValueReference[string]]]{
				KeyNode:   typeLabel,
				ValueNode: typeValue,
				Value:     SchemaDynamicValue[string, []low.ValueReference[string]]{N: 1, B: refs},
			}
		}
	}

	// determine exclusive minimum type, bool (3.0) or int (3.1)
	_, exMinLabel, exMinValue := utils.FindKeyNodeFullTop(ExclusiveMinimumLabel, root.Content)
	if exMinValue != nil {
		// if there is an index, determine if this a 3.0 or 3.1+ schema
		if idx != nil {
			if idx.GetConfig().SpecInfo.VersionNumeric >= 3.1 {
				val, _ := strconv.ParseFloat(exMinValue.Value, 64)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
			if idx.GetConfig().SpecInfo.VersionNumeric <= 3.0 {
				val, _ := strconv.ParseBool(exMinValue.Value)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
		} else {

			// there is no index, so we have to determine the type based on the value
			if utils.IsNodeBoolValue(exMinValue) {
				val, _ := strconv.ParseBool(exMinValue.Value)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
			if utils.IsNodeIntValue(exMinValue) {
				val, _ := strconv.ParseFloat(exMinValue.Value, 64)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
		}
	}

	// determine exclusive maximum type, bool (3.0) or int (3.1+)
	_, exMaxLabel, exMaxValue := utils.FindKeyNodeFullTop(ExclusiveMaximumLabel, root.Content)
	if exMaxValue != nil {
		// if there is an index, determine if this a 3.0 or 3.1+ schema
		if idx != nil {
			if idx.GetConfig().SpecInfo.VersionNumeric >= 3.1 {
				val, _ := strconv.ParseFloat(exMaxValue.Value, 64)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
			if idx.GetConfig().SpecInfo.VersionNumeric <= 3.0 {
				val, _ := strconv.ParseBool(exMaxValue.Value)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
		} else {

			// there is no index, so we have to determine the type based on the value
			if utils.IsNodeBoolValue(exMaxValue) {
				val, _ := strconv.ParseBool(exMaxValue.Value)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
			if utils.IsNodeIntValue(exMaxValue) {
				val, _ := strconv.ParseFloat(exMaxValue.Value, 64)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
		}
	}

	// handle schema reference type if set. (3.1)
	_, schemaRefLabel, schemaRefNode := utils.FindKeyNodeFullTop(SchemaTypeLabel, root.Content)
	if schemaRefNode != nil {
		s.SchemaTypeRef = low.NodeReference[string]{
			Value: schemaRefNode.Value, KeyNode: schemaRefLabel, ValueNode: schemaRefNode,
		}
	}

	// handle $id if set. (3.1+, JSON Schema 2020-12)
	_, idLabel, idNode := utils.FindKeyNodeFullTop(IdLabel, root.Content)
	if idNode != nil {
		s.Id = low.NodeReference[string]{
			Value: idNode.Value, KeyNode: idLabel, ValueNode: idNode,
		}
	}

	// handle anchor if set. (3.1)
	_, anchorLabel, anchorNode := utils.FindKeyNodeFullTop(AnchorLabel, root.Content)
	if anchorNode != nil {
		s.Anchor = low.NodeReference[string]{
			Value: anchorNode.Value, KeyNode: anchorLabel, ValueNode: anchorNode,
		}
	}

	// handle $dynamicAnchor if set. (3.1+, JSON Schema 2020-12)
	_, dynamicAnchorLabel, dynamicAnchorNode := utils.FindKeyNodeFullTop(DynamicAnchorLabel, root.Content)
	if dynamicAnchorNode != nil {
		s.DynamicAnchor = low.NodeReference[string]{
			Value: dynamicAnchorNode.Value, KeyNode: dynamicAnchorLabel, ValueNode: dynamicAnchorNode,
		}
	}

	// handle $dynamicRef if set. (3.1+, JSON Schema 2020-12)
	_, dynamicRefLabel, dynamicRefNode := utils.FindKeyNodeFullTop(DynamicRefLabel, root.Content)
	if dynamicRefNode != nil {
		s.DynamicRef = low.NodeReference[string]{
			Value: dynamicRefNode.Value, KeyNode: dynamicRefLabel, ValueNode: dynamicRefNode,
		}
	}

	// handle $comment if set. (JSON Schema 2020-12)
	_, commentLabel, commentNode := utils.FindKeyNodeFullTop(CommentLabel, root.Content)
	if commentNode != nil {
		s.Comment = low.NodeReference[string]{
			Value: commentNode.Value, KeyNode: commentLabel, ValueNode: commentNode,
		}
	}

	// handle $vocabulary if set. (JSON Schema 2020-12 - typically in meta-schemas)
	_, vocabLabel, vocabNode := utils.FindKeyNodeFullTop(VocabularyLabel, root.Content)
	if vocabNode != nil && utils.IsNodeMap(vocabNode) {
		vocabularyMap := orderedmap.New[low.KeyReference[string], low.ValueReference[bool]]()
		var currentKey *yaml.Node
		for i, node := range vocabNode.Content {
			if i%2 == 0 {
				currentKey = node
				continue
			}
			// use strconv.ParseBool for robust boolean parsing (handles "true", "false", "1", "0", etc.)
			boolVal, _ := strconv.ParseBool(node.Value)
			vocabularyMap.Set(low.KeyReference[string]{
				KeyNode: currentKey,
				Value:   currentKey.Value,
			}, low.ValueReference[bool]{
				Value:     boolVal,
				ValueNode: node,
			})
		}
		s.Vocabulary = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[bool]]]{
			Value:     vocabularyMap,
			KeyNode:   vocabLabel,
			ValueNode: vocabNode,
		}
	}

	// handle example if set. (3.0)
	_, expLabel, expNode := utils.FindKeyNodeFullTop(ExampleLabel, root.Content)
	if expNode != nil {
		s.Example = low.NodeReference[*yaml.Node]{Value: expNode, KeyNode: expLabel, ValueNode: expNode}

		// extract nodes for all value nodes down the tree.
		expChildNodes := low.ExtractNodesRecursive(ctx, expNode)
		// map to the local schema
		expChildNodes.Range(func(k, v interface{}) bool {
			if arr, ko := v.([]*yaml.Node); ko {
				if _, ok := s.Nodes.Load(k); !ok {
					s.Nodes.Store(k, arr)
				}
			}
			return true
		})
	}

	// handle examples if set.(3.1)
	_, expArrLabel, expArrNode := utils.FindKeyNodeFullTop(ExamplesLabel, root.Content)
	if expArrNode != nil {
		if utils.IsNodeArray(expArrNode) {
			var examples []low.ValueReference[*yaml.Node]
			for i := range expArrNode.Content {
				examples = append(examples, low.ValueReference[*yaml.Node]{Value: expArrNode.Content[i], ValueNode: expArrNode.Content[i]})
			}
			s.Examples = low.NodeReference[[]low.ValueReference[*yaml.Node]]{
				Value:     examples,
				ValueNode: expArrNode,
				KeyNode:   expArrLabel,
			}
			// extract nodes for all value nodes down the tree.
			expChildNodes := low.ExtractNodesRecursive(ctx, expArrNode)
			// map to the local schema
			expChildNodes.Range(func(k, v interface{}) bool {
				if arr, ko := v.([]*yaml.Node); ko {
					if _, ok := s.Nodes.Load(k); !ok {
						s.Nodes.Store(k, arr)
					}
				}
				return true
			})
		}
	}

	// check additionalProperties type for schema or bool
	addPropsIsBool := false
	addPropsBoolValue := true
	_, addPLabel, addPValue := utils.FindKeyNodeFullTop(AdditionalPropertiesLabel, root.Content)
	if addPValue != nil {
		if utils.IsNodeBoolValue(addPValue) {
			addPropsIsBool = true
			addPropsBoolValue, _ = strconv.ParseBool(addPValue.Value)
		}
	}
	if addPropsIsBool {
		s.AdditionalProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				B: addPropsBoolValue,
				N: 1,
			},
			KeyNode:   addPLabel,
			ValueNode: addPValue,
		}
	}

	// handle discriminator if set.
	_, discLabel, discNode := utils.FindKeyNodeFullTop(DiscriminatorLabel, root.Content)
	if discNode != nil {
		var discriminator Discriminator
		_ = low.BuildModel(discNode, &discriminator)
		discriminator.KeyNode = discLabel
		discriminator.RootNode = discNode
		discriminator.Nodes = low.ExtractNodes(ctx, discNode)
		s.Discriminator = low.NodeReference[*Discriminator]{Value: &discriminator, KeyNode: discLabel, ValueNode: discNode}
		// add discriminator nodes, because there is no build method.
		dn := low.ExtractNodesRecursive(ctx, discNode)
		dn.Range(func(key, val any) bool {
			if n, ok := val.([]*yaml.Node); ok {
				for _, g := range n {
					discriminator.AddNode(key.(int), g)
				}
			}
			return true
		})
	}

	// handle externalDocs if set.
	_, extDocLabel, extDocNode := utils.FindKeyNodeFullTop(ExternalDocsLabel, root.Content)
	if extDocNode != nil {
		var exDoc ExternalDoc
		_ = low.BuildModel(extDocNode, &exDoc)
		_ = exDoc.Build(ctx, extDocLabel, extDocNode, idx) // throws no errors, can't check for one.
		exDoc.Nodes = low.ExtractNodes(ctx, extDocNode)
		s.ExternalDocs = low.NodeReference[*ExternalDoc]{Value: &exDoc, KeyNode: extDocLabel, ValueNode: extDocNode}
	}

	// handle xml if set.
	_, xmlLabel, xmlNode := utils.FindKeyNodeFullTop(XMLLabel, root.Content)
	if xmlNode != nil {
		var xml XML
		_ = low.BuildModel(xmlNode, &xml)
		// extract extensions if set.
		_ = xml.Build(xmlNode, idx) // returns no errors, can't check for one.
		xml.Nodes = low.ExtractNodes(ctx, xmlNode)
		s.XML = low.NodeReference[*XML]{Value: &xml, KeyNode: xmlLabel, ValueNode: xmlNode}
	}

	// handle properties
	props, err := buildPropertyMap(ctx, s, root, idx, PropertiesLabel)
	if err != nil {
		return err
	}
	if props != nil {
		s.Properties = *props
	}

	// handle dependent schemas
	props, err = buildPropertyMap(ctx, s, root, idx, DependentSchemasLabel)
	if err != nil {
		return err
	}
	if props != nil {
		s.DependentSchemas = *props
	}

	// handle dependent required
	depReq, err := buildDependentRequiredMap(root, DependentRequiredLabel)
	if err != nil {
		return err
	}
	if depReq != nil {
		s.DependentRequired = *depReq
	}

	// handle pattern properties
	props, err = buildPropertyMap(ctx, s, root, idx, PatternPropertiesLabel)
	if err != nil {
		return err
	}
	if props != nil {
		s.PatternProperties = *props
	}

	// check items type for schema or bool (3.1 only)
	itemsIsBool := false
	itemsBoolValue := false
	_, itemsLabel, itemsValue := utils.FindKeyNodeFullTop(ItemsLabel, root.Content)
	if itemsValue != nil {
		if utils.IsNodeBoolValue(itemsValue) {
			itemsIsBool = true
			itemsBoolValue, _ = strconv.ParseBool(itemsValue.Value)
		}
	}
	if itemsIsBool {
		s.Items = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				B: itemsBoolValue,
				N: 1,
			},
			KeyNode:   itemsLabel,
			ValueNode: itemsValue,
		}
	}

	// check unevaluatedProperties type for schema or bool (3.1 only)
	unevalIsBool := false
	unevalBoolValue := true
	_, unevalLabel, unevalValue := utils.FindKeyNodeFullTop(UnevaluatedPropertiesLabel, root.Content)
	if unevalValue != nil {
		if utils.IsNodeBoolValue(unevalValue) {
			unevalIsBool = true
			unevalBoolValue, _ = strconv.ParseBool(unevalValue.Value)
		}
	}
	if unevalIsBool {
		s.UnevaluatedProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				B: unevalBoolValue,
				N: 1,
			},
			KeyNode:   unevalLabel,
			ValueNode: unevalValue,
		}
	}

	var allOf, anyOf, oneOf, prefixItems []low.ValueReference[*SchemaProxy]
	var items, not, contains, sif, selse, sthen, propertyNames, unevalItems, unevalProperties, addProperties, contentSch low.ValueReference[*SchemaProxy]

	_, allOfLabel, allOfValue := utils.FindKeyNodeFullTop(AllOfLabel, root.Content)
	_, anyOfLabel, anyOfValue := utils.FindKeyNodeFullTop(AnyOfLabel, root.Content)
	_, oneOfLabel, oneOfValue := utils.FindKeyNodeFullTop(OneOfLabel, root.Content)
	_, notLabel, notValue := utils.FindKeyNodeFullTop(NotLabel, root.Content)
	_, prefixItemsLabel, prefixItemsValue := utils.FindKeyNodeFullTop(PrefixItemsLabel, root.Content)
	_, containsLabel, containsValue := utils.FindKeyNodeFullTop(ContainsLabel, root.Content)
	_, sifLabel, sifValue := utils.FindKeyNodeFullTop(IfLabel, root.Content)
	_, selseLabel, selseValue := utils.FindKeyNodeFullTop(ElseLabel, root.Content)
	_, sthenLabel, sthenValue := utils.FindKeyNodeFullTop(ThenLabel, root.Content)
	_, propNamesLabel, propNamesValue := utils.FindKeyNodeFullTop(PropertyNamesLabel, root.Content)
	_, unevalItemsLabel, unevalItemsValue := utils.FindKeyNodeFullTop(UnevaluatedItemsLabel, root.Content)
	// Reuse earlier lookups for unevaluatedProperties and additionalProperties instead of re-scanning.
	unevalPropsLabel, unevalPropsValue := unevalLabel, unevalValue
	addPropsLabel, addPropsValue := addPLabel, addPValue
	_, contentSchLabel, contentSchValue := utils.FindKeyNodeFullTop(ContentSchemaLabel, root.Content)

	if ctx == nil {
		ctx = context.Background()
	}

	g, gCtx := errgroup.WithContext(ctx)

	if allOfValue != nil {
		g.Go(func() error {
			res, err := buildSchemaList(gCtx, allOfLabel, allOfValue, idx)
			if err == nil {
				allOf = res
			}
			return err
		})
	}
	if anyOfValue != nil {
		g.Go(func() error {
			res, err := buildSchemaList(gCtx, anyOfLabel, anyOfValue, idx)
			if err == nil {
				anyOf = res
			}
			return err
		})
	}
	if oneOfValue != nil {
		g.Go(func() error {
			res, err := buildSchemaList(gCtx, oneOfLabel, oneOfValue, idx)
			if err == nil {
				oneOf = res
			}
			return err
		})
	}
	if prefixItemsValue != nil {
		g.Go(func() error {
			res, err := buildSchemaList(gCtx, prefixItemsLabel, prefixItemsValue, idx)
			if err == nil {
				prefixItems = res
			}
			return err
		})
	}
	if notValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, notLabel, notValue, idx)
			if err == nil {
				not = res
			}
			return err
		})
	}
	if containsValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, containsLabel, containsValue, idx)
			if err == nil {
				contains = res
			}
			return err
		})
	}
	if !itemsIsBool && itemsValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, itemsLabel, itemsValue, idx)
			if err == nil {
				items = res
			}
			return err
		})
	}
	if sifValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, sifLabel, sifValue, idx)
			if err == nil {
				sif = res
			}
			return err
		})
	}
	if selseValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, selseLabel, selseValue, idx)
			if err == nil {
				selse = res
			}
			return err
		})
	}
	if sthenValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, sthenLabel, sthenValue, idx)
			if err == nil {
				sthen = res
			}
			return err
		})
	}
	if propNamesValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, propNamesLabel, propNamesValue, idx)
			if err == nil {
				propertyNames = res
			}
			return err
		})
	}
	if unevalItemsValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, unevalItemsLabel, unevalItemsValue, idx)
			if err == nil {
				unevalItems = res
			}
			return err
		})
	}
	if !unevalIsBool && unevalPropsValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, unevalPropsLabel, unevalPropsValue, idx)
			if err == nil {
				unevalProperties = res
			}
			return err
		})
	}
	if !addPropsIsBool && addPropsValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, addPropsLabel, addPropsValue, idx)
			if err == nil {
				addProperties = res
			}
			return err
		})
	}
	if contentSchValue != nil {
		g.Go(func() error {
			res, err := buildSchema(gCtx, contentSchLabel, contentSchValue, idx)
			if err == nil {
				contentSch = res
			}
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}

	if len(anyOf) > 0 {
		s.AnyOf = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     anyOf,
			KeyNode:   anyOfLabel,
			ValueNode: anyOfValue,
		}
	}
	if len(oneOf) > 0 {
		s.OneOf = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     oneOf,
			KeyNode:   oneOfLabel,
			ValueNode: oneOfValue,
		}
	}
	if len(allOf) > 0 {
		s.AllOf = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     allOf,
			KeyNode:   allOfLabel,
			ValueNode: allOfValue,
		}
	}
	if !not.IsEmpty() {
		s.Not = low.NodeReference[*SchemaProxy]{
			Value:     not.Value,
			KeyNode:   notLabel,
			ValueNode: notValue,
		}
	}
	if !itemsIsBool && !items.IsEmpty() {
		s.Items = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				A: items.Value,
			},
			KeyNode:   itemsLabel,
			ValueNode: itemsValue,
		}
	}
	if len(prefixItems) > 0 {
		s.PrefixItems = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     prefixItems,
			KeyNode:   prefixItemsLabel,
			ValueNode: prefixItemsValue,
		}
	}
	if !contains.IsEmpty() {
		s.Contains = low.NodeReference[*SchemaProxy]{
			Value:     contains.Value,
			KeyNode:   containsLabel,
			ValueNode: containsValue,
		}
	}
	if !sif.IsEmpty() {
		s.If = low.NodeReference[*SchemaProxy]{
			Value:     sif.Value,
			KeyNode:   sifLabel,
			ValueNode: sifValue,
		}
	}
	if !selse.IsEmpty() {
		s.Else = low.NodeReference[*SchemaProxy]{
			Value:     selse.Value,
			KeyNode:   selseLabel,
			ValueNode: selseValue,
		}
	}
	if !sthen.IsEmpty() {
		s.Then = low.NodeReference[*SchemaProxy]{
			Value:     sthen.Value,
			KeyNode:   sthenLabel,
			ValueNode: sthenValue,
		}
	}
	if !propertyNames.IsEmpty() {
		s.PropertyNames = low.NodeReference[*SchemaProxy]{
			Value:     propertyNames.Value,
			KeyNode:   propNamesLabel,
			ValueNode: propNamesValue,
		}
	}
	if !unevalItems.IsEmpty() {
		s.UnevaluatedItems = low.NodeReference[*SchemaProxy]{
			Value:     unevalItems.Value,
			KeyNode:   unevalItemsLabel,
			ValueNode: unevalItemsValue,
		}
	}
	if !unevalIsBool && !unevalProperties.IsEmpty() {
		s.UnevaluatedProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				A: unevalProperties.Value,
			},
			KeyNode:   unevalPropsLabel,
			ValueNode: unevalPropsValue,
		}
	}
	if !addPropsIsBool && !addProperties.IsEmpty() {
		s.AdditionalProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				A: addProperties.Value,
			},
			KeyNode:   addPropsLabel,
			ValueNode: addPropsValue,
		}
	}
	if !contentSch.IsEmpty() {
		s.ContentSchema = low.NodeReference[*SchemaProxy]{
			Value:     contentSch.Value,
			KeyNode:   contentSchLabel,
			ValueNode: contentSchValue,
		}
	}
	return nil
}

func buildPropertyMap(ctx context.Context, parent *Schema, root *yaml.Node, idx *index.SpecIndex, label string) (*low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]], error) {
	_, propLabel, propsNode := utils.FindKeyNodeFullTop(label, root.Content)
	if propsNode != nil {
		propertyMap := orderedmap.New[low.KeyReference[string], low.ValueReference[*SchemaProxy]]()
		var currentProp *yaml.Node
		for i, prop := range propsNode.Content {
			if i%2 == 0 {
				currentProp = prop
				parent.Nodes.Store(prop.Line, prop)
				continue
			}

			foundCtx := ctx
			foundIdx := idx
			// check our prop isn't reference
			refString := ""
			var refNode *yaml.Node
			if h, _, l := utils.IsNodeRefValue(prop); h {
				ref, fIdx, err, fctx := low.LocateRefNodeWithContext(foundCtx, prop, foundIdx)
				if ref != nil {
					refNode = prop
					prop = ref
					refString = l
					foundCtx = fctx
					foundIdx = fIdx
				} else if errors.Is(err, low.ErrExternalRefSkipped) {
					refString = l
					refNode = prop
				} else {
					return nil, fmt.Errorf("schema properties build failed: cannot find reference %s, line %d, col %d",
						prop.Content[1].Value, prop.Content[1].Line, prop.Content[1].Column)
				}
			}

			sp := &SchemaProxy{ctx: foundCtx, kn: currentProp, vn: prop, idx: foundIdx}
			sp.SetReference(refString, refNode)

			_ = sp.Build(foundCtx, currentProp, prop, foundIdx)

			propertyMap.Set(low.KeyReference[string]{
				KeyNode: currentProp,
				Value:   currentProp.Value,
			}, low.ValueReference[*SchemaProxy]{
				Value:     sp,
				ValueNode: sp.vn, // use transformed node
			})
		}

		return &low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]{
			Value:     propertyMap,
			KeyNode:   propLabel,
			ValueNode: propsNode,
		}, nil
	}
	return nil, nil
}

// buildDependentRequiredMap builds an ordered map of string arrays for the dependentRequired property
func buildDependentRequiredMap(root *yaml.Node, label string) (*low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]], error) {
	_, propLabel, propsNode := utils.FindKeyNodeFullTop(label, root.Content)
	if propsNode != nil {
		dependentRequiredMap := orderedmap.New[low.KeyReference[string], low.ValueReference[[]string]]()
		var currentKey *yaml.Node
		for i, node := range propsNode.Content {
			if i%2 == 0 {
				currentKey = node
				continue
			}

			// node should be an array of strings
			if !utils.IsNodeArray(node) {
				return nil, fmt.Errorf("dependentRequired value must be an array, found %v at line %d, col %d",
					node.Kind, node.Line, node.Column)
			}

			var requiredProps []string
			for _, propNode := range node.Content {
				if propNode.Kind != yaml.ScalarNode {
					return nil, fmt.Errorf("dependentRequired array items must be strings, found %v at line %d, col %d",
						propNode.Kind, propNode.Line, propNode.Column)
				}
				requiredProps = append(requiredProps, propNode.Value)
			}

			dependentRequiredMap.Set(low.KeyReference[string]{
				KeyNode: currentKey,
				Value:   currentKey.Value,
			}, low.ValueReference[[]string]{
				Value:     requiredProps,
				ValueNode: node,
			})
		}

		return &low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]]{
			Value:     dependentRequiredMap,
			KeyNode:   propLabel,
			ValueNode: propsNode,
		}, nil
	}
	return nil, nil
}

// extract extensions from schema
func (s *Schema) extractExtensions(root *yaml.Node) {
	s.Extensions = low.ExtractExtensions(root)
}

// buildSchemaProxy builds out a SchemaProxy for a single node.
func buildSchemaProxy(ctx context.Context, idx *index.SpecIndex, kn, vn *yaml.Node, rf *yaml.Node, isRef bool, refLocation string) low.ValueReference[*SchemaProxy] {
	// a proxy design works best here. polymorphism, pretty much guarantees that a sub-schema can
	// take on circular references through polymorphism. Like the resolver, if we try and follow these
	// journey's through hyperspace, we will end up creating endless amounts of threads, spinning off
	// chasing down circles, that in turn spin up endless threads.
	// In order to combat this, we need a schema proxy that will only resolve the schema when asked, and then
	// it will only do it one level at a time.
	sp := new(SchemaProxy)

	// call Build to ensure transformation happens
	_ = sp.Build(ctx, kn, vn, idx)

	if isRef {
		sp.SetReference(refLocation, rf)
	}
	return low.ValueReference[*SchemaProxy]{
		Value:     sp,
		ValueNode: sp.vn, // use transformed node
	}
}

// buildSchema builds out a child schema for parent schema. Expected to be a singular schema object.
func buildSchema(ctx context.Context, labelNode, valueNode *yaml.Node, idx *index.SpecIndex) (low.ValueReference[*SchemaProxy], error) {
	if valueNode == nil {
		return low.ValueReference[*SchemaProxy]{}, nil
	}

	if !utils.IsNodeMap(valueNode) {
		return low.ValueReference[*SchemaProxy]{}, fmt.Errorf("build schema failed: expected a single schema object for '%s', but found an array or scalar at line %d, col %d",
			utils.MakeTagReadable(valueNode), valueNode.Line, valueNode.Column)
	}

	isRef := false
	refLocation := ""
	var refNode *yaml.Node
	foundCtx := ctx
	foundIdx := idx

	h := false
	if h, _, refLocation = utils.IsNodeRefValue(valueNode); h {
		isRef = true
		ref, fIdx, err, fctx := low.LocateRefNodeWithContext(foundCtx, valueNode, foundIdx)
		if ref != nil {
			refNode = valueNode
			valueNode = ref
			foundCtx = fctx
			foundIdx = fIdx
		} else if errors.Is(err, low.ErrExternalRefSkipped) {
			refNode = valueNode
		} else {
			return low.ValueReference[*SchemaProxy]{}, fmt.Errorf("build schema failed: reference cannot be found: %s, line %d, col %d",
				valueNode.Content[1].Value, valueNode.Content[1].Line, valueNode.Content[1].Column)
		}
	}

	return buildSchemaProxy(foundCtx, foundIdx, labelNode, valueNode, refNode, isRef, refLocation), nil
}

// buildSchemaList builds out child schemas for a parent schema. Expected to be an array of schema objects.
func buildSchemaList(ctx context.Context, labelNode, valueNode *yaml.Node, idx *index.SpecIndex) ([]low.ValueReference[*SchemaProxy], error) {
	if valueNode == nil {
		return nil, nil
	}

	if !utils.IsNodeArray(valueNode) {
		return nil, fmt.Errorf("build schema failed: expected an array of schemas for '%s', but found an object or scalar at line %d, col %d",
			utils.MakeTagReadable(valueNode), valueNode.Line, valueNode.Column)
	}

	results := make([]low.ValueReference[*SchemaProxy], 0, len(valueNode.Content))

	for _, vn := range valueNode.Content {
		isRef := false
		refLocation := ""
		var refNode *yaml.Node
		foundCtx := ctx
		foundIdx := idx

		h := false
		if h, _, refLocation = utils.IsNodeRefValue(vn); h {
			isRef = true
			ref, fIdx, err, fctx := low.LocateRefNodeWithContext(foundCtx, vn, foundIdx)
			if ref != nil {
				refNode = vn
				vn = ref
				foundCtx = fctx
				foundIdx = fIdx
			} else if errors.Is(err, low.ErrExternalRefSkipped) {
				refNode = vn
			} else {
				return nil, fmt.Errorf("build schema failed: reference cannot be found: %s, line %d, col %d",
					vn.Content[1].Value, vn.Content[1].Line, vn.Content[1].Column)
			}
		}

		r := buildSchemaProxy(foundCtx, foundIdx, vn, vn, refNode, isRef, refLocation)
		results = append(results, r)
	}

	return results, nil
}

// ExtractSchema will return a pointer to a NodeReference that contains a *SchemaProxy if successful. The function
// will specifically look for a key node named 'schema' and extract the value mapped to that key. If the operation
// fails then no NodeReference is returned and an error is returned instead.
func ExtractSchema(ctx context.Context, root *yaml.Node, idx *index.SpecIndex) (*low.NodeReference[*SchemaProxy], error) {
	var schLabel, schNode *yaml.Node
	errStr := "schema build failed: reference '%s' cannot be found at line %d, col %d"

	refLocation := ""
	var refNode *yaml.Node

	foundIndex := idx
	foundCtx := ctx
	if rf, rl, rv := utils.IsNodeRefValue(root); rf {
		// locate reference in index.
		ref, fIdx, err, nCtx := low.LocateRefNodeWithContext(ctx, root, idx)
		if ref != nil {
			schNode = ref
			schLabel = rl
			foundCtx = nCtx
			foundIndex = fIdx
		} else if errors.Is(err, low.ErrExternalRefSkipped) {
			refLocation = rv
			schema := &SchemaProxy{kn: root, vn: root, idx: idx, ctx: ctx}
			_ = schema.Build(ctx, root, root, idx)
			n := &low.NodeReference[*SchemaProxy]{Value: schema, KeyNode: root, ValueNode: root}
			n.SetReference(refLocation, root)
			schema.SetReference(refLocation, root)
			return n, nil
		} else {
			v := root.Content[1].Value
			if root.Content[1].Value == "" {
				v = "[empty]"
			}
			return nil, fmt.Errorf(errStr,
				v, root.Content[1].Line, root.Content[1].Column)
		}
	} else {
		_, schLabel, schNode = utils.FindKeyNodeFull(SchemaLabel, root.Content)
		if schNode != nil {
			h := false
			if h, _, refLocation = utils.IsNodeRefValue(schNode); h {
				ref, fIdx, lerr, nCtx := low.LocateRefNodeWithContext(foundCtx, schNode, foundIndex)
				if ref != nil {
					refNode = schNode
					schNode = ref
					if fIdx != nil {
						foundIndex = fIdx
					}
					foundCtx = nCtx
				} else if errors.Is(lerr, low.ErrExternalRefSkipped) {
					refNode = schNode
				} else {
					v := schNode.Content[1].Value
					if schNode.Content[1].Value == "" {
						v = "[empty]"
					}
					return nil, fmt.Errorf(errStr,
						v, schNode.Content[1].Line, schNode.Content[1].Column)
				}
			}
		}
	}

	if schNode != nil {
		// check if schema has already been built.
		schema := &SchemaProxy{kn: schLabel, vn: schNode, idx: foundIndex, ctx: foundCtx}

		// call Build to ensure transformation happens
		_ = schema.Build(foundCtx, schLabel, schNode, foundIndex)

		schema.SetReference(refLocation, refNode)

		n := &low.NodeReference[*SchemaProxy]{
			Value:     schema,
			KeyNode:   schLabel,
			ValueNode: schema.vn, // use transformed node
		}
		n.SetReference(refLocation, refNode)
		return n, nil
	}
	return nil, nil
}

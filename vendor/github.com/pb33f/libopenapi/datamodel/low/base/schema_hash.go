// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"hash/maphash"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
)

// Hash will generate a stable hash of the SchemaDynamicValue
func (s *SchemaDynamicValue[A, B]) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		// label the side that is set, so an A and a B that hash to the same string still hash differently.
		if s.IsA() {
			low.HashString(h, "a", low.GenerateHashString(s.A))
		} else {
			low.HashString(h, "b", low.GenerateHashString(s.B))
		}
		return h.Sum64()
	})
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

func (s *Schema) hash(quick bool) uint64 {
	if s == nil {
		return 0
	}

	key := ""
	if quick {
		key = s.quickHashKey()
		if v, ok := SchemaQuickHashMap.Load(key); ok {
			if r, k := v.(uint64); k {
				return r
			}
		}
	}

	h := low.WithHasher(func(hasher *maphash.Hash) uint64 {
		s.hashProperties(hasher)
		return hasher.Sum64()
	})
	if quick {
		SchemaQuickHashMap.Store(key, h)
	}
	return h
}

// hashProperties writes every property in the schema to h. Every value is written after the keyword it belongs to, so
// keywords holding the same value never hash the same: {minimum: 5} is not {maximum: 5}, and a oneOf is not an allOf
// over the same schemas.
func (s *Schema) hashProperties(h *maphash.Hash) {
	var scratch []string

	if !s.SchemaTypeRef.IsEmpty() {
		low.HashString(h, "$schema", s.SchemaTypeRef.Value)
	}
	if !s.Title.IsEmpty() {
		low.HashString(h, "title", s.Title.Value)
	}
	if !s.MultipleOf.IsEmpty() {
		hashSchemaFloat(h, "multipleOf", s.MultipleOf.Value)
	}
	if !s.Maximum.IsEmpty() {
		hashSchemaFloat(h, "maximum", s.Maximum.Value)
	}
	if !s.Minimum.IsEmpty() {
		hashSchemaFloat(h, "minimum", s.Minimum.Value)
	}
	if !s.MaxLength.IsEmpty() {
		hashSchemaInt(h, "maxLength", s.MaxLength.Value)
	}
	if !s.MinLength.IsEmpty() {
		hashSchemaInt(h, "minLength", s.MinLength.Value)
	}
	if !s.Pattern.IsEmpty() {
		low.HashString(h, "pattern", s.Pattern.Value)
	}
	if !s.Format.IsEmpty() {
		low.HashString(h, "format", s.Format.Value)
	}
	if !s.MaxItems.IsEmpty() {
		hashSchemaInt(h, "maxItems", s.MaxItems.Value)
	}
	if !s.MinItems.IsEmpty() {
		hashSchemaInt(h, "minItems", s.MinItems.Value)
	}
	if !s.UniqueItems.IsEmpty() {
		hashSchemaBool(h, "uniqueItems", s.UniqueItems.Value)
	}
	if !s.MaxProperties.IsEmpty() {
		hashSchemaInt(h, "maxProperties", s.MaxProperties.Value)
	}
	if !s.MinProperties.IsEmpty() {
		hashSchemaInt(h, "minProperties", s.MinProperties.Value)
	}
	if !s.AdditionalProperties.IsEmpty() {
		low.HashString(h, "additionalProperties", low.GenerateHashString(s.AdditionalProperties.Value))
	}
	if !s.Description.IsEmpty() {
		low.HashString(h, "description", s.Description.Value)
	}
	if !s.ContentEncoding.IsEmpty() {
		low.HashString(h, "contentEncoding", s.ContentEncoding.Value)
	}
	if !s.ContentMediaType.IsEmpty() {
		low.HashString(h, "contentMediaType", s.ContentMediaType.Value)
	}
	if !s.Default.IsEmpty() {
		low.HashString(h, "default", low.GenerateHashString(s.Default.Value))
	}
	if !s.Const.IsEmpty() {
		low.HashString(h, "const", low.GenerateHashString(s.Const.Value))
	}
	if !s.Nullable.IsEmpty() {
		hashSchemaBool(h, "nullable", s.Nullable.Value)
	}
	if !s.ReadOnly.IsEmpty() {
		hashSchemaBool(h, "readOnly", s.ReadOnly.Value)
	}
	if !s.WriteOnly.IsEmpty() {
		hashSchemaBool(h, "writeOnly", s.WriteOnly.Value)
	}
	if !s.Deprecated.IsEmpty() {
		hashSchemaBool(h, "deprecated", s.Deprecated.Value)
	}
	if !s.ExclusiveMaximum.IsEmpty() && s.ExclusiveMaximum.Value.IsA() {
		hashSchemaBool(h, "exclusiveMaximum", s.ExclusiveMaximum.Value.A)
	}
	if !s.ExclusiveMaximum.IsEmpty() && s.ExclusiveMaximum.Value.IsB() {
		hashSchemaFloat(h, "exclusiveMaximum", s.ExclusiveMaximum.Value.B)
	}
	if !s.ExclusiveMinimum.IsEmpty() && s.ExclusiveMinimum.Value.IsA() {
		hashSchemaBool(h, "exclusiveMinimum", s.ExclusiveMinimum.Value.A)
	}
	if !s.ExclusiveMinimum.IsEmpty() && s.ExclusiveMinimum.Value.IsB() {
		hashSchemaFloat(h, "exclusiveMinimum", s.ExclusiveMinimum.Value.B)
	}
	// a single type is written as a one entry list, as it means the same as a type array holding only that type.
	if !s.Type.IsEmpty() && s.Type.Value.IsA() {
		hashSchemaList(h, "type", []string{s.Type.Value.A}, true)
	}
	if !s.Type.IsEmpty() && s.Type.Value.IsB() {
		scratch = resizeSchemaHashScratch(scratch, len(s.Type.Value.B))
		for i := range s.Type.Value.B {
			scratch[i] = s.Type.Value.B[i].Value
		}
		hashSchemaList(h, "type", scratch, true)
	}

	if len(s.Required.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.Required.Value))
		for i := range s.Required.Value {
			scratch[i] = s.Required.Value[i].Value
		}
		hashSchemaList(h, "required", scratch, true)
	}

	if len(s.Enum.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.Enum.Value))
		for i := range s.Enum.Value {
			// hash the node rather than its text, so the tag counts and enum [1] is not enum ['1'].
			scratch[i] = low.GenerateHashString(s.Enum.Value[i].Value)
		}
		hashSchemaList(h, "enum", scratch, true)
	}

	low.HashMap(h, "properties", s.Properties.Value)

	if s.XML.Value != nil {
		low.HashString(h, "xml", low.GenerateHashString(s.XML.Value))
	}
	if s.ExternalDocs.Value != nil {
		low.HashString(h, "externalDocs", low.GenerateHashString(s.ExternalDocs.Value))
	}
	if s.Discriminator.Value != nil {
		low.HashString(h, "discriminator", low.GenerateHashString(s.Discriminator.Value))
	}

	if len(s.OneOf.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.OneOf.Value))
		for i := range s.OneOf.Value {
			scratch[i] = low.GenerateHashString(s.OneOf.Value[i].Value)
		}
		hashSchemaList(h, "oneOf", scratch, true)
	}

	if len(s.AllOf.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.AllOf.Value))
		for i := range s.AllOf.Value {
			scratch[i] = low.GenerateHashString(s.AllOf.Value[i].Value)
		}
		hashSchemaList(h, "allOf", scratch, true)
	}

	if len(s.AnyOf.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.AnyOf.Value))
		for i := range s.AnyOf.Value {
			scratch[i] = low.GenerateHashString(s.AnyOf.Value[i].Value)
		}
		hashSchemaList(h, "anyOf", scratch, true)
	}

	if !s.Not.IsEmpty() {
		low.HashString(h, "not", low.GenerateHashString(s.Not.Value))
	}

	if !s.Items.IsEmpty() && s.Items.Value.IsA() {
		low.HashString(h, "items", low.GenerateHashString(s.Items.Value.A))
	}
	if !s.Items.IsEmpty() && s.Items.Value.IsB() {
		hashSchemaBool(h, "items", s.Items.Value.B)
	}
	if !s.If.IsEmpty() {
		low.HashString(h, "if", low.GenerateHashString(s.If.Value))
	}
	if !s.Else.IsEmpty() {
		low.HashString(h, "else", low.GenerateHashString(s.Else.Value))
	}
	if !s.Then.IsEmpty() {
		low.HashString(h, "then", low.GenerateHashString(s.Then.Value))
	}
	if !s.PropertyNames.IsEmpty() {
		low.HashString(h, "propertyNames", low.GenerateHashString(s.PropertyNames.Value))
	}
	if !s.UnevaluatedProperties.IsEmpty() {
		low.HashString(h, "unevaluatedProperties", low.GenerateHashString(s.UnevaluatedProperties.Value))
	}
	if !s.UnevaluatedItems.IsEmpty() {
		low.HashString(h, "unevaluatedItems", low.GenerateHashString(s.UnevaluatedItems.Value))
	}
	if !s.Id.IsEmpty() {
		low.HashString(h, "$id", s.Id.Value)
	}
	if !s.Anchor.IsEmpty() {
		low.HashString(h, "$anchor", s.Anchor.Value)
	}
	if !s.DynamicAnchor.IsEmpty() {
		low.HashString(h, "$dynamicAnchor", s.DynamicAnchor.Value)
	}
	if !s.DynamicRef.IsEmpty() {
		low.HashString(h, "$dynamicRef", s.DynamicRef.Value)
	}
	if !s.Comment.IsEmpty() {
		low.HashString(h, "$comment", s.Comment.Value)
	}
	if !s.ContentSchema.IsEmpty() {
		low.HashString(h, "contentSchema", low.GenerateHashString(s.ContentSchema.Value))
	}
	low.HashMap(h, "$vocabulary", s.Vocabulary.Value)

	low.HashMap(h, "dependentSchemas", s.DependentSchemas.Value)

	hashSchemaDependentRequired(h, "dependentRequired", s.DependentRequired.Value)

	low.HashMap(h, "patternProperties", s.PatternProperties.Value)

	low.HashMap(h, "$defs", s.Defs.Value)

	// prefixItems is positional, so unlike the other schema lists its order is part of the hash.
	if len(s.PrefixItems.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.PrefixItems.Value))
		for i := range s.PrefixItems.Value {
			scratch[i] = low.GenerateHashString(s.PrefixItems.Value[i].Value)
		}
		hashSchemaList(h, "prefixItems", scratch, false)
	}

	low.HashMap(h, "extensions", s.Extensions)

	if s.Example.Value != nil {
		low.HashString(h, "example", low.GenerateHashString(s.Example.Value))
	}

	if !s.Contains.IsEmpty() {
		low.HashString(h, "contains", low.GenerateHashString(s.Contains.Value))
	}
	if !s.MinContains.IsEmpty() {
		hashSchemaInt(h, "minContains", s.MinContains.Value)
	}
	if !s.MaxContains.IsEmpty() {
		hashSchemaInt(h, "maxContains", s.MaxContains.Value)
	}
	if len(s.Examples.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.Examples.Value))
		for i := range s.Examples.Value {
			scratch[i] = low.GenerateHashString(s.Examples.Value[i].Value)
		}
		hashSchemaList(h, "examples", scratch, false)
	}
}

// hashSchemaBool, hashSchemaInt and hashSchemaFloat write a value as text, the same way low.HashString writes a
// string. A keyword that holds a bool in one OpenAPI version and a number or a schema in another, like
// exclusiveMinimum or items, therefore hashes each form apart.
func hashSchemaBool(h *maphash.Hash, label string, value bool) {
	low.HashString(h, label, strconv.FormatBool(value))
}

func hashSchemaInt(h *maphash.Hash, label string, value int64) {
	var buf [20]byte
	hashSchemaText(h, label, strconv.AppendInt(buf[:0], value, 10))
}

func hashSchemaFloat(h *maphash.Hash, label string, value float64) {
	var buf [32]byte
	hashSchemaText(h, label, strconv.AppendFloat(buf[:0], value, 'g', -1, 64))
}

// hashSchemaText is low.HashString for text that is already formatted into a byte slice.
func hashSchemaText(h *maphash.Hash, label string, text []byte) {
	low.HashLabel(h, label)
	low.HashInt64(h, int64(len(text)))
	h.Write(text)
	h.WriteByte(low.HASH_PIPE)
}

// hashSchemaList writes a keyword and its list of values. Lists whose order means nothing are sorted first, so
// declaring their values in another order does not change the hash.
func hashSchemaList(h *maphash.Hash, label string, values []string, sorted bool) {
	if len(values) == 0 {
		return
	}
	if sorted {
		sort.Strings(values)
	}

	low.HashLabel(h, label)
	low.HashInt64(h, int64(len(values)))
	for _, value := range values {
		low.HashInt64(h, int64(len(value)))
		h.WriteString(value)
	}
	h.WriteByte(low.HASH_PIPE)
}

func resizeSchemaHashScratch(scratch []string, size int) []string {
	if cap(scratch) < size {
		return make([]string, size)
	}
	return scratch[:size]
}

func hashSchemaDependentRequired(h *maphash.Hash, label string, m *orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]) {
	if m == nil || m.Len() == 0 {
		return
	}

	type entry struct {
		key    string
		values []string
	}

	entries := make([]entry, 0, m.Len())
	for k, v := range m.FromOldest() {
		entries = append(entries, entry{
			key:    k.Value,
			values: v.Value,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	low.HashLabel(h, label)
	low.HashInt64(h, int64(len(entries)))
	for _, entry := range entries {
		low.HashInt64(h, int64(len(entry.key)))
		h.WriteString(entry.key)
		low.HashInt64(h, int64(len(entry.values)))
		for _, value := range entry.values {
			low.HashInt64(h, int64(len(value)))
			h.WriteString(value)
		}
	}
	h.WriteByte(low.HASH_PIPE)
}

func (s *Schema) quickHashKey() string {
	return quickHashKeyOf(s.refIndex, s.Index, s.RootNode)
}

// quickHashKeyOf builds the quick-hash key for a schema whose root node is root, read from the file of
// refIndex, in the document of idx. The key identifies a node by file plus position, so the path must come
// from the file root is in. Index may have been re-attributed to the file a $ref resolves to, which would
// pair a resolved-file path with a referring-file line and column: two different $refs at the same position
// in different files pointing into one shared file would then collide and return each other's hash.
// refIndex is assigned unconditionally in Build alongside Index, so it is nil only when the schema was never
// built, in which case Index is nil too and there is no path either way.
func quickHashKeyOf(refIndex, idx *index.SpecIndex, root *yaml.Node) string {
	path := ""
	if refIndex != nil {
		path = refIndex.GetSpecAbsolutePath()
	}
	cfID := "root"
	if idx != nil {
		if idx.GetRolodex() != nil {
			if idx.GetRolodex().GetId() != "" {
				cfID = idx.GetRolodex().GetId()
			}
		} else {
			cfID = idx.GetConfig().GetId()
		}
	}

	var keyBuf strings.Builder
	keyBuf.Grow(len(path) + len(cfID) + 16)
	keyBuf.WriteString(path)
	keyBuf.WriteByte(':')
	keyBuf.WriteString(strconv.Itoa(root.Line))
	keyBuf.WriteByte(':')
	keyBuf.WriteString(strconv.Itoa(root.Column))
	keyBuf.WriteByte(':')
	keyBuf.WriteString(cfID)
	return keyBuf.String()
}

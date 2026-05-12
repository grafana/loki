// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"hash/maphash"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

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

	// Use string builder pool for efficient string concatenation
	sb := low.GetStringBuilder()
	defer low.PutStringBuilder(sb)
	var scratch []string

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
		scratch = resizeSchemaHashScratch(scratch, len(s.Type.Value.B))
		for h := range s.Type.Value.B {
			scratch[h] = s.Type.Value.B[h].Value
		}
		writeSortedSchemaStrings(sb, scratch, false)
	}

	if len(s.Required.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.Required.Value))
		for i := range s.Required.Value {
			scratch[i] = s.Required.Value[i].Value
		}
		writeSortedSchemaStrings(sb, scratch, true)
	}

	if len(s.Enum.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.Enum.Value))
		for i := range s.Enum.Value {
			scratch[i] = low.ValueToString(s.Enum.Value[i].Value)
		}
		writeSortedSchemaStrings(sb, scratch, true)
	}

	writeSchemaMapHashes(sb, s.Properties.Value)

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

	if len(s.OneOf.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.OneOf.Value))
		for i := range s.OneOf.Value {
			scratch[i] = low.GenerateHashString(s.OneOf.Value[i].Value)
		}
		writeSortedSchemaStrings(sb, scratch, true)
	}

	if len(s.AllOf.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.AllOf.Value))
		for i := range s.AllOf.Value {
			scratch[i] = low.GenerateHashString(s.AllOf.Value[i].Value)
		}
		writeSortedSchemaStrings(sb, scratch, true)
	}

	if len(s.AnyOf.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.AnyOf.Value))
		for i := range s.AnyOf.Value {
			scratch[i] = low.GenerateHashString(s.AnyOf.Value[i].Value)
		}
		writeSortedSchemaStrings(sb, scratch, true)
	}

	if !s.Not.IsEmpty() {
		sb.WriteString(low.GenerateHashString(s.Not.Value))
		sb.WriteByte('|')
	}

	if !s.Items.IsEmpty() && s.Items.Value.IsA() {
		sb.WriteString(low.GenerateHashString(s.Items.Value.A))
		sb.WriteByte('|')
	}
	if !s.Items.IsEmpty() && s.Items.Value.IsB() {
		sb.WriteString(strconv.FormatBool(s.Items.Value.B))
		sb.WriteByte('|')
	}
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
	writeSchemaBoolMap(sb, s.Vocabulary.Value)

	writeSchemaMapHashes(sb, s.DependentSchemas.Value)

	writeSchemaDependentRequired(sb, s.DependentRequired.Value)

	writeSchemaMapHashes(sb, s.PatternProperties.Value)

	if len(s.PrefixItems.Value) > 0 {
		scratch = resizeSchemaHashScratch(scratch, len(s.PrefixItems.Value))
		for i := range s.PrefixItems.Value {
			scratch[i] = low.GenerateHashString(s.PrefixItems.Value[i].Value)
		}
		writeSortedSchemaStrings(sb, scratch, true)
	}

	writeSchemaExtensions(sb, s.Extensions)

	if s.Example.Value != nil {
		sb.WriteString(low.GenerateHashString(s.Example.Value))
		sb.WriteByte('|')
	}

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
	if quick {
		SchemaQuickHashMap.Store(key, h)
	}
	return h
}

func writeSchemaMapHashes[V any](sb *strings.Builder, m *orderedmap.Map[low.KeyReference[string], low.ValueReference[V]]) {
	if m == nil || m.Len() == 0 {
		return
	}

	type entry struct {
		key   string
		value V
	}

	entries := make([]entry, 0, m.Len())
	for k, v := range m.FromOldest() {
		entries = append(entries, entry{
			key:   k.Value,
			value: v.Value,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	for _, entry := range entries {
		sb.WriteString(entry.key)
		sb.WriteByte('-')
		sb.WriteString(low.GenerateHashString(entry.value))
		sb.WriteByte('|')
	}
}

func resizeSchemaHashScratch(scratch []string, size int) []string {
	if cap(scratch) < size {
		return make([]string, size)
	}
	return scratch[:size]
}

func writeSortedSchemaStrings(sb *strings.Builder, values []string, separate bool) {
	if len(values) == 0 {
		return
	}

	sort.Strings(values)
	for _, value := range values {
		sb.WriteString(value)
		if separate {
			sb.WriteByte('|')
		}
	}
	if !separate {
		sb.WriteByte('|')
	}
}

func writeSchemaBoolMap(sb *strings.Builder, m *orderedmap.Map[low.KeyReference[string], low.ValueReference[bool]]) {
	if m == nil || m.Len() == 0 {
		return
	}

	type entry struct {
		key   string
		value bool
	}

	entries := make([]entry, 0, m.Len())
	for k, v := range m.FromOldest() {
		entries = append(entries, entry{
			key:   k.Value,
			value: v.Value,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	for _, entry := range entries {
		sb.WriteString(entry.key)
		sb.WriteByte(':')
		sb.WriteString(strconv.FormatBool(entry.value))
		sb.WriteByte('|')
	}
}

func writeSchemaDependentRequired(sb *strings.Builder, m *orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]) {
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

	for _, entry := range entries {
		sb.WriteString(entry.key)
		sb.WriteByte(':')
		for i, value := range entry.values {
			sb.WriteString(value)
			if i < len(entry.values)-1 {
				sb.WriteByte(',')
			}
		}
		sb.WriteByte('|')
	}
}

func writeSchemaExtensions(sb *strings.Builder, ext *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]) {
	if ext == nil || ext.Len() == 0 {
		return
	}

	type entry struct {
		key  string
		node *yaml.Node
	}

	entries := make([]entry, 0, ext.Len())
	for k, v := range ext.FromOldest() {
		entries = append(entries, entry{
			key:  k.Value,
			node: v.Value,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	for _, entry := range entries {
		sb.WriteString(entry.key)
		sb.WriteByte('-')
		sb.WriteString(low.GenerateHashString(entry.node))
		sb.WriteByte('|')
	}
}

func (s *Schema) quickHashKey() string {
	idx := s.GetIndex()
	path := ""
	if idx != nil {
		path = idx.GetSpecAbsolutePath()
	}
	cfID := "root"
	if s.Index != nil {
		if s.Index.GetRolodex() != nil {
			if s.Index.GetRolodex().GetId() != "" {
				cfID = s.Index.GetRolodex().GetId()
			}
		} else {
			cfID = s.Index.GetConfig().GetId()
		}
	}

	var keyBuf strings.Builder
	keyBuf.Grow(len(path) + len(cfID) + 16)
	keyBuf.WriteString(path)
	keyBuf.WriteByte(':')
	keyBuf.WriteString(strconv.Itoa(s.RootNode.Line))
	keyBuf.WriteByte(':')
	keyBuf.WriteString(strconv.Itoa(s.RootNode.Column))
	keyBuf.WriteByte(':')
	keyBuf.WriteString(cfID)
	return keyBuf.String()
}

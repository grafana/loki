// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"context"
	"hash/maphash"
	"sort"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Items is a low-level representation of a Swagger / OpenAPI 2 Items object.
//
// Items is a limited subset of JSON-Schema's items object. It is used by parameter definitions that are not
// located in "body". Items, is actually identical to a Header, except it does not have description.
//   - https://swagger.io/specification/v2/#itemsObject
type Items struct {
	Type             low.NodeReference[string]
	Format           low.NodeReference[string]
	CollectionFormat low.NodeReference[string]
	Items            low.NodeReference[*Items]
	Default          low.NodeReference[*yaml.Node]
	Maximum          low.NodeReference[int]
	ExclusiveMaximum low.NodeReference[bool]
	Minimum          low.NodeReference[int]
	ExclusiveMinimum low.NodeReference[bool]
	MaxLength        low.NodeReference[int]
	MinLength        low.NodeReference[int]
	Pattern          low.NodeReference[string]
	MaxItems         low.NodeReference[int]
	MinItems         low.NodeReference[int]
	UniqueItems      low.NodeReference[bool]
	Enum             low.NodeReference[[]low.ValueReference[*yaml.Node]]
	MultipleOf       low.NodeReference[int]
	Extensions       *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
}

// FindExtension will attempt to locate an extension value using a name lookup.
func (i *Items) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, i.Extensions)
}

// GetExtensions returns all Items extensions and satisfies the low.HasExtensions interface.
func (i *Items) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return i.Extensions
}

// Hash will return a consistent Hash of the Items object
func (itm *Items) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if itm.Type.Value != "" {
			h.WriteString(itm.Type.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if itm.Format.Value != "" {
			h.WriteString(itm.Format.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if itm.CollectionFormat.Value != "" {
			h.WriteString(itm.CollectionFormat.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if itm.Default.Value != nil && !itm.Default.Value.IsZero() {
			h.WriteString(low.GenerateHashString(itm.Default.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		low.HashInt64(h, int64(itm.Maximum.Value))
		h.WriteByte(low.HASH_PIPE)
		low.HashInt64(h, int64(itm.Minimum.Value))
		h.WriteByte(low.HASH_PIPE)
		low.HashBool(h, itm.ExclusiveMinimum.Value)
		h.WriteByte(low.HASH_PIPE)
		low.HashBool(h, itm.ExclusiveMaximum.Value)
		h.WriteByte(low.HASH_PIPE)
		low.HashInt64(h, int64(itm.MinLength.Value))
		h.WriteByte(low.HASH_PIPE)
		low.HashInt64(h, int64(itm.MaxLength.Value))
		h.WriteByte(low.HASH_PIPE)
		low.HashInt64(h, int64(itm.MinItems.Value))
		h.WriteByte(low.HASH_PIPE)
		low.HashInt64(h, int64(itm.MaxItems.Value))
		h.WriteByte(low.HASH_PIPE)
		low.HashInt64(h, int64(itm.MultipleOf.Value))
		h.WriteByte(low.HASH_PIPE)
		low.HashBool(h, itm.UniqueItems.Value)
		h.WriteByte(low.HASH_PIPE)
		if itm.Pattern.Value != "" {
			h.WriteString(itm.Pattern.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		keys := make([]string, len(itm.Enum.Value))
		for k := range itm.Enum.Value {
			keys[k] = low.ValueToString(itm.Enum.Value[k].Value)
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.WriteString(key)
			h.WriteByte(low.HASH_PIPE)
		}

		if itm.Items.Value != nil {
			h.WriteString(low.GenerateHashString(itm.Items.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(itm.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

// Build will build out items and default value.
func (i *Items) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	i.Extensions = low.ExtractExtensions(root)
	items, iErr := low.ExtractObject[*Items](ctx, ItemsLabel, root, idx)
	if iErr != nil {
		return iErr
	}
	i.Items = items

	_, ln, vn := utils.FindKeyNodeFull(DefaultLabel, root.Content)
	if vn != nil {
		i.Default = low.NodeReference[*yaml.Node]{
			Value:     vn,
			KeyNode:   ln,
			ValueNode: vn,
		}
		return nil
	}
	return nil
}

// IsHeader compliance methods

func (i *Items) GetType() *low.NodeReference[string] {
	return &i.Type
}

func (i *Items) GetFormat() *low.NodeReference[string] {
	return &i.Format
}

func (i *Items) GetItems() *low.NodeReference[any] {
	k := low.NodeReference[any]{
		KeyNode:   i.Items.KeyNode,
		ValueNode: i.Items.ValueNode,
		Value:     i.Items.Value,
	}
	return &k
}

func (i *Items) GetCollectionFormat() *low.NodeReference[string] {
	return &i.CollectionFormat
}

func (i *Items) GetDescription() *low.NodeReference[string] {
	return nil // not implemented, but required to align with header contract
}

func (i *Items) GetDefault() *low.NodeReference[*yaml.Node] {
	return &i.Default
}

func (i *Items) GetMaximum() *low.NodeReference[int] {
	return &i.Maximum
}

func (i *Items) GetExclusiveMaximum() *low.NodeReference[bool] {
	return &i.ExclusiveMaximum
}

func (i *Items) GetMinimum() *low.NodeReference[int] {
	return &i.Minimum
}

func (i *Items) GetExclusiveMinimum() *low.NodeReference[bool] {
	return &i.ExclusiveMinimum
}

func (i *Items) GetMaxLength() *low.NodeReference[int] {
	return &i.MaxLength
}

func (i *Items) GetMinLength() *low.NodeReference[int] {
	return &i.MinLength
}

func (i *Items) GetPattern() *low.NodeReference[string] {
	return &i.Pattern
}

func (i *Items) GetMaxItems() *low.NodeReference[int] {
	return &i.MaxItems
}

func (i *Items) GetMinItems() *low.NodeReference[int] {
	return &i.MinItems
}

func (i *Items) GetUniqueItems() *low.NodeReference[bool] {
	return &i.UniqueItems
}

func (i *Items) GetEnum() *low.NodeReference[[]low.ValueReference[*yaml.Node]] {
	return &i.Enum
}

func (i *Items) GetMultipleOf() *low.NodeReference[int] {
	return &i.MultipleOf
}

// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"go.yaml.in/yaml/v4"
)

// Items is a high-level representation of a Swagger / OpenAPI 2 Items object, backed by a low level one.
// Items is a limited subset of JSON-Schema's items object. It is used by parameter definitions that are not
// located in "body"
//   - https://swagger.io/specification/v2/#itemsObject
type Items struct {
	Type             string
	Format           string
	CollectionFormat string
	Items            *Items
	Default          *yaml.Node
	Maximum          int
	ExclusiveMaximum bool
	Minimum          int
	ExclusiveMinimum bool
	MaxLength        int
	MinLength        int
	Pattern          string
	MaxItems         int
	MinItems         int
	UniqueItems      bool
	Enum             []*yaml.Node
	MultipleOf       int
	low              *low.Items
}

// NewItems creates a new high-level Items instance from a low-level one.
func NewItems(items *low.Items) *Items {
	i := new(Items)
	i.low = items
	if !items.Type.IsEmpty() {
		i.Type = items.Type.Value
	}
	if !items.Format.IsEmpty() {
		i.Format = items.Format.Value
	}
	if !items.Items.IsEmpty() {
		i.Items = NewItems(items.Items.Value)
	}
	if !items.CollectionFormat.IsEmpty() {
		i.CollectionFormat = items.CollectionFormat.Value
	}
	if !items.Default.IsEmpty() {
		i.Default = items.Default.Value
	}
	if !items.Maximum.IsEmpty() {
		i.Maximum = items.Maximum.Value
	}
	if !items.ExclusiveMaximum.IsEmpty() {
		i.ExclusiveMaximum = items.ExclusiveMaximum.Value
	}
	if !items.Minimum.IsEmpty() {
		i.Minimum = items.Minimum.Value
	}
	if !items.ExclusiveMinimum.IsEmpty() {
		i.ExclusiveMinimum = items.ExclusiveMinimum.Value
	}
	if !items.MaxLength.IsEmpty() {
		i.MaxLength = items.MaxLength.Value
	}
	if !items.MinLength.IsEmpty() {
		i.MinLength = items.MinLength.Value
	}
	if !items.Pattern.IsEmpty() {
		i.Pattern = items.Pattern.Value
	}
	if !items.MinItems.IsEmpty() {
		i.MinItems = items.MinItems.Value
	}
	if !items.MaxItems.IsEmpty() {
		i.MaxItems = items.MaxItems.Value
	}
	if !items.UniqueItems.IsEmpty() {
		i.UniqueItems = items.UniqueItems.Value
	}
	if !items.Enum.IsEmpty() {
		var enums []*yaml.Node
		for e := range items.Enum.Value {
			enums = append(enums, items.Enum.Value[e].Value)
		}
		i.Enum = enums
	}
	if !items.MultipleOf.IsEmpty() {
		i.MultipleOf = items.MultipleOf.Value
	}
	return i
}

// GoLow returns the low-level Items object that was used to create the high-level one.
func (i *Items) GoLow() *low.Items {
	return i.low
}

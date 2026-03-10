// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Header Represents a high-level Swagger / OpenAPI 2 Header object, backed by a low-level one.
// A Header is essentially identical to a Parameter, except it does not contain 'name' or 'in' properties.
//   - https://swagger.io/specification/v2/#headerObject
type Header struct {
	Type             string
	Format           string
	Description      string
	Items            *Items
	CollectionFormat string
	Default          any
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
	Enum             []any
	MultipleOf       int
	Extensions       *orderedmap.Map[string, *yaml.Node]
	low              *low.Header
}

// NewHeader will create a new high-level Swagger / OpenAPI 2 Header instance, from a low-level one.
func NewHeader(header *low.Header) *Header {
	h := new(Header)
	h.low = header
	h.Extensions = high.ExtractExtensions(header.Extensions)
	if !header.Type.IsEmpty() {
		h.Type = header.Type.Value
	}
	if !header.Format.IsEmpty() {
		h.Format = header.Type.Value
	}
	if !header.Description.IsEmpty() {
		h.Description = header.Description.Value
	}
	if !header.Items.IsEmpty() {
		h.Items = NewItems(header.Items.Value)
	}
	if !header.CollectionFormat.IsEmpty() {
		h.CollectionFormat = header.CollectionFormat.Value
	}
	if !header.Default.IsEmpty() {
		h.Default = header.Default.Value
	}
	if !header.Maximum.IsEmpty() {
		h.Maximum = header.Maximum.Value
	}
	if !header.ExclusiveMaximum.IsEmpty() {
		h.ExclusiveMaximum = header.ExclusiveMaximum.Value
	}
	if !header.Minimum.IsEmpty() {
		h.Minimum = header.Minimum.Value
	}
	if !header.ExclusiveMinimum.Value {
		h.ExclusiveMinimum = header.ExclusiveMinimum.Value
	}
	if !header.MaxLength.IsEmpty() {
		h.MaxLength = header.MaxLength.Value
	}
	if !header.MinLength.IsEmpty() {
		h.MinLength = header.MinLength.Value
	}
	if !header.Pattern.IsEmpty() {
		h.Pattern = header.Pattern.Value
	}
	if !header.MinItems.IsEmpty() {
		h.MinItems = header.MinItems.Value
	}
	if !header.MaxItems.IsEmpty() {
		h.MaxItems = header.MaxItems.Value
	}
	if !header.UniqueItems.IsEmpty() {
		h.UniqueItems = header.UniqueItems.IsEmpty()
	}
	if !header.Enum.IsEmpty() {
		var enums []any
		for e := range header.Enum.Value {
			enums = append(enums, header.Enum.Value[e].Value)
		}
		h.Enum = enums
	}
	if !header.MultipleOf.IsEmpty() {
		h.MultipleOf = header.MultipleOf.Value
	}
	return h
}

// GoLow returns the low-level header used to create the high-level one.
func (h *Header) GoLow() *low.Header {
	return h.low
}

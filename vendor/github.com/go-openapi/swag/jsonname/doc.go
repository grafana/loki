// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

// Package jsonname is a provider of json property names from go properties.
//
// The entire package has moved closer to the main consumer of this functionality: jsonpointer.
//
// Deprecated: use [github.com/go-openapi/jsonpointer/jsonname] instead.
package jsonname

import "github.com/go-openapi/jsonpointer/jsonname"

// GoNameProvider resolves json property names to go struct field names following
// the same rules as the standard library's [encoding/json] package.
//
// Deprecated: use [jsonname.GoNameProvideer] instead.
type GoNameProvider = jsonname.GoNameProvider

// NewGoNameProvider creates a new [GoNameProvider].
func NewGoNameProvider() *GoNameProvider {
	return jsonname.NewGoNameProvider()
}

// DefaultJSONNameProvider is the default cache for types.
//
// Deprecated: use [jsonname.DefaultJSONNameProvider] instead.
var DefaultJSONNameProvider = jsonname.DefaultJSONNameProvider

// NameProvider represents an object capable of translating from go property names
// to json property names.
//
// Deprecated: use [jsonname.GoNameProvideer] instead.
type NameProvider = jsonname.NameProvider

// NewNameProvider creates a new name provider
//
// Deprecated: use [jsonname.GoNameProvideer] instead.
func NewNameProvider() *NameProvider {
	return jsonname.NewNameProvider()
}

// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package cache

import (
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"
)

// SchemaCacheEntry holds a compiled schema and its intermediate representations.
// This is stored in the cache to avoid re-rendering and re-compiling schemas on each request.
type SchemaCacheEntry struct {
	Schema          *base.Schema
	RenderedInline  []byte
	ReferenceSchema string // String version of RenderedInline
	RenderedJSON    []byte
	CompiledSchema  *jsonschema.Schema
	RenderedNode    *yaml.Node
}

// SchemaCache defines the interface for schema caching implementations.
// The key is a uint64 hash of the schema (from schema.GoLow().Hash()).
type SchemaCache interface {
	Load(key uint64) (*SchemaCacheEntry, bool)
	Store(key uint64, value *SchemaCacheEntry)
	Range(f func(key uint64, value *SchemaCacheEntry) bool)
}

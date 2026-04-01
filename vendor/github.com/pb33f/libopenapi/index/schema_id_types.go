// Copyright 2022-2025 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"go.yaml.in/yaml/v4"
)

// SchemaIdEntry represents a schema registered by its JSON Schema 2020-12 $id.
// This enables $ref resolution against $id values per JSON Schema specification.
type SchemaIdEntry struct {
	Id             string     // The $id value as declared in the schema
	ResolvedUri    string     // Fully resolved absolute URI after applying base URI resolution
	SchemaNode     *yaml.Node // The YAML node containing the schema with this $id
	ParentId       string     // The $id of the parent scope (for nested schemas with $id)
	Index          *SpecIndex // Reference to the SpecIndex containing this schema
	DefinitionPath string     // JSON pointer path to this schema (e.g., #/components/schemas/Pet)
	Line           int        // Line number where $id was declared (for error reporting)
	Column         int        // Column number where $id was declared (for error reporting)
}

// GetKey returns the registry key for this entry.
// Uses ResolvedUri if available, otherwise falls back to Id.
func (e *SchemaIdEntry) GetKey() string {
	if e.ResolvedUri != "" {
		return e.ResolvedUri
	}
	return e.Id
}

// SchemaIdScope tracks the resolution context during tree walking.
// Used to maintain the base URI hierarchy when extracting $id values.
type SchemaIdScope struct {
	BaseUri string   // Current base URI for relative $id and $ref resolution
	Chain   []string // Stack of $id URIs from root to current location
}

// NewSchemaIdScope initializes scope tracking for base URI resolution during schema tree traversal.
func NewSchemaIdScope(baseUri string) *SchemaIdScope {
	return &SchemaIdScope{
		BaseUri: baseUri,
		Chain:   make([]string, 0),
	}
}

// PushId updates the base URI context when entering a schema with $id.
// The new $id becomes the base for resolving relative references in child schemas.
func (s *SchemaIdScope) PushId(id string) {
	s.Chain = append(s.Chain, id)
	s.BaseUri = id
}

// PopId restores the previous base URI when exiting a schema scope.
func (s *SchemaIdScope) PopId() {
	if len(s.Chain) > 0 {
		s.Chain = s.Chain[:len(s.Chain)-1]
		if len(s.Chain) > 0 {
			s.BaseUri = s.Chain[len(s.Chain)-1]
		}
	}
}

// Copy creates an independent scope for exploring alternative branches without
// affecting the parent scope's state (used in anyOf/oneOf/allOf traversal).
func (s *SchemaIdScope) Copy() *SchemaIdScope {
	chainCopy := make([]string, len(s.Chain))
	copy(chainCopy, s.Chain)
	return &SchemaIdScope{
		BaseUri: s.BaseUri,
		Chain:   chainCopy,
	}
}

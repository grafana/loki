// Copyright 2022-2025 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
)

// ResolvingIdsKey is the context key for tracking $id values currently being resolved.
const ResolvingIdsKey ContextKey = "resolvingIds"

// SchemaIdScopeKey is the context key for tracking the current $id scope during extraction.
const SchemaIdScopeKey ContextKey = "schemaIdScope"

// GetSchemaIdScope returns the current $id scope from the context.
func GetSchemaIdScope(ctx context.Context) *SchemaIdScope {
	if v := ctx.Value(SchemaIdScopeKey); v != nil {
		return v.(*SchemaIdScope)
	}
	return nil
}

// WithSchemaIdScope returns a new context with the given $id scope.
func WithSchemaIdScope(ctx context.Context, scope *SchemaIdScope) context.Context {
	return context.WithValue(ctx, SchemaIdScopeKey, scope)
}

// GetResolvingIds returns the set of $id values currently being resolved in the call chain.
func GetResolvingIds(ctx context.Context) map[string]bool {
	if v := ctx.Value(ResolvingIdsKey); v != nil {
		return v.(map[string]bool)
	}
	return nil
}

// AddResolvingId adds a $id to the resolving set in the context.
// Returns a new context with the updated set (copy-on-write for thread safety).
func AddResolvingId(ctx context.Context, id string) context.Context {
	existing := GetResolvingIds(ctx)
	newSet := make(map[string]bool, len(existing)+1)
	for k, v := range existing {
		newSet[k] = v
	}
	newSet[id] = true
	return context.WithValue(ctx, ResolvingIdsKey, newSet)
}

// IsIdBeingResolved checks if a $id is currently being resolved in the call chain.
// Used to detect and prevent circular $id resolution.
func IsIdBeingResolved(ctx context.Context, id string) bool {
	ids := GetResolvingIds(ctx)
	if ids == nil {
		return false
	}
	return ids[id]
}

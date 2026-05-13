// Copyright 2022-2025 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"fmt"
	"log/slog"
)

// schemaIdRegistrationResult holds the result of a schema ID registration attempt.
type schemaIdRegistrationResult struct {
	registered bool   // true if successfully registered
	duplicate  bool   // true if a duplicate was found (first-wins policy applied)
	key        string // the key used for registration
}

// registerSchemaIdToRegistry is the common registration logic for both SpecIndex and Rolodex.
// Returns the registration result. Duplicates are logged but not treated as errors.
func registerSchemaIdToRegistry(
	registry map[string]*SchemaIdEntry,
	entry *SchemaIdEntry,
	logger *slog.Logger,
	registryName string,
) (*schemaIdRegistrationResult, error) {
	if entry == nil {
		return nil, fmt.Errorf("cannot register nil SchemaIdEntry")
	}

	if err := ValidateSchemaId(entry.Id); err != nil {
		if logger != nil {
			logger.Warn("invalid $id value, skipping registration",
				"registry", registryName,
				"id", entry.Id,
				"error", err.Error(),
				"line", entry.Line,
				"column", entry.Column)
		}
		return nil, err
	}

	key := entry.GetKey()

	if existing, ok := registry[key]; ok {
		if logger != nil {
			existingPath := ""
			newPath := ""
			if existing.Index != nil {
				existingPath = existing.Index.GetSpecAbsolutePath()
			}
			if entry.Index != nil {
				newPath = entry.Index.GetSpecAbsolutePath()
			}
			logger.Warn("duplicate $id detected, keeping first registration",
				"registry", registryName,
				"id", key,
				"first_location", fmt.Sprintf("%s:%d:%d", existingPath, existing.Line, existing.Column),
				"duplicate_location", fmt.Sprintf("%s:%d:%d", newPath, entry.Line, entry.Column))
		}
		return &schemaIdRegistrationResult{registered: false, duplicate: true, key: key}, nil
	}

	registry[key] = entry
	return &schemaIdRegistrationResult{registered: true, duplicate: false, key: key}, nil
}

// copySchemaIdRegistry creates a defensive copy of a schema ID registry.
func copySchemaIdRegistry(registry map[string]*SchemaIdEntry) map[string]*SchemaIdEntry {
	if registry == nil {
		return make(map[string]*SchemaIdEntry)
	}
	result := make(map[string]*SchemaIdEntry, len(registry))
	for k, v := range registry {
		result[k] = v
	}
	return result
}

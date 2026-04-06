// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package libopenapi

import (
	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowbase "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/utils"
)

// ClearAllCaches resets every global in-process cache in libopenapi.
// Call this between document lifecycles in long-running processes
// (servers, CLI tools that process many specs) to release memory that
// would otherwise accumulate and never be garbage-collected.
func ClearAllCaches() {
	low.ClearHashCache()              // hashCache + indexCollectionCache
	lowbase.ClearSchemaQuickHashMap() // SchemaQuickHashMap
	index.ClearHashCache()            // nodeHashCache
	index.ClearContentDetectionCache()
	highbase.ClearInlineRenderingTracker()
	utils.ClearJSONPathCache()

	// Drain sync.Pool instances that hold *yaml.Node pointers.
	// Pooled slices/maps keep the entire YAML parse tree alive.
	index.ClearNodePools()
	low.ClearNodePools()
}

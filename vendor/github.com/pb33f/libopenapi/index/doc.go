// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

/*
Package index builds and queries reference indexes for OpenAPI and JSON Schema documents.

# Internal layout

The package is organized around a few cooperating subsystems:

  - SpecIndex owns a single parsed document, its discovered references, derived component maps,
    counters, and runtime caches.
  - reference extraction walks YAML nodes and records references, inline definitions, and
    component metadata used later by lookup and resolution.
  - lookup resolves local, external, and schema-id based references. Exact component references
    use direct map lookups first, with JSONPath-based traversal retained as a fallback.
  - Rolodex coordinates local and remote file systems, external document indexing, and shared
    reference lookup across multiple indexed documents.
  - Resolver performs circular reference analysis and, when requested, destructive in-place
    resolution of relative references.

Key invariants

  - Public behavior is preserved through staged internal refactors. Helper extraction and
    subsystem boundaries should not change external APIs.
  - SpecIndex.Release and Rolodex.Release are responsible for clearing owned runtime state so
    long-lived processes do not retain unnecessary memory.
  - Common exact component references should avoid JSONPath parsing on the hot path.
  - External lookup must remain safe under concurrent indexing and must not leak locks, response
    bodies, or in-flight wait state.

When editing this package, prefer extending the existing subsystem seams instead of adding more
responsibility to the top-level orchestration files for indexing, rolodex loading, or resolver
entry points.
*/
package index

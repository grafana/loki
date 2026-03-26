// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

// Package strict provides strict validation that detects undeclared
// properties in requests and responses, even when additionalProperties
// would normally allow them.
//
// Strict mode is designed for API governance scenarios where you want to
// ensure that clients only send properties that are explicitly documented
// in the OpenAPI specification, regardless of whether additionalProperties
// is set to true.
//
// # Key Features
//
//   - Detects undeclared properties in request/response bodies (JSON only)
//   - Detects undeclared query parameters, headers, and cookies
//   - Supports ignore paths with glob patterns (e.g., "$.body.metadata.*")
//   - Handles polymorphic schemas (oneOf/anyOf) via per-branch validation
//   - Respects readOnly/writeOnly based on request vs response direction
//   - Configurable header ignore list with sensible defaults
//
// # Known Limitations
//
// Property names containing single quotes (e.g., {"it's": "value"}) cannot be
// represented in bracket notation and cannot be matched by ignore patterns.
// Such properties will always be reported as undeclared if not in schema.
// This is acceptable because property names with quotes are extremely rare.
package strict

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/pb33f/libopenapi-validator/config"
)

// Direction indicates whether validation is for a request or response.
// This affects readOnly/writeOnly handling and Set-Cookie behavior.
type Direction int

const (
	// DirectionRequest indicates validation of an HTTP request.
	// readOnly properties are not expected in request bodies.
	DirectionRequest Direction = iota

	// DirectionResponse indicates validation of an HTTP response.
	// writeOnly properties are not expected in response bodies.
	// Set-Cookie headers are ignored (expected in responses).
	DirectionResponse
)

// String returns a human-readable direction name.
func (d Direction) String() string {
	if d == DirectionResponse {
		return "response"
	}
	return "request"
}

// UndeclaredValue represents a value found in data that is not declared
// in the schema. This is the core output of strict validation.
type UndeclaredValue struct {
	// Path is the instance JSONPath where the undeclared value was found.
	// uses bracket notation for property names with special characters.
	// examples: "$.body.user.extra", "$.body['a.b'].value", "$.query.debug"
	Path string

	// Name is the property, parameter, header, or cookie name.
	Name string

	// Value is the actual value found (it may be truncated for display).
	Value any

	// Type indicates what kind of value this is.
	// one of: "property", "header", "query", "cookie", "item"
	Type string

	// DeclaredProperties lists property names that ARE declared at this
	// location in the schema. Helps users understand what's expected.
	// for headers/query/cookies, this lists declared parameter names.
	DeclaredProperties []string

	// Direction indicates whether this was in a request or response.
	// used for error message disambiguation when Path is "$.body".
	Direction Direction

	// SpecLine is the line number in the OpenAPI spec where the parent
	// schema is defined. Zero if unavailable.
	SpecLine int

	// SpecCol is the column number in the OpenAPI spec where the parent
	// schema is defined. Zero if unavailable.
	SpecCol int
}

// extractSchemaLocation extracts the line and column from a schema's low-level
// representation. returns (0, 0) if the schema is nil or has no low-level info.
func extractSchemaLocation(schema *base.Schema) (line, col int) {
	if schema == nil {
		return 0, 0
	}
	low := schema.GoLow()
	if low == nil || low.RootNode == nil {
		return 0, 0
	}
	return low.RootNode.Line, low.RootNode.Column
}

// newUndeclaredProperty creates an UndeclaredValue for an undeclared object property.
// the schema parameter is the parent schema where the property would need to be declared.
func newUndeclaredProperty(path, name string, value any, declaredNames []string, direction Direction, schema *base.Schema) UndeclaredValue {
	line, col := extractSchemaLocation(schema)
	return UndeclaredValue{
		Path:               path,
		Name:               name,
		Value:              TruncateValue(value),
		Type:               "property",
		DeclaredProperties: declaredNames,
		Direction:          direction,
		SpecLine:           line,
		SpecCol:            col,
	}
}

// newUndeclaredParam creates an UndeclaredValue for an undeclared parameter (query/header/cookie).
// note: parameters don't have SpecLine/SpecCol because they're defined in OpenAPI parameter objects,
// not schema objects. the parameter itself is the issue, not a schema definition.
func newUndeclaredParam(path, name string, value any, paramType string, declaredNames []string, direction Direction) UndeclaredValue {
	return UndeclaredValue{
		Path:               path,
		Name:               name,
		Value:              value,
		Type:               paramType,
		DeclaredProperties: declaredNames,
		Direction:          direction,
	}
}

// newUndeclaredItem creates an UndeclaredValue for an undeclared array item.
func newUndeclaredItem(path, name string, value any, direction Direction) UndeclaredValue {
	return UndeclaredValue{
		Path:      path,
		Name:      name,
		Value:     TruncateValue(value),
		Type:      "item",
		Direction: direction,
	}
}

// Input contains the parameters for strict validation.
type Input struct {
	// Schema is the OpenAPI schema to validate against.
	Schema *base.Schema

	// Data is the unmarshalled data to validate (from request/response body).
	// Should be the result of json.Unmarshal.
	Data any

	// Direction indicates request vs response validation.
	// affects readOnly/writeOnly and Set-Cookie handling.
	Direction Direction

	// Options contains validation configuration including ignore paths.
	Options *config.ValidationOptions

	// BasePath is the prefix for generated instance paths.
	// typically "$.body" for bodies, "$.query" for query params, etc.
	BasePath string

	// Version is the OpenAPI version (3.0 or 3.1).
	// affects nullable handling in schema matching.
	Version float32
}

// Result contains the output of strict validation.
type Result struct {
	Valid bool

	// UndeclaredValues lists all undeclared properties, parameters,
	// headers, or cookies found during validation.
	UndeclaredValues []UndeclaredValue
}

// cycleKey uniquely identifies a schema at a specific validation path.
// Using a struct key avoids string allocation in the hot path.
type cycleKey struct {
	path      string
	schemaKey string
}

// traversalContext tracks state during schema traversal to detect cycles
// and limit recursion depth.
type traversalContext struct {
	// visited tracks schemas already being validated at specific paths.
	// key combines instance path + schema key to allow same schema at different paths.
	visited map[cycleKey]bool

	// depth tracks current recursion depth for safety limits.
	depth int

	// maxDepth is the maximum allowed recursion depth (default: 100).
	maxDepth int

	// direction indicates request vs response for readOnly/writeOnly.
	direction Direction

	// ignorePaths are compiled regex patterns for paths to skip.
	ignorePaths []*regexp.Regexp

	// path is the current instance path being validated.
	path string
}

// newTraversalContext creates a new context for schema traversal.
func newTraversalContext(direction Direction, ignorePaths []*regexp.Regexp, basePath string) *traversalContext {
	return &traversalContext{
		visited:     make(map[cycleKey]bool),
		depth:       0,
		maxDepth:    100,
		direction:   direction,
		ignorePaths: ignorePaths,
		path:        basePath,
	}
}

// withPath returns a new context with an updated path.
func (c *traversalContext) withPath(path string) *traversalContext {
	return &traversalContext{
		visited:     c.visited,
		depth:       c.depth + 1,
		maxDepth:    c.maxDepth,
		direction:   c.direction,
		ignorePaths: c.ignorePaths,
		path:        path,
	}
}

// shouldIgnore checks if the current path matches any ignore pattern.
func (c *traversalContext) shouldIgnore() bool {
	for _, pattern := range c.ignorePaths {
		if pattern.MatchString(c.path) {
			return true
		}
	}
	return false
}

// exceedsDepth checks if we've exceeded the maximum recursion depth.
func (c *traversalContext) exceedsDepth() bool {
	return c.depth > c.maxDepth
}

// checkAndMarkVisited checks if a schema has been visited at the current path.
// Returns true if this is a cycle (already visited), false otherwise.
// If not a cycle, marks the schema as visited.
func (c *traversalContext) checkAndMarkVisited(schemaKey string) bool {
	key := cycleKey{path: c.path, schemaKey: schemaKey}
	if c.visited[key] {
		return true // Cycle detected
	}
	c.visited[key] = true
	return false
}

// Validator performs strict property validation against OpenAPI schemas.
// It detects any properties present in data that are not explicitly
// declared in the schema, regardless of additionalProperties settings.
//
// A new Validator should be created for each validation call to ensure
// isolation of internal caches and render contexts.
//
// # Cycle Detection
//
// The Validator uses two distinct cycle detection mechanisms:
//
//  1. traversalContext.visited: Tracks visited (path, schemaKey) combinations
//     during the main validation traversal. This prevents infinite recursion
//     when the same schema is encountered at the same instance path. The key
//     uses a struct for zero-allocation lookups in the hot path.
//
//  2. renderCtx (InlineRenderContext): libopenapi's built-in cycle detection
//     for schema rendering. This is used when compiling schemas for oneOf/anyOf
//     variant matching. It operates at the schema reference level rather than
//     instance path level.
//
// These mechanisms serve complementary purposes: visited tracks data traversal
// while renderCtx tracks schema resolution during compilation.
type Validator struct {
	options *config.ValidationOptions
	logger  *slog.Logger

	// localCache stores compiled schemas for reuse within this validation.
	// ley is schema hash (as string for map compatibility), value is compiled jsonschema.
	localCache map[string]*jsonschema.Schema

	// patternCache stores compiled regex patterns for patternProperties.
	// key is the pattern string, value is the compiled regex.
	patternCache map[string]*regexp.Regexp

	// renderCtx is used for safe schema rendering with cycle detection.
	// see Validator doc comment for how this relates to traversalContext.visited.
	renderCtx *base.InlineRenderContext

	// version is the OpenAPI version (3.0 or 3.1).
	version float32

	// compiledIgnorePaths are the pre-compiled regex patterns.
	compiledIgnorePaths []*regexp.Regexp
}

// NewValidator creates a fresh validator for a single validation call.
// The validator should not be reused across concurrent requests.
// Uses the logger from options if available, otherwise logging is silent.
func NewValidator(options *config.ValidationOptions, version float32) *Validator {
	var logger *slog.Logger
	if options != nil && options.Logger != nil {
		logger = options.Logger
	} else {
		// create a no-op logger that discards all output
		logger = slog.New(discardHandler{})
	}

	v := &Validator{
		options:      options,
		logger:       logger,
		localCache:   make(map[string]*jsonschema.Schema),
		patternCache: make(map[string]*regexp.Regexp),
		renderCtx:    base.NewInlineRenderContext(),
		version:      version,
	}

	if options != nil {
		v.compiledIgnorePaths = compileIgnorePaths(options.StrictIgnorePaths)
	}

	return v
}

// discardHandler is a slog.Handler that discards all log records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

// matchesIgnorePath checks if a path matches any pre-compiled ignore pattern.
func (v *Validator) matchesIgnorePath(path string) bool {
	for _, pattern := range v.compiledIgnorePaths {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

// getCompiledPattern returns a cached compiled regex for a pattern string.
// If the pattern is not in the cache, it compiles and caches it.
// Returns nil if the pattern is invalid.
func (v *Validator) getCompiledPattern(pattern string) *regexp.Regexp {
	if cached, ok := v.patternCache[pattern]; ok {
		return cached
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	v.patternCache[pattern] = compiled
	return compiled
}

// getSchemaKey returns a unique key for a schema used in cycle detection.
// Uses the schema's low-level hash if available, otherwise the pointer address.
func (v *Validator) getSchemaKey(schema *base.Schema) string {
	if schema == nil {
		return ""
	}
	if low := schema.GoLow(); low != nil {
		hash := low.Hash()
		return fmt.Sprintf("%x", hash) // uint64 hash as hex string
	}
	// fallback to pointer address for inline schemas without low-level info
	return fmt.Sprintf("%p", schema)
}

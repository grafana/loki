package config

import (
	"log/slog"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/pb33f/libopenapi-validator/cache"
)

// RegexCache can be set to enable compiled regex caching.
// It can be just a sync.Map, or a custom implementation with possible cleanup.
//
// Be aware that the cache should be thread safe
type RegexCache interface {
	Load(key any) (value any, ok bool) // Get a compiled regex from the cache
	Store(key, value any)              // Set a compiled regex to the cache
}

// ValidationOptions A container for validation configuration.
//
// Generally fluent With... style functions are used to establish the desired behavior.
type ValidationOptions struct {
	RegexEngine         jsonschema.RegexpEngine
	RegexCache          RegexCache // Enable compiled regex caching
	FormatAssertions    bool
	ContentAssertions   bool
	SecurityValidation  bool
	OpenAPIMode         bool // Enable OpenAPI-specific vocabulary validation
	AllowScalarCoercion bool // Enable string->boolean/number coercion
	Formats             map[string]func(v any) error
	SchemaCache         cache.SchemaCache // Optional cache for compiled schemas
	Logger              *slog.Logger      // Logger for debug/error output (nil = silent)

	// strict mode options - detect undeclared properties even when additionalProperties: true
	StrictMode                bool     // Enable strict property validation
	StrictIgnorePaths         []string // Instance JSONPath patterns to exclude from strict checks
	StrictIgnoredHeaders      []string // Headers to always ignore in strict mode (nil = use defaults)
	strictIgnoredHeadersMerge bool     // Internal: true if merging with defaults
}

// Option Enables an 'Options pattern' approach
type Option func(*ValidationOptions)

// NewValidationOptions creates a new ValidationOptions instance with default values.
func NewValidationOptions(opts ...Option) *ValidationOptions {
	// create the set of default values
	o := &ValidationOptions{
		FormatAssertions:   false,
		ContentAssertions:  false,
		SecurityValidation: true,
		OpenAPIMode:        true,                    // Enable OpenAPI vocabulary by default
		SchemaCache:        cache.NewDefaultCache(), // Enable caching by default
	}

	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// WithExistingOpts returns an Option that will copy the values from the supplied ValidationOptions instance
func WithExistingOpts(options *ValidationOptions) Option {
	return func(o *ValidationOptions) {
		if options != nil {
			o.RegexEngine = options.RegexEngine
			o.RegexCache = options.RegexCache
			o.FormatAssertions = options.FormatAssertions
			o.ContentAssertions = options.ContentAssertions
			o.SecurityValidation = options.SecurityValidation
			o.OpenAPIMode = options.OpenAPIMode
			o.AllowScalarCoercion = options.AllowScalarCoercion
			o.Formats = options.Formats
			o.SchemaCache = options.SchemaCache
			o.Logger = options.Logger
			o.StrictMode = options.StrictMode
			o.StrictIgnorePaths = options.StrictIgnorePaths
			o.StrictIgnoredHeaders = options.StrictIgnoredHeaders
			o.strictIgnoredHeadersMerge = options.strictIgnoredHeadersMerge
		}
	}
}

// WithLogger sets the logger for validation debug/error output.
// If not set, logging is silent (nil logger is handled gracefully).
func WithLogger(logger *slog.Logger) Option {
	return func(o *ValidationOptions) {
		o.Logger = logger
	}
}

// WithRegexEngine Assigns a custom regular-expression engine to be used during validation.
func WithRegexEngine(engine jsonschema.RegexpEngine) Option {
	return func(o *ValidationOptions) {
		o.RegexEngine = engine
	}
}

// WithRegexCache assigns a cache for compiled regular expressions.
// A sync.Map should be sufficient for most use cases. It does not implement any cleanup
func WithRegexCache(regexCache RegexCache) Option {
	return func(o *ValidationOptions) {
		o.RegexCache = regexCache
	}
}

// WithFormatAssertions enables checks for 'format' assertions (such as date, date-time, uuid, etc)
func WithFormatAssertions() Option {
	return func(o *ValidationOptions) {
		o.FormatAssertions = true
	}
}

// WithContentAssertions enables checks for contentType, contentEncoding, etc
func WithContentAssertions() Option {
	return func(o *ValidationOptions) {
		o.ContentAssertions = true
	}
}

// WithoutSecurityValidation disables security validation for request validation
func WithoutSecurityValidation() Option {
	return func(o *ValidationOptions) {
		o.SecurityValidation = false
	}
}

// WithCustomFormat adds custom formats and their validators that checks for custom 'format' assertions
// When you add different validators with the same name, they will be overridden,
// and only the last registration will take effect.
func WithCustomFormat(name string, validator func(v any) error) Option {
	return func(o *ValidationOptions) {
		if o.Formats == nil {
			o.Formats = make(map[string]func(v any) error)
		}

		o.Formats[name] = validator
	}
}

// WithOpenAPIMode enables OpenAPI-specific keyword validation (default: true)
func WithOpenAPIMode() Option {
	return func(o *ValidationOptions) {
		o.OpenAPIMode = true
	}
}

// WithoutOpenAPIMode disables OpenAPI-specific keyword validation
func WithoutOpenAPIMode() Option {
	return func(o *ValidationOptions) {
		o.OpenAPIMode = false
	}
}

// WithScalarCoercion enables string to boolean/number coercion (Jackson-style)
func WithScalarCoercion() Option {
	return func(o *ValidationOptions) {
		o.AllowScalarCoercion = true
	}
}

// WithSchemaCache sets a custom cache implementation or disables caching if nil.
// Pass nil to disable schema caching and skip cache warming during validator initialization.
// The default cache is a thread-safe sync.Map wrapper.
func WithSchemaCache(cache cache.SchemaCache) Option {
	return func(o *ValidationOptions) {
		o.SchemaCache = cache
	}
}

// WithStrictMode enables strict property validation.
// In strict mode, undeclared properties are reported as errors even when
// additionalProperties: true would normally allow them.
//
// This is useful for API governance scenarios where you want to ensure
// clients only send properties that are explicitly documented in the
// OpenAPI specification.
func WithStrictMode() Option {
	return func(o *ValidationOptions) {
		o.StrictMode = true
	}
}

// WithStrictIgnorePaths sets JSONPath patterns for paths to exclude from strict validation.
// Patterns use glob syntax:
//   - * matches a single path segment
//   - ** matches any depth (zero or more segments)
//   - [*] matches any array index
//   - \* escapes a literal asterisk
//
// Examples:
//   - "$.body.metadata.*" - any property under metadata
//   - "$.body.**.x-*" - any x-* property at any depth
//   - "$.headers.X-*" - any header starting with X-
func WithStrictIgnorePaths(paths ...string) Option {
	return func(o *ValidationOptions) {
		o.StrictIgnorePaths = paths
	}
}

// WithStrictIgnoredHeaders replaces the default ignored headers list entirely.
// Use this to fully control which headers are ignored in strict mode.
// For the default list, see the strict package's DefaultIgnoredHeaders.
func WithStrictIgnoredHeaders(headers ...string) Option {
	return func(o *ValidationOptions) {
		o.StrictIgnoredHeaders = headers
		o.strictIgnoredHeadersMerge = false
	}
}

// WithStrictIgnoredHeadersExtra adds headers to the default ignored list.
// Unlike WithStrictIgnoredHeaders, this merges with the defaults rather
// than replacing them.
func WithStrictIgnoredHeadersExtra(headers ...string) Option {
	return func(o *ValidationOptions) {
		o.StrictIgnoredHeaders = headers
		o.strictIgnoredHeadersMerge = true
	}
}

// defaultIgnoredHeaders contains standard HTTP headers ignored by default.
// This is the fallback list used when no custom headers are configured.
var defaultIgnoredHeaders = []string{
	"content-type", "content-length", "accept", "authorization",
	"user-agent", "host", "connection", "accept-encoding",
	"accept-language", "cache-control", "pragma", "origin",
	"referer", "cookie", "date", "etag", "expires",
	"if-match", "if-none-match", "if-modified-since",
	"last-modified", "transfer-encoding", "vary", "x-forwarded-for",
	"x-forwarded-proto", "x-real-ip", "x-request-id",
	"request-start-time", // Added by some API clients for timing
}

// GetEffectiveStrictIgnoredHeaders returns the list of headers to ignore
// based on configuration. Returns defaults if not configured, merged list
// if extra headers were added, or replaced list if headers were fully replaced.
func (o *ValidationOptions) GetEffectiveStrictIgnoredHeaders() []string {
	if o.StrictIgnoredHeaders == nil {
		return defaultIgnoredHeaders
	}
	if o.strictIgnoredHeadersMerge {
		return append(defaultIgnoredHeaders, o.StrictIgnoredHeaders...)
	}
	return o.StrictIgnoredHeaders
}

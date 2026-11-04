package config

import "errors"

// Option configures JSONPath compilation and evaluation.
type Option func(*config)

// WithPropertyNameExtension enables the use of the "~" character to access a property key.
// It is not enabled by default as this is outside of RFC 9535, but is important for several use-cases
func WithPropertyNameExtension() Option {
	return func(cfg *config) {
		cfg.propertyNameExtension = true
	}
}

// WithLazyContextTracking enables on-demand tracking for JSONPath Plus context variables.
// When enabled, tracking is only turned on if the query uses @property, @path, @parentProperty, or @index.
// Defaults to false to preserve historical eager tracking behavior.
func WithLazyContextTracking() Option {
	return func(cfg *config) {
		cfg.lazyContextTracking = true
	}
}

// WithStrictRFC9535 disables JSONPath Plus extensions and enforces strict RFC 9535 compliance.
// By default, JSONPath Plus extensions are enabled as they are a true superset of RFC 9535.
// Use this option if you need to ensure pure RFC 9535 compliance.
func WithStrictRFC9535() Option {
	return func(cfg *config) {
		cfg.strictRFC9535 = true
	}
}

// WithSpectralCompatibility enables the safe Spectral expression dialect.
//
// Spectral compatibility includes JSONPath Plus context variables, parent
// selection and the property-name extension. It cannot be combined with
// WithStrictRFC9535; callers should validate Config before use. NewPath does
// this automatically.
func WithSpectralCompatibility() Option {
	return func(cfg *config) {
		cfg.spectralCompatibility = true
		cfg.propertyNameExtension = true
	}
}

// Config exposes the resolved JSONPath dialect and context-tracking settings.
type Config interface {
	PropertyNameEnabled() bool
	JSONPathPlusEnabled() bool
	LazyContextTrackingEnabled() bool
}

type config struct {
	propertyNameExtension bool
	strictRFC9535         bool
	lazyContextTracking   bool
	spectralCompatibility bool
}

func (c *config) PropertyNameEnabled() bool {
	return c.propertyNameExtension
}

// JSONPathPlusEnabled returns true if JSONPath Plus extensions are enabled.
// JSONPath Plus is ON by default (true superset, no conflicts with RFC 9535).
// Returns false only if WithStrictRFC9535() was explicitly called.
func (c *config) JSONPathPlusEnabled() bool {
	return !c.strictRFC9535
}

// LazyContextTrackingEnabled returns true if on-demand tracking is enabled.
// Defaults to false for backward compatibility.
func (c *config) LazyContextTrackingEnabled() bool {
	return c.lazyContextTracking
}

// SpectralCompatibilityEnabled reports whether cfg enables the safe Spectral dialect.
// It is a function rather than a Config method so adding the dialect does not
// break external implementations of the existing Config interface.
func SpectralCompatibilityEnabled(cfg Config) bool {
	resolved, ok := cfg.(*config)
	return ok && resolved.spectralCompatibility
}

// Validate rejects incompatible dialect options without making option order significant.
func Validate(cfg Config) error {
	resolved, ok := cfg.(*config)
	if ok && resolved.strictRFC9535 && resolved.spectralCompatibility {
		return errors.New("config.WithStrictRFC9535 and config.WithSpectralCompatibility cannot be combined")
	}
	return nil
}

// New resolves options into an immutable-by-interface configuration view.
func New(opts ...Option) Config {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

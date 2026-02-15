package otelzap

import (
	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap/zapcore"
)

// Option applies a configuration to the given config.
type Option func(l *Logger)

// WithLoggerProvider returns an [Option] that configures [log.LoggerProvider]
// used by a [Core] to create its [log.Logger].
//
// By default if this Option is not provided, the Handler will use the global
// LoggerProvider.
func WithLoggerProvider(provider log.LoggerProvider) Option {
	return func(l *Logger) {
		l.provider = provider
	}
}

// WithVersion returns an [Option] that configures the version of the
// [log.Logger] used by a [Core]. The version should be the version of the
// package that is being logged.
func WithVersion(version string) Option {
	return func(l *Logger) {
		l.version = version
	}
}

// WithSchemaURL returns an [Option] that configures the semantic convention
// schema URL of the [log.Logger] used by a [Core]. The schemaURL should be
// the schema URL for the semantic conventions used in log records.
func WithSchemaURL(schemaURL string) Option {
	return func(l *Logger) {
		l.schemaURL = schemaURL
	}
}

// WithMinLevel sets the minimal zap logging level on which the log message
// is recorded on the span.
//
// The default is >= zap.WarnLevel.
func WithMinLevel(lvl zapcore.Level) Option {
	return func(l *Logger) {
		l.minLevel = lvl
	}
}

// WithErrorStatusLevel sets the minimal zap logging level on which
// the span status is set to codes.Error.
//
// The default is >= zap.ErrorLevel.
func WithErrorStatusLevel(lvl zapcore.Level) Option {
	return func(l *Logger) {
		l.errorStatusLevel = lvl
	}
}

// WithCaller configures the logger to annotate each event with the filename,
// line number, and function name of the caller.
//
// It is enabled by default.

func WithCaller(on bool) Option {
	return func(l *Logger) {
		l.caller = on
	}
}

// WithCallerDepth allows you to you to adjust the depth of the caller by setting a number greater than 0. It can
// be useful if you're wrapping this library with your own helper functions.
func WithCallerDepth(depth int) Option {
	return func(l *Logger) {
		l.callerDepth = depth
	}
}

// WithStackTrace configures the logger to capture logs with a stack trace.
func WithStackTrace(on bool) Option {
	return func(l *Logger) {
		l.stackTrace = on
	}
}

// WithExtraFields configures the logger to add the given extra fields to structured log messages
// and the span
func WithExtraFields(fields ...zapcore.Field) Option {
	return func(l *Logger) {
		l.extraFields = append(l.extraFields, fields...)
	}
}

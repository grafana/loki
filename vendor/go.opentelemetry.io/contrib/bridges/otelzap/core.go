// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package otelzap provides a bridge between the [go.uber.org/zap] and
// [OpenTelemetry].
//
// # Record Conversion
//
// The [zapcore.Entry] and [zapcore.Field] are converted to OpenTelemetry [log.Record] in the following
// way:
//
//   - Time is set as the Timestamp.
//   - Message is set as the Body using a [log.StringValue].
//   - Level is transformed and set as the Severity. The SeverityText is also
//     set.
//   - Fields are transformed and set as the Attributes.
//   - Field value of type [context.Context] is used as context when emitting log records.
//   - For named loggers, LoggerName is used to access [log.Logger] from [log.LoggerProvider]
//
// The Level is transformed to the OpenTelemetry Severity types in the following way.
//
//   - [zapcore.DebugLevel] is transformed to [log.SeverityDebug]
//   - [zapcore.InfoLevel] is transformed to [log.SeverityInfo]
//   - [zapcore.WarnLevel] is transformed to [log.SeverityWarn]
//   - [zapcore.ErrorLevel] is transformed to [log.SeverityError]
//   - [zapcore.DPanicLevel] is transformed to [log.SeverityFatal1]
//   - [zapcore.PanicLevel] is transformed to [log.SeverityFatal2]
//   - [zapcore.FatalLevel] is transformed to [log.SeverityFatal3]
//
// Fields are transformed based on their type into log attributes, or
// into a string value encoded using [fmt.Sprintf] if there is no matching type.
//
// [OpenTelemetry]: https://opentelemetry.io/docs/concepts/signals/logs/
package otelzap // import "go.opentelemetry.io/contrib/bridges/otelzap"

import (
	"context"
	"slices"

	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

type config struct {
	provider   log.LoggerProvider
	version    string
	schemaURL  string
	attributes []attribute.KeyValue
}

func newConfig(options []Option) config {
	var c config
	for _, opt := range options {
		c = opt.apply(c)
	}

	if c.provider == nil {
		c.provider = global.GetLoggerProvider()
	}

	return c
}

// Option configures a [Core].
type Option interface {
	apply(config) config
}

type optFunc func(config) config

func (f optFunc) apply(c config) config { return f(c) }

// WithVersion returns an [Option] that configures the version of the
// [log.Logger] used by a [Core]. The version should be the version of the
// package that is being logged.
func WithVersion(version string) Option {
	return optFunc(func(c config) config {
		c.version = version
		return c
	})
}

// WithSchemaURL returns an [Option] that configures the semantic convention
// schema URL of the [log.Logger] used by a [Core]. The schemaURL should be
// the schema URL for the semantic conventions used in log records.
func WithSchemaURL(schemaURL string) Option {
	return optFunc(func(c config) config {
		c.schemaURL = schemaURL
		return c
	})
}

// WithAttributes returns an [Option] that configures the instrumentation scope
// attributes of the [log.Logger] used by a [Core].
func WithAttributes(attributes ...attribute.KeyValue) Option {
	return optFunc(func(c config) config {
		c.attributes = attributes
		return c
	})
}

// WithLoggerProvider returns an [Option] that configures [log.LoggerProvider]
// used by a [Core] to create its [log.Logger].
//
// By default if this Option is not provided, the Handler will use the global
// LoggerProvider.
func WithLoggerProvider(provider log.LoggerProvider) Option {
	return optFunc(func(c config) config {
		c.provider = provider
		return c
	})
}

// Core is a [zapcore.Core] that sends logging records to OpenTelemetry.
type Core struct {
	provider log.LoggerProvider
	logger   log.Logger
	opts     []log.LoggerOption
	attr     []log.KeyValue
	ctx      context.Context
}

// Compile-time check *Core implements zapcore.Core.
var _ zapcore.Core = (*Core)(nil)

// NewCore creates a new [zapcore.Core] that can be used with [go.uber.org/zap.New].
// The name should be the package import path that is being logged.
// The name is ignored for named loggers created using [go.uber.org/zap.Logger.Named].
func NewCore(name string, opts ...Option) *Core {
	cfg := newConfig(opts)

	var loggerOpts []log.LoggerOption
	if cfg.version != "" {
		loggerOpts = append(loggerOpts, log.WithInstrumentationVersion(cfg.version))
	}
	if cfg.schemaURL != "" {
		loggerOpts = append(loggerOpts, log.WithSchemaURL(cfg.schemaURL))
	}
	if cfg.attributes != nil {
		loggerOpts = append(loggerOpts, log.WithInstrumentationAttributes(cfg.attributes...))
	}

	logger := cfg.provider.Logger(name, loggerOpts...)

	return &Core{
		provider: cfg.provider,
		logger:   logger,
		opts:     loggerOpts,
		ctx:      context.Background(),
	}
}

// Enabled decides whether a given logging level is enabled when logging a message.
func (o *Core) Enabled(level zapcore.Level) bool {
	param := log.EnabledParameters{Severity: convertLevel(level)}
	return o.logger.Enabled(context.Background(), param)
}

// With adds structured context to the Core.
func (o *Core) With(fields []zapcore.Field) zapcore.Core {
	cloned := o.clone()
	if len(fields) > 0 {
		ctx, attrbuf := convertField(fields)
		if ctx != nil {
			cloned.ctx = ctx
		}
		cloned.attr = append(cloned.attr, attrbuf...)
	}
	return cloned
}

func (o *Core) clone() *Core {
	return &Core{
		provider: o.provider,
		opts:     o.opts,
		logger:   o.logger,
		attr:     slices.Clone(o.attr),
		ctx:      o.ctx,
	}
}

// Sync flushes buffered logs (if any).
func (o *Core) Sync() error {
	return nil
}

// Check determines whether the supplied Entry should be logged.
// If the entry should be logged, the Core adds itself to the CheckedEntry and returns the result.
func (o *Core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	param := log.EnabledParameters{Severity: convertLevel(ent.Level)}

	logger := o.logger
	if ent.LoggerName != "" {
		logger = o.provider.Logger(ent.LoggerName, o.opts...)
	}

	if logger.Enabled(context.Background(), param) {
		return ce.AddCore(ent, o)
	}
	return ce
}

// Write method encodes zap fields to OTel logs and emits them.
func (o *Core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	r := log.Record{}
	r.SetTimestamp(ent.Time)
	r.SetBody(log.StringValue(ent.Message))
	r.SetSeverity(convertLevel(ent.Level))
	r.SetSeverityText(ent.Level.String())

	r.AddAttributes(o.attr...)
	if ent.Caller.Defined {
		r.AddAttributes(
			log.String(string(semconv.CodeFilePathKey), ent.Caller.File),
			log.Int(string(semconv.CodeLineNumberKey), ent.Caller.Line),
			log.String(string(semconv.CodeFunctionNameKey), ent.Caller.Function),
		)
	}
	if ent.Stack != "" {
		r.AddAttributes(log.String(string(semconv.CodeStacktraceKey), ent.Stack))
	}
	emitCtx := o.ctx
	if len(fields) > 0 {
		ctx, attrbuf := convertField(fields)
		if ctx != nil {
			emitCtx = ctx
		}
		r.AddAttributes(attrbuf...)
	}

	logger := o.logger
	if ent.LoggerName != "" {
		logger = o.provider.Logger(ent.LoggerName, o.opts...)
	}
	logger.Emit(emitCtx, r)
	return nil
}

func convertField(fields []zapcore.Field) (context.Context, []log.KeyValue) {
	var ctx context.Context
	enc := newObjectEncoder(len(fields))
	for _, field := range fields {
		if ctxFld, ok := field.Interface.(context.Context); ok {
			ctx = ctxFld
			continue
		}
		field.AddTo(enc)
	}

	enc.calculate(enc.root)
	return ctx, enc.root.attrs
}

func convertLevel(level zapcore.Level) log.Severity {
	switch level {
	case zapcore.DebugLevel:
		return log.SeverityDebug
	case zapcore.InfoLevel:
		return log.SeverityInfo
	case zapcore.WarnLevel:
		return log.SeverityWarn
	case zapcore.ErrorLevel:
		return log.SeverityError
	case zapcore.DPanicLevel:
		return log.SeverityFatal1
	case zapcore.PanicLevel:
		return log.SeverityFatal2
	case zapcore.FatalLevel:
		return log.SeverityFatal3
	default:
		return log.SeverityUndefined
	}
}

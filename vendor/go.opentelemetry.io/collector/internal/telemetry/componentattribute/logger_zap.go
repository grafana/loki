// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package componentattribute // import "go.opentelemetry.io/collector/internal/telemetry/componentattribute"

import (
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Interface for Zap cores that support setting and resetting a set of component attributes.
//
// There are two wrappers that implement this interface:
//
//   - [NewConsoleCoreWithAttributes] injects component attributes as Zap fields.
//
//     This is used for the Collector's console output.
//
//   - [NewOTelTeeCoreWithAttributes] copies logs to a [log.LoggerProvider] using [otelzap]. For the
//     copied logs, component attributes are injected as instrumentation scope attributes.
//
//     This is used when service::telemetry::logs::processors is configured.
type coreWithAttributes interface {
	zapcore.Core
	withAttributeSet(attribute.Set) zapcore.Core
}

// Tries setting the component attribute set for a Zap core.
//
// Does nothing if the core does not implement [coreWithAttributes].
func tryWithAttributeSet(c zapcore.Core, attrs attribute.Set) zapcore.Core {
	if cwa, ok := c.(coreWithAttributes); ok {
		return cwa.withAttributeSet(attrs)
	}
	zap.New(c).Debug("Logger core does not support injecting component attributes")
	return c
}

type consoleCoreWithAttributes struct {
	zapcore.Core
	from        zapcore.Core
	extraFields []zap.Field
}

var _ coreWithAttributes = (*consoleCoreWithAttributes)(nil)

// NewConsoleCoreWithAttributes wraps a Zap core in order to inject component attributes as Zap fields.
//
// This is used for the Collector's console output.
func NewConsoleCoreWithAttributes(c zapcore.Core, attrs attribute.Set, extraFields ...zap.Field) zapcore.Core {
	return &consoleCoreWithAttributes{
		Core: c.With(ToZapFields(attrs)).With(extraFields),
		from: c,
	}
}

func (ccwa *consoleCoreWithAttributes) With(fields []zapcore.Field) zapcore.Core {
	return &consoleCoreWithAttributes{
		Core:        ccwa.Core.With(fields),
		from:        ccwa.from,
		extraFields: append(ccwa.extraFields, fields...),
	}
}

func (ccwa *consoleCoreWithAttributes) withAttributeSet(attrs attribute.Set) zapcore.Core {
	return NewConsoleCoreWithAttributes(ccwa.from, attrs, ccwa.extraFields...)
}

type otelTeeCoreWithAttributes struct {
	sourceCore zapcore.Core
	otelCore   zapcore.Core
	lp         log.LoggerProvider
	scopeName  string
}

var _ coreWithAttributes = (*otelTeeCoreWithAttributes)(nil)

// NewOTelTeeCoreWithAttributes wraps a Zap core in order to copy logs to a [log.LoggerProvider] using [otelzap].
// For the copied logs, component attributes are injected as instrumentation scope attributes.
//
// Note that we intentionally do not use zapcore.NewTee here, because it will simply duplicate all log entries
// to each core. The provided Zap core may have sampling or a minimum log level applied to it, so in order to
// maintain consistency we need to ensure that only the logs accepted by the provided core are copied to the
// log.LoggerProvider.
func NewOTelTeeCoreWithAttributes(core zapcore.Core, lp log.LoggerProvider, scopeName string, attrs attribute.Set) zapcore.Core {
	otelCore := otelzap.NewCore(
		scopeName,
		otelzap.WithLoggerProvider(lp),
		otelzap.WithAttributes(attrs.ToSlice()...),
	)
	return &otelTeeCoreWithAttributes{
		sourceCore: core,
		otelCore:   otelCore,
		lp:         lp,
		scopeName:  scopeName,
	}
}

func (ocwa *otelTeeCoreWithAttributes) withAttributeSet(attrs attribute.Set) zapcore.Core {
	return NewOTelTeeCoreWithAttributes(
		tryWithAttributeSet(ocwa.sourceCore, attrs),
		ocwa.lp, ocwa.scopeName, attrs,
	)
}

func (ocwa *otelTeeCoreWithAttributes) With(fields []zapcore.Field) zapcore.Core {
	sourceCoreWith := ocwa.sourceCore.With(fields)
	otelCoreWith := ocwa.otelCore.With(fields)
	return &otelTeeCoreWithAttributes{
		sourceCore: sourceCoreWith,
		otelCore:   otelCoreWith,
		lp:         ocwa.lp,
		scopeName:  ocwa.scopeName,
	}
}

func (ocwa *otelTeeCoreWithAttributes) Enabled(level zapcore.Level) bool {
	return ocwa.sourceCore.Enabled(level)
}

func (ocwa *otelTeeCoreWithAttributes) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	ce = ocwa.sourceCore.Check(entry, ce)
	if ce != nil {
		// Only log to the otelzap core if the input core accepted the log entry.
		ce = ce.AddCore(entry, ocwa.otelCore)
	}
	return ce
}

func (ocwa *otelTeeCoreWithAttributes) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	err := ocwa.sourceCore.Write(entry, fields)
	return multierr.Append(err, ocwa.otelCore.Write(entry, fields))
}

func (ocwa *otelTeeCoreWithAttributes) Sync() error {
	err := ocwa.sourceCore.Sync()
	return multierr.Append(err, ocwa.otelCore.Sync())
}

// ZapLoggerWithAttributes creates a Zap Logger with a new set of injected component attributes.
func ZapLoggerWithAttributes(logger *zap.Logger, attrs attribute.Set) *zap.Logger {
	return logger.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return tryWithAttributeSet(c, attrs)
	}))
}

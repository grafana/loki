// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package componentattribute // import "go.opentelemetry.io/collector/internal/telemetry/componentattribute"

import (
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
)

type loggerProviderWithAttributes struct {
	log.LoggerProvider
	attrs []attribute.KeyValue
}

// LoggerProviderWithAttributes creates a LoggerProvider with a new set of injected instrumentation scope attributes.
func LoggerProviderWithAttributes(lp log.LoggerProvider, attrs attribute.Set) log.LoggerProvider {
	if lpwa, ok := lp.(loggerProviderWithAttributes); ok {
		lp = lpwa.LoggerProvider
	}
	return loggerProviderWithAttributes{
		LoggerProvider: lp,
		attrs:          attrs.ToSlice(),
	}
}

func (lpwa loggerProviderWithAttributes) Logger(name string, opts ...log.LoggerOption) log.Logger {
	conf := log.NewLoggerConfig(opts...)
	attrSet := conf.InstrumentationAttributes()
	// prepend our attributes so they can be overwritten
	newAttrs := append(slices.Clone(lpwa.attrs), attrSet.ToSlice()...)
	// append our attribute set option to overwrite the old one
	opts = append(opts, log.WithInstrumentationAttributes(newAttrs...))
	return lpwa.LoggerProvider.Logger(name, opts...)
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

/*
Package global provides access to a global implementation of the OpenTelemetry
Logs Bridge API.

This package is experimental. It will be deprecated and removed when the [log]
package becomes stable. Its functionality will be migrated to
go.opentelemetry.io/otel.
*/
package global // import "go.opentelemetry.io/otel/log/global"

import (
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/internal/global"
)

// Logger returns a [log.Logger] configured with the provided name and options
// from the globally configured [log.LoggerProvider].
//
// If this is called before a global LoggerProvider is configured, the returned
// Logger will be a No-Op implementation of a Logger. When a global
// LoggerProvider is registered for the first time, the returned Logger is
// updated in-place to report to this new LoggerProvider. There is no need to
// call this function again for an updated instance.
//
// This is a convenience function. It is equivalent to:
//
//	GetLoggerProvider().Logger(name, options...)
func Logger(name string, options ...log.LoggerOption) log.Logger {
	return GetLoggerProvider().Logger(name, options...)
}

// GetLoggerProvider returns the globally configured [log.LoggerProvider].
//
// If a global LoggerProvider has not been configured with [SetLoggerProvider],
// the returned Logger will be a No-Op implementation of a LoggerProvider. When
// a global LoggerProvider is registered for the first time, the returned
// LoggerProvider and all of its created Loggers are updated in-place. There is
// no need to call this function again for an updated instance.
func GetLoggerProvider() log.LoggerProvider {
	return global.GetLoggerProvider()
}

// SetLoggerProvider configures provider as the global [log.LoggerProvider].
func SetLoggerProvider(provider log.LoggerProvider) {
	global.SetLoggerProvider(provider)
}

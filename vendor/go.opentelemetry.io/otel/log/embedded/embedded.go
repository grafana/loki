// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package embedded provides interfaces embedded within the [OpenTelemetry Logs
// Bridge API].
//
// Implementers of the [OpenTelemetry Logs Bridge API] can embed the relevant
// type from this package into their implementation directly. Doing so will
// result in a compilation error for users when the [OpenTelemetry Logs Bridge
// API] is extended (which is something that can happen without a major version
// bump of the API package).
//
// [OpenTelemetry Logs Bridge API]: https://pkg.go.dev/go.opentelemetry.io/otel/log
package embedded // import "go.opentelemetry.io/otel/log/embedded"

// LoggerProvider is embedded in the [Logs Bridge API LoggerProvider].
//
// Embed this interface in your implementation of the [Logs Bridge API
// LoggerProvider] if you want users to experience a compilation error,
// signaling they need to update to your latest implementation, when the [Logs
// Bridge API LoggerProvider] interface is extended (which is something that
// can happen without a major version bump of the API package).
//
// [Logs Bridge API LoggerProvider]: https://pkg.go.dev/go.opentelemetry.io/otel/log#LoggerProvider
type LoggerProvider interface{ loggerProvider() }

// Logger is embedded in [Logs Bridge API Logger].
//
// Embed this interface in your implementation of the [Logs Bridge API Logger]
// if you want users to experience a compilation error, signaling they need to
// update to your latest implementation, when the [Logs Bridge API Logger]
// interface is extended (which is something that can happen without a major
// version bump of the API package).
//
// [Logs Bridge API Logger]: https://pkg.go.dev/go.opentelemetry.io/otel/log#Logger
type Logger interface{ logger() }

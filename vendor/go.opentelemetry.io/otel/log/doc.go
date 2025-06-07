// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

/*
Package log provides the OpenTelemetry Logs API.

This package is intended to be used by bridges between existing logging
libraries and OpenTelemetry. Users should not directly use this package as a
logging library. Instead, install one of the bridges listed in the
[registry], and use the associated logging library.

# API Implementations

This package does not conform to the standard Go versioning policy, all of its
interfaces may have methods added to them without a package major version bump.
This non-standard API evolution could surprise an uninformed implementation
author. They could unknowingly build their implementation in a way that would
result in a runtime panic for their users that update to the new API.

The API is designed to help inform an instrumentation author about this
non-standard API evolution. It requires them to choose a default behavior for
unimplemented interface methods. There are three behavior choices they can
make:

  - Compilation failure
  - Panic
  - Default to another implementation

All interfaces in this API embed a corresponding interface from
[go.opentelemetry.io/otel/log/embedded]. If an author wants the default
behavior of their implementations to be a compilation failure, signaling to
their users they need to update to the latest version of that implementation,
they need to embed the corresponding interface from
[go.opentelemetry.io/otel/log/embedded] in their implementation. For example,

	import "go.opentelemetry.io/otel/log/embedded"

	type LoggerProvider struct {
		embedded.LoggerProvider
		// ...
	}

If an author wants the default behavior of their implementations to a panic,
they need to embed the API interface directly.

	import "go.opentelemetry.io/otel/log"

	type LoggerProvider struct {
		log.LoggerProvider
		// ...
	}

This is not a recommended behavior as it could lead to publishing packages that
contain runtime panics when users update other package that use newer versions
of [go.opentelemetry.io/otel/log].

Finally, an author can embed another implementation in theirs. The embedded
implementation will be used for methods not defined by the author. For example,
an author who wants to default to silently dropping the call can use
[go.opentelemetry.io/otel/log/noop]:

	import "go.opentelemetry.io/otel/log/noop"

	type LoggerProvider struct {
		noop.LoggerProvider
		// ...
	}

It is strongly recommended that authors only embed
go.opentelemetry.io/otel/log/noop if they choose this default behavior. That
implementation is the only one OpenTelemetry authors can guarantee will fully
implement all the API interfaces when a user updates their API.

[registry]: https://opentelemetry.io/ecosystem/registry/?language=go&component=log-bridge
*/
package log // import "go.opentelemetry.io/otel/log"

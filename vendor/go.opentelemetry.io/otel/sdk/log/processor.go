// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log // import "go.opentelemetry.io/otel/sdk/log"

import (
	"context"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/instrumentation"
)

// Processor handles the processing of log records.
//
// Any of the Processor's methods may be called concurrently with itself
// or with other methods. It is the responsibility of the Processor to manage
// this concurrency.
type Processor interface {
	// Enabled reports whether the Processor will process for the given context
	// and param.
	//
	// The passed param is likely to be partial record information being
	// provided (e.g. a param with only the Severity set).
	// If a Processor needs more information than is provided, it
	// is said to be in an indeterminate state (see below).
	//
	// The returned value will be true when the Processor will process for the
	// provided context and param, and will be false if the Processor will not
	// process. The returned value may be true or false in an indeterminate state.
	// An implementation should default to returning true for an indeterminate
	// state, but may return false if valid reasons in particular circumstances
	// exist (e.g. performance, correctness).
	//
	// The param should not be held by the implementation. A copy should be
	// made if the param needs to be held after the call returns.
	//
	// Processor implementations are expected to re-evaluate the [Record] passed
	// to OnEmit. It is not expected that the caller to OnEmit will
	// use the result from Enabled prior to calling OnEmit.
	//
	// The SDK's Logger.Enabled returns false if all the registered processors
	// return false. Otherwise, it returns true.
	//
	// Implementations of this method need to be safe for a user to call
	// concurrently.
	Enabled(ctx context.Context, param EnabledParameters) bool

	// OnEmit is called when a Record is emitted.
	//
	// OnEmit will be called independent of Enabled. Implementations need to
	// validate the arguments themselves before processing.
	//
	// Implementation should not interrupt the record processing
	// if the context is canceled.
	//
	// All retry logic must be contained in this function. The SDK does not
	// implement any retry logic. All errors returned by this function are
	// considered unrecoverable and will be reported to a configured error
	// Handler.
	//
	// The SDK invokes the processors sequentially in the same order as
	// they were registered using WithProcessor.
	// Implementations may synchronously modify the record so that the changes
	// are visible in the next registered processor.
	//
	// Note that Record is not concurrent safe. Therefore, asynchronous
	// processing may cause race conditions. Use Record.Clone
	// to create a copy that shares no state with the original.
	OnEmit(ctx context.Context, record *Record) error

	// Shutdown is called when the SDK shuts down. Any cleanup or release of
	// resources held by the exporter should be done in this call.
	//
	// The deadline or cancellation of the passed context must be honored. An
	// appropriate error should be returned in these situations.
	//
	// After Shutdown is called, calls to Export, Shutdown, or ForceFlush
	// should perform no operation and return nil error.
	Shutdown(ctx context.Context) error

	// ForceFlush exports log records to the configured Exporter that have not yet
	// been exported.
	//
	// The deadline or cancellation of the passed context must be honored. An
	// appropriate error should be returned in these situations.
	ForceFlush(ctx context.Context) error
}

// EnabledParameters represents payload for [Processor]'s Enabled method.
type EnabledParameters struct {
	InstrumentationScope instrumentation.Scope
	Severity             log.Severity
	EventName            string
}

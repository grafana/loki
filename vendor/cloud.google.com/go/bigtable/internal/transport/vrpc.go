// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
)

type contextKey struct{}

type vrpcMetadata struct {
	method  string
	attempt int
}

// WithVRpcMetadata returns a new context with virtual RPC metadata set.
func WithVRpcMetadata(ctx context.Context, method string, attempt int) context.Context {
	return context.WithValue(ctx, contextKey{}, &vrpcMetadata{
		method:  method,
		attempt: attempt,
	})
}

// WithAttempt returns a new context with the virtual RPC attempt updated.
func WithAttempt(ctx context.Context, attempt int) context.Context {
	if md, ok := ctx.Value(contextKey{}).(*vrpcMetadata); ok {
		return context.WithValue(ctx, contextKey{}, &vrpcMetadata{
			method:  md.method,
			attempt: attempt,
		})
	}
	return ctx
}

// VRpcMethod extracts the virtual RPC method name from the context.
func VRpcMethod(ctx context.Context) string {
	if md, ok := ctx.Value(contextKey{}).(*vrpcMetadata); ok {
		return md.method
	}
	return ""
}

// VRpcAttempt extracts the virtual RPC attempt number (1-indexed) from the context.
func VRpcAttempt(ctx context.Context) int {
	if md, ok := ctx.Value(contextKey{}).(*vrpcMetadata); ok {
		return md.attempt
	}
	return 0
}

// prevAttemptErrKey carries the previous attempt's error across retry
// boundaries inside RetryingVRpc. Lets the downstream Session.Invoke
// observe what caused the retry so sessionz can record a "retry"
// SessionEvent with the original gRPC code + message.
type prevAttemptErrKey struct{}

// WithPrevAttemptErr returns a derived context tagged with the prior
// attempt's error. Set by RetryingVRpc immediately before invoking the
// next attempt. A nil err clears the value.
func WithPrevAttemptErr(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, prevAttemptErrKey{}, err)
}

// PrevAttemptErr returns the previous attempt's error stashed by
// WithPrevAttemptErr, or nil if this is the first attempt or the context
// wasn't tagged.
func PrevAttemptErr(ctx context.Context) error {
	if err, ok := ctx.Value(prevAttemptErrKey{}).(error); ok {
		return err
	}
	return nil
}

// Handler represents the core execution function of a virtual RPC downstream invocation.
type Handler func(ctx context.Context, req interface{}) (interface{}, error)

// Interceptor represents a decorator middleware that intercepts a virtual RPC downstream invocation.
type Interceptor func(ctx context.Context, req interface{}, handler Handler) (interface{}, error)

// VRpcTracer receives per-attempt lifecycle notifications from
// RetryingVRpc. Matches Java's VRpcTracer role and the tracing/OTel
// vocabulary — this is a passive observer, not an event subscription
// (nothing is buffered or delivered asynchronously).
type VRpcTracer interface {
	// OnAttemptStart is called before a new attempt is executed.
	OnAttemptStart(ctx context.Context)
	// OnAttemptComplete is called after an attempt completes (with success or error).
	OnAttemptComplete(ctx context.Context, err error)
}

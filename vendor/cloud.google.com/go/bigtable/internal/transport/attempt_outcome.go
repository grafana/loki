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

import "errors"

// AttemptState classifies how far a single vRPC attempt progressed before
// terminating. RetryingVRpc consumes this instead of raw gRPC codes so
// retry decisions match Java's VRpc.VRpcResult.State semantics:
//
//   - Uncommitted: safe to retry unconditionally (server saw nothing).
//   - TransportFailure: safe only for idempotent ops (server may have applied).
//   - ServerResult: retry only if the server explicitly permits it via
//     RetryInfo, or the code is in the narrowed always-retryable set.
type AttemptState int

const (
	// StateServerResult is the zero value: any bare error from a code
	// path that hasn't yet adopted tagErr (or an error introduced by a
	// non-vRPC layer) classifies here. Under the strict Java-parity
	// default (see shouldRetryDefault in retrying.go) StateServerResult
	// does NOT retry unless the server explicitly attached RetryInfo.
	StateServerResult AttemptState = iota
	// StateUncommitted means the attempt never reached the wire — encode
	// failed, session was Closing, etc. Retry is safe regardless of
	// idempotency.
	StateUncommitted
	// StateTransportFailure means the frame was handed to the transport but
	// we never observed a server response — the server may or may not have
	// processed it. Retry only for idempotent ops.
	StateTransportFailure
)

func (s AttemptState) String() string {
	switch s {
	case StateUncommitted:
		return "Uncommitted"
	case StateTransportFailure:
		return "TransportFailure"
	case StateServerResult:
		return "ServerResult"
	}
	return "Unknown"
}

// AttemptOutcome pairs the classification with the underlying error so
// callers can inspect both. Err is always non-nil for a failed attempt.
type AttemptOutcome struct {
	State AttemptState
	Err   error
}

// vrpcErr wraps a raw error with its outcome so RetryingVRpc can classify
// by state. Unwrap() exposes the underlying err so status.FromError(err) and
// errors.Is(err, ...) continue to work for callers that don't know about
// the wrapper.
type vrpcErr struct {
	outcome AttemptOutcome
}

func (e *vrpcErr) Error() string { return e.outcome.Err.Error() }
func (e *vrpcErr) Unwrap() error { return e.outcome.Err }

// tagErr wraps err with the given AttemptState. Returns nil for nil err so
// call sites can compose without extra guards.
func tagErr(state AttemptState, err error) error {
	if err == nil {
		return nil
	}
	return &vrpcErr{outcome: AttemptOutcome{State: state, Err: err}}
}

// TagErr wraps err with the given AttemptState so callers outside this
// package can produce errors that carry the same classifier hint the
// real transport attaches. Intended for test doubles / fakes that stand
// in for a Session or a SessionPool: production errors from Session.Invoke
// are always tagged (see session_vrpc.go, session_pool.go), so a test's
// fake Invoker must tag its errors the same way for the RetryingVRpc
// interceptor's default classification to see them.
//
// Returns nil for nil err. Wraps unwrap()-transparently — errors.Is and
// status.FromError continue to see the underlying err.
func TagErr(state AttemptState, err error) error { return tagErr(state, err) }

// ClassifyErr returns the outcome for any error. Untagged errors fall
// through as StateServerResult — which, under the current default, is
// NOT retryable without server-attached RetryInfo. Callers that produce
// errors on paths where retry is expected must tag with the appropriate
// state via tagErr (see session_vrpc.go for the reference call sites).
func ClassifyErr(err error) AttemptOutcome {
	if err == nil {
		return AttemptOutcome{}
	}
	var v *vrpcErr
	if errors.As(err, &v) {
		return v.outcome
	}
	return AttemptOutcome{State: StateServerResult, Err: err}
}

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
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

// RetryingOptions configures the retry behavior of RetryingVRpc.
type RetryingOptions struct {
	// MaxAttempts caps the number of tries per Invoke — but ONLY for
	// non-server-directed retries (Uncommitted or TransportFailure with
	// no server RetryInfo). Attempts driven by server-attached
	// RetryInfo are uncapped, so a server that keeps sending
	// RetryDelay-carrying errors keeps getting retried until the
	// caller's ctx deadline runs out. Defaults to 3.
	MaxAttempts int32
	Tracer      VRpcTracer
	// Idempotent tells the interceptor whether TransportFailure attempts
	// (frame handed to the wire, no server response observed) are safe
	// to retry. Reads set this true; non-idempotent Apply sets it false.
	// Uncommitted attempts (never left the client) retry regardless.
	Idempotent bool
}

// RetryingVRpc returns an Interceptor that retries failed virtual RPCs
// per the AttemptState oracle (Uncommitted always, TransportFailure if
// idempotent) and honors server-attached RetryInfo. Backoff is
// server-driven: RetryInfo.retryDelay if the server sent one, otherwise
// immediate retry. There is no client-side exponential backoff.
func RetryingVRpc(opts RetryingOptions) Interceptor {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	maxAttempts := opts.MaxAttempts

	return func(ctx context.Context, req interface{}, next Handler) (interface{}, error) {
		var attempt int32
		tracer := opts.Tracer // may be nil
		var lastErr error

		for {
			attempt++

			attemptCtx := WithAttempt(ctx, int(attempt))
			// Tag the next attempt's context with the prior attempt's
			// error so the downstream Session.Invoke can record a
			// "retry" SessionEvent carrying the original gRPC code +
			// message. lastErr is nil on the very first attempt.
			if lastErr != nil {
				attemptCtx = WithPrevAttemptErr(attemptCtx, lastErr)
			}

			if tracer != nil {
				tracer.OnAttemptStart(attemptCtx)
			}

			res, err := next(attemptCtx, req)

			if tracer != nil {
				tracer.OnAttemptComplete(attemptCtx, err)
			}

			if err == nil {
				return res, nil
			}

			lastErr = err

			// Return the last attempt's typed error, not raw
			// context.Canceled/DeadlineExceeded. lastErr carries the
			// gRPC code + AttemptState tag callers need.
			if ctx.Err() != nil {
				return nil, lastErr
			}

			// Server RetryInfo is server-only authority. Only a
			// well-formed RetryDelay counts as the server saying
			// "retry"; an empty or malformed RetryInfo detail is not
			// enough to bypass the state-based default gate.
			var delay time.Duration
			serverPermitsRetry := false
			if s, ok := status.FromError(err); ok {
				for _, detail := range s.Details() {
					if retryInfo, ok := detail.(*errdetails.RetryInfo); ok {
						if retryInfo.GetRetryDelay().IsValid() {
							delay = retryInfo.GetRetryDelay().AsDuration()
							serverPermitsRetry = true
						}
						break
					}
				}
			}

			if serverPermitsRetry {
				// Server-directed retries are uncapped by MaxAttempts —
				// the server is telling us this specific request is
				// safe to retry, so we honor it until the caller's
				// ctx deadline runs out.
			} else {
				if !shouldRetryDefault(err, opts.Idempotent) {
					return nil, err
				}
				// The 3-attempt cap applies only to non-server-directed
				// retries. Once we've done maxAttempts and would need
				// to retry again for a non-server reason, stop.
				if attempt >= maxAttempts {
					return nil, lastErr
				}
			}

			// If a delay is scheduled (server RetryInfo) and it would
			// exhaust the caller's remaining deadline, skip the retry
			// entirely and surface lastErr. Waiting past the deadline
			// only to have the next attempt fail with DeadlineExceeded
			// loses the typed error and burns budget.
			if delay > 0 {
				if dl, ok := ctx.Deadline(); ok && time.Until(dl) < delay {
					return nil, lastErr
				}
				// time.NewTimer + Stop() (rather than time.After) so a
				// ctx-cancel exit path releases the timer immediately
				// instead of leaking it until delay elapses.
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, lastErr
				case <-timer.C:
				}
			}
			// If delay == 0 (no server RetryInfo, or a non-server-
			// directed retry) we loop immediately — no client backoff.
		}
	}
}

// shouldRetryDefault applies strict retry classification for the
// default retry path (no server RetryInfo). Server RetryInfo is handled
// by the caller before this fn is consulted.
//
// Retry rules (see AttemptState doc for state semantics):
//   - Uncommitted → always retry (server saw nothing).
//   - TransportFailure → retry only if idempotent (server may have applied).
//   - ServerResult → NEVER retry from this path. A bare server-explicit
//     error is NOT retried without an explicit server go-ahead
//     (RetryInfo handled upstream).
func shouldRetryDefault(err error, idempotent bool) bool {
	outcome := ClassifyErr(err)
	switch outcome.State {
	case StateUncommitted:
		return true
	case StateTransportFailure:
		return idempotent
	case StateServerResult:
		return false
	}
	return false
}

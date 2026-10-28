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

package session

import (
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultUnimplementedThreshold is the standard number of consecutive
// codes.Unimplemented responses required to trip the sticky breaker.
// Callers pass this to NewUnimplementedBreaker unless they
// have a specific reason to pick a different value (tests use small
// values to keep breaker-trip assertions fast).
const DefaultUnimplementedThreshold int32 = 30

// UnimplementedBreaker holds the session→classic fallback state for one
// resource. ShouldFallback answers "re-serve this request on classic?"
// per call; Bypass answers "stop dialing session at all?" per resource.
// Any non-Unimplemented reply resets the count, since it proves
// whichever backend served it understands the RPC.
//
// The trip is sticky, unlike Java's equivalent
// (SessionPoolImpl.consecutiveUnimplementedFailures, behind
// UnaryShim.supports). Java counts session opens, which keep happening
// while the gate is shut, so it recovers on its own. This counts
// diverted requests, so once the gate closes nothing can reset it.
// tripped just says so instead of leaving the count stuck forever.
//
// TODO: move this to the session pool and mirror Java — count
// Unimplemented on session open (transport already tracks
// consecutiveFailures) and gate on count < threshold || ReadyCount() > 0.
// Then fallback recovers and this type goes away.
type UnimplementedBreaker struct {
	threshold int32
	count     atomic.Int32
	tripped   atomic.Bool
}

// NewUnimplementedBreaker returns a breaker that trips after
// `threshold` consecutive Unimplemented responses. Pass
// DefaultUnimplementedThreshold in production.
func NewUnimplementedBreaker(threshold int32) *UnimplementedBreaker {
	return &UnimplementedBreaker{threshold: threshold}
}

// Bypass reports whether the breaker has tripped. Cheap atomic Load;
// callers use it to skip session before dialing.
func (b *UnimplementedBreaker) Bypass() bool {
	return b.tripped.Load()
}

// Count returns the current consecutive-Unimplemented count. Exposed
// for tests and observability; routing should call Bypass().
func (b *UnimplementedBreaker) Count() int32 {
	return b.count.Load()
}

// ShouldFallback records a session RPC's outcome and reports whether to
// re-serve the request on classic. One call does both, so routing can't
// record one verdict and then act on another.
//
//   - Unimplemented: count++, returns true. At the threshold the breaker
//     trips, via CompareAndSwap so a metric hook would fire once.
//   - anything else: count resets, returns false, and the caller keeps
//     whatever session produced.
func (b *UnimplementedBreaker) ShouldFallback(err error) bool {
	if err == nil || status.Code(err) != codes.Unimplemented {
		b.count.Store(0)
		return false
	}
	if n := b.count.Add(1); n >= b.threshold {
		b.tripped.CompareAndSwap(false, true)
	}
	return true
}

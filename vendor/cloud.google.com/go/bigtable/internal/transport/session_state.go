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

import "time"

// State represents the lifecycle state of a Session. Sessions move strictly
// forward through the values (monotonic by ordinal); once StateClosed is
// reached the session is terminal. Modeled on the SessionState enum in the
// Java Bigtable client (google-cloud-java).
type State int

const (
	// StateNew indicates the session is newly created and not yet started.
	StateNew State = iota
	// StateStarting indicates the session is dialing and handshaking
	// (OpenSession in flight).
	StateStarting
	// StateReady indicates the session has received OpenSessionResponse and
	// is ready to dispatch vRPCs.
	StateReady
	// StateClosing indicates a client-initiated close is in progress: the
	// session is draining outstanding vRPCs before sending CloseSession.
	StateClosing
	// StateWaitServerClose indicates CloseSession has been sent and the
	// session is waiting for the server's EOF (trailers) on the stream.
	StateWaitServerClose
	// StateClosed indicates the session is fully closed. Terminal.
	StateClosed
)

// String returns a human-readable name for the state.
func (s State) String() string {
	switch s {
	case StateNew:
		return "New"
	case StateStarting:
		return "Starting"
	case StateReady:
		return "Ready"
	case StateClosing:
		return "Closing"
	case StateWaitServerClose:
		return "WaitServerClose"
	case StateClosed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// transitionTo sets the session state to `to` iff ok(currentState) returns
// true. Returns the previous state and whether the transition was applied.
// Retries on CAS failure so a losing racer with a still-valid current state
// still transitions; the predicate is re-evaluated after each spurious loss.
func (s *Session) transitionTo(to State, ok func(State) bool) (prev State, applied bool) {
	for {
		prev = State(s.state.Load())
		if !ok(prev) {
			return prev, false
		}
		if s.state.CompareAndSwap(int32(prev), int32(to)) {
			s.lastStateChangeNano.Store(time.Now().UnixNano())
			return prev, true
		}
	}
}

// isState returns a predicate matching any of `allowed`.
func isState(allowed ...State) func(State) bool {
	return func(s State) bool {
		for _, a := range allowed {
			if s == a {
				return true
			}
		}
		return false
	}
}

// notState returns a predicate matching any state NOT in `forbidden`.
func notState(forbidden ...State) func(State) bool {
	return func(s State) bool {
		for _, f := range forbidden {
			if s == f {
				return false
			}
		}
		return true
	}
}

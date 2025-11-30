// Copyright 2025 Google LLC
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

package pubsub

import "time"

// ShutdownOptions configures the shutdown behavior of the subscriber.
// When ShutdownOptions is nil, the client library will
// assume disabled/infinite timeout.
//
// Warning: The interaction between Timeout and Behavior might be surprising.
// Read about the interaction of these below to ensure you
// get the desired behavior.
type ShutdownOptions struct {
	// Timeout specifies the time the subscriber should wait
	// before forcefully shutting down..
	// In ShutdownBehaviorNackImmediately mode, this configures the timeout
	// for message nacks before shutting down.
	//
	// Set to zero to immediately shutdown.
	// Set to a negative value to disable timeout.
	// Both zero and negative values overrides the ShutdownBehavior.
	Timeout time.Duration

	// Behavior defines the strategy the subscriber should use when
	// shutting down (wait or nack messages).
	// When ShutdownOptions is set, but Timeout is unspecified, the default zero-value
	// will result in immediate shutdown. When needing a specific a behavior,
	// always set a non-zero Timeout.
	Behavior ShutdownBehavior
}

// ShutdownBehavior defines the strategy the subscriber should take when
// shutting down. Current options are graceful shutdown vs nacking messages.
type ShutdownBehavior int

const (
	// ShutdownBehaviorWaitForProcessing means the subscriber client will wait for
	// outstanding messages to be processed.
	ShutdownBehaviorWaitForProcessing = iota

	// ShutdownBehaviorNackImmediately means the subscriber client will nack all
	// outstanding messages before closing.
	ShutdownBehaviorNackImmediately
)

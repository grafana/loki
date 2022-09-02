// Copyright 2017 Google LLC
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

import (
	"context"
	"errors"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

// LimitExceededBehavior configures the behavior that flowController can use in case
// the flow control limits are exceeded.
type LimitExceededBehavior int

const (
	// FlowControlIgnore disables flow control.
	FlowControlIgnore LimitExceededBehavior = iota
	// FlowControlBlock signals to wait until the request can be made without exceeding the limit.
	FlowControlBlock
	// FlowControlSignalError signals an error to the caller of acquire.
	FlowControlSignalError
)

// flowControllerPurpose indicates whether a flowController is for a topic or a
// subscription.
type flowControllerPurpose int

const (
	flowControllerPurposeSubscription flowControllerPurpose = iota
	flowControllerPurposeTopic
)

// FlowControlSettings controls flow control for messages while publishing or subscribing.
type FlowControlSettings struct {
	// MaxOutstandingMessages is the maximum number of buffered messages to be published.
	// If less than or equal to zero, this is disabled.
	MaxOutstandingMessages int

	// MaxOutstandingBytes is the maximum size of buffered messages to be published.
	// If less than or equal to zero, this is disabled.
	MaxOutstandingBytes int

	// LimitExceededBehavior configures the behavior when trying to publish
	// additional messages while the flow controller is full. The available options
	// are Ignore (disable, default), Block, and SignalError (publish
	// results will return an error).
	LimitExceededBehavior LimitExceededBehavior
}

var (
	// ErrFlowControllerMaxOutstandingMessages indicates that outstanding messages exceeds MaxOutstandingMessages.
	ErrFlowControllerMaxOutstandingMessages = errors.New("pubsub: MaxOutstandingMessages flow controller limit exceeded")

	// ErrFlowControllerMaxOutstandingBytes indicates that outstanding bytes of messages exceeds MaxOutstandingBytes.
	ErrFlowControllerMaxOutstandingBytes = errors.New("pubsub: MaxOutstandingBytes flow control limit exceeded")
)

// flowController implements flow control for publishing and subscribing.
type flowController struct {
	maxCount          int
	maxSize           int                 // max total size of messages
	semCount, semSize *semaphore.Weighted // enforces max number and size of messages
	// Number of calls to acquire - number of calls to release. This can go
	// negative if semCount == nil and a large acquire is followed by multiple
	// small releases.
	// Atomic.
	countRemaining int64
	// Number of outstanding bytes remaining. Atomic.
	bytesRemaining int64
	limitBehavior  LimitExceededBehavior
	purpose        flowControllerPurpose
}

// newFlowController creates a new flowController that ensures no more than
// maxCount messages or maxSize bytes are outstanding at once. If maxCount or
// maxSize is < 1, then an unlimited number of messages or bytes is permitted,
// respectively.
func newFlowController(fc FlowControlSettings) flowController {
	f := flowController{
		maxCount:      fc.MaxOutstandingMessages,
		maxSize:       fc.MaxOutstandingBytes,
		semCount:      nil,
		semSize:       nil,
		limitBehavior: fc.LimitExceededBehavior,
	}
	if fc.MaxOutstandingMessages > 0 {
		f.semCount = semaphore.NewWeighted(int64(fc.MaxOutstandingMessages))
	}
	if fc.MaxOutstandingBytes > 0 {
		f.semSize = semaphore.NewWeighted(int64(fc.MaxOutstandingBytes))
	}
	return f
}

func newTopicFlowController(fc FlowControlSettings) flowController {
	f := newFlowController(fc)
	f.purpose = flowControllerPurposeTopic
	return f
}

func newSubscriptionFlowController(fc FlowControlSettings) flowController {
	f := newFlowController(fc)
	f.purpose = flowControllerPurposeSubscription
	return f
}

// acquire allocates space for a message: the message count and its size.
//
// In FlowControlSignalError mode, large messages greater than maxSize
// will be result in an error. In other modes, large messages will be treated
// as if it were equal to maxSize.
func (f *flowController) acquire(ctx context.Context, size int) error {
	switch f.limitBehavior {
	case FlowControlIgnore:
		return nil
	case FlowControlBlock:
		if f.semCount != nil {
			if err := f.semCount.Acquire(ctx, 1); err != nil {
				return err
			}
		}
		if f.semSize != nil {
			if err := f.semSize.Acquire(ctx, f.bound(size)); err != nil {
				if f.semCount != nil {
					f.semCount.Release(1)
				}
				return err
			}
		}
	case FlowControlSignalError:
		if f.semCount != nil {
			if !f.semCount.TryAcquire(1) {
				return ErrFlowControllerMaxOutstandingMessages
			}
		}
		if f.semSize != nil {
			// Try to acquire the full size of the message here.
			if !f.semSize.TryAcquire(int64(size)) {
				if f.semCount != nil {
					f.semCount.Release(1)
				}
				return ErrFlowControllerMaxOutstandingBytes
			}
		}
	}

	if f.semCount != nil {
		outstandingMessages := atomic.AddInt64(&f.countRemaining, 1)
		f.recordOutstandingMessages(ctx, outstandingMessages)
	}

	if f.semSize != nil {
		outstandingBytes := atomic.AddInt64(&f.bytesRemaining, f.bound(size))
		f.recordOutstandingBytes(ctx, outstandingBytes)
	}
	return nil
}

// release notes that one message of size bytes is no longer outstanding.
func (f *flowController) release(ctx context.Context, size int) {
	if f.limitBehavior == FlowControlIgnore {
		return
	}

	if f.semCount != nil {
		outstandingMessages := atomic.AddInt64(&f.countRemaining, -1)
		f.recordOutstandingMessages(ctx, outstandingMessages)
		f.semCount.Release(1)
	}
	if f.semSize != nil {
		outstandingBytes := atomic.AddInt64(&f.bytesRemaining, -1*f.bound(size))
		f.recordOutstandingBytes(ctx, outstandingBytes)
		f.semSize.Release(f.bound(size))
	}
}

func (f *flowController) bound(size int) int64 {
	if size > f.maxSize {
		return int64(f.maxSize)
	}
	return int64(size)
}

// count returns the number of outstanding messages.
// if maxCount is 0, this will always return 0.
func (f *flowController) count() int {
	return int(atomic.LoadInt64(&f.countRemaining))
}

func (f *flowController) recordOutstandingMessages(ctx context.Context, n int64) {
	if f.purpose == flowControllerPurposeTopic {
		recordStat(ctx, PublisherOutstandingMessages, n)
		return
	}

	recordStat(ctx, OutstandingMessages, n)
}

func (f *flowController) recordOutstandingBytes(ctx context.Context, n int64) {
	if f.purpose == flowControllerPurposeTopic {
		recordStat(ctx, PublisherOutstandingBytes, n)
		return
	}

	recordStat(ctx, OutstandingBytes, n)
}

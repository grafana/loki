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

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	ipubsub "cloud.google.com/go/internal/pubsub"
	"cloud.google.com/go/pubsub/v2/internal/scheduler"
	"github.com/google/uuid"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

// Subscriber is a subscriber client that references a subscription.
type Subscriber struct {
	c *Client

	// The fully qualified identifier for the subscription, in the format "projects/<projid>/subscriptions/<name>"
	name string

	// Settings for pulling messages. Configure these before calling Receive.
	ReceiveSettings ReceiveSettings

	mu            sync.Mutex
	receiveActive bool

	// clientID to be used across all streaming pull connections that are created.
	// This indicates to the server that any guarantees made for a stream that
	// disconnected will be made for the stream that is created to replace it.
	clientID string
	// enableTracing enable otel tracing of Pub/Sub messages on this subscriber.
	// This is configured at client instantiation, and allows
	// disabling of tracing even when a tracer provider is detected.
	enableTracing bool
}

// Subscriber creates a subscriber client which references a single subscription.
func (c *Client) Subscriber(nameOrID string) *Subscriber {
	s := strings.Split(nameOrID, "/")
	// The string looks like a properly formatted topic name, use it directly.
	if len(s) == 4 {
		return newSubscriber(c, nameOrID)
	}
	// In all other cases, treat the arg as the topicID, even if misformatted.
	return newSubscriber(c, fmt.Sprintf("projects/%s/subscriptions/%s", c.projectID, nameOrID))
}

func newSubscriber(c *Client, name string) *Subscriber {
	return &Subscriber{
		c:               c,
		name:            name,
		clientID:        uuid.NewString(),
		ReceiveSettings: DefaultReceiveSettings,
		enableTracing:   c.enableTracing,
	}
}

// String returns the globally unique printable name of the subscription.
func (s *Subscriber) String() string {
	return s.name
}

// ID returns the unique identifier of the subscription within its project.
func (s *Subscriber) ID() string {
	slash := strings.LastIndex(s.name, "/")
	if slash == -1 {
		// name is not a fully-qualified name.
		panic("bad subscription name")
	}
	return s.name[slash+1:]
}

// ReceiveSettings configure the Receive method.
// A zero ReceiveSettings will result in values equivalent to DefaultReceiveSettings.
type ReceiveSettings struct {
	// MaxExtension is the maximum period for which the subscriber should
	// automatically extend the ack deadline for each message.
	//
	// The subscriber will automatically extend the ack deadline of all
	// fetched Messages up to the duration specified. Automatic deadline
	// extension beyond the initial receipt may be disabled by specifying a
	// duration less than 0.
	MaxExtension time.Duration

	// MaxDurationPerAckExtension is the maximum duration by which to extend the ack
	// deadline at a time. The ack deadline will continue to be extended by up
	// to this duration until MaxExtension is reached. Setting MaxDurationPerAckExtension
	// bounds the maximum amount of time before a message redelivery in the
	// event the subscriber fails to extend the deadline.
	//
	// MaxDurationPerAckExtension must be between 10s and 600s (inclusive). This configuration
	// can be disabled by specifying a duration less than (or equal to) 0.
	MaxDurationPerAckExtension time.Duration

	// MinDurationPerAckExtension is the the min duration for a single lease extension attempt.
	// By default the 99th percentile of ack latency is used to determine lease extension
	// periods but this value can be set to minimize the number of extraneous RPCs sent.
	//
	// MinDurationPerAckExtension must be between 10s and 600s (inclusive). This configuration
	// can be disabled by specifying a duration less than (or equal to) 0.
	// Disabled by default but set to 60 seconds if the subscription has exactly-once delivery enabled.
	MinDurationPerAckExtension time.Duration

	// MaxOutstandingMessages is the maximum number of unprocessed messages
	// (unacknowledged but not yet expired). If MaxOutstandingMessages is 0, it
	// will be treated as if it were DefaultReceiveSettings.MaxOutstandingMessages.
	// If the value is negative, then there will be no limit on the number of
	// unprocessed messages.
	MaxOutstandingMessages int

	// MaxOutstandingBytes is the maximum size of unprocessed messages
	// (unacknowledged but not yet expired). If MaxOutstandingBytes is 0, it will
	// be treated as if it were DefaultReceiveSettings.MaxOutstandingBytes. If
	// the value is negative, then there will be no limit on the number of bytes
	// for unprocessed messages.
	MaxOutstandingBytes int

	// NumGoroutines sets the number of StreamingPull streams to pull messages
	// from the subscription.
	//
	// NumGoroutines defaults to DefaultReceiveSettings.NumGoroutines.
	//
	// NumGoroutines does not limit the number of messages that can be processed
	// concurrently. Even with one goroutine, many messages might be processed at
	// once, because that goroutine may continually receive messages and invoke the
	// function passed to Receive on them. To limit the number of messages being
	// processed concurrently, set MaxOutstandingMessages.
	NumGoroutines int
}

// DefaultReceiveSettings holds the default values for ReceiveSettings.
var DefaultReceiveSettings = ReceiveSettings{
	MaxExtension:               60 * time.Minute,
	MaxDurationPerAckExtension: 0,
	MinDurationPerAckExtension: 0,
	MaxOutstandingMessages:     1000,
	MaxOutstandingBytes:        1e9, // 1G
	NumGoroutines:              1,
}

var errReceiveInProgress = errors.New("pubsub: Receive already in progress for this subscriber")

// Receive calls f with the outstanding messages from the subscription.
// It blocks until ctx is done, or the service returns a non-retryable error.
//
// The standard way to terminate a Receive is to cancel its context:
//
//	cctx, cancel := context.WithCancel(ctx)
//	err := sub.Receive(cctx, callback)
//	// Call cancel from callback, or another goroutine.
//
// If the service returns a non-retryable error, Receive returns that error after
// all of the outstanding calls to f have returned. If ctx is done, Receive
// returns nil after all of the outstanding calls to f have returned and
// all messages have been acknowledged or have expired.
//
// Receive calls f concurrently from multiple goroutines. It is encouraged to
// process messages synchronously in f, even if that processing is relatively
// time-consuming; Receive will spawn new goroutines for incoming messages,
// limited by MaxOutstandingMessages and MaxOutstandingBytes in ReceiveSettings.
//
// The context passed to f will be canceled when ctx is Done or there is a
// fatal service error.
//
// Receive will send an ack deadline extension on message receipt, then
// automatically extend the ack deadline of all fetched Messages up to the
// period specified by s.ReceiveSettings.MaxExtension.
//
// Each Subscriber may have only one invocation of Receive active at a time.
func (s *Subscriber) Receive(ctx context.Context, f func(context.Context, *Message)) error {
	s.mu.Lock()
	if s.receiveActive {
		s.mu.Unlock()
		return errReceiveInProgress
	}
	s.receiveActive = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.receiveActive = false; s.mu.Unlock() }()

	// TODO(hongalex): move settings check to a helper function to make it more testable
	maxCount := s.ReceiveSettings.MaxOutstandingMessages
	if maxCount == 0 {
		maxCount = DefaultReceiveSettings.MaxOutstandingMessages
	}
	maxBytes := s.ReceiveSettings.MaxOutstandingBytes
	if maxBytes == 0 {
		maxBytes = DefaultReceiveSettings.MaxOutstandingBytes
	}
	maxExt := s.ReceiveSettings.MaxExtension
	if maxExt == 0 {
		maxExt = DefaultReceiveSettings.MaxExtension
	} else if maxExt < 0 {
		// If MaxExtension is negative, disable automatic extension.
		maxExt = 0
	}
	maxExtPeriod := s.ReceiveSettings.MaxDurationPerAckExtension
	if maxExtPeriod < 0 {
		maxExtPeriod = DefaultReceiveSettings.MaxDurationPerAckExtension
	}
	minExtPeriod := s.ReceiveSettings.MinDurationPerAckExtension
	if minExtPeriod < 0 {
		minExtPeriod = DefaultReceiveSettings.MinDurationPerAckExtension
	}

	var numGoroutines int
	switch {
	case s.ReceiveSettings.NumGoroutines >= 1:
		numGoroutines = s.ReceiveSettings.NumGoroutines
	default:
		numGoroutines = DefaultReceiveSettings.NumGoroutines
	}
	// TODO(jba): add tests that verify that ReceiveSettings are correctly processed.
	po := &pullOptions{
		maxExtension:           maxExt,
		maxExtensionPeriod:     maxExtPeriod,
		minExtensionPeriod:     minExtPeriod,
		maxPrefetch:            trunc32(int64(maxCount)),
		maxOutstandingMessages: maxCount,
		maxOutstandingBytes:    maxBytes,
		clientID:               s.clientID,
	}
	fc := newSubscriberFlowController(FlowControlSettings{
		MaxOutstandingMessages: maxCount,
		MaxOutstandingBytes:    maxBytes,
		LimitExceededBehavior:  FlowControlBlock,
	})

	sched := scheduler.NewReceiveScheduler(maxCount)

	// Wait for all goroutines started by Receive to return, so instead of an
	// obscure goroutine leak we have an obvious blocked call to Receive.
	group, gctx := errgroup.WithContext(ctx)

	type closeablePair struct {
		wg   *sync.WaitGroup
		iter *messageIterator
	}

	var pairs []closeablePair

	// Cancel a sub-context which, when we finish a single receiver, will kick
	// off the context-aware callbacks and the goroutine below (which stops
	// all receivers, iterators, and the scheduler).
	ctx2, cancel2 := context.WithCancel(gctx)
	defer cancel2()

	for i := 0; i < numGoroutines; i++ {
		// The iterator does not use the context passed to Receive. If it did,
		// canceling that context would immediately stop the iterator without
		// waiting for unacked messages.
		iter := newMessageIterator(s.c.SubscriptionAdminClient, s.name, po)
		iter.enableTracing = s.enableTracing

		// We cannot use errgroup from Receive here. Receive might already be
		// calling group.Wait, and group.Wait cannot be called concurrently with
		// group.Go. We give each receive() its own WaitGroup instead.
		//
		// Since wg.Add is only called from the main goroutine, wg.Wait is
		// guaranteed to be called after all Adds.
		var wg sync.WaitGroup
		wg.Add(1)
		pairs = append(pairs, closeablePair{wg: &wg, iter: iter})

		group.Go(func() error {
			defer wg.Wait()
			defer cancel2()
			for {
				var maxToPull int32 // maximum number of messages to pull
				// If the context is done, don't pull more messages.
				select {
				case <-ctx.Done():
					return nil
				default:
				}

				msgs, err := iter.receive(maxToPull)
				if errors.Is(err, io.EOF) {
					return nil
				}
				if err != nil {
					return err
				}
				// If context is done and messages have been pulled,
				// nack them.
				select {
				case <-ctx.Done():
					for _, m := range msgs {
						m.Nack()
					}
					return nil
				default:
				}

				for i, msg := range msgs {
					msg := msg
					iter.eoMu.RLock()
					ackh, _ := msgAckHandler(msg, iter.enableExactlyOnceDelivery)
					iter.eoMu.RUnlock()
					// otelCtx is used to store the main subscribe span to the other child spans.
					// We want this to derive from the main subscribe ctx, so the iterator remains
					// cancellable.
					// We cannot reassign into ctx2 directly since this ctx should be different per
					// batch of messages and also per message iterator.
					otelCtx := ctx2
					// Stores the concurrency control span, which starts before the call to
					// acquire is made, and ends immediately after. This used to be called
					// flow control, but is more accurately describes as concurrency control
					// since this limits the number of simultaneous callback invocations.
					var ccSpan trace.Span
					if iter.enableTracing {
						c, ok := iter.activeSpans.Load(ackh.ackID)
						if ok {
							sc := c.(trace.Span)
							otelCtx = trace.ContextWithSpanContext(otelCtx, sc.SpanContext())
							// Don't override otelCtx here since the parent of subsequent spans
							// should be the subscribe span still.
							_, ccSpan = startSpan(otelCtx, ccSpanName, "")
						}
					}
					// Use the original user defined ctx for this operation so the acquire operation can be cancelled.
					if err := fc.acquire(ctx, len(msg.Data)); err != nil {
						// TODO(jba): test that these "orphaned" messages are nacked immediately when ctx is done.
						for _, m := range msgs[i:] {
							m.Nack()
						}
						// Return nil if the context is done, not err.
						return nil
					}
					if iter.enableTracing {
						ccSpan.End()
					}

					wg.Add(1)
					// Only schedule messages in order if an ordering key is present and the subscriber client
					// received the ordering flag from a Streaming Pull response.
					var key string
					iter.orderingMu.RLock()
					if iter.enableOrdering {
						key = msg.OrderingKey
					}
					// TODO(deklerk): Can we have a generic handler at the
					// constructor level?
					var schedulerSpan trace.Span
					if iter.enableTracing {
						_, schedulerSpan = startSpan(otelCtx, scheduleSpanName, "")
					}
					iter.orderingMu.RUnlock()
					msgLen := len(msg.Data)
					if err := sched.Add(key, msg, func(msg interface{}) {
						m := msg.(*Message)
						defer wg.Done()
						var ps trace.Span
						if iter.enableTracing {
							schedulerSpan.End()
							// Start the process span, and augment the done function to end this span and record events.
							otelCtx, ps = startSpan(otelCtx, processSpanName, s.ID())
							old := ackh.doneFunc
							ackh.doneFunc = func(ackID string, ack bool, r *ipubsub.AckResult, receiveTime time.Time) {
								var eventString string
								if ack {
									eventString = eventAckCalled
								} else {
									eventString = eventNackCalled
								}
								ps.AddEvent(eventString)
								// This is the process operation, but is currently named "Deliver". Replace once
								// updated here: https://github.com/open-telemetry/opentelemetry-go/blob/eb6bd28f3288b173d148c67f9ed45390594abdc2/semconv/v1.26.0/attribute_group.go#L5240
								ps.SetAttributes(semconv.MessagingOperationTypeDeliver)
								ps.End()
								old(ackID, ack, r, receiveTime)
							}
						}
						defer fc.release(ctx, msgLen)
						f(otelCtx, m)
					}); err != nil {
						wg.Done()
						// TODO(hongalex): propagate these errors to an otel span.

						// If there are any errors with scheduling messages,
						// nack them so they can be redelivered.
						msg.Nack()
						// Currently, only this error is returned by the receive scheduler.
						if errors.Is(err, scheduler.ErrReceiveDraining) {
							return nil
						}
						return err
					}
				}
			}
		})
	}

	go func() {
		<-ctx2.Done()

		// Wait for all iterators to stop.
		for _, p := range pairs {
			p.iter.stop()
			p.wg.Done()
		}

		// This _must_ happen after every iterator has stopped, or some
		// iterator will still have undelivered messages but the scheduler will
		// already be shut down.
		sched.Shutdown()
	}()

	return group.Wait()
}

type pullOptions struct {
	maxExtension       time.Duration // the maximum time to extend a message's ack deadline in total
	maxExtensionPeriod time.Duration // the maximum time to extend a message's ack deadline per modack rpc
	minExtensionPeriod time.Duration // the minimum time to extend a message's lease duration per modack
	maxPrefetch        int32         // the max number of outstanding messages, used to calculate maxToPull
	// If true, use unary Pull instead of StreamingPull, and never pull more
	// than maxPrefetch messages.
	maxOutstandingMessages int
	maxOutstandingBytes    int
	clientID               string
}

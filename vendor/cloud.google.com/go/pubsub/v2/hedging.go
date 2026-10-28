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

package pubsub

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	pb "cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	gax "github.com/googleapis/gax-go/v2"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

const (
	defaultHedgingDelay     time.Duration = 1 * time.Second
	defaultHedgingRatio     float64       = 0.1
	defaultMaxHedgingTokens int64         = 50

	minHedgingDelay       time.Duration = 100 * time.Millisecond
	maxHedgingDelay       time.Duration = 10 * time.Second
	maxHedgingTokensLimit int64         = 250
	minHedgingRatio       float64       = 0.001
	maxHedgingRatio       float64       = 0.2

	// tokenScaleFactor scales token bucket calculations to fixed-point integers
	// (1 token = 1,000 milli-tokens) to avoid floating-point accumulation drift.
	tokenScaleFactor int64 = 1000
)

var errPublisherHedgingAndOrderingEnabled = errors.New("pubsub: Hedging and MessageOrdering cannot both be enabled on the Publisher")

// HedgingSettings configures publish hedging, which sends additional copies of
// slow publish requests to help reduce tail latency.
//
// If a publish request does not complete within Delay, the publisher may send
// hedged copies of the batch at Delay intervals while tokens are available in
// the Publisher's token bucket. The first successful response resolves the
// batch and cancels the other attempts; errors from hedged attempts are
// ignored.
//
// Because a cancelled attempt may still be processed by the server, hedging
// can produce duplicate messages with distinct message IDs. PublishResult
// returns the ID from the attempt that resolved the batch.
//
// Hedging cannot be used with Publisher.EnableMessageOrdering; if both are
// enabled, Publish returns a PublishResult with an error.
//
// The zero value of each field uses its default.
type HedgingSettings struct {
	// Delay is how long to wait before sending a hedged publish request.
	// Defaults to 1s. Must be in [100ms, 10s].
	Delay time.Duration

	// MaxTokens is the capacity of the token bucket that rate-limits hedged
	// requests. Sending a hedged request consumes 1 token.
	// Defaults to 50. Must be in [1, 250].
	MaxTokens int64

	// RefillRatio is the number of tokens added to the bucket for each
	// successful publish, up to MaxTokens.
	// Defaults to 0.1. Must be in [0.001, 0.2].
	RefillRatio float64
}

func validateHedgingSettings(hs *HedgingSettings) error {
	if hs == nil {
		return nil
	}
	if hs.Delay != 0 && (hs.Delay < minHedgingDelay || hs.Delay > maxHedgingDelay) {
		return fmt.Errorf("pubsub: HedgingSettings.Delay (%v) must be between %v and %v", hs.Delay, minHedgingDelay, maxHedgingDelay)
	}
	if hs.MaxTokens != 0 && (hs.MaxTokens <= 0 || hs.MaxTokens > maxHedgingTokensLimit) {
		return fmt.Errorf("pubsub: HedgingSettings.MaxTokens (%d) must be > 0 and <= %d", hs.MaxTokens, maxHedgingTokensLimit)
	}
	if hs.RefillRatio != 0 && (hs.RefillRatio < minHedgingRatio || hs.RefillRatio > maxHedgingRatio) {
		return fmt.Errorf("pubsub: HedgingSettings.RefillRatio (%v) must be between %v and %v", hs.RefillRatio, minHedgingRatio, maxHedgingRatio)
	}
	return nil
}

// hedgedRequest is a scheduled hedged attempt in the hedging queue. It holds no
// reference to the batch payload, which lives on cs and is released as soon as
// the publish resolves, even while the request remains queued until sendAfter.
type hedgedRequest struct {
	attemptID int
	sendAfter time.Time
	cs        *cancellationSharer
}

func (req *hedgedRequest) isDone() bool {
	return req.cs.isDone()
}

// hedgeBatch is the per-batch state shared by all hedged attempts of a single
// publish.
type hedgeBatch struct {
	ctx       context.Context
	startTime time.Time
	resCh     chan *pb.PublishResponse
	pbMsgs    []*pb.PubsubMessage
	gaxOpts   []gax.CallOption
	bmsgs     []*bundledMessage
}

// cancellationSharer coordinates cancellation across publish attempts and owns
// the shared batch payload. The payload is cleared on cancelOthers or cancelAll,
// and inflight tracks active hedged attempts so publishHedged can wait for them
// to exit before returning results to the caller.
type cancellationSharer struct {
	mu       sync.Mutex
	cancels  map[int]context.CancelFunc
	done     atomic.Bool
	nextID   int
	batch    *hedgeBatch
	inflight sync.WaitGroup
}

func newCancellationSharer(batch *hedgeBatch) *cancellationSharer {
	return &cancellationSharer{
		cancels: make(map[int]context.CancelFunc),
		batch:   batch,
	}
}

func (cs *cancellationSharer) isDone() bool {
	return cs.done.Load()
}

// registerAttempt registers a cancel function and returns its ID.
// Returns -1 if the cancellationSharer is already done.
func (cs *cancellationSharer) registerAttempt(cancel context.CancelFunc) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.done.Load() {
		cancel()
		return -1
	}
	return cs.registerAttemptLocked(cancel)
}

// registerAttemptLocked registers a cancel function and returns its ID.
// cs.mu must be held.
func (cs *cancellationSharer) registerAttemptLocked(cancel context.CancelFunc) int {
	id := cs.nextID
	cs.nextID++
	cs.cancels[id] = cancel
	return id
}

// acquireHedge registers a hedged attempt and returns its ID, cancellable
// context, and batch payload, or ok=false if the publish has already resolved.
// The caller must call releaseHedge when done with the payload.
func (cs *cancellationSharer) acquireHedge() (id int, ctx context.Context, b *hedgeBatch, ok bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.done.Load() || cs.batch == nil {
		return 0, nil, nil, false
	}
	ctx, cancel := context.WithCancel(cs.batch.ctx)
	id = cs.registerAttemptLocked(cancel)
	// Add under mu while !done so it precedes wait(), which is called after cancelAll.
	cs.inflight.Add(1)
	return id, ctx, cs.batch, true
}

func (cs *cancellationSharer) releaseHedge() {
	cs.inflight.Done()
}

// cancelOthers marks the coordinator as done, releases the shared batch, and
// cancels all attempts except winnerID. Subsequent calls are no-ops.
func (cs *cancellationSharer) cancelOthers(winnerID int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.done.Load() {
		return
	}
	cs.done.Store(true)
	cs.batch = nil
	for id, cancel := range cs.cancels {
		if id != winnerID {
			cancel()
		}
	}
}

// cancelAll cancels all registered attempt contexts and releases the batch
// payload. It is safe to call more than once.
func (cs *cancellationSharer) cancelAll() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.done.Store(true)
	cs.batch = nil
	for _, cancel := range cs.cancels {
		cancel()
	}
	cs.cancels = nil
}

// wait blocks until every hedged attempt acquired via acquireHedge has called
// releaseHedge. It must be called after cancelAll.
func (cs *cancellationSharer) wait() {
	cs.inflight.Wait()
}

// publishHedged sends the initial publish synchronously and schedules hedged
// attempts via the hedging queue. It waits for any in-flight hedged attempts to
// exit before returning.
func (t *Publisher) publishHedged(ctx context.Context, start time.Time, pbMsgs []*pb.PubsubMessage, bms []*bundledMessage, gaxOpts []gax.CallOption) (*pb.PublishResponse, error) {
	resCh := make(chan *pb.PublishResponse, 1)
	cs := newCancellationSharer(&hedgeBatch{
		ctx:       ctx,
		startTime: start,
		resCh:     resCh,
		pbMsgs:    pbMsgs,
		gaxOpts:   gaxOpts,
		bmsgs:     bms,
	})

	mainCtx, mainCancel := context.WithCancel(ctx)
	mainID := cs.registerAttempt(mainCancel)
	t.enqueueHedgedRequest(&hedgedRequest{
		attemptID: 1,
		sendAfter: start.Add(t.hedgingDelay),
		cs:        cs,
	})
	mainCtx = metadata.AppendToOutgoingContext(
		mainCtx,
		pubsubClientTelemetryHeader,
		encodePubsubClientTelemetry(0, start),
	)
	r, e := t.c.TopicAdminClient.Publish(mainCtx, &pb.PublishRequest{
		Topic:    t.name,
		Messages: pbMsgs,
	}, gaxOpts...)

	if e == nil {
		select {
		case resCh <- r:
			cs.cancelOthers(mainID)
		default:
		}
	} else {
		// Terminal error on original attempt cancels any pending hedged attempts.
		cs.cancelAll()
	}

	var res *pb.PublishResponse
	var err error
	select {
	case res = <-resCh:
		t.replenishHedgingTokens()
	default:
		res, err = r, e
	}

	// Cancel outstanding hedges, release the shared payload, and wait for
	// in-flight hedges to exit before the caller releases flow control and
	// resolves PublishResults (after which the user may mutate msg.Data/Attributes).
	cs.cancelAll()
	cs.wait()
	return res, err
}

func (t *Publisher) stopHedging() {
	t.hedgingMu.Lock()
	defer t.hedgingMu.Unlock()
	t.hedgingStopped = true
	if t.hedgingTimer != nil {
		t.hedgingTimer.Stop()
		t.hedgingTimer = nil
	}
	t.hedgingQueue = nil
}

func (t *Publisher) enqueueHedgedRequest(req *hedgedRequest) {
	t.hedgingMu.Lock()
	defer t.hedgingMu.Unlock()
	if t.hedgingStopped {
		return
	}
	t.hedgingQueue = append(t.hedgingQueue, req)
	if len(t.hedgingQueue) == 1 {
		t.hedgingTimer = time.AfterFunc(max(time.Until(req.sendAfter), 0), t.processHedgingQueue)
	}
}

func (t *Publisher) drainDueHedgedRequests(now time.Time) []*hedgedRequest {
	t.hedgingMu.Lock()
	defer t.hedgingMu.Unlock()
	if t.hedgingStopped {
		return nil
	}

	var ready []*hedgedRequest
	for len(t.hedgingQueue) > 0 {
		head := t.hedgingQueue[0]
		if head.sendAfter.After(now) {
			break
		}
		ready = append(ready, head)
		t.hedgingQueue[0] = nil // don't retain the popped request in the backing array
		t.hedgingQueue = t.hedgingQueue[1:]
	}

	if len(t.hedgingQueue) > 0 {
		t.hedgingTimer = time.AfterFunc(max(time.Until(t.hedgingQueue[0].sendAfter), 0), t.processHedgingQueue)
	} else {
		t.hedgingTimer = nil
	}
	return ready
}

func (t *Publisher) processHedgingQueue() {
	ready := t.drainDueHedgedRequests(time.Now())
	for _, req := range ready {
		if !req.isDone() {
			go t.fireHedgedAttempt(req)
		}
	}
}

func (t *Publisher) fireHedgedAttempt(req *hedgedRequest) {
	id, hedgedCtx, b, ok := req.cs.acquireHedge()
	if !ok {
		return
	}
	defer req.cs.releaseHedge()
	if b.ctx.Err() != nil {
		return
	}

	var timeout time.Duration
	if deadline, ok := b.ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	} else {
		timeout = t.PublishSettings.Timeout - time.Since(b.startTime)
	}
	if timeout <= 0 || !t.tryAcquireHedgingToken() {
		return
	}
	timeout = min(timeout, 10*time.Second)

	t.enqueueHedgedRequest(&hedgedRequest{
		attemptID: req.attemptID + 1,
		sendAfter: time.Now().Add(t.hedgingDelay),
		cs:        req.cs,
	})

	// Hedged attempts are not retried; their errors are ignored so they do not
	// fail the batch while the original attempt is still running.
	opts := append([]gax.CallOption(nil), b.gaxOpts...)
	opts = append(opts,
		gax.WithRetry(func() gax.Retryer { return gax.OnCodes([]codes.Code{}, gax.Backoff{}) }),
		gax.WithTimeout(timeout),
	)

	if t.enableTracing {
		for _, m := range b.bmsgs {
			m.createSpan.AddEvent(eventHedgedPublishStart, trace.WithAttributes(semconv.MessagingBatchMessageCount(len(b.bmsgs))))
		}
	}

	hedgedCtx = metadata.AppendToOutgoingContext(
		hedgedCtx,
		pubsubClientTelemetryHeader,
		encodePubsubClientTelemetry(req.attemptID, b.startTime),
	)

	r, e := t.c.TopicAdminClient.Publish(hedgedCtx, &pb.PublishRequest{
		Topic:    t.name,
		Messages: b.pbMsgs,
	}, opts...)

	if t.enableTracing {
		for _, m := range b.bmsgs {
			m.createSpan.AddEvent(eventHedgedPublishEnd)
		}
	}

	if e == nil {
		select {
		case b.resCh <- r:
			req.cs.cancelOthers(id)
		default:
		}
	}
}

// initHedging validates HedgingSettings and snapshots the effective hedging
// configuration. It is called once from initBundler.
func (t *Publisher) initHedging() {
	hs := t.PublishSettings.HedgingSettings
	if hs == nil || t.EnableMessageOrdering {
		return
	}
	if err := validateHedgingSettings(hs); err != nil {
		t.hedgingSettingsErr = err
		return
	}

	delay := hs.Delay
	if delay == 0 {
		delay = defaultHedgingDelay
	}
	ratio := hs.RefillRatio
	if !(ratio > 0) { // also catches NaN, which passes validation
		ratio = defaultHedgingRatio
	}
	maxTokens := hs.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxHedgingTokens
	}

	t.hedgingDelay = delay
	t.hedgingRefillMilliTokens = int64(math.Round(ratio * float64(tokenScaleFactor)))
	t.hedgingMaxMilliTokens = maxTokens * tokenScaleFactor
}

// tryAcquireHedgingToken takes one token from the bucket if a full token is
// available, and reports whether it did.
func (t *Publisher) tryAcquireHedgingToken() bool {
	for {
		cur := t.hedgingTokenBucket.Load()
		if cur < tokenScaleFactor {
			return false
		}
		if t.hedgingTokenBucket.CompareAndSwap(cur, cur-tokenScaleFactor) {
			return true
		}
	}
}

// replenishHedgingTokens adds the per-success refill to the bucket, capped at
// the configured maximum. It is called after every successful publish.
func (t *Publisher) replenishHedgingTokens() {
	for {
		cur := t.hedgingTokenBucket.Load()
		if cur >= t.hedgingMaxMilliTokens {
			return
		}
		next := min(cur+t.hedgingRefillMilliTokens, t.hedgingMaxMilliTokens)
		if t.hedgingTokenBucket.CompareAndSwap(cur, next) {
			return
		}
	}
}

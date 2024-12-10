// Copyright 2016 Google LLC
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
	"io"
	"log"
	"strings"
	"sync"
	"time"

	ipubsub "cloud.google.com/go/internal/pubsub"
	vkit "cloud.google.com/go/pubsub/apiv1"
	pb "cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/internal/distribution"
	gax "github.com/googleapis/gax-go/v2"
	"github.com/googleapis/gax-go/v2/apierror"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// Between message receipt and ack (that is, the time spent processing a message) we want to extend the message
// deadline by way of modack. However, we don't want to extend the deadline right as soon as the deadline expires;
// instead, we'd want to extend the deadline a little bit of time ahead. gracePeriod is that amount of time ahead
// of the actual deadline.
const gracePeriod = 5 * time.Second

// ackIDBatchSize is the maximum number of ACK IDs to send in a single Ack/Modack RPC.
// The backend imposes a maximum request size limit of 524288 bytes (512 KiB) per
// acknowledge / modifyAckDeadline request. ACK IDs have a maximum size of 164
// bytes, thus we cannot send more than 524288/176 ~= 2979 ACK IDs in an Ack/ModAc

// Accounting for some overhead, we should thus only send a maximum of 2500 ACK
// IDs at a time.
// This is a var such that it can be modified for tests.
const ackIDBatchSize int = 2500

// These are vars so tests can change them.
var (
	maxDurationPerLeaseExtension            = 10 * time.Minute
	minDurationPerLeaseExtension            = 10 * time.Second
	minDurationPerLeaseExtensionExactlyOnce = 1 * time.Minute

	// The total amount of time to retry acks/modacks with exactly once delivery enabled subscriptions.
	exactlyOnceDeliveryRetryDeadline = 600 * time.Second
)

type messageIterator struct {
	ctx           context.Context
	cancel        func() // the function that will cancel ctx; called in stop
	po            *pullOptions
	ps            *pullStream
	subc          *vkit.SubscriberClient
	projectID     string
	subID         string
	subName       string
	kaTick        <-chan time.Time // keep-alive (deadline extensions)
	ackTicker     *time.Ticker     // message acks
	nackTicker    *time.Ticker     // message nacks
	pingTicker    *time.Ticker     //  sends to the stream to keep it open
	receiptTicker *time.Ticker     // sends receipt modacks
	failed        chan struct{}    // closed on stream error
	drained       chan struct{}    // closed when stopped && no more pending messages
	wg            sync.WaitGroup

	// This mutex guards the structs related to lease extension.
	mu          sync.Mutex
	ackTimeDist *distribution.D // dist uses seconds

	// keepAliveDeadlines is a map of id to expiration time. This map is used in conjunction with
	// subscription.ReceiveSettings.MaxExtension to record the maximum amount of time (the
	// deadline, more specifically) we're willing to extend a message's ack deadline. As each
	// message arrives, we'll record now+MaxExtension in this table; whenever we have a chance
	// to update ack deadlines (via modack), we'll consult this table and only include IDs
	// that are not beyond their deadline.
	keepAliveDeadlines map[string]time.Time
	pendingAcks        map[string]*AckResult
	pendingNacks       map[string]*AckResult
	// ack IDs whose ack deadline is to be modified
	// ModAcks don't have AckResults but allows reuse of the SendModAck function.
	pendingModAcks map[string]*AckResult
	// ack IDs whose receipt need to be acknowledged with a modack.
	pendingReceipts map[string]*AckResult
	err             error // error from stream failure

	eoMu                      sync.RWMutex
	enableExactlyOnceDelivery bool
	sendNewAckDeadline        bool

	orderingMu sync.RWMutex
	// enableOrdering determines if messages should be processed in order. This is populated
	// by the response in StreamingPull and can change mid Receive. Must be accessed
	// with the lock held.
	enableOrdering bool

	// enableTracing enables span creation for this subscriber iterator.
	enableTracing bool
	// This maps trace ackID (string) to root subscribe spans(trace.Span), used for otel tracing.
	// Active ackIDs in this map should also exist 1:1 with ids in keepAliveDeadlines.
	// Elements are removed when messages are acked, nacked, or expired in iterator.handleKeepAlives()
	activeSpans sync.Map
}

// newMessageIterator starts and returns a new messageIterator.
// subName is the full name of the subscription to pull messages from.
// Stop must be called on the messageIterator when it is no longer needed.
// The iterator always uses the background context for acking messages and extending message deadlines.
func newMessageIterator(subc *vkit.SubscriberClient, subName string, po *pullOptions) *messageIterator {
	var ps *pullStream
	if !po.synchronous {
		maxMessages := po.maxOutstandingMessages
		maxBytes := po.maxOutstandingBytes
		if po.useLegacyFlowControl {
			maxMessages = 0
			maxBytes = 0
		}
		ps = newPullStream(context.Background(), subc.StreamingPull, subName, po.clientID, maxMessages, maxBytes, po.maxExtensionPeriod)
	}
	// The period will update each tick based on the distribution of acks. We'll start by arbitrarily sending
	// the first keepAlive halfway towards the minimum ack deadline.
	keepAlivePeriod := minDurationPerLeaseExtension / 2

	// Ack promptly so users don't lose work if client crashes.
	ackTicker := time.NewTicker(100 * time.Millisecond)
	nackTicker := time.NewTicker(100 * time.Millisecond)
	pingTicker := time.NewTicker(30 * time.Second)
	receiptTicker := time.NewTicker(100 * time.Millisecond)
	cctx, cancel := context.WithCancel(context.Background())
	cctx = withSubscriptionKey(cctx, subName)

	projectID, subID := parseResourceName(subName)

	it := &messageIterator{
		ctx:                cctx,
		cancel:             cancel,
		ps:                 ps,
		po:                 po,
		subc:               subc,
		projectID:          projectID,
		subID:              subID,
		subName:            subName,
		kaTick:             time.After(keepAlivePeriod),
		ackTicker:          ackTicker,
		nackTicker:         nackTicker,
		pingTicker:         pingTicker,
		receiptTicker:      receiptTicker,
		failed:             make(chan struct{}),
		drained:            make(chan struct{}),
		ackTimeDist:        distribution.New(int(maxDurationPerLeaseExtension/time.Second) + 1),
		keepAliveDeadlines: map[string]time.Time{},
		pendingAcks:        map[string]*AckResult{},
		pendingNacks:       map[string]*AckResult{},
		pendingModAcks:     map[string]*AckResult{},
		pendingReceipts:    map[string]*AckResult{},
	}
	it.wg.Add(1)
	go it.sender()
	return it
}

// Subscription.receive will call stop on its messageIterator when finished with it.
// Stop will block until Done has been called on all Messages that have been
// returned by Next, or until the context with which the messageIterator was created
// is cancelled or exceeds its deadline.
func (it *messageIterator) stop() {
	it.cancel()
	it.mu.Lock()
	it.checkDrained()
	it.mu.Unlock()
	it.wg.Wait()
	if it.ps != nil {
		it.ps.cancel()
	}
}

// checkDrained closes the drained channel if the iterator has been stopped and all
// pending messages have either been n/acked or expired.
//
// Called with the lock held.
func (it *messageIterator) checkDrained() {
	select {
	case <-it.drained:
		return
	default:
	}
	select {
	case <-it.ctx.Done():
		if len(it.keepAliveDeadlines) == 0 {
			close(it.drained)
		}
	default:
	}
}

// Given a receiveTime, add the elapsed time to the iterator's ack distribution.
// These values are bounded by the ModifyAckDeadline limits, which are
// min/maxDurationPerLeaseExtension.
func (it *messageIterator) addToDistribution(receiveTime time.Time) {
	d := time.Since(receiveTime)
	d = maxDuration(d, minDurationPerLeaseExtension)
	d = minDuration(d, maxDurationPerLeaseExtension)
	it.ackTimeDist.Record(int(d / time.Second))
}

// Called when a message is acked/nacked.
func (it *messageIterator) done(ackID string, ack bool, r *AckResult, receiveTime time.Time) {
	it.addToDistribution(receiveTime)
	it.mu.Lock()
	defer it.mu.Unlock()
	delete(it.keepAliveDeadlines, ackID)
	if ack {
		it.pendingAcks[ackID] = r
	} else {
		it.pendingNacks[ackID] = r
	}
	it.checkDrained()
}

// fail is called when a stream method returns a permanent error.
// fail returns it.err. This may be err, or it may be the error
// set by an earlier call to fail.
func (it *messageIterator) fail(err error) error {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.err == nil {
		it.err = err
		close(it.failed)
	}
	return it.err
}

// receive makes a call to the stream's Recv method, or the Pull RPC, and returns
// its messages.
// maxToPull is the maximum number of messages for the Pull RPC.
func (it *messageIterator) receive(maxToPull int32) ([]*Message, error) {
	it.mu.Lock()
	ierr := it.err
	it.mu.Unlock()
	if ierr != nil {
		return nil, ierr
	}

	// Stop retrieving messages if the iterator's Stop method was called.
	select {
	case <-it.ctx.Done():
		it.wg.Wait()
		return nil, io.EOF
	default:
	}

	var rmsgs []*pb.ReceivedMessage
	var err error
	if it.po.synchronous {
		rmsgs, err = it.pullMessages(maxToPull)
	} else {
		rmsgs, err = it.recvMessages()
		// If stopping the iterator results in the grpc stream getting shut down and
		// returning an error here, treat the same as above and return EOF.
		// If the cancellation comes from the underlying grpc client getting closed,
		// do propagate the cancellation error.
		// See https://github.com/googleapis/google-cloud-go/pull/10153#discussion_r1600814775
		if err != nil && errors.Is(it.ps.ctx.Err(), context.Canceled) {
			err = io.EOF
		}
	}
	// Any error here is fatal.
	if err != nil {
		return nil, it.fail(err)
	}

	recordStat(it.ctx, PullCount, int64(len(rmsgs)))

	now := time.Now()
	msgs, err := convertMessages(rmsgs, now, it.done)
	if err != nil {
		return nil, it.fail(err)
	}
	// We received some messages. Remember them so we can keep them alive. Also,
	// do a receipt mod-ack when streaming.
	maxExt := time.Now().Add(it.po.maxExtension)
	ackIDs := map[string]*AckResult{}
	it.eoMu.RLock()
	exactlyOnceDelivery := it.enableExactlyOnceDelivery
	it.eoMu.RUnlock()
	it.mu.Lock()

	// pendingMessages maps ackID -> message, and is used
	// only when exactly once delivery is enabled.
	// At first, all messages are pending, and they
	// are removed if the modack call fails. All other
	// messages are returned to the client for processing.
	pendingMessages := make(map[string]*ipubsub.Message)
	for _, m := range msgs {
		ackID := msgAckID(m)
		addRecv(m.ID, ackID, now)
		it.keepAliveDeadlines[ackID] = maxExt
		// Don't change the mod-ack if the message is going to be nacked. This is
		// possible if there are retries.
		if _, ok := it.pendingNacks[ackID]; !ok {
			// Don't use the message's AckResult here since these are only for receipt modacks.
			// modack results are transparent to the user so these can automatically succeed unless
			// exactly once is enabled.
			// We can't use an empty AckResult here either since SetAckResult will try to
			// close the channel without checking if it exists.
			if !exactlyOnceDelivery {
				ackIDs[ackID] = newSuccessAckResult()
			} else {
				ackIDs[ackID] = ipubsub.NewAckResult()
				pendingMessages[ackID] = m
			}
		}

		if it.enableTracing {
			ctx := context.Background()
			if m.Attributes != nil {
				ctx = propagation.TraceContext{}.Extract(ctx, newMessageCarrier(m))
			}
			opts := getSubscriberOpts(it.projectID, it.subID, m)
			opts = append(
				opts,
				trace.WithAttributes(
					attribute.Bool(eosAttribute, it.enableExactlyOnceDelivery),
					semconv.MessagingGCPPubsubMessageAckID(ackID),
					semconv.MessagingBatchMessageCount(len(msgs)),
					semconv.CodeFunction("receive"),
				),
			)
			_, span := startSpan(ctx, subscribeSpanName, it.subID, opts...)
			// Always store the subscribe span, even if sampling isn't enabled.
			// This is useful since we need to propagate the sampling flag
			// to the callback in Receive, so traces have an unbroken sampling decision.
			it.activeSpans.Store(ackID, span)
		}
	}
	deadline := it.ackDeadline()
	it.mu.Unlock()

	if len(ackIDs) > 0 {
		if !exactlyOnceDelivery {
			// When exactly once delivery is not enabled, modacks are fire and forget.
			// Add pending receipt modacks to queue to batch with other modacks.
			it.mu.Lock()
			for id := range ackIDs {
				// Use a SuccessAckResult (dummy) since we don't propagate modacks back to the user.
				it.pendingReceipts[id] = newSuccessAckResult()
			}
			it.mu.Unlock()
			return msgs, nil
		}

		// If exactly once is enabled, we should wait until modack responses are successes
		// before attempting to process messages.
		it.sendModAck(ackIDs, deadline, false, true)
		for ackID, ar := range ackIDs {
			ctx := context.Background()
			_, err := ar.Get(ctx)
			if err != nil {
				delete(pendingMessages, ackID)
				it.mu.Lock()
				// Remove the message from lease management if modack fails here.
				delete(it.keepAliveDeadlines, ackID)
				it.mu.Unlock()
			}
		}
		// Only return for processing messages that were successfully modack'ed.
		// Iterate over the original messages slice for ordering.
		v := make([]*ipubsub.Message, 0, len(pendingMessages))
		for _, m := range msgs {
			ackID := msgAckID(m)
			if _, ok := pendingMessages[ackID]; ok {
				v = append(v, m)
			}
		}
		return v, nil
	}
	return nil, nil
}

// Get messages using the Pull RPC.
// This may block indefinitely. It may also return zero messages, after some time waiting.
func (it *messageIterator) pullMessages(maxToPull int32) ([]*pb.ReceivedMessage, error) {
	// Use it.ctx as the RPC context, so that if the iterator is stopped, the call
	// will return immediately.
	res, err := it.subc.Pull(it.ctx, &pb.PullRequest{
		Subscription: it.subName,
		MaxMessages:  maxToPull,
	}, gax.WithGRPCOptions(grpc.MaxCallRecvMsgSize(maxSendRecvBytes)))
	switch {
	case errors.Is(err, context.Canceled):
		return nil, nil
	case status.Code(err) == codes.Canceled:
		return nil, nil
	case err != nil:
		return nil, err
	default:
		return res.ReceivedMessages, nil
	}
}

func (it *messageIterator) recvMessages() ([]*pb.ReceivedMessage, error) {
	res, err := it.ps.Recv()
	if err != nil {
		return nil, err
	}

	// If the new exactly once settings are different than the current settings, update it.
	it.eoMu.RLock()
	enableEOD := it.enableExactlyOnceDelivery
	it.eoMu.RUnlock()

	subProp := res.GetSubscriptionProperties()
	if got := subProp.GetExactlyOnceDeliveryEnabled(); got != enableEOD {
		it.eoMu.Lock()
		it.sendNewAckDeadline = true
		it.enableExactlyOnceDelivery = got
		it.eoMu.Unlock()
	}

	// Also update the subscriber's ordering setting if stale.
	it.orderingMu.RLock()
	enableOrdering := it.enableOrdering
	it.orderingMu.RUnlock()

	if got := subProp.GetMessageOrderingEnabled(); got != enableOrdering {
		it.orderingMu.Lock()
		it.enableOrdering = got
		it.orderingMu.Unlock()
	}
	return res.ReceivedMessages, nil
}

// sender runs in a goroutine and handles all sends to the stream.
func (it *messageIterator) sender() {
	defer it.wg.Done()
	defer it.ackTicker.Stop()
	defer it.nackTicker.Stop()
	defer it.pingTicker.Stop()
	defer it.receiptTicker.Stop()
	defer func() {
		if it.ps != nil {
			it.ps.CloseSend()
		}
	}()

	done := false
	for !done {
		sendAcks := false
		sendNacks := false
		sendModAcks := false
		sendPing := false
		sendReceipt := false

		dl := it.ackDeadline()

		select {
		case <-it.failed:
			// Stream failed: nothing to do, so stop immediately.
			return

		case <-it.drained:
			// All outstanding messages have been marked done:
			// nothing left to do except make the final calls.
			it.mu.Lock()
			sendAcks = (len(it.pendingAcks) > 0)
			sendNacks = (len(it.pendingNacks) > 0)
			// No point in sending modacks.
			done = true

		case <-it.kaTick:
			it.mu.Lock()
			it.handleKeepAlives()
			sendModAcks = (len(it.pendingModAcks) > 0)

			nextTick := dl - gracePeriod
			if nextTick <= 0 {
				// If the deadline is <= gracePeriod, let's tick again halfway to
				// the deadline.
				nextTick = dl / 2
			}
			it.kaTick = time.After(nextTick)

		case <-it.nackTicker.C:
			it.mu.Lock()
			sendNacks = (len(it.pendingNacks) > 0)

		case <-it.ackTicker.C:
			it.mu.Lock()
			sendAcks = (len(it.pendingAcks) > 0)

		case <-it.pingTicker.C:
			it.mu.Lock()
			// Ping only if we are processing messages via streaming.
			sendPing = !it.po.synchronous
		case <-it.receiptTicker.C:
			it.mu.Lock()
			sendReceipt = (len(it.pendingReceipts) > 0)
		}
		// Lock is held here.
		var acks, nacks, modAcks, receipts map[string]*AckResult
		if sendAcks {
			acks = it.pendingAcks
			it.pendingAcks = map[string]*AckResult{}
		}
		if sendNacks {
			nacks = it.pendingNacks
			it.pendingNacks = map[string]*AckResult{}
		}
		if sendModAcks {
			modAcks = it.pendingModAcks
			it.pendingModAcks = map[string]*AckResult{}
		}
		if sendReceipt {
			receipts = it.pendingReceipts
			it.pendingReceipts = map[string]*AckResult{}
		}
		it.mu.Unlock()
		// Make Ack and ModAck RPCs.
		if sendAcks {
			it.sendAck(acks)
		}
		if sendNacks {
			// Nack indicated by modifying the deadline to zero.
			it.sendModAck(nacks, 0, false, false)
		}
		if sendModAcks {
			it.sendModAck(modAcks, dl, true, false)
		}
		if sendPing {
			it.pingStream()
		}
		if sendReceipt {
			it.sendModAck(receipts, dl, true, true)
		}
	}
}

// handleKeepAlives modifies the pending request to include deadline extensions
// for live messages. It also purges expired messages.
//
// Called with the lock held.
func (it *messageIterator) handleKeepAlives() {
	now := time.Now()
	for id, expiry := range it.keepAliveDeadlines {
		if expiry.Before(now) {
			// Message is now expired.
			// This delete will not result in skipping any map items, as implied by
			// the spec at https://golang.org/ref/spec#For_statements, "For
			// statements with range clause", note 3, and stated explicitly at
			// https://groups.google.com/forum/#!msg/golang-nuts/UciASUb03Js/pzSq5iVFAQAJ.
			delete(it.keepAliveDeadlines, id)
			if it.enableTracing {
				// get the parent span context for this ackID for otel tracing.
				// This message is now expired, so if the ackID is still valid,
				// mark that span as expired and end the span.
				s, ok := it.activeSpans.LoadAndDelete(id)
				if ok {
					span := s.(trace.Span)
					span.SetAttributes(attribute.String(resultAttribute, resultExpired))
					span.End()
				}
			}
		} else {
			// Use a success AckResult since we don't propagate ModAcks back to the user.
			it.pendingModAcks[id] = newSuccessAckResult()
		}
	}
	it.checkDrained()
}

type ackFunc = func(ctx context.Context, subName string, ackIds []string) error
type ackRecordStat = func(ctx context.Context, toSend []string)
type retryAckFunc = func(toRetry map[string]*ipubsub.AckResult)

func (it *messageIterator) sendAckWithFunc(m map[string]*AckResult, ackFunc ackFunc, retryAckFunc retryAckFunc, ackRecordStat ackRecordStat) {
	ackIDs := make([]string, 0, len(m))
	for ackID := range m {
		ackIDs = append(ackIDs, ackID)
	}
	it.eoMu.RLock()
	exactlyOnceDelivery := it.enableExactlyOnceDelivery
	it.eoMu.RUnlock()
	batches := makeBatches(ackIDs, ackIDBatchSize)
	wg := sync.WaitGroup{}

	for _, batch := range batches {
		wg.Add(1)
		go func(toSend []string) {
			defer wg.Done()
			ackRecordStat(it.ctx, toSend)
			// Use context.Background() as the call's context, not it.ctx. We don't
			// want to cancel this RPC when the iterator is stopped.
			cctx, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel2()
			err := ackFunc(cctx, it.subName, toSend)
			if exactlyOnceDelivery {
				resultsByAckID := make(map[string]*AckResult)
				for _, ackID := range toSend {
					resultsByAckID[ackID] = m[ackID]
				}
				st, md := extractMetadata(err)
				_, toRetry := processResults(st, resultsByAckID, md)
				if len(toRetry) > 0 {
					// Retry acks/modacks/nacks in a separate goroutine.
					go func() {
						retryAckFunc(toRetry)
					}()
				}
			}
		}(batch)
	}
	wg.Wait()
}

// sendAck is used to confirm acknowledgement of a message. If exactly once delivery is
// enabled, we'll retry these messages for a short duration in a goroutine.
func (it *messageIterator) sendAck(m map[string]*AckResult) {
	it.sendAckWithFunc(m, func(ctx context.Context, subName string, ackIDs []string) error {
		// For each ackID (message), setup links to the main subscribe span.
		// If this is a nack, also remove it from active spans.
		// If the ackID is not found, don't create any more spans.
		if it.enableTracing {
			var links []trace.Link
			subscribeSpans := make([]trace.Span, 0, len(ackIDs))
			for _, ackID := range ackIDs {
				// get the main subscribe span context for this ackID for otel tracing.
				s, ok := it.activeSpans.LoadAndDelete(ackID)
				if ok {
					subscribeSpan := s.(trace.Span)
					defer subscribeSpan.End()
					defer subscribeSpan.SetAttributes(attribute.String(resultAttribute, resultAcked))
					subscribeSpans = append(subscribeSpans, subscribeSpan)
					subscribeSpan.AddEvent(eventAckStart, trace.WithAttributes(semconv.MessagingBatchMessageCount(len(ackIDs))))
					defer subscribeSpan.AddEvent(eventAckEnd)
					// Only add this link if the span is sampled, otherwise we're creating invalid links.
					if subscribeSpan.SpanContext().IsSampled() {
						links = append(links, trace.Link{SpanContext: subscribeSpan.SpanContext()})
					}
				}
			}

			// Create the single ack span for this request, and for each
			// message, add Subscribe<->Ack links.
			opts := getCommonOptions(it.projectID, it.subID)
			opts = append(
				opts,
				trace.WithLinks(links...),
				trace.WithAttributes(
					semconv.MessagingBatchMessageCount(len(ackIDs)),
					semconv.CodeFunction("sendAck"),
				),
			)
			_, ackSpan := startSpan(context.Background(), ackSpanName, it.subID, opts...)
			defer ackSpan.End()
			if ackSpan.SpanContext().IsSampled() {
				for _, s := range subscribeSpans {
					s.AddLink(trace.Link{
						SpanContext: ackSpan.SpanContext(),
						Attributes: []attribute.KeyValue{
							semconv.MessagingOperationName(ackSpanName),
						},
					})
				}
			}
		}
		return it.subc.Acknowledge(ctx, &pb.AcknowledgeRequest{
			Subscription: it.subName,
			AckIds:       ackIDs,
		})
	}, it.retryAcks, func(ctx context.Context, toSend []string) {
		recordStat(it.ctx, AckCount, int64(len(toSend)))
		addAcks(toSend)

	})
}

// sendModAck is used to extend the lease of messages or nack them.
// The receipt mod-ack amount is derived from a percentile distribution based
// on the time it takes to process messages. The percentile chosen is the 99%th
// percentile in order to capture the highest amount of time necessary without
// considering 1% outliers. If the ModAck RPC fails and exactly once delivery is
// enabled, we retry it in a separate goroutine for a short duration.
func (it *messageIterator) sendModAck(m map[string]*AckResult, deadline time.Duration, logOnInvalid, isReceipt bool) {
	deadlineSec := int32(deadline / time.Second)
	isNack := deadline == 0
	var spanName, eventStart, eventEnd string
	if isNack {
		spanName = nackSpanName
		eventStart = eventNackStart
		eventEnd = eventNackEnd
	} else {
		spanName = modackSpanName
		eventStart = eventModackStart
		eventEnd = eventModackEnd
	}
	it.sendAckWithFunc(m, func(ctx context.Context, subName string, ackIDs []string) error {
		if it.enableTracing {
			// For each ackID (message), link back to the main subscribe span.
			// If this is a nack, also remove it from active spans.
			// If the ackID is not found, don't create any more spans.
			links := make([]trace.Link, 0, len(ackIDs))
			subscribeSpans := make([]trace.Span, 0, len(ackIDs))
			for _, ackID := range ackIDs {
				// get the parent span context for this ackID for otel tracing.
				var s any
				var ok bool
				if isNack {
					s, ok = it.activeSpans.LoadAndDelete(ackID)
				} else {
					s, ok = it.activeSpans.Load(ackID)
				}
				if ok {
					subscribeSpan := s.(trace.Span)
					subscribeSpans = append(subscribeSpans, subscribeSpan)
					if isNack {
						defer subscribeSpan.End()
						defer subscribeSpan.SetAttributes(attribute.String(resultAttribute, resultNacked))
					}
					subscribeSpan.AddEvent(eventStart, trace.WithAttributes(semconv.MessagingBatchMessageCount(len(ackIDs))))
					defer subscribeSpan.AddEvent(eventEnd)

					// Only add this link if the span is sampled, otherwise we're creating invalid links.
					if subscribeSpan.SpanContext().IsSampled() {
						links = append(links, trace.Link{SpanContext: subscribeSpan.SpanContext()})
					}
				}
			}

			// Create the single modack/nack span for this request, and for each
			// message, add Subscribe<->Modack links.
			opts := getCommonOptions(it.projectID, it.subID)
			opts = append(
				opts,
				trace.WithLinks(links...),
				trace.WithAttributes(
					semconv.MessagingBatchMessageCount(len(ackIDs)),
					semconv.CodeFunction("sendModAck"),
				),
			)
			if !isNack {
				opts = append(
					opts,
					trace.WithAttributes(
						semconv.MessagingGCPPubsubMessageAckDeadline(int(deadlineSec)),
						attribute.Bool(receiptModackAttribute, isReceipt),
					),
				)
			}
			_, mSpan := startSpan(context.Background(), spanName, it.subID, opts...)
			defer mSpan.End()
			if mSpan.SpanContext().IsSampled() {
				for _, s := range subscribeSpans {
					s.AddLink(trace.Link{
						SpanContext: mSpan.SpanContext(),
						Attributes: []attribute.KeyValue{
							semconv.MessagingOperationName(spanName),
						},
					})
				}
			}
		}
		return it.subc.ModifyAckDeadline(ctx, &pb.ModifyAckDeadlineRequest{
			Subscription:       it.subName,
			AckDeadlineSeconds: deadlineSec,
			AckIds:             ackIDs,
		})
	}, func(toRetry map[string]*ipubsub.AckResult) {
		it.retryModAcks(toRetry, deadlineSec, logOnInvalid)
	}, func(ctx context.Context, toSend []string) {
		if deadline == 0 {
			recordStat(it.ctx, NackCount, int64(len(toSend)))
		} else {
			recordStat(it.ctx, ModAckCount, int64(len(toSend)))
		}
		addModAcks(toSend, deadlineSec)
	})
}

// retryAcks retries the ack RPC with backoff. This must be called in a goroutine
// in it.sendAck(), with a max of 2500 ackIDs.
func (it *messageIterator) retryAcks(m map[string]*AckResult) {
	ctx, cancel := context.WithTimeout(context.Background(), exactlyOnceDeliveryRetryDeadline)
	defer cancel()
	bo := newExactlyOnceBackoff()
	for {
		if ctx.Err() != nil {
			for _, r := range m {
				ipubsub.SetAckResult(r, AcknowledgeStatusOther, ctx.Err())
			}
			return
		}
		// Don't need to split map since this is the retry function and
		// there is already a max of 2500 ackIDs here.
		ackIDs := make([]string, 0, len(m))
		for k := range m {
			ackIDs = append(ackIDs, k)
		}
		cctx2, cancel2 := context.WithTimeout(ctx, 60*time.Second)
		defer cancel2()
		err := it.subc.Acknowledge(cctx2, &pb.AcknowledgeRequest{
			Subscription: it.subName,
			AckIds:       ackIDs,
		})
		st, md := extractMetadata(err)
		_, toRetry := processResults(st, m, md)
		if len(toRetry) == 0 {
			return
		}
		time.Sleep(bo.Pause())
		m = toRetry
	}
}

// retryModAcks retries the modack RPC with backoff. This must be called in a goroutine
// in it.sendModAck(), with a max of 2500 ackIDs. Modacks are retried up to 3 times
// since after that, the message will have expired. Nacks are retried up until the default
// deadline of 10 minutes.
func (it *messageIterator) retryModAcks(m map[string]*AckResult, deadlineSec int32, logOnInvalid bool) {
	bo := newExactlyOnceBackoff()
	retryCount := 0
	ctx, cancel := context.WithTimeout(context.Background(), exactlyOnceDeliveryRetryDeadline)
	defer cancel()
	for {
		// If context is done, complete all AckResults with errors.
		if ctx.Err() != nil {
			for _, r := range m {
				ipubsub.SetAckResult(r, AcknowledgeStatusOther, ctx.Err())
			}
			return
		}
		// Only retry modack requests up to 3 times.
		if deadlineSec != 0 && retryCount > 3 {
			ackIDs := make([]string, 0, len(m))
			for k, ar := range m {
				ackIDs = append(ackIDs, k)
				ipubsub.SetAckResult(ar, AcknowledgeStatusOther, errors.New("modack retry failed"))
			}
			if logOnInvalid {
				log.Printf("automatic lease modack retry failed for following IDs: %v", ackIDs)
			}
			return
		}
		// Don't need to split map since this is the retry function and
		// there is already a max of 2500 ackIDs here.
		ackIDs := make([]string, 0, len(m))
		for k := range m {
			ackIDs = append(ackIDs, k)
		}
		cctx2, cancel2 := context.WithTimeout(ctx, 60*time.Second)
		defer cancel2()
		err := it.subc.ModifyAckDeadline(cctx2, &pb.ModifyAckDeadlineRequest{
			Subscription:       it.subName,
			AckIds:             ackIDs,
			AckDeadlineSeconds: deadlineSec,
		})
		st, md := extractMetadata(err)
		_, toRetry := processResults(st, m, md)
		if len(toRetry) == 0 {
			return
		}
		time.Sleep(bo.Pause())
		m = toRetry
		retryCount++
	}
}

// Send a message to the stream to keep it open. The stream will close if there's no
// traffic on it for a while. By keeping it open, we delay the start of the
// expiration timer on messages that are buffered by gRPC or elsewhere in the
// network. This matters if it takes a long time to process messages relative to the
// default ack deadline, and if the messages are small enough so that many can fit
// into the buffer.
func (it *messageIterator) pingStream() {
	spr := &pb.StreamingPullRequest{}
	it.eoMu.RLock()
	if it.sendNewAckDeadline {
		spr.StreamAckDeadlineSeconds = int32(it.ackDeadline().Seconds())
		it.sendNewAckDeadline = false
	}
	it.eoMu.RUnlock()
	it.ps.Send(spr)
}

// calcFieldSizeString returns the number of bytes string fields
// will take up in an encoded proto message.
func calcFieldSizeString(fields ...string) int {
	overhead := 0
	for _, field := range fields {
		overhead += 1 + len(field) + protowire.SizeVarint(uint64(len(field)))
	}
	return overhead
}

// calcFieldSizeInt returns the number of bytes int fields
// will take up in an encoded proto message.
func calcFieldSizeInt(fields ...int) int {
	overhead := 0
	for _, field := range fields {
		overhead += 1 + protowire.SizeVarint(uint64(field))
	}
	return overhead
}

// makeBatches takes a slice of ackIDs and returns a slice of ackID batches.
// Each ackID batch can be used in a request where the payload does not exceed maxBatchSize.
func makeBatches(ids []string, maxBatchSize int) [][]string {
	var batches [][]string
	for len(ids) > 0 {
		if len(ids) < maxBatchSize {
			batches = append(batches, ids)
			ids = []string{}
		} else {
			batches = append(batches, ids[:maxBatchSize])
			ids = ids[maxBatchSize:]
		}
	}
	return batches
}

// The deadline to ack is derived from a percentile distribution based
// on the time it takes to process messages. The percentile chosen is the 99%th
// percentile - that is, processing times up to the 99%th longest processing
// times should be safe. The highest 1% may expire. This number was chosen
// as a way to cover most users' usecases without losing the value of
// expiration.
func (it *messageIterator) ackDeadline() time.Duration {
	pt := time.Duration(it.ackTimeDist.Percentile(.99)) * time.Second
	it.eoMu.RLock()
	enableExactlyOnce := it.enableExactlyOnceDelivery
	it.eoMu.RUnlock()
	return boundedDuration(pt, it.po.minExtensionPeriod, it.po.maxExtensionPeriod, enableExactlyOnce)
}

func boundedDuration(ackDeadline, minExtension, maxExtension time.Duration, exactlyOnce bool) time.Duration {
	// If the user explicitly sets a maxExtensionPeriod, respect it.
	if maxExtension > 0 {
		ackDeadline = minDuration(ackDeadline, maxExtension)
	}

	// If the user explicitly sets a minExtensionPeriod, respect it.
	if minExtension > 0 {
		ackDeadline = maxDuration(ackDeadline, minExtension)
	} else if exactlyOnce {
		// Higher minimum ack_deadline for subscriptions with
		// exactly-once delivery enabled.
		ackDeadline = maxDuration(ackDeadline, minDurationPerLeaseExtensionExactlyOnce)
	} else if ackDeadline < minDurationPerLeaseExtension {
		// Otherwise, lower bound is min ack extension. This is normally bounded
		// when adding datapoints to the distribution, but this is needed for
		// the initial few calls to ackDeadline.
		ackDeadline = minDurationPerLeaseExtension
	}

	return ackDeadline
}

func minDuration(x, y time.Duration) time.Duration {
	if x < y {
		return x
	}
	return y
}

func maxDuration(x, y time.Duration) time.Duration {
	if x > y {
		return x
	}
	return y
}

const (
	transientErrStringPrefix     = "TRANSIENT_"
	permanentInvalidAckErrString = "PERMANENT_FAILURE_INVALID_ACK_ID"
)

// extracts information from an API error for exactly once delivery's ack/modack err responses.
func extractMetadata(err error) (*status.Status, map[string]string) {
	apiErr, ok := apierror.FromError(err)
	if ok {
		return apiErr.GRPCStatus(), apiErr.Metadata()
	}
	return nil, nil
}

// processResults processes AckResults by referring to errorStatus and errorsByAckID.
// The errors returned by the server in `errorStatus` or in `errorsByAckID`
// are used to complete the AckResults in `ackResMap` (with a success
// or error) or to return requests for further retries.
// This function returns two maps of ackID to ack results, one for completed results and the other for ones to retry.
// Logic is derived from python-pubsub: https://github.com/googleapis/python-pubsub/blob/main/google/cloud/pubsub_v1/subscriber/_protocol/streaming_pull_manager.py#L161-L220
func processResults(errorStatus *status.Status, ackResMap map[string]*AckResult, errorsByAckID map[string]string) (map[string]*AckResult, map[string]*AckResult) {
	completedResults := make(map[string]*AckResult)
	retryResults := make(map[string]*AckResult)
	for ackID, ar := range ackResMap {
		// Handle special errors returned for ack/modack RPCs via the ErrorInfo
		// sidecar metadata when exactly-once delivery is enabled.
		if errAckID, ok := errorsByAckID[ackID]; ok {
			if strings.HasPrefix(errAckID, transientErrStringPrefix) {
				retryResults[ackID] = ar
			} else {
				if errAckID == permanentInvalidAckErrString {
					ipubsub.SetAckResult(ar, AcknowledgeStatusInvalidAckID, errors.New(errAckID))
				} else {
					ipubsub.SetAckResult(ar, AcknowledgeStatusOther, errors.New(errAckID))
				}
				completedResults[ackID] = ar
			}
		} else if errorStatus != nil && contains(errorStatus.Code(), exactlyOnceDeliveryTemporaryRetryErrors) {
			retryResults[ackID] = ar
		} else if errorStatus != nil {
			// Other gRPC errors are not retried.
			switch errorStatus.Code() {
			case codes.PermissionDenied:
				ipubsub.SetAckResult(ar, AcknowledgeStatusPermissionDenied, errorStatus.Err())
			case codes.FailedPrecondition:
				ipubsub.SetAckResult(ar, AcknowledgeStatusFailedPrecondition, errorStatus.Err())
			default:
				ipubsub.SetAckResult(ar, AcknowledgeStatusOther, errorStatus.Err())
			}
			completedResults[ackID] = ar
		} else if ar != nil {
			// Since no error occurred, requests with AckResults are completed successfully.
			ipubsub.SetAckResult(ar, AcknowledgeStatusSuccess, nil)
			completedResults[ackID] = ar
		} else {
			// All other requests are considered completed.
			completedResults[ackID] = ar
		}
	}
	return completedResults, retryResults
}

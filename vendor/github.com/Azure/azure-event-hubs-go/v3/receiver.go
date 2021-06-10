package eventhub

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

import (
	"context"
	"fmt"
	"time"

	common "github.com/Azure/azure-amqp-common-go/v3"
	"github.com/devigned/tab"
	"github.com/Azure/go-amqp"

	"github.com/Azure/azure-event-hubs-go/v3/persist"
)

const (
	// DefaultConsumerGroup is the default name for a event stream consumer group
	DefaultConsumerGroup = "$Default"

	offsetAnnotationName       = "x-opt-offset"
	enqueuedTimeAnnotationName = "x-opt-enqueued-time"

	amqpAnnotationFormat = "amqp.annotation.%s >%s '%v'"

	defaultPrefetchCount = 1000

	epochKey = MsftVendor + ":epoch"
)

// receiver provides session and link handling for a receiving entity path
type (
	receiver struct {
		hub           *Hub
		connection    *amqp.Client
		session       *session
		receiver      *amqp.Receiver
		consumerGroup string
		partitionID   string
		prefetchCount uint32
		done          func()
		epoch         *int64
		lastError     error
		checkpoint    persist.Checkpoint
	}

	// ReceiveOption provides a structure for configuring receivers
	ReceiveOption func(receiver *receiver) error

	// ListenerHandle provides the ability to close or listen to the close of a Receiver
	ListenerHandle struct {
		r   *receiver
		ctx context.Context
	}
)

// ReceiveWithConsumerGroup configures the receiver to listen to a specific consumer group
func ReceiveWithConsumerGroup(consumerGroup string) ReceiveOption {
	return func(receiver *receiver) error {
		receiver.consumerGroup = consumerGroup
		return nil
	}
}

// ReceiveWithStartingOffset configures the receiver to start at a given position in the event stream
func ReceiveWithStartingOffset(offset string) ReceiveOption {
	return func(receiver *receiver) error {
		receiver.checkpoint = persist.NewCheckpoint(offset, 0, time.Time{})
		return nil
	}
}

// ReceiveWithLatestOffset configures the receiver to start at a given position in the event stream
func ReceiveWithLatestOffset() ReceiveOption {
	return func(receiver *receiver) error {
		receiver.checkpoint = persist.NewCheckpointFromEndOfStream()
		return nil
	}
}

// ReceiveFromTimestamp configures the receiver to start receiving from a specific point in time in the event stream
func ReceiveFromTimestamp(t time.Time) ReceiveOption {
	return func(receiver *receiver) error {
		receiver.checkpoint = persist.NewCheckpoint("", 0, t)
		return nil
	}
}

// ReceiveWithPrefetchCount configures the receiver to attempt to fetch as many messages as the prefetch amount
func ReceiveWithPrefetchCount(prefetch uint32) ReceiveOption {
	return func(receiver *receiver) error {
		receiver.prefetchCount = prefetch
		return nil
	}
}

// ReceiveWithEpoch configures the receiver to use an epoch. Specifying an epoch for a receiver will cause any receiver
// with a lower epoch value to be disconnected from the message broker. If a receiver attempts to start with a lower
// epoch than the broker currently knows for a given partition, the broker will respond with an error on initiation of
// the receive request.
//
// Ownership enforcement: Once you created an epoch based receiver, you cannot create a non-epoch receiver to the same
// consumer group / partition combo until all receivers to the combo are closed.
//
// Ownership stealing: If a receiver with higher epoch value is created for a consumer group / partition combo, any
// older epoch receiver to that combo will be force closed.
func ReceiveWithEpoch(epoch int64) ReceiveOption {
	return func(receiver *receiver) error {
		receiver.epoch = &epoch
		return nil
	}
}

// newReceiver creates a new Service Bus message listener given an AMQP client and an entity path
func (h *Hub) newReceiver(ctx context.Context, partitionID string, opts ...ReceiveOption) (*receiver, error) {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.newReceiver")
	defer span.End()

	receiver := &receiver{
		hub:           h,
		consumerGroup: DefaultConsumerGroup,
		prefetchCount: defaultPrefetchCount,
		partitionID:   partitionID,
	}

	for _, opt := range opts {
		if err := opt(receiver); err != nil {
			return nil, err
		}
	}

	// update checkpoint if old checkpoint is successfully read from e.g. file or memory
	oldCheckpoint, err := receiver.getLastReceivedCheckpoint()
	if err == nil {
		receiver.checkpoint = oldCheckpoint
	}

	if err = receiver.storeLastReceivedCheckpoint(receiver.checkpoint); err != nil {
		return nil, err
	}

	tab.For(ctx).Debug("creating a new receiver")
	return receiver, receiver.newSessionAndLink(ctx)
}

// Close will close the AMQP session and link of the receiver
func (r *receiver) Close(ctx context.Context) error {
	span, _ := r.startConsumerSpanFromContext(ctx, "eh.receiver.Close")
	defer span.End()

	if r.done != nil {
		r.done()
	}

	err := r.receiver.Close(ctx)
	if err != nil {
		tab.For(ctx).Error(err)
		if sessionErr := r.session.Close(ctx); sessionErr != nil {
			tab.For(ctx).Error(sessionErr)
		}

		if connErr := r.connection.Close(); connErr != nil {
			tab.For(ctx).Error(connErr)
		}

		return err
	}

	if sessionErr := r.session.Close(ctx); sessionErr != nil {
		tab.For(ctx).Error(sessionErr)

		if connErr := r.connection.Close(); connErr != nil {
			tab.For(ctx).Error(connErr)
		}

		return sessionErr
	}

	return r.connection.Close()
}

// Recover will attempt to close the current session and link, then rebuild them
func (r *receiver) Recover(ctx context.Context) error {
	span, ctx := r.startConsumerSpanFromContext(ctx, "eh.receiver.Recover")
	defer span.End()

	_ = r.connection.Close() // we expect the receiver is in an error state
	return r.newSessionAndLink(ctx)
}

// Listen start a listener for messages sent to the entity path
func (r *receiver) Listen(handler Handler) *ListenerHandle {
	ctx, done := context.WithCancel(context.Background())
	r.done = done

	span, ctx := r.startConsumerSpanFromContext(ctx, "eh.receiver.Listen")
	defer span.End()

	messages := make(chan *amqp.Message)
	go r.listenForMessages(ctx, messages)
	go r.handleMessages(ctx, messages, handler)

	return &ListenerHandle{
		r:   r,
		ctx: ctx,
	}
}

func (r *receiver) handleMessages(ctx context.Context, messages chan *amqp.Message, handler Handler) {
	span, ctx := r.startConsumerSpanFromContext(ctx, "eh.receiver.handleMessages")
	defer span.End()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-messages:
			r.handleMessage(ctx, msg, handler)
		}
	}
}

func (r *receiver) handleMessage(ctx context.Context, msg *amqp.Message, handler Handler) {
	const optName = "eh.Receiver.handleMessage"

	event, err := eventFromMsg(msg)
	if err != nil {
		tab.For(ctx).Error(err)
		r.lastError = err
		r.done()
	}

	ctx, span := tab.StartSpanWithRemoteParent(ctx, optName, event)
	defer span.End()

	id := messageID(msg)
	if str, ok := id.(string); ok {
		span.AddAttributes(tab.StringAttribute("eh.message_id", str))
	}

	err = handler(ctx, event)
	if err != nil {
		err = msg.Modify(true, false, nil)
		if err != nil {
			tab.For(ctx).Error(err)
		}
		tab.For(ctx).Error(fmt.Errorf("message modified(true, false, nil): id: %v", id))
		return
	}
	err = msg.Accept()
	if err != nil {
		tab.For(ctx).Error(err)
	}

	err = r.storeLastReceivedCheckpoint(event.GetCheckpoint())
	if err != nil {
		tab.For(ctx).Error(err)
	}
}

func (r *receiver) listenForMessages(ctx context.Context, msgChan chan *amqp.Message) {
	span, ctx := r.startConsumerSpanFromContext(ctx, "eh.receiver.listenForMessages")
	defer span.End()

	for {
		msg, err := r.listenForMessage(ctx)
		if err == nil {
			msgChan <- msg
			continue
		}

		select {
		case <-ctx.Done():
			tab.For(ctx).Debug("context done")
			return
		default:
			if amqpErr, ok := err.(*amqp.DetachError); ok && amqpErr.RemoteError != nil && amqpErr.RemoteError.Condition == "amqp:link:stolen" {
				tab.For(ctx).Debug("link has been stolen by a higher epoch")
				_ = r.Close(ctx)
				return
			}

			_, retryErr := common.Retry(10, 10*time.Second, func() (interface{}, error) {
				sp, ctx := r.startConsumerSpanFromContext(ctx, "eh.receiver.listenForMessages.tryRecover")
				defer sp.End()

				tab.For(ctx).Debug("recovering connection")
				err := r.Recover(ctx)
				if err == nil {
					tab.For(ctx).Debug("recovered connection")
					return nil, nil
				}

				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
					return nil, common.Retryable(err.Error())
				}
			})

			if retryErr != nil {
				tab.For(ctx).Debug("retried, but error was unrecoverable")
				r.lastError = retryErr
				_ = r.Close(ctx)
				return
			}
		}
	}
}

func (r *receiver) listenForMessage(ctx context.Context) (*amqp.Message, error) {
	span, ctx := r.startConsumerSpanFromContext(ctx, "eh.receiver.listenForMessage")
	defer span.End()

	msg, err := r.receiver.Receive(ctx)
	if err != nil {
		tab.For(ctx).Debug(err.Error())
		return nil, err
	}

	id := messageID(msg)
	if str, ok := id.(string); ok {
		span.AddAttributes(tab.StringAttribute("he.message_id", str))
	}
	return msg, nil
}

// newSessionAndLink will replace the session and link on the receiver
func (r *receiver) newSessionAndLink(ctx context.Context) error {
	span, ctx := r.startConsumerSpanFromContext(ctx, "eh.receiver.newSessionAndLink")
	defer span.End()

	connection, err := r.hub.namespace.newConnection()
	if err != nil {
		return err
	}
	r.connection = connection

	address := r.getAddress()
	err = r.hub.namespace.negotiateClaim(ctx, connection, address)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	amqpSession, err := connection.NewSession()
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	offsetExpression, err := r.getOffsetExpression()
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	r.session, err = newSession(amqpSession)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	opts := []amqp.LinkOption{
		amqp.LinkSourceAddress(address),
		amqp.LinkCredit(r.prefetchCount),
		amqp.LinkReceiverSettle(amqp.ModeFirst),
		amqp.LinkSelectorFilter(offsetExpression),
	}

	if r.epoch != nil {
		opts = append(opts, amqp.LinkPropertyInt64(epochKey, *r.epoch))
	}

	amqpReceiver, err := amqpSession.NewReceiver(opts...)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	r.receiver = amqpReceiver
	return nil
}

func (r *receiver) getLastReceivedCheckpoint() (persist.Checkpoint, error) {
	return r.offsetPersister().Read(r.namespaceName(), r.hubName(), r.consumerGroup, r.partitionID)
}

func (r *receiver) storeLastReceivedCheckpoint(checkpoint persist.Checkpoint) error {
	return r.offsetPersister().Write(r.namespaceName(), r.hubName(), r.consumerGroup, r.partitionID, checkpoint)
}

func (r *receiver) getOffsetExpression() (string, error) {
	checkpoint, err := r.getLastReceivedCheckpoint()
	if err != nil {
		// assume err read is due to not having an offset -- probably want to change this as it's ambiguous
		return fmt.Sprintf(amqpAnnotationFormat, offsetAnnotationName, "=", persist.StartOfStream), nil
	}

	if checkpoint.Offset == "" {
		return fmt.Sprintf(amqpAnnotationFormat, enqueuedTimeAnnotationName, "", checkpoint.EnqueueTime.UnixNano()/int64(time.Millisecond)), nil
	}

	return fmt.Sprintf(amqpAnnotationFormat, offsetAnnotationName, "", checkpoint.Offset), nil
}

func (r *receiver) getAddress() string {
	return fmt.Sprintf("%s/ConsumerGroups/%s/Partitions/%s", r.hubName(), r.consumerGroup, r.partitionID)
}

func (r *receiver) getIdentifier() string {
	if r.epoch != nil {
		return fmt.Sprintf("%s/ConsumerGroups/%s/Partitions/%s/epoch/%d", r.hubName(), r.consumerGroup, r.partitionID, *r.epoch)
	}
	return r.getAddress()
}

func (r *receiver) getFullIdentifier() string {
	return r.hub.namespace.getEntityAudience(r.getIdentifier())
}

func (r *receiver) namespaceName() string {
	return r.hub.namespace.name
}

func (r *receiver) hubName() string {
	return r.hub.name
}

func (r *receiver) offsetPersister() persist.CheckpointPersister {
	return r.hub.offsetPersister
}

func messageID(msg *amqp.Message) interface{} {
	var id interface{} = "null"
	if msg.Properties != nil {
		id = msg.Properties.MessageID
	}
	return id
}

// Close will close the listener
func (lc *ListenerHandle) Close(ctx context.Context) error {
	return lc.r.Close(ctx)
}

// Done will close the channel when the listener has stopped
func (lc *ListenerHandle) Done() <-chan struct{} {
	return lc.ctx.Done()
}

// Err will return the last error encountered
func (lc *ListenerHandle) Err() error {
	if lc.r.lastError != nil {
		return lc.r.lastError
	}
	return lc.ctx.Err()
}

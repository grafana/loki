// Copyright 2022-2023 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jetstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
)

type (
	// MessagesContext supports iterating over a messages on a stream.
	MessagesContext interface {
		// Next retreives nest message on a stream. It will block until the next message is available.
		Next() (Msg, error)
		// Stop closes the iterator and cancels subscription.
		Stop()
	}

	ConsumeContext interface {
		Stop()
	}

	// MessageHandler is a handler function used as callback in [Consume]
	MessageHandler func(msg Msg)

	// PullConsumeOpt represent additional options used in [Consume] for pull consumers
	PullConsumeOpt interface {
		configureConsume(*consumeOpts) error
	}

	// PullMessagesOpt represent additional options used in [Messages] for pull consumers
	PullMessagesOpt interface {
		configureMessages(*consumeOpts) error
	}

	pullConsumer struct {
		sync.Mutex
		jetStream     *jetStream
		stream        string
		durable       bool
		name          string
		info          *ConsumerInfo
		subscriptions map[string]*pullSubscription
	}

	pullRequest struct {
		Expires   time.Duration `json:"expires,omitempty"`
		Batch     int           `json:"batch,omitempty"`
		MaxBytes  int           `json:"max_bytes,omitempty"`
		NoWait    bool          `json:"no_wait,omitempty"`
		Heartbeat time.Duration `json:"idle_heartbeat,omitempty"`
	}

	consumeOpts struct {
		Expires                 time.Duration
		MaxMessages             int
		MaxBytes                int
		Heartbeat               time.Duration
		ErrHandler              ConsumeErrHandlerFunc
		ReportMissingHeartbeats bool
		ThresholdMessages       int
		ThresholdBytes          int
	}

	ConsumeErrHandlerFunc func(consumeCtx ConsumeContext, err error)

	pullSubscription struct {
		sync.Mutex
		id                string
		consumer          *pullConsumer
		subscription      *nats.Subscription
		msgs              chan *nats.Msg
		errs              chan error
		pending           pendingMsgs
		hbMonitor         *hbMonitor
		fetchInProgress   uint32
		closed            uint32
		done              chan struct{}
		connStatusChanged chan nats.Status
		fetchNext         chan *pullRequest
		consumeOpts       *consumeOpts
	}

	pendingMsgs struct {
		msgCount  int
		byteCount int
	}

	MessageBatch interface {
		Messages() <-chan Msg
		Error() error
	}

	fetchResult struct {
		msgs chan Msg
		err  error
		done bool
		sseq uint64
	}

	FetchOpt func(*pullRequest) error

	hbMonitor struct {
		timer *time.Timer
		sync.Mutex
	}
)

const (
	DefaultMaxMessages = 500
	DefaultExpires     = 30 * time.Second
	DefaultHeartbeat   = 5 * time.Second
	unset              = -1
)

// Consume returns a ConsumeContext, allowing for processing incoming messages from a stream in a given callback function.
//
// Available options:
// [ConsumeMaxMessages] - sets maximum number of messages stored in a buffer, default is set to 100
// [ConsumeMaxBytes] - sets maximum number of bytes stored in a buffer
// [ConsumeExpiry] - sets a timeout for individual batch request, default is set to 30 seconds
// [ConsumeHeartbeat] - sets an idle heartbeat setting for a pull request, default is set to 5s
// [ConsumeErrHandler] - sets custom consume error callback handler
// [ConsumeThresholdMessages] - sets the byte count on which Consume will trigger new pull request to the server
// [ConsumeThresholdBytes] - sets the message count on which Consume will trigger new pull request to the server
func (p *pullConsumer) Consume(handler MessageHandler, opts ...PullConsumeOpt) (ConsumeContext, error) {
	if handler == nil {
		return nil, ErrHandlerRequired
	}
	consumeOpts, err := parseConsumeOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOption, err)
	}
	p.Lock()

	subject := apiSubj(p.jetStream.apiPrefix, fmt.Sprintf(apiRequestNextT, p.stream, p.name))

	// for single consume, use empty string as id
	// this is useful for ordered consumer, where only a single subscription is valid
	var consumeID string
	if len(p.subscriptions) > 0 {
		consumeID = nuid.Next()
	}
	sub := &pullSubscription{
		id:          consumeID,
		consumer:    p,
		errs:        make(chan error, 1),
		done:        make(chan struct{}, 1),
		fetchNext:   make(chan *pullRequest, 1),
		consumeOpts: consumeOpts,
	}
	sub.connStatusChanged = p.jetStream.conn.StatusChanged(nats.CONNECTED, nats.RECONNECTING)

	sub.hbMonitor = sub.scheduleHeartbeatCheck(consumeOpts.Heartbeat)

	p.subscriptions[sub.id] = sub
	p.Unlock()

	internalHandler := func(msg *nats.Msg) {
		if sub.hbMonitor != nil {
			sub.hbMonitor.Reset(2 * consumeOpts.Heartbeat)
		}
		userMsg, msgErr := checkMsg(msg)
		if !userMsg && msgErr == nil {
			return
		}
		defer func() {
			sub.Lock()
			sub.checkPending()
			sub.Unlock()
		}()
		if !userMsg {
			// heartbeat message
			if msgErr == nil {
				return
			}

			sub.Lock()
			err := sub.handleStatusMsg(msg, msgErr)
			sub.Unlock()

			if err != nil {
				if atomic.LoadUint32(&sub.closed) == 1 {
					return
				}
				if sub.consumeOpts.ErrHandler != nil {
					sub.consumeOpts.ErrHandler(sub, err)
				}
				sub.Stop()
			}
			return
		}
		handler(p.jetStream.toJSMsg(msg))
		sub.Lock()
		sub.decrementPendingMsgs(msg)
		sub.Unlock()
	}
	inbox := nats.NewInbox()
	sub.subscription, err = p.jetStream.conn.Subscribe(inbox, internalHandler)
	if err != nil {
		return nil, err
	}

	sub.Lock()
	// initial pull
	sub.resetPendingMsgs()
	if err := sub.pull(&pullRequest{
		Expires:   consumeOpts.Expires,
		Batch:     consumeOpts.MaxMessages,
		MaxBytes:  consumeOpts.MaxBytes,
		Heartbeat: consumeOpts.Heartbeat,
	}, subject); err != nil {
		sub.errs <- err
	}
	sub.Unlock()

	go func() {
		isConnected := true
		for {
			if atomic.LoadUint32(&sub.closed) == 1 {
				return
			}
			select {
			case status, ok := <-sub.connStatusChanged:
				if !ok {
					continue
				}
				if status == nats.RECONNECTING {
					if sub.hbMonitor != nil {
						sub.hbMonitor.Stop()
					}
					isConnected = false
				}
				if status == nats.CONNECTED {
					sub.Lock()
					if !isConnected {
						isConnected = true
						// try fetching consumer info several times to make sure consumer is available after reconnect
						for i := 0; i < 5; i++ {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							_, err := p.Info(ctx)
							cancel()
							if err == nil {
								break
							}
							if err != nil {
								if i == 4 {
									sub.cleanup()
									if sub.consumeOpts.ErrHandler != nil {
										sub.consumeOpts.ErrHandler(sub, err)
									}
									sub.Unlock()
									return
								}
							}
							time.Sleep(5 * time.Second)
						}

						sub.fetchNext <- &pullRequest{
							Expires:   sub.consumeOpts.Expires,
							Batch:     sub.consumeOpts.MaxMessages,
							MaxBytes:  sub.consumeOpts.MaxBytes,
							Heartbeat: sub.consumeOpts.Heartbeat,
						}
						sub.resetPendingMsgs()
					}
					sub.Unlock()
				}
			case err := <-sub.errs:
				sub.Lock()
				if sub.consumeOpts.ErrHandler != nil {
					sub.consumeOpts.ErrHandler(sub, err)
				}
				if errors.Is(err, ErrNoHeartbeat) {
					sub.fetchNext <- &pullRequest{
						Expires:   sub.consumeOpts.Expires,
						Batch:     sub.consumeOpts.MaxMessages,
						MaxBytes:  sub.consumeOpts.MaxBytes,
						Heartbeat: sub.consumeOpts.Heartbeat,
					}
					sub.resetPendingMsgs()
				}
				sub.Unlock()
			}
		}
	}()

	go sub.pullMessages(subject)

	return sub, nil
}

// resetPendingMsgs resets pending message count and byte count
// to the values set in consumeOpts
// lock should be held before calling this method
func (s *pullSubscription) resetPendingMsgs() {
	s.pending.msgCount = s.consumeOpts.MaxMessages
	s.pending.byteCount = s.consumeOpts.MaxBytes
}

// decrementPendingMsgs decrements pending message count and byte count
// lock should be held before calling this method
func (s *pullSubscription) decrementPendingMsgs(msg *nats.Msg) {
	s.pending.msgCount--
	if s.consumeOpts.MaxBytes != 0 {
		s.pending.byteCount -= msg.Size()
	}
}

// checkPending verifies whether there are enough messages in
// the buffer to trigger a new pull request.
// lock should be held before calling this method
func (s *pullSubscription) checkPending() {
	if s.pending.msgCount < s.consumeOpts.ThresholdMessages ||
		(s.pending.byteCount < s.consumeOpts.ThresholdBytes && s.consumeOpts.MaxBytes != 0) &&
			atomic.LoadUint32(&s.fetchInProgress) == 1 {

		s.fetchNext <- &pullRequest{
			Expires:   s.consumeOpts.Expires,
			Batch:     s.consumeOpts.MaxMessages - s.pending.msgCount,
			MaxBytes:  s.consumeOpts.MaxBytes - s.pending.byteCount,
			Heartbeat: s.consumeOpts.Heartbeat,
		}

		s.pending.msgCount = s.consumeOpts.MaxMessages
		s.pending.byteCount = s.consumeOpts.MaxBytes
	}
}

// Messages returns MessagesContext, allowing continuously iterating over messages on a stream.
//
// Available options:
// [ConsumeMaxMessages] - sets maximum number of messages stored in a buffer, default is set to 100
// [ConsumeMaxBytes] - sets maximum number of bytes stored in a buffer
// [ConsumeExpiry] - sets a timeout for individual batch request, default is set to 30 seconds
// [ConsumeHeartbeat] - sets an idle heartbeat setting for a pull request, default is set to 5s
// [ConsumeErrHandler] - sets custom consume error callback handler
// [ConsumeThresholdMessages] - sets the byte count on which Consume will trigger new pull request to the server
// [ConsumeThresholdBytes] - sets the message count on which Consume will trigger new pull request to the server
func (p *pullConsumer) Messages(opts ...PullMessagesOpt) (MessagesContext, error) {
	consumeOpts, err := parseMessagesOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOption, err)
	}

	p.Lock()
	subject := apiSubj(p.jetStream.apiPrefix, fmt.Sprintf(apiRequestNextT, p.stream, p.name))

	msgs := make(chan *nats.Msg, consumeOpts.MaxMessages)

	// for single consume, use empty string as id
	// this is useful for ordered consumer, where only a single subscription is valid
	var consumeID string
	if len(p.subscriptions) > 0 {
		consumeID = nuid.Next()
	}
	sub := &pullSubscription{
		id:          consumeID,
		consumer:    p,
		done:        make(chan struct{}, 1),
		msgs:        msgs,
		errs:        make(chan error, 1),
		fetchNext:   make(chan *pullRequest, 1),
		consumeOpts: consumeOpts,
	}
	sub.connStatusChanged = p.jetStream.conn.StatusChanged(nats.CONNECTED, nats.RECONNECTING)
	inbox := nats.NewInbox()
	sub.subscription, err = p.jetStream.conn.ChanSubscribe(inbox, sub.msgs)
	if err != nil {
		p.Unlock()
		return nil, err
	}

	go func() {
		<-sub.done
		sub.cleanup()
	}()
	p.subscriptions[sub.id] = sub
	p.Unlock()

	go sub.pullMessages(subject)

	go func() {
		for {
			status, ok := <-sub.connStatusChanged
			if !ok {
				return
			}
			if status == nats.CONNECTED {
				sub.errs <- errConnected
			}
			if status == nats.RECONNECTING {
				sub.errs <- errDisconnected
			}
		}
	}()

	return sub, nil
}

var (
	errConnected    = errors.New("connected")
	errDisconnected = errors.New("disconnected")
)

func (s *pullSubscription) Next() (Msg, error) {
	s.Lock()
	defer s.Unlock()
	if atomic.LoadUint32(&s.closed) == 1 {
		return nil, ErrMsgIteratorClosed
	}
	hbMonitor := s.scheduleHeartbeatCheck(s.consumeOpts.Heartbeat)
	defer func() {
		if hbMonitor != nil {
			hbMonitor.Stop()
		}
	}()

	isConnected := true
	for {
		s.checkPending()
		select {
		case msg := <-s.msgs:
			if hbMonitor != nil {
				hbMonitor.Reset(2 * s.consumeOpts.Heartbeat)
			}
			userMsg, msgErr := checkMsg(msg)
			if !userMsg {
				// heartbeat message
				if msgErr == nil {
					continue
				}
				if err := s.handleStatusMsg(msg, msgErr); err != nil {
					s.Stop()
					return nil, err
				}
				continue
			}
			s.decrementPendingMsgs(msg)
			return s.consumer.jetStream.toJSMsg(msg), nil
		case err := <-s.errs:
			if errors.Is(err, ErrNoHeartbeat) {
				s.pending.msgCount = 0
				s.pending.byteCount = 0
				if s.consumeOpts.ReportMissingHeartbeats {
					return nil, err
				}
			}
			if errors.Is(err, errConnected) {
				if !isConnected {
					isConnected = true
					// try fetching consumer info several times to make sure consumer is available after reconnect
					for i := 0; i < 5; i++ {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						_, err := s.consumer.Info(ctx)
						cancel()
						if err == nil {
							break
						}
						if err != nil {
							if i == 4 {
								s.Stop()
								return nil, err
							}
						}
						time.Sleep(5 * time.Second)
					}
					s.pending.msgCount = 0
					s.pending.byteCount = 0
					hbMonitor = s.scheduleHeartbeatCheck(s.consumeOpts.Heartbeat)
				}
			}
			if errors.Is(err, errDisconnected) {
				if hbMonitor != nil {
					hbMonitor.Stop()
				}
				isConnected = false
			}
		}
	}
}

func (s *pullSubscription) handleStatusMsg(msg *nats.Msg, msgErr error) error {
	if !errors.Is(msgErr, nats.ErrTimeout) && !errors.Is(msgErr, ErrMaxBytesExceeded) {
		if s.consumeOpts.ErrHandler != nil {
			s.consumeOpts.ErrHandler(s, msgErr)
		}
		if errors.Is(msgErr, ErrConsumerDeleted) || errors.Is(msgErr, ErrBadRequest) {
			return msgErr
		}
		if errors.Is(msgErr, ErrConsumerLeadershipChanged) {
			s.pending.msgCount = 0
			s.pending.byteCount = 0
		}
		return nil
	}
	msgsLeft, bytesLeft, err := parsePending(msg)
	if err != nil {
		if s.consumeOpts.ErrHandler != nil {
			s.consumeOpts.ErrHandler(s, err)
		}
	}
	s.pending.msgCount -= msgsLeft
	if s.pending.msgCount < 0 {
		s.pending.msgCount = 0
	}
	if s.consumeOpts.MaxBytes > 0 {
		s.pending.byteCount -= bytesLeft
		if s.pending.byteCount < 0 {
			s.pending.byteCount = 0
		}
	}
	return nil
}

func (hb *hbMonitor) Stop() {
	hb.Mutex.Lock()
	hb.timer.Stop()
	hb.Mutex.Unlock()
}

func (hb *hbMonitor) Reset(dur time.Duration) {
	hb.Mutex.Lock()
	hb.timer.Reset(dur)
	hb.Mutex.Unlock()
}

func (s *pullSubscription) Stop() {
	if atomic.LoadUint32(&s.closed) == 1 {
		return
	}
	close(s.done)
	atomic.StoreUint32(&s.closed, 1)
}

// Fetch sends a single request to retrieve given number of messages.
// It will wait up to provided expiry time if not all messages are available.
func (p *pullConsumer) Fetch(batch int, opts ...FetchOpt) (MessageBatch, error) {
	req := &pullRequest{
		Batch:   batch,
		Expires: DefaultExpires,
	}
	for _, opt := range opts {
		if err := opt(req); err != nil {
			return nil, err
		}
	}
	// for longer pulls, set heartbeat value
	if req.Expires >= 10*time.Second {
		req.Heartbeat = 5 * time.Second
	}

	return p.fetch(req)

}

// FetchBytes is used to retrieve up to a provided bytes from the stream.
// This method will always send a single request and wait until provided number of bytes is
// exceeded or request times out.
func (p *pullConsumer) FetchBytes(maxBytes int, opts ...FetchOpt) (MessageBatch, error) {
	req := &pullRequest{
		Batch:    1000000,
		MaxBytes: maxBytes,
		Expires:  DefaultExpires,
	}
	for _, opt := range opts {
		if err := opt(req); err != nil {
			return nil, err
		}
	}
	// for longer pulls, set heartbeat value
	if req.Expires >= 10*time.Second {
		req.Heartbeat = 5 * time.Second
	}

	return p.fetch(req)
}

// Fetch sends a single request to retrieve given number of messages.
// If there are any messages available at the time of sending request,
// FetchNoWait will return immediately.
func (p *pullConsumer) FetchNoWait(batch int) (MessageBatch, error) {
	req := &pullRequest{
		Batch:  batch,
		NoWait: true,
	}

	return p.fetch(req)
}

func (p *pullConsumer) fetch(req *pullRequest) (MessageBatch, error) {
	res := &fetchResult{
		msgs: make(chan Msg, req.Batch),
	}
	msgs := make(chan *nats.Msg, 2*req.Batch)
	subject := apiSubj(p.jetStream.apiPrefix, fmt.Sprintf(apiRequestNextT, p.stream, p.name))

	sub := &pullSubscription{
		consumer: p,
		done:     make(chan struct{}, 1),
		msgs:     msgs,
		errs:     make(chan error, 1),
	}
	inbox := nats.NewInbox()
	var err error
	sub.subscription, err = p.jetStream.conn.ChanSubscribe(inbox, sub.msgs)
	if err != nil {
		return nil, err
	}
	if err := sub.pull(req, subject); err != nil {
		return nil, err
	}

	var receivedMsgs, receivedBytes int
	hbTimer := sub.scheduleHeartbeatCheck(req.Heartbeat)
	go func(res *fetchResult) {
		defer sub.subscription.Unsubscribe()
		defer close(res.msgs)
		for {
			if receivedMsgs == req.Batch || (req.MaxBytes != 0 && receivedBytes == req.MaxBytes) {
				res.done = true
				return
			}
			select {
			case msg := <-msgs:
				if hbTimer != nil {
					hbTimer.Reset(2 * req.Heartbeat)
				}
				userMsg, err := checkMsg(msg)
				if err != nil {
					errNotTimeoutOrNoMsgs := !errors.Is(err, nats.ErrTimeout) && !errors.Is(err, ErrNoMessages)
					if errNotTimeoutOrNoMsgs && !errors.Is(err, ErrMaxBytesExceeded) {
						res.err = err
					}
					res.done = true
					return
				}
				if !userMsg {
					continue
				}
				res.msgs <- p.jetStream.toJSMsg(msg)
				meta, err := msg.Metadata()
				if err != nil {
					res.err = fmt.Errorf("parsing message metadata: %s", err)
				}
				res.sseq = meta.Sequence.Stream
				receivedMsgs++
				if req.MaxBytes != 0 {
					receivedBytes += msg.Size()
				}
			case <-time.After(req.Expires + 1*time.Second):
				res.done = true
				return
			}
		}
	}(res)
	return res, nil
}

func (fr *fetchResult) Messages() <-chan Msg {
	return fr.msgs
}

func (fr *fetchResult) Error() error {
	return fr.err
}

func (p *pullConsumer) Next(opts ...FetchOpt) (Msg, error) {
	res, err := p.Fetch(1, opts...)
	if err != nil {
		return nil, err
	}
	msg := <-res.Messages()
	if msg != nil {
		return msg, nil
	}
	if res.Error() == nil {
		return nil, nats.ErrTimeout
	}
	return nil, res.Error()
}

func (s *pullSubscription) pullMessages(subject string) {
	for {
		select {
		case req := <-s.fetchNext:
			atomic.StoreUint32(&s.fetchInProgress, 1)

			if err := s.pull(req, subject); err != nil {
				if errors.Is(err, ErrMsgIteratorClosed) {
					s.cleanup()
					return
				}
				s.errs <- err
			}
			atomic.StoreUint32(&s.fetchInProgress, 0)
		case <-s.done:
			s.cleanup()
			return
		}
	}
}

func (s *pullSubscription) scheduleHeartbeatCheck(dur time.Duration) *hbMonitor {
	if dur == 0 {
		return nil
	}
	return &hbMonitor{
		timer: time.AfterFunc(2*dur, func() {
			s.errs <- ErrNoHeartbeat
		}),
	}
}

func (s *pullSubscription) cleanup() {
	s.consumer.Lock()
	defer s.consumer.Unlock()
	if s.subscription == nil {
		return
	}
	if s.hbMonitor != nil {
		s.hbMonitor.Stop()
	}
	s.subscription.Unsubscribe()
	close(s.connStatusChanged)
	s.subscription = nil
	delete(s.consumer.subscriptions, s.id)
}

// pull sends a pull request to the server and waits for messages using a subscription from [pullSubscription].
// Messages will be fetched up to given batch_size or until there are no more messages or timeout is returned
func (s *pullSubscription) pull(req *pullRequest, subject string) error {
	s.consumer.Lock()
	defer s.consumer.Unlock()
	if atomic.LoadUint32(&s.closed) == 1 {
		return ErrMsgIteratorClosed
	}
	if req.Batch < 1 {
		return fmt.Errorf("%w: batch size must be at least 1", nats.ErrInvalidArg)
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return err
	}

	reply := s.subscription.Subject
	if err := s.consumer.jetStream.conn.PublishRequest(subject, reply, reqJSON); err != nil {
		return err
	}
	return nil
}

func parseConsumeOpts(opts ...PullConsumeOpt) (*consumeOpts, error) {
	consumeOpts := &consumeOpts{
		MaxMessages:             unset,
		MaxBytes:                unset,
		Expires:                 DefaultExpires,
		Heartbeat:               unset,
		ReportMissingHeartbeats: true,
	}
	for _, opt := range opts {
		if err := opt.configureConsume(consumeOpts); err != nil {
			return nil, err
		}
	}
	if err := consumeOpts.setDefaults(); err != nil {
		return nil, err
	}
	return consumeOpts, nil
}

func parseMessagesOpts(opts ...PullMessagesOpt) (*consumeOpts, error) {
	consumeOpts := &consumeOpts{
		MaxMessages:             unset,
		MaxBytes:                unset,
		Expires:                 DefaultExpires,
		Heartbeat:               unset,
		ReportMissingHeartbeats: true,
	}
	for _, opt := range opts {
		if err := opt.configureMessages(consumeOpts); err != nil {
			return nil, err
		}
	}
	if err := consumeOpts.setDefaults(); err != nil {
		return nil, err
	}
	return consumeOpts, nil
}

func (consumeOpts *consumeOpts) setDefaults() error {
	if consumeOpts.MaxBytes != unset && consumeOpts.MaxMessages != unset {
		return fmt.Errorf("only one of MaxMessages and MaxBytes can be specified")
	}
	if consumeOpts.MaxBytes != unset {
		// when max_bytes is used, set batch size to a very large number
		consumeOpts.MaxMessages = 1000000
	} else if consumeOpts.MaxMessages != unset {
		consumeOpts.MaxBytes = 0
	} else {
		if consumeOpts.MaxBytes == unset {
			consumeOpts.MaxBytes = 0
		}
		if consumeOpts.MaxMessages == unset {
			consumeOpts.MaxMessages = DefaultMaxMessages
		}
	}

	if consumeOpts.ThresholdMessages == 0 {
		consumeOpts.ThresholdMessages = int(math.Ceil(float64(consumeOpts.MaxMessages) / 2))
	}
	if consumeOpts.ThresholdBytes == 0 {
		consumeOpts.ThresholdBytes = int(math.Ceil(float64(consumeOpts.MaxBytes) / 2))
	}
	if consumeOpts.Heartbeat == unset {
		consumeOpts.Heartbeat = consumeOpts.Expires / 2
		if consumeOpts.Heartbeat > 30*time.Second {
			consumeOpts.Heartbeat = 30 * time.Second
		}
	}
	if consumeOpts.Heartbeat > consumeOpts.Expires/2 {
		return fmt.Errorf("the value of Heartbeat must be less than 50%% of expiry")
	}
	return nil
}

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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
)

type (
	asyncPublisherOpts struct {
		// For async publish error handling.
		aecb MsgErrHandler
		// Max async pub ack in flight
		maxpa int
	}

	PublishOpt func(*pubOpts) error

	pubOpts struct {
		id             string
		lastMsgID      string  // Expected last msgId
		stream         string  // Expected stream name
		lastSeq        *uint64 // Expected last sequence
		lastSubjectSeq *uint64 // Expected last sequence per subject

		// Publish retries for NoResponders err.
		retryWait     time.Duration // Retry wait between attempts
		retryAttempts int           // Retry attempts

		// stallWait is the max wait of a async pub ack.
		stallWait time.Duration
	}

	// PubAckFuture is a future for a PubAck.
	PubAckFuture interface {
		// Ok returns a receive only channel that can be used to get a PubAck.
		Ok() <-chan *PubAck

		// Err returns a receive only channel that can be used to get the error from an async publish.
		Err() <-chan error

		// Msg returns the message that was sent to the server.
		Msg() *nats.Msg
	}

	pubAckFuture struct {
		jsClient *jetStreamClient
		msg      *nats.Msg
		ack      *PubAck
		err      error
		errCh    chan error
		doneCh   chan *PubAck
	}

	jetStreamClient struct {
		asyncPublishContext
		asyncPublisherOpts
	}

	// MsgErrHandler is used to process asynchronous errors from
	// JetStream PublishAsync. It will return the original
	// message sent to the server for possible retransmitting and the error encountered.
	MsgErrHandler func(JetStream, *nats.Msg, error)

	asyncPublishContext struct {
		sync.RWMutex
		replyPrefix  string
		replySubject *nats.Subscription
		acks         map[string]*pubAckFuture
		stallCh      chan struct{}
		doneCh       chan struct{}
		rr           *rand.Rand
	}

	pubAckResponse struct {
		apiResponse
		*PubAck
	}

	// PubAck is an ack received after successfully publishing a message.
	PubAck struct {
		Stream    string `json:"stream"`
		Sequence  uint64 `json:"seq"`
		Duplicate bool   `json:"duplicate,omitempty"`
		Domain    string `json:"domain,omitempty"`
	}
)

const (
	// Default time wait between retries on Publish iff err is NoResponders.
	DefaultPubRetryWait = 250 * time.Millisecond

	// Default number of retries
	DefaultPubRetryAttempts = 2
)

const (
	statusHdr = "Status"

	inboxPrefix = "_INBOX."
	rdigits     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	base        = 62
)

func (js *jetStream) Publish(ctx context.Context, subj string, data []byte, opts ...PublishOpt) (*PubAck, error) {
	return js.PublishMsg(ctx, &nats.Msg{Subject: subj, Data: data}, opts...)
}

// PublishMsg publishes a Msg to a stream from JetStream.
func (js *jetStream) PublishMsg(ctx context.Context, m *nats.Msg, opts ...PublishOpt) (*PubAck, error) {
	o := pubOpts{
		retryWait:     DefaultPubRetryWait,
		retryAttempts: DefaultPubRetryAttempts,
	}
	if len(opts) > 0 {
		if m.Header == nil {
			m.Header = nats.Header{}
		}
		for _, opt := range opts {
			if err := opt(&o); err != nil {
				return nil, err
			}
		}
	}
	if o.stallWait > 0 {
		return nil, fmt.Errorf("%w: stall wait cannot be set to sync publish", ErrInvalidOption)
	}

	if o.id != "" {
		m.Header.Set(MsgIDHeader, o.id)
	}
	if o.lastMsgID != "" {
		m.Header.Set(ExpectedLastMsgIDHeader, o.lastMsgID)
	}
	if o.stream != "" {
		m.Header.Set(ExpectedStreamHeader, o.stream)
	}
	if o.lastSeq != nil {
		m.Header.Set(ExpectedLastSeqHeader, strconv.FormatUint(*o.lastSeq, 10))
	}
	if o.lastSubjectSeq != nil {
		m.Header.Set(ExpectedLastSubjSeqHeader, strconv.FormatUint(*o.lastSubjectSeq, 10))
	}

	var resp *nats.Msg
	var err error

	resp, err = js.conn.RequestMsgWithContext(ctx, m)

	if err != nil {
		for r := 0; errors.Is(err, nats.ErrNoResponders) && (r < o.retryAttempts || o.retryAttempts < 0); r++ {
			// To protect against small blips in leadership changes etc, if we get a no responders here retry.
			select {
			case <-ctx.Done():
			case <-time.After(o.retryWait):
			}
			resp, err = js.conn.RequestMsgWithContext(ctx, m)
		}
		if err != nil {
			if errors.Is(err, nats.ErrNoResponders) {
				return nil, ErrNoStreamResponse
			}
			return nil, err
		}
	}

	var ackResp pubAckResponse
	if err := json.Unmarshal(resp.Data, &ackResp); err != nil {
		return nil, ErrInvalidJSAck
	}
	if ackResp.Error != nil {
		return nil, fmt.Errorf("nats: %w", ackResp.Error)
	}
	if ackResp.PubAck == nil || ackResp.PubAck.Stream == "" {
		return nil, ErrInvalidJSAck
	}
	return ackResp.PubAck, nil
}

func (js *jetStream) PublishAsync(subj string, data []byte, opts ...PublishOpt) (PubAckFuture, error) {
	return js.PublishMsgAsync(&nats.Msg{Subject: subj, Data: data}, opts...)
}

func (js *jetStream) PublishMsgAsync(m *nats.Msg, opts ...PublishOpt) (PubAckFuture, error) {
	var o pubOpts
	if len(opts) > 0 {
		if m.Header == nil {
			m.Header = nats.Header{}
		}
		for _, opt := range opts {
			if err := opt(&o); err != nil {
				return nil, err
			}
		}
	}
	defaultStallWait := 200 * time.Millisecond

	stallWait := defaultStallWait
	if o.stallWait > 0 {
		stallWait = o.stallWait
	}

	if o.id != "" {
		m.Header.Set(MsgIDHeader, o.id)
	}
	if o.lastMsgID != "" {
		m.Header.Set(ExpectedLastMsgIDHeader, o.lastMsgID)
	}
	if o.stream != "" {
		m.Header.Set(ExpectedStreamHeader, o.stream)
	}
	if o.lastSeq != nil {
		m.Header.Set(ExpectedLastSeqHeader, strconv.FormatUint(*o.lastSeq, 10))
	}
	if o.lastSubjectSeq != nil {
		m.Header.Set(ExpectedLastSubjSeqHeader, strconv.FormatUint(*o.lastSubjectSeq, 10))
	}

	// Reply
	if m.Reply != "" {
		return nil, ErrAsyncPublishReplySubjectSet
	}
	var err error
	reply := m.Reply
	m.Reply, err = js.newAsyncReply()
	if err != nil {
		return nil, fmt.Errorf("nats: error creating async reply handler: %s", err)
	}
	defer func() { m.Reply = reply }()

	id := m.Reply[aReplyPreLen:]
	paf := &pubAckFuture{msg: m, jsClient: js.publisher}
	numPending, maxPending := js.registerPAF(id, paf)

	if maxPending > 0 && numPending > maxPending {
		select {
		case <-js.asyncStall():
		case <-time.After(stallWait):
			js.clearPAF(id)
			return nil, ErrTooManyStalledMsgs
		}
	}
	if err := js.conn.PublishMsg(m); err != nil {
		js.clearPAF(id)
		return nil, err
	}

	return paf, nil
}

// For quick token lookup etjs.
const (
	aReplyPreLen    = 14
	aReplyTokensize = 6
)

func (js *jetStream) newAsyncReply() (string, error) {
	js.publisher.Lock()
	if js.publisher.replySubject == nil {
		// Create our wildcard reply subject.
		sha := sha256.New()
		sha.Write([]byte(nuid.Next()))
		b := sha.Sum(nil)
		for i := 0; i < aReplyTokensize; i++ {
			b[i] = rdigits[int(b[i]%base)]
		}
		js.publisher.replyPrefix = fmt.Sprintf("%s%s.", inboxPrefix, b[:aReplyTokensize])
		sub, err := js.conn.Subscribe(fmt.Sprintf("%s*", js.publisher.replyPrefix), js.handleAsyncReply)
		if err != nil {
			js.publisher.Unlock()
			return "", err
		}
		js.publisher.replySubject = sub
		js.publisher.rr = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	var sb strings.Builder
	sb.WriteString(js.publisher.replyPrefix)
	rn := js.publisher.rr.Int63()
	var b [aReplyTokensize]byte
	for i, l := 0, rn; i < len(b); i++ {
		b[i] = rdigits[l%base]
		l /= base
	}
	sb.Write(b[:])
	js.publisher.Unlock()
	return sb.String(), nil
}

// Handle an async reply from PublishAsync.
func (js *jetStream) handleAsyncReply(m *nats.Msg) {
	if len(m.Subject) <= aReplyPreLen {
		return
	}
	id := m.Subject[aReplyPreLen:]

	js.publisher.Lock()
	paf := js.getPAF(id)
	if paf == nil {
		js.publisher.Unlock()
		return
	}
	// Remove
	delete(js.publisher.acks, id)

	// Check on anyone stalled and waiting.
	if js.publisher.stallCh != nil && len(js.publisher.acks) < js.publisher.asyncPublisherOpts.maxpa {
		close(js.publisher.stallCh)
		js.publisher.stallCh = nil
	}
	// Check on anyone waiting on done status.
	if js.publisher.doneCh != nil && len(js.publisher.acks) == 0 {
		dch := js.publisher.doneCh
		js.publisher.doneCh = nil
		// Defer here so error is processed and can be checked.
		defer close(dch)
	}

	doErr := func(err error) {
		paf.err = err
		if paf.errCh != nil {
			paf.errCh <- paf.err
		}
		cb := js.publisher.asyncPublisherOpts.aecb
		js.publisher.Unlock()
		if cb != nil {
			cb(js, paf.msg, err)
		}
	}

	// Process no responders etjs.
	if len(m.Data) == 0 && m.Header.Get(statusHdr) == noResponders {
		doErr(nats.ErrNoResponders)
		return
	}

	var pa pubAckResponse
	if err := json.Unmarshal(m.Data, &pa); err != nil {
		doErr(ErrInvalidJSAck)
		return
	}
	if pa.Error != nil {
		doErr(pa.Error)
		return
	}
	if pa.PubAck == nil || pa.PubAck.Stream == "" {
		doErr(ErrInvalidJSAck)
		return
	}

	// So here we have received a proper puback.
	paf.ack = pa.PubAck
	if paf.doneCh != nil {
		paf.doneCh <- paf.ack
	}
	js.publisher.Unlock()
}

// registerPAF will register for a PubAckFuture.
func (js *jetStream) registerPAF(id string, paf *pubAckFuture) (int, int) {
	js.publisher.Lock()
	if js.publisher.acks == nil {
		js.publisher.acks = make(map[string]*pubAckFuture)
	}
	js.publisher.acks[id] = paf
	np := len(js.publisher.acks)
	maxpa := js.publisher.asyncPublisherOpts.maxpa
	js.publisher.Unlock()
	return np, maxpa
}

// Lock should be held.
func (js *jetStream) getPAF(id string) *pubAckFuture {
	if js.publisher.acks == nil {
		return nil
	}
	return js.publisher.acks[id]
}

// clearPAF will remove a PubAckFuture that was registered.
func (js *jetStream) clearPAF(id string) {
	js.publisher.Lock()
	delete(js.publisher.acks, id)
	js.publisher.Unlock()
}

func (js *jetStream) asyncStall() <-chan struct{} {
	js.publisher.Lock()
	if js.publisher.stallCh == nil {
		js.publisher.stallCh = make(chan struct{})
	}
	stc := js.publisher.stallCh
	js.publisher.Unlock()
	return stc
}

func (paf *pubAckFuture) Ok() <-chan *PubAck {
	paf.jsClient.Lock()
	defer paf.jsClient.Unlock()

	if paf.doneCh == nil {
		paf.doneCh = make(chan *PubAck, 1)
		if paf.ack != nil {
			paf.doneCh <- paf.ack
		}
	}

	return paf.doneCh
}

func (paf *pubAckFuture) Err() <-chan error {
	paf.jsClient.Lock()
	defer paf.jsClient.Unlock()

	if paf.errCh == nil {
		paf.errCh = make(chan error, 1)
		if paf.err != nil {
			paf.errCh <- paf.err
		}
	}

	return paf.errCh
}

func (paf *pubAckFuture) Msg() *nats.Msg {
	paf.jsClient.RLock()
	defer paf.jsClient.RUnlock()
	return paf.msg
}

// PublishAsyncPending returns how many PubAckFutures are pending.
func (js *jetStream) PublishAsyncPending() int {
	js.publisher.RLock()
	defer js.publisher.RUnlock()
	return len(js.publisher.acks)
}

// PublishAsyncComplete returns a channel that will be closed when all outstanding messages have been ack'd.
func (js *jetStream) PublishAsyncComplete() <-chan struct{} {
	js.publisher.Lock()
	defer js.publisher.Unlock()
	if js.publisher.doneCh == nil {
		js.publisher.doneCh = make(chan struct{})
	}
	dch := js.publisher.doneCh
	if len(js.publisher.acks) == 0 {
		close(js.publisher.doneCh)
		js.publisher.doneCh = nil
	}
	return dch
}

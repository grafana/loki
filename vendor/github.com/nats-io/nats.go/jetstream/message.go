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
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/internal/parser"
)

type (
	// Msg contains methods to operate on a JetStream message
	// Metadata, Data, Headers, Subject and Reply can be used to retrieve the specific parts of the underlying message
	// Ack, DoubleAck, Nak, InProgress and Term are various flavors of ack requests
	Msg interface {
		// Metadata returns [MsgMetadata] for a JetStream message
		Metadata() (*MsgMetadata, error)
		// Data returns the message body
		Data() []byte
		// Headers returns a map of headers for a message
		Headers() nats.Header
		// Subject returns a subject on which a message is published
		Subject() string
		// Reply returns a reply subject for a message
		Reply() string

		// Ack acknowledges a message
		// This tells the server that the message was successfully processed and it can move on to the next message
		Ack() error
		// DoubleAck acknowledges a message and waits for ack from server
		DoubleAck(context.Context) error
		// Nak negatively acknowledges a message
		// This tells the server to redeliver the message
		Nak(...NakOpt) error
		// InProgress tells the server that this message is being worked on
		// It resets the redelivery timer on the server
		InProgress() error
		// Term tells the server to not redeliver this message, regardless of the value of nats.MaxDeliver
		Term() error
	}

	// MsgMetadata is the JetStream metadata associated with received messages.
	MsgMetadata struct {
		Sequence     SequencePair
		NumDelivered uint64
		NumPending   uint64
		Timestamp    time.Time
		Stream       string
		Consumer     string
		Domain       string
	}

	// SequencePair includes the consumer and stream sequence info from a JetStream consumer.
	SequencePair struct {
		Consumer uint64 `json:"consumer_seq"`
		Stream   uint64 `json:"stream_seq"`
	}

	jetStreamMsg struct {
		msg  *nats.Msg
		ackd bool
		js   *jetStream
		sync.Mutex
	}

	ackOpts struct {
		nakDelay time.Duration
	}

	AckOpt func(*ackOpts) error

	NakOpt func(*ackOpts) error

	ackType []byte
)

const (
	controlMsg       = "100"
	badRequest       = "400"
	noMessages       = "404"
	reqTimeout       = "408"
	maxBytesExceeded = "409"
	noResponders     = "503"
)

const (
	MsgIDHeader               = "Nats-Msg-Id"
	ExpectedStreamHeader      = "Nats-Expected-Stream"
	ExpectedLastSeqHeader     = "Nats-Expected-Last-Sequence"
	ExpectedLastSubjSeqHeader = "Nats-Expected-Last-Subject-Sequence"
	ExpectedLastMsgIDHeader   = "Nats-Expected-Last-Msg-Id"
	MsgRollup                 = "Nats-Rollup"
)

// Headers for republished messages and direct gets.
const (
	StreamHeader       = "Nats-Stream"
	SequenceHeader     = "Nats-Sequence"
	TimeStampHeaer     = "Nats-Time-Stamp"
	SubjectHeader      = "Nats-Subject"
	LastSequenceHeader = "Nats-Last-Sequence"
)

// Rollups, can be subject only or all messages.
const (
	MsgRollupSubject = "sub"
	MsgRollupAll     = "all"
)

var (
	ackAck      ackType = []byte("+ACK")
	ackNak      ackType = []byte("-NAK")
	ackProgress ackType = []byte("+WPI")
	ackTerm     ackType = []byte("+TERM")
)

// Metadata returns [MsgMetadata] for a JetStream message
func (m *jetStreamMsg) Metadata() (*MsgMetadata, error) {
	if err := m.checkReply(); err != nil {
		return nil, err
	}

	tokens, err := parser.GetMetadataFields(m.msg.Reply)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotJSMessage, err)
	}

	meta := &MsgMetadata{
		Domain:       tokens[parser.AckDomainTokenPos],
		NumDelivered: parser.ParseNum(tokens[parser.AckNumDeliveredTokenPos]),
		NumPending:   parser.ParseNum(tokens[parser.AckNumPendingTokenPos]),
		Timestamp:    time.Unix(0, int64(parser.ParseNum(tokens[parser.AckTimestampSeqTokenPos]))),
		Stream:       tokens[parser.AckStreamTokenPos],
		Consumer:     tokens[parser.AckConsumerTokenPos],
	}
	meta.Sequence.Stream = parser.ParseNum(tokens[parser.AckStreamSeqTokenPos])
	meta.Sequence.Consumer = parser.ParseNum(tokens[parser.AckConsumerSeqTokenPos])
	return meta, nil
}

// Data returns the message body
func (m *jetStreamMsg) Data() []byte {
	return m.msg.Data
}

// Headers returns a map of headers for a message
func (m *jetStreamMsg) Headers() nats.Header {
	return m.msg.Header
}

// Subject reutrns a subject on which a message is published
func (m *jetStreamMsg) Subject() string {
	return m.msg.Subject
}

// Reply reutrns a reply subject for a JetStream message
func (m *jetStreamMsg) Reply() string {
	return m.msg.Reply
}

// Ack acknowledges a message
// This tells the server that the message was successfully processed and it can move on to the next message
func (m *jetStreamMsg) Ack() error {
	return m.ackReply(context.Background(), ackAck, false, ackOpts{})
}

// DoubleAck acknowledges a message and waits for ack from server
func (m *jetStreamMsg) DoubleAck(ctx context.Context) error {
	return m.ackReply(ctx, ackAck, true, ackOpts{})
}

// Nak negatively acknowledges a message
// This tells the server to redeliver the message
// Nak() can be supplied with following options:
// - WithNakDelay() - specify the duration after which the mesage should be redelivered
func (m *jetStreamMsg) Nak(opts ...NakOpt) error {
	var o ackOpts
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			return err
		}
	}
	return m.ackReply(context.Background(), ackNak, false, o)
}

// InProgress tells the server that this message is being worked on
// It resets the redelivery timer on the server
func (m *jetStreamMsg) InProgress() error {
	return m.ackReply(context.Background(), ackProgress, false, ackOpts{})
}

// Term tells the server to not redeliver this message, regardless of the value of nats.MaxDeliver
func (m *jetStreamMsg) Term() error {
	return m.ackReply(context.Background(), ackTerm, false, ackOpts{})
}

func (m *jetStreamMsg) ackReply(ctx context.Context, ackType ackType, sync bool, opts ackOpts) error {
	err := m.checkReply()
	if err != nil {
		return err
	}

	m.Lock()
	if m.ackd {
		return ErrMsgAlreadyAckd
	}
	m.Unlock()

	if sync {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			return nats.ErrNoDeadlineContext
		}
	}

	var body []byte
	if opts.nakDelay > 0 {
		body = []byte(fmt.Sprintf("%s {\"delay\": %d}", ackType, opts.nakDelay.Nanoseconds()))
	} else {
		body = ackType
	}

	if sync {
		_, err = m.js.conn.RequestWithContext(ctx, m.msg.Reply, body)
	} else {
		err = m.js.conn.Publish(m.msg.Reply, body)
	}
	if err != nil {
		return err
	}

	// Mark that the message has been acked unless it is ackProgress
	// which can be sent many times.
	if !bytes.Equal(ackType, ackProgress) {
		m.Lock()
		m.ackd = true
		m.Unlock()
	}
	return nil
}

func (m *jetStreamMsg) checkReply() error {
	if m == nil || m.msg.Sub == nil {
		return ErrMsgNotBound
	}
	if m.msg.Reply == "" {
		return ErrMsgNoReply
	}
	return nil
}

// Returns if the given message is a user message or not, and if
// checkSts() is true, returns appropriate error based on the
// content of the status (404, etc..)
func checkMsg(msg *nats.Msg) (bool, error) {
	// If payload or no header, consider this a user message
	if len(msg.Data) > 0 || len(msg.Header) == 0 {
		return true, nil
	}
	// Look for status header
	val := msg.Header.Get("Status")
	descr := msg.Header.Get("Description")
	// If not present, then this is considered a user message
	if val == "" {
		return true, nil
	}

	switch val {
	case badRequest:
		return false, ErrBadRequest
	case noResponders:
		return false, nats.ErrNoResponders
	case noMessages:
		// 404 indicates that there are no messages.
		return false, ErrNoMessages
	case reqTimeout:
		return false, nats.ErrTimeout
	case controlMsg:
		return false, nil
	case maxBytesExceeded:
		if strings.Contains(strings.ToLower(descr), "message size exceeds maxbytes") {
			return false, ErrMaxBytesExceeded
		}
		if strings.Contains(strings.ToLower(descr), "consumer deleted") {
			return false, ErrConsumerDeleted
		}
		if strings.Contains(strings.ToLower(descr), "leadership change") {
			return false, ErrConsumerLeadershipChanged
		}
	}
	return false, fmt.Errorf("nats: %s", msg.Header.Get("Description"))
}

func parsePending(msg *nats.Msg) (int, int, error) {
	msgsLeftStr := msg.Header.Get("Nats-Pending-Messages")
	var msgsLeft int
	var err error
	if msgsLeftStr != "" {
		msgsLeft, err = strconv.Atoi(msgsLeftStr)
		if err != nil {
			return 0, 0, fmt.Errorf("nats: invalid format of Nats-Pending-Messages")
		}
	}
	bytesLeftStr := msg.Header.Get("Nats-Pending-Bytes")
	var bytesLeft int
	if bytesLeftStr != "" {
		bytesLeft, err = strconv.Atoi(bytesLeftStr)
		if err != nil {
			return 0, 0, fmt.Errorf("nats: invalid format of Nats-Pending-Bytes")
		}
	}
	return msgsLeft, bytesLeft, nil
}

// toJSMsg converts core [nats.Msg] to [jetStreamMsg], exposing JetStream-specific operations
func (js *jetStream) toJSMsg(msg *nats.Msg) *jetStreamMsg {
	return &jetStreamMsg{
		msg: msg,
		js:  js,
	}
}

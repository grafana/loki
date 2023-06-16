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
	"fmt"
	"time"
)

type pullOptFunc func(*consumeOpts) error

func (fn pullOptFunc) configureConsume(opts *consumeOpts) error {
	return fn(opts)
}

func (fn pullOptFunc) configureMessages(opts *consumeOpts) error {
	return fn(opts)
}

// WithClientTrace enables request/response API calls tracing
// ClientTrace is used to provide handlers for each event
func WithClientTrace(ct *ClientTrace) JetStreamOpt {
	return func(opts *jsOpts) error {
		opts.clientTrace = ct
		return nil
	}
}

// WithPublishAsyncErrHandler sets error handler for async message publish
func WithPublishAsyncErrHandler(cb MsgErrHandler) JetStreamOpt {
	return func(opts *jsOpts) error {
		opts.publisherOpts.aecb = cb
		return nil
	}
}

// WithPublishAsyncMaxPending sets the maximum outstanding async publishes that can be inflight at one time.
func WithPublishAsyncMaxPending(max int) JetStreamOpt {
	return func(opts *jsOpts) error {
		if max < 1 {
			return fmt.Errorf("%w: max ack pending should be >= 1", ErrInvalidOption)
		}
		opts.publisherOpts.maxpa = max
		return nil
	}
}

// WithPurgeSubject sets a sprecific subject for which messages on a stream will be purged
func WithPurgeSubject(subject string) StreamPurgeOpt {
	return func(req *StreamPurgeRequest) error {
		req.Subject = subject
		return nil
	}
}

// WithPurgeSequence is used to set a sprecific sequence number up to which (but not including) messages will be purged from a stream
// Can be combined with [WithPurgeSubject] option, but not with [WithPurgeKeep]
func WithPurgeSequence(sequence uint64) StreamPurgeOpt {
	return func(req *StreamPurgeRequest) error {
		if req.Keep != 0 {
			return fmt.Errorf("%w: both 'keep' and 'sequence' cannot be provided in purge request", ErrInvalidOption)
		}
		req.Sequence = sequence
		return nil
	}
}

// WithPurgeKeep sets the number of messages to be kept in the stream after purge.
// Can be combined with [WithPurgeSubject] option, but not with [WithPurgeSequence]
func WithPurgeKeep(keep uint64) StreamPurgeOpt {
	return func(req *StreamPurgeRequest) error {
		if req.Sequence != 0 {
			return fmt.Errorf("%w: both 'keep' and 'sequence' cannot be provided in purge request", ErrInvalidOption)
		}
		req.Keep = keep
		return nil
	}
}

// WithGetMsgSubject sets the stream subject from which the message should be retrieved.
// Server will return a first message with a seq >= to the input seq that has the specified subject.
func WithGetMsgSubject(subject string) GetMsgOpt {
	return func(req *apiMsgGetRequest) error {
		req.NextFor = subject
		return nil
	}
}

// PullMaxMessages limits the number of messages to be fetched from the stream in one request
// If not provided, a default of 100 messages will be used
type PullMaxMessages int

func (max PullMaxMessages) configureConsume(opts *consumeOpts) error {
	if max <= 0 {
		return fmt.Errorf("%w: maxMessages size must be at least 1", ErrInvalidOption)
	}
	opts.MaxMessages = int(max)
	return nil
}

func (max PullMaxMessages) configureMessages(opts *consumeOpts) error {
	if max <= 0 {
		return fmt.Errorf("%w: maxMessages size must be at least 1", ErrInvalidOption)
	}
	opts.MaxMessages = int(max)
	return nil
}

// PullExpiry sets timeout on a single batch request, waiting until at least one message is available
type PullExpiry time.Duration

func (exp PullExpiry) configureConsume(opts *consumeOpts) error {
	if exp < 0 {
		return fmt.Errorf("%w: expires value must be positive", ErrInvalidOption)
	}
	opts.Expires = time.Duration(exp)
	return nil
}

func (exp PullExpiry) configureMessages(opts *consumeOpts) error {
	if exp < 0 {
		return fmt.Errorf("%w: expires value must be positive", ErrInvalidOption)
	}
	opts.Expires = time.Duration(exp)
	return nil
}

// PullMaxBytes sets max_bytes limit on a fetch request
type PullMaxBytes int

func (max PullMaxBytes) configureConsume(opts *consumeOpts) error {
	if max <= 0 {
		return fmt.Errorf("%w: max bytes must be greater then 0", ErrInvalidOption)
	}
	opts.MaxBytes = int(max)
	return nil
}

func (max PullMaxBytes) configureMessages(opts *consumeOpts) error {
	if max <= 0 {
		return fmt.Errorf("%w: max bytes must be greater then 0", ErrInvalidOption)
	}
	opts.MaxBytes = int(max)
	return nil
}

// PullThresholdMessages sets the message count on which Consume will trigger
// new pull request to the server. Defaults to 50% of MaxMessages.
type PullThresholdMessages int

func (t PullThresholdMessages) configureConsume(opts *consumeOpts) error {
	opts.ThresholdMessages = int(t)
	return nil
}

// PullThresholdBytes sets the byte count on which Consume will trigger
// new pull request to the server. Defaults to 50% of MaxBytes (if set).
type PullThresholdBytes int

func (t PullThresholdBytes) configureConsume(opts *consumeOpts) error {
	opts.ThresholdBytes = int(t)
	return nil
}

// PullHeartbeat sets the idle heartbeat duration for a pull subscription
// If a client does not receive a heartbeat message from a stream for more
// than the idle heartbeat setting, the subscription will be removed
// and error will be passed to the message handler
type PullHeartbeat time.Duration

func (hb PullHeartbeat) configureConsume(opts *consumeOpts) error {
	hbTime := time.Duration(hb)
	if hbTime < 1*time.Second || hbTime > 30*time.Second {
		return fmt.Errorf("%w: idle_heartbeat value must be within 1s-30s range", ErrInvalidOption)
	}
	opts.Heartbeat = hbTime
	return nil
}

func (hb PullHeartbeat) configureMessages(opts *consumeOpts) error {
	hbTime := time.Duration(hb)
	if hbTime < 1*time.Second || hbTime > 30*time.Second {
		return fmt.Errorf("%w: idle_heartbeat value must be within 1s-30s range", ErrInvalidOption)
	}
	opts.Heartbeat = hbTime
	return nil
}

// ConsumeErrHandler sets custom error handler invoked when an error was encountered while consuming messages
// It will be invoked for both terminal (Consumer Deleted, invalid request body) and non-terminal (e.g. missing heartbeats) errors
func ConsumeErrHandler(cb ConsumeErrHandlerFunc) PullConsumeOpt {
	return pullOptFunc(func(cfg *consumeOpts) error {
		cfg.ErrHandler = cb
		return nil
	})
}

// ConsumeErrHandler sets custom error handler invoked when an error was encountered while consuming messages
// It will be invoked for both terminal (Consumer Deleted, invalid request body) and non-terminal (e.g. missing heartbeats) errors
func WithMessagesErrOnMissingHeartbeat(hbErr bool) PullMessagesOpt {
	return pullOptFunc(func(cfg *consumeOpts) error {
		cfg.ReportMissingHeartbeats = hbErr
		return nil
	})
}

// FetchMaxWait sets custom timeout fir fetching predefined batch of messages
func FetchMaxWait(timeout time.Duration) FetchOpt {
	return func(req *pullRequest) error {
		if timeout <= 0 {
			return fmt.Errorf("%w: timeout value must be greater than 0", ErrInvalidOption)
		}
		req.Expires = timeout
		return nil
	}
}

// WithDeletedDetails can be used to display the information about messages deleted from a stream on a stream info request
func WithDeletedDetails(deletedDetails bool) StreamInfoOpt {
	return func(req *streamInfoRequest) error {
		req.DeletedDetails = deletedDetails
		return nil
	}
}

// WithSubjectFilter can be used to display the information about messages stored on given subjects
func WithSubjectFilter(subject string) StreamInfoOpt {
	return func(req *streamInfoRequest) error {
		req.SubjectFilter = subject
		return nil
	}
}

// WithNakDelay can be used to specify the duration after which the message should be redelivered
func WithNakDelay(delay time.Duration) NakOpt {
	return func(opts *ackOpts) error {
		opts.nakDelay = delay
		return nil
	}
}

// WithMsgID sets the message ID used for deduplication.
func WithMsgID(id string) PublishOpt {
	return func(opts *pubOpts) error {
		opts.id = id
		return nil
	}
}

// WithExpectStream sets the expected stream to respond from the publish.
func WithExpectStream(stream string) PublishOpt {
	return func(opts *pubOpts) error {
		opts.stream = stream
		return nil
	}
}

// WithExpectLastSequence sets the expected sequence in the response from the publish.
func WithExpectLastSequence(seq uint64) PublishOpt {
	return func(opts *pubOpts) error {
		opts.lastSeq = &seq
		return nil
	}
}

// WithExpectLastSequencePerSubject sets the expected sequence per subject in the response from the publish.
func WithExpectLastSequencePerSubject(seq uint64) PublishOpt {
	return func(opts *pubOpts) error {
		opts.lastSubjectSeq = &seq
		return nil
	}
}

// ExpectLastMsgId sets the expected last msgId in the response from the publish.
func WithExpectLastMsgID(id string) PublishOpt {
	return func(opts *pubOpts) error {
		opts.lastMsgID = id
		return nil
	}
}

// WithRetryWait sets the retry wait time when ErrNoResponders is encountered.
func WithRetryWait(dur time.Duration) PublishOpt {
	return func(opts *pubOpts) error {
		opts.retryWait = dur
		return nil
	}
}

// WithRetryAttempts sets the retry number of attempts when ErrNoResponders is encountered.
func WithRetryAttempts(num int) PublishOpt {
	return func(opts *pubOpts) error {
		opts.retryAttempts = num
		return nil
	}
}

// WithStallWait sets the max wait when the producer becomes stall producing messages.
func WithStallWait(ttl time.Duration) PublishOpt {
	return func(opts *pubOpts) error {
		if ttl <= 0 {
			return fmt.Errorf("%w: stall wait should be more than 0", ErrInvalidOption)
		}
		opts.stallWait = ttl
		return nil
	}
}

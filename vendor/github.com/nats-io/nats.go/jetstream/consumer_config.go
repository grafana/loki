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
	"encoding/json"
	"fmt"
	"time"
)

type (
	// ConsumerInfo is the info from a JetStream consumer.
	ConsumerInfo struct {
		Stream         string         `json:"stream_name"`
		Name           string         `json:"name"`
		Created        time.Time      `json:"created"`
		Config         ConsumerConfig `json:"config"`
		Delivered      SequenceInfo   `json:"delivered"`
		AckFloor       SequenceInfo   `json:"ack_floor"`
		NumAckPending  int            `json:"num_ack_pending"`
		NumRedelivered int            `json:"num_redelivered"`
		NumWaiting     int            `json:"num_waiting"`
		NumPending     uint64         `json:"num_pending"`
		Cluster        *ClusterInfo   `json:"cluster,omitempty"`
		PushBound      bool           `json:"push_bound,omitempty"`
	}

	// ConsumerConfig is the configuration of a JetStream consumer.
	ConsumerConfig struct {
		Name            string          `json:"name,omitempty"`
		Durable         string          `json:"durable_name,omitempty"`
		Description     string          `json:"description,omitempty"`
		DeliverPolicy   DeliverPolicy   `json:"deliver_policy"`
		OptStartSeq     uint64          `json:"opt_start_seq,omitempty"`
		OptStartTime    *time.Time      `json:"opt_start_time,omitempty"`
		AckPolicy       AckPolicy       `json:"ack_policy"`
		AckWait         time.Duration   `json:"ack_wait,omitempty"`
		MaxDeliver      int             `json:"max_deliver,omitempty"`
		BackOff         []time.Duration `json:"backoff,omitempty"`
		FilterSubjects  []string        `json:"filter_subjects,omitempty"`
		FilterSubject   string          `json:"filter_subject,omitempty"`
		ReplayPolicy    ReplayPolicy    `json:"replay_policy"`
		RateLimit       uint64          `json:"rate_limit_bps,omitempty"` // Bits per sec
		SampleFrequency string          `json:"sample_freq,omitempty"`
		MaxWaiting      int             `json:"max_waiting,omitempty"`
		MaxAckPending   int             `json:"max_ack_pending,omitempty"`
		HeadersOnly     bool            `json:"headers_only,omitempty"`

		// Pull based options.
		MaxRequestBatch    int           `json:"max_batch,omitempty"`
		MaxRequestExpires  time.Duration `json:"max_expires,omitempty"`
		MaxRequestMaxBytes int           `json:"max_bytes,omitempty"`

		// Inactivity threshold.
		InactiveThreshold time.Duration `json:"inactive_threshold,omitempty"`

		// Generally inherited by parent stream and other markers, now can be configured directly.
		Replicas int `json:"num_replicas"`
		// Force memory storage.
		MemoryStorage bool `json:"mem_storage,omitempty"`
	}

	OrderedConsumerConfig struct {
		FilterSubjects    []string      `json:"filter_subjects,omitempty"`
		DeliverPolicy     DeliverPolicy `json:"deliver_policy"`
		OptStartSeq       uint64        `json:"opt_start_seq,omitempty"`
		OptStartTime      *time.Time    `json:"opt_start_time,omitempty"`
		ReplayPolicy      ReplayPolicy  `json:"replay_policy"`
		InactiveThreshold time.Duration `json:"inactive_threshold,omitempty"`

		// Maximum number of attempts for the consumer to be recreated
		// Defaults to unlimited
		MaxResetAttempts int
	}

	DeliverPolicy int

	// AckPolicy determines how the consumer should acknowledge delivered messages.
	AckPolicy int

	// ReplayPolicy determines how the consumer should replay messages it already has queued in the stream.
	ReplayPolicy int

	// SequenceInfo has both the consumer and the stream sequence and last activity.
	SequenceInfo struct {
		Consumer uint64     `json:"consumer_seq"`
		Stream   uint64     `json:"stream_seq"`
		Last     *time.Time `json:"last_active,omitempty"`
	}
)

const (
	// DeliverAllPolicy starts delivering messages from the very beginning of a
	// stream. This is the default.
	DeliverAllPolicy DeliverPolicy = iota

	// DeliverLastPolicy will start the consumer with the last sequence
	// received.
	DeliverLastPolicy

	// DeliverNewPolicy will only deliver new messages that are sent after the
	// consumer is created.
	DeliverNewPolicy

	// DeliverByStartSequencePolicy will deliver messages starting from a given
	// sequence.
	DeliverByStartSequencePolicy

	// DeliverByStartTimePolicy will deliver messages starting from a given
	// time.
	DeliverByStartTimePolicy

	// DeliverLastPerSubjectPolicy will start the consumer with the last message
	// for all subjects received.
	DeliverLastPerSubjectPolicy
)

func (p *DeliverPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString("all"), jsonString("undefined"):
		*p = DeliverAllPolicy
	case jsonString("last"):
		*p = DeliverLastPolicy
	case jsonString("new"):
		*p = DeliverNewPolicy
	case jsonString("by_start_sequence"):
		*p = DeliverByStartSequencePolicy
	case jsonString("by_start_time"):
		*p = DeliverByStartTimePolicy
	case jsonString("last_per_subject"):
		*p = DeliverLastPerSubjectPolicy
	default:
		return fmt.Errorf("nats: can not unmarshal %q", data)
	}

	return nil
}

func (p DeliverPolicy) MarshalJSON() ([]byte, error) {
	switch p {
	case DeliverAllPolicy:
		return json.Marshal("all")
	case DeliverLastPolicy:
		return json.Marshal("last")
	case DeliverNewPolicy:
		return json.Marshal("new")
	case DeliverByStartSequencePolicy:
		return json.Marshal("by_start_sequence")
	case DeliverByStartTimePolicy:
		return json.Marshal("by_start_time")
	case DeliverLastPerSubjectPolicy:
		return json.Marshal("last_per_subject")
	}
	return nil, fmt.Errorf("nats: unknown deliver policy %v", p)
}

func (p DeliverPolicy) String() string {
	switch p {
	case DeliverAllPolicy:
		return "all"
	case DeliverLastPolicy:
		return "last"
	case DeliverNewPolicy:
		return "new"
	case DeliverByStartSequencePolicy:
		return "by_start_sequence"
	case DeliverByStartTimePolicy:
		return "by_start_time"
	case DeliverLastPerSubjectPolicy:
		return "last_per_subject"
	}
	return ""
}

const (
	// AckExplicitPolicy requires ack or nack for all messages.
	AckExplicitPolicy AckPolicy = iota

	// AckAllPolicy when acking a sequence number, this implicitly acks all
	// sequences below this one as well.
	AckAllPolicy

	// AckNonePolicy requires no acks for delivered messages.
	AckNonePolicy
)

func (p *AckPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString("none"):
		*p = AckNonePolicy
	case jsonString("all"):
		*p = AckAllPolicy
	case jsonString("explicit"):
		*p = AckExplicitPolicy
	default:
		return fmt.Errorf("nats: can not unmarshal %q", data)
	}
	return nil
}

func (p AckPolicy) MarshalJSON() ([]byte, error) {
	switch p {
	case AckNonePolicy:
		return json.Marshal("none")
	case AckAllPolicy:
		return json.Marshal("all")
	case AckExplicitPolicy:
		return json.Marshal("explicit")
	}
	return nil, fmt.Errorf("nats: unknown acknowledgement policy %v", p)
}

func (p AckPolicy) String() string {
	switch p {
	case AckNonePolicy:
		return "AckNone"
	case AckAllPolicy:
		return "AckAll"
	case AckExplicitPolicy:
		return "AckExplicit"
	}
	return "Unknown AckPolicy"
}

const (
	// ReplayInstantPolicy will replay messages as fast as possible.
	ReplayInstantPolicy ReplayPolicy = iota

	// ReplayOriginalPolicy will maintain the same timing as the messages were received.
	ReplayOriginalPolicy
)

func (p *ReplayPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString("instant"):
		*p = ReplayInstantPolicy
	case jsonString("original"):
		*p = ReplayOriginalPolicy
	default:
		return fmt.Errorf("nats: can not unmarshal %q", data)
	}
	return nil
}

func (p ReplayPolicy) MarshalJSON() ([]byte, error) {
	switch p {
	case ReplayOriginalPolicy:
		return json.Marshal("original")
	case ReplayInstantPolicy:
		return json.Marshal("instant")
	}
	return nil, fmt.Errorf("nats: unknown replay policy %v", p)
}

func (p ReplayPolicy) String() string {
	switch p {
	case ReplayOriginalPolicy:
		return "original"
	case ReplayInstantPolicy:
		return "instant"
	}
	return ""
}

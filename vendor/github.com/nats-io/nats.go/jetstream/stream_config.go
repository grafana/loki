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
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type (
	// StreamInfo shows config and current state for this stream.
	StreamInfo struct {
		Config  StreamConfig        `json:"config"`
		Created time.Time           `json:"created"`
		State   StreamState         `json:"state"`
		Cluster *ClusterInfo        `json:"cluster,omitempty"`
		Mirror  *StreamSourceInfo   `json:"mirror,omitempty"`
		Sources []*StreamSourceInfo `json:"sources,omitempty"`
	}

	StreamConfig struct {
		Name                 string          `json:"name"`
		Description          string          `json:"description,omitempty"`
		Subjects             []string        `json:"subjects,omitempty"`
		Retention            RetentionPolicy `json:"retention"`
		MaxConsumers         int             `json:"max_consumers"`
		MaxMsgs              int64           `json:"max_msgs"`
		MaxBytes             int64           `json:"max_bytes"`
		Discard              DiscardPolicy   `json:"discard"`
		DiscardNewPerSubject bool            `json:"discard_new_per_subject,omitempty"`
		MaxAge               time.Duration   `json:"max_age"`
		MaxMsgsPerSubject    int64           `json:"max_msgs_per_subject"`
		MaxMsgSize           int32           `json:"max_msg_size,omitempty"`
		Storage              StorageType     `json:"storage"`
		Replicas             int             `json:"num_replicas"`
		NoAck                bool            `json:"no_ack,omitempty"`
		Template             string          `json:"template_owner,omitempty"`
		Duplicates           time.Duration   `json:"duplicate_window,omitempty"`
		Placement            *Placement      `json:"placement,omitempty"`
		Mirror               *StreamSource   `json:"mirror,omitempty"`
		Sources              []*StreamSource `json:"sources,omitempty"`
		Sealed               bool            `json:"sealed,omitempty"`
		DenyDelete           bool            `json:"deny_delete,omitempty"`
		DenyPurge            bool            `json:"deny_purge,omitempty"`
		AllowRollup          bool            `json:"allow_rollup_hdrs,omitempty"`

		// Allow republish of the message after being sequenced and stored.
		RePublish *RePublish `json:"republish,omitempty"`

		// Allow higher performance, direct access to get individual messages. E.g. KeyValue
		AllowDirect bool `json:"allow_direct"`
		// Allow higher performance and unified direct access for mirrors as well.
		MirrorDirect bool `json:"mirror_direct"`
	}

	// StreamSourceInfo shows information about an upstream stream source.
	StreamSourceInfo struct {
		Name   string        `json:"name"`
		Lag    uint64        `json:"lag"`
		Active time.Duration `json:"active"`
	}

	// StreamState is information about the given stream.
	StreamState struct {
		Msgs        uint64            `json:"messages"`
		Bytes       uint64            `json:"bytes"`
		FirstSeq    uint64            `json:"first_seq"`
		FirstTime   time.Time         `json:"first_ts"`
		LastSeq     uint64            `json:"last_seq"`
		LastTime    time.Time         `json:"last_ts"`
		Consumers   int               `json:"consumer_count"`
		Deleted     []uint64          `json:"deleted"`
		NumDeleted  int               `json:"num_deleted"`
		NumSubjects uint64            `json:"num_subjects"`
		Subjects    map[string]uint64 `json:"subjects"`
	}

	// ClusterInfo shows information about the underlying set of servers
	// that make up the stream or consumer.
	ClusterInfo struct {
		Name     string      `json:"name,omitempty"`
		Leader   string      `json:"leader,omitempty"`
		Replicas []*PeerInfo `json:"replicas,omitempty"`
	}

	// PeerInfo shows information about all the peers in the cluster that
	// are supporting the stream or consumer.
	PeerInfo struct {
		Name    string        `json:"name"`
		Current bool          `json:"current"`
		Offline bool          `json:"offline,omitempty"`
		Active  time.Duration `json:"active"`
		Lag     uint64        `json:"lag,omitempty"`
	}

	// RePublish is for republishing messages once committed to a stream. The original
	// subject cis remapped from the subject pattern to the destination pattern.
	RePublish struct {
		Source      string `json:"src,omitempty"`
		Destination string `json:"dest"`
		HeadersOnly bool   `json:"headers_only,omitempty"`
	}

	// Placement is used to guide placement of streams in clustered JetStream.
	Placement struct {
		Cluster string   `json:"cluster"`
		Tags    []string `json:"tags,omitempty"`
	}

	// StreamSource dictates how streams can source from other streams.
	StreamSource struct {
		Name          string          `json:"name"`
		OptStartSeq   uint64          `json:"opt_start_seq,omitempty"`
		OptStartTime  *time.Time      `json:"opt_start_time,omitempty"`
		FilterSubject string          `json:"filter_subject,omitempty"`
		External      *ExternalStream `json:"external,omitempty"`
		Domain        string          `json:"-"`
	}

	// ExternalStream allows you to qualify access to a stream source in another
	// account.
	ExternalStream struct {
		APIPrefix     string `json:"api"`
		DeliverPrefix string `json:"deliver"`
	}

	// DiscardPolicy determines how to proceed when limits of messages or bytes are
	// reached.
	DiscardPolicy int

	// RetentionPolicy determines how messages in a set are retained.
	RetentionPolicy int

	// StorageType determines how messages are stored for retention.
	StorageType int
)

const (
	// LimitsPolicy (default) means that messages are retained until any given limit is reached.
	// This could be one of MaxMsgs, MaxBytes, or MaxAge.
	LimitsPolicy RetentionPolicy = iota
	// InterestPolicy specifies that when all known observables have acknowledged a message it can be removed.
	InterestPolicy
	// WorkQueuePolicy specifies that when the first worker or subscriber acknowledges the message it can be removed.
	WorkQueuePolicy
)

const (
	// DiscardOld will remove older messages to return to the limits. This is
	// the default.
	DiscardOld DiscardPolicy = iota
	//DiscardNew will fail to store new messages.
	DiscardNew
)

const (
	limitsPolicyString    = "limits"
	interestPolicyString  = "interest"
	workQueuePolicyString = "workqueue"
)

func (rp RetentionPolicy) String() string {
	switch rp {
	case LimitsPolicy:
		return "Limits"
	case InterestPolicy:
		return "Interest"
	case WorkQueuePolicy:
		return "WorkQueue"
	default:
		return "Unknown Retention Policy"
	}
}

func (rp RetentionPolicy) MarshalJSON() ([]byte, error) {
	switch rp {
	case LimitsPolicy:
		return json.Marshal(limitsPolicyString)
	case InterestPolicy:
		return json.Marshal(interestPolicyString)
	case WorkQueuePolicy:
		return json.Marshal(workQueuePolicyString)
	default:
		return nil, fmt.Errorf("nats: can not marshal %v", rp)
	}
}

func (rp *RetentionPolicy) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString(limitsPolicyString):
		*rp = LimitsPolicy
	case jsonString(interestPolicyString):
		*rp = InterestPolicy
	case jsonString(workQueuePolicyString):
		*rp = WorkQueuePolicy
	default:
		return fmt.Errorf("nats: can not unmarshal %q", data)
	}
	return nil
}

func (dp DiscardPolicy) String() string {
	switch dp {
	case DiscardOld:
		return "DiscardOld"
	case DiscardNew:
		return "DiscardNew"
	default:
		return "Unknown Discard Policy"
	}
}

func (dp DiscardPolicy) MarshalJSON() ([]byte, error) {
	switch dp {
	case DiscardOld:
		return json.Marshal("old")
	case DiscardNew:
		return json.Marshal("new")
	default:
		return nil, fmt.Errorf("nats: can not marshal %v", dp)
	}
}

func (dp *DiscardPolicy) UnmarshalJSON(data []byte) error {
	switch strings.ToLower(string(data)) {
	case jsonString("old"):
		*dp = DiscardOld
	case jsonString("new"):
		*dp = DiscardNew
	default:
		return fmt.Errorf("nats: can not unmarshal %q", data)
	}
	return nil
}

const (
	// FileStorage specifies on disk storage. It's the default.
	FileStorage StorageType = iota
	// MemoryStorage specifies in memory only.
	MemoryStorage
)

const (
	memoryStorageString = "memory"
	fileStorageString   = "file"
)

func (st StorageType) String() string {
	caser := cases.Title(language.AmericanEnglish)
	switch st {
	case MemoryStorage:
		return caser.String(memoryStorageString)
	case FileStorage:
		return caser.String(fileStorageString)
	default:
		return "Unknown Storage Type"
	}
}

func (st StorageType) MarshalJSON() ([]byte, error) {
	switch st {
	case MemoryStorage:
		return json.Marshal(memoryStorageString)
	case FileStorage:
		return json.Marshal(fileStorageString)
	default:
		return nil, fmt.Errorf("nats: can not marshal %v", st)
	}
}

func (st *StorageType) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case jsonString(memoryStorageString):
		*st = MemoryStorage
	case jsonString(fileStorageString):
		*st = FileStorage
	default:
		return fmt.Errorf("nats: can not unmarshal %q", data)
	}
	return nil
}

func jsonString(s string) string {
	return "\"" + s + "\""
}

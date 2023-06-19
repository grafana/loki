// Copyright 2019-2023 The NATS Authors
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

package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nuid"
	"golang.org/x/time/rate"
)

// Headers sent with Request Timeout
const (
	JSPullRequestPendingMsgs  = "Nats-Pending-Messages"
	JSPullRequestPendingBytes = "Nats-Pending-Bytes"
)

type ConsumerInfo struct {
	Stream         string          `json:"stream_name"`
	Name           string          `json:"name"`
	Created        time.Time       `json:"created"`
	Config         *ConsumerConfig `json:"config,omitempty"`
	Delivered      SequenceInfo    `json:"delivered"`
	AckFloor       SequenceInfo    `json:"ack_floor"`
	NumAckPending  int             `json:"num_ack_pending"`
	NumRedelivered int             `json:"num_redelivered"`
	NumWaiting     int             `json:"num_waiting"`
	NumPending     uint64          `json:"num_pending"`
	Cluster        *ClusterInfo    `json:"cluster,omitempty"`
	PushBound      bool            `json:"push_bound,omitempty"`
}

type ConsumerConfig struct {
	// Durable is deprecated. All consumers should have names, picked by clients.
	Durable         string          `json:"durable_name,omitempty"`
	Name            string          `json:"name,omitempty"`
	Description     string          `json:"description,omitempty"`
	DeliverPolicy   DeliverPolicy   `json:"deliver_policy"`
	OptStartSeq     uint64          `json:"opt_start_seq,omitempty"`
	OptStartTime    *time.Time      `json:"opt_start_time,omitempty"`
	AckPolicy       AckPolicy       `json:"ack_policy"`
	AckWait         time.Duration   `json:"ack_wait,omitempty"`
	MaxDeliver      int             `json:"max_deliver,omitempty"`
	BackOff         []time.Duration `json:"backoff,omitempty"`
	FilterSubject   string          `json:"filter_subject,omitempty"`
	ReplayPolicy    ReplayPolicy    `json:"replay_policy"`
	RateLimit       uint64          `json:"rate_limit_bps,omitempty"` // Bits per sec
	SampleFrequency string          `json:"sample_freq,omitempty"`
	MaxWaiting      int             `json:"max_waiting,omitempty"`
	MaxAckPending   int             `json:"max_ack_pending,omitempty"`
	Heartbeat       time.Duration   `json:"idle_heartbeat,omitempty"`
	FlowControl     bool            `json:"flow_control,omitempty"`
	HeadersOnly     bool            `json:"headers_only,omitempty"`

	// Pull based options.
	MaxRequestBatch    int           `json:"max_batch,omitempty"`
	MaxRequestExpires  time.Duration `json:"max_expires,omitempty"`
	MaxRequestMaxBytes int           `json:"max_bytes,omitempty"`

	// Push based consumers.
	DeliverSubject string `json:"deliver_subject,omitempty"`
	DeliverGroup   string `json:"deliver_group,omitempty"`

	// Ephemeral inactivity threshold.
	InactiveThreshold time.Duration `json:"inactive_threshold,omitempty"`

	// Generally inherited by parent stream and other markers, now can be configured directly.
	Replicas int `json:"num_replicas"`
	// Force memory storage.
	MemoryStorage bool `json:"mem_storage,omitempty"`

	// Don't add to general clients.
	Direct bool `json:"direct,omitempty"`
}

// SequenceInfo has both the consumer and the stream sequence and last activity.
type SequenceInfo struct {
	Consumer uint64     `json:"consumer_seq"`
	Stream   uint64     `json:"stream_seq"`
	Last     *time.Time `json:"last_active,omitempty"`
}

type CreateConsumerRequest struct {
	Stream string         `json:"stream_name"`
	Config ConsumerConfig `json:"config"`
}

// ConsumerNakOptions is for optional NAK values, e.g. delay.
type ConsumerNakOptions struct {
	Delay time.Duration `json:"delay"`
}

// DeliverPolicy determines how the consumer should select the first message to deliver.
type DeliverPolicy int

const (
	// DeliverAll will be the default so can be omitted from the request.
	DeliverAll DeliverPolicy = iota
	// DeliverLast will start the consumer with the last sequence received.
	DeliverLast
	// DeliverNew will only deliver new messages that are sent after the consumer is created.
	DeliverNew
	// DeliverByStartSequence will look for a defined starting sequence to start.
	DeliverByStartSequence
	// DeliverByStartTime will select the first messsage with a timestamp >= to StartTime.
	DeliverByStartTime
	// DeliverLastPerSubject will start the consumer with the last message for all subjects received.
	DeliverLastPerSubject
)

func (dp DeliverPolicy) String() string {
	switch dp {
	case DeliverAll:
		return "all"
	case DeliverLast:
		return "last"
	case DeliverNew:
		return "new"
	case DeliverByStartSequence:
		return "by_start_sequence"
	case DeliverByStartTime:
		return "by_start_time"
	case DeliverLastPerSubject:
		return "last_per_subject"
	default:
		return "undefined"
	}
}

// AckPolicy determines how the consumer should acknowledge delivered messages.
type AckPolicy int

const (
	// AckNone requires no acks for delivered messages.
	AckNone AckPolicy = iota
	// AckAll when acking a sequence number, this implicitly acks all sequences below this one as well.
	AckAll
	// AckExplicit requires ack or nack for all messages.
	AckExplicit
)

func (a AckPolicy) String() string {
	switch a {
	case AckNone:
		return "none"
	case AckAll:
		return "all"
	default:
		return "explicit"
	}
}

// ReplayPolicy determines how the consumer should replay messages it already has queued in the stream.
type ReplayPolicy int

const (
	// ReplayInstant will replay messages as fast as possible.
	ReplayInstant ReplayPolicy = iota
	// ReplayOriginal will maintain the same timing as the messages were received.
	ReplayOriginal
)

func (r ReplayPolicy) String() string {
	switch r {
	case ReplayInstant:
		return "instant"
	default:
		return "original"
	}
}

// OK
const OK = "+OK"

// Ack responses. Note that a nil or no payload is same as AckAck
var (
	// Ack
	AckAck = []byte("+ACK") // nil or no payload to ack subject also means ACK
	AckOK  = []byte(OK)     // deprecated but +OK meant ack as well.

	// Nack
	AckNak = []byte("-NAK")
	// Progress indicator
	AckProgress = []byte("+WPI")
	// Ack + Deliver the next message(s).
	AckNext = []byte("+NXT")
	// Terminate delivery of the message.
	AckTerm = []byte("+TERM")
)

// Calculate accurate replicas for the consumer config with the parent stream config.
func (consCfg ConsumerConfig) replicas(strCfg *StreamConfig) int {
	if consCfg.Replicas == 0 {
		if !isDurableConsumer(&consCfg) && strCfg.Retention == LimitsPolicy {
			return 1
		}
		return strCfg.Replicas
	} else {
		return consCfg.Replicas
	}
}

// Consumer is a jetstream consumer.
type consumer struct {
	// Atomic used to notify that we want to process an ack.
	// This will be checked in checkPending to abort processing
	// and let ack be processed in priority.
	awl               int64
	mu                sync.RWMutex
	js                *jetStream
	mset              *stream
	acc               *Account
	srv               *Server
	client            *client
	sysc              *client
	sid               int
	name              string
	stream            string
	sseq              uint64
	dseq              uint64
	adflr             uint64
	asflr             uint64
	npc               int64
	npf               uint64
	dsubj             string
	qgroup            string
	lss               *lastSeqSkipList
	rlimit            *rate.Limiter
	reqSub            *subscription
	ackSub            *subscription
	ackReplyT         string
	ackSubj           string
	nextMsgSubj       string
	nextMsgReqs       *ipQueue[*nextMsgReq]
	maxp              int
	pblimit           int
	maxpb             int
	pbytes            int
	fcsz              int
	fcid              string
	fcSub             *subscription
	outq              *jsOutQ
	pending           map[uint64]*Pending
	ptmr              *time.Timer
	rdq               []uint64
	rdqi              map[uint64]struct{}
	rdc               map[uint64]uint64
	maxdc             uint64
	waiting           *waitQueue
	cfg               ConsumerConfig
	ici               *ConsumerInfo
	store             ConsumerStore
	active            bool
	replay            bool
	filterWC          bool
	dtmr              *time.Timer
	gwdtmr            *time.Timer
	dthresh           time.Duration
	mch               chan struct{}
	qch               chan struct{}
	inch              chan bool
	sfreq             int32
	ackEventT         string
	nakEventT         string
	deliveryExcEventT string
	created           time.Time
	ldt               time.Time
	lat               time.Time
	closed            bool

	// Clustered.
	ca        *consumerAssignment
	node      RaftNode
	infoSub   *subscription
	lqsent    time.Time
	prm       map[string]struct{}
	prOk      bool
	uch       chan struct{}
	retention RetentionPolicy

	monitorWg sync.WaitGroup
	inMonitor bool

	// R>1 proposals
	pch   chan struct{}
	phead *proposal
	ptail *proposal

	// Ack queue
	ackMsgs *ipQueue[*jsAckMsg]

	// For stream signaling.
	sigSub *subscription
}

type proposal struct {
	data []byte
	next *proposal
}

const (
	// JsAckWaitDefault is the default AckWait, only applicable on explicit ack policy consumers.
	JsAckWaitDefault = 30 * time.Second
	// JsDeleteWaitTimeDefault is the default amount of time we will wait for non-durable
	// consumers to be in an inactive state before deleting them.
	JsDeleteWaitTimeDefault = 5 * time.Second
	// JsFlowControlMaxPending specifies default pending bytes during flow control that can be
	// outstanding.
	JsFlowControlMaxPending = 32 * 1024 * 1024
	// JsDefaultMaxAckPending is set for consumers with explicit ack that do not set the max ack pending.
	JsDefaultMaxAckPending = 1000
)

// Helper function to set consumer config defaults from above.
func setConsumerConfigDefaults(config *ConsumerConfig, lim *JSLimitOpts, accLim *JetStreamAccountLimits) {
	// Set to default if not specified.
	if config.DeliverSubject == _EMPTY_ && config.MaxWaiting == 0 {
		config.MaxWaiting = JSWaitQueueDefaultMax
	}
	// Setup proper default for ack wait if we are in explicit ack mode.
	if config.AckWait == 0 && (config.AckPolicy == AckExplicit || config.AckPolicy == AckAll) {
		config.AckWait = JsAckWaitDefault
	}
	// Setup default of -1, meaning no limit for MaxDeliver.
	if config.MaxDeliver == 0 {
		config.MaxDeliver = -1
	}
	// If BackOff was specified that will override the AckWait and the MaxDeliver.
	if len(config.BackOff) > 0 {
		config.AckWait = config.BackOff[0]
	}
	// Set proper default for max ack pending if we are ack explicit and none has been set.
	if (config.AckPolicy == AckExplicit || config.AckPolicy == AckAll) && config.MaxAckPending == 0 {
		accPending := JsDefaultMaxAckPending
		if lim.MaxAckPending > 0 && lim.MaxAckPending < accPending {
			accPending = lim.MaxAckPending
		}
		if accLim.MaxAckPending > 0 && accLim.MaxAckPending < accPending {
			accPending = accLim.MaxAckPending
		}
		config.MaxAckPending = accPending
	}
	// if applicable set max request batch size
	if config.DeliverSubject == _EMPTY_ && config.MaxRequestBatch == 0 && lim.MaxRequestBatch > 0 {
		config.MaxRequestBatch = lim.MaxRequestBatch
	}
}

// Check the consumer config. If we are recovering don't check filter subjects.
func checkConsumerCfg(
	config *ConsumerConfig,
	srvLim *JSLimitOpts,
	cfg *StreamConfig,
	acc *Account,
	accLim *JetStreamAccountLimits,
	isRecovering bool,
) *ApiError {

	// Check if replicas is defined but exceeds parent stream.
	if config.Replicas > 0 && config.Replicas > cfg.Replicas {
		return NewJSConsumerReplicasExceedsStreamError()
	}
	// Check that it is not negative
	if config.Replicas < 0 {
		return NewJSReplicasCountCannotBeNegativeError()
	}
	// If the stream is interest or workqueue retention make sure the replicas
	// match that of the stream. This is REQUIRED for now.
	if cfg.Retention == InterestPolicy || cfg.Retention == WorkQueuePolicy {
		// Only error here if not recovering.
		// We handle recovering in a different spot to allow consumer to come up
		// if previous version allowed it to be created. We do not want it to not come up.
		if !isRecovering && config.Replicas != 0 && config.Replicas != cfg.Replicas {
			return NewJSConsumerReplicasShouldMatchStreamError()
		}
	}

	// Check if we have a BackOff defined that MaxDeliver is within range etc.
	if lbo := len(config.BackOff); lbo > 0 && config.MaxDeliver <= lbo {
		return NewJSConsumerMaxDeliverBackoffError()
	}

	if len(config.Description) > JSMaxDescriptionLen {
		return NewJSConsumerDescriptionTooLongError(JSMaxDescriptionLen)
	}

	// For now expect a literal subject if its not empty. Empty means work queue mode (pull mode).
	if config.DeliverSubject != _EMPTY_ {
		if !subjectIsLiteral(config.DeliverSubject) {
			return NewJSConsumerDeliverToWildcardsError()
		}
		if !IsValidSubject(config.DeliverSubject) {
			return NewJSConsumerInvalidDeliverSubjectError()
		}
		if deliveryFormsCycle(cfg, config.DeliverSubject) {
			return NewJSConsumerDeliverCycleError()
		}
		if config.MaxWaiting != 0 {
			return NewJSConsumerPushMaxWaitingError()
		}
		if config.MaxAckPending > 0 && config.AckPolicy == AckNone {
			return NewJSConsumerMaxPendingAckPolicyRequiredError()
		}
		if config.Heartbeat > 0 && config.Heartbeat < 100*time.Millisecond {
			return NewJSConsumerSmallHeartbeatError()
		}
	} else {
		// Pull mode with work queue retention from the stream requires an explicit ack.
		if config.AckPolicy == AckNone && cfg.Retention == WorkQueuePolicy {
			return NewJSConsumerPullRequiresAckError()
		}
		if config.RateLimit > 0 {
			return NewJSConsumerPullWithRateLimitError()
		}
		if config.MaxWaiting < 0 {
			return NewJSConsumerMaxWaitingNegativeError()
		}
		if config.Heartbeat > 0 {
			return NewJSConsumerHBRequiresPushError()
		}
		if config.FlowControl {
			return NewJSConsumerFCRequiresPushError()
		}
		if config.MaxRequestBatch < 0 {
			return NewJSConsumerMaxRequestBatchNegativeError()
		}
		if config.MaxRequestExpires != 0 && config.MaxRequestExpires < time.Millisecond {
			return NewJSConsumerMaxRequestExpiresToSmallError()
		}
		if srvLim.MaxRequestBatch > 0 && config.MaxRequestBatch > srvLim.MaxRequestBatch {
			return NewJSConsumerMaxRequestBatchExceededError(srvLim.MaxRequestBatch)
		}
	}
	if srvLim.MaxAckPending > 0 && config.MaxAckPending > srvLim.MaxAckPending {
		return NewJSConsumerMaxPendingAckExcessError(srvLim.MaxAckPending)
	}
	if accLim.MaxAckPending > 0 && config.MaxAckPending > accLim.MaxAckPending {
		return NewJSConsumerMaxPendingAckExcessError(accLim.MaxAckPending)
	}

	// Direct need to be non-mapped ephemerals.
	if config.Direct {
		if config.DeliverSubject == _EMPTY_ {
			return NewJSConsumerDirectRequiresPushError()
		}
		if isDurableConsumer(config) {
			return NewJSConsumerDirectRequiresEphemeralError()
		}
	}

	// As best we can make sure the filtered subject is valid.
	if config.FilterSubject != _EMPTY_ {
		subjects := copyStrings(cfg.Subjects)
		// explicitly skip validFilteredSubject when recovering
		hasExt := isRecovering
		if !isRecovering {
			subjects, hasExt = gatherSourceMirrorSubjects(subjects, cfg, acc)
		}
		if !hasExt && !validFilteredSubject(config.FilterSubject, subjects) {
			return NewJSConsumerFilterNotSubsetError()
		}
	}

	// Helper function to formulate similar errors.
	badStart := func(dp, start string) error {
		return fmt.Errorf("consumer delivery policy is deliver %s, but optional start %s is also set", dp, start)
	}
	notSet := func(dp, notSet string) error {
		return fmt.Errorf("consumer delivery policy is deliver %s, but optional %s is not set", dp, notSet)
	}

	// Check on start position conflicts.
	switch config.DeliverPolicy {
	case DeliverAll:
		if config.OptStartSeq > 0 {
			return NewJSConsumerInvalidPolicyError(badStart("all", "sequence"))
		}
		if config.OptStartTime != nil {
			return NewJSConsumerInvalidPolicyError(badStart("all", "time"))
		}
	case DeliverLast:
		if config.OptStartSeq > 0 {
			return NewJSConsumerInvalidPolicyError(badStart("last", "sequence"))
		}
		if config.OptStartTime != nil {
			return NewJSConsumerInvalidPolicyError(badStart("last", "time"))
		}
	case DeliverLastPerSubject:
		if config.OptStartSeq > 0 {
			return NewJSConsumerInvalidPolicyError(badStart("last per subject", "sequence"))
		}
		if config.OptStartTime != nil {
			return NewJSConsumerInvalidPolicyError(badStart("last per subject", "time"))
		}
		if config.FilterSubject == _EMPTY_ {
			return NewJSConsumerInvalidPolicyError(notSet("last per subject", "filter subject"))
		}
	case DeliverNew:
		if config.OptStartSeq > 0 {
			return NewJSConsumerInvalidPolicyError(badStart("new", "sequence"))
		}
		if config.OptStartTime != nil {
			return NewJSConsumerInvalidPolicyError(badStart("new", "time"))
		}
	case DeliverByStartSequence:
		if config.OptStartSeq == 0 {
			return NewJSConsumerInvalidPolicyError(notSet("by start sequence", "start sequence"))
		}
		if config.OptStartTime != nil {
			return NewJSConsumerInvalidPolicyError(badStart("by start sequence", "time"))
		}
	case DeliverByStartTime:
		if config.OptStartTime == nil {
			return NewJSConsumerInvalidPolicyError(notSet("by start time", "start time"))
		}
		if config.OptStartSeq != 0 {
			return NewJSConsumerInvalidPolicyError(badStart("by start time", "start sequence"))
		}
	}

	if config.SampleFrequency != _EMPTY_ {
		s := strings.TrimSuffix(config.SampleFrequency, "%")
		if sampleFreq, err := strconv.Atoi(s); err != nil || sampleFreq < 0 {
			return NewJSConsumerInvalidSamplingError(err)
		}
	}

	// We reject if flow control is set without heartbeats.
	if config.FlowControl && config.Heartbeat == 0 {
		return NewJSConsumerWithFlowControlNeedsHeartbeatsError()
	}

	if config.Durable != _EMPTY_ && config.Name != _EMPTY_ {
		if config.Name != config.Durable {
			return NewJSConsumerCreateDurableAndNameMismatchError()
		}
	}

	return nil
}

func (mset *stream) addConsumer(config *ConsumerConfig) (*consumer, error) {
	return mset.addConsumerWithAssignment(config, _EMPTY_, nil, false)
}

func (mset *stream) addConsumerWithAssignment(config *ConsumerConfig, oname string, ca *consumerAssignment, isRecovering bool) (*consumer, error) {
	mset.mu.RLock()
	s, jsa, tierName, cfg, acc := mset.srv, mset.jsa, mset.tier, mset.cfg, mset.acc
	retention := cfg.Retention
	mset.mu.RUnlock()

	// If we do not have the consumer currently assigned to us in cluster mode we will proceed but warn.
	// This can happen on startup with restored state where on meta replay we still do not have
	// the assignment. Running in single server mode this always returns true.
	if oname != _EMPTY_ && !jsa.consumerAssigned(mset.name(), oname) {
		s.Debugf("Consumer %q > %q does not seem to be assigned to this server", mset.name(), oname)
	}

	if config == nil {
		return nil, NewJSConsumerConfigRequiredError()
	}

	jsa.usageMu.RLock()
	selectedLimits, limitsFound := jsa.limits[tierName]
	jsa.usageMu.RUnlock()
	if !limitsFound {
		return nil, NewJSNoLimitsError()
	}

	srvLim := &s.getOpts().JetStreamLimits
	// Make sure we have sane defaults.
	setConsumerConfigDefaults(config, srvLim, &selectedLimits)

	if err := checkConsumerCfg(config, srvLim, &cfg, acc, &selectedLimits, isRecovering); err != nil {
		return nil, err
	}

	sampleFreq := 0
	if config.SampleFrequency != _EMPTY_ {
		// Can't fail as checkConsumerCfg checks correct format
		sampleFreq, _ = strconv.Atoi(strings.TrimSuffix(config.SampleFrequency, "%"))
	}

	// Grab the client, account and server reference.
	c := mset.client
	if c == nil {
		return nil, NewJSStreamInvalidError()
	}
	var accName string
	c.mu.Lock()
	s, a := c.srv, c.acc
	if a != nil {
		accName = a.Name
	}
	c.mu.Unlock()

	// Hold mset lock here.
	mset.mu.Lock()
	if mset.client == nil || mset.store == nil || mset.consumers == nil {
		mset.mu.Unlock()
		return nil, errors.New("invalid stream")
	}

	// If this one is durable and already exists, we let that be ok as long as only updating what should be allowed.
	var cName string
	if isDurableConsumer(config) {
		cName = config.Durable
	} else if config.Name != _EMPTY_ {
		cName = config.Name
	}
	if cName != _EMPTY_ {
		if eo, ok := mset.consumers[cName]; ok {
			mset.mu.Unlock()
			err := eo.updateConfig(config)
			if err == nil {
				return eo, nil
			}
			return nil, NewJSConsumerCreateError(err, Unless(err))
		}
	}

	// Check for any limits, if the config for the consumer sets a limit we check against that
	// but if not we use the value from account limits, if account limits is more restrictive
	// than stream config we prefer the account limits to handle cases where account limits are
	// updated during the lifecycle of the stream
	maxc := mset.cfg.MaxConsumers
	if maxc <= 0 || (selectedLimits.MaxConsumers > 0 && selectedLimits.MaxConsumers < maxc) {
		maxc = selectedLimits.MaxConsumers
	}
	if maxc > 0 && mset.numPublicConsumers() >= maxc {
		mset.mu.Unlock()
		return nil, NewJSMaximumConsumersLimitError()
	}

	// Check on stream type conflicts with WorkQueues.
	if mset.cfg.Retention == WorkQueuePolicy && !config.Direct {
		// Force explicit acks here.
		if config.AckPolicy != AckExplicit {
			mset.mu.Unlock()
			return nil, NewJSConsumerWQRequiresExplicitAckError()
		}

		if len(mset.consumers) > 0 {
			if config.FilterSubject == _EMPTY_ {
				mset.mu.Unlock()
				return nil, NewJSConsumerWQMultipleUnfilteredError()
			} else if !mset.partitionUnique(config.FilterSubject) {
				// Prior to v2.9.7, on a stream with WorkQueue policy, the servers
				// were not catching the error of having multiple consumers with
				// overlapping filter subjects depending on the scope, for instance
				// creating "foo.*.bar" and then "foo.>" was not detected, while
				// "foo.>" and then "foo.*.bar" would have been. Failing here
				// in recovery mode would leave the rejected consumer in a bad state,
				// so we will simply warn here, asking the user to remove this
				// consumer administratively. Otherwise, if this is the creation
				// of a new consumer, we will return the error.
				if isRecovering {
					s.Warnf("Consumer %q > %q has a filter subject that overlaps "+
						"with other consumers, which is not allowed for a stream "+
						"with WorkQueue policy, it should be administratively deleted",
						cfg.Name, cName)
				} else {
					// We have a partition but it is not unique amongst the others.
					mset.mu.Unlock()
					return nil, NewJSConsumerWQConsumerNotUniqueError()
				}
			}
		}
		if config.DeliverPolicy != DeliverAll {
			mset.mu.Unlock()
			return nil, NewJSConsumerWQConsumerNotDeliverAllError()
		}
	}

	// Set name, which will be durable name if set, otherwise we create one at random.
	o := &consumer{
		mset:      mset,
		js:        s.getJetStream(),
		acc:       a,
		srv:       s,
		client:    s.createInternalJetStreamClient(),
		sysc:      s.createInternalJetStreamClient(),
		cfg:       *config,
		dsubj:     config.DeliverSubject,
		outq:      mset.outq,
		active:    true,
		qch:       make(chan struct{}),
		uch:       make(chan struct{}, 1),
		mch:       make(chan struct{}, 1),
		sfreq:     int32(sampleFreq),
		maxdc:     uint64(config.MaxDeliver),
		maxp:      config.MaxAckPending,
		retention: retention,
		created:   time.Now().UTC(),
	}

	// Bind internal client to the user account.
	o.client.registerWithAccount(a)
	// Bind to the system account.
	o.sysc.registerWithAccount(s.SystemAccount())

	if isDurableConsumer(config) {
		if len(config.Durable) > JSMaxNameLen {
			mset.mu.Unlock()
			o.deleteWithoutAdvisory()
			return nil, NewJSConsumerNameTooLongError(JSMaxNameLen)
		}
		o.name = config.Durable
	} else if oname != _EMPTY_ {
		o.name = oname
	} else {
		if config.Name != _EMPTY_ {
			o.name = config.Name
		} else {
			// Legacy ephemeral auto-generated.
			for {
				o.name = createConsumerName()
				if _, ok := mset.consumers[o.name]; !ok {
					break
				}
			}
			config.Name = o.name
		}
	}
	// Create ackMsgs queue now that we have a consumer name
	o.ackMsgs = newIPQueue[*jsAckMsg](s, fmt.Sprintf("[ACC:%s] consumer '%s' on stream '%s' ackMsgs", accName, o.name, mset.cfg.Name))

	// Create our request waiting queue.
	if o.isPullMode() {
		o.waiting = newWaitQueue(config.MaxWaiting)
		// Create our internal queue for next msg requests.
		o.nextMsgReqs = newIPQueue[*nextMsgReq](s, fmt.Sprintf("[ACC:%s] consumer '%s' on stream '%s' pull requests", accName, o.name, mset.cfg.Name))
	}

	// Check if we have  filtered subject that is a wildcard.
	if config.FilterSubject != _EMPTY_ && subjectHasWildcard(config.FilterSubject) {
		o.filterWC = true
	}

	// already under lock, mset.Name() would deadlock
	o.stream = mset.cfg.Name
	o.ackEventT = JSMetricConsumerAckPre + "." + o.stream + "." + o.name
	o.nakEventT = JSAdvisoryConsumerMsgNakPre + "." + o.stream + "." + o.name
	o.deliveryExcEventT = JSAdvisoryConsumerMaxDeliveryExceedPre + "." + o.stream + "." + o.name

	if !isValidName(o.name) {
		mset.mu.Unlock()
		o.deleteWithoutAdvisory()
		return nil, NewJSConsumerBadDurableNameError()
	}

	// Setup our storage if not a direct consumer.
	if !config.Direct {
		store, err := mset.store.ConsumerStore(o.name, config)
		if err != nil {
			mset.mu.Unlock()
			o.deleteWithoutAdvisory()
			return nil, NewJSConsumerStoreFailedError(err)
		}
		o.store = store
	}

	if o.store != nil && o.store.HasState() {
		// Restore our saved state.
		o.mu.Lock()
		o.readStoredState(0)
		o.mu.Unlock()
	} else {
		// Select starting sequence number
		o.selectStartingSeqNo()
	}

	// Now register with mset and create the ack subscription.
	// Check if we already have this one registered.
	if eo, ok := mset.consumers[o.name]; ok {
		mset.mu.Unlock()
		if !o.isDurable() || !o.isPushMode() {
			o.name = _EMPTY_ // Prevent removal since same name.
			o.deleteWithoutAdvisory()
			return nil, NewJSConsumerNameExistError()
		}
		// If we are here we have already registered this durable. If it is still active that is an error.
		if eo.isActive() {
			o.name = _EMPTY_ // Prevent removal since same name.
			o.deleteWithoutAdvisory()
			return nil, NewJSConsumerExistingActiveError()
		}
		// Since we are here this means we have a potentially new durable so we should update here.
		// Check that configs are the same.
		if !configsEqualSansDelivery(o.cfg, eo.cfg) {
			o.name = _EMPTY_ // Prevent removal since same name.
			o.deleteWithoutAdvisory()
			return nil, NewJSConsumerReplacementWithDifferentNameError()
		}
		// Once we are here we have a replacement push-based durable.
		eo.updateDeliverSubject(o.cfg.DeliverSubject)
		return eo, nil
	}

	// Set up the ack subscription for this consumer. Will use wildcard for all acks.
	// We will remember the template to generate replies with sequence numbers and use
	// that to scanf them back in.
	mn := mset.cfg.Name
	pre := fmt.Sprintf(jsAckT, mn, o.name)
	o.ackReplyT = fmt.Sprintf("%s.%%d.%%d.%%d.%%d.%%d", pre)
	o.ackSubj = fmt.Sprintf("%s.*.*.*.*.*", pre)
	o.nextMsgSubj = fmt.Sprintf(JSApiRequestNextT, mn, o.name)

	// Check/update the inactive threshold
	o.updateInactiveThreshold(&o.cfg)

	if o.isPushMode() {
		// Check if we are running only 1 replica and that the delivery subject has interest.
		// Check in place here for interest. Will setup properly in setLeader.
		if config.replicas(&mset.cfg) == 1 {
			r := o.acc.sl.Match(o.cfg.DeliverSubject)
			if !o.hasDeliveryInterest(len(r.psubs)+len(r.qsubs) > 0) {
				// Let the interest come to us eventually, but setup delete timer.
				o.updateDeliveryInterest(false)
			}
		}
	}

	// Set our ca.
	if ca != nil {
		o.setConsumerAssignment(ca)
	}

	// Check if we have a rate limit set.
	if config.RateLimit != 0 {
		o.setRateLimit(config.RateLimit)
	}

	mset.setConsumer(o)
	mset.mu.Unlock()

	if config.Direct || (!s.JetStreamIsClustered() && s.standAloneMode()) {
		o.setLeader(true)
	}

	// This is always true in single server mode.
	if o.IsLeader() {
		// Send advisory.
		var suppress bool
		if !s.standAloneMode() && ca == nil {
			suppress = true
		} else if ca != nil {
			suppress = ca.responded
		}
		if !suppress {
			o.sendCreateAdvisory()
		}
	}

	return o, nil
}

// Updates the consumer `dthresh` delete timer duration and set
// cfg.InactiveThreshold to JsDeleteWaitTimeDefault for ephemerals
// if not explicitly already specified by the user.
// Lock should be held.
func (o *consumer) updateInactiveThreshold(cfg *ConsumerConfig) {
	// Ephemerals will always have inactive thresholds.
	if !o.isDurable() && cfg.InactiveThreshold <= 0 {
		// Add in 1 sec of jitter above and beyond the default of 5s.
		o.dthresh = JsDeleteWaitTimeDefault + 100*time.Millisecond + time.Duration(rand.Int63n(900))*time.Millisecond
		// Only stamp config with default sans jitter.
		cfg.InactiveThreshold = JsDeleteWaitTimeDefault
	} else if cfg.InactiveThreshold > 0 {
		// Add in up to 1 sec of jitter if pull mode.
		if o.isPullMode() {
			o.dthresh = cfg.InactiveThreshold + 100*time.Millisecond + time.Duration(rand.Int63n(900))*time.Millisecond
		} else {
			o.dthresh = cfg.InactiveThreshold
		}
	} else if cfg.InactiveThreshold <= 0 {
		// We accept InactiveThreshold be set to 0 (for durables)
		o.dthresh = 0
	}
}

func (o *consumer) consumerAssignment() *consumerAssignment {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.ca
}

func (o *consumer) setConsumerAssignment(ca *consumerAssignment) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.ca = ca
	if ca == nil {
		return
	}
	// Set our node.
	o.node = ca.Group.node

	// Trigger update chan.
	select {
	case o.uch <- struct{}{}:
	default:
	}
}

func (o *consumer) updateC() <-chan struct{} {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.uch
}

// checkQueueInterest will check on our interest's queue group status.
// Lock should be held.
func (o *consumer) checkQueueInterest() {
	if !o.active || o.cfg.DeliverSubject == _EMPTY_ {
		return
	}
	subj := o.dsubj
	if subj == _EMPTY_ {
		subj = o.cfg.DeliverSubject
	}

	if rr := o.acc.sl.Match(subj); len(rr.qsubs) > 0 {
		// Just grab first
		if qsubs := rr.qsubs[0]; len(qsubs) > 0 {
			if sub := rr.qsubs[0][0]; len(sub.queue) > 0 {
				o.qgroup = string(sub.queue)
			}
		}
	}
}

// clears our node if we have one. When we scale down to 1.
func (o *consumer) clearNode() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.node != nil {
		o.node.Delete()
		o.node = nil
	}
}

// IsLeader will return if we are the current leader.
func (o *consumer) IsLeader() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.isLeader()
}

// Lock should be held.
func (o *consumer) isLeader() bool {
	if o.node != nil {
		return o.node.Leader()
	}
	return true
}

func (o *consumer) setLeader(isLeader bool) {
	o.mu.RLock()
	mset := o.mset
	isRunning := o.ackSub != nil
	o.mu.RUnlock()

	// If we are here we have a change in leader status.
	if isLeader {
		if mset == nil {
			return
		}
		if isRunning {
			// If we detect we are scaling up, make sure to create clustered routines and channels.
			o.mu.Lock()
			if o.node != nil && o.pch == nil {
				// We are moving from R1 to clustered.
				o.pch = make(chan struct{}, 1)
				go o.loopAndForwardProposals(o.qch)
				if o.phead != nil {
					select {
					case o.pch <- struct{}{}:
					default:
					}
				}
			}
			o.mu.Unlock()
			return
		}

		mset.mu.RLock()
		s, jsa, stream, lseq := mset.srv, mset.jsa, mset.cfg.Name, mset.lseq
		mset.mu.RUnlock()

		// Register as a leader with our parent stream.
		mset.setConsumerAsLeader(o)

		o.mu.Lock()
		o.rdq, o.rdqi = nil, nil

		// Restore our saved state. During non-leader status we just update our underlying store.
		o.readStoredState(lseq)

		// Setup initial num pending.
		o.streamNumPending()

		// Cleanup lss when we take over in clustered mode.
		if o.hasSkipListPending() && o.sseq >= o.lss.resume {
			o.lss = nil
		}

		// Update the group on the our starting sequence if we are starting but we skipped some in the stream.
		if o.dseq == 1 && o.sseq > 1 {
			o.updateSkipped()
		}

		// Do info sub.
		if o.infoSub == nil && jsa != nil {
			isubj := fmt.Sprintf(clusterConsumerInfoT, jsa.acc(), stream, o.name)
			// Note below the way we subscribe here is so that we can send requests to ourselves.
			o.infoSub, _ = s.systemSubscribe(isubj, _EMPTY_, false, o.sysc, o.handleClusterConsumerInfoRequest)
		}

		var err error
		if o.ackSub, err = o.subscribeInternal(o.ackSubj, o.pushAck); err != nil {
			o.mu.Unlock()
			o.deleteWithoutAdvisory()
			return
		}

		// Setup the internal sub for next message requests regardless.
		// Will error if wrong mode to provide feedback to users.
		if o.reqSub, err = o.subscribeInternal(o.nextMsgSubj, o.processNextMsgReq); err != nil {
			o.mu.Unlock()
			o.deleteWithoutAdvisory()
			return
		}

		// Check on flow control settings.
		if o.cfg.FlowControl {
			o.setMaxPendingBytes(JsFlowControlMaxPending)
			fcsubj := fmt.Sprintf(jsFlowControl, stream, o.name)
			if o.fcSub, err = o.subscribeInternal(fcsubj, o.processFlowControl); err != nil {
				o.mu.Unlock()
				o.deleteWithoutAdvisory()
				return
			}
		}

		// If push mode, register for notifications on interest.
		if o.isPushMode() {
			o.inch = make(chan bool, 8)
			o.acc.sl.registerNotification(o.cfg.DeliverSubject, o.cfg.DeliverGroup, o.inch)
			if o.active = <-o.inch; o.active {
				o.checkQueueInterest()
			}

			// Check gateways in case they are enabled.
			if s.gateway.enabled {
				if !o.active {
					o.active = s.hasGatewayInterest(o.acc.Name, o.cfg.DeliverSubject)
				}
				stopAndClearTimer(&o.gwdtmr)
				o.gwdtmr = time.AfterFunc(time.Second, func() { o.watchGWinterest() })
			}
		}

		if o.dthresh > 0 && (o.isPullMode() || !o.active) {
			// Pull consumer. We run the dtmr all the time for this one.
			stopAndClearTimer(&o.dtmr)
			o.dtmr = time.AfterFunc(o.dthresh, func() { o.deleteNotActive() })
		}

		// If we are not in ReplayInstant mode mark us as in replay state until resolved.
		if o.cfg.ReplayPolicy != ReplayInstant {
			o.replay = true
		}

		// Recreate quit channel.
		o.qch = make(chan struct{})
		qch := o.qch
		node := o.node
		if node != nil && o.pch == nil {
			o.pch = make(chan struct{}, 1)
		}
		pullMode := o.isPullMode()
		o.mu.Unlock()

		// Snapshot initial info.
		o.infoWithSnap(true)

		// Now start up Go routine to deliver msgs.
		go o.loopAndGatherMsgs(qch)

		// Now start up Go routine to process acks.
		go o.processInboundAcks(qch)

		if pullMode {
			// Now start up Go routine to process inbound next message requests.
			go o.processInboundNextMsgReqs(qch)

		}

		// If we are R>1 spin up our proposal loop.
		if node != nil {
			// Determine if we can send pending requests info to the group.
			// They must be on server versions >= 2.7.1
			o.checkAndSetPendingRequestsOk()
			o.checkPendingRequests()
			go o.loopAndForwardProposals(qch)
		}

	} else {
		// Shutdown the go routines and the subscriptions.
		o.mu.Lock()
		if o.qch != nil {
			close(o.qch)
			o.qch = nil
		}
		// Make sure to clear out any re delivery queues
		stopAndClearTimer(&o.ptmr)
		o.rdq, o.rdqi = nil, nil
		o.pending = nil
		// ok if they are nil, we protect inside unsubscribe()
		o.unsubscribe(o.ackSub)
		o.unsubscribe(o.reqSub)
		o.unsubscribe(o.fcSub)
		o.ackSub, o.reqSub, o.fcSub = nil, nil, nil
		if o.infoSub != nil {
			o.srv.sysUnsubscribe(o.infoSub)
			o.infoSub = nil
		}
		// Reset waiting if we are in pull mode.
		if o.isPullMode() {
			o.waiting = newWaitQueue(o.cfg.MaxWaiting)
			if !o.isDurable() {
				stopAndClearTimer(&o.dtmr)
			}
			o.nextMsgReqs.drain()
		} else if o.srv.gateway.enabled {
			stopAndClearTimer(&o.gwdtmr)
		}
		o.mu.Unlock()

		// Unregister as a leader with our parent stream.
		if mset != nil {
			mset.removeConsumerAsLeader(o)
		}
	}
}

// This is coming on the wire so do not block here.
func (o *consumer) handleClusterConsumerInfoRequest(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
	go o.infoWithSnapAndReply(false, reply)
}

// Lock should be held.
func (o *consumer) subscribeInternal(subject string, cb msgHandler) (*subscription, error) {
	c := o.client
	if c == nil {
		return nil, fmt.Errorf("invalid consumer")
	}
	if !c.srv.EventsEnabled() {
		return nil, ErrNoSysAccount
	}
	if cb == nil {
		return nil, fmt.Errorf("undefined message handler")
	}

	o.sid++

	// Now create the subscription
	return c.processSub([]byte(subject), nil, []byte(strconv.Itoa(o.sid)), cb, false)
}

// Unsubscribe from our subscription.
// Lock should be held.
func (o *consumer) unsubscribe(sub *subscription) {
	if sub == nil || o.client == nil {
		return
	}
	o.client.processUnsub(sub.sid)
}

// We need to make sure we protect access to the outq.
// Do all advisory sends here.
func (o *consumer) sendAdvisory(subj string, msg []byte) {
	o.outq.sendMsg(subj, msg)
}

func (o *consumer) sendDeleteAdvisoryLocked() {
	e := JSConsumerActionAdvisory{
		TypedEvent: TypedEvent{
			Type: JSConsumerActionAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   o.stream,
		Consumer: o.name,
		Action:   DeleteEvent,
		Domain:   o.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(e)
	if err != nil {
		return
	}

	subj := JSAdvisoryConsumerDeletedPre + "." + o.stream + "." + o.name
	o.sendAdvisory(subj, j)
}

func (o *consumer) sendCreateAdvisory() {
	o.mu.Lock()
	defer o.mu.Unlock()

	e := JSConsumerActionAdvisory{
		TypedEvent: TypedEvent{
			Type: JSConsumerActionAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   o.stream,
		Consumer: o.name,
		Action:   CreateEvent,
		Domain:   o.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(e)
	if err != nil {
		return
	}

	subj := JSAdvisoryConsumerCreatedPre + "." + o.stream + "." + o.name
	o.sendAdvisory(subj, j)
}

// Created returns created time.
func (o *consumer) createdTime() time.Time {
	o.mu.Lock()
	created := o.created
	o.mu.Unlock()
	return created
}

// Internal to allow creation time to be restored.
func (o *consumer) setCreatedTime(created time.Time) {
	o.mu.Lock()
	o.created = created
	o.mu.Unlock()
}

// This will check for extended interest in a subject. If we have local interest we just return
// that, but in the absence of local interest and presence of gateways or service imports we need
// to check those as well.
func (o *consumer) hasDeliveryInterest(localInterest bool) bool {
	o.mu.Lock()
	mset := o.mset
	if mset == nil {
		o.mu.Unlock()
		return false
	}
	acc := o.acc
	deliver := o.cfg.DeliverSubject
	o.mu.Unlock()

	if localInterest {
		return true
	}

	// If we are here check gateways.
	if s := acc.srv; s != nil && s.hasGatewayInterest(acc.Name, deliver) {
		return true
	}
	return false
}

func (s *Server) hasGatewayInterest(account, subject string) bool {
	gw := s.gateway
	if !gw.enabled {
		return false
	}
	gw.RLock()
	defer gw.RUnlock()
	for _, gwc := range gw.outo {
		psi, qr := gwc.gatewayInterest(account, subject)
		if psi || qr != nil {
			return true
		}
	}
	return false
}

// This processes an update to the local interest for a deliver subject.
func (o *consumer) updateDeliveryInterest(localInterest bool) bool {
	interest := o.hasDeliveryInterest(localInterest)

	o.mu.Lock()
	defer o.mu.Unlock()

	mset := o.mset
	if mset == nil || o.isPullMode() {
		return false
	}

	if interest && !o.active {
		o.signalNewMessages()
	}
	// Update active status, if not active clear any queue group we captured.
	if o.active = interest; !o.active {
		o.qgroup = _EMPTY_
	} else {
		o.checkQueueInterest()
	}

	// If the delete timer has already been set do not clear here and return.
	// Note that durable can now have an inactive threshold, so don't check
	// for durable status, instead check for dthresh > 0.
	if o.dtmr != nil && o.dthresh > 0 && !interest {
		return true
	}

	// Stop and clear the delete timer always.
	stopAndClearTimer(&o.dtmr)

	// If we do not have interest anymore and have a delete threshold set, then set
	// a timer to delete us. We wait for a bit in case of server reconnect.
	if !interest && o.dthresh > 0 {
		o.dtmr = time.AfterFunc(o.dthresh, func() { o.deleteNotActive() })
		return true
	}
	return false
}

func (o *consumer) deleteNotActive() {
	o.mu.Lock()
	if o.mset == nil {
		o.mu.Unlock()
		return
	}
	// Push mode just look at active.
	if o.isPushMode() {
		// If we are active simply return.
		if o.active {
			o.mu.Unlock()
			return
		}
	} else {
		// Pull mode.
		elapsed := time.Since(o.waiting.last)
		if elapsed <= o.cfg.InactiveThreshold {
			// These need to keep firing so reset but use delta.
			if o.dtmr != nil {
				o.dtmr.Reset(o.dthresh - elapsed)
			} else {
				o.dtmr = time.AfterFunc(o.dthresh-elapsed, func() { o.deleteNotActive() })
			}
			o.mu.Unlock()
			return
		}
		// Check if we still have valid requests waiting.
		if o.checkWaitingForInterest() {
			if o.dtmr != nil {
				o.dtmr.Reset(o.dthresh)
			} else {
				o.dtmr = time.AfterFunc(o.dthresh, func() { o.deleteNotActive() })
			}
			o.mu.Unlock()
			return
		}
	}

	s, js := o.mset.srv, o.mset.srv.js
	acc, stream, name, isDirect := o.acc.Name, o.stream, o.name, o.cfg.Direct
	o.mu.Unlock()

	// If we are clustered, check if we still have this consumer assigned.
	// If we do forward a proposal to delete ourselves to the metacontroller leader.
	if !isDirect && s.JetStreamIsClustered() {
		js.mu.RLock()
		ca, cc := js.consumerAssignment(acc, stream, name), js.cluster
		js.mu.RUnlock()

		if ca != nil && cc != nil {
			cca := *ca
			cca.Reply = _EMPTY_
			meta, removeEntry := cc.meta, encodeDeleteConsumerAssignment(&cca)
			meta.ForwardProposal(removeEntry)

			// Check to make sure we went away.
			// Don't think this needs to be a monitored go routine.
			go func() {
				ticker := time.NewTicker(10 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					js.mu.RLock()
					ca := js.consumerAssignment(acc, stream, name)
					js.mu.RUnlock()
					if ca != nil {
						s.Warnf("Consumer assignment for '%s > %s > %s' not cleaned up, retrying", acc, stream, name)
						meta.ForwardProposal(removeEntry)
					} else {
						return
					}
				}
			}()
		}
	}

	// We will delete here regardless.
	o.delete()
}

func (o *consumer) watchGWinterest() {
	pa := o.isActive()
	// If there is no local interest...
	if o.hasNoLocalInterest() {
		o.updateDeliveryInterest(false)
		if !pa && o.isActive() {
			o.signalNewMessages()
		}
	}

	// We want this to always be running so we can also pick up on interest returning.
	o.mu.Lock()
	if o.gwdtmr != nil {
		o.gwdtmr.Reset(time.Second)
	} else {
		stopAndClearTimer(&o.gwdtmr)
		o.gwdtmr = time.AfterFunc(time.Second, func() { o.watchGWinterest() })
	}
	o.mu.Unlock()
}

// Config returns the consumer's configuration.
func (o *consumer) config() ConsumerConfig {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cfg
}

// Force expiration of all pending.
// Lock should be held.
func (o *consumer) forceExpirePending() {
	var expired []uint64
	for seq := range o.pending {
		if !o.onRedeliverQueue(seq) {
			expired = append(expired, seq)
		}
	}
	if len(expired) > 0 {
		sort.Slice(expired, func(i, j int) bool { return expired[i] < expired[j] })
		o.addToRedeliverQueue(expired...)
		// Now we should update the timestamp here since we are redelivering.
		// We will use an incrementing time to preserve order for any other redelivery.
		off := time.Now().UnixNano() - o.pending[expired[0]].Timestamp
		for _, seq := range expired {
			if p, ok := o.pending[seq]; ok && p != nil {
				p.Timestamp += off
			}
		}
		o.ptmr.Reset(o.ackWait(0))
	}
	o.signalNewMessages()
}

// Acquire proper locks and update rate limit.
// Will use what is in config.
func (o *consumer) setRateLimitNeedsLocks() {
	o.mu.RLock()
	mset := o.mset
	o.mu.RUnlock()

	if mset == nil {
		return
	}

	mset.mu.RLock()
	o.mu.Lock()
	o.setRateLimit(o.cfg.RateLimit)
	o.mu.Unlock()
	mset.mu.RUnlock()
}

// Set the rate limiter
// Both mset and consumer lock should be held.
func (o *consumer) setRateLimit(bps uint64) {
	if bps == 0 {
		o.rlimit = nil
		return
	}

	// TODO(dlc) - Make sane values or error if not sane?
	// We are configured in bits per sec so adjust to bytes.
	rl := rate.Limit(bps / 8)
	mset := o.mset

	// Burst should be set to maximum msg size for this account, etc.
	var burst int
	if mset.cfg.MaxMsgSize > 0 {
		burst = int(mset.cfg.MaxMsgSize)
	} else if mset.jsa.account.limits.mpay > 0 {
		burst = int(mset.jsa.account.limits.mpay)
	} else {
		s := mset.jsa.account.srv
		burst = int(s.getOpts().MaxPayload)
	}

	o.rlimit = rate.NewLimiter(rl, burst)
}

// Check if new consumer config allowed vs old.
func (acc *Account) checkNewConsumerConfig(cfg, ncfg *ConsumerConfig) error {
	if reflect.DeepEqual(cfg, ncfg) {
		return nil
	}
	// Something different, so check since we only allow certain things to be updated.
	if cfg.DeliverPolicy != ncfg.DeliverPolicy {
		return errors.New("deliver policy can not be updated")
	}
	if cfg.OptStartSeq != ncfg.OptStartSeq {
		return errors.New("start sequence can not be updated")
	}
	if cfg.OptStartTime != ncfg.OptStartTime {
		return errors.New("start time can not be updated")
	}
	if cfg.AckPolicy != ncfg.AckPolicy {
		return errors.New("ack policy can not be updated")
	}
	if cfg.ReplayPolicy != ncfg.ReplayPolicy {
		return errors.New("replay policy can not be updated")
	}
	if cfg.Heartbeat != ncfg.Heartbeat {
		return errors.New("heart beats can not be updated")
	}
	if cfg.FlowControl != ncfg.FlowControl {
		return errors.New("flow control can not be updated")
	}
	if cfg.MaxWaiting != ncfg.MaxWaiting {
		return errors.New("max waiting can not be updated")
	}

	// Deliver Subject is conditional on if its bound.
	if cfg.DeliverSubject != ncfg.DeliverSubject {
		if cfg.DeliverSubject == _EMPTY_ {
			return errors.New("can not update pull consumer to push based")
		}
		if ncfg.DeliverSubject == _EMPTY_ {
			return errors.New("can not update push consumer to pull based")
		}
		rr := acc.sl.Match(cfg.DeliverSubject)
		if len(rr.psubs)+len(rr.qsubs) != 0 {
			return NewJSConsumerNameExistError()
		}
	}

	// Check if BackOff is defined, MaxDeliver is within range.
	if lbo := len(ncfg.BackOff); lbo > 0 && ncfg.MaxDeliver <= lbo {
		return NewJSConsumerMaxDeliverBackoffError()
	}

	return nil
}

// Update the config based on the new config, or error if update not allowed.
func (o *consumer) updateConfig(cfg *ConsumerConfig) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.acc.checkNewConsumerConfig(&o.cfg, cfg); err != nil {
		return err
	}

	if o.store != nil {
		// Update local state always.
		if err := o.store.UpdateConfig(cfg); err != nil {
			return err
		}
	}

	// DeliverSubject
	if cfg.DeliverSubject != o.cfg.DeliverSubject {
		o.updateDeliverSubjectLocked(cfg.DeliverSubject)
	}

	// MaxAckPending
	if cfg.MaxAckPending != o.cfg.MaxAckPending {
		o.maxp = cfg.MaxAckPending
		o.signalNewMessages()
	}
	// AckWait
	if cfg.AckWait != o.cfg.AckWait {
		if o.ptmr != nil {
			o.ptmr.Reset(100 * time.Millisecond)
		}
	}
	// Rate Limit
	if cfg.RateLimit != o.cfg.RateLimit {
		// We need both locks here so do in Go routine.
		go o.setRateLimitNeedsLocks()
	}
	if cfg.SampleFrequency != o.cfg.SampleFrequency {
		s := strings.TrimSuffix(cfg.SampleFrequency, "%")
		// String has been already verified for validity up in the stack, so no
		// need to check for error here.
		sampleFreq, _ := strconv.Atoi(s)
		o.sfreq = int32(sampleFreq)
	}
	// Set MaxDeliver if changed
	if cfg.MaxDeliver != o.cfg.MaxDeliver {
		o.maxdc = uint64(cfg.MaxDeliver)
	}
	// Set InactiveThreshold if changed.
	if val := cfg.InactiveThreshold; val != o.cfg.InactiveThreshold {
		o.updateInactiveThreshold(cfg)
		stopAndClearTimer(&o.dtmr)
		// Restart timer only if we are the leader.
		if o.isLeader() && o.dthresh > 0 {
			o.dtmr = time.AfterFunc(o.dthresh, func() { o.deleteNotActive() })
		}
	}

	if o.cfg.FilterSubject != cfg.FilterSubject {
		if cfg.FilterSubject != _EMPTY_ {
			o.filterWC = subjectHasWildcard(cfg.FilterSubject)
		} else {
			o.filterWC = false
		}
		// Make sure we have correct signaling setup.
		// Consumer lock can not be held.
		mset := o.mset
		o.mu.Unlock()
		mset.swapSigSubs(o, cfg.FilterSubject)
		o.mu.Lock()
	}

	// Record new config for others that do not need special handling.
	// Allowed but considered no-op, [Description, SampleFrequency, MaxWaiting, HeadersOnly]
	o.cfg = *cfg

	// Re-calculate num pending on update.
	o.streamNumPending()

	return nil
}

// This is a config change for the delivery subject for a
// push based consumer.
func (o *consumer) updateDeliverSubject(newDeliver string) {
	// Update the config and the dsubj
	o.mu.Lock()
	defer o.mu.Unlock()
	o.updateDeliverSubjectLocked(newDeliver)
}

// This is a config change for the delivery subject for a
// push based consumer.
func (o *consumer) updateDeliverSubjectLocked(newDeliver string) {
	if o.closed || o.isPullMode() || o.cfg.DeliverSubject == newDeliver {
		return
	}

	// Force redeliver of all pending on change of delivery subject.
	if len(o.pending) > 0 {
		o.forceExpirePending()
	}

	o.acc.sl.clearNotification(o.dsubj, o.cfg.DeliverGroup, o.inch)
	o.dsubj, o.cfg.DeliverSubject = newDeliver, newDeliver
	// When we register new one it will deliver to update state loop.
	o.acc.sl.registerNotification(newDeliver, o.cfg.DeliverGroup, o.inch)
}

// Check that configs are equal but allow delivery subjects to be different.
func configsEqualSansDelivery(a, b ConsumerConfig) bool {
	// These were copied in so can set Delivery here.
	a.DeliverSubject, b.DeliverSubject = _EMPTY_, _EMPTY_
	return reflect.DeepEqual(a, b)
}

// Helper to send a reply to an ack.
func (o *consumer) sendAckReply(subj string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sendAdvisory(subj, nil)
}

type jsAckMsg struct {
	subject string
	reply   string
	hdr     int
	msg     []byte
}

var jsAckMsgPool sync.Pool

func newJSAckMsg(subj, reply string, hdr int, msg []byte) *jsAckMsg {
	var m *jsAckMsg
	am := jsAckMsgPool.Get()
	if am != nil {
		m = am.(*jsAckMsg)
	} else {
		m = &jsAckMsg{}
	}
	// When getting something from a pool it is criticical that all fields are
	// initialized. Doing this way guarantees that if someone adds a field to
	// the structure, the compiler will fail the build if this line is not updated.
	(*m) = jsAckMsg{subj, reply, hdr, msg}
	return m
}

func (am *jsAckMsg) returnToPool() {
	if am == nil {
		return
	}
	am.subject, am.reply, am.hdr, am.msg = _EMPTY_, _EMPTY_, -1, nil
	jsAckMsgPool.Put(am)
}

// Push the ack message to the consumer's ackMsgs queue
func (o *consumer) pushAck(_ *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
	atomic.AddInt64(&o.awl, 1)
	o.ackMsgs.push(newJSAckMsg(subject, reply, c.pa.hdr, copyBytes(rmsg)))
}

// Processes a message for the ack reply subject delivered with a message.
func (o *consumer) processAck(subject, reply string, hdr int, rmsg []byte) {
	defer atomic.AddInt64(&o.awl, -1)

	var msg []byte
	if hdr > 0 {
		msg = rmsg[hdr:]
	} else {
		msg = rmsg
	}

	sseq, dseq, dc := ackReplyInfo(subject)

	skipAckReply := sseq == 0

	switch {
	case len(msg) == 0, bytes.Equal(msg, AckAck), bytes.Equal(msg, AckOK):
		o.processAckMsg(sseq, dseq, dc, true)
	case bytes.HasPrefix(msg, AckNext):
		o.processAckMsg(sseq, dseq, dc, true)
		o.processNextMsgRequest(reply, msg[len(AckNext):])
		skipAckReply = true
	case bytes.HasPrefix(msg, AckNak):
		o.processNak(sseq, dseq, dc, msg)
	case bytes.Equal(msg, AckProgress):
		o.progressUpdate(sseq)
	case bytes.Equal(msg, AckTerm):
		o.processTerm(sseq, dseq, dc)
	}

	// Ack the ack if requested.
	if len(reply) > 0 && !skipAckReply {
		o.sendAckReply(reply)
	}
}

// Used to process a working update to delay redelivery.
func (o *consumer) progressUpdate(seq uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if p, ok := o.pending[seq]; ok {
		p.Timestamp = time.Now().UnixNano()
		// Update store system.
		o.updateDelivered(p.Sequence, seq, 1, p.Timestamp)
	}
}

// Lock should be held.
func (o *consumer) updateSkipped() {
	// Clustered mode and R>1 only.
	if o.node == nil || !o.isLeader() {
		return
	}
	var b [1 + 8]byte
	b[0] = byte(updateSkipOp)
	var le = binary.LittleEndian
	le.PutUint64(b[1:], o.sseq)
	o.propose(b[:])
}

func (o *consumer) loopAndForwardProposals(qch chan struct{}) {
	o.mu.RLock()
	node, pch := o.node, o.pch
	o.mu.RUnlock()

	if node == nil || pch == nil {
		return
	}

	forwardProposals := func() {
		o.mu.Lock()
		proposal := o.phead
		o.phead, o.ptail = nil, nil
		o.mu.Unlock()
		// 256k max for now per batch.
		const maxBatch = 256 * 1024
		var entries []*Entry
		for sz := 0; proposal != nil; proposal = proposal.next {
			entries = append(entries, &Entry{EntryNormal, proposal.data})
			sz += len(proposal.data)
			if sz > maxBatch {
				node.ProposeDirect(entries)
				// We need to re-create `entries` because there is a reference
				// to it in the node's pae map.
				sz, entries = 0, nil
			}
		}
		if len(entries) > 0 {
			node.ProposeDirect(entries)
		}
	}

	// In case we have anything pending on entry.
	forwardProposals()

	for {
		select {
		case <-qch:
			forwardProposals()
			return
		case <-pch:
			forwardProposals()
		}
	}
}

// Lock should be held.
func (o *consumer) propose(entry []byte) {
	var notify bool
	p := &proposal{data: entry}
	if o.phead == nil {
		o.phead = p
		notify = true
	} else {
		o.ptail.next = p
	}
	o.ptail = p

	// Kick our looper routine if needed.
	if notify {
		select {
		case o.pch <- struct{}{}:
		default:
		}
	}
}

// Lock should be held.
func (o *consumer) updateDelivered(dseq, sseq, dc uint64, ts int64) {
	// Clustered mode and R>1.
	if o.node != nil {
		// Inline for now, use variable compression.
		var b [4*binary.MaxVarintLen64 + 1]byte
		b[0] = byte(updateDeliveredOp)
		n := 1
		n += binary.PutUvarint(b[n:], dseq)
		n += binary.PutUvarint(b[n:], sseq)
		n += binary.PutUvarint(b[n:], dc)
		n += binary.PutVarint(b[n:], ts)
		o.propose(b[:n])
	}
	if o.store != nil {
		// Update local state always.
		o.store.UpdateDelivered(dseq, sseq, dc, ts)
	}
	// Update activity.
	o.ldt = time.Now()
}

// Lock should be held.
func (o *consumer) updateAcks(dseq, sseq uint64) {
	if o.node != nil {
		// Inline for now, use variable compression.
		var b [2*binary.MaxVarintLen64 + 1]byte
		b[0] = byte(updateAcksOp)
		n := 1
		n += binary.PutUvarint(b[n:], dseq)
		n += binary.PutUvarint(b[n:], sseq)
		o.propose(b[:n])
	} else if o.store != nil {
		o.store.UpdateAcks(dseq, sseq)
	}
	// Update activity.
	o.lat = time.Now()
}

// Communicate to the cluster an addition of a pending request.
// Lock should be held.
func (o *consumer) addClusterPendingRequest(reply string) {
	if o.node == nil || !o.pendingRequestsOk() {
		return
	}
	b := make([]byte, len(reply)+1)
	b[0] = byte(addPendingRequest)
	copy(b[1:], reply)
	o.propose(b)
}

// Communicate to the cluster a removal of a pending request.
// Lock should be held.
func (o *consumer) removeClusterPendingRequest(reply string) {
	if o.node == nil || !o.pendingRequestsOk() {
		return
	}
	b := make([]byte, len(reply)+1)
	b[0] = byte(removePendingRequest)
	copy(b[1:], reply)
	o.propose(b)
}

// Set whether or not we can send pending requests to followers.
func (o *consumer) setPendingRequestsOk(ok bool) {
	o.mu.Lock()
	o.prOk = ok
	o.mu.Unlock()
}

// Lock should be held.
func (o *consumer) pendingRequestsOk() bool {
	return o.prOk
}

// Set whether or not we can send info about pending pull requests to our group.
// Will require all peers have a minimum version.
func (o *consumer) checkAndSetPendingRequestsOk() {
	o.mu.RLock()
	s, isValid := o.srv, o.mset != nil
	o.mu.RUnlock()
	if !isValid {
		return
	}

	if ca := o.consumerAssignment(); ca != nil && len(ca.Group.Peers) > 1 {
		for _, pn := range ca.Group.Peers {
			if si, ok := s.nodeToInfo.Load(pn); ok {
				if !versionAtLeast(si.(nodeInfo).version, 2, 7, 1) {
					// We expect all of our peers to eventually be up to date.
					// So check again in awhile.
					time.AfterFunc(eventsHBInterval, func() { o.checkAndSetPendingRequestsOk() })
					o.setPendingRequestsOk(false)
					return
				}
			}
		}
	}
	o.setPendingRequestsOk(true)
}

// On leadership change make sure we alert the pending requests that they are no longer valid.
func (o *consumer) checkPendingRequests() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.mset == nil || o.outq == nil {
		return
	}
	hdr := []byte("NATS/1.0 409 Leadership Change\r\n\r\n")
	for reply := range o.prm {
		o.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
	}
	o.prm = nil
}

// This will release any pending pull requests if applicable.
// Should be called only by the leader being deleted or stopped.
// Lock should be held.
func (o *consumer) releaseAnyPendingRequests() {
	if o.mset == nil || o.outq == nil || o.waiting.len() == 0 {
		return
	}
	hdr := []byte("NATS/1.0 409 Consumer Deleted\r\n\r\n")
	wq := o.waiting
	o.waiting = nil
	for i, rp := 0, wq.rp; i < wq.n; i++ {
		if wr := wq.reqs[rp]; wr != nil {
			o.outq.send(newJSPubMsg(wr.reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
			wr.recycle()
		}
		rp = (rp + 1) % cap(wq.reqs)
	}
}

// Process a NAK.
func (o *consumer) processNak(sseq, dseq, dc uint64, nak []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Check for out of range.
	if dseq <= o.adflr || dseq > o.dseq {
		return
	}
	// If we are explicit ack make sure this is still on our pending list.
	if _, ok := o.pending[sseq]; !ok {
		return
	}

	// Deliver an advisory
	e := JSConsumerDeliveryNakAdvisory{
		TypedEvent: TypedEvent{
			Type: JSConsumerDeliveryNakAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:      o.stream,
		Consumer:    o.name,
		ConsumerSeq: dseq,
		StreamSeq:   sseq,
		Deliveries:  dc,
		Domain:      o.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(e)
	if err != nil {
		return
	}

	o.sendAdvisory(o.nakEventT, j)

	// Check to see if we have delays attached.
	if len(nak) > len(AckNak) {
		arg := bytes.TrimSpace(nak[len(AckNak):])
		if len(arg) > 0 {
			var d time.Duration
			var err error
			if arg[0] == '{' {
				var nd ConsumerNakOptions
				if err = json.Unmarshal(arg, &nd); err == nil {
					d = nd.Delay
				}
			} else {
				d, err = time.ParseDuration(string(arg))
			}
			if err != nil {
				// Treat this as normal NAK.
				o.srv.Warnf("JetStream consumer '%s > %s > %s' bad NAK delay value: %q", o.acc.Name, o.stream, o.name, arg)
			} else {
				// We have a parsed duration that the user wants us to wait before retrying.
				// Make sure we are not on the rdq.
				o.removeFromRedeliverQueue(sseq)
				if p, ok := o.pending[sseq]; ok {
					// now - ackWait is expired now, so offset from there.
					p.Timestamp = time.Now().Add(-o.cfg.AckWait).Add(d).UnixNano()
					// Update store system which will update followers as well.
					o.updateDelivered(p.Sequence, sseq, dc, p.Timestamp)
					if o.ptmr != nil {
						// Want checkPending to run and figure out the next timer ttl.
						// TODO(dlc) - We could optimize this maybe a bit more and track when we expect the timer to fire.
						o.ptmr.Reset(10 * time.Millisecond)
					}
				}
				// Nothing else for use to do now so return.
				return
			}
		}
	}

	// If already queued up also ignore.
	if !o.onRedeliverQueue(sseq) {
		o.addToRedeliverQueue(sseq)
	}

	o.signalNewMessages()
}

// Process a TERM
func (o *consumer) processTerm(sseq, dseq, dc uint64) {
	// Treat like an ack to suppress redelivery.
	o.processAckMsg(sseq, dseq, dc, false)

	o.mu.Lock()
	defer o.mu.Unlock()

	// Deliver an advisory
	e := JSConsumerDeliveryTerminatedAdvisory{
		TypedEvent: TypedEvent{
			Type: JSConsumerDeliveryTerminatedAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:      o.stream,
		Consumer:    o.name,
		ConsumerSeq: dseq,
		StreamSeq:   sseq,
		Deliveries:  dc,
		Domain:      o.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(e)
	if err != nil {
		return
	}

	subj := JSAdvisoryConsumerMsgTerminatedPre + "." + o.stream + "." + o.name
	o.sendAdvisory(subj, j)
}

// Introduce a small delay in when timer fires to check pending.
// Allows bursts to be treated in same time frame.
const ackWaitDelay = time.Millisecond

// ackWait returns how long to wait to fire the pending timer.
func (o *consumer) ackWait(next time.Duration) time.Duration {
	if next > 0 {
		return next + ackWaitDelay
	}
	return o.cfg.AckWait + ackWaitDelay
}

// Due to bug in calculation of sequences on restoring redelivered let's do quick sanity check.
// Lock should be held.
func (o *consumer) checkRedelivered(slseq uint64) {
	var lseq uint64
	if mset := o.mset; mset != nil {
		lseq = slseq
	}
	var shouldUpdateState bool
	for sseq := range o.rdc {
		if sseq < o.asflr || (lseq > 0 && sseq > lseq) {
			delete(o.rdc, sseq)
			o.removeFromRedeliverQueue(sseq)
			shouldUpdateState = true
		}
	}
	if shouldUpdateState {
		if err := o.writeStoreStateUnlocked(); err != nil && o.srv != nil && o.mset != nil && !o.closed {
			s, acc, mset, name := o.srv, o.acc, o.mset, o.name
			s.Warnf("Consumer '%s > %s > %s' error on write store state from check redelivered: %v", acc, mset.cfg.Name, name, err)
		}
	}
}

// This will restore the state from disk.
// Lock should be held.
func (o *consumer) readStoredState(slseq uint64) error {
	if o.store == nil {
		return nil
	}
	state, err := o.store.State()
	if err == nil {
		o.applyState(state)
		if len(o.rdc) > 0 {
			o.checkRedelivered(slseq)
		}
	}
	return err
}

// Apply the consumer stored state.
// Lock should be held.
func (o *consumer) applyState(state *ConsumerState) {
	if state == nil {
		return
	}

	// If o.sseq is greater don't update. Don't go backwards on o.sseq.
	if o.sseq <= state.Delivered.Stream {
		o.sseq = state.Delivered.Stream + 1
	}
	o.dseq = state.Delivered.Consumer + 1

	o.adflr = state.AckFloor.Consumer
	o.asflr = state.AckFloor.Stream
	o.pending = state.Pending
	o.rdc = state.Redelivered

	// Setup tracking timer if we have restored pending.
	if len(o.pending) > 0 {
		// This is on startup or leader change. We want to check pending
		// sooner in case there are inconsistencies etc. Pick between 500ms - 1.5s
		delay := 500*time.Millisecond + time.Duration(rand.Int63n(1000))*time.Millisecond
		// If normal is lower than this just use that.
		if o.cfg.AckWait < delay {
			delay = o.ackWait(0)
		}
		if o.ptmr == nil {
			o.ptmr = time.AfterFunc(delay, o.checkPending)
		} else {
			o.ptmr.Reset(delay)
		}
	}
}

// Sets our store state from another source. Used in clustered mode on snapshot restore.
// Lock should be held.
func (o *consumer) setStoreState(state *ConsumerState) error {
	if state == nil || o.store == nil {
		return nil
	}
	o.applyState(state)
	return o.store.Update(state)
}

// Update our state to the store.
func (o *consumer) writeStoreState() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.writeStoreStateUnlocked()
}

// Update our state to the store.
// Lock should be held.
func (o *consumer) writeStoreStateUnlocked() error {
	if o.store == nil {
		return nil
	}
	state := ConsumerState{
		Delivered: SequencePair{
			Consumer: o.dseq - 1,
			Stream:   o.sseq - 1,
		},
		AckFloor: SequencePair{
			Consumer: o.adflr,
			Stream:   o.asflr,
		},
		Pending:     o.pending,
		Redelivered: o.rdc,
	}
	return o.store.Update(&state)
}

// Returns an initial info. Only applicable for non-clustered consumers.
// We will clear after we return it, so one shot.
func (o *consumer) initialInfo() *ConsumerInfo {
	o.mu.Lock()
	ici := o.ici
	o.ici = nil // gc friendly
	o.mu.Unlock()
	if ici == nil {
		ici = o.info()
	}
	return ici
}

// Clears our initial info.
// Used when we have a leader change in cluster mode but do not send a response.
func (o *consumer) clearInitialInfo() {
	o.mu.Lock()
	o.ici = nil // gc friendly
	o.mu.Unlock()
}

// Info returns our current consumer state.
func (o *consumer) info() *ConsumerInfo {
	return o.infoWithSnap(false)
}

func (o *consumer) infoWithSnap(snap bool) *ConsumerInfo {
	return o.infoWithSnapAndReply(snap, _EMPTY_)
}

func (o *consumer) infoWithSnapAndReply(snap bool, reply string) *ConsumerInfo {
	o.mu.Lock()
	mset := o.mset
	if mset == nil || mset.srv == nil {
		o.mu.Unlock()
		return nil
	}
	js := o.js
	if js == nil {
		o.mu.Unlock()
		return nil
	}

	// Capture raftGroup.
	var rg *raftGroup
	if o.ca != nil {
		rg = o.ca.Group
	}

	cfg := o.cfg
	info := &ConsumerInfo{
		Stream:  o.stream,
		Name:    o.name,
		Created: o.created,
		Config:  &cfg,
		Delivered: SequenceInfo{
			Consumer: o.dseq - 1,
			Stream:   o.sseq - 1,
		},
		AckFloor: SequenceInfo{
			Consumer: o.adflr,
			Stream:   o.asflr,
		},
		NumAckPending:  len(o.pending),
		NumRedelivered: len(o.rdc),
		NumPending:     o.checkNumPending(),
		PushBound:      o.isPushMode() && o.active,
	}
	// Adjust active based on non-zero etc. Also make UTC here.
	if !o.ldt.IsZero() {
		ldt := o.ldt.UTC() // This copies as well.
		info.Delivered.Last = &ldt
	}
	if !o.lat.IsZero() {
		lat := o.lat.UTC() // This copies as well.
		info.AckFloor.Last = &lat
	}

	// If we are a pull mode consumer, report on number of waiting requests.
	if o.isPullMode() {
		o.processWaiting(false)
		info.NumWaiting = o.waiting.len()
	}
	// If we were asked to snapshot do so here.
	if snap {
		o.ici = info
	}
	sysc := o.sysc
	o.mu.Unlock()

	// Do cluster.
	if rg != nil {
		info.Cluster = js.clusterInfo(rg)
	}

	// If we have a reply subject send the response here.
	if reply != _EMPTY_ && sysc != nil {
		sysc.sendInternalMsg(reply, _EMPTY_, nil, info)
	}

	return info
}

// Will signal us that new messages are available. Will break out of waiting.
func (o *consumer) signalNewMessages() {
	// Kick our new message channel
	select {
	case o.mch <- struct{}{}:
	default:
	}
}

// shouldSample lets us know if we are sampling metrics on acks.
func (o *consumer) shouldSample() bool {
	switch {
	case o.sfreq <= 0:
		return false
	case o.sfreq >= 100:
		return true
	}

	// TODO(ripienaar) this is a tad slow so we need to rethink here, however this will only
	// hit for those with sampling enabled and its not the default
	return rand.Int31n(100) <= o.sfreq
}

func (o *consumer) sampleAck(sseq, dseq, dc uint64) {
	if !o.shouldSample() {
		return
	}

	now := time.Now().UTC()
	unow := now.UnixNano()

	e := JSConsumerAckMetric{
		TypedEvent: TypedEvent{
			Type: JSConsumerAckMetricType,
			ID:   nuid.Next(),
			Time: now,
		},
		Stream:      o.stream,
		Consumer:    o.name,
		ConsumerSeq: dseq,
		StreamSeq:   sseq,
		Delay:       unow - o.pending[sseq].Timestamp,
		Deliveries:  dc,
		Domain:      o.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(e)
	if err != nil {
		return
	}

	o.sendAdvisory(o.ackEventT, j)
}

func (o *consumer) processAckMsg(sseq, dseq, dc uint64, doSample bool) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}

	var sagap uint64
	var needSignal bool

	switch o.cfg.AckPolicy {
	case AckExplicit:
		if p, ok := o.pending[sseq]; ok {
			if doSample {
				o.sampleAck(sseq, dseq, dc)
			}
			if o.maxp > 0 && len(o.pending) >= o.maxp {
				needSignal = true
			}
			delete(o.pending, sseq)
			// Use the original deliver sequence from our pending record.
			dseq = p.Sequence
		}
		if len(o.pending) == 0 {
			o.adflr, o.asflr = o.dseq-1, o.sseq-1
		} else if dseq == o.adflr+1 {
			o.adflr, o.asflr = dseq, sseq
			for ss := sseq + 1; ss < o.sseq; ss++ {
				if p, ok := o.pending[ss]; ok {
					if p.Sequence > 0 {
						o.adflr, o.asflr = p.Sequence-1, ss-1
					}
					break
				}
			}
		}
		// We do these regardless.
		delete(o.rdc, sseq)
		o.removeFromRedeliverQueue(sseq)
	case AckAll:
		// no-op
		if dseq <= o.adflr || sseq <= o.asflr {
			o.mu.Unlock()
			return
		}
		if o.maxp > 0 && len(o.pending) >= o.maxp {
			needSignal = true
		}
		sagap = sseq - o.asflr
		o.adflr, o.asflr = dseq, sseq
		for seq := sseq; seq > sseq-sagap; seq-- {
			delete(o.pending, seq)
			delete(o.rdc, seq)
			o.removeFromRedeliverQueue(seq)
		}
	case AckNone:
		// FIXME(dlc) - This is error but do we care?
		o.mu.Unlock()
		return
	}

	// Update underlying store.
	o.updateAcks(dseq, sseq)

	mset := o.mset
	clustered := o.node != nil
	o.mu.Unlock()

	// Let the owning stream know if we are interest or workqueue retention based.
	// If this consumer is clustered this will be handled by processReplicatedAck
	// after the ack has propagated.
	if !clustered && mset != nil && mset.cfg.Retention != LimitsPolicy {
		if sagap > 1 {
			// FIXME(dlc) - This is very inefficient, will need to fix.
			for seq := sseq; seq > sseq-sagap; seq-- {
				mset.ackMsg(o, seq)
			}
		} else {
			mset.ackMsg(o, sseq)
		}
	}

	// If we had max ack pending set and were at limit we need to unblock ourselves.
	if needSignal {
		o.signalNewMessages()
	}
}

// Determine if this is a truly filtered consumer. Modern clients will place filtered subjects
// even if the stream only has a single non-wildcard subject designation.
// Read lock should be held.
func (o *consumer) isFiltered() bool {
	if o.cfg.FilterSubject == _EMPTY_ {
		return false
	}
	// If we are here we want to check if the filtered subject is
	// a direct match for our only listed subject.
	mset := o.mset
	if mset == nil {
		return true
	}
	if len(mset.cfg.Subjects) == 1 {
		return o.cfg.FilterSubject != mset.cfg.Subjects[0]
	}
	// All else return true.
	return true
}

// Check if we need an ack for this store seq.
// This is called for interest based retention streams to remove messages.
func (o *consumer) needAck(sseq uint64, subj string) bool {
	var needAck bool
	var asflr, osseq uint64
	var pending map[uint64]*Pending

	o.mu.RLock()
	defer o.mu.RUnlock()

	isFiltered := o.isFiltered()
	if isFiltered && o.mset == nil {
		return false
	}

	// Check if we are filtered, and if so check if this is even applicable to us.
	if isFiltered {
		if subj == _EMPTY_ {
			var svp StoreMsg
			if _, err := o.mset.store.LoadMsg(sseq, &svp); err != nil {
				return false
			}
			subj = svp.subj
		}
		if !o.isFilteredMatch(subj) {
			return false
		}
	}

	if o.isLeader() {
		asflr, osseq = o.asflr, o.sseq
		pending = o.pending
	} else {
		if o.store == nil {
			return false
		}
		state, err := o.store.BorrowState()
		if err != nil || state == nil {
			// Fall back to what we track internally for now.
			return sseq > o.asflr && !o.isFiltered()
		}
		// If loading state as here, the osseq is +1.
		asflr, osseq, pending = state.AckFloor.Stream, state.Delivered.Stream+1, state.Pending
	}

	switch o.cfg.AckPolicy {
	case AckNone, AckAll:
		needAck = sseq > asflr
	case AckExplicit:
		if sseq > asflr {
			if sseq >= osseq {
				needAck = true
			} else {
				_, needAck = pending[sseq]
			}
		}
	}

	return needAck
}

// Helper for the next message requests.
func nextReqFromMsg(msg []byte) (time.Time, int, int, bool, time.Duration, time.Time, error) {
	req := bytes.TrimSpace(msg)

	switch {
	case len(req) == 0:
		return time.Time{}, 1, 0, false, 0, time.Time{}, nil

	case req[0] == '{':
		var cr JSApiConsumerGetNextRequest
		if err := json.Unmarshal(req, &cr); err != nil {
			return time.Time{}, -1, 0, false, 0, time.Time{}, err
		}
		var hbt time.Time
		if cr.Heartbeat > 0 {
			if cr.Heartbeat*2 > cr.Expires {
				return time.Time{}, 1, 0, false, 0, time.Time{}, errors.New("heartbeat value too large")
			}
			hbt = time.Now().Add(cr.Heartbeat)
		}
		if cr.Expires == time.Duration(0) {
			return time.Time{}, cr.Batch, cr.MaxBytes, cr.NoWait, cr.Heartbeat, hbt, nil
		}
		return time.Now().Add(cr.Expires), cr.Batch, cr.MaxBytes, cr.NoWait, cr.Heartbeat, hbt, nil
	default:
		if n, err := strconv.Atoi(string(req)); err == nil {
			return time.Time{}, n, 0, false, 0, time.Time{}, nil
		}
	}

	return time.Time{}, 1, 0, false, 0, time.Time{}, nil
}

// Represents a request that is on the internal waiting queue
type waitingRequest struct {
	acc      *Account
	interest string
	reply    string
	n        int // For batching
	d        int
	b        int // For max bytes tracking.
	expires  time.Time
	received time.Time
	hb       time.Duration
	hbt      time.Time
	noWait   bool
}

// sync.Pool for waiting requests.
var wrPool = sync.Pool{
	New: func() interface{} {
		return new(waitingRequest)
	},
}

// Recycle this request. This request can not be accessed after this call.
func (wr *waitingRequest) recycleIfDone() bool {
	if wr != nil && wr.n <= 0 {
		wr.recycle()
		return true
	}
	return false
}

// Force a recycle.
func (wr *waitingRequest) recycle() {
	if wr != nil {
		wr.acc, wr.interest, wr.reply = nil, _EMPTY_, _EMPTY_
		wrPool.Put(wr)
	}
}

// waiting queue for requests that are waiting for new messages to arrive.
type waitQueue struct {
	rp, wp, n int
	last      time.Time
	reqs      []*waitingRequest
}

// Create a new ring buffer with at most max items.
func newWaitQueue(max int) *waitQueue {
	return &waitQueue{rp: -1, reqs: make([]*waitingRequest, max)}
}

var (
	errWaitQueueFull = errors.New("wait queue is full")
	errWaitQueueNil  = errors.New("wait queue is nil")
)

// Adds in a new request.
func (wq *waitQueue) add(wr *waitingRequest) error {
	if wq == nil {
		return errWaitQueueNil
	}
	if wq.isFull() {
		return errWaitQueueFull
	}
	wq.reqs[wq.wp] = wr
	// TODO(dlc) - Could make pow2 and get rid of mod.
	wq.wp = (wq.wp + 1) % cap(wq.reqs)

	// Adjust read pointer if we were empty.
	if wq.rp < 0 {
		wq.rp = 0
	}
	// Track last active via when we receive a request.
	wq.last = wr.received
	wq.n++
	return nil
}

func (wq *waitQueue) isFull() bool {
	return wq.n == cap(wq.reqs)
}

func (wq *waitQueue) isEmpty() bool {
	return wq.len() == 0
}

func (wq *waitQueue) len() int {
	if wq == nil {
		return 0
	}
	return wq.n
}

// Peek will return the next request waiting or nil if empty.
func (wq *waitQueue) peek() *waitingRequest {
	if wq == nil {
		return nil
	}
	var wr *waitingRequest
	if wq.rp >= 0 {
		wr = wq.reqs[wq.rp]
	}
	return wr
}

// pop will return the next request and move the read cursor.
// This will now place a request that still has pending items at the ends of the list.
func (wq *waitQueue) pop() *waitingRequest {
	wr := wq.peek()
	if wr != nil {
		wr.d++
		wr.n--

		// Always remove current now on a pop, and move to end if still valid.
		// If we were the only one don't need to remove since this can be a no-op.
		if wr.n > 0 && wq.n > 1 {
			wq.removeCurrent()
			wq.add(wr)
		} else if wr.n <= 0 {
			wq.removeCurrent()
		}
	}
	return wr
}

// Removes the current read pointer (head FIFO) entry.
func (wq *waitQueue) removeCurrent() {
	if wq.rp < 0 {
		return
	}
	wq.reqs[wq.rp] = nil
	wq.rp = (wq.rp + 1) % cap(wq.reqs)
	wq.n--
	// Check if we are empty.
	if wq.n == 0 {
		wq.rp, wq.wp = -1, 0
	}
}

// Will compact when we have interior deletes.
func (wq *waitQueue) compact() {
	if wq.isEmpty() {
		return
	}
	nreqs, i := make([]*waitingRequest, cap(wq.reqs)), 0
	for j, rp := 0, wq.rp; j < wq.n; j++ {
		if wr := wq.reqs[rp]; wr != nil {
			nreqs[i] = wr
			i++
		}
		rp = (rp + 1) % cap(wq.reqs)
	}
	// Reset here.
	wq.rp, wq.wp, wq.n, wq.reqs = 0, i, i, nreqs
}

// Return the map of pending requests keyed by the reply subject.
// No-op if push consumer or invalid etc.
func (o *consumer) pendingRequests() map[string]*waitingRequest {
	if o.waiting == nil {
		return nil
	}
	wq, m := o.waiting, make(map[string]*waitingRequest)
	for i, rp := 0, wq.rp; i < wq.n; i++ {
		if wr := wq.reqs[rp]; wr != nil {
			m[wr.reply] = wr
		}
		rp = (rp + 1) % cap(wq.reqs)
	}
	return m
}

// Return next waiting request. This will check for expirations but not noWait or interest.
// That will be handled by processWaiting.
// Lock should be held.
func (o *consumer) nextWaiting(sz int) *waitingRequest {
	if o.waiting == nil || o.waiting.isEmpty() {
		return nil
	}
	for wr := o.waiting.peek(); !o.waiting.isEmpty(); wr = o.waiting.peek() {
		if wr == nil {
			break
		}
		// Check if we have max bytes set.
		if wr.b > 0 {
			if sz <= wr.b {
				wr.b -= sz
				// If we are right now at zero, set batch to 1 to deliver this one but stop after.
				if wr.b == 0 {
					wr.n = 1
				}
			} else {
				// Since we can't send that message to the requestor, we need to
				// notify that we are closing the request.
				const maxBytesT = "NATS/1.0 409 Message Size Exceeds MaxBytes\r\n%s: %d\r\n%s: %d\r\n\r\n"
				hdr := []byte(fmt.Sprintf(maxBytesT, JSPullRequestPendingMsgs, wr.n, JSPullRequestPendingBytes, wr.b))
				o.outq.send(newJSPubMsg(wr.reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
				// Remove the current one, no longer valid due to max bytes limit.
				o.waiting.removeCurrent()
				if o.node != nil {
					o.removeClusterPendingRequest(wr.reply)
				}
				wr.recycle()
				continue
			}
		}

		if wr.expires.IsZero() || time.Now().Before(wr.expires) {
			rr := wr.acc.sl.Match(wr.interest)
			if len(rr.psubs)+len(rr.qsubs) > 0 {
				return o.waiting.pop()
			} else if time.Since(wr.received) < defaultGatewayRecentSubExpiration && (o.srv.leafNodeEnabled || o.srv.gateway.enabled) {
				return o.waiting.pop()
			} else if o.srv.gateway.enabled && o.srv.hasGatewayInterest(wr.acc.Name, wr.interest) {
				return o.waiting.pop()
			}
		} else {
			// We do check for expiration in `processWaiting`, but it is possible to hit the expiry here, and not there.
			hdr := []byte(fmt.Sprintf("NATS/1.0 408 Request Timeout\r\n%s: %d\r\n%s: %d\r\n\r\n", JSPullRequestPendingMsgs, wr.n, JSPullRequestPendingBytes, wr.b))
			o.outq.send(newJSPubMsg(wr.reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
			o.waiting.removeCurrent()
			if o.node != nil {
				o.removeClusterPendingRequest(wr.reply)
			}
			wr.recycle()
			continue

		}
		if wr.interest != wr.reply {
			const intExpT = "NATS/1.0 408 Interest Expired\r\n%s: %d\r\n%s: %d\r\n\r\n"
			hdr := []byte(fmt.Sprintf(intExpT, JSPullRequestPendingMsgs, wr.n, JSPullRequestPendingBytes, wr.b))
			o.outq.send(newJSPubMsg(wr.reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		}
		// Remove the current one, no longer valid.
		o.waiting.removeCurrent()
		if o.node != nil {
			o.removeClusterPendingRequest(wr.reply)
		}
		wr.recycle()
	}
	return nil
}

// Next message request.
type nextMsgReq struct {
	reply string
	msg   []byte
}

var nextMsgReqPool sync.Pool

func newNextMsgReq(reply string, msg []byte) *nextMsgReq {
	var nmr *nextMsgReq
	m := nextMsgReqPool.Get()
	if m != nil {
		nmr = m.(*nextMsgReq)
	} else {
		nmr = &nextMsgReq{}
	}
	// When getting something from a pool it is critical that all fields are
	// initialized. Doing this way guarantees that if someone adds a field to
	// the structure, the compiler will fail the build if this line is not updated.
	(*nmr) = nextMsgReq{reply, msg}
	return nmr
}

func (nmr *nextMsgReq) returnToPool() {
	if nmr == nil {
		return
	}
	nmr.reply, nmr.msg = _EMPTY_, nil
	nextMsgReqPool.Put(nmr)
}

// processNextMsgReq will process a request for the next message available. A nil message payload means deliver
// a single message. If the payload is a formal request or a number parseable with Atoi(), then we will send a
// batch of messages without requiring another request to this endpoint, or an ACK.
func (o *consumer) processNextMsgReq(_ *subscription, c *client, _ *Account, _, reply string, msg []byte) {
	if reply == _EMPTY_ {
		return
	}

	// Short circuit error here.
	if o.nextMsgReqs == nil {
		hdr := []byte("NATS/1.0 409 Consumer is push based\r\n\r\n")
		o.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}

	_, msg = c.msgParts(msg)
	o.nextMsgReqs.push(newNextMsgReq(reply, copyBytes(msg)))
}

func (o *consumer) processNextMsgRequest(reply string, msg []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	mset := o.mset
	if mset == nil {
		return
	}

	sendErr := func(status int, description string) {
		hdr := []byte(fmt.Sprintf("NATS/1.0 %d %s\r\n\r\n", status, description))
		o.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
	}

	if o.isPushMode() || o.waiting == nil {
		sendErr(409, "Consumer is push based")
		return
	}

	// Check payload here to see if they sent in batch size or a formal request.
	expires, batchSize, maxBytes, noWait, hb, hbt, err := nextReqFromMsg(msg)
	if err != nil {
		sendErr(400, fmt.Sprintf("Bad Request - %v", err))
		return
	}

	// Check for request limits
	if o.cfg.MaxRequestBatch > 0 && batchSize > o.cfg.MaxRequestBatch {
		sendErr(409, fmt.Sprintf("Exceeded MaxRequestBatch of %d", o.cfg.MaxRequestBatch))
		return
	}

	if !expires.IsZero() && o.cfg.MaxRequestExpires > 0 && expires.After(time.Now().Add(o.cfg.MaxRequestExpires)) {
		sendErr(409, fmt.Sprintf("Exceeded MaxRequestExpires of %v", o.cfg.MaxRequestExpires))
		return
	}

	if maxBytes > 0 && o.cfg.MaxRequestMaxBytes > 0 && maxBytes > o.cfg.MaxRequestMaxBytes {
		sendErr(409, fmt.Sprintf("Exceeded MaxRequestMaxBytes of %v", o.cfg.MaxRequestMaxBytes))
		return
	}

	// If we have the max number of requests already pending try to expire.
	if o.waiting.isFull() {
		// Try to expire some of the requests.
		o.processWaiting(false)
	}

	// If the request is for noWait and we have pending requests already, check if we have room.
	if noWait {
		msgsPending := o.numPending() + uint64(len(o.rdq))
		// If no pending at all, decide what to do with request.
		// If no expires was set then fail.
		if msgsPending == 0 && expires.IsZero() {
			o.waiting.last = time.Now()
			sendErr(404, "No Messages")
			return
		}
		if msgsPending > 0 {
			_, _, batchPending, _ := o.processWaiting(false)
			if msgsPending < uint64(batchPending) {
				o.waiting.last = time.Now()
				sendErr(408, "Requests Pending")
				return
			}
		}
		// If we are here this should be considered a one-shot situation.
		// We will wait for expires but will return as soon as we have any messages.
	}

	// If we receive this request though an account export, we need to track that interest subject and account.
	acc, interest := trackDownAccountAndInterest(o.acc, reply)

	// Create a waiting request.
	wr := wrPool.Get().(*waitingRequest)
	wr.acc, wr.interest, wr.reply, wr.n, wr.d, wr.noWait, wr.expires, wr.hb, wr.hbt = acc, interest, reply, batchSize, 0, noWait, expires, hb, hbt
	wr.b = maxBytes
	wr.received = time.Now()

	if err := o.waiting.add(wr); err != nil {
		sendErr(409, "Exceeded MaxWaiting")
		return
	}
	o.signalNewMessages()
	// If we are clustered update our followers about this request.
	if o.node != nil {
		o.addClusterPendingRequest(wr.reply)
	}
}

func trackDownAccountAndInterest(acc *Account, interest string) (*Account, string) {
	for strings.HasPrefix(interest, replyPrefix) {
		oa := acc
		oa.mu.RLock()
		if oa.exports.responses == nil {
			oa.mu.RUnlock()
			break
		}
		si := oa.exports.responses[interest]
		if si == nil {
			oa.mu.RUnlock()
			break
		}
		acc, interest = si.acc, si.to
		oa.mu.RUnlock()
	}
	return acc, interest
}

// Increase the delivery count for this message.
// ONLY used on redelivery semantics.
// Lock should be held.
func (o *consumer) incDeliveryCount(sseq uint64) uint64 {
	if o.rdc == nil {
		o.rdc = make(map[uint64]uint64)
	}
	o.rdc[sseq] += 1
	return o.rdc[sseq] + 1
}

// send a delivery exceeded advisory.
func (o *consumer) notifyDeliveryExceeded(sseq, dc uint64) {
	e := JSConsumerDeliveryExceededAdvisory{
		TypedEvent: TypedEvent{
			Type: JSConsumerDeliveryExceededAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:     o.stream,
		Consumer:   o.name,
		StreamSeq:  sseq,
		Deliveries: dc,
		Domain:     o.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(e)
	if err != nil {
		return
	}

	o.sendAdvisory(o.deliveryExcEventT, j)
}

// Check to see if the candidate subject matches a filter if its present.
// Lock should be held.
func (o *consumer) isFilteredMatch(subj string) bool {
	// No filter is automatic match.
	if o.cfg.FilterSubject == _EMPTY_ {
		return true
	}
	if !o.filterWC {
		return subj == o.cfg.FilterSubject
	}
	// If we are here we have a wildcard filter subject.
	// TODO(dlc) at speed might be better to just do a sublist with L2 and/or possibly L1.
	return subjectIsSubsetMatch(subj, o.cfg.FilterSubject)
}

var (
	errMaxAckPending = errors.New("max ack pending reached")
	errBadConsumer   = errors.New("consumer not valid")
	errNoInterest    = errors.New("consumer requires interest for delivery subject when ephemeral")
)

// Get next available message from underlying store.
// Is partition aware and redeliver aware.
// Lock should be held.
func (o *consumer) getNextMsg() (*jsPubMsg, uint64, error) {
	if o.mset == nil || o.mset.store == nil {
		return nil, 0, errBadConsumer
	}
	seq, dc := o.sseq, uint64(1)
	// Process redelivered messages before looking at possibly "skip list" (deliver last per subject)
	if o.hasRedeliveries() {
		for seq = o.getNextToRedeliver(); seq > 0; seq = o.getNextToRedeliver() {
			dc = o.incDeliveryCount(seq)
			if o.maxdc > 0 && dc > o.maxdc {
				// Only send once
				if dc == o.maxdc+1 {
					o.notifyDeliveryExceeded(seq, dc-1)
				}
				// Make sure to remove from pending.
				delete(o.pending, seq)
				continue
			}
			if seq > 0 {
				pmsg := getJSPubMsgFromPool()
				sm, err := o.mset.store.LoadMsg(seq, &pmsg.StoreMsg)
				if sm == nil || err != nil {
					pmsg.returnToPool()
					pmsg, dc = nil, 0
				}
				return pmsg, dc, err
			}
		}
		// Fallback if all redeliveries are gone.
		seq, dc = o.sseq, 1
	}
	// Don't make it a "else" because it is possible that there were redeliveries
	// but we exhausted the redelivery count and are back to try deliver the next message.
	if o.hasSkipListPending() {
		seq = o.lss.seqs[0]
		if len(o.lss.seqs) == 1 {
			o.sseq = o.lss.resume
			o.lss = nil
			o.updateSkipped()
		} else {
			o.lss.seqs = o.lss.seqs[1:]
		}
	}

	// Check if we have max pending.
	if o.maxp > 0 && len(o.pending) >= o.maxp {
		// maxp only set when ack policy != AckNone and user set MaxAckPending
		// Stall if we have hit max pending.
		return nil, 0, errMaxAckPending
	}

	store := o.mset.store
	filter, filterWC := o.cfg.FilterSubject, o.filterWC

	// Grab next message applicable to us.
	// We will unlock here in case lots of contention, e.g. WQ.
	o.mu.Unlock()
	pmsg := getJSPubMsgFromPool()
	sm, sseq, err := store.LoadNextMsg(filter, filterWC, seq, &pmsg.StoreMsg)
	if sm == nil {
		pmsg.returnToPool()
		pmsg, dc = nil, 0
	}
	o.mu.Lock()

	if sseq >= o.sseq {
		o.sseq = sseq + 1
		if err == ErrStoreEOF {
			o.updateSkipped()
		}
	}

	return pmsg, dc, err
}

// Will check for expiration and lack of interest on waiting requests.
// Will also do any heartbeats and return the next expiration or HB interval.
func (o *consumer) processWaiting(eos bool) (int, int, int, time.Time) {
	var fexp time.Time
	if o.srv == nil || o.waiting.isEmpty() {
		return 0, 0, 0, fexp
	}

	var expired, brp int
	s, now := o.srv, time.Now()

	// Signals interior deletes, which we will compact if needed.
	var hid bool
	remove := func(wr *waitingRequest, i int) {
		if i == o.waiting.rp {
			o.waiting.removeCurrent()
		} else {
			o.waiting.reqs[i] = nil
			hid = true
		}
		if o.node != nil {
			o.removeClusterPendingRequest(wr.reply)
		}
		expired++
		wr.recycle()
	}

	wq := o.waiting
	for i, rp, n := 0, wq.rp, wq.n; i < n; rp = (rp + 1) % cap(wq.reqs) {
		wr := wq.reqs[rp]
		// Check expiration.
		if (eos && wr.noWait && wr.d > 0) || (!wr.expires.IsZero() && now.After(wr.expires)) {
			hdr := []byte(fmt.Sprintf("NATS/1.0 408 Request Timeout\r\n%s: %d\r\n%s: %d\r\n\r\n", JSPullRequestPendingMsgs, wr.n, JSPullRequestPendingBytes, wr.b))
			o.outq.send(newJSPubMsg(wr.reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
			remove(wr, rp)
			i++
			continue
		}
		// Now check interest.
		rr := wr.acc.sl.Match(wr.interest)
		interest := len(rr.psubs)+len(rr.qsubs) > 0
		if !interest && (s.leafNodeEnabled || s.gateway.enabled) {
			// If we are here check on gateways and leaf nodes (as they can mask gateways on the other end).
			// If we have interest or the request is too young break and do not expire.
			if time.Since(wr.received) < defaultGatewayRecentSubExpiration {
				interest = true
			} else if s.gateway.enabled && s.hasGatewayInterest(wr.acc.Name, wr.interest) {
				interest = true
			}
		}
		// If interest, update batch pending requests counter and update fexp timer.
		if interest {
			brp += wr.n
			if !wr.hbt.IsZero() {
				if now.After(wr.hbt) {
					// Fire off a heartbeat here.
					o.sendIdleHeartbeat(wr.reply)
					// Update next HB.
					wr.hbt = now.Add(wr.hb)
				}
				if fexp.IsZero() || wr.hbt.Before(fexp) {
					fexp = wr.hbt
				}
			}
			if !wr.expires.IsZero() && (fexp.IsZero() || wr.expires.Before(fexp)) {
				fexp = wr.expires
			}
			i++
			continue
		}
		// No more interest here so go ahead and remove this one from our list.
		remove(wr, rp)
		i++
	}

	// If we have interior deletes from out of order invalidation, compact the waiting queue.
	if hid {
		o.waiting.compact()
	}

	return expired, wq.len(), brp, fexp
}

// Will check to make sure those waiting still have registered interest.
func (o *consumer) checkWaitingForInterest() bool {
	o.processWaiting(true)
	return o.waiting.len() > 0
}

// Lock should be held.
func (o *consumer) hbTimer() (time.Duration, *time.Timer) {
	if o.cfg.Heartbeat == 0 {
		return 0, nil
	}
	return o.cfg.Heartbeat, time.NewTimer(o.cfg.Heartbeat)
}

func (o *consumer) processInboundAcks(qch chan struct{}) {
	// Grab the server lock to watch for server quit.
	o.mu.RLock()
	s := o.srv
	hasInactiveThresh := o.cfg.InactiveThreshold > 0
	o.mu.RUnlock()

	for {
		select {
		case <-o.ackMsgs.ch:
			acks := o.ackMsgs.pop()
			for _, ack := range acks {
				o.processAck(ack.subject, ack.reply, ack.hdr, ack.msg)
				ack.returnToPool()
			}
			o.ackMsgs.recycle(&acks)
			// If we have an inactiveThreshold set, mark our activity.
			if hasInactiveThresh {
				o.suppressDeletion()
			}
		case <-qch:
			return
		case <-s.quitCh:
			return
		}
	}
}

// Process inbound next message requests.
func (o *consumer) processInboundNextMsgReqs(qch chan struct{}) {
	// Grab the server lock to watch for server quit.
	o.mu.RLock()
	s := o.srv
	o.mu.RUnlock()

	for {
		select {
		case <-o.nextMsgReqs.ch:
			reqs := o.nextMsgReqs.pop()
			for _, req := range reqs {
				o.processNextMsgRequest(req.reply, req.msg)
				req.returnToPool()
			}
			o.nextMsgReqs.recycle(&reqs)
		case <-qch:
			return
		case <-s.quitCh:
			return
		}
	}
}

// Suppress auto cleanup on ack activity of any kind.
func (o *consumer) suppressDeletion() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.closed {
		return
	}

	if o.isPushMode() && o.dtmr != nil {
		// if dtmr is not nil we have started the countdown, simply reset to threshold.
		o.dtmr.Reset(o.dthresh)
	} else if o.isPullMode() && o.waiting != nil {
		// Pull mode always has timer running, just update last on waiting queue.
		o.waiting.last = time.Now()
	}
}

func (o *consumer) loopAndGatherMsgs(qch chan struct{}) {
	// On startup check to see if we are in a a reply situation where replay policy is not instant.
	var (
		lts  int64 // last time stamp seen, used for replay.
		lseq uint64
	)

	o.mu.RLock()
	mset := o.mset
	getLSeq := o.replay
	o.mu.RUnlock()
	// consumer is closed when mset is set to nil.
	if mset == nil {
		return
	}
	if getLSeq {
		lseq = mset.state().LastSeq
	}

	o.mu.Lock()
	s := o.srv
	// need to check again if consumer is closed
	if o.mset == nil {
		o.mu.Unlock()
		return
	}
	// For idle heartbeat support.
	var hbc <-chan time.Time
	hbd, hb := o.hbTimer()
	if hb != nil {
		hbc = hb.C
	}
	// Interest changes.
	inch := o.inch
	o.mu.Unlock()

	// Grab the stream's retention policy
	mset.mu.RLock()
	rp := mset.cfg.Retention
	mset.mu.RUnlock()

	var err error

	// Deliver all the msgs we have now, once done or on a condition, we wait for new ones.
	for {
		var (
			pmsg     *jsPubMsg
			dc       uint64
			dsubj    string
			ackReply string
			delay    time.Duration
			sz       int
		)
		o.mu.Lock()
		// consumer is closed when mset is set to nil.
		if o.mset == nil {
			o.mu.Unlock()
			return
		}

		// Clear last error.
		err = nil

		// If we are in push mode and not active or under flowcontrol let's stop sending.
		if o.isPushMode() {
			if !o.active || (o.maxpb > 0 && o.pbytes > o.maxpb) {
				goto waitForMsgs
			}
		} else if o.waiting.isEmpty() {
			// If we are in pull mode and no one is waiting already break and wait.
			goto waitForMsgs
		}

		// Grab our next msg.
		pmsg, dc, err = o.getNextMsg()

		// On error either wait or return.
		if err != nil || pmsg == nil {
			// On EOF we can optionally fast sync num pending state.
			if err == ErrStoreEOF {
				o.checkNumPendingOnEOF()
			}
			if err == ErrStoreMsgNotFound || err == ErrStoreEOF || err == errMaxAckPending || err == errPartialCache {
				goto waitForMsgs
			} else {
				s.Errorf("Received an error looking up message for consumer: %v", err)
				goto waitForMsgs
			}
		}

		// Update our cached num pending here first.
		if dc == 1 {
			o.npc--
		}
		// Pre-calculate ackReply
		ackReply = o.ackReply(pmsg.seq, o.dseq, dc, pmsg.ts, o.numPending())

		// If headers only do not send msg payload.
		// Add in msg size itself as header.
		if o.cfg.HeadersOnly {
			convertToHeadersOnly(pmsg)
		}
		// Calculate payload size. This can be calculated on client side.
		// We do not include transport subject here since not generally known on client.
		sz = len(pmsg.subj) + len(ackReply) + len(pmsg.hdr) + len(pmsg.msg)

		if o.isPushMode() {
			dsubj = o.dsubj
		} else if wr := o.nextWaiting(sz); wr != nil {
			dsubj = wr.reply
			if done := wr.recycleIfDone(); done && o.node != nil {
				o.removeClusterPendingRequest(dsubj)
			} else if !done && wr.hb > 0 {
				wr.hbt = time.Now().Add(wr.hb)
			}
		} else {
			// We will redo this one.
			o.sseq--
			if dc == 1 {
				o.npc++
			}
			pmsg.returnToPool()
			goto waitForMsgs
		}

		// If we are in a replay scenario and have not caught up check if we need to delay here.
		if o.replay && lts > 0 {
			if delay = time.Duration(pmsg.ts - lts); delay > time.Millisecond {
				o.mu.Unlock()
				select {
				case <-qch:
					pmsg.returnToPool()
					return
				case <-time.After(delay):
				}
				o.mu.Lock()
			}
		}

		// Track this regardless.
		lts = pmsg.ts

		// If we have a rate limit set make sure we check that here.
		if o.rlimit != nil {
			now := time.Now()
			r := o.rlimit.ReserveN(now, sz)
			delay := r.DelayFrom(now)
			if delay > 0 {
				o.mu.Unlock()
				select {
				case <-qch:
					pmsg.returnToPool()
					return
				case <-time.After(delay):
				}
				o.mu.Lock()
			}
		}

		// Do actual delivery.
		o.deliverMsg(dsubj, ackReply, pmsg, dc, rp)

		// Reset our idle heartbeat timer if set.
		if hb != nil {
			hb.Reset(hbd)
		}

		o.mu.Unlock()
		continue

	waitForMsgs:
		// If we were in a replay state check to see if we are caught up. If so clear.
		if o.replay && o.sseq > lseq {
			o.replay = false
		}

		// Make sure to process any expired requests that are pending.
		var wrExp <-chan time.Time
		if o.isPullMode() {
			// Dont expire oneshots if we are here because of max ack pending limit.
			_, _, _, fexp := o.processWaiting(err != errMaxAckPending)
			if !fexp.IsZero() {
				expires := time.Until(fexp)
				if expires <= 0 {
					expires = time.Millisecond
				}
				wrExp = time.NewTimer(expires).C
			}
		}

		// We will wait here for new messages to arrive.
		mch, odsubj := o.mch, o.cfg.DeliverSubject
		o.mu.Unlock()

		select {
		case <-mch:
			// Messages are waiting.
		case interest := <-inch:
			// inch can be nil on pull-based, but then this will
			// just block and not fire.
			o.updateDeliveryInterest(interest)
		case <-qch:
			return
		case <-wrExp:
			o.mu.Lock()
			o.processWaiting(true)
			o.mu.Unlock()
		case <-hbc:
			if o.isActive() {
				o.mu.RLock()
				o.sendIdleHeartbeat(odsubj)
				o.mu.RUnlock()
			}
			// Reset our idle heartbeat timer.
			hb.Reset(hbd)
		}
	}
}

// Lock should be held.
func (o *consumer) sendIdleHeartbeat(subj string) {
	const t = "NATS/1.0 100 Idle Heartbeat\r\n%s: %d\r\n%s: %d\r\n\r\n"
	sseq, dseq := o.sseq-1, o.dseq-1
	hdr := []byte(fmt.Sprintf(t, JSLastConsumerSeq, dseq, JSLastStreamSeq, sseq))
	if fcp := o.fcid; fcp != _EMPTY_ {
		// Add in that we are stalled on flow control here.
		addOn := []byte(fmt.Sprintf("%s: %s\r\n\r\n", JSConsumerStalled, fcp))
		hdr = append(hdr[:len(hdr)-LEN_CR_LF], []byte(addOn)...)
	}
	o.outq.send(newJSPubMsg(subj, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
}

func (o *consumer) ackReply(sseq, dseq, dc uint64, ts int64, pending uint64) string {
	return fmt.Sprintf(o.ackReplyT, dc, sseq, dseq, ts, pending)
}

// Used mostly for testing. Sets max pending bytes for flow control setups.
func (o *consumer) setMaxPendingBytes(limit int) {
	o.pblimit = limit
	o.maxpb = limit / 16
	if o.maxpb == 0 {
		o.maxpb = 1
	}
}

// Does some sanity checks to see if we should re-calculate.
// Since there is a race when decrementing when there is contention at the beginning of the stream.
// The race is a getNextMsg skips a deleted msg, and then the decStreamPending call fires.
// This does some quick sanity checks to see if we should re-calculate num pending.
// Lock should be held.
func (o *consumer) checkNumPending() uint64 {
	if o.mset != nil {
		var state StreamState
		o.mset.store.FastState(&state)
		if o.sseq > state.LastSeq && o.npc != 0 || o.npc > int64(state.Msgs) {
			// Re-calculate.
			o.streamNumPending()
		}
	}
	return o.numPending()
}

// Lock should be held.
func (o *consumer) numPending() uint64 {
	if o.npc < 0 {
		return 0
	}
	return uint64(o.npc)
}

// This will do a quick sanity check on num pending when we encounter
// and EOF in the loop and gather.
// Lock should be held.
func (o *consumer) checkNumPendingOnEOF() {
	if o.mset == nil {
		return
	}
	var state StreamState
	o.mset.store.FastState(&state)
	if o.sseq > state.LastSeq && o.npc != 0 {
		// We know here we can reset our running state for num pending.
		o.npc, o.npf = 0, state.LastSeq
	}
}

// Call into streamNumPending after acquiring the consumer lock.
func (o *consumer) streamNumPendingLocked() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.streamNumPending()
}

// Will force a set from the stream store of num pending.
// Depends on delivery policy, for last per subject we calculate differently.
// Lock should be held.
func (o *consumer) streamNumPending() uint64 {
	if o.mset == nil || o.mset.store == nil {
		o.npc, o.npf = 0, 0
	} else {
		isLastPerSubject := o.cfg.DeliverPolicy == DeliverLastPerSubject
		// Set our num pending and valid sequence floor.
		npc, npf := o.mset.store.NumPending(o.sseq, o.cfg.FilterSubject, isLastPerSubject)
		o.npc, o.npf = int64(npc), npf
	}

	return o.numPending()
}

func convertToHeadersOnly(pmsg *jsPubMsg) {
	// If headers only do not send msg payload.
	// Add in msg size itself as header.
	hdr, msg := pmsg.hdr, pmsg.msg
	var bb bytes.Buffer
	if len(hdr) == 0 {
		bb.WriteString(hdrLine)
	} else {
		bb.Write(hdr)
		bb.Truncate(len(hdr) - LEN_CR_LF)
	}
	bb.WriteString(JSMsgSize)
	bb.WriteString(": ")
	bb.WriteString(strconv.FormatInt(int64(len(msg)), 10))
	bb.WriteString(CR_LF)
	bb.WriteString(CR_LF)
	// Replace underlying buf which we can use directly when we send.
	// TODO(dlc) - Probably just use directly when forming bytes.Buffer?
	pmsg.buf = pmsg.buf[:0]
	pmsg.buf = append(pmsg.buf, bb.Bytes()...)
	// Replace with new header.
	pmsg.hdr = pmsg.buf
	// Cancel msg payload
	pmsg.msg = nil
}

// Deliver a msg to the consumer.
// Lock should be held and o.mset validated to be non-nil.
func (o *consumer) deliverMsg(dsubj, ackReply string, pmsg *jsPubMsg, dc uint64, rp RetentionPolicy) {
	if o.mset == nil {
		pmsg.returnToPool()
		return
	}

	dseq := o.dseq
	o.dseq++

	pmsg.dsubj, pmsg.reply, pmsg.o = dsubj, ackReply, o
	psz := pmsg.size()

	if o.maxpb > 0 {
		o.pbytes += psz
	}

	mset := o.mset
	ap := o.cfg.AckPolicy

	// Cant touch pmsg after this sending so capture what we need.
	seq, ts := pmsg.seq, pmsg.ts
	// Send message.
	o.outq.send(pmsg)

	if ap == AckExplicit || ap == AckAll {
		o.trackPending(seq, dseq)
	} else if ap == AckNone {
		o.adflr = dseq
		o.asflr = seq
	}

	// Flow control.
	if o.maxpb > 0 && o.needFlowControl(psz) {
		o.sendFlowControl()
	}

	// If pull mode and we have inactivity threshold, signaled by dthresh, update last activity.
	if o.isPullMode() && o.dthresh > 0 {
		o.waiting.last = time.Now()
	}

	// FIXME(dlc) - Capture errors?
	o.updateDelivered(dseq, seq, dc, ts)

	// If we are ack none and mset is interest only we should make sure stream removes interest.
	if ap == AckNone && rp != LimitsPolicy {
		if o.node == nil || o.cfg.Direct {
			mset.ackq.push(seq)
		} else {
			o.updateAcks(dseq, seq)
		}
	}
}

func (o *consumer) needFlowControl(sz int) bool {
	if o.maxpb == 0 {
		return false
	}
	// Decide whether to send a flow control message which we will need the user to respond.
	// We send when we are over 50% of our current window limit.
	if o.fcid == _EMPTY_ && o.pbytes > o.maxpb/2 {
		return true
	}
	// If we have an existing outstanding FC, check to see if we need to expand the o.fcsz
	if o.fcid != _EMPTY_ && (o.pbytes-o.fcsz) >= o.maxpb {
		o.fcsz += sz
	}
	return false
}

func (o *consumer) processFlowControl(_ *subscription, c *client, _ *Account, subj, _ string, _ []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Ignore if not the latest we have sent out.
	if subj != o.fcid {
		return
	}

	// For slow starts and ramping up.
	if o.maxpb < o.pblimit {
		o.maxpb *= 2
		if o.maxpb > o.pblimit {
			o.maxpb = o.pblimit
		}
	}

	// Update accounting.
	o.pbytes -= o.fcsz
	if o.pbytes < 0 {
		o.pbytes = 0
	}
	o.fcid, o.fcsz = _EMPTY_, 0

	o.signalNewMessages()
}

// Lock should be held.
func (o *consumer) fcReply() string {
	var sb strings.Builder
	sb.WriteString(jsFlowControlPre)
	sb.WriteString(o.stream)
	sb.WriteByte(btsep)
	sb.WriteString(o.name)
	sb.WriteByte(btsep)
	var b [4]byte
	rn := rand.Int63()
	for i, l := 0, rn; i < len(b); i++ {
		b[i] = digits[l%base]
		l /= base
	}
	sb.Write(b[:])
	return sb.String()
}

// sendFlowControl will send a flow control packet to the consumer.
// Lock should be held.
func (o *consumer) sendFlowControl() {
	if !o.isPushMode() {
		return
	}
	subj, rply := o.cfg.DeliverSubject, o.fcReply()
	o.fcsz, o.fcid = o.pbytes, rply
	hdr := []byte("NATS/1.0 100 FlowControl Request\r\n\r\n")
	o.outq.send(newJSPubMsg(subj, _EMPTY_, rply, hdr, nil, nil, 0))
}

// Tracks our outstanding pending acks. Only applicable to AckExplicit mode.
// Lock should be held.
func (o *consumer) trackPending(sseq, dseq uint64) {
	if o.pending == nil {
		o.pending = make(map[uint64]*Pending)
	}
	if o.ptmr == nil {
		o.ptmr = time.AfterFunc(o.ackWait(0), o.checkPending)
	}
	if p, ok := o.pending[sseq]; ok {
		p.Timestamp = time.Now().UnixNano()
		p.Sequence = dseq
	} else {
		o.pending[sseq] = &Pending{dseq, time.Now().UnixNano()}
	}
}

// didNotDeliver is called when a delivery for a consumer message failed.
// Depending on our state, we will process the failure.
func (o *consumer) didNotDeliver(seq uint64) {
	o.mu.Lock()
	mset := o.mset
	if mset == nil {
		o.mu.Unlock()
		return
	}
	var checkDeliveryInterest bool
	if o.isPushMode() {
		o.active = false
		checkDeliveryInterest = true
	} else if o.pending != nil {
		// pull mode and we have pending.
		if _, ok := o.pending[seq]; ok {
			// We found this messsage on pending, we need
			// to queue it up for immediate redelivery since
			// we know it was not delivered.
			if !o.onRedeliverQueue(seq) {
				o.addToRedeliverQueue(seq)
				o.signalNewMessages()
			}
		}
	}
	o.mu.Unlock()

	// If we do not have interest update that here.
	if checkDeliveryInterest && o.hasNoLocalInterest() {
		o.updateDeliveryInterest(false)
	}
}

// Lock should be held.
func (o *consumer) addToRedeliverQueue(seqs ...uint64) {
	if o.rdqi == nil {
		o.rdqi = make(map[uint64]struct{})
	}
	o.rdq = append(o.rdq, seqs...)
	for _, seq := range seqs {
		o.rdqi[seq] = struct{}{}
	}
}

// Lock should be held.
func (o *consumer) hasRedeliveries() bool {
	return len(o.rdq) > 0
}

func (o *consumer) getNextToRedeliver() uint64 {
	if len(o.rdq) == 0 {
		return 0
	}
	seq := o.rdq[0]
	if len(o.rdq) == 1 {
		o.rdq, o.rdqi = nil, nil
	} else {
		o.rdq = append(o.rdq[:0], o.rdq[1:]...)
		delete(o.rdqi, seq)
	}
	return seq
}

// This checks if we already have this sequence queued for redelivery.
// FIXME(dlc) - This is O(n) but should be fast with small redeliver size.
// Lock should be held.
func (o *consumer) onRedeliverQueue(seq uint64) bool {
	if o.rdqi == nil {
		return false
	}
	_, ok := o.rdqi[seq]
	return ok
}

// Remove a sequence from the redelivery queue.
// Lock should be held.
func (o *consumer) removeFromRedeliverQueue(seq uint64) bool {
	if !o.onRedeliverQueue(seq) {
		return false
	}
	for i, rseq := range o.rdq {
		if rseq == seq {
			if len(o.rdq) == 1 {
				o.rdq, o.rdqi = nil, nil
			} else {
				o.rdq = append(o.rdq[:i], o.rdq[i+1:]...)
				delete(o.rdqi, seq)
			}
			return true
		}
	}
	return false
}

// Checks the pending messages.
func (o *consumer) checkPending() {
	o.mu.RLock()
	mset := o.mset
	// On stop, mset and timer will be nil.
	if o.closed || mset == nil || o.ptmr == nil {
		stopAndClearTimer(&o.ptmr)
		o.mu.RUnlock()
		return
	}
	o.mu.RUnlock()

	var shouldUpdateState bool
	var state StreamState
	mset.store.FastState(&state)
	fseq := state.FirstSeq

	o.mu.Lock()
	defer o.mu.Unlock()

	now := time.Now().UnixNano()
	ttl := int64(o.cfg.AckWait)
	next := int64(o.ackWait(0))
	// However, if there is backoff, initializes with the largest backoff.
	// It will be adjusted as needed.
	if l := len(o.cfg.BackOff); l > 0 {
		next = int64(o.cfg.BackOff[l-1])
	}

	// Since we can update timestamps, we have to review all pending.
	// We will now bail if we see an ack pending in bound to us via o.awl.
	var expired []uint64
	check := len(o.pending) > 1024
	for seq, p := range o.pending {
		if check && atomic.LoadInt64(&o.awl) > 0 {
			o.ptmr.Reset(100 * time.Millisecond)
			return
		}
		// Check if these are no longer valid.
		if seq < fseq || seq <= o.asflr {
			delete(o.pending, seq)
			delete(o.rdc, seq)
			o.removeFromRedeliverQueue(seq)
			shouldUpdateState = true
			// Check if we need to move ack floors.
			if seq > o.asflr {
				o.asflr = seq
			}
			if p.Sequence > o.adflr {
				o.adflr = p.Sequence
			}
			continue
		}
		elapsed, deadline := now-p.Timestamp, ttl
		if len(o.cfg.BackOff) > 0 {
			// This is ok even if o.rdc is nil, we would get dc == 0, which is what we want.
			dc := int(o.rdc[seq])
			// This will be the index for the next backoff, will set to last element if needed.
			nbi := dc + 1
			if dc+1 >= len(o.cfg.BackOff) {
				dc = len(o.cfg.BackOff) - 1
				nbi = dc
			}
			deadline = int64(o.cfg.BackOff[dc])
			// Set `next` to the next backoff (if smaller than current `next` value).
			if nextBackoff := int64(o.cfg.BackOff[nbi]); nextBackoff < next {
				next = nextBackoff
			}
		}
		if elapsed >= deadline {
			if !o.onRedeliverQueue(seq) {
				expired = append(expired, seq)
			}
		} else if deadline-elapsed < next {
			// Update when we should fire next.
			next = deadline - elapsed
		}
	}

	if len(expired) > 0 {
		// We need to sort.
		sort.Slice(expired, func(i, j int) bool { return expired[i] < expired[j] })
		o.addToRedeliverQueue(expired...)
		// Now we should update the timestamp here since we are redelivering.
		// We will use an incrementing time to preserve order for any other redelivery.
		off := now - o.pending[expired[0]].Timestamp
		for _, seq := range expired {
			if p, ok := o.pending[seq]; ok {
				p.Timestamp += off
			}
		}
		o.signalNewMessages()
	}

	if len(o.pending) > 0 {
		delay := time.Duration(next)
		if o.ptmr == nil {
			o.ptmr = time.AfterFunc(delay, o.checkPending)
		} else {
			o.ptmr.Reset(o.ackWait(delay))
		}
	} else {
		// Make sure to stop timer and clear out any re delivery queues
		stopAndClearTimer(&o.ptmr)
		o.rdq, o.rdqi = nil, nil
		o.pending = nil
	}

	// Update our state if needed.
	if shouldUpdateState {
		if err := o.writeStoreStateUnlocked(); err != nil && o.srv != nil && o.mset != nil && !o.closed {
			s, acc, mset, name := o.srv, o.acc, o.mset, o.name
			s.Warnf("Consumer '%s > %s > %s' error on write store state from check pending: %v", acc, mset.cfg.Name, name, err)
		}
	}
}

// SeqFromReply will extract a sequence number from a reply subject.
func (o *consumer) seqFromReply(reply string) uint64 {
	_, dseq, _ := ackReplyInfo(reply)
	return dseq
}

// StreamSeqFromReply will extract the stream sequence from the reply subject.
func (o *consumer) streamSeqFromReply(reply string) uint64 {
	sseq, _, _ := ackReplyInfo(reply)
	return sseq
}

// Quick parser for positive numbers in ack reply encoding.
func parseAckReplyNum(d string) (n int64) {
	if len(d) == 0 {
		return -1
	}
	for _, dec := range d {
		if dec < asciiZero || dec > asciiNine {
			return -1
		}
		n = n*10 + (int64(dec) - asciiZero)
	}
	return n
}

const expectedNumReplyTokens = 9

// Grab encoded information in the reply subject for a delivered message.
func replyInfo(subject string) (sseq, dseq, dc uint64, ts int64, pending uint64) {
	tsa := [expectedNumReplyTokens]string{}
	start, tokens := 0, tsa[:0]
	for i := 0; i < len(subject); i++ {
		if subject[i] == btsep {
			tokens = append(tokens, subject[start:i])
			start = i + 1
		}
	}
	tokens = append(tokens, subject[start:])
	if len(tokens) != expectedNumReplyTokens || tokens[0] != "$JS" || tokens[1] != "ACK" {
		return 0, 0, 0, 0, 0
	}
	// TODO(dlc) - Should we error if we do not match consumer name?
	// stream is tokens[2], consumer is 3.
	dc = uint64(parseAckReplyNum(tokens[4]))
	sseq, dseq = uint64(parseAckReplyNum(tokens[5])), uint64(parseAckReplyNum(tokens[6]))
	ts = parseAckReplyNum(tokens[7])
	pending = uint64(parseAckReplyNum(tokens[8]))

	return sseq, dseq, dc, ts, pending
}

func ackReplyInfo(subject string) (sseq, dseq, dc uint64) {
	tsa := [expectedNumReplyTokens]string{}
	start, tokens := 0, tsa[:0]
	for i := 0; i < len(subject); i++ {
		if subject[i] == btsep {
			tokens = append(tokens, subject[start:i])
			start = i + 1
		}
	}
	tokens = append(tokens, subject[start:])
	if len(tokens) != expectedNumReplyTokens || tokens[0] != "$JS" || tokens[1] != "ACK" {
		return 0, 0, 0
	}
	dc = uint64(parseAckReplyNum(tokens[4]))
	sseq, dseq = uint64(parseAckReplyNum(tokens[5])), uint64(parseAckReplyNum(tokens[6]))

	return sseq, dseq, dc
}

// NextSeq returns the next delivered sequence number for this consumer.
func (o *consumer) nextSeq() uint64 {
	o.mu.RLock()
	dseq := o.dseq
	o.mu.RUnlock()
	return dseq
}

// Used to hold skip list when deliver policy is last per subject.
type lastSeqSkipList struct {
	resume uint64
	seqs   []uint64
}

// Will create a skip list for us from a store's subjects state.
func createLastSeqSkipList(mss map[string]SimpleState) []uint64 {
	seqs := make([]uint64, 0, len(mss))
	for _, ss := range mss {
		seqs = append(seqs, ss.Last)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	return seqs
}

// Let's us know we have a skip list, which is for deliver last per subject and we are just starting.
// Lock should be held.
func (o *consumer) hasSkipListPending() bool {
	return o.lss != nil && len(o.lss.seqs) > 0
}

// Will select the starting sequence.
func (o *consumer) selectStartingSeqNo() {
	if o.mset == nil || o.mset.store == nil {
		o.sseq = 1
	} else {
		var state StreamState
		o.mset.store.FastState(&state)
		if o.cfg.OptStartSeq == 0 {
			if o.cfg.DeliverPolicy == DeliverAll {
				o.sseq = state.FirstSeq
			} else if o.cfg.DeliverPolicy == DeliverLast {
				o.sseq = state.LastSeq
				// If we are partitioned here this will be properly set when we become leader.
				if o.cfg.FilterSubject != _EMPTY_ {
					ss := o.mset.store.FilteredState(1, o.cfg.FilterSubject)
					o.sseq = ss.Last
				}
			} else if o.cfg.DeliverPolicy == DeliverLastPerSubject {
				if mss := o.mset.store.SubjectsState(o.cfg.FilterSubject); len(mss) > 0 {
					o.lss = &lastSeqSkipList{
						resume: state.LastSeq,
						seqs:   createLastSeqSkipList(mss),
					}
					o.sseq = o.lss.seqs[0]
				} else {
					// If no mapping info just set to last.
					o.sseq = state.LastSeq
				}
			} else if o.cfg.OptStartTime != nil {
				// If we are here we are time based.
				// TODO(dlc) - Once clustered can't rely on this.
				o.sseq = o.mset.store.GetSeqFromTime(*o.cfg.OptStartTime)
			} else {
				// DeliverNew
				o.sseq = state.LastSeq + 1
			}
		} else {
			o.sseq = o.cfg.OptStartSeq
		}

		if state.FirstSeq == 0 {
			o.sseq = 1
		} else if o.sseq < state.FirstSeq {
			o.sseq = state.FirstSeq
		} else if o.sseq > state.LastSeq {
			o.sseq = state.LastSeq + 1
		}
	}

	// Always set delivery sequence to 1.
	o.dseq = 1
	// Set ack delivery floor to delivery-1
	o.adflr = o.dseq - 1
	// Set ack store floor to store-1
	o.asflr = o.sseq - 1

	// Set our starting sequence state.
	if o.store != nil && o.sseq > 0 {
		o.store.SetStarting(o.sseq - 1)
	}
}

// Test whether a config represents a durable subscriber.
func isDurableConsumer(config *ConsumerConfig) bool {
	return config != nil && config.Durable != _EMPTY_
}

func (o *consumer) isDurable() bool {
	return o.cfg.Durable != _EMPTY_
}

// Are we in push mode, delivery subject, etc.
func (o *consumer) isPushMode() bool {
	return o.cfg.DeliverSubject != _EMPTY_
}

func (o *consumer) isPullMode() bool {
	return o.cfg.DeliverSubject == _EMPTY_
}

// Name returns the name of this consumer.
func (o *consumer) String() string {
	o.mu.RLock()
	n := o.name
	o.mu.RUnlock()
	return n
}

func createConsumerName() string {
	return getHash(nuid.Next())
}

// deleteConsumer will delete the consumer from this stream.
func (mset *stream) deleteConsumer(o *consumer) error {
	return o.delete()
}

func (o *consumer) getStream() *stream {
	o.mu.RLock()
	mset := o.mset
	o.mu.RUnlock()
	return mset
}

func (o *consumer) streamName() string {
	o.mu.RLock()
	mset := o.mset
	o.mu.RUnlock()
	if mset != nil {
		return mset.name()
	}
	return _EMPTY_
}

// Active indicates if this consumer is still active.
func (o *consumer) isActive() bool {
	o.mu.RLock()
	active := o.active && o.mset != nil
	o.mu.RUnlock()
	return active
}

// hasNoLocalInterest return true if we have no local interest.
func (o *consumer) hasNoLocalInterest() bool {
	o.mu.RLock()
	rr := o.acc.sl.Match(o.cfg.DeliverSubject)
	o.mu.RUnlock()
	return len(rr.psubs)+len(rr.qsubs) == 0
}

// This is when the underlying stream has been purged.
// sseq is the new first seq for the stream after purge.
// Lock should be held.
func (o *consumer) purge(sseq uint64, slseq uint64) {
	// Do not update our state unless we know we are the leader.
	if !o.isLeader() {
		return
	}
	// Signals all have been purged for this consumer.
	if sseq == 0 {
		sseq = slseq + 1
	}

	o.mu.Lock()
	// Do not go backwards
	if o.sseq < sseq {
		o.sseq = sseq
	}

	if o.asflr < sseq {
		o.asflr = sseq - 1

		// We need to remove those no longer relevant from pending.
		for seq, p := range o.pending {
			if seq <= o.asflr {
				if p.Sequence > o.adflr {
					o.adflr = p.Sequence
					if o.adflr > o.dseq {
						o.dseq = o.adflr
					}
				}
				delete(o.pending, seq)
				delete(o.rdc, seq)
				// rdq handled below.
			}
		}
	}
	// This means we can reset everything at this point.
	if len(o.pending) == 0 {
		o.pending, o.rdc = nil, nil
	}

	// We need to remove all those being queued for redelivery under o.rdq
	if len(o.rdq) > 0 {
		rdq := o.rdq
		o.rdq, o.rdqi = nil, nil
		for _, sseq := range rdq {
			if sseq >= o.sseq {
				o.addToRedeliverQueue(sseq)
			}
		}
	}
	// Grab some info in case of error below.
	s, acc, mset, name := o.srv, o.acc, o.mset, o.name
	o.mu.Unlock()

	if err := o.writeStoreState(); err != nil && s != nil && mset != nil {
		s.Warnf("Consumer '%s > %s > %s' error on write store state from purge: %v", acc, mset.name(), name, err)
	}
}

func stopAndClearTimer(tp **time.Timer) {
	if *tp == nil {
		return
	}
	// Will get drained in normal course, do not try to
	// drain here.
	(*tp).Stop()
	*tp = nil
}

// Stop will shutdown  the consumer for the associated stream.
func (o *consumer) stop() error {
	return o.stopWithFlags(false, false, true, false)
}

func (o *consumer) deleteWithoutAdvisory() error {
	return o.stopWithFlags(true, false, true, false)
}

// Delete will delete the consumer for the associated stream and send advisories.
func (o *consumer) delete() error {
	return o.stopWithFlags(true, false, true, true)
}

func (o *consumer) stopWithFlags(dflag, sdflag, doSignal, advisory bool) error {
	o.mu.Lock()
	js := o.js

	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true

	// Check if we are the leader and are being deleted.
	if dflag && o.isLeader() {
		// If we are clustered and node leader (probable from above), stepdown.
		if node := o.node; node != nil && node.Leader() {
			node.StepDown()
		}
		if advisory {
			o.sendDeleteAdvisoryLocked()
		}
		if o.isPullMode() {
			// Release any pending.
			o.releaseAnyPendingRequests()
		}
	}

	if o.qch != nil {
		close(o.qch)
		o.qch = nil
	}

	a := o.acc
	store := o.store
	mset := o.mset
	o.mset = nil
	o.active = false
	o.unsubscribe(o.ackSub)
	o.unsubscribe(o.reqSub)
	o.unsubscribe(o.fcSub)
	o.ackSub = nil
	o.reqSub = nil
	o.fcSub = nil
	if o.infoSub != nil {
		o.srv.sysUnsubscribe(o.infoSub)
		o.infoSub = nil
	}
	c := o.client
	o.client = nil
	sysc := o.sysc
	o.sysc = nil
	stopAndClearTimer(&o.ptmr)
	stopAndClearTimer(&o.dtmr)
	stopAndClearTimer(&o.gwdtmr)
	delivery := o.cfg.DeliverSubject
	o.waiting = nil
	// Break us out of the readLoop.
	if doSignal {
		o.signalNewMessages()
	}
	n := o.node
	qgroup := o.cfg.DeliverGroup
	o.ackMsgs.unregister()
	if o.nextMsgReqs != nil {
		o.nextMsgReqs.unregister()
	}

	// For cleaning up the node assignment.
	var ca *consumerAssignment
	if dflag {
		ca = o.ca
	}
	sigSub := o.sigSub
	o.mu.Unlock()

	if c != nil {
		c.closeConnection(ClientClosed)
	}
	if sysc != nil {
		sysc.closeConnection(ClientClosed)
	}

	if delivery != _EMPTY_ {
		a.sl.clearNotification(delivery, qgroup, o.inch)
	}

	var rp RetentionPolicy
	if mset != nil {
		if sigSub != nil {
			mset.removeConsumerAsLeader(o)
		}
		mset.mu.Lock()
		mset.removeConsumer(o)
		rp = mset.cfg.Retention
		mset.mu.Unlock()
	}

	// We need to optionally remove all messages since we are interest based retention.
	// We will do this consistently on all replicas. Note that if in clustered mode the
	// non-leader consumers will need to restore state first.
	if dflag && rp == InterestPolicy {
		state := mset.state()
		stop := state.LastSeq
		o.mu.Lock()
		if !o.isLeader() {
			o.readStoredState(stop)
		}
		start := o.asflr
		o.mu.Unlock()
		// Make sure we start at worst with first sequence in the stream.
		if start < state.FirstSeq {
			start = state.FirstSeq
		}

		var rmseqs []uint64
		mset.mu.Lock()
		for seq := start; seq <= stop; seq++ {
			if mset.noInterest(seq, o) {
				rmseqs = append(rmseqs, seq)
			}
		}
		mset.mu.Unlock()

		// These can be removed.
		for _, seq := range rmseqs {
			mset.store.RemoveMsg(seq)
		}
	}

	// Cluster cleanup.
	if n != nil {
		if dflag {
			n.Delete()
		} else {
			// Try to install snapshot on clean exit
			if o.store != nil && (o.retention != LimitsPolicy || n.NeedSnapshot()) {
				if snap, err := o.store.EncodedState(); err == nil {
					n.InstallSnapshot(snap)
				}
			}
			n.Stop()
		}
	}

	if ca != nil {
		js.mu.Lock()
		if ca.Group != nil {
			ca.Group.node = nil
		}
		js.mu.Unlock()
	}

	// Clean up our store.
	var err error
	if store != nil {
		if dflag {
			if sdflag {
				err = store.StreamDelete()
			} else {
				err = store.Delete()
			}
		} else {
			err = store.Stop()
		}
	}

	return err
}

// Check that we do not form a cycle by delivering to a delivery subject
// that is part of the interest group.
func deliveryFormsCycle(cfg *StreamConfig, deliverySubject string) bool {
	for _, subject := range cfg.Subjects {
		if subjectIsSubsetMatch(deliverySubject, subject) {
			return true
		}
	}
	return false
}

// Check that the filtered subject is valid given a set of stream subjects.
func validFilteredSubject(filteredSubject string, subjects []string) bool {
	if !IsValidSubject(filteredSubject) {
		return false
	}
	hasWC := subjectHasWildcard(filteredSubject)

	for _, subject := range subjects {
		if subjectIsSubsetMatch(filteredSubject, subject) {
			return true
		}
		// If we have a wildcard as the filtered subject check to see if we are
		// a wider scope but do match a subject.
		if hasWC && subjectIsSubsetMatch(subject, filteredSubject) {
			return true
		}
	}
	return false
}

// switchToEphemeral is called on startup when recovering ephemerals.
func (o *consumer) switchToEphemeral() {
	o.mu.Lock()
	o.cfg.Durable = _EMPTY_
	store, ok := o.store.(*consumerFileStore)
	rr := o.acc.sl.Match(o.cfg.DeliverSubject)
	// Setup dthresh.
	o.updateInactiveThreshold(&o.cfg)
	o.mu.Unlock()

	// Update interest
	o.updateDeliveryInterest(len(rr.psubs)+len(rr.qsubs) > 0)
	// Write out new config
	if ok {
		store.updateConfig(o.cfg)
	}
}

// RequestNextMsgSubject returns the subject to request the next message when in pull or worker mode.
// Returns empty otherwise.
func (o *consumer) requestNextMsgSubject() string {
	return o.nextMsgSubj
}

func (o *consumer) decStreamPending(sseq uint64, subj string) {
	o.mu.Lock()
	// Update our cached num pending only if we think deliverMsg has not done so.
	if sseq >= o.sseq && o.isFilteredMatch(subj) {
		o.npc--
	}

	// Check if this message was pending.
	p, wasPending := o.pending[sseq]
	var rdc uint64 = 1
	if o.rdc != nil {
		rdc = o.rdc[sseq]
	}
	o.mu.Unlock()

	// If it was pending process it like an ack.
	// TODO(dlc) - we could do a term here instead with a reason to generate the advisory.
	if wasPending {
		// We could have lock for stream so do this in a go routine.
		// TODO(dlc) - We should do this with ipq vs naked go routines.
		go o.processTerm(sseq, p.Sequence, rdc)
	}
}

func (o *consumer) account() *Account {
	o.mu.RLock()
	a := o.acc
	o.mu.RUnlock()
	return a
}

func (o *consumer) signalSub() *subscription {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.sigSub != nil {
		return o.sigSub
	}

	subject := o.cfg.FilterSubject
	if subject == _EMPTY_ {
		subject = fwcs
	}
	sub := &subscription{subject: []byte(subject), icb: o.processStreamSignal}
	o.sigSub = sub
	return sub
}

// This is what will be called when our parent stream wants to kick us regarding a new message.
// We know that we are the leader and that this subject matches us by how the parent handles registering
// us with the signaling sublist.
// We do need the sequence of the message however and we use the msg as the encoded seq.
func (o *consumer) processStreamSignal(_ *subscription, _ *client, _ *Account, subject, _ string, seqb []byte) {
	var le = binary.LittleEndian
	seq := le.Uint64(seqb)

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.mset == nil {
		return
	}
	if seq > o.npf {
		o.npc++
	}
	if seq < o.sseq {
		return
	}
	if o.isPushMode() && o.active || o.isPullMode() && !o.waiting.isEmpty() {
		o.signalNewMessages()
	}
}

// Will check if we are running in the monitor already and if not set the appropriate flag.
func (o *consumer) checkInMonitor() bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.inMonitor {
		return true
	}
	o.inMonitor = true
	return false
}

// Clear us being in the monitor routine.
func (o *consumer) clearMonitorRunning() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inMonitor = false
}

// Test whether we are in the monitor routine.
func (o *consumer) isMonitorRunning() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.inMonitor
}

// If we are a consumer of an interest or workqueue policy stream, process that state and make sure consistent.
func (o *consumer) checkStateForInterestStream() {
	o.mu.Lock()
	// See if we need to process this update if our parent stream is not a limits policy stream.
	mset := o.mset
	shouldProcessState := mset != nil && o.retention != LimitsPolicy
	if o.closed || !shouldProcessState {
		o.mu.Unlock()
		return
	}
	state, err := o.store.State()
	o.mu.Unlock()

	if err != nil {
		return
	}

	// We should make sure to update the acks.
	var ss StreamState
	mset.store.FastState(&ss)

	asflr := state.AckFloor.Stream
	for seq := ss.FirstSeq; seq <= asflr; seq++ {
		mset.ackMsg(o, seq)
	}

	o.mu.RLock()
	// See if we need to process this update if our parent stream is not a limits policy stream.
	state, _ = o.store.State()
	o.mu.RUnlock()

	// If we have pending, we will need to walk through to delivered in case we missed any of those acks as well.
	if state != nil && len(state.Pending) > 0 {
		for seq := state.AckFloor.Stream + 1; seq <= state.Delivered.Stream; seq++ {
			if _, ok := state.Pending[seq]; !ok {
				mset.ackMsg(o, seq)
			}
		}
	}
}

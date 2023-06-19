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
	"archive/tar"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/nats-io/nuid"
)

// StreamConfig will determine the name, subjects and retention policy
// for a given stream. If subjects is empty the name will be used.
type StreamConfig struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Subjects     []string        `json:"subjects,omitempty"`
	Retention    RetentionPolicy `json:"retention"`
	MaxConsumers int             `json:"max_consumers"`
	MaxMsgs      int64           `json:"max_msgs"`
	MaxBytes     int64           `json:"max_bytes"`
	MaxAge       time.Duration   `json:"max_age"`
	MaxMsgsPer   int64           `json:"max_msgs_per_subject"`
	MaxMsgSize   int32           `json:"max_msg_size,omitempty"`
	Discard      DiscardPolicy   `json:"discard"`
	Storage      StorageType     `json:"storage"`
	Replicas     int             `json:"num_replicas"`
	NoAck        bool            `json:"no_ack,omitempty"`
	Template     string          `json:"template_owner,omitempty"`
	Duplicates   time.Duration   `json:"duplicate_window,omitempty"`
	Placement    *Placement      `json:"placement,omitempty"`
	Mirror       *StreamSource   `json:"mirror,omitempty"`
	Sources      []*StreamSource `json:"sources,omitempty"`

	// Allow republish of the message after being sequenced and stored.
	RePublish *RePublish `json:"republish,omitempty"`

	// Allow higher performance, direct access to get individual messages. E.g. KeyValue
	AllowDirect bool `json:"allow_direct"`
	// Allow higher performance and unified direct access for mirrors as well.
	MirrorDirect bool `json:"mirror_direct"`

	// Allow KV like semantics to also discard new on a per subject basis
	DiscardNewPer bool `json:"discard_new_per_subject,omitempty"`

	// Optional qualifiers. These can not be modified after set to true.

	// Sealed will seal a stream so no messages can get out or in.
	Sealed bool `json:"sealed"`
	// DenyDelete will restrict the ability to delete messages.
	DenyDelete bool `json:"deny_delete"`
	// DenyPurge will restrict the ability to purge messages.
	DenyPurge bool `json:"deny_purge"`
	// AllowRollup allows messages to be placed into the system and purge
	// all older messages using a special msg header.
	AllowRollup bool `json:"allow_rollup_hdrs"`
}

// RePublish is for republishing messages once committed to a stream.
type RePublish struct {
	Source      string `json:"src,omitempty"`
	Destination string `json:"dest"`
	HeadersOnly bool   `json:"headers_only,omitempty"`
}

// JSPubAckResponse is a formal response to a publish operation.
type JSPubAckResponse struct {
	Error *ApiError `json:"error,omitempty"`
	*PubAck
}

// ToError checks if the response has a error and if it does converts it to an error
// avoiding the pitfalls described by https://yourbasic.org/golang/gotcha-why-nil-error-not-equal-nil/
func (r *JSPubAckResponse) ToError() error {
	if r.Error == nil {
		return nil
	}
	return r.Error
}

// PubAck is the detail you get back from a publish to a stream that was successful.
// e.g. +OK {"stream": "Orders", "seq": 22}
type PubAck struct {
	Stream    string `json:"stream"`
	Sequence  uint64 `json:"seq"`
	Domain    string `json:"domain,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// StreamInfo shows config and current state for this stream.
type StreamInfo struct {
	Config     StreamConfig        `json:"config"`
	Created    time.Time           `json:"created"`
	State      StreamState         `json:"state"`
	Domain     string              `json:"domain,omitempty"`
	Cluster    *ClusterInfo        `json:"cluster,omitempty"`
	Mirror     *StreamSourceInfo   `json:"mirror,omitempty"`
	Sources    []*StreamSourceInfo `json:"sources,omitempty"`
	Alternates []StreamAlternate   `json:"alternates,omitempty"`
}

type StreamAlternate struct {
	Name    string `json:"name"`
	Domain  string `json:"domain,omitempty"`
	Cluster string `json:"cluster"`
}

// ClusterInfo shows information about the underlying set of servers
// that make up the stream or consumer.
type ClusterInfo struct {
	Name     string      `json:"name,omitempty"`
	Leader   string      `json:"leader,omitempty"`
	Replicas []*PeerInfo `json:"replicas,omitempty"`
}

// PeerInfo shows information about all the peers in the cluster that
// are supporting the stream or consumer.
type PeerInfo struct {
	Name    string        `json:"name"`
	Current bool          `json:"current"`
	Offline bool          `json:"offline,omitempty"`
	Active  time.Duration `json:"active"`
	Lag     uint64        `json:"lag,omitempty"`
	Peer    string        `json:"peer"`
	// For migrations.
	cluster string
}

// StreamSourceInfo shows information about an upstream stream source.
type StreamSourceInfo struct {
	Name     string          `json:"name"`
	External *ExternalStream `json:"external,omitempty"`
	Lag      uint64          `json:"lag"`
	Active   time.Duration   `json:"active"`
	Error    *ApiError       `json:"error,omitempty"`
}

// StreamSource dictates how streams can source from other streams.
type StreamSource struct {
	Name          string          `json:"name"`
	OptStartSeq   uint64          `json:"opt_start_seq,omitempty"`
	OptStartTime  *time.Time      `json:"opt_start_time,omitempty"`
	FilterSubject string          `json:"filter_subject,omitempty"`
	External      *ExternalStream `json:"external,omitempty"`

	// Internal
	iname string // For indexing when stream names are the same for multiple sources.
}

// ExternalStream allows you to qualify access to a stream source in another account.
type ExternalStream struct {
	ApiPrefix     string `json:"api"`
	DeliverPrefix string `json:"deliver"`
}

// Stream is a jetstream stream of messages. When we receive a message internally destined
// for a Stream we will direct link from the client to this structure.
type stream struct {
	mu        sync.RWMutex
	js        *jetStream
	jsa       *jsAccount
	acc       *Account
	srv       *Server
	client    *client
	sysc      *client
	sid       int
	pubAck    []byte
	outq      *jsOutQ
	msgs      *ipQueue[*inMsg]
	store     StreamStore
	ackq      *ipQueue[uint64]
	lseq      uint64
	lmsgId    string
	consumers map[string]*consumer
	numFilter int
	cfg       StreamConfig
	created   time.Time
	stype     StorageType
	tier      string
	ddmap     map[string]*ddentry
	ddarr     []*ddentry
	ddindex   int
	ddtmr     *time.Timer
	qch       chan struct{}
	active    bool
	ddloaded  bool
	closed    bool

	// Mirror
	mirror *sourceInfo

	// Sources
	sources map[string]*sourceInfo

	// Indicates we have direct consumers.
	directs int

	// For republishing.
	tr *transform

	// For processing consumers without main stream lock.
	clsMu sync.RWMutex
	cList []*consumer
	sch   chan struct{}
	sigq  *ipQueue[*cMsg]
	csl   *Sublist

	// For non limits policy streams when they process an ack before the actual msg.
	// Can happen in stretch clusters, multi-cloud, or during catchup for a restarted server.
	preAcks map[uint64]map[*consumer]struct{}

	// TODO(dlc) - Hide everything below behind two pointers.
	// Clustered mode.
	sa         *streamAssignment
	node       RaftNode
	catchup    bool
	syncSub    *subscription
	infoSub    *subscription
	clMu       sync.Mutex
	clseq      uint64
	clfs       uint64
	leader     string
	lqsent     time.Time
	catchups   map[string]uint64
	uch        chan struct{}
	compressOK bool
	inMonitor  bool

	// Direct get subscription.
	directSub *subscription
	lastBySub *subscription

	monitorWg sync.WaitGroup
}

type sourceInfo struct {
	name  string
	iname string
	cname string
	sub   *subscription
	dsub  *subscription
	lbsub *subscription
	msgs  *ipQueue[*inMsg]
	sseq  uint64
	dseq  uint64
	start time.Time
	lag   uint64
	err   *ApiError
	last  time.Time
	lreq  time.Time
	qch   chan struct{}
	sip   bool // setup in progress
	wg    sync.WaitGroup
}

// For mirrors and direct get
const (
	dgetGroup          = sysGroup
	dgetCaughtUpThresh = 10
)

// Headers for published messages.
const (
	JSMsgId               = "Nats-Msg-Id"
	JSExpectedStream      = "Nats-Expected-Stream"
	JSExpectedLastSeq     = "Nats-Expected-Last-Sequence"
	JSExpectedLastSubjSeq = "Nats-Expected-Last-Subject-Sequence"
	JSExpectedLastMsgId   = "Nats-Expected-Last-Msg-Id"
	JSStreamSource        = "Nats-Stream-Source"
	JSLastConsumerSeq     = "Nats-Last-Consumer"
	JSLastStreamSeq       = "Nats-Last-Stream"
	JSConsumerStalled     = "Nats-Consumer-Stalled"
	JSMsgRollup           = "Nats-Rollup"
	JSMsgSize             = "Nats-Msg-Size"
	JSResponseType        = "Nats-Response-Type"
)

// Headers for republished messages and direct gets.
const (
	JSStream       = "Nats-Stream"
	JSSequence     = "Nats-Sequence"
	JSTimeStamp    = "Nats-Time-Stamp"
	JSSubject      = "Nats-Subject"
	JSLastSequence = "Nats-Last-Sequence"
)

// Rollups, can be subject only or all messages.
const (
	JSMsgRollupSubject = "sub"
	JSMsgRollupAll     = "all"
)

const (
	jsCreateResponse = "create"
)

// Dedupe entry
type ddentry struct {
	id  string
	seq uint64
	ts  int64
}

// Replicas Range
const (
	StreamMaxReplicas = 5
)

// AddStream adds a stream for the given account.
func (a *Account) addStream(config *StreamConfig) (*stream, error) {
	return a.addStreamWithAssignment(config, nil, nil)
}

// AddStreamWithStore adds a stream for the given account with custome store config options.
func (a *Account) addStreamWithStore(config *StreamConfig, fsConfig *FileStoreConfig) (*stream, error) {
	return a.addStreamWithAssignment(config, fsConfig, nil)
}

func (a *Account) addStreamWithAssignment(config *StreamConfig, fsConfig *FileStoreConfig, sa *streamAssignment) (*stream, error) {
	s, jsa, err := a.checkForJetStream()
	if err != nil {
		return nil, err
	}

	// If we do not have the stream currently assigned to us in cluster mode we will proceed but warn.
	// This can happen on startup with restored state where on meta replay we still do not have
	// the assignment. Running in single server mode this always returns true.
	if !jsa.streamAssigned(config.Name) {
		s.Debugf("Stream '%s > %s' does not seem to be assigned to this server", a.Name, config.Name)
	}

	// Sensible defaults.
	cfg, apiErr := s.checkStreamCfg(config, a)
	if apiErr != nil {
		return nil, apiErr
	}

	singleServerMode := !s.JetStreamIsClustered() && s.standAloneMode()
	if singleServerMode && cfg.Replicas > 1 {
		return nil, ApiErrors[JSStreamReplicasNotSupportedErr]
	}

	// Make sure we are ok when these are done in parallel.
	v, loaded := jsa.inflight.LoadOrStore(cfg.Name, &sync.WaitGroup{})
	wg := v.(*sync.WaitGroup)
	if loaded {
		wg.Wait()
	} else {
		wg.Add(1)
		defer func() {
			jsa.inflight.Delete(cfg.Name)
			wg.Done()
		}()
	}

	js, isClustered := jsa.jetStreamAndClustered()
	jsa.mu.Lock()
	if mset, ok := jsa.streams[cfg.Name]; ok {
		jsa.mu.Unlock()
		// Check to see if configs are same.
		ocfg := mset.config()
		if reflect.DeepEqual(ocfg, cfg) {
			if sa != nil {
				mset.setStreamAssignment(sa)
			}
			return mset, nil
		} else {
			return nil, ApiErrors[JSStreamNameExistErr]
		}
	}
	jsa.usageMu.RLock()
	selected, tier, hasTier := jsa.selectLimits(&cfg)
	jsa.usageMu.RUnlock()
	reserved := int64(0)
	if !isClustered {
		reserved = jsa.tieredReservation(tier, &cfg)
	}
	jsa.mu.Unlock()

	if !hasTier {
		return nil, NewJSNoLimitsError()
	}
	js.mu.RLock()
	if isClustered {
		_, reserved = tieredStreamAndReservationCount(js.cluster.streams[a.Name], tier, &cfg)
	}
	if err := js.checkAllLimits(&selected, &cfg, reserved, 0); err != nil {
		js.mu.RUnlock()
		return nil, err
	}
	js.mu.RUnlock()
	jsa.mu.Lock()
	// Check for template ownership if present.
	if cfg.Template != _EMPTY_ && jsa.account != nil {
		if !jsa.checkTemplateOwnership(cfg.Template, cfg.Name) {
			jsa.mu.Unlock()
			return nil, fmt.Errorf("stream not owned by template")
		}
	}

	// Setup our internal indexed names here for sources.
	if len(cfg.Sources) > 0 {
		for _, ssi := range cfg.Sources {
			ssi.setIndexName()
		}
	}

	// Check for overlapping subjects with other streams.
	// These are not allowed for now.
	if jsa.subjectsOverlap(cfg.Subjects, nil) {
		jsa.mu.Unlock()
		return nil, NewJSStreamSubjectOverlapError()
	}

	if !hasTier {
		jsa.mu.Unlock()
		return nil, fmt.Errorf("no applicable tier found")
	}

	// Setup the internal clients.
	c := s.createInternalJetStreamClient()
	ic := s.createInternalJetStreamClient()

	qpfx := fmt.Sprintf("[ACC:%s] stream '%s' ", a.Name, config.Name)
	mset := &stream{
		acc:       a,
		jsa:       jsa,
		cfg:       cfg,
		js:        js,
		srv:       s,
		client:    c,
		sysc:      ic,
		tier:      tier,
		stype:     cfg.Storage,
		consumers: make(map[string]*consumer),
		msgs:      newIPQueue[*inMsg](s, qpfx+"messages"),
		qch:       make(chan struct{}),
		uch:       make(chan struct{}, 4),
		sch:       make(chan struct{}, 1),
	}

	// Start our signaling routine to process consumers.
	mset.sigq = newIPQueue[*cMsg](s, qpfx+"obs") // of *cMsg
	go mset.signalConsumersLoop()

	// For no-ack consumers when we are interest retention.
	if cfg.Retention != LimitsPolicy {
		mset.ackq = newIPQueue[uint64](s, qpfx+"acks")
	}

	// Check for RePublish.
	if cfg.RePublish != nil {
		// Empty same as all.
		if cfg.RePublish.Source == _EMPTY_ {
			cfg.RePublish.Source = fwcs
		}
		tr, err := newTransform(cfg.RePublish.Source, cfg.RePublish.Destination)
		if err != nil {
			jsa.mu.Unlock()
			return nil, fmt.Errorf("stream configuration for republish not valid")
		}
		// Assign our transform for republishing.
		mset.tr = tr
	}
	storeDir := filepath.Join(jsa.storeDir, streamsDir, cfg.Name)
	jsa.mu.Unlock()

	// Bind to the user account.
	c.registerWithAccount(a)
	// Bind to the system account.
	ic.registerWithAccount(s.SystemAccount())

	// Create the appropriate storage
	fsCfg := fsConfig
	if fsCfg == nil {
		fsCfg = &FileStoreConfig{}
		// If we are file based and not explicitly configured
		// we may be able to auto-tune based on max msgs or bytes.
		if cfg.Storage == FileStorage {
			mset.autoTuneFileStorageBlockSize(fsCfg)
		}
	}
	fsCfg.StoreDir = storeDir
	fsCfg.AsyncFlush = false
	fsCfg.SyncInterval = 2 * time.Minute

	if err := mset.setupStore(fsCfg); err != nil {
		mset.stop(true, false)
		return nil, NewJSStreamStoreFailedError(err)
	}

	// Create our pubAck template here. Better than json marshal each time on success.
	if domain := s.getOpts().JetStreamDomain; domain != _EMPTY_ {
		mset.pubAck = []byte(fmt.Sprintf("{%q:%q, %q:%q, %q:", "stream", cfg.Name, "domain", domain, "seq"))
	} else {
		mset.pubAck = []byte(fmt.Sprintf("{%q:%q, %q:", "stream", cfg.Name, "seq"))
	}
	end := len(mset.pubAck)
	mset.pubAck = mset.pubAck[:end:end]

	// Set our known last sequence.
	var state StreamState
	mset.store.FastState(&state)

	// Possible race with consumer.setLeader during recovery.
	mset.mu.Lock()
	mset.lseq = state.LastSeq
	mset.mu.Unlock()

	// If no msgs (new stream), set dedupe state loaded to true.
	if state.Msgs == 0 {
		mset.ddloaded = true
	}

	// Set our stream assignment if in clustered mode.
	if sa != nil {
		mset.setStreamAssignment(sa)
	}

	// Setup our internal send go routine.
	mset.setupSendCapabilities()

	// Reserve resources if MaxBytes present.
	mset.js.reserveStreamResources(&mset.cfg)

	// Call directly to set leader if not in clustered mode.
	// This can be called though before we actually setup clustering, so check both.
	if singleServerMode {
		if err := mset.setLeader(true); err != nil {
			mset.stop(true, false)
			return nil, err
		}
	}

	// This is always true in single server mode.
	if mset.IsLeader() {
		// Send advisory.
		var suppress bool
		if !s.standAloneMode() && sa == nil {
			if cfg.Replicas > 1 {
				suppress = true
			}
		} else if sa != nil {
			suppress = sa.responded
		}
		if !suppress {
			mset.sendCreateAdvisory()
		}
	}

	// Register with our account last.
	jsa.mu.Lock()
	jsa.streams[cfg.Name] = mset
	jsa.mu.Unlock()

	return mset, nil
}

// Sets the index name. Usually just the stream name but when the stream is external we will
// use additional information in case the stream names are the same.
func (ssi *StreamSource) setIndexName() {
	if ssi.External != nil {
		ssi.iname = ssi.Name + ":" + getHash(ssi.External.ApiPrefix)
	} else {
		ssi.iname = ssi.Name
	}
}

func (mset *stream) streamAssignment() *streamAssignment {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.sa
}

func (mset *stream) setStreamAssignment(sa *streamAssignment) {
	mset.mu.Lock()
	defer mset.mu.Unlock()

	mset.sa = sa
	if sa == nil {
		return
	}

	// Set our node.
	mset.node = sa.Group.node
	if mset.node != nil {
		mset.node.UpdateKnownPeers(sa.Group.Peers)
	}

	// Setup our info sub here as well for all stream members. This is now by design.
	if mset.infoSub == nil {
		isubj := fmt.Sprintf(clusterStreamInfoT, mset.jsa.acc(), mset.cfg.Name)
		// Note below the way we subscribe here is so that we can send requests to ourselves.
		mset.infoSub, _ = mset.srv.systemSubscribe(isubj, _EMPTY_, false, mset.sysc, mset.handleClusterStreamInfoRequest)
	}

	// Trigger update chan.
	select {
	case mset.uch <- struct{}{}:
	default:
	}
}

func (mset *stream) updateC() <-chan struct{} {
	if mset == nil {
		return nil
	}
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.uch
}

// IsLeader will return if we are the current leader.
func (mset *stream) IsLeader() bool {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.isLeader()
}

// Lock should be held.
func (mset *stream) isLeader() bool {
	if mset.isClustered() {
		return mset.node.Leader()
	}
	return true
}

// TODO(dlc) - Check to see if we can accept being the leader or we should should step down.
func (mset *stream) setLeader(isLeader bool) error {
	mset.mu.Lock()
	// If we are here we have a change in leader status.
	if isLeader {
		// Make sure we are listening for sync requests.
		// TODO(dlc) - Original design was that all in sync members of the group would do DQ.
		mset.startClusterSubs()
		// Setup subscriptions
		if err := mset.subscribeToStream(); err != nil {
			mset.mu.Unlock()
			return err
		}
	} else {
		// Stop responding to sync requests.
		mset.stopClusterSubs()
		// Unsubscribe from direct stream.
		mset.unsubscribeToStream(false)
		// Clear catchup state
		mset.clearAllCatchupPeers()
	}
	// Track group leader.
	if mset.isClustered() {
		mset.leader = mset.node.GroupLeader()
	} else {
		mset.leader = _EMPTY_
	}
	mset.mu.Unlock()
	return nil
}

// Lock should be held.
func (mset *stream) startClusterSubs() {
	if mset.isClustered() && mset.syncSub == nil {
		mset.syncSub, _ = mset.srv.systemSubscribe(mset.sa.Sync, _EMPTY_, false, mset.sysc, mset.handleClusterSyncRequest)
	}
}

// Lock should be held.
func (mset *stream) stopClusterSubs() {
	if mset.syncSub != nil {
		mset.srv.sysUnsubscribe(mset.syncSub)
		mset.syncSub = nil
	}
}

// account gets the account for this stream.
func (mset *stream) account() *Account {
	mset.mu.RLock()
	jsa := mset.jsa
	mset.mu.RUnlock()
	if jsa == nil {
		return nil
	}
	return jsa.acc()
}

// Helper to determine the max msg size for this stream if file based.
func (mset *stream) maxMsgSize() uint64 {
	maxMsgSize := mset.cfg.MaxMsgSize
	if maxMsgSize <= 0 {
		// Pull from the account.
		if mset.jsa != nil {
			if acc := mset.jsa.acc(); acc != nil {
				acc.mu.RLock()
				maxMsgSize = acc.mpay
				acc.mu.RUnlock()
			}
		}
		// If all else fails use default.
		if maxMsgSize <= 0 {
			maxMsgSize = MAX_PAYLOAD_SIZE
		}
	}
	// Now determine an estimation for the subjects etc.
	maxSubject := -1
	for _, subj := range mset.cfg.Subjects {
		if subjectIsLiteral(subj) {
			if len(subj) > maxSubject {
				maxSubject = len(subj)
			}
		}
	}
	if maxSubject < 0 {
		const defaultMaxSubject = 256
		maxSubject = defaultMaxSubject
	}
	// filestore will add in estimates for record headers, etc.
	return fileStoreMsgSizeEstimate(maxSubject, int(maxMsgSize))
}

// If we are file based and the file storage config was not explicitly set
// we can autotune block sizes to better match. Our target will be to store 125%
// of the theoretical limit. We will round up to nearest 100 bytes as well.
func (mset *stream) autoTuneFileStorageBlockSize(fsCfg *FileStoreConfig) {
	var totalEstSize uint64

	// MaxBytes will take precedence for now.
	if mset.cfg.MaxBytes > 0 {
		totalEstSize = uint64(mset.cfg.MaxBytes)
	} else if mset.cfg.MaxMsgs > 0 {
		// Determine max message size to estimate.
		totalEstSize = mset.maxMsgSize() * uint64(mset.cfg.MaxMsgs)
	} else if mset.cfg.MaxMsgsPer > 0 {
		fsCfg.BlockSize = uint64(defaultKVBlockSize)
		return
	} else {
		// If nothing set will let underlying filestore determine blkSize.
		return
	}

	blkSize := (totalEstSize / 4) + 1 // (25% overhead)
	// Round up to nearest 100
	if m := blkSize % 100; m != 0 {
		blkSize += 100 - m
	}
	if blkSize <= FileStoreMinBlkSize {
		blkSize = FileStoreMinBlkSize
	} else if blkSize >= FileStoreMaxBlkSize {
		blkSize = FileStoreMaxBlkSize
	} else {
		blkSize = defaultMediumBlockSize
	}
	fsCfg.BlockSize = uint64(blkSize)
}

// rebuildDedupe will rebuild any dedupe structures needed after recovery of a stream.
// Will be called lazily to avoid penalizing startup times.
// TODO(dlc) - Might be good to know if this should be checked at all for streams with no
// headers and msgId in them. Would need signaling from the storage layer.
// Lock should be held.
func (mset *stream) rebuildDedupe() {
	if mset.ddloaded {
		return
	}

	mset.ddloaded = true

	// We have some messages. Lookup starting sequence by duplicate time window.
	sseq := mset.store.GetSeqFromTime(time.Now().Add(-mset.cfg.Duplicates))
	if sseq == 0 {
		return
	}

	var smv StoreMsg
	var state StreamState
	mset.store.FastState(&state)

	for seq := sseq; seq <= state.LastSeq; seq++ {
		sm, err := mset.store.LoadMsg(seq, &smv)
		if err != nil {
			continue
		}
		var msgId string
		if len(sm.hdr) > 0 {
			if msgId = getMsgId(sm.hdr); msgId != _EMPTY_ {
				mset.storeMsgIdLocked(&ddentry{msgId, sm.seq, sm.ts})
			}
		}
		if seq == state.LastSeq {
			mset.lmsgId = msgId
		}
	}
}

func (mset *stream) lastSeqAndCLFS() (uint64, uint64) {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.lseq, mset.clfs
}

func (mset *stream) lastSeq() uint64 {
	mset.mu.RLock()
	lseq := mset.lseq
	mset.mu.RUnlock()
	return lseq
}

func (mset *stream) setLastSeq(lseq uint64) {
	mset.mu.Lock()
	mset.lseq = lseq
	mset.mu.Unlock()
}

func (mset *stream) sendCreateAdvisory() {
	mset.mu.RLock()
	name := mset.cfg.Name
	template := mset.cfg.Template
	outq := mset.outq
	srv := mset.srv
	mset.mu.RUnlock()

	if outq == nil {
		return
	}

	// finally send an event that this stream was created
	m := JSStreamActionAdvisory{
		TypedEvent: TypedEvent{
			Type: JSStreamActionAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   name,
		Action:   CreateEvent,
		Template: template,
		Domain:   srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(m)
	if err != nil {
		return
	}

	subj := JSAdvisoryStreamCreatedPre + "." + name
	outq.sendMsg(subj, j)
}

func (mset *stream) sendDeleteAdvisoryLocked() {
	if mset.outq == nil {
		return
	}

	m := JSStreamActionAdvisory{
		TypedEvent: TypedEvent{
			Type: JSStreamActionAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   mset.cfg.Name,
		Action:   DeleteEvent,
		Template: mset.cfg.Template,
		Domain:   mset.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(m)
	if err == nil {
		subj := JSAdvisoryStreamDeletedPre + "." + mset.cfg.Name
		mset.outq.sendMsg(subj, j)
	}
}

func (mset *stream) sendUpdateAdvisoryLocked() {
	if mset.outq == nil {
		return
	}

	m := JSStreamActionAdvisory{
		TypedEvent: TypedEvent{
			Type: JSStreamActionAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream: mset.cfg.Name,
		Action: ModifyEvent,
		Domain: mset.srv.getOpts().JetStreamDomain,
	}

	j, err := json.Marshal(m)
	if err == nil {
		subj := JSAdvisoryStreamUpdatedPre + "." + mset.cfg.Name
		mset.outq.sendMsg(subj, j)
	}
}

// Created returns created time.
func (mset *stream) createdTime() time.Time {
	mset.mu.RLock()
	created := mset.created
	mset.mu.RUnlock()
	return created
}

// Internal to allow creation time to be restored.
func (mset *stream) setCreatedTime(created time.Time) {
	mset.mu.Lock()
	mset.created = created
	mset.mu.Unlock()
}

// subjectsOverlap to see if these subjects overlap with existing subjects.
// Use only for non-clustered JetStream
// RLock minimum should be held.
func (jsa *jsAccount) subjectsOverlap(subjects []string, self *stream) bool {
	for _, mset := range jsa.streams {
		if self != nil && mset == self {
			continue
		}
		for _, subj := range mset.cfg.Subjects {
			for _, tsubj := range subjects {
				if SubjectsCollide(tsubj, subj) {
					return true
				}
			}
		}
	}
	return false
}

// StreamDefaultDuplicatesWindow default duplicates window.
const StreamDefaultDuplicatesWindow = 2 * time.Minute

func (s *Server) checkStreamCfg(config *StreamConfig, acc *Account) (StreamConfig, *ApiError) {
	lim := &s.getOpts().JetStreamLimits

	if config == nil {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration invalid"))
	}
	if !isValidName(config.Name) {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("stream name is required and can not contain '.', '*', '>'"))
	}
	if len(config.Name) > JSMaxNameLen {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("stream name is too long, maximum allowed is %d", JSMaxNameLen))
	}
	if len(config.Description) > JSMaxDescriptionLen {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("stream description is too long, maximum allowed is %d", JSMaxDescriptionLen))
	}

	cfg := *config

	// Make file the default.
	if cfg.Storage == 0 {
		cfg.Storage = FileStorage
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = 1
	}
	if cfg.Replicas > StreamMaxReplicas {
		return cfg, NewJSStreamInvalidConfigError(fmt.Errorf("maximum replicas is %d", StreamMaxReplicas))
	}
	if cfg.Replicas < 0 {
		return cfg, NewJSReplicasCountCannotBeNegativeError()
	}
	if cfg.MaxMsgs == 0 {
		cfg.MaxMsgs = -1
	}
	if cfg.MaxMsgsPer == 0 {
		cfg.MaxMsgsPer = -1
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = -1
	}
	if cfg.MaxMsgSize == 0 {
		cfg.MaxMsgSize = -1
	}
	if cfg.MaxConsumers == 0 {
		cfg.MaxConsumers = -1
	}
	if cfg.Duplicates == 0 && cfg.Mirror == nil {
		maxWindow := StreamDefaultDuplicatesWindow
		if lim.Duplicates > 0 && maxWindow > lim.Duplicates {
			maxWindow = lim.Duplicates
		}
		if cfg.MaxAge != 0 && cfg.MaxAge < maxWindow {
			cfg.Duplicates = cfg.MaxAge
		} else {
			cfg.Duplicates = maxWindow
		}
	}
	if cfg.MaxAge > 0 && cfg.MaxAge < 100*time.Millisecond {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("max age needs to be >= 100ms"))
	}
	if cfg.Duplicates < 0 {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("duplicates window can not be negative"))
	}
	// Check that duplicates is not larger then age if set.
	if cfg.MaxAge != 0 && cfg.Duplicates > cfg.MaxAge {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("duplicates window can not be larger then max age"))
	}
	if lim.Duplicates > 0 && cfg.Duplicates > lim.Duplicates {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("duplicates window can not be larger then server limit of %v",
			lim.Duplicates.String()))
	}
	if cfg.Duplicates > 0 && cfg.Duplicates < 100*time.Millisecond {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("duplicates window needs to be >= 100ms"))
	}

	if cfg.DenyPurge && cfg.AllowRollup {
		return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("roll-ups require the purge permission"))
	}

	// Check for new discard new per subject, we require the discard policy to also be new.
	if cfg.DiscardNewPer {
		if cfg.Discard != DiscardNew {
			return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("discard new per subject requires discard new policy to be set"))
		}
		if cfg.MaxMsgsPer <= 0 {
			return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("discard new per subject requires max msgs per subject > 0"))
		}
	}

	getStream := func(streamName string) (bool, StreamConfig) {
		var exists bool
		var cfg StreamConfig
		if s.JetStreamIsClustered() {
			if js, _ := s.getJetStreamCluster(); js != nil {
				js.mu.RLock()
				if sa := js.streamAssignment(acc.Name, streamName); sa != nil {
					cfg = *sa.Config
					exists = true
				}
				js.mu.RUnlock()
			}
		} else if mset, err := acc.lookupStream(streamName); err == nil {
			cfg = mset.cfg
			exists = true
		}
		return exists, cfg
	}
	hasStream := func(streamName string) (bool, int32, []string) {
		exists, cfg := getStream(streamName)
		return exists, cfg.MaxMsgSize, cfg.Subjects
	}

	hasFilterSubjectOverlap := func(filter string, streamSubs []string) bool {
		if filter == _EMPTY_ || len(streamSubs) == 0 {
			return true
		}
		for _, sub := range streamSubs {
			if SubjectsCollide(sub, filter) {
				return true
			}
		}
		return false
	}

	var streamSubs []string
	var deliveryPrefixes []string
	var apiPrefixes []string

	// Some errors we want to suppress on recovery.
	var isRecovering bool
	if js := s.getJetStream(); js != nil {
		isRecovering = js.isMetaRecovering()
	}

	// Do some pre-checking for mirror config to avoid cycles in clustered mode.
	if cfg.Mirror != nil {
		if len(cfg.Subjects) > 0 {
			return StreamConfig{}, NewJSMirrorWithSubjectsError()

		}
		if len(cfg.Sources) > 0 {
			return StreamConfig{}, NewJSMirrorWithSourcesError()
		}
		// We do not require other stream to exist anymore, but if we can see it check payloads.
		exists, maxMsgSize, subs := hasStream(cfg.Mirror.Name)
		if len(subs) > 0 {
			streamSubs = append(streamSubs, subs...)
		}
		if exists {
			if cfg.MaxMsgSize > 0 && maxMsgSize > 0 && cfg.MaxMsgSize < maxMsgSize {
				return StreamConfig{}, NewJSMirrorMaxMessageSizeTooBigError()
			}
			if !isRecovering && !hasFilterSubjectOverlap(cfg.Mirror.FilterSubject, subs) {
				return StreamConfig{}, NewJSStreamInvalidConfigError(
					fmt.Errorf("mirror '%s' filter subject '%s' does not overlap with any origin stream subject",
						cfg.Mirror.Name, cfg.Mirror.FilterSubject))
			}
		}
		if cfg.Mirror.External != nil {
			if cfg.Mirror.External.DeliverPrefix != _EMPTY_ {
				deliveryPrefixes = append(deliveryPrefixes, cfg.Mirror.External.DeliverPrefix)
			}
			if cfg.Mirror.External.ApiPrefix != _EMPTY_ {
				apiPrefixes = append(apiPrefixes, cfg.Mirror.External.ApiPrefix)
			}
		}
		// Determine if we are inheriting direct gets.
		if exists, ocfg := getStream(cfg.Mirror.Name); exists {
			cfg.MirrorDirect = ocfg.AllowDirect
		} else if js := s.getJetStream(); js != nil && js.isClustered() {
			// Could not find it here. If we are clustered we can look it up.
			js.mu.RLock()
			if cc := js.cluster; cc != nil {
				if as := cc.streams[acc.Name]; as != nil {
					if sa := as[cfg.Mirror.Name]; sa != nil {
						cfg.MirrorDirect = sa.Config.AllowDirect
					}
				}
			}
			js.mu.RUnlock()
		}
	}
	if len(cfg.Sources) > 0 {
		for _, src := range cfg.Sources {
			exists, maxMsgSize, subs := hasStream(src.Name)
			if len(subs) > 0 {
				streamSubs = append(streamSubs, subs...)
			}
			if exists {
				if cfg.MaxMsgSize > 0 && maxMsgSize > 0 && cfg.MaxMsgSize < maxMsgSize {
					return StreamConfig{}, NewJSSourceMaxMessageSizeTooBigError()
				}
				if !isRecovering && !hasFilterSubjectOverlap(src.FilterSubject, streamSubs) {
					return StreamConfig{}, NewJSStreamInvalidConfigError(
						fmt.Errorf("source '%s' filter subject '%s' does not overlap with any origin stream subject",
							src.Name, src.FilterSubject))
				}
			}
			if src.External == nil {
				continue
			}
			if src.External.DeliverPrefix != _EMPTY_ {
				deliveryPrefixes = append(deliveryPrefixes, src.External.DeliverPrefix)
			}
			if src.External.ApiPrefix != _EMPTY_ {
				apiPrefixes = append(apiPrefixes, src.External.ApiPrefix)
			}
		}
	}
	// check prefix overlap with subjects
	for _, pfx := range deliveryPrefixes {
		if !IsValidPublishSubject(pfx) {
			return StreamConfig{}, NewJSStreamInvalidExternalDeliverySubjError(pfx)
		}
		for _, sub := range streamSubs {
			if SubjectsCollide(sub, fmt.Sprintf("%s.%s", pfx, sub)) {
				return StreamConfig{}, NewJSStreamExternalDelPrefixOverlapsError(pfx, sub)
			}
		}
	}
	// check if api prefixes overlap
	for _, apiPfx := range apiPrefixes {
		if !IsValidPublishSubject(apiPfx) {
			return StreamConfig{}, NewJSStreamInvalidConfigError(
				fmt.Errorf("stream external api prefix %q must be a valid subject without wildcards", apiPfx))
		}
		if SubjectsCollide(apiPfx, JSApiPrefix) {
			return StreamConfig{}, NewJSStreamExternalApiOverlapError(apiPfx, JSApiPrefix)
		}
	}

	// cycle check for source cycle
	toVisit := []*StreamConfig{&cfg}
	visited := make(map[string]struct{})
	overlaps := func(subjects []string, filter string) bool {
		if filter == _EMPTY_ {
			return true
		}
		for _, subject := range subjects {
			if SubjectsCollide(subject, filter) {
				return true
			}
		}
		return false
	}

	for len(toVisit) > 0 {
		cfg := toVisit[0]
		toVisit = toVisit[1:]
		visited[cfg.Name] = struct{}{}
		for _, src := range cfg.Sources {
			if src.External != nil {
				continue
			}
			// We can detect a cycle between streams, but let's double check that the
			// subjects actually form a cycle.
			if _, ok := visited[src.Name]; ok {
				if overlaps(cfg.Subjects, src.FilterSubject) {
					return StreamConfig{}, NewJSStreamInvalidConfigError(errors.New("detected cycle"))
				}
			} else if exists, cfg := getStream(src.Name); exists {
				toVisit = append(toVisit, &cfg)
			}
		}
		// Avoid cycles hiding behind mirrors
		if m := cfg.Mirror; m != nil {
			if m.External == nil {
				if _, ok := visited[m.Name]; ok {
					return StreamConfig{}, NewJSStreamInvalidConfigError(errors.New("detected cycle"))
				}
				if exists, cfg := getStream(m.Name); exists {
					toVisit = append(toVisit, &cfg)
				}
			}
		}
	}

	if len(cfg.Subjects) == 0 {
		if cfg.Mirror == nil && len(cfg.Sources) == 0 {
			cfg.Subjects = append(cfg.Subjects, cfg.Name)
		}
	} else {
		if cfg.Mirror != nil {
			return StreamConfig{}, NewJSMirrorWithSubjectsError()
		}

		// Check for literal duplication of subject interest in config
		// and no overlap with any JS API subject space
		dset := make(map[string]struct{}, len(cfg.Subjects))
		for _, subj := range cfg.Subjects {
			if _, ok := dset[subj]; ok {
				return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("duplicate subjects detected"))
			}
			// Also check to make sure we do not overlap with our $JS API subjects.
			if subjectIsSubsetMatch(subj, "$JS.API.>") {
				return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("subjects overlap with jetstream api"))
			}
			// Make sure the subject is valid.
			if !IsValidSubject(subj) {
				return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("invalid subject"))
			}
			// Mark for duplicate check.
			dset[subj] = struct{}{}
		}
	}

	if len(cfg.Subjects) == 0 && len(cfg.Sources) == 0 && cfg.Mirror == nil {
		return StreamConfig{}, NewJSStreamInvalidConfigError(
			fmt.Errorf("stream needs at least one configured subject or be a source/mirror"))
	}

	// Check for MaxBytes required and it's limit
	if required, limit := acc.maxBytesLimits(&cfg); required && cfg.MaxBytes <= 0 {
		return StreamConfig{}, NewJSStreamMaxBytesRequiredError()
	} else if limit > 0 && cfg.MaxBytes > limit {
		return StreamConfig{}, NewJSStreamMaxStreamBytesExceededError()
	}

	// Now check if we have multiple subjects they we do not overlap ourselves
	// which would cause duplicate entries (assuming no MsgID).
	if len(cfg.Subjects) > 1 {
		for _, subj := range cfg.Subjects {
			for _, tsubj := range cfg.Subjects {
				if tsubj != subj && SubjectsCollide(tsubj, subj) {
					return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("subject %q overlaps with %q", subj, tsubj))
				}
			}
		}
	}

	// If we have a republish directive check if we can create a transform here.
	if cfg.RePublish != nil {
		// Check to make sure source is a valid subset of the subjects we have.
		// Also make sure it does not form a cycle.
		var srcValid bool
		// Empty same as all.
		if cfg.RePublish.Source == _EMPTY_ {
			cfg.RePublish.Source = fwcs
		}
		for _, subj := range cfg.Subjects {
			if SubjectsCollide(cfg.RePublish.Source, subj) {
				srcValid = true
				break
			}
		}
		if !srcValid {
			return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration for republish source is not valid subset of subjects"))
		}
		var formsCycle bool
		for _, subj := range cfg.Subjects {
			if SubjectsCollide(cfg.RePublish.Destination, subj) {
				formsCycle = true
				break
			}
		}
		if formsCycle {
			return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration for republish destination forms a cycle"))
		}
		if _, err := newTransform(cfg.RePublish.Source, cfg.RePublish.Destination); err != nil {
			return StreamConfig{}, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration for republish not valid"))
		}
	}

	return cfg, nil
}

// Config returns the stream's configuration.
func (mset *stream) config() StreamConfig {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.cfg
}

func (mset *stream) fileStoreConfig() (FileStoreConfig, error) {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	fs, ok := mset.store.(*fileStore)
	if !ok {
		return FileStoreConfig{}, ErrStoreWrongType
	}
	return fs.fileStoreConfig(), nil
}

// Do not hold jsAccount or jetStream lock
func (jsa *jsAccount) configUpdateCheck(old, new *StreamConfig, s *Server) (*StreamConfig, error) {
	cfg, apiErr := s.checkStreamCfg(new, jsa.acc())
	if apiErr != nil {
		return nil, apiErr
	}

	// Name must match.
	if cfg.Name != old.Name {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration name must match original"))
	}
	// Can't change MaxConsumers for now.
	if cfg.MaxConsumers != old.MaxConsumers {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not change MaxConsumers"))
	}
	// Can't change storage types.
	if cfg.Storage != old.Storage {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not change storage type"))
	}
	// Can't change retention.
	if cfg.Retention != old.Retention {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not change retention policy"))
	}
	// Can not have a template owner for now.
	if old.Template != _EMPTY_ {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update not allowed on template owned stream"))
	}
	if cfg.Template != _EMPTY_ {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not be owned by a template"))
	}
	// Can not change from true to false.
	if !cfg.Sealed && old.Sealed {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not unseal a sealed stream"))
	}
	// Can not change from true to false.
	if !cfg.DenyDelete && old.DenyDelete {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not cancel deny message deletes"))
	}
	// Can not change from true to false.
	if !cfg.DenyPurge && old.DenyPurge {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not cancel deny purge"))
	}
	// Check for mirror changes which are not allowed.
	if !reflect.DeepEqual(cfg.Mirror, old.Mirror) {
		return nil, NewJSStreamMirrorNotUpdatableError()
	}
	// Can't change RePublish
	if !reflect.DeepEqual(cfg.RePublish, old.RePublish) {
		return nil, NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration update can not change RePublish"))
	}

	// Check on new discard new per subject.
	if cfg.DiscardNewPer {
		if cfg.Discard != DiscardNew {
			return nil, NewJSStreamInvalidConfigError(fmt.Errorf("discard new per subject requires discard new policy to be set"))
		}
		if cfg.MaxMsgsPer <= 0 {
			return nil, NewJSStreamInvalidConfigError(fmt.Errorf("discard new per subject requires max msgs per subject > 0"))
		}
	}

	// Do some adjustments for being sealed.
	if cfg.Sealed {
		cfg.MaxAge = 0
		cfg.Discard = DiscardNew
		cfg.DenyDelete, cfg.DenyPurge = true, true
		cfg.AllowRollup = false
	}

	// Check limits. We need some extra handling to allow updating MaxBytes.

	// First, let's calculate the difference between the new and old MaxBytes.
	maxBytesDiff := cfg.MaxBytes - old.MaxBytes
	if maxBytesDiff < 0 {
		// If we're updating to a lower MaxBytes (maxBytesDiff is negative),
		// then set to zero so checkBytesLimits doesn't set addBytes to 1.
		maxBytesDiff = 0
	}
	// If maxBytesDiff == 0, then that means MaxBytes didn't change.
	// If maxBytesDiff > 0, then we want to reserve additional bytes.

	// Save the user configured MaxBytes.
	newMaxBytes := cfg.MaxBytes

	maxBytesOffset := int64(0)
	if old.MaxBytes > 0 {
		if excessRep := cfg.Replicas - old.Replicas; excessRep > 0 {
			maxBytesOffset = old.MaxBytes * int64(excessRep)
		}
	}

	// We temporarily set cfg.MaxBytes to maxBytesDiff because checkAllLimits
	// adds cfg.MaxBytes to the current reserved limit and checks if we've gone
	// over. However, we don't want an addition cfg.MaxBytes, we only want to
	// reserve the difference between the new and the old values.
	cfg.MaxBytes = maxBytesDiff

	// Check limits.
	js, isClustered := jsa.jetStreamAndClustered()
	jsa.mu.RLock()
	acc := jsa.account
	jsa.usageMu.RLock()
	selected, tier, hasTier := jsa.selectLimits(&cfg)
	if !hasTier && old.Replicas != cfg.Replicas {
		selected, tier, hasTier = jsa.selectLimits(old)
	}
	jsa.usageMu.RUnlock()
	reserved := int64(0)
	if !isClustered {
		reserved = jsa.tieredReservation(tier, &cfg)
	}
	jsa.mu.RUnlock()
	if !hasTier {
		return nil, NewJSNoLimitsError()
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	if isClustered {
		_, reserved = tieredStreamAndReservationCount(js.cluster.streams[acc.Name], tier, &cfg)
	}
	// reservation does not account for this stream, hence add the old value
	reserved += int64(old.Replicas) * old.MaxBytes
	if err := js.checkAllLimits(&selected, &cfg, reserved, maxBytesOffset); err != nil {
		return nil, err
	}
	// Restore the user configured MaxBytes.
	cfg.MaxBytes = newMaxBytes
	return &cfg, nil
}

// Update will allow certain configuration properties of an existing stream to be updated.
func (mset *stream) update(config *StreamConfig) error {
	return mset.updateWithAdvisory(config, true)
}

// Update will allow certain configuration properties of an existing stream to be updated.
func (mset *stream) updateWithAdvisory(config *StreamConfig, sendAdvisory bool) error {
	_, jsa, err := mset.acc.checkForJetStream()
	if err != nil {
		return err
	}

	mset.mu.RLock()
	ocfg := mset.cfg
	s := mset.srv
	mset.mu.RUnlock()

	cfg, err := mset.jsa.configUpdateCheck(&ocfg, config, s)
	if err != nil {
		return NewJSStreamInvalidConfigError(err, Unless(err))
	}

	jsa.mu.RLock()
	if jsa.subjectsOverlap(cfg.Subjects, mset) {
		jsa.mu.RUnlock()
		return NewJSStreamSubjectOverlapError()
	}
	jsa.mu.RUnlock()

	mset.mu.Lock()
	if mset.isLeader() {
		// Now check for subject interest differences.
		current := make(map[string]struct{}, len(ocfg.Subjects))
		for _, s := range ocfg.Subjects {
			current[s] = struct{}{}
		}
		// Update config with new values. The store update will enforce any stricter limits.

		// Now walk new subjects. All of these need to be added, but we will check
		// the originals first, since if it is in there we can skip, already added.
		for _, s := range cfg.Subjects {
			if _, ok := current[s]; !ok {
				if _, err := mset.subscribeInternal(s, mset.processInboundJetStreamMsg); err != nil {
					mset.mu.Unlock()
					return err
				}
			}
			delete(current, s)
		}
		// What is left in current needs to be deleted.
		for s := range current {
			if err := mset.unsubscribeInternal(s); err != nil {
				mset.mu.Unlock()
				return err
			}
		}

		// Check for the Duplicates
		if cfg.Duplicates != ocfg.Duplicates && mset.ddtmr != nil {
			// Let it fire right away, it will adjust properly on purge.
			mset.ddtmr.Reset(time.Microsecond)
		}

		// Check for Sources.
		if len(cfg.Sources) > 0 || len(ocfg.Sources) > 0 {
			current := make(map[string]string)
			for _, s := range ocfg.Sources {
				current[s.iname] = s.FilterSubject
			}
			for _, s := range cfg.Sources {
				s.setIndexName()
				if oFilter, ok := current[s.iname]; !ok {
					if mset.sources == nil {
						mset.sources = make(map[string]*sourceInfo)
					}
					mset.cfg.Sources = append(mset.cfg.Sources, s)
					si := &sourceInfo{name: s.Name, iname: s.iname}
					mset.sources[s.iname] = si
					mset.setStartingSequenceForSource(s.iname)
					mset.setSourceConsumer(s.iname, si.sseq+1, time.Time{})
				} else if oFilter != s.FilterSubject {
					if si, ok := mset.sources[s.iname]; ok {
						filterOverlap := true
						if oFilter != _EMPTY_ && s.FilterSubject != _EMPTY_ {
							newFilter := strings.Split(s.FilterSubject, tsep)
							oldFilter := strings.Split(oFilter, tsep)
							if !isSubsetMatchTokenized(oldFilter, newFilter) &&
								!isSubsetMatchTokenized(newFilter, oldFilter) {
								filterOverlap = false
							}
						}
						if filterOverlap {
							// si.sseq is the last message we received
							// if upstream has more messages (with a bigger sequence number)
							// that we used to filter, now we get them
							mset.setSourceConsumer(s.iname, si.sseq+1, time.Time{})
						} else {
							// since the filter has no overlap at all, we will request messages starting now
							mset.setSourceConsumer(s.iname, si.sseq+1, time.Now())
						}
					}
				}
				delete(current, s.iname)
			}
			// What is left in current needs to be deleted.
			for iname := range current {
				mset.cancelSourceConsumer(iname)
				delete(mset.sources, iname)
			}
		}
	}

	// Check for a change in allow direct status.
	// These will run on all members, so just update as appropriate here.
	// We do make sure we are caught up under monitorStream() during initial startup.
	if cfg.AllowDirect != ocfg.AllowDirect {
		if cfg.AllowDirect {
			mset.subscribeToDirect()
		} else {
			mset.unsubscribeToDirect()
		}
	}

	js := mset.js

	if targetTier := tierName(cfg); mset.tier != targetTier {
		// In cases such as R1->R3, only one update is needed
		mset.jsa.usageMu.RLock()
		_, ok := mset.jsa.limits[targetTier]
		mset.jsa.usageMu.RUnlock()
		if ok {
			// error never set
			_, reported, _ := mset.store.Utilization()
			mset.jsa.updateUsage(mset.tier, mset.stype, -int64(reported))
			mset.jsa.updateUsage(targetTier, mset.stype, int64(reported))
			mset.tier = targetTier
		}
		// else in case the new tier does not exist (say on move), keep the old tier around
		// a subsequent update to an existing tier will then move from existing past tier to existing new tier
	}

	// Now update config and store's version of our config.
	mset.cfg = *cfg

	// If we are the leader never suppress update advisory, simply send.
	if mset.isLeader() && sendAdvisory {
		mset.sendUpdateAdvisoryLocked()
	}
	mset.mu.Unlock()

	if js != nil {
		maxBytesDiff := cfg.MaxBytes - ocfg.MaxBytes
		if maxBytesDiff > 0 {
			// Reserve the difference
			js.reserveStreamResources(&StreamConfig{
				MaxBytes: maxBytesDiff,
				Storage:  cfg.Storage,
			})
		} else if maxBytesDiff < 0 {
			// Release the difference
			js.releaseStreamResources(&StreamConfig{
				MaxBytes: -maxBytesDiff,
				Storage:  ocfg.Storage,
			})
		}
	}

	mset.store.UpdateConfig(cfg)

	return nil
}

// Purge will remove all messages from the stream and underlying store based on the request.
func (mset *stream) purge(preq *JSApiStreamPurgeRequest) (purged uint64, err error) {
	mset.mu.RLock()
	if mset.client == nil || mset.store == nil {
		mset.mu.RUnlock()
		return 0, errors.New("invalid stream")
	}
	if mset.cfg.Sealed {
		mset.mu.RUnlock()
		return 0, errors.New("sealed stream")
	}
	store := mset.store
	mset.mu.RUnlock()

	if preq != nil {
		purged, err = mset.store.PurgeEx(preq.Subject, preq.Sequence, preq.Keep)
	} else {
		purged, err = mset.store.Purge()
	}
	if err != nil {
		return purged, err
	}

	// Purge consumers.
	var state StreamState
	store.FastState(&state)
	fseq, lseq := state.FirstSeq, state.LastSeq

	// Check for filtered purge.
	if preq != nil && preq.Subject != _EMPTY_ {
		ss := store.FilteredState(state.FirstSeq, preq.Subject)
		fseq = ss.First
	}

	mset.clsMu.RLock()
	for _, o := range mset.cList {
		// we update consumer sequences if:
		// no subject was specified, we can purge all consumers sequences
		if preq == nil ||
			preq.Subject == _EMPTY_ ||
			// or consumer filter subject is equal to purged subject
			preq.Subject == o.cfg.FilterSubject ||
			// or consumer subject is subset of purged subject,
			// but not the other way around.
			subjectIsSubsetMatch(o.cfg.FilterSubject, preq.Subject) {
			o.purge(fseq, lseq)
		}
	}
	mset.clsMu.RUnlock()

	return purged, nil
}

// RemoveMsg will remove a message from a stream.
// FIXME(dlc) - Should pick one and be consistent.
func (mset *stream) removeMsg(seq uint64) (bool, error) {
	return mset.deleteMsg(seq)
}

// DeleteMsg will remove a message from a stream.
func (mset *stream) deleteMsg(seq uint64) (bool, error) {
	mset.mu.RLock()
	if mset.client == nil {
		mset.mu.RUnlock()
		return false, fmt.Errorf("invalid stream")
	}
	mset.mu.RUnlock()

	return mset.store.RemoveMsg(seq)
}

// EraseMsg will securely remove a message and rewrite the data with random data.
func (mset *stream) eraseMsg(seq uint64) (bool, error) {
	mset.mu.RLock()
	if mset.client == nil {
		mset.mu.RUnlock()
		return false, fmt.Errorf("invalid stream")
	}
	mset.mu.RUnlock()
	return mset.store.EraseMsg(seq)
}

// Are we a mirror?
func (mset *stream) isMirror() bool {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.cfg.Mirror != nil
}

func (mset *stream) sourcesInfo() (sis []*StreamSourceInfo) {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	for _, si := range mset.sources {
		sis = append(sis, mset.sourceInfo(si))
	}
	return sis
}

func gatherSourceMirrorSubjects(subjects []string, cfg *StreamConfig, acc *Account) ([]string, bool) {
	var hasExt bool
	var seen map[string]bool

	if cfg.Mirror != nil {
		subjs, localHasExt := acc.streamSourceSubjects(cfg.Mirror, make(map[string]bool))
		if len(subjs) > 0 {
			subjects = append(subjects, subjs...)
		}
		if localHasExt {
			hasExt = true
		}
	} else if len(cfg.Sources) > 0 {
		seen = make(map[string]bool)
		for _, si := range cfg.Sources {
			subjs, localHasExt := acc.streamSourceSubjects(si, seen)
			if len(subjs) > 0 {
				subjects = append(subjects, subjs...)
			}
			if localHasExt {
				hasExt = true
			}
		}
	}

	return subjects, hasExt
}

// Return the subjects for a stream source.
func (a *Account) streamSourceSubjects(ss *StreamSource, seen map[string]bool) (subjects []string, hasExt bool) {
	if ss != nil && ss.External != nil {
		return nil, true
	}

	s, js, _ := a.getJetStreamFromAccount()

	if !s.JetStreamIsClustered() {
		return a.streamSourceSubjectsNotClustered(ss.Name, seen)
	} else {
		return js.streamSourceSubjectsClustered(a.Name, ss.Name, seen)
	}
}

func (js *jetStream) streamSourceSubjectsClustered(accountName, streamName string, seen map[string]bool) (subjects []string, hasExt bool) {
	if seen[streamName] {
		return nil, false
	}

	// We are clustered here so need to work through stream assignments.
	sa := js.streamAssignment(accountName, streamName)
	if sa == nil {
		return nil, false
	}
	seen[streamName] = true

	js.mu.RLock()
	cfg := sa.Config
	if len(cfg.Subjects) > 0 {
		subjects = append(subjects, cfg.Subjects...)
	}

	// Check if we need to keep going.
	var sources []*StreamSource
	if cfg.Mirror != nil {
		sources = append(sources, cfg.Mirror)
	} else if len(cfg.Sources) > 0 {
		sources = append(sources, cfg.Sources...)
	}
	js.mu.RUnlock()

	if len(sources) > 0 {
		var subjs []string
		if acc, err := js.srv.lookupAccount(accountName); err == nil {
			for _, ss := range sources {
				subjs, hasExt = acc.streamSourceSubjects(ss, seen)
				if len(subjs) > 0 {
					subjects = append(subjects, subjs...)
				}
				if hasExt {
					break
				}
			}
		}
	}

	return subjects, hasExt
}

func (a *Account) streamSourceSubjectsNotClustered(streamName string, seen map[string]bool) (subjects []string, hasExt bool) {
	if seen[streamName] {
		return nil, false
	}

	mset, err := a.lookupStream(streamName)
	if err != nil {
		return nil, false
	}
	seen[streamName] = true

	cfg := mset.config()
	if len(cfg.Subjects) > 0 {
		subjects = append(subjects, cfg.Subjects...)
	}

	var subjs []string
	if cfg.Mirror != nil {
		subjs, hasExt = a.streamSourceSubjects(cfg.Mirror, seen)
		if len(subjs) > 0 {
			subjects = append(subjects, subjs...)
		}
	} else if len(cfg.Sources) > 0 {
		for _, si := range cfg.Sources {
			subjs, hasExt = a.streamSourceSubjects(si, seen)
			if len(subjs) > 0 {
				subjects = append(subjects, subjs...)
			}
			if hasExt {
				break
			}
		}
	}

	return subjects, hasExt
}

// Lock should be held
func (mset *stream) sourceInfo(si *sourceInfo) *StreamSourceInfo {
	if si == nil {
		return nil
	}

	ssi := &StreamSourceInfo{Name: si.name, Lag: si.lag, Error: si.err}
	// If we have not heard from the source, set Active to -1.
	if si.last.IsZero() {
		ssi.Active = -1
	} else {
		ssi.Active = time.Since(si.last)
	}

	var ext *ExternalStream
	if mset.cfg.Mirror != nil {
		ext = mset.cfg.Mirror.External
	} else if ss := mset.streamSource(si.iname); ss != nil && ss.External != nil {
		ext = ss.External
	}
	if ext != nil {
		ssi.External = &ExternalStream{
			ApiPrefix:     ext.ApiPrefix,
			DeliverPrefix: ext.DeliverPrefix,
		}
	}
	return ssi
}

// Return our source info for our mirror.
func (mset *stream) mirrorInfo() *StreamSourceInfo {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.sourceInfo(mset.mirror)
}

const sourceHealthCheckInterval = 1 * time.Second

// Will run as a Go routine to process mirror consumer messages.
func (mset *stream) processMirrorMsgs(mirror *sourceInfo, ready *sync.WaitGroup) {
	s := mset.srv
	defer func() {
		mirror.wg.Done()
		s.grWG.Done()
	}()

	// Grab stream quit channel.
	mset.mu.Lock()
	msgs, qch, siqch := mirror.msgs, mset.qch, mirror.qch
	// Set the last seen as now so that we don't fail at the first check.
	mirror.last = time.Now()
	mset.mu.Unlock()

	// Signal the caller that we have captured the above fields.
	ready.Done()

	t := time.NewTicker(sourceHealthCheckInterval)
	defer t.Stop()

	for {
		select {
		case <-s.quitCh:
			return
		case <-qch:
			return
		case <-siqch:
			return
		case <-msgs.ch:
			ims := msgs.pop()
			for _, im := range ims {
				if !mset.processInboundMirrorMsg(im) {
					break
				}
			}
			msgs.recycle(&ims)
		case <-t.C:
			mset.mu.RLock()
			isLeader := mset.isLeader()
			stalled := mset.mirror != nil && time.Since(mset.mirror.last) > 3*sourceHealthCheckInterval
			mset.mu.RUnlock()
			// No longer leader.
			if !isLeader {
				mset.mu.Lock()
				mset.cancelMirrorConsumer()
				mset.mu.Unlock()
				return
			}
			// We are stalled.
			if stalled {
				mset.retryMirrorConsumer()
			}
		}
	}
}

// Checks that the message is from our current direct consumer. We can not depend on sub comparison
// since cross account imports break.
func (si *sourceInfo) isCurrentSub(reply string) bool {
	return si.cname != _EMPTY_ && strings.HasPrefix(reply, jsAckPre) && si.cname == tokenAt(reply, 4)
}

// processInboundMirrorMsg handles processing messages bound for a stream.
func (mset *stream) processInboundMirrorMsg(m *inMsg) bool {
	mset.mu.Lock()
	if mset.mirror == nil {
		mset.mu.Unlock()
		return false
	}
	if !mset.isLeader() {
		mset.cancelMirrorConsumer()
		mset.mu.Unlock()
		return false
	}

	isControl := m.isControlMsg()

	// Ignore from old subscriptions.
	// The reason we can not just compare subs is that on cross account imports they will not match.
	if !mset.mirror.isCurrentSub(m.rply) && !isControl {
		mset.mu.Unlock()
		return false
	}

	mset.mirror.last = time.Now()
	node := mset.node

	// Check for heartbeats and flow control messages.
	if isControl {
		var needsRetry bool
		// Flow controls have reply subjects.
		if m.rply != _EMPTY_ {
			mset.handleFlowControl(mset.mirror, m)
		} else {
			// For idle heartbeats make sure we did not miss anything and check if we are considered stalled.
			if ldseq := parseInt64(getHeader(JSLastConsumerSeq, m.hdr)); ldseq > 0 && uint64(ldseq) != mset.mirror.dseq {
				needsRetry = true
			} else if fcReply := getHeader(JSConsumerStalled, m.hdr); len(fcReply) > 0 {
				// Other side thinks we are stalled, so send flow control reply.
				mset.outq.sendMsg(string(fcReply), nil)
			}
		}
		mset.mu.Unlock()
		if needsRetry {
			mset.retryMirrorConsumer()
		}
		return !needsRetry
	}

	sseq, dseq, dc, ts, pending := replyInfo(m.rply)

	if dc > 1 {
		mset.mu.Unlock()
		return false
	}

	// Mirror info tracking.
	olag, osseq, odseq := mset.mirror.lag, mset.mirror.sseq, mset.mirror.dseq
	if sseq == mset.mirror.sseq+1 {
		mset.mirror.dseq = dseq
		mset.mirror.sseq++
	} else if sseq <= mset.mirror.sseq {
		// Ignore older messages.
		mset.mu.Unlock()
		return true
	} else if mset.mirror.cname == _EMPTY_ {
		mset.mirror.cname = tokenAt(m.rply, 4)
		mset.mirror.dseq, mset.mirror.sseq = dseq, sseq
	} else {
		// If the deliver sequence matches then the upstream stream has expired or deleted messages.
		if dseq == mset.mirror.dseq+1 {
			mset.skipMsgs(mset.mirror.sseq+1, sseq-1)
			mset.mirror.dseq++
			mset.mirror.sseq = sseq
		} else {
			mset.mu.Unlock()
			mset.retryMirrorConsumer()
			return false
		}
	}

	if pending == 0 {
		mset.mirror.lag = 0
	} else {
		mset.mirror.lag = pending - 1
	}

	// Check if we allow mirror direct here. If so check they we have mostly caught up.
	// The reason we do not require 0 is if the source is active we may always be slightly behind.
	if mset.cfg.MirrorDirect && mset.mirror.dsub == nil && pending < dgetCaughtUpThresh {
		if err := mset.subscribeToMirrorDirect(); err != nil {
			// Disable since we had problems above.
			mset.cfg.MirrorDirect = false
		}
	}

	js, stype := mset.js, mset.cfg.Storage
	mset.mu.Unlock()

	s := mset.srv
	var err error
	if node != nil {
		if js.limitsExceeded(stype) {
			s.resourcesExeededError()
			err = ApiErrors[JSInsufficientResourcesErr]
		} else {
			err = node.Propose(encodeStreamMsg(m.subj, _EMPTY_, m.hdr, m.msg, sseq-1, ts))
		}
	} else {
		err = mset.processJetStreamMsg(m.subj, _EMPTY_, m.hdr, m.msg, sseq-1, ts)
	}
	if err != nil {
		if strings.Contains(err.Error(), "no space left") {
			s.Errorf("JetStream out of space, will be DISABLED")
			s.DisableJetStream()
			return false
		}
		if err != errLastSeqMismatch {
			mset.mu.RLock()
			accName, sname := mset.acc.Name, mset.cfg.Name
			mset.mu.RUnlock()
			s.RateLimitWarnf("Error processing inbound mirror message for '%s' > '%s': %v",
				accName, sname, err)
		} else {
			// We may have missed messages, restart.
			if sseq <= mset.lastSeq() {
				mset.mu.Lock()
				mset.mirror.lag = olag
				mset.mirror.sseq = osseq
				mset.mirror.dseq = odseq
				mset.mu.Unlock()
				return false
			} else {
				mset.mu.Lock()
				mset.mirror.dseq = odseq
				mset.mirror.sseq = osseq
				mset.mu.Unlock()
				mset.retryMirrorConsumer()
			}
		}
	}
	return err == nil
}

func (mset *stream) setMirrorErr(err *ApiError) {
	mset.mu.Lock()
	if mset.mirror != nil {
		mset.mirror.err = err
	}
	mset.mu.Unlock()
}

// Cancels a mirror consumer.
//
// Lock held on entry
func (mset *stream) cancelMirrorConsumer() {
	if mset.mirror == nil {
		return
	}
	mset.cancelSourceInfo(mset.mirror)
}

// Similar to setupMirrorConsumer except that it will print a debug statement
// indicating that there is a retry.
//
// Lock is acquired in this function
func (mset *stream) retryMirrorConsumer() error {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	mset.srv.Debugf("Retrying mirror consumer for '%s > %s'", mset.acc.Name, mset.cfg.Name)
	return mset.setupMirrorConsumer()
}

// Lock should be held.
func (mset *stream) skipMsgs(start, end uint64) {
	node, store := mset.node, mset.store
	var entries []*Entry
	for seq := start; seq <= end; seq++ {
		if node != nil {
			entries = append(entries, &Entry{EntryNormal, encodeStreamMsg(_EMPTY_, _EMPTY_, nil, nil, seq-1, 0)})
			// So a single message does not get too big.
			if len(entries) > 10_000 {
				node.ProposeDirect(entries)
				// We need to re-craete `entries` because there is a reference
				// to it in the node's pae map.
				entries = entries[:0]
			}
		} else {
			mset.lseq = store.SkipMsg()
		}
	}
	// Send all at once.
	if node != nil && len(entries) > 0 {
		node.ProposeDirect(entries)
	}
}

// This will schedule a call to setupMirrorConsumer, taking into account the last
// time it was retried and determine the soonest setSourceConsumer can be called
// without tripping the sourceConsumerRetryThreshold.
// The mset.mirror pointer has been verified to be not nil by the caller.
//
// Lock held on entry
func (mset *stream) scheduleSetupMirrorConsumerRetryAsap() {
	// We are trying to figure out how soon we can retry. setupMirrorConsumer will reject
	// a retry if last was done less than "sourceConsumerRetryThreshold" ago.
	next := sourceConsumerRetryThreshold - time.Since(mset.mirror.lreq)
	if next < 0 {
		// It means that we have passed the threshold and so we are ready to go.
		next = 0
	}
	// To make *sure* that the next request will not fail, add a bit of buffer
	// and some randomness.
	next += time.Duration(rand.Intn(50)) + 10*time.Millisecond
	time.AfterFunc(next, func() {
		mset.mu.Lock()
		mset.setupMirrorConsumer()
		mset.mu.Unlock()
	})
}

// Setup our mirror consumer.
// Lock should be held.
func (mset *stream) setupMirrorConsumer() error {
	if mset.outq == nil {
		return errors.New("outq required")
	}
	// We use to prevent update of a mirror configuration in cluster
	// mode but not in standalone. This is now fixed. However, without
	// rejecting the update, it could be that if the source stream was
	// removed and then later the mirrored stream config changed to
	// remove mirror configuration, this function would panic when
	// accessing mset.cfg.Mirror fields. Adding this protection in case
	// we allow in the future the mirror config to be changed (removed).
	if mset.cfg.Mirror == nil {
		return errors.New("invalid mirror configuration")
	}

	// If this is the first time
	if mset.mirror == nil {
		mset.mirror = &sourceInfo{name: mset.cfg.Mirror.Name}
	} else {
		mset.cancelSourceInfo(mset.mirror)
		mset.mirror.sseq = mset.lseq

		// If we are no longer the leader stop trying.
		if !mset.isLeader() {
			return nil
		}
	}
	mirror := mset.mirror

	// We want to throttle here in terms of how fast we request new consumers,
	// or if the previous is still in progress.
	if last := time.Since(mirror.lreq); last < sourceConsumerRetryThreshold || mirror.sip {
		mset.scheduleSetupMirrorConsumerRetryAsap()
		return nil
	}
	mirror.lreq = time.Now()

	// Determine subjects etc.
	var deliverSubject string
	ext := mset.cfg.Mirror.External

	if ext != nil && ext.DeliverPrefix != _EMPTY_ {
		deliverSubject = strings.ReplaceAll(ext.DeliverPrefix+syncSubject(".M"), "..", ".")
	} else {
		deliverSubject = syncSubject("$JS.M")
	}

	// Now send off request to create/update our consumer. This will be all API based even in single server mode.
	// We calculate durable names apriori so we do not need to save them off.

	var state StreamState
	mset.store.FastState(&state)

	req := &CreateConsumerRequest{
		Stream: mset.cfg.Mirror.Name,
		Config: ConsumerConfig{
			DeliverSubject: deliverSubject,
			DeliverPolicy:  DeliverByStartSequence,
			OptStartSeq:    state.LastSeq + 1,
			AckPolicy:      AckNone,
			AckWait:        22 * time.Hour,
			MaxDeliver:     1,
			Heartbeat:      sourceHealthCheckInterval,
			FlowControl:    true,
			Direct:         true,
		},
	}

	// Only use start optionals on first time.
	if state.Msgs == 0 && state.FirstSeq == 0 {
		req.Config.OptStartSeq = 0
		if mset.cfg.Mirror.OptStartSeq > 0 {
			req.Config.OptStartSeq = mset.cfg.Mirror.OptStartSeq
		} else if mset.cfg.Mirror.OptStartTime != nil {
			req.Config.OptStartTime = mset.cfg.Mirror.OptStartTime
			req.Config.DeliverPolicy = DeliverByStartTime
		}
	}
	if req.Config.OptStartSeq == 0 && req.Config.OptStartTime == nil {
		// If starting out and lastSeq is 0.
		req.Config.DeliverPolicy = DeliverAll
	}

	// Filters
	if mset.cfg.Mirror.FilterSubject != _EMPTY_ {
		req.Config.FilterSubject = mset.cfg.Mirror.FilterSubject
	}

	respCh := make(chan *JSApiConsumerCreateResponse, 1)
	reply := infoReplySubject()
	crSub, err := mset.subscribeInternal(reply, func(sub *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
		mset.unsubscribeUnlocked(sub)
		_, msg := c.msgParts(rmsg)

		var ccr JSApiConsumerCreateResponse
		if err := json.Unmarshal(msg, &ccr); err != nil {
			c.Warnf("JetStream bad mirror consumer create response: %q", msg)
			mset.setMirrorErr(ApiErrors[JSInvalidJSONErr])
			return
		}
		respCh <- &ccr
	})
	if err != nil {
		mirror.err = NewJSMirrorConsumerSetupFailedError(err, Unless(err))
		mset.scheduleSetupMirrorConsumerRetryAsap()
		return nil
	}

	b, _ := json.Marshal(req)

	var subject string
	if req.Config.FilterSubject != _EMPTY_ {
		req.Config.Name = fmt.Sprintf("mirror-%s", createConsumerName())
		subject = fmt.Sprintf(JSApiConsumerCreateExT, mset.cfg.Mirror.Name, req.Config.Name, req.Config.FilterSubject)
	} else {
		subject = fmt.Sprintf(JSApiConsumerCreateT, mset.cfg.Mirror.Name)
	}
	if ext != nil {
		subject = strings.Replace(subject, JSApiPrefix, ext.ApiPrefix, 1)
		subject = strings.ReplaceAll(subject, "..", ".")
	}

	// We need to create the subscription that will receive the messages prior
	// to sending the consumer create request, because in some complex topologies
	// with gateways and optimistic mode, it is possible that the consumer starts
	// delivering messages as soon as the consumer request is received.
	qname := fmt.Sprintf("[ACC:%s] stream mirror '%s' of '%s' msgs", mset.acc.Name, mset.cfg.Name, mset.cfg.Mirror.Name)
	// Create a new queue each time
	mirror.msgs = newIPQueue[*inMsg](mset.srv, qname)
	msgs := mirror.msgs
	sub, err := mset.subscribeInternal(deliverSubject, func(sub *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
		hdr, msg := c.msgParts(copyBytes(rmsg)) // Need to copy.
		mset.queueInbound(msgs, subject, reply, hdr, msg)
	})
	if err != nil {
		mirror.err = NewJSMirrorConsumerSetupFailedError(err, Unless(err))
		mset.unsubscribeUnlocked(crSub)
		mset.scheduleSetupMirrorConsumerRetryAsap()
		return nil
	}
	mirror.err = nil
	mirror.sub = sub
	mirror.sip = true

	// Send the consumer create request
	mset.outq.send(newJSPubMsg(subject, _EMPTY_, reply, nil, b, nil, 0))

	go func() {

		var retry bool
		defer func() {
			mset.mu.Lock()
			// Check that this is still valid and if so, clear the "setup in progress" flag.
			if mset.mirror != nil {
				mset.mirror.sip = false
				// If we need to retry, schedule now
				if retry {
					mset.scheduleSetupMirrorConsumerRetryAsap()
				}
			}
			mset.mu.Unlock()
		}()

		// Wait for previous processMirrorMsgs go routine to be completely done.
		// If none is running, this will not block.
		mirror.wg.Wait()

		select {
		case ccr := <-respCh:
			mset.mu.Lock()
			// Mirror config has been removed.
			if mset.mirror == nil {
				mset.mu.Unlock()
				return
			}
			ready := sync.WaitGroup{}
			mirror := mset.mirror
			mirror.err = nil
			if ccr.Error != nil || ccr.ConsumerInfo == nil {
				mset.srv.Warnf("JetStream error response for create mirror consumer: %+v", ccr.Error)
				mirror.err = ccr.Error
				// Let's retry as soon as possible, but we are gated by sourceConsumerRetryThreshold
				retry = true
			} else {

				// When an upstream stream expires messages or in general has messages that we want
				// that are no longer available we need to adjust here.
				var state StreamState
				mset.store.FastState(&state)

				// Check if we need to skip messages.
				if state.LastSeq != ccr.ConsumerInfo.Delivered.Stream {
					mset.skipMsgs(state.LastSeq+1, ccr.ConsumerInfo.Delivered.Stream)
				}

				// Capture consumer name.
				mirror.cname = ccr.ConsumerInfo.Name
				mirror.dseq = 0
				mirror.sseq = ccr.ConsumerInfo.Delivered.Stream
				mirror.qch = make(chan struct{})
				mirror.wg.Add(1)
				ready.Add(1)
				if !mset.srv.startGoRoutine(func() { mset.processMirrorMsgs(mirror, &ready) }) {
					ready.Done()
				}
			}
			mset.mu.Unlock()
			ready.Wait()
		case <-time.After(5 * time.Second):
			mset.unsubscribeUnlocked(crSub)
			// We already waited 5 seconds, let's retry now.
			retry = true
		}
	}()

	return nil
}

func (mset *stream) streamSource(iname string) *StreamSource {
	for _, ssi := range mset.cfg.Sources {
		if ssi.iname == iname {
			return ssi
		}
	}
	return nil
}

func (mset *stream) retrySourceConsumer(sname string) {
	mset.mu.Lock()
	defer mset.mu.Unlock()

	si := mset.sources[sname]
	if si == nil {
		return
	}
	mset.setStartingSequenceForSource(sname)
	mset.retrySourceConsumerAtSeq(sname, si.sseq+1)
}

// Same than setSourceConsumer but simply issue a debug statement indicating
// that there is a retry.
//
// Lock should be held.
func (mset *stream) retrySourceConsumerAtSeq(iname string, seq uint64) {
	s := mset.srv

	s.Debugf("Retrying source consumer for '%s > %s'", mset.acc.Name, mset.cfg.Name)

	// setSourceConsumer will check that the source is still configured.
	mset.setSourceConsumer(iname, seq, time.Time{})
}

// Lock should be held.
func (mset *stream) cancelSourceConsumer(iname string) {
	if si := mset.sources[iname]; si != nil {
		mset.cancelSourceInfo(si)
		si.sseq, si.dseq = 0, 0
	}
}

// The `si` has been verified to be not nil. The sourceInfo's sub will
// be unsubscribed and set to nil (if not already done) and the
// cname will be reset. The message processing's go routine quit channel
// will be closed if still opened.
//
// Lock should be held
func (mset *stream) cancelSourceInfo(si *sourceInfo) {
	if si.sub != nil {
		mset.unsubscribe(si.sub)
		si.sub = nil
	}
	// In case we had a mirror direct subscription.
	if si.dsub != nil {
		mset.unsubscribe(si.dsub)
		si.dsub = nil
	}
	mset.removeInternalConsumer(si)
	if si.qch != nil {
		close(si.qch)
		si.qch = nil
	}
	si.msgs.drain()
	si.msgs.unregister()
}

const sourceConsumerRetryThreshold = 2 * time.Second

// This will schedule a call to setSourceConsumer, taking into account the last
// time it was retried and determine the soonest setSourceConsumer can be called
// without tripping the sourceConsumerRetryThreshold.
//
// Lock held on entry
func (mset *stream) scheduleSetSourceConsumerRetryAsap(si *sourceInfo, seq uint64, startTime time.Time) {
	// We are trying to figure out how soon we can retry. setSourceConsumer will reject
	// a retry if last was done less than "sourceConsumerRetryThreshold" ago.
	next := sourceConsumerRetryThreshold - time.Since(si.lreq)
	if next < 0 {
		// It means that we have passed the threshold and so we are ready to go.
		next = 0
	}
	// To make *sure* that the next request will not fail, add a bit of buffer
	// and some randomness.
	next += time.Duration(rand.Intn(50)) + 10*time.Millisecond
	mset.scheduleSetSourceConsumerRetry(si.iname, seq, next, startTime)
}

// Simply schedules setSourceConsumer at the given delay.
//
// Does not require lock
func (mset *stream) scheduleSetSourceConsumerRetry(iname string, seq uint64, delay time.Duration, startTime time.Time) {
	time.AfterFunc(delay, func() {
		mset.mu.Lock()
		mset.setSourceConsumer(iname, seq, startTime)
		mset.mu.Unlock()
	})
}

// Lock should be held.
func (mset *stream) setSourceConsumer(iname string, seq uint64, startTime time.Time) {
	si := mset.sources[iname]
	if si == nil {
		return
	}
	// Cancel previous instance if applicable
	mset.cancelSourceInfo(si)

	ssi := mset.streamSource(iname)
	if ssi == nil {
		return
	}

	// We want to throttle here in terms of how fast we request new consumers,
	// or if the previous is still in progress.
	if last := time.Since(si.lreq); last < sourceConsumerRetryThreshold || si.sip {
		mset.scheduleSetSourceConsumerRetryAsap(si, seq, startTime)
		return
	}
	si.lreq = time.Now()

	// Determine subjects etc.
	var deliverSubject string
	ext := ssi.External

	if ext != nil && ext.DeliverPrefix != _EMPTY_ {
		deliverSubject = strings.ReplaceAll(ext.DeliverPrefix+syncSubject(".S"), "..", ".")
	} else {
		deliverSubject = syncSubject("$JS.S")
	}

	req := &CreateConsumerRequest{
		Stream: si.name,
		Config: ConsumerConfig{
			DeliverSubject: deliverSubject,
			AckPolicy:      AckNone,
			AckWait:        22 * time.Hour,
			MaxDeliver:     1,
			Heartbeat:      sourceHealthCheckInterval,
			FlowControl:    true,
			Direct:         true,
		},
	}

	// If starting, check any configs.
	if !startTime.IsZero() && seq > 1 {
		req.Config.OptStartTime = &startTime
		req.Config.DeliverPolicy = DeliverByStartTime
	} else if seq <= 1 {
		if ssi.OptStartSeq > 0 {
			req.Config.OptStartSeq = ssi.OptStartSeq
			req.Config.DeliverPolicy = DeliverByStartSequence
		} else if ssi.OptStartTime != nil {
			// Check to see if our configured start is before what we remember.
			// Applicable on restart similar to below.
			if ssi.OptStartTime.Before(si.start) {
				req.Config.OptStartTime = &si.start
			} else {
				req.Config.OptStartTime = ssi.OptStartTime
			}
			req.Config.DeliverPolicy = DeliverByStartTime
		} else if !si.start.IsZero() {
			// We are falling back to time based startup on a recover, but our messages are gone. e.g. purge, expired, retention policy.
			req.Config.OptStartTime = &si.start
			req.Config.DeliverPolicy = DeliverByStartTime
		}
	} else {
		req.Config.OptStartSeq = seq
		req.Config.DeliverPolicy = DeliverByStartSequence
	}
	// Filters
	if ssi.FilterSubject != _EMPTY_ {
		req.Config.FilterSubject = ssi.FilterSubject
	}

	respCh := make(chan *JSApiConsumerCreateResponse, 1)
	reply := infoReplySubject()
	crSub, err := mset.subscribeInternal(reply, func(sub *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
		mset.unsubscribeUnlocked(sub)
		_, msg := c.msgParts(rmsg)
		var ccr JSApiConsumerCreateResponse
		if err := json.Unmarshal(msg, &ccr); err != nil {
			c.Warnf("JetStream bad source consumer create response: %q", msg)
			return
		}
		respCh <- &ccr
	})
	if err != nil {
		si.err = NewJSSourceConsumerSetupFailedError(err, Unless(err))
		mset.scheduleSetSourceConsumerRetryAsap(si, seq, startTime)
		return
	}

	var subject string
	if req.Config.FilterSubject != _EMPTY_ {
		req.Config.Name = fmt.Sprintf("src-%s", createConsumerName())
		subject = fmt.Sprintf(JSApiConsumerCreateExT, si.name, req.Config.Name, req.Config.FilterSubject)
	} else {
		subject = fmt.Sprintf(JSApiConsumerCreateT, si.name)
	}
	if ext != nil {
		subject = strings.Replace(subject, JSApiPrefix, ext.ApiPrefix, 1)
		subject = strings.ReplaceAll(subject, "..", ".")
	}

	// Marshal request.
	b, _ := json.Marshal(req)

	// We need to create the subscription that will receive the messages prior
	// to sending the consumer create request, because in some complex topologies
	// with gateways and optimistic mode, it is possible that the consumer starts
	// delivering messages as soon as the consumer request is received.
	qname := fmt.Sprintf("[ACC:%s] stream source '%s' from '%s' msgs", mset.acc.Name, mset.cfg.Name, si.name)
	// Create a new queue each time
	si.msgs = newIPQueue[*inMsg](mset.srv, qname)
	msgs := si.msgs
	sub, err := mset.subscribeInternal(deliverSubject, func(sub *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
		hdr, msg := c.msgParts(copyBytes(rmsg)) // Need to copy.
		mset.queueInbound(msgs, subject, reply, hdr, msg)
	})
	if err != nil {
		si.err = NewJSSourceConsumerSetupFailedError(err, Unless(err))
		mset.unsubscribeUnlocked(crSub)
		mset.scheduleSetSourceConsumerRetryAsap(si, seq, startTime)
		return
	}
	si.err = nil
	si.sub = sub
	si.sip = true

	// Send the consumer create request
	mset.outq.send(newJSPubMsg(subject, _EMPTY_, reply, nil, b, nil, 0))

	go func() {

		var retry bool
		defer func() {
			mset.mu.Lock()
			// Check that this is still valid and if so, clear the "setup in progress" flag.
			if si := mset.sources[iname]; si != nil {
				si.sip = false
				// If we need to retry, schedule now
				if retry {
					mset.scheduleSetSourceConsumerRetryAsap(si, seq, startTime)
				}
			}
			mset.mu.Unlock()
		}()

		// Wait for previous processSourceMsgs go routine to be completely done.
		// If none is running, this will not block.
		si.wg.Wait()

		select {
		case ccr := <-respCh:
			ready := sync.WaitGroup{}
			mset.mu.Lock()
			// Check that it has not been removed or canceled (si.sub would be nil)
			if si := mset.sources[iname]; si != nil && si.sub != nil {
				si.err = nil
				if ccr.Error != nil || ccr.ConsumerInfo == nil {
					mset.srv.Warnf("JetStream error response for create source consumer: %+v", ccr.Error)
					si.err = ccr.Error
					// Let's retry as soon as possible, but we are gated by sourceConsumerRetryThreshold
					retry = true
				} else {
					if si.sseq != ccr.ConsumerInfo.Delivered.Stream {
						si.sseq = ccr.ConsumerInfo.Delivered.Stream + 1
					}
					// Capture consumer name.
					si.cname = ccr.ConsumerInfo.Name
					// Do not set si.sseq to seq here. si.sseq will be set in processInboundSourceMsg
					si.dseq = 0
					si.qch = make(chan struct{})
					si.wg.Add(1)
					ready.Add(1)
					if !mset.srv.startGoRoutine(func() { mset.processSourceMsgs(si, &ready) }) {
						ready.Done()
					}
				}
			}
			mset.mu.Unlock()
			ready.Wait()
		case <-time.After(5 * time.Second):
			mset.unsubscribeUnlocked(crSub)
			// We already waited 5 seconds, let's retry now.
			retry = true
		}
	}()
}

func (mset *stream) processSourceMsgs(si *sourceInfo, ready *sync.WaitGroup) {
	s := mset.srv
	defer func() {
		si.wg.Done()
		s.grWG.Done()
	}()

	// Grab some stream and sourceInfo values now...
	mset.mu.Lock()
	msgs, qch, siqch, iname := si.msgs, mset.qch, si.qch, si.iname
	// Set the last seen as now so that we don't fail at the first check.
	si.last = time.Now()
	mset.mu.Unlock()

	// Signal the caller that we have captured the above fields.
	ready.Done()

	t := time.NewTicker(sourceHealthCheckInterval)
	defer t.Stop()

	for {
		select {
		case <-s.quitCh:
			return
		case <-qch:
			return
		case <-siqch:
			return
		case <-msgs.ch:
			ims := msgs.pop()
			for _, im := range ims {
				if !mset.processInboundSourceMsg(si, im) {
					break
				}
			}
			msgs.recycle(&ims)
		case <-t.C:
			mset.mu.RLock()
			isLeader := mset.isLeader()
			stalled := time.Since(si.last) > 3*sourceHealthCheckInterval
			mset.mu.RUnlock()
			// No longer leader.
			if !isLeader {
				mset.mu.Lock()
				mset.cancelSourceConsumer(iname)
				mset.mu.Unlock()
				return
			}
			// We are stalled.
			if stalled {
				mset.mu.Lock()
				// We don't need to schedule here, we are going to simply
				// call setSourceConsumer with the current state+1.
				mset.setSourceConsumer(iname, si.sseq+1, time.Time{})
				mset.mu.Unlock()
			}
		}
	}
}

// isControlMsg determines if this is a control message.
func (m *inMsg) isControlMsg() bool {
	return len(m.msg) == 0 && len(m.hdr) > 0 && bytes.HasPrefix(m.hdr, []byte("NATS/1.0 100 "))
}

// Sends a reply to a flow control request.
func (mset *stream) sendFlowControlReply(reply string) {
	mset.mu.RLock()
	if mset.isLeader() && mset.outq != nil {
		mset.outq.sendMsg(reply, nil)
	}
	mset.mu.RUnlock()
}

// handleFlowControl will properly handle flow control messages for both R==1 and R>1.
// Lock should be held.
func (mset *stream) handleFlowControl(si *sourceInfo, m *inMsg) {
	// If we are clustered we will send the flow control message through the replication stack.
	if mset.isClustered() {
		mset.node.Propose(encodeStreamMsg(_EMPTY_, m.rply, m.hdr, nil, 0, 0))
	} else {
		mset.outq.sendMsg(m.rply, nil)
	}
}

// processInboundSourceMsg handles processing other stream messages bound for this stream.
func (mset *stream) processInboundSourceMsg(si *sourceInfo, m *inMsg) bool {
	mset.mu.Lock()

	// If we are no longer the leader cancel this subscriber.
	if !mset.isLeader() {
		mset.cancelSourceConsumer(si.iname)
		mset.mu.Unlock()
		return false
	}

	isControl := m.isControlMsg()

	// Ignore from old subscriptions.
	if !si.isCurrentSub(m.rply) && !isControl {
		mset.mu.Unlock()
		return false
	}

	si.last = time.Now()
	node := mset.node

	// Check for heartbeats and flow control messages.
	if isControl {
		var needsRetry bool
		// Flow controls have reply subjects.
		if m.rply != _EMPTY_ {
			mset.handleFlowControl(si, m)
		} else {
			// For idle heartbeats make sure we did not miss anything.
			if ldseq := parseInt64(getHeader(JSLastConsumerSeq, m.hdr)); ldseq > 0 && uint64(ldseq) != si.dseq {
				needsRetry = true
				mset.retrySourceConsumerAtSeq(si.iname, si.sseq+1)
			} else if fcReply := getHeader(JSConsumerStalled, m.hdr); len(fcReply) > 0 {
				// Other side thinks we are stalled, so send flow control reply.
				mset.outq.sendMsg(string(fcReply), nil)
			}
		}
		mset.mu.Unlock()
		return !needsRetry
	}

	sseq, dseq, dc, _, pending := replyInfo(m.rply)

	if dc > 1 {
		mset.mu.Unlock()
		return false
	}

	// Tracking is done here.
	if dseq == si.dseq+1 {
		si.dseq++
		si.sseq = sseq
	} else if dseq > si.dseq {
		if si.cname == _EMPTY_ {
			si.cname = tokenAt(m.rply, 4)
			si.dseq, si.sseq = dseq, sseq
		} else {
			mset.retrySourceConsumerAtSeq(si.iname, si.sseq+1)
			mset.mu.Unlock()
			return false
		}
	} else {
		mset.mu.Unlock()
		return false
	}

	if pending == 0 {
		si.lag = 0
	} else {
		si.lag = pending - 1
	}
	mset.mu.Unlock()

	hdr, msg := m.hdr, m.msg

	// If we are daisy chained here make sure to remove the original one.
	if len(hdr) > 0 {
		hdr = removeHeaderIfPresent(hdr, JSStreamSource)
	}
	// Hold onto the origin reply which has all the metadata.
	hdr = genHeader(hdr, JSStreamSource, si.genSourceHeader(m.rply))

	var err error
	// If we are clustered we need to propose this message to the underlying raft group.
	if node != nil {
		err = mset.processClusteredInboundMsg(m.subj, _EMPTY_, hdr, msg)
	} else {
		err = mset.processJetStreamMsg(m.subj, _EMPTY_, hdr, msg, 0, 0)
	}

	if err != nil {
		s := mset.srv
		if strings.Contains(err.Error(), "no space left") {
			s.Errorf("JetStream out of space, will be DISABLED")
			s.DisableJetStream()
		} else {
			mset.mu.RLock()
			accName, sname, iname := mset.acc.Name, mset.cfg.Name, si.iname
			mset.mu.RUnlock()
			// Log some warning for errors other than errLastSeqMismatch
			if err != errLastSeqMismatch {
				s.RateLimitWarnf("Error processing inbound source %q for '%s' > '%s': %v",
					iname, accName, sname, err)
			}
			// Retry in all type of errors.
			// This will make sure the source is still in mset.sources map,
			// find the last sequence and then call setSourceConsumer.
			mset.retrySourceConsumer(iname)
		}
		return false
	}

	return true
}

// Generate a new style source header.
func (si *sourceInfo) genSourceHeader(reply string) string {
	var b strings.Builder
	b.WriteString(si.iname)
	b.WriteByte(' ')
	// Grab sequence as text here from reply subject.
	var tsa [expectedNumReplyTokens]string
	start, tokens := 0, tsa[:0]
	for i := 0; i < len(reply); i++ {
		if reply[i] == btsep {
			tokens, start = append(tokens, reply[start:i]), i+1
		}
	}
	tokens = append(tokens, reply[start:])
	seq := "1" // Default
	if len(tokens) == expectedNumReplyTokens && tokens[0] == "$JS" && tokens[1] == "ACK" {
		seq = tokens[5]
	}
	b.WriteString(seq)
	return b.String()
}

// Original version of header that stored ack reply direct.
func streamAndSeqFromAckReply(reply string) (string, uint64) {
	tsa := [expectedNumReplyTokens]string{}
	start, tokens := 0, tsa[:0]
	for i := 0; i < len(reply); i++ {
		if reply[i] == btsep {
			tokens, start = append(tokens, reply[start:i]), i+1
		}
	}
	tokens = append(tokens, reply[start:])
	if len(tokens) != expectedNumReplyTokens || tokens[0] != "$JS" || tokens[1] != "ACK" {
		return _EMPTY_, 0
	}
	return tokens[2], uint64(parseAckReplyNum(tokens[5]))
}

// Extract the stream (indexed name) and sequence from the source header.
func streamAndSeq(shdr string) (string, uint64) {
	if strings.HasPrefix(shdr, jsAckPre) {
		return streamAndSeqFromAckReply(shdr)
	}
	// New version which is stream index name <SPC> sequence
	fields := strings.Fields(shdr)
	if len(fields) != 2 {
		return _EMPTY_, 0
	}
	return fields[0], uint64(parseAckReplyNum(fields[1]))
}

// Lock should be held.
func (mset *stream) setStartingSequenceForSource(sname string) {
	si := mset.sources[sname]
	if si == nil {
		return
	}

	var state StreamState
	mset.store.FastState(&state)

	// Do not reset sseq here so we can remember when purge/expiration happens.
	if state.Msgs == 0 {
		si.dseq = 0
		return
	}

	var smv StoreMsg
	for seq := state.LastSeq; seq >= state.FirstSeq; seq-- {
		sm, err := mset.store.LoadMsg(seq, &smv)
		if err != nil || len(sm.hdr) == 0 {
			continue
		}
		ss := getHeader(JSStreamSource, sm.hdr)
		if len(ss) == 0 {
			continue
		}
		iname, sseq := streamAndSeq(string(ss))
		if iname == sname {
			si.sseq = sseq
			si.dseq = 0
			return
		}
	}
}

// Lock should be held.
// This will do a reverse scan on startup or leader election
// searching for the starting sequence number.
// This can be slow in degenerative cases.
// Lock should be held.
func (mset *stream) startingSequenceForSources() {
	if len(mset.cfg.Sources) == 0 {
		return
	}
	// Always reset here.
	mset.sources = make(map[string]*sourceInfo)

	for _, ssi := range mset.cfg.Sources {
		if ssi.iname == _EMPTY_ {
			ssi.setIndexName()
		}
		si := &sourceInfo{name: ssi.Name, iname: ssi.iname}
		mset.sources[ssi.iname] = si
	}

	var state StreamState
	mset.store.FastState(&state)

	// If the last time has been stamped remember in case we need to fall back to this for any given upstream source.
	// TODO(dlc) - This will be ok, but should formalize with new approach and more formal and durable state.
	if !state.LastTime.IsZero() {
		for _, si := range mset.sources {
			si.start = state.LastTime
		}
	}
	// Bail if no messages, meaning no context.
	if state.Msgs == 0 {
		return
	}

	// For short circuiting return.
	expected := len(mset.cfg.Sources)
	seqs := make(map[string]uint64)

	// Stamp our si seq records on the way out.
	defer func() {
		for sname, seq := range seqs {
			// Ignore if not set.
			if seq == 0 {
				continue
			}
			if si := mset.sources[sname]; si != nil {
				si.sseq = seq
				si.dseq = 0
			}
		}
	}()

	var smv StoreMsg
	for seq := state.LastSeq; seq >= state.FirstSeq; seq-- {
		sm, err := mset.store.LoadMsg(seq, &smv)
		if err != nil || sm == nil || len(sm.hdr) == 0 {
			continue
		}
		ss := getHeader(JSStreamSource, sm.hdr)
		if len(ss) == 0 {
			continue
		}
		name, sseq := streamAndSeq(string(ss))
		// Only update active in case we have older ones in here that got configured out.
		if si := mset.sources[name]; si != nil {
			if _, ok := seqs[name]; !ok {
				seqs[name] = sseq
				if len(seqs) == expected {
					return
				}
			}
		}
	}
}

// Setup our source consumers.
// Lock should be held.
func (mset *stream) setupSourceConsumers() error {
	if mset.outq == nil {
		return errors.New("outq required")
	}
	// Reset if needed.
	for _, si := range mset.sources {
		if si.sub != nil {
			mset.cancelSourceConsumer(si.iname)
		}
	}

	mset.startingSequenceForSources()

	// Setup our consumers at the proper starting position.
	for _, ssi := range mset.cfg.Sources {
		if si := mset.sources[ssi.iname]; si != nil {
			mset.setSourceConsumer(ssi.iname, si.sseq+1, time.Time{})
		}
	}

	return nil
}

// Will create internal subscriptions for the stream.
// Lock should be held.
func (mset *stream) subscribeToStream() error {
	if mset.active {
		return nil
	}
	for _, subject := range mset.cfg.Subjects {
		if _, err := mset.subscribeInternal(subject, mset.processInboundJetStreamMsg); err != nil {
			return err
		}
	}
	// Check if we need to setup mirroring.
	if mset.cfg.Mirror != nil {
		if err := mset.setupMirrorConsumer(); err != nil {
			return err
		}
	} else if len(mset.cfg.Sources) > 0 {
		if err := mset.setupSourceConsumers(); err != nil {
			return err
		}
	}
	// Check for direct get access.
	// We spin up followers for clustered streams in monitorStream().
	if mset.cfg.AllowDirect {
		if err := mset.subscribeToDirect(); err != nil {
			return err
		}
	}

	mset.active = true
	return nil
}

// Lock should be held.
func (mset *stream) subscribeToDirect() error {
	// We will make this listen on a queue group by default, which can allow mirrors to participate on opt-in basis.
	if mset.directSub == nil {
		dsubj := fmt.Sprintf(JSDirectMsgGetT, mset.cfg.Name)
		if sub, err := mset.queueSubscribeInternal(dsubj, dgetGroup, mset.processDirectGetRequest); err == nil {
			mset.directSub = sub
		} else {
			return err
		}
	}
	// Now the one that will have subject appended past stream name.
	if mset.lastBySub == nil {
		dsubj := fmt.Sprintf(JSDirectGetLastBySubjectT, mset.cfg.Name, fwcs)
		// We will make this listen on a queue group by default, which can allow mirrors to participate on opt-in basis.
		if sub, err := mset.queueSubscribeInternal(dsubj, dgetGroup, mset.processDirectGetLastBySubjectRequest); err == nil {
			mset.lastBySub = sub
		} else {
			return err
		}
	}

	return nil
}

// Lock should be held.
func (mset *stream) unsubscribeToDirect() {
	if mset.directSub != nil {
		mset.unsubscribe(mset.directSub)
		mset.directSub = nil
	}
	if mset.lastBySub != nil {
		mset.unsubscribe(mset.lastBySub)
		mset.lastBySub = nil
	}
}

// Lock should be held.
func (mset *stream) subscribeToMirrorDirect() error {
	if mset.mirror == nil {
		return nil
	}

	// We will make this listen on a queue group by default, which can allow mirrors to participate on opt-in basis.
	if mset.mirror.dsub == nil {
		dsubj := fmt.Sprintf(JSDirectMsgGetT, mset.mirror.name)
		// We will make this listen on a queue group by default, which can allow mirrors to participate on opt-in basis.
		if sub, err := mset.queueSubscribeInternal(dsubj, dgetGroup, mset.processDirectGetRequest); err == nil {
			mset.mirror.dsub = sub
		} else {
			return err
		}
	}
	// Now the one that will have subject appended past stream name.
	if mset.mirror.lbsub == nil {
		dsubj := fmt.Sprintf(JSDirectGetLastBySubjectT, mset.mirror.name, fwcs)
		// We will make this listen on a queue group by default, which can allow mirrors to participate on opt-in basis.
		if sub, err := mset.queueSubscribeInternal(dsubj, dgetGroup, mset.processDirectGetLastBySubjectRequest); err == nil {
			mset.mirror.lbsub = sub
		} else {
			return err
		}
	}

	return nil
}

// Stop our source consumers.
// Lock should be held.
func (mset *stream) stopSourceConsumers() {
	for _, si := range mset.sources {
		mset.cancelSourceInfo(si)
	}
}

// Lock should be held.
func (mset *stream) removeInternalConsumer(si *sourceInfo) {
	if si == nil || si.cname == _EMPTY_ {
		return
	}
	si.cname = _EMPTY_
}

// Will unsubscribe from the stream.
// Lock should be held.
func (mset *stream) unsubscribeToStream(stopping bool) error {
	for _, subject := range mset.cfg.Subjects {
		mset.unsubscribeInternal(subject)
	}
	if mset.mirror != nil {
		mset.cancelSourceInfo(mset.mirror)
		mset.mirror = nil
	}

	if len(mset.sources) > 0 {
		mset.stopSourceConsumers()
	}

	// In case we had a direct get subscriptions.
	if stopping {
		mset.unsubscribeToDirect()
	}

	mset.active = false
	return nil
}

// Lock should be held.
func (mset *stream) subscribeInternal(subject string, cb msgHandler) (*subscription, error) {
	c := mset.client
	if c == nil {
		return nil, fmt.Errorf("invalid stream")
	}
	if cb == nil {
		return nil, fmt.Errorf("undefined message handler")
	}

	mset.sid++

	// Now create the subscription
	return c.processSub([]byte(subject), nil, []byte(strconv.Itoa(mset.sid)), cb, false)
}

// Helper for unlocked stream.
func (mset *stream) subscribeInternalUnlocked(subject string, cb msgHandler) (*subscription, error) {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	return mset.subscribeInternal(subject, cb)
}

// Lock should be held.
func (mset *stream) queueSubscribeInternal(subject, group string, cb msgHandler) (*subscription, error) {
	c := mset.client
	if c == nil {
		return nil, fmt.Errorf("invalid stream")
	}
	if cb == nil {
		return nil, fmt.Errorf("undefined message handler")
	}

	mset.sid++

	// Now create the subscription
	return c.processSub([]byte(subject), []byte(group), []byte(strconv.Itoa(mset.sid)), cb, false)
}

// This will unsubscribe us from the exact subject given.
// We do not currently track the subs so do not have the sid.
// This should be called only on an update.
// Lock should be held.
func (mset *stream) unsubscribeInternal(subject string) error {
	c := mset.client
	if c == nil {
		return fmt.Errorf("invalid stream")
	}

	var sid []byte
	c.mu.Lock()
	for _, sub := range c.subs {
		if subject == string(sub.subject) {
			sid = sub.sid
			break
		}
	}
	c.mu.Unlock()

	if sid != nil {
		return c.processUnsub(sid)
	}
	return nil
}

// Lock should be held.
func (mset *stream) unsubscribe(sub *subscription) {
	if sub == nil || mset.client == nil {
		return
	}
	mset.client.processUnsub(sub.sid)
}

func (mset *stream) unsubscribeUnlocked(sub *subscription) {
	mset.mu.Lock()
	mset.unsubscribe(sub)
	mset.mu.Unlock()
}

func (mset *stream) setupStore(fsCfg *FileStoreConfig) error {
	mset.mu.Lock()
	mset.created = time.Now().UTC()

	switch mset.cfg.Storage {
	case MemoryStorage:
		ms, err := newMemStore(&mset.cfg)
		if err != nil {
			mset.mu.Unlock()
			return err
		}
		mset.store = ms
	case FileStorage:
		s := mset.srv
		prf := s.jsKeyGen(mset.acc.Name)
		if prf != nil {
			// We are encrypted here, fill in correct cipher selection.
			fsCfg.Cipher = s.getOpts().JetStreamCipher
		}
		fs, err := newFileStoreWithCreated(*fsCfg, mset.cfg, mset.created, prf)
		if err != nil {
			mset.mu.Unlock()
			return err
		}
		mset.store = fs
		// Register our server.
		fs.registerServer(s)
	}
	mset.mu.Unlock()

	mset.store.RegisterStorageUpdates(mset.storeUpdates)

	return nil
}

// Called for any updates to the underlying stream. We pass through the bytes to the
// jetstream account. We do local processing for stream pending for consumers, but only
// for removals.
// Lock should not be held.
func (mset *stream) storeUpdates(md, bd int64, seq uint64, subj string) {
	// If we have a single negative update then we will process our consumers for stream pending.
	// Purge and Store handled separately inside individual calls.
	if md == -1 && seq > 0 && subj != _EMPTY_ {
		// We use our consumer list mutex here instead of the main stream lock since it may be held already.
		mset.clsMu.RLock()
		// TODO(dlc) - Do sublist like signaling so we do not have to match?
		for _, o := range mset.cList {
			o.decStreamPending(seq, subj)
		}
		mset.clsMu.RUnlock()
	} else if md < 0 {
		// Batch decrements we need to force consumers to re-calculate num pending.
		mset.clsMu.RLock()
		for _, o := range mset.cList {
			o.streamNumPendingLocked()
		}
		mset.clsMu.RUnlock()
	}

	if mset.jsa != nil {
		mset.jsa.updateUsage(mset.tier, mset.stype, bd)
	}
}

// NumMsgIds returns the number of message ids being tracked for duplicate suppression.
func (mset *stream) numMsgIds() int {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	if !mset.ddloaded {
		mset.rebuildDedupe()
	}
	return len(mset.ddmap)
}

// checkMsgId will process and check for duplicates.
// Lock should be held.
func (mset *stream) checkMsgId(id string) *ddentry {
	if !mset.ddloaded {
		mset.rebuildDedupe()
	}
	if id == _EMPTY_ || len(mset.ddmap) == 0 {
		return nil
	}
	return mset.ddmap[id]
}

// Will purge the entries that are past the window.
// Should be called from a timer.
func (mset *stream) purgeMsgIds() {
	mset.mu.Lock()
	defer mset.mu.Unlock()

	now := time.Now().UnixNano()
	tmrNext := mset.cfg.Duplicates
	window := int64(tmrNext)

	for i, dde := range mset.ddarr[mset.ddindex:] {
		if now-dde.ts >= window {
			delete(mset.ddmap, dde.id)
		} else {
			mset.ddindex += i
			// Check if we should garbage collect here if we are 1/3 total size.
			if cap(mset.ddarr) > 3*(len(mset.ddarr)-mset.ddindex) {
				mset.ddarr = append([]*ddentry(nil), mset.ddarr[mset.ddindex:]...)
				mset.ddindex = 0
			}
			tmrNext = time.Duration(window - (now - dde.ts))
			break
		}
	}
	if len(mset.ddmap) > 0 {
		// Make sure to not fire too quick
		const minFire = 50 * time.Millisecond
		if tmrNext < minFire {
			tmrNext = minFire
		}
		if mset.ddtmr != nil {
			mset.ddtmr.Reset(tmrNext)
		} else {
			mset.ddtmr = time.AfterFunc(tmrNext, mset.purgeMsgIds)
		}
	} else {
		if mset.ddtmr != nil {
			mset.ddtmr.Stop()
			mset.ddtmr = nil
		}
		mset.ddmap = nil
		mset.ddarr = nil
		mset.ddindex = 0
	}
}

// storeMsgId will store the message id for duplicate detection.
func (mset *stream) storeMsgId(dde *ddentry) {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	mset.storeMsgIdLocked(dde)
}

// storeMsgIdLocked will store the message id for duplicate detection.
// Lock should he held.
func (mset *stream) storeMsgIdLocked(dde *ddentry) {
	if mset.ddmap == nil {
		mset.ddmap = make(map[string]*ddentry)
	}
	mset.ddmap[dde.id] = dde
	mset.ddarr = append(mset.ddarr, dde)
	if mset.ddtmr == nil {
		mset.ddtmr = time.AfterFunc(mset.cfg.Duplicates, mset.purgeMsgIds)
	}
}

// Fast lookup of msgId.
func getMsgId(hdr []byte) string {
	return string(getHeader(JSMsgId, hdr))
}

// Fast lookup of expected last msgId.
func getExpectedLastMsgId(hdr []byte) string {
	return string(getHeader(JSExpectedLastMsgId, hdr))
}

// Fast lookup of expected stream.
func getExpectedStream(hdr []byte) string {
	return string(getHeader(JSExpectedStream, hdr))
}

// Fast lookup of expected stream.
func getExpectedLastSeq(hdr []byte) (uint64, bool) {
	bseq := getHeader(JSExpectedLastSeq, hdr)
	if len(bseq) == 0 {
		return 0, false
	}
	return uint64(parseInt64(bseq)), true
}

// Fast lookup of rollups.
func getRollup(hdr []byte) string {
	r := getHeader(JSMsgRollup, hdr)
	if len(r) == 0 {
		return _EMPTY_
	}
	return strings.ToLower(string(r))
}

// Fast lookup of expected stream sequence per subject.
func getExpectedLastSeqPerSubject(hdr []byte) (uint64, bool) {
	bseq := getHeader(JSExpectedLastSubjSeq, hdr)
	if len(bseq) == 0 {
		return 0, false
	}
	return uint64(parseInt64(bseq)), true
}

// Signal if we are clustered. Will acquire rlock.
func (mset *stream) IsClustered() bool {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.isClustered()
}

// Lock should be held.
func (mset *stream) isClustered() bool {
	return mset.node != nil
}

// Used if we have to queue things internally to avoid the route/gw path.
type inMsg struct {
	subj string
	rply string
	hdr  []byte
	msg  []byte
}

func (mset *stream) queueInbound(ib *ipQueue[*inMsg], subj, rply string, hdr, msg []byte) {
	ib.push(&inMsg{subj, rply, hdr, msg})
}

func (mset *stream) queueInboundMsg(subj, rply string, hdr, msg []byte) {
	// Copy these.
	if len(hdr) > 0 {
		hdr = copyBytes(hdr)
	}
	if len(msg) > 0 {
		msg = copyBytes(msg)
	}
	mset.queueInbound(mset.msgs, subj, rply, hdr, msg)
}

// processDirectGetRequest handles direct get request for stream messages.
func (mset *stream) processDirectGetRequest(_ *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
	_, msg := c.msgParts(rmsg)
	if len(reply) == 0 {
		return
	}
	if len(msg) == 0 {
		hdr := []byte("NATS/1.0 408 Empty Request\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}
	var req JSApiMsgGetRequest
	err := json.Unmarshal(msg, &req)
	if err != nil {
		hdr := []byte("NATS/1.0 408 Malformed Request\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}
	// Check if nothing set.
	if req.Seq == 0 && req.LastFor == _EMPTY_ && req.NextFor == _EMPTY_ {
		hdr := []byte("NATS/1.0 408 Empty Request\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}
	// Check that we do not have both options set.
	if req.Seq > 0 && req.LastFor != _EMPTY_ {
		hdr := []byte("NATS/1.0 408 Bad Request\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}
	if req.LastFor != _EMPTY_ && req.NextFor != _EMPTY_ {
		hdr := []byte("NATS/1.0 408 Bad Request\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}

	inlineOk := c.kind != ROUTER && c.kind != GATEWAY && c.kind != LEAF
	if !inlineOk {
		// Check how long we have been away from the readloop for the route or gateway or leafnode.
		// If too long move to a separate go routine.
		if elapsed := time.Since(c.in.start); elapsed < noBlockThresh {
			inlineOk = true
		}
	}

	if inlineOk {
		mset.getDirectRequest(&req, reply)
	} else {
		go mset.getDirectRequest(&req, reply)
	}
}

// This is for direct get by last subject which is part of the subject itself.
func (mset *stream) processDirectGetLastBySubjectRequest(_ *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
	_, msg := c.msgParts(rmsg)
	if len(reply) == 0 {
		return
	}
	// This version expects no payload.
	if len(msg) != 0 {
		hdr := []byte("NATS/1.0 408 Bad Request\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}
	// Extract the key.
	var key string
	for i, n := 0, 0; i < len(subject); i++ {
		if subject[i] == btsep {
			if n == 4 {
				if start := i + 1; start < len(subject) {
					key = subject[i+1:]
				}
				break
			}
			n++
		}
	}
	if len(key) == 0 {
		hdr := []byte("NATS/1.0 408 Bad Request\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}

	inlineOk := c.kind != ROUTER && c.kind != GATEWAY && c.kind != LEAF
	if !inlineOk {
		// Check how long we have been away from the readloop for the route or gateway or leafnode.
		// If too long move to a separate go routine.
		if elapsed := time.Since(c.in.start); elapsed < noBlockThresh {
			inlineOk = true
		}
	}

	if inlineOk {
		mset.getDirectRequest(&JSApiMsgGetRequest{LastFor: key}, reply)
	} else {
		go mset.getDirectRequest(&JSApiMsgGetRequest{LastFor: key}, reply)
	}
}

// Do actual work on a direct msg request.
// This could be called in a Go routine if we are inline for a non-client connection.
func (mset *stream) getDirectRequest(req *JSApiMsgGetRequest, reply string) {
	var svp StoreMsg
	var sm *StoreMsg
	var err error

	mset.mu.RLock()
	store, name := mset.store, mset.cfg.Name
	mset.mu.RUnlock()

	if req.Seq > 0 && req.NextFor == _EMPTY_ {
		sm, err = store.LoadMsg(req.Seq, &svp)
	} else if req.NextFor != _EMPTY_ {
		sm, _, err = store.LoadNextMsg(req.NextFor, subjectHasWildcard(req.NextFor), req.Seq, &svp)
	} else {
		sm, err = store.LoadLastMsg(req.LastFor, &svp)
	}
	if err != nil {
		hdr := []byte("NATS/1.0 404 Message Not Found\r\n\r\n")
		mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, nil, nil, 0))
		return
	}

	hdr := sm.hdr
	ts := time.Unix(0, sm.ts).UTC()

	if len(hdr) == 0 {
		const ht = "NATS/1.0\r\nNats-Stream: %s\r\nNats-Subject: %s\r\nNats-Sequence: %d\r\nNats-Time-Stamp: %s\r\n\r\n"
		hdr = []byte(fmt.Sprintf(ht, name, sm.subj, sm.seq, ts.Format(time.RFC3339Nano)))
	} else {
		hdr = copyBytes(hdr)
		hdr = genHeader(hdr, JSStream, name)
		hdr = genHeader(hdr, JSSubject, sm.subj)
		hdr = genHeader(hdr, JSSequence, strconv.FormatUint(sm.seq, 10))
		hdr = genHeader(hdr, JSTimeStamp, ts.Format(time.RFC3339Nano))
	}
	mset.outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, hdr, sm.msg, nil, 0))
}

// processInboundJetStreamMsg handles processing messages bound for a stream.
func (mset *stream) processInboundJetStreamMsg(_ *subscription, c *client, _ *Account, subject, reply string, rmsg []byte) {
	hdr, msg := c.msgParts(rmsg)
	mset.queueInboundMsg(subject, reply, hdr, msg)
}

var (
	errLastSeqMismatch = errors.New("last sequence mismatch")
	errMsgIdDuplicate  = errors.New("msgid is duplicate")
)

// processJetStreamMsg is where we try to actually process the stream msg.
func (mset *stream) processJetStreamMsg(subject, reply string, hdr, msg []byte, lseq uint64, ts int64) error {
	mset.mu.Lock()
	c, s, store := mset.client, mset.srv, mset.store
	if mset.closed || c == nil {
		mset.mu.Unlock()
		return nil
	}

	var accName string
	if mset.acc != nil {
		accName = mset.acc.Name
	}

	js, jsa, doAck := mset.js, mset.jsa, !mset.cfg.NoAck
	name, stype := mset.cfg.Name, mset.cfg.Storage
	maxMsgSize := int(mset.cfg.MaxMsgSize)
	numConsumers := len(mset.consumers)
	interestRetention := mset.cfg.Retention == InterestPolicy
	// Snapshot if we are the leader and if we can respond.
	isLeader, isSealed := mset.isLeader(), mset.cfg.Sealed
	canRespond := doAck && len(reply) > 0 && isLeader

	var resp = &JSPubAckResponse{}

	// Bail here if sealed.
	if isSealed {
		outq := mset.outq
		mset.mu.Unlock()
		if canRespond && outq != nil {
			resp.PubAck = &PubAck{Stream: name}
			resp.Error = ApiErrors[JSStreamSealedErr]
			b, _ := json.Marshal(resp)
			outq.sendMsg(reply, b)
		}
		return ApiErrors[JSStreamSealedErr]
	}

	var buf [256]byte
	pubAck := append(buf[:0], mset.pubAck...)

	// If this is a non-clustered msg and we are not considered active, meaning no active subscription, do not process.
	if lseq == 0 && ts == 0 && !mset.active {
		mset.mu.Unlock()
		return nil
	}

	// For clustering the lower layers will pass our expected lseq. If it is present check for that here.
	if lseq > 0 && lseq != (mset.lseq+mset.clfs) {
		isMisMatch := true
		// We may be able to recover here if we have no state whatsoever, or we are a mirror.
		// See if we have to adjust our starting sequence.
		if mset.lseq == 0 || mset.cfg.Mirror != nil {
			var state StreamState
			mset.store.FastState(&state)
			if state.FirstSeq == 0 {
				mset.store.Compact(lseq + 1)
				mset.lseq = lseq
				isMisMatch = false
			}
		}
		// Really is a mismatch.
		if isMisMatch {
			outq := mset.outq
			mset.mu.Unlock()
			if canRespond && outq != nil {
				resp.PubAck = &PubAck{Stream: name}
				resp.Error = ApiErrors[JSStreamSequenceNotMatchErr]
				b, _ := json.Marshal(resp)
				outq.sendMsg(reply, b)
			}
			return errLastSeqMismatch
		}
	}

	// If we have received this message across an account we may have request information attached.
	// For now remove. TODO(dlc) - Should this be opt-in or opt-out?
	if len(hdr) > 0 {
		hdr = removeHeaderIfPresent(hdr, ClientInfoHdr)
	}

	// Process additional msg headers if still present.
	var msgId string
	var rollupSub, rollupAll bool

	if len(hdr) > 0 {
		outq := mset.outq
		isClustered := mset.isClustered()

		// Certain checks have already been performed if in clustered mode, so only check if not.
		if !isClustered {
			// Expected stream.
			if sname := getExpectedStream(hdr); sname != _EMPTY_ && sname != name {
				mset.clfs++
				mset.mu.Unlock()
				if canRespond {
					resp.PubAck = &PubAck{Stream: name}
					resp.Error = NewJSStreamNotMatchError()
					b, _ := json.Marshal(resp)
					outq.sendMsg(reply, b)
				}
				return errors.New("expected stream does not match")
			}
		}

		// Dedupe detection.
		if msgId = getMsgId(hdr); msgId != _EMPTY_ {
			if dde := mset.checkMsgId(msgId); dde != nil {
				mset.clfs++
				mset.mu.Unlock()
				if canRespond {
					response := append(pubAck, strconv.FormatUint(dde.seq, 10)...)
					response = append(response, ",\"duplicate\": true}"...)
					outq.sendMsg(reply, response)
				}
				return errMsgIdDuplicate
			}
		}
		// Expected last sequence per subject.
		// If we are clustered we have prechecked seq > 0.
		if seq, exists := getExpectedLastSeqPerSubject(hdr); exists && (!isClustered || seq == 0) {
			// TODO(dlc) - We could make a new store func that does this all in one.
			var smv StoreMsg
			var fseq uint64
			sm, err := store.LoadLastMsg(subject, &smv)
			if sm != nil {
				fseq = sm.seq
			}
			if err == ErrStoreMsgNotFound && seq == 0 {
				fseq, err = 0, nil
			}
			if err != nil || fseq != seq {
				mset.clfs++
				mset.mu.Unlock()
				if canRespond {
					resp.PubAck = &PubAck{Stream: name}
					resp.Error = NewJSStreamWrongLastSequenceError(fseq)
					b, _ := json.Marshal(resp)
					outq.sendMsg(reply, b)
				}
				return fmt.Errorf("last sequence by subject mismatch: %d vs %d", seq, fseq)
			}
		}

		// Expected last sequence.
		if seq, exists := getExpectedLastSeq(hdr); exists && seq != mset.lseq {
			mlseq := mset.lseq
			mset.clfs++
			mset.mu.Unlock()
			if canRespond {
				resp.PubAck = &PubAck{Stream: name}
				resp.Error = NewJSStreamWrongLastSequenceError(mlseq)
				b, _ := json.Marshal(resp)
				outq.sendMsg(reply, b)
			}
			return fmt.Errorf("last sequence mismatch: %d vs %d", seq, mlseq)
		}
		// Expected last msgId.
		if lmsgId := getExpectedLastMsgId(hdr); lmsgId != _EMPTY_ {
			if mset.lmsgId == _EMPTY_ && !mset.ddloaded {
				mset.rebuildDedupe()
			}
			if lmsgId != mset.lmsgId {
				last := mset.lmsgId
				mset.clfs++
				mset.mu.Unlock()
				if canRespond {
					resp.PubAck = &PubAck{Stream: name}
					resp.Error = NewJSStreamWrongLastMsgIDError(last)
					b, _ := json.Marshal(resp)
					outq.sendMsg(reply, b)
				}
				return fmt.Errorf("last msgid mismatch: %q vs %q", lmsgId, last)
			}
		}
		// Check for any rollups.
		if rollup := getRollup(hdr); rollup != _EMPTY_ {
			if !mset.cfg.AllowRollup || mset.cfg.DenyPurge {
				mset.clfs++
				mset.mu.Unlock()
				if canRespond {
					resp.PubAck = &PubAck{Stream: name}
					resp.Error = NewJSStreamRollupFailedError(errors.New("rollup not permitted"))
					b, _ := json.Marshal(resp)
					outq.sendMsg(reply, b)
				}
				return errors.New("rollup not permitted")
			}
			switch rollup {
			case JSMsgRollupSubject:
				rollupSub = true
			case JSMsgRollupAll:
				rollupAll = true
			default:
				mset.mu.Unlock()
				return fmt.Errorf("rollup value invalid: %q", rollup)
			}
		}
	}

	// Response Ack.
	var (
		response []byte
		seq      uint64
		err      error
	)

	// Check to see if we are over the max msg size.
	if maxMsgSize >= 0 && (len(hdr)+len(msg)) > maxMsgSize {
		mset.clfs++
		mset.mu.Unlock()
		if canRespond {
			resp.PubAck = &PubAck{Stream: name}
			resp.Error = NewJSStreamMessageExceedsMaximumError()
			response, _ = json.Marshal(resp)
			mset.outq.sendMsg(reply, response)
		}
		return ErrMaxPayload
	}

	if len(hdr) > math.MaxUint16 {
		mset.clfs++
		mset.mu.Unlock()
		if canRespond {
			resp.PubAck = &PubAck{Stream: name}
			resp.Error = NewJSStreamHeaderExceedsMaximumError()
			response, _ = json.Marshal(resp)
			mset.outq.sendMsg(reply, response)
		}
		return ErrMaxPayload
	}

	// Check to see if we have exceeded our limits.
	if js.limitsExceeded(stype) {
		s.resourcesExeededError()
		mset.clfs++
		mset.mu.Unlock()
		if canRespond {
			resp.PubAck = &PubAck{Stream: name}
			resp.Error = NewJSInsufficientResourcesError()
			response, _ = json.Marshal(resp)
			mset.outq.sendMsg(reply, response)
		}
		// Stepdown regardless.
		if node := mset.raftNode(); node != nil {
			node.StepDown()
		}
		return NewJSInsufficientResourcesError()
	}

	var noInterest bool

	// If we are interest based retention and have no consumers then we can skip.
	if interestRetention {
		if numConsumers == 0 {
			noInterest = true
		} else if mset.numFilter > 0 {
			// Assume no interest and check to disqualify.
			noInterest = true
			mset.clsMu.RLock()
			for _, o := range mset.cList {
				if o.cfg.FilterSubject == _EMPTY_ || subjectIsSubsetMatch(subject, o.cfg.FilterSubject) {
					noInterest = false
					break
				}
			}
			mset.clsMu.RUnlock()
		}
	}

	// Grab timestamp if not already set.
	if ts == 0 && lseq > 0 {
		ts = time.Now().UnixNano()
	}

	// Skip msg here.
	if noInterest {
		mset.lseq = store.SkipMsg()
		mset.lmsgId = msgId
		// If we have a msgId make sure to save.
		if msgId != _EMPTY_ {
			mset.storeMsgIdLocked(&ddentry{msgId, seq, ts})
		}
		if canRespond {
			response = append(pubAck, strconv.FormatUint(mset.lseq, 10)...)
			response = append(response, '}')
			mset.outq.sendMsg(reply, response)
		}
		mset.mu.Unlock()
		return nil
	}

	// If here we will attempt to store the message.
	// Assume this will succeed.
	olmsgId := mset.lmsgId
	mset.lmsgId = msgId
	clfs := mset.clfs
	mset.lseq++
	tierName := mset.tier

	// Republish state if needed.
	var tsubj string
	var tlseq uint64
	var thdrsOnly bool
	if mset.tr != nil {
		tsubj, _ = mset.tr.Match(subject)
		if mset.cfg.RePublish != nil {
			thdrsOnly = mset.cfg.RePublish.HeadersOnly
		}
	}
	republish := tsubj != _EMPTY_ && isLeader

	// If we are republishing grab last sequence for this exact subject. Aids in gap detection for lightweight clients.
	if republish {
		var smv StoreMsg
		if sm, _ := store.LoadLastMsg(subject, &smv); sm != nil {
			tlseq = sm.seq
		}
	}

	// Store actual msg.
	if lseq == 0 && ts == 0 {
		seq, ts, err = store.StoreMsg(subject, hdr, msg)
	} else {
		// Make sure to take into account any message assignments that we had to skip (clfs).
		seq = lseq + 1 - clfs
		// Check for preAcks and the need to skip vs store.

		if mset.hasAllPreAcks(seq, subject) {
			mset.clearAllPreAcks(seq)
			store.SkipMsg()
		} else {
			err = store.StoreRawMsg(subject, hdr, msg, seq, ts)
		}
	}

	if err != nil {
		// If we did not succeed put those values back and increment clfs in case we are clustered.
		var state StreamState
		mset.store.FastState(&state)
		mset.lseq = state.LastSeq
		mset.lmsgId = olmsgId
		mset.clfs++
		mset.mu.Unlock()

		switch err {
		case ErrMaxMsgs, ErrMaxBytes, ErrMaxMsgsPerSubject, ErrMsgTooLarge:
			s.Debugf("JetStream failed to store a msg on stream '%s > %s': %v", accName, name, err)
		case ErrStoreClosed:
		default:
			s.Errorf("JetStream failed to store a msg on stream '%s > %s': %v", accName, name, err)
		}

		if canRespond {
			resp.PubAck = &PubAck{Stream: name}
			resp.Error = NewJSStreamStoreFailedError(err, Unless(err))
			response, _ = json.Marshal(resp)
			mset.outq.sendMsg(reply, response)
		}
		return err
	}

	if exceeded, apiErr := jsa.limitsExceeded(stype, tierName); exceeded {
		s.RateLimitWarnf("JetStream resource limits exceeded for account: %q", accName)
		if canRespond {
			resp.PubAck = &PubAck{Stream: name}
			if apiErr == nil {
				resp.Error = NewJSAccountResourcesExceededError()
			} else {
				resp.Error = apiErr
			}
			response, _ = json.Marshal(resp)
			mset.outq.sendMsg(reply, response)
		}
		// If we did not succeed put those values back.
		var state StreamState
		mset.store.FastState(&state)
		mset.lseq = state.LastSeq
		mset.lmsgId = olmsgId
		mset.mu.Unlock()
		store.RemoveMsg(seq)
		return nil
	}

	// If we have a msgId make sure to save.
	if msgId != _EMPTY_ {
		mset.storeMsgIdLocked(&ddentry{msgId, seq, ts})
	}

	// If here we succeeded in storing the message.
	mset.mu.Unlock()

	// No errors, this is the normal path.
	if rollupSub {
		mset.purge(&JSApiStreamPurgeRequest{Subject: subject, Keep: 1})
	} else if rollupAll {
		mset.purge(&JSApiStreamPurgeRequest{Keep: 1})
	}

	// Check for republish.
	if republish {
		var rpMsg []byte
		if len(hdr) == 0 {
			const ht = "NATS/1.0\r\nNats-Stream: %s\r\nNats-Subject: %s\r\nNats-Sequence: %d\r\nNats-Last-Sequence: %d\r\n\r\n"
			const htho = "NATS/1.0\r\nNats-Stream: %s\r\nNats-Subject: %s\r\nNats-Sequence: %d\r\nNats-Last-Sequence: %d\r\nNats-Msg-Size: %d\r\n\r\n"
			if !thdrsOnly {
				hdr = []byte(fmt.Sprintf(ht, name, subject, seq, tlseq))
				rpMsg = copyBytes(msg)
			} else {
				hdr = []byte(fmt.Sprintf(htho, name, subject, seq, tlseq, len(msg)))
			}
		} else {
			// Slow path.
			hdr = genHeader(hdr, JSStream, name)
			hdr = genHeader(hdr, JSSubject, subject)
			hdr = genHeader(hdr, JSSequence, strconv.FormatUint(seq, 10))
			hdr = genHeader(hdr, JSLastSequence, strconv.FormatUint(tlseq, 10))
			if !thdrsOnly {
				rpMsg = copyBytes(msg)
			} else {
				hdr = genHeader(hdr, JSMsgSize, strconv.Itoa(len(msg)))
			}
		}
		mset.outq.send(newJSPubMsg(tsubj, _EMPTY_, _EMPTY_, copyBytes(hdr), rpMsg, nil, seq))
	}

	// Send response here.
	if canRespond {
		response = append(pubAck, strconv.FormatUint(seq, 10)...)
		response = append(response, '}')
		mset.outq.sendMsg(reply, response)
	}

	// Signal consumers for new messages.
	if numConsumers > 0 {
		mset.sigq.push(newCMsg(subject, seq))
		select {
		case mset.sch <- struct{}{}:
		default:
		}
	}

	return nil
}

// Used to signal inbound message to registered consumers.
type cMsg struct {
	seq  uint64
	subj string
}

// Pool to recycle consumer bound msgs.
var cMsgPool sync.Pool

// Used to queue up consumer bound msgs for signaling.
func newCMsg(subj string, seq uint64) *cMsg {
	var m *cMsg
	cm := cMsgPool.Get()
	if cm != nil {
		m = cm.(*cMsg)
	} else {
		m = new(cMsg)
	}
	m.subj, m.seq = subj, seq

	return m
}

func (m *cMsg) returnToPool() {
	if m == nil {
		return
	}
	m.subj, m.seq = _EMPTY_, 0
	cMsgPool.Put(m)
}

// Go routine to signal consumers.
// Offloaded from stream msg processing.
func (mset *stream) signalConsumersLoop() {
	mset.mu.RLock()
	s, qch, sch, msgs := mset.srv, mset.qch, mset.sch, mset.sigq
	mset.mu.RUnlock()

	for {
		select {
		case <-s.quitCh:
			return
		case <-qch:
			return
		case <-sch:
			cms := msgs.pop()
			for _, m := range cms {
				seq, subj := m.seq, m.subj
				m.returnToPool()
				// Signal all appropriate consumers.
				mset.signalConsumers(subj, seq)
			}
			msgs.recycle(&cms)
		}
	}
}

// This will update and signal all consumers that match.
func (mset *stream) signalConsumers(subj string, seq uint64) {
	mset.clsMu.RLock()
	if mset.csl == nil {
		mset.clsMu.RUnlock()
		return
	}
	r := mset.csl.Match(subj)
	mset.clsMu.RUnlock()

	if len(r.psubs) == 0 {
		return
	}
	// Encode the sequence here.
	var eseq [8]byte
	var le = binary.LittleEndian
	le.PutUint64(eseq[:], seq)
	msg := eseq[:]
	for _, sub := range r.psubs {
		sub.icb(sub, nil, nil, subj, _EMPTY_, msg)
	}
}

// Internal message for use by jetstream subsystem.
type jsPubMsg struct {
	dsubj string // Subject to send to, e.g. _INBOX.xxx
	reply string
	StoreMsg
	o *consumer
}

var jsPubMsgPool sync.Pool

func newJSPubMsg(dsubj, subj, reply string, hdr, msg []byte, o *consumer, seq uint64) *jsPubMsg {
	var m *jsPubMsg
	var buf []byte
	pm := jsPubMsgPool.Get()
	if pm != nil {
		m = pm.(*jsPubMsg)
		buf = m.buf[:0]
	} else {
		m = new(jsPubMsg)
	}
	// When getting something from a pool it is critical that all fields are
	// initialized. Doing this way guarantees that if someone adds a field to
	// the structure, the compiler will fail the build if this line is not updated.
	(*m) = jsPubMsg{dsubj, reply, StoreMsg{subj, hdr, msg, buf, seq, 0}, o}

	return m
}

// Gets a jsPubMsg from the pool.
func getJSPubMsgFromPool() *jsPubMsg {
	pm := jsPubMsgPool.Get()
	if pm != nil {
		return pm.(*jsPubMsg)
	}
	return new(jsPubMsg)
}

func (pm *jsPubMsg) returnToPool() {
	if pm == nil {
		return
	}
	pm.subj, pm.dsubj, pm.reply, pm.hdr, pm.msg, pm.o = _EMPTY_, _EMPTY_, _EMPTY_, nil, nil, nil
	if len(pm.buf) > 0 {
		pm.buf = pm.buf[:0]
	}
	jsPubMsgPool.Put(pm)
}

func (pm *jsPubMsg) size() int {
	if pm == nil {
		return 0
	}
	return len(pm.dsubj) + len(pm.reply) + len(pm.hdr) + len(pm.msg)
}

// Queue of *jsPubMsg for sending internal system messages.
type jsOutQ struct {
	*ipQueue[*jsPubMsg]
}

func (q *jsOutQ) sendMsg(subj string, msg []byte) {
	if q != nil {
		q.send(newJSPubMsg(subj, _EMPTY_, _EMPTY_, nil, msg, nil, 0))
	}
}

func (q *jsOutQ) send(msg *jsPubMsg) {
	if q == nil || msg == nil {
		return
	}
	q.push(msg)
}

func (q *jsOutQ) unregister() {
	if q == nil {
		return
	}
	q.ipQueue.unregister()
}

// StoredMsg is for raw access to messages in a stream.
type StoredMsg struct {
	Subject  string    `json:"subject"`
	Sequence uint64    `json:"seq"`
	Header   []byte    `json:"hdrs,omitempty"`
	Data     []byte    `json:"data,omitempty"`
	Time     time.Time `json:"time"`
}

// This is similar to system semantics but did not want to overload the single system sendq,
// or require system account when doing simple setup with jetstream.
func (mset *stream) setupSendCapabilities() {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	if mset.outq != nil {
		return
	}
	qname := fmt.Sprintf("[ACC:%s] stream '%s' sendQ", mset.acc.Name, mset.cfg.Name)
	mset.outq = &jsOutQ{newIPQueue[*jsPubMsg](mset.srv, qname)}
	go mset.internalLoop()
}

// Returns the associated account name.
func (mset *stream) accName() string {
	if mset == nil {
		return _EMPTY_
	}
	mset.mu.RLock()
	acc := mset.acc
	mset.mu.RUnlock()
	return acc.Name
}

// Name returns the stream name.
func (mset *stream) name() string {
	if mset == nil {
		return _EMPTY_
	}
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.cfg.Name
}

func (mset *stream) internalLoop() {
	mset.mu.RLock()
	s := mset.srv
	c := s.createInternalJetStreamClient()
	c.registerWithAccount(mset.acc)
	defer c.closeConnection(ClientClosed)
	outq, qch, msgs := mset.outq, mset.qch, mset.msgs

	// For the ack msgs queue for interest retention.
	var (
		amch chan struct{}
		ackq *ipQueue[uint64]
	)
	if mset.ackq != nil {
		ackq, amch = mset.ackq, mset.ackq.ch
	}
	mset.mu.RUnlock()

	// Raw scratch buffer.
	// This should be rarely used now so can be smaller.
	var _r [1024]byte

	for {
		select {
		case <-outq.ch:
			pms := outq.pop()
			for _, pm := range pms {
				c.pa.subject = []byte(pm.dsubj)
				c.pa.deliver = []byte(pm.subj)
				c.pa.size = len(pm.msg) + len(pm.hdr)
				c.pa.szb = []byte(strconv.Itoa(c.pa.size))
				c.pa.reply = []byte(pm.reply)

				// If we have an underlying buf that is the wire contents for hdr + msg, else construct on the fly.
				var msg []byte
				if len(pm.buf) > 0 {
					msg = pm.buf
				} else {
					if len(pm.hdr) > 0 {
						msg = pm.hdr
						if len(pm.msg) > 0 {
							msg = _r[:0]
							msg = append(msg, pm.hdr...)
							msg = append(msg, pm.msg...)
						}
					} else if len(pm.msg) > 0 {
						// We own this now from a low level buffer perspective so can use directly here.
						msg = pm.msg
					}
				}

				if len(pm.hdr) > 0 {
					c.pa.hdr = len(pm.hdr)
					c.pa.hdb = []byte(strconv.Itoa(c.pa.hdr))
				} else {
					c.pa.hdr = -1
					c.pa.hdb = nil
				}

				msg = append(msg, _CRLF_...)

				didDeliver, _ := c.processInboundClientMsg(msg)
				c.pa.szb, c.pa.subject, c.pa.deliver = nil, nil, nil

				// Check to see if this is a delivery for a consumer and
				// we failed to deliver the message. If so alert the consumer.
				if pm.o != nil && pm.seq > 0 && !didDeliver {
					pm.o.didNotDeliver(pm.seq)
				}
				pm.returnToPool()
			}
			// TODO: Move in the for-loop?
			c.flushClients(0)
			outq.recycle(&pms)
		case <-msgs.ch:
			// This can possibly change now so needs to be checked here.
			isClustered := mset.IsClustered()
			ims := msgs.pop()
			for _, im := range ims {
				// If we are clustered we need to propose this message to the underlying raft group.
				if isClustered {
					mset.processClusteredInboundMsg(im.subj, im.rply, im.hdr, im.msg)
				} else {
					mset.processJetStreamMsg(im.subj, im.rply, im.hdr, im.msg, 0, 0)
				}
			}
			msgs.recycle(&ims)
		case <-amch:
			seqs := ackq.pop()
			for _, seq := range seqs {
				mset.ackMsg(nil, seq)
			}
			ackq.recycle(&seqs)
		case <-qch:
			return
		case <-s.quitCh:
			return
		}
	}
}

// Used to break consumers out of their
func (mset *stream) resetAndWaitOnConsumers() {
	mset.mu.RLock()
	consumers := make([]*consumer, 0, len(mset.consumers))
	for _, o := range mset.consumers {
		consumers = append(consumers, o)
	}
	mset.mu.RUnlock()

	for _, o := range consumers {
		if node := o.raftNode(); node != nil {
			if o.IsLeader() {
				node.StepDown()
			}
			node.Delete()
		}
		o.monitorWg.Wait()
	}
}

// Internal function to delete a stream.
func (mset *stream) delete() error {
	if mset == nil {
		return nil
	}
	return mset.stop(true, true)
}

// Internal function to stop or delete the stream.
func (mset *stream) stop(deleteFlag, advisory bool) error {
	mset.mu.RLock()
	js, jsa := mset.js, mset.jsa
	mset.mu.RUnlock()

	if jsa == nil {
		return NewJSNotEnabledForAccountError()
	}

	// Remove from our account map first.
	jsa.mu.Lock()
	delete(jsa.streams, mset.cfg.Name)
	accName := jsa.account.Name
	jsa.mu.Unlock()

	// Clean up consumers.
	mset.mu.Lock()
	mset.closed = true
	var obs []*consumer
	for _, o := range mset.consumers {
		obs = append(obs, o)
	}
	mset.clsMu.Lock()
	mset.consumers, mset.cList, mset.csl = nil, nil, nil
	mset.clsMu.Unlock()

	// Check if we are a mirror.
	if mset.mirror != nil && mset.mirror.sub != nil {
		mset.unsubscribe(mset.mirror.sub)
		mset.mirror.sub = nil
		mset.removeInternalConsumer(mset.mirror)
	}
	// Now check for sources.
	if len(mset.sources) > 0 {
		for _, si := range mset.sources {
			mset.cancelSourceConsumer(si.iname)
		}
	}

	// Cluster cleanup
	var sa *streamAssignment
	if n := mset.node; n != nil {
		if deleteFlag {
			n.Delete()
			sa = mset.sa
		} else if n.NeedSnapshot() {
			// Attempt snapshot on clean exit.
			n.InstallSnapshot(mset.stateSnapshotLocked())
			n.Stop()
		}
	}
	mset.mu.Unlock()

	for _, o := range obs {
		// Third flag says do not broadcast a signal.
		// TODO(dlc) - If we have an err here we don't want to stop
		// but should we log?
		o.stopWithFlags(deleteFlag, deleteFlag, false, advisory)
		o.monitorWg.Wait()
	}

	mset.mu.Lock()
	// Stop responding to sync requests.
	mset.stopClusterSubs()
	// Unsubscribe from direct stream.
	mset.unsubscribeToStream(true)

	// Our info sub if we spun it up.
	if mset.infoSub != nil {
		mset.srv.sysUnsubscribe(mset.infoSub)
		mset.infoSub = nil
	}

	// Send stream delete advisory after the consumers.
	if deleteFlag && advisory {
		mset.sendDeleteAdvisoryLocked()
	}

	// Quit channel, do this after sending the delete advisory
	if mset.qch != nil {
		close(mset.qch)
		mset.qch = nil
	}

	c := mset.client
	mset.client = nil
	if c == nil {
		mset.mu.Unlock()
		return nil
	}

	// Cleanup duplicate timer if running.
	if mset.ddtmr != nil {
		mset.ddtmr.Stop()
		mset.ddtmr = nil
		mset.ddmap = nil
		mset.ddarr = nil
		mset.ddindex = 0
	}

	sysc := mset.sysc
	mset.sysc = nil

	if deleteFlag {
		// Unregistering ipQueues do not prevent them from push/pop
		// just will remove them from the central monitoring map
		mset.msgs.unregister()
		mset.ackq.unregister()
		mset.outq.unregister()
		mset.sigq.unregister()
	}

	// Snapshot store.
	store := mset.store

	// Clustered cleanup.
	mset.mu.Unlock()

	// Check if the stream assignment has the group node specified.
	// We need this cleared for if the stream gets reassigned here.
	if sa != nil {
		js.mu.Lock()
		if sa.Group != nil {
			sa.Group.node = nil
		}
		js.mu.Unlock()
	}

	c.closeConnection(ClientClosed)

	if sysc != nil {
		sysc.closeConnection(ClientClosed)
	}

	if store == nil {
		return nil
	}

	if deleteFlag {
		if err := store.Delete(); err != nil {
			return err
		}
		js.releaseStreamResources(&mset.cfg)

		// cleanup directories after the stream
		accDir := filepath.Join(js.config.StoreDir, accName)
		// no op if not empty
		os.Remove(filepath.Join(accDir, streamsDir))
		os.Remove(accDir)
	} else if err := store.Stop(); err != nil {
		return err
	}

	return nil
}

func (mset *stream) getMsg(seq uint64) (*StoredMsg, error) {
	var smv StoreMsg
	sm, err := mset.store.LoadMsg(seq, &smv)
	if err != nil {
		return nil, err
	}
	// This only used in tests directly so no need to pool etc.
	return &StoredMsg{
		Subject:  sm.subj,
		Sequence: sm.seq,
		Header:   sm.hdr,
		Data:     sm.msg,
		Time:     time.Unix(0, sm.ts).UTC(),
	}, nil
}

// getConsumers will return a copy of all the current consumers for this stream.
func (mset *stream) getConsumers() []*consumer {
	mset.clsMu.RLock()
	defer mset.clsMu.RUnlock()
	return append([]*consumer(nil), mset.cList...)
}

// Lock should be held for this one.
func (mset *stream) numPublicConsumers() int {
	return len(mset.consumers) - mset.directs
}

// This returns all consumers that are not DIRECT.
func (mset *stream) getPublicConsumers() []*consumer {
	mset.clsMu.RLock()
	defer mset.clsMu.RUnlock()

	var obs []*consumer
	for _, o := range mset.cList {
		if !o.cfg.Direct {
			obs = append(obs, o)
		}
	}
	return obs
}

func (mset *stream) isInterestRetention() bool {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.cfg.Retention != LimitsPolicy
}

// NumConsumers reports on number of active consumers for this stream.
func (mset *stream) numConsumers() int {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return len(mset.consumers)
}

// Lock should be held.
func (mset *stream) setConsumer(o *consumer) {
	mset.consumers[o.name] = o
	if o.cfg.FilterSubject != _EMPTY_ {
		mset.numFilter++
	}
	if o.cfg.Direct {
		mset.directs++
	}
	// Now update consumers list as well
	mset.clsMu.Lock()
	mset.cList = append(mset.cList, o)
	mset.clsMu.Unlock()
}

// Lock should be held.
func (mset *stream) removeConsumer(o *consumer) {
	if o.cfg.FilterSubject != _EMPTY_ && mset.numFilter > 0 {
		mset.numFilter--
	}
	if o.cfg.Direct && mset.directs > 0 {
		mset.directs--
	}
	if mset.consumers != nil {
		delete(mset.consumers, o.name)
		// Now update consumers list as well
		mset.clsMu.Lock()
		for i, ol := range mset.cList {
			if ol == o {
				mset.cList = append(mset.cList[:i], mset.cList[i+1:]...)
				break
			}
		}
		// Always remove from the leader sublist.
		if mset.csl != nil {
			mset.csl.Remove(o.signalSub())
		}
		mset.clsMu.Unlock()
	}
}

// Set the consumer as a leader. This will update signaling sublist.
func (mset *stream) setConsumerAsLeader(o *consumer) {
	mset.clsMu.Lock()
	defer mset.clsMu.Unlock()

	if mset.csl == nil {
		mset.csl = NewSublistWithCache()
	}
	mset.csl.Insert(o.signalSub())
}

// Remove the consumer as a leader. This will update signaling sublist.
func (mset *stream) removeConsumerAsLeader(o *consumer) {
	mset.clsMu.Lock()
	defer mset.clsMu.Unlock()
	if mset.csl != nil {
		mset.csl.Remove(o.signalSub())
	}
}

// swapSigSubs will update signal Subs for a new subject filter.
// consumer lock should not be held.
func (mset *stream) swapSigSubs(o *consumer, newFilter string) {
	mset.clsMu.Lock()
	o.mu.Lock()

	if o.sigSub != nil {
		if mset.csl != nil {
			mset.csl.Remove(o.sigSub)
		}
		o.sigSub = nil
	}

	if o.isLeader() {
		subject := newFilter
		if subject == _EMPTY_ {
			subject = fwcs
		}
		o.sigSub = &subscription{subject: []byte(subject), icb: o.processStreamSignal}
		if mset.csl == nil {
			mset.csl = NewSublistWithCache()
		}
		mset.csl.Insert(o.sigSub)
	}

	oldFilter := o.cfg.FilterSubject

	o.mu.Unlock()
	mset.clsMu.Unlock()

	// Do any numFilter accounting needed.
	mset.mu.Lock()
	defer mset.mu.Unlock()

	// Decrement numFilter if old filter was an actual filter.
	if oldFilter != _EMPTY_ && mset.numFilter > 0 {
		mset.numFilter--
	}
	if newFilter != _EMPTY_ {
		mset.numFilter++
	}
}

// lookupConsumer will retrieve a consumer by name.
func (mset *stream) lookupConsumer(name string) *consumer {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.consumers[name]
}

func (mset *stream) numDirectConsumers() (num int) {
	mset.clsMu.RLock()
	defer mset.clsMu.RUnlock()

	// Consumers that are direct are not recorded at the store level.
	for _, o := range mset.cList {
		o.mu.RLock()
		if o.cfg.Direct {
			num++
		}
		o.mu.RUnlock()
	}
	return num
}

// State will return the current state for this stream.
func (mset *stream) state() StreamState {
	return mset.stateWithDetail(false)
}

func (mset *stream) stateWithDetail(details bool) StreamState {
	// mset.store does not change once set, so ok to reference here directly.
	// We do this elsewhere as well.
	store := mset.store
	if store == nil {
		return StreamState{}
	}

	// Currently rely on store for details.
	if details {
		return store.State()
	}
	// Here we do the fast version.
	var state StreamState
	store.FastState(&state)
	return state
}

func (mset *stream) Store() StreamStore {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.store
}

// Determines if the new proposed partition is unique amongst all consumers.
// Lock should be held.
func (mset *stream) partitionUnique(partition string) bool {
	for _, o := range mset.consumers {
		if o.cfg.FilterSubject == _EMPTY_ {
			return false
		}
		if subjectIsSubsetMatch(partition, o.cfg.FilterSubject) ||
			subjectIsSubsetMatch(o.cfg.FilterSubject, partition) {
			return false
		}
	}
	return true
}

// Lock should be held.
func (mset *stream) potentialFilteredConsumers() bool {
	numSubjects := len(mset.cfg.Subjects)
	if len(mset.consumers) == 0 || numSubjects == 0 {
		return false
	}
	if numSubjects > 1 || subjectHasWildcard(mset.cfg.Subjects[0]) {
		return true
	}
	return false
}

// Check if there is no interest in this sequence number across our consumers.
// The consumer passed is optional if we are processing the ack for that consumer.
// Write lock should be held.
func (mset *stream) noInterest(seq uint64, obs *consumer) bool {
	return !mset.checkForInterest(seq, obs)
}

// Check if there is no interest in this sequence number and subject across our consumers.
// The consumer passed is optional if we are processing the ack for that consumer.
// Write lock should be held.
func (mset *stream) noInterestWithSubject(seq uint64, subj string, obs *consumer) bool {
	return !mset.checkForInterestWithSubject(seq, subj, obs)
}

// Write lock should be held here for the stream to avoid race conditions on state.
func (mset *stream) checkForInterest(seq uint64, obs *consumer) bool {
	var subj string
	if mset.potentialFilteredConsumers() {
		pmsg := getJSPubMsgFromPool()
		defer pmsg.returnToPool()
		sm, err := mset.store.LoadMsg(seq, &pmsg.StoreMsg)
		if err != nil {
			if err == ErrStoreEOF {
				// Register this as a preAck.
				mset.registerPreAck(obs, seq)
				return true
			}
			mset.clearAllPreAcks(seq)
			return false
		}
		subj = sm.subj
	}
	return mset.checkForInterestWithSubject(seq, subj, obs)
}

// Checks for interest given a sequence and subject.
func (mset *stream) checkForInterestWithSubject(seq uint64, subj string, obs *consumer) bool {
	for _, o := range mset.consumers {
		// If this is us or we have a registered preAck for this consumer continue inspecting.
		if o == obs || mset.hasPreAck(o, seq) {
			continue
		}
		// Check if we need an ack.
		if o.needAck(seq, subj) {
			return true
		}
	}
	mset.clearAllPreAcks(seq)
	return false
}

// Check if we have a pre-registered ack for this sequence.
// Write lock should be held.
func (mset *stream) hasPreAck(o *consumer, seq uint64) bool {
	if o == nil || len(mset.preAcks) == 0 {
		return false
	}
	consumers := mset.preAcks[seq]
	if len(consumers) == 0 {
		return false
	}
	_, found := consumers[o]
	return found
}

// Check if we have all consumers pre-acked for this sequence and subject.
// Write lock should be held.
func (mset *stream) hasAllPreAcks(seq uint64, subj string) bool {
	if len(mset.preAcks) == 0 || len(mset.preAcks[seq]) == 0 {
		return false
	}
	// Since these can be filtered and mutually exclusive,
	// if we have some preAcks we need to check all interest here.
	return mset.noInterestWithSubject(seq, subj, nil)
}

// Check if we have all consumers pre-acked.
// Write lock should be held.
func (mset *stream) clearAllPreAcks(seq uint64) {
	delete(mset.preAcks, seq)
}

// Clear all preAcks below floor.
// Write lock should be held.
func (mset *stream) clearAllPreAcksBelowFloor(floor uint64) {
	for seq := range mset.preAcks {
		if seq < floor {
			delete(mset.preAcks, seq)
		}
	}
}

// This will register an ack for a consumer if it arrives before the actual message.
func (mset *stream) registerPreAckLock(o *consumer, seq uint64) {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	mset.registerPreAck(o, seq)
}

// This will register an ack for a consumer if it arrives before
// the actual message.
// Write lock should be held.
func (mset *stream) registerPreAck(o *consumer, seq uint64) {
	if o == nil {
		return
	}
	if mset.preAcks == nil {
		mset.preAcks = make(map[uint64]map[*consumer]struct{})
	}
	if mset.preAcks[seq] == nil {
		mset.preAcks[seq] = make(map[*consumer]struct{})
	}
	mset.preAcks[seq][o] = struct{}{}
}

// This will clear an ack for a consumer.
// Write lock should be held.
func (mset *stream) clearPreAck(o *consumer, seq uint64) {
	if o == nil || len(mset.preAcks) == 0 {
		return
	}
	if consumers := mset.preAcks[seq]; len(consumers) > 0 {
		delete(consumers, o)
		if len(consumers) == 0 {
			delete(mset.preAcks, seq)
		}
	}
}

// ackMsg is called into from a consumer when we have a WorkQueue or Interest Retention Policy.
func (mset *stream) ackMsg(o *consumer, seq uint64) {
	if seq == 0 || mset.cfg.Retention == LimitsPolicy {
		return
	}

	// Don't make this RLock(). We need to have only 1 running at a time to gauge interest across all consumers.
	mset.mu.Lock()
	if mset.closed || mset.store == nil {
		mset.mu.Unlock()
		return
	}

	var state StreamState
	mset.store.FastState(&state)

	// Make sure this sequence is not below our first sequence.
	if seq < state.FirstSeq {
		mset.clearPreAck(o, seq)
		mset.mu.Unlock()
		return
	}

	// If this has arrived before we have processed the message itself.
	if seq > state.LastSeq {
		mset.registerPreAck(o, seq)
		mset.mu.Unlock()
		return
	}

	var shouldRemove bool
	switch mset.cfg.Retention {
	case WorkQueuePolicy:
		// Normally we just remove a message when its ack'd here but if we have direct consumers
		// from sources and/or mirrors we need to make sure they have delivered the msg.
		shouldRemove = mset.directs <= 0 || mset.noInterest(seq, o)
	case InterestPolicy:
		shouldRemove = mset.noInterest(seq, o)
	}
	mset.mu.Unlock()

	// If nothing else to do.
	if !shouldRemove {
		return
	}

	// If we are here we should attempt to remove.
	if _, err := mset.store.RemoveMsg(seq); err == ErrStoreEOF {
		// This should not happen, but being pedantic.
		mset.registerPreAckLock(o, seq)
	}
}

// Snapshot creates a snapshot for the stream and possibly consumers.
func (mset *stream) snapshot(deadline time.Duration, checkMsgs, includeConsumers bool) (*SnapshotResult, error) {
	mset.mu.RLock()
	if mset.client == nil || mset.store == nil {
		mset.mu.RUnlock()
		return nil, errors.New("invalid stream")
	}
	store := mset.store
	mset.mu.RUnlock()

	return store.Snapshot(deadline, checkMsgs, includeConsumers)
}

const snapsDir = "__snapshots__"

// RestoreStream will restore a stream from a snapshot.
func (a *Account) RestoreStream(ncfg *StreamConfig, r io.Reader) (*stream, error) {
	if ncfg == nil {
		return nil, errors.New("nil config on stream restore")
	}

	s, jsa, err := a.checkForJetStream()
	if err != nil {
		return nil, err
	}

	cfg, apiErr := s.checkStreamCfg(ncfg, a)
	if apiErr != nil {
		return nil, apiErr
	}

	sd := filepath.Join(jsa.storeDir, snapsDir)
	if _, err := os.Stat(sd); os.IsNotExist(err) {
		if err := os.MkdirAll(sd, defaultDirPerms); err != nil {
			return nil, fmt.Errorf("could not create snapshots directory - %v", err)
		}
	}
	sdir, err := os.MkdirTemp(sd, "snap-")
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(sdir); os.IsNotExist(err) {
		if err := os.MkdirAll(sdir, defaultDirPerms); err != nil {
			return nil, fmt.Errorf("could not create snapshots directory - %v", err)
		}
	}
	defer os.RemoveAll(sdir)

	logAndReturnError := func() error {
		a.mu.RLock()
		err := fmt.Errorf("unexpected content (account=%s)", a.Name)
		if a.srv != nil {
			a.srv.Errorf("Stream restore failed due to %v", err)
		}
		a.mu.RUnlock()
		return err
	}
	sdirCheck := filepath.Clean(sdir) + string(os.PathSeparator)

	tr := tar.NewReader(s2.NewReader(r))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of snapshot
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, logAndReturnError()
		}
		fpath := filepath.Join(sdir, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(fpath, sdirCheck) {
			return nil, logAndReturnError()
		}
		os.MkdirAll(filepath.Dir(fpath), defaultDirPerms)
		fd, err := os.OpenFile(fpath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(fd, tr)
		fd.Close()
		if err != nil {
			return nil, err
		}
	}

	// Check metadata.
	// The cfg passed in will be the new identity for the stream.
	var fcfg FileStreamInfo
	b, err := os.ReadFile(filepath.Join(sdir, JetStreamMetaFile))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &fcfg); err != nil {
		return nil, err
	}

	// Check to make sure names match.
	if fcfg.Name != cfg.Name {
		return nil, errors.New("stream names do not match")
	}

	// See if this stream already exists.
	if _, err := a.lookupStream(cfg.Name); err == nil {
		return nil, NewJSStreamNameExistRestoreFailedError()
	}
	// Move into the correct place here.
	ndir := filepath.Join(jsa.storeDir, streamsDir, cfg.Name)
	// Remove old one if for some reason it is still here.
	if _, err := os.Stat(ndir); err == nil {
		os.RemoveAll(ndir)
	}
	// Make sure our destination streams directory exists.
	if err := os.MkdirAll(filepath.Join(jsa.storeDir, streamsDir), defaultDirPerms); err != nil {
		return nil, err
	}
	// Move into new location.
	if err := os.Rename(sdir, ndir); err != nil {
		return nil, err
	}

	if cfg.Template != _EMPTY_ {
		if err := jsa.addStreamNameToTemplate(cfg.Template, cfg.Name); err != nil {
			return nil, err
		}
	}
	mset, err := a.addStream(&cfg)
	if err != nil {
		return nil, err
	}
	if !fcfg.Created.IsZero() {
		mset.setCreatedTime(fcfg.Created)
	}
	lseq := mset.lastSeq()

	// Make sure we do an update if the configs have changed.
	if !reflect.DeepEqual(fcfg.StreamConfig, cfg) {
		if err := mset.update(&cfg); err != nil {
			return nil, err
		}
	}

	// Now do consumers.
	odir := filepath.Join(ndir, consumerDir)
	ofis, _ := os.ReadDir(odir)
	for _, ofi := range ofis {
		metafile := filepath.Join(odir, ofi.Name(), JetStreamMetaFile)
		metasum := filepath.Join(odir, ofi.Name(), JetStreamMetaFileSum)
		if _, err := os.Stat(metafile); os.IsNotExist(err) {
			mset.stop(true, false)
			return nil, fmt.Errorf("error restoring consumer [%q]: %v", ofi.Name(), err)
		}
		buf, err := os.ReadFile(metafile)
		if err != nil {
			mset.stop(true, false)
			return nil, fmt.Errorf("error restoring consumer [%q]: %v", ofi.Name(), err)
		}
		if _, err := os.Stat(metasum); os.IsNotExist(err) {
			mset.stop(true, false)
			return nil, fmt.Errorf("error restoring consumer [%q]: %v", ofi.Name(), err)
		}
		var cfg FileConsumerInfo
		if err := json.Unmarshal(buf, &cfg); err != nil {
			mset.stop(true, false)
			return nil, fmt.Errorf("error restoring consumer [%q]: %v", ofi.Name(), err)
		}
		isEphemeral := !isDurableConsumer(&cfg.ConsumerConfig)
		if isEphemeral {
			// This is an ephermal consumer and this could fail on restart until
			// the consumer can reconnect. We will create it as a durable and switch it.
			cfg.ConsumerConfig.Durable = ofi.Name()
		}
		obs, err := mset.addConsumer(&cfg.ConsumerConfig)
		if err != nil {
			mset.stop(true, false)
			return nil, fmt.Errorf("error restoring consumer [%q]: %v", ofi.Name(), err)
		}
		if isEphemeral {
			obs.switchToEphemeral()
		}
		if !cfg.Created.IsZero() {
			obs.setCreatedTime(cfg.Created)
		}
		obs.mu.Lock()
		err = obs.readStoredState(lseq)
		obs.mu.Unlock()
		if err != nil {
			mset.stop(true, false)
			return nil, fmt.Errorf("error restoring consumer [%q]: %v", ofi.Name(), err)
		}
	}
	return mset, nil
}

// This is to check for dangling messages on interest retention streams.
// Issue https://github.com/nats-io/nats-server/issues/3612
func (mset *stream) checkForOrphanMsgs() {
	mset.mu.RLock()
	consumers := make([]*consumer, 0, len(mset.consumers))
	for _, o := range mset.consumers {
		consumers = append(consumers, o)
	}
	mset.mu.RUnlock()
	for _, o := range consumers {
		o.checkStateForInterestStream()
	}
}

// Check on startup to make sure that consumers replication matches us.
// Interest retention requires replication matches.
func (mset *stream) checkConsumerReplication() {
	mset.mu.RLock()
	defer mset.mu.RUnlock()

	if mset.cfg.Retention != InterestPolicy {
		return
	}

	s, acc := mset.srv, mset.acc
	for _, o := range mset.consumers {
		o.mu.RLock()
		// Consumer replicas 0 can be a legit config for the replicas and we will inherit from the stream
		// when this is the case.
		if mset.cfg.Replicas != o.cfg.Replicas && o.cfg.Replicas != 0 {
			s.Errorf("consumer '%s > %s > %s' MUST match replication (%d vs %d) of stream with interest policy",
				acc, mset.cfg.Name, o.cfg.Name, mset.cfg.Replicas, o.cfg.Replicas)
		}
		o.mu.RUnlock()
	}
}

// Will check if we are running in the monitor already and if not set the appropriate flag.
func (mset *stream) checkInMonitor() bool {
	mset.mu.Lock()
	defer mset.mu.Unlock()

	if mset.inMonitor {
		return true
	}
	mset.inMonitor = true
	return false
}

// Clear us being in the monitor routine.
func (mset *stream) clearMonitorRunning() {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	mset.inMonitor = false
}

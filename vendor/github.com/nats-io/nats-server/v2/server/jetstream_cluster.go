// Copyright 2020-2023 The NATS Authors
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
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/minio/highwayhash"
	"github.com/nats-io/nuid"
)

// jetStreamCluster holds information about the meta group and stream assignments.
type jetStreamCluster struct {
	// The metacontroller raftNode.
	meta RaftNode
	// For stream and consumer assignments. All servers will have this be the same.
	// ACCOUNT -> STREAM -> Stream Assignment -> Consumers
	streams map[string]map[string]*streamAssignment
	// These are inflight proposals and used to apply limits when there are
	// concurrent requests that would otherwise be accepted.
	// We also record the group for the stream. This is needed since if we have
	// concurrent requests for same account and stream we need to let it process to get
	// a response but they need to be same group, peers etc.
	inflight map[string]map[string]*raftGroup
	// Signals meta-leader should check the stream assignments.
	streamsCheck bool
	// Server.
	s *Server
	// Internal client.
	c *client
	// Processing assignment results.
	streamResults   *subscription
	consumerResults *subscription
	// System level request to have the leader stepdown.
	stepdown *subscription
	// System level requests to remove a peer.
	peerRemove *subscription
	// System level request to move a stream
	peerStreamMove *subscription
	// System level request to cancel a stream move
	peerStreamCancelMove *subscription
	// To pop out the monitorCluster before the raft layer.
	qch chan struct{}
}

// Used to guide placement of streams and meta controllers in clustered JetStream.
type Placement struct {
	Cluster string   `json:"cluster,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// Define types of the entry.
type entryOp uint8

// ONLY ADD TO THE END, DO NOT INSERT IN BETWEEN WILL BREAK SERVER INTEROP.
const (
	// Meta ops.
	assignStreamOp entryOp = iota
	assignConsumerOp
	removeStreamOp
	removeConsumerOp
	// Stream ops.
	streamMsgOp
	purgeStreamOp
	deleteMsgOp
	// Consumer ops.
	updateDeliveredOp
	updateAcksOp
	// Compressed consumer assignments.
	assignCompressedConsumerOp
	// Filtered Consumer skip.
	updateSkipOp
	// Update Stream.
	updateStreamOp
	// For updating information on pending pull requests.
	addPendingRequest
	removePendingRequest
	// For sending compressed streams, either through RAFT or catchup.
	compressedStreamMsgOp
)

// raftGroups are controlled by the metagroup controller.
// The raftGroups will house streams and consumers.
type raftGroup struct {
	Name      string      `json:"name"`
	Peers     []string    `json:"peers"`
	Storage   StorageType `json:"store"`
	Cluster   string      `json:"cluster,omitempty"`
	Preferred string      `json:"preferred,omitempty"`
	// Internal
	node RaftNode
}

// streamAssignment is what the meta controller uses to assign streams to peers.
type streamAssignment struct {
	Client  *ClientInfo   `json:"client,omitempty"`
	Created time.Time     `json:"created"`
	Config  *StreamConfig `json:"stream"`
	Group   *raftGroup    `json:"group"`
	Sync    string        `json:"sync"`
	Subject string        `json:"subject"`
	Reply   string        `json:"reply"`
	Restore *StreamState  `json:"restore_state,omitempty"`
	// Internal
	consumers  map[string]*consumerAssignment
	responded  bool
	recovering bool
	err        error
}

// consumerAssignment is what the meta controller uses to assign consumers to streams.
type consumerAssignment struct {
	Client  *ClientInfo     `json:"client,omitempty"`
	Created time.Time       `json:"created"`
	Name    string          `json:"name"`
	Stream  string          `json:"stream"`
	Config  *ConsumerConfig `json:"consumer"`
	Group   *raftGroup      `json:"group"`
	Subject string          `json:"subject"`
	Reply   string          `json:"reply"`
	State   *ConsumerState  `json:"state,omitempty"`
	// Internal
	responded  bool
	recovering bool
	deleted    bool
	err        error
}

// streamPurge is what the stream leader will replicate when purging a stream.
type streamPurge struct {
	Client  *ClientInfo              `json:"client,omitempty"`
	Stream  string                   `json:"stream"`
	LastSeq uint64                   `json:"last_seq"`
	Subject string                   `json:"subject"`
	Reply   string                   `json:"reply"`
	Request *JSApiStreamPurgeRequest `json:"request,omitempty"`
}

// streamMsgDelete is what the stream leader will replicate when deleting a message.
type streamMsgDelete struct {
	Client  *ClientInfo `json:"client,omitempty"`
	Stream  string      `json:"stream"`
	Seq     uint64      `json:"seq"`
	NoErase bool        `json:"no_erase,omitempty"`
	Subject string      `json:"subject"`
	Reply   string      `json:"reply"`
}

const (
	defaultStoreDirName  = "_js_"
	defaultMetaGroupName = "_meta_"
	defaultMetaFSBlkSize = 1024 * 1024
	jsExcludePlacement   = "!jetstream"
)

// Returns information useful in mixed mode.
func (s *Server) trackedJetStreamServers() (js, total int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.running || !s.eventsEnabled() {
		return -1, -1
	}
	s.nodeToInfo.Range(func(k, v interface{}) bool {
		si := v.(nodeInfo)
		if si.js {
			js++
		}
		total++
		return true
	})
	return js, total
}

func (s *Server) getJetStreamCluster() (*jetStream, *jetStreamCluster) {
	s.mu.RLock()
	shutdown, js := s.shutdown, s.js
	s.mu.RUnlock()

	if shutdown || js == nil {
		return nil, nil
	}

	// Only set once, do not need a lock.
	return js, js.cluster
}

func (s *Server) JetStreamIsClustered() bool {
	js := s.getJetStream()
	if js == nil {
		return false
	}
	return js.isClustered()
}

func (s *Server) JetStreamIsLeader() bool {
	js := s.getJetStream()
	if js == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.cluster.isLeader()
}

func (s *Server) JetStreamIsCurrent() bool {
	js := s.getJetStream()
	if js == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.cluster.isCurrent()
}

func (s *Server) JetStreamSnapshotMeta() error {
	js := s.getJetStream()
	if js == nil {
		return NewJSNotEnabledError()
	}
	js.mu.RLock()
	cc := js.cluster
	isLeader := cc.isLeader()
	meta := cc.meta
	js.mu.RUnlock()

	if !isLeader {
		return errNotLeader
	}

	return meta.InstallSnapshot(js.metaSnapshot())
}

func (s *Server) JetStreamStepdownStream(account, stream string) error {
	js, cc := s.getJetStreamCluster()
	if js == nil {
		return NewJSNotEnabledError()
	}
	if cc == nil {
		return NewJSClusterNotActiveError()
	}
	// Grab account
	acc, err := s.LookupAccount(account)
	if err != nil {
		return err
	}
	// Grab stream
	mset, err := acc.lookupStream(stream)
	if err != nil {
		return err
	}

	if node := mset.raftNode(); node != nil && node.Leader() {
		node.StepDown()
	}

	return nil
}

func (s *Server) JetStreamStepdownConsumer(account, stream, consumer string) error {
	js, cc := s.getJetStreamCluster()
	if js == nil {
		return NewJSNotEnabledError()
	}
	if cc == nil {
		return NewJSClusterNotActiveError()
	}
	// Grab account
	acc, err := s.LookupAccount(account)
	if err != nil {
		return err
	}
	// Grab stream
	mset, err := acc.lookupStream(stream)
	if err != nil {
		return err
	}

	o := mset.lookupConsumer(consumer)
	if o == nil {
		return NewJSConsumerNotFoundError()
	}

	if node := o.raftNode(); node != nil && node.Leader() {
		node.StepDown()
	}

	return nil
}

func (s *Server) JetStreamSnapshotStream(account, stream string) error {
	js, cc := s.getJetStreamCluster()
	if js == nil {
		return NewJSNotEnabledForAccountError()
	}
	if cc == nil {
		return NewJSClusterNotActiveError()
	}
	// Grab account
	acc, err := s.LookupAccount(account)
	if err != nil {
		return err
	}
	// Grab stream
	mset, err := acc.lookupStream(stream)
	if err != nil {
		return err
	}

	mset.mu.RLock()
	if !mset.node.Leader() {
		mset.mu.RUnlock()
		return NewJSNotEnabledForAccountError()
	}
	n := mset.node
	mset.mu.RUnlock()

	return n.InstallSnapshot(mset.stateSnapshot())
}

func (s *Server) JetStreamClusterPeers() []string {
	js := s.getJetStream()
	if js == nil {
		return nil
	}
	js.mu.RLock()
	defer js.mu.RUnlock()

	cc := js.cluster
	if !cc.isLeader() || cc.meta == nil {
		return nil
	}
	peers := cc.meta.Peers()
	var nodes []string
	for _, p := range peers {
		si, ok := s.nodeToInfo.Load(p.ID)
		if !ok || si == nil {
			continue
		}
		ni := si.(nodeInfo)
		// Ignore if offline, no JS, or no current stats have been received.
		if ni.offline || !ni.js || ni.stats == nil {
			continue
		}
		nodes = append(nodes, si.(nodeInfo).name)
	}
	return nodes
}

// Read lock should be held.
func (cc *jetStreamCluster) isLeader() bool {
	if cc == nil {
		// Non-clustered mode
		return true
	}
	return cc.meta != nil && cc.meta.Leader()
}

// isCurrent will determine if this node is a leader or an up to date follower.
// Read lock should be held.
func (cc *jetStreamCluster) isCurrent() bool {
	if cc == nil {
		// Non-clustered mode
		return true
	}
	if cc.meta == nil {
		return false
	}
	return cc.meta.Current()
}

// isStreamCurrent will determine if the stream is up to date.
// For R1 it will make sure the stream is present on this server.
// Read lock should be held.
func (cc *jetStreamCluster) isStreamCurrent(account, stream string) bool {
	if cc == nil {
		// Non-clustered mode
		return true
	}
	as := cc.streams[account]
	if as == nil {
		return false
	}
	sa := as[stream]
	if sa == nil {
		return false
	}
	rg := sa.Group
	if rg == nil {
		return false
	}

	if rg.node == nil || rg.node.Current() {
		// Check if we are processing a snapshot and are catching up.
		acc, err := cc.s.LookupAccount(account)
		if err != nil {
			return false
		}
		mset, err := acc.lookupStream(stream)
		if err != nil {
			return false
		}
		if mset.isCatchingUp() {
			return false
		}
		// Success.
		return true
	}

	return false
}

// isStreamHealthy will determine if the stream is up to date or very close.
// For R1 it will make sure the stream is present on this server.
// Read lock should be held.
func (js *jetStream) isStreamHealthy(account, stream string) bool {
	cc := js.cluster
	if cc == nil {
		// Non-clustered mode
		return true
	}
	as := cc.streams[account]
	if as == nil {
		return false
	}
	sa := as[stream]
	if sa == nil {
		return false
	}
	rg := sa.Group
	if rg == nil {
		return false
	}

	if rg.node == nil || rg.node.Healthy() {
		// Check if we are processing a snapshot and are catching up.
		acc, err := cc.s.LookupAccount(account)
		if err != nil {
			return false
		}
		mset, err := acc.lookupStream(stream)
		if err != nil {
			return false
		}
		if mset.isCatchingUp() {
			return false
		}
		// Success.
		return true
	}

	return false
}

// isConsumerCurrent will determine if the consumer is up to date.
// For R1 it will make sure the consunmer is present on this server.
// Read lock should be held.
func (js *jetStream) isConsumerCurrent(account, stream, consumer string) bool {
	cc := js.cluster
	if cc == nil {
		// Non-clustered mode
		return true
	}
	acc, err := cc.s.LookupAccount(account)
	if err != nil {
		return false
	}
	mset, err := acc.lookupStream(stream)
	if err != nil {
		return false
	}
	o := mset.lookupConsumer(consumer)
	if o == nil {
		return false
	}
	if n := o.raftNode(); n != nil && !n.Current() {
		return false
	}
	return true
}

// subjectsOverlap checks all existing stream assignments for the account cross-cluster for subject overlap
// Use only for clustered JetStream
// Read lock should be held.
func (jsc *jetStreamCluster) subjectsOverlap(acc string, subjects []string, osa *streamAssignment) bool {
	asa := jsc.streams[acc]
	for _, sa := range asa {
		// can't overlap yourself, assume osa pre-checked for deep equal if passed
		if osa != nil && sa == osa {
			continue
		}
		for _, subj := range sa.Config.Subjects {
			for _, tsubj := range subjects {
				if SubjectsCollide(tsubj, subj) {
					return true
				}
			}
		}
	}
	return false
}

func (a *Account) getJetStreamFromAccount() (*Server, *jetStream, *jsAccount) {
	a.mu.RLock()
	jsa := a.js
	a.mu.RUnlock()
	if jsa == nil {
		return nil, nil, nil
	}
	jsa.mu.RLock()
	js := jsa.js
	jsa.mu.RUnlock()
	if js == nil {
		return nil, nil, nil
	}
	js.mu.RLock()
	s := js.srv
	js.mu.RUnlock()
	return s, js, jsa
}

func (s *Server) JetStreamIsStreamLeader(account, stream string) bool {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return cc.isStreamLeader(account, stream)
}

func (a *Account) JetStreamIsStreamLeader(stream string) bool {
	s, js, jsa := a.getJetStreamFromAccount()
	if s == nil || js == nil || jsa == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.cluster.isStreamLeader(a.Name, stream)
}

func (s *Server) JetStreamIsStreamCurrent(account, stream string) bool {
	js, cc := s.getJetStreamCluster()
	if js == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return cc.isStreamCurrent(account, stream)
}

func (a *Account) JetStreamIsConsumerLeader(stream, consumer string) bool {
	s, js, jsa := a.getJetStreamFromAccount()
	if s == nil || js == nil || jsa == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.cluster.isConsumerLeader(a.Name, stream, consumer)
}

func (s *Server) JetStreamIsConsumerLeader(account, stream, consumer string) bool {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return cc.isConsumerLeader(account, stream, consumer)
}

func (s *Server) enableJetStreamClustering() error {
	if !s.isRunning() {
		return nil
	}
	js := s.getJetStream()
	if js == nil {
		return NewJSNotEnabledForAccountError()
	}
	// Already set.
	if js.cluster != nil {
		return nil
	}

	s.Noticef("Starting JetStream cluster")
	// We need to determine if we have a stable cluster name and expected number of servers.
	s.Debugf("JetStream cluster checking for stable cluster name and peers")

	hasLeafNodeSystemShare := s.canExtendOtherDomain()
	if s.isClusterNameDynamic() && !hasLeafNodeSystemShare {
		return errors.New("JetStream cluster requires cluster name")
	}
	if s.configuredRoutes() == 0 && !hasLeafNodeSystemShare {
		return errors.New("JetStream cluster requires configured routes or solicited leafnode for the system account")
	}

	return js.setupMetaGroup()
}

// isClustered returns if we are clustered.
// Lock should not be held.
func (js *jetStream) isClustered() bool {
	// This is only ever set, no need for lock here.
	return js.cluster != nil
}

// isClusteredNoLock returns if we are clustered, but unlike isClustered() does
// not use the jetstream's lock, instead, uses an atomic operation.
// There are situations where some code wants to know if we are clustered but
// can't use js.isClustered() without causing a lock inversion.
func (js *jetStream) isClusteredNoLock() bool {
	return atomic.LoadInt32(&js.clustered) == 1
}

func (js *jetStream) setupMetaGroup() error {
	s := js.srv
	s.Noticef("Creating JetStream metadata controller")

	// Setup our WAL for the metagroup.
	sysAcc := s.SystemAccount()
	storeDir := filepath.Join(js.config.StoreDir, sysAcc.Name, defaultStoreDirName, defaultMetaGroupName)

	fs, err := newFileStoreWithCreated(
		FileStoreConfig{StoreDir: storeDir, BlockSize: defaultMetaFSBlkSize, AsyncFlush: false},
		StreamConfig{Name: defaultMetaGroupName, Storage: FileStorage},
		time.Now().UTC(),
		s.jsKeyGen(defaultMetaGroupName),
	)
	if err != nil {
		s.Errorf("Error creating filestore: %v", err)
		return err
	}
	// Register our server.
	fs.registerServer(s)

	cfg := &RaftConfig{Name: defaultMetaGroupName, Store: storeDir, Log: fs}

	// If we are soliciting leafnode connections and we are sharing a system account and do not disable it with a hint,
	// we want to move to observer mode so that we extend the solicited cluster or supercluster but do not form our own.
	cfg.Observer = s.canExtendOtherDomain() && s.opts.JetStreamExtHint != jsNoExtend

	var bootstrap bool
	if ps, err := readPeerState(storeDir); err != nil {
		s.Noticef("JetStream cluster bootstrapping")
		bootstrap = true
		peers := s.ActivePeers()
		s.Debugf("JetStream cluster initial peers: %+v", peers)
		if err := s.bootstrapRaftNode(cfg, peers, false); err != nil {
			return err
		}
		if cfg.Observer {
			s.Noticef("Turning JetStream metadata controller Observer Mode on")
		}
	} else {
		s.Noticef("JetStream cluster recovering state")
		// correlate the value of observer with observations from a previous run.
		if cfg.Observer {
			switch ps.domainExt {
			case extExtended:
				s.Noticef("Keeping JetStream metadata controller Observer Mode on - due to previous contact")
			case extNotExtended:
				s.Noticef("Turning JetStream metadata controller Observer Mode off - due to previous contact")
				cfg.Observer = false
			case extUndetermined:
				s.Noticef("Turning JetStream metadata controller Observer Mode on - no previous contact")
				s.Noticef("In cases where JetStream will not be extended")
				s.Noticef("and waiting for leader election until first contact is not acceptable,")
				s.Noticef(`manually disable Observer Mode by setting the JetStream Option "extension_hint: %s"`, jsNoExtend)
			}
		} else {
			// To track possible configuration changes, responsible for an altered value of cfg.Observer,
			// set extension state to undetermined.
			ps.domainExt = extUndetermined
			if err := writePeerState(storeDir, ps); err != nil {
				return err
			}
		}
	}

	// Start up our meta node.
	n, err := s.startRaftNode(sysAcc.GetName(), cfg)
	if err != nil {
		s.Warnf("Could not start metadata controller: %v", err)
		return err
	}

	// If we are bootstrapped with no state, start campaign early.
	if bootstrap {
		n.Campaign()
	}

	c := s.createInternalJetStreamClient()
	sacc := s.SystemAccount()

	js.mu.Lock()
	defer js.mu.Unlock()
	js.cluster = &jetStreamCluster{
		meta:    n,
		streams: make(map[string]map[string]*streamAssignment),
		s:       s,
		c:       c,
		qch:     make(chan struct{}),
	}
	atomic.StoreInt32(&js.clustered, 1)
	c.registerWithAccount(sacc)

	js.srv.startGoRoutine(js.monitorCluster)
	return nil
}

func (js *jetStream) getMetaGroup() RaftNode {
	js.mu.RLock()
	defer js.mu.RUnlock()
	if js.cluster == nil {
		return nil
	}
	return js.cluster.meta
}

func (js *jetStream) server() *Server {
	js.mu.RLock()
	s := js.srv
	js.mu.RUnlock()
	return s
}

// Will respond if we do not think we have a metacontroller leader.
func (js *jetStream) isLeaderless() bool {
	js.mu.RLock()
	defer js.mu.RUnlock()

	cc := js.cluster
	if cc == nil || cc.meta == nil {
		return false
	}
	// If we don't have a leader.
	// Make sure we have been running for enough time.
	if cc.meta.GroupLeader() == _EMPTY_ && time.Since(cc.meta.Created()) > lostQuorumIntervalDefault {
		return true
	}
	return false
}

// Will respond iff we are a member and we know we have no leader.
func (js *jetStream) isGroupLeaderless(rg *raftGroup) bool {
	if rg == nil || js == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()

	cc := js.cluster

	// If we are not a member we can not say..
	if cc.meta == nil {
		return false
	}
	if !rg.isMember(cc.meta.ID()) {
		return false
	}
	// Single peer groups always have a leader if we are here.
	if rg.node == nil {
		return false
	}
	// If we don't have a leader.
	if rg.node.GroupLeader() == _EMPTY_ {
		// Threshold for jetstream startup.
		const startupThreshold = 10 * time.Second

		if rg.node.HadPreviousLeader() {
			// Make sure we have been running long enough to intelligently determine this.
			if time.Since(js.started) > startupThreshold {
				return true
			}
		}
		// Make sure we have been running for enough time.
		if time.Since(rg.node.Created()) > lostQuorumIntervalDefault {
			return true
		}
	}

	return false
}

func (s *Server) JetStreamIsStreamAssigned(account, stream string) bool {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return false
	}
	acc, _ := s.LookupAccount(account)
	if acc == nil {
		return false
	}
	js.mu.RLock()
	assigned := cc.isStreamAssigned(acc, stream)
	js.mu.RUnlock()
	return assigned
}

// streamAssigned informs us if this server has this stream assigned.
func (jsa *jsAccount) streamAssigned(stream string) bool {
	jsa.mu.RLock()
	js, acc := jsa.js, jsa.account
	jsa.mu.RUnlock()

	if js == nil {
		return false
	}
	js.mu.RLock()
	assigned := js.cluster.isStreamAssigned(acc, stream)
	js.mu.RUnlock()
	return assigned
}

// Read lock should be held.
func (cc *jetStreamCluster) isStreamAssigned(a *Account, stream string) bool {
	// Non-clustered mode always return true.
	if cc == nil {
		return true
	}
	if cc.meta == nil {
		return false
	}
	as := cc.streams[a.Name]
	if as == nil {
		return false
	}
	sa := as[stream]
	if sa == nil {
		return false
	}
	rg := sa.Group
	if rg == nil {
		return false
	}
	// Check if we are the leader of this raftGroup assigned to the stream.
	ourID := cc.meta.ID()
	for _, peer := range rg.Peers {
		if peer == ourID {
			return true
		}
	}
	return false
}

// Read lock should be held.
func (cc *jetStreamCluster) isStreamLeader(account, stream string) bool {
	// Non-clustered mode always return true.
	if cc == nil {
		return true
	}
	if cc.meta == nil {
		return false
	}

	var sa *streamAssignment
	if as := cc.streams[account]; as != nil {
		sa = as[stream]
	}
	if sa == nil {
		return false
	}
	rg := sa.Group
	if rg == nil {
		return false
	}
	// Check if we are the leader of this raftGroup assigned to the stream.
	ourID := cc.meta.ID()
	for _, peer := range rg.Peers {
		if peer == ourID {
			if len(rg.Peers) == 1 || rg.node != nil && rg.node.Leader() {
				return true
			}
		}
	}
	return false
}

// Read lock should be held.
func (cc *jetStreamCluster) isConsumerLeader(account, stream, consumer string) bool {
	// Non-clustered mode always return true.
	if cc == nil {
		return true
	}
	if cc.meta == nil {
		return false
	}

	var sa *streamAssignment
	if as := cc.streams[account]; as != nil {
		sa = as[stream]
	}
	if sa == nil {
		return false
	}
	// Check if we are the leader of this raftGroup assigned to this consumer.
	ca := sa.consumers[consumer]
	if ca == nil {
		return false
	}
	rg := ca.Group
	ourID := cc.meta.ID()
	for _, peer := range rg.Peers {
		if peer == ourID {
			if len(rg.Peers) == 1 || (rg.node != nil && rg.node.Leader()) {
				return true
			}
		}
	}
	return false
}

// Remove the stream `streamName` for the account `accName` from the inflight
// proposals map. This is done on success (processStreamAssignment) or on
// failure (processStreamAssignmentResults).
// (Write) Lock held on entry.
func (cc *jetStreamCluster) removeInflightProposal(accName, streamName string) {
	streams, ok := cc.inflight[accName]
	if !ok {
		return
	}
	delete(streams, streamName)
	if len(streams) == 0 {
		delete(cc.inflight, accName)
	}
}

// Return the cluster quit chan.
func (js *jetStream) clusterQuitC() chan struct{} {
	js.mu.RLock()
	defer js.mu.RUnlock()
	if js.cluster != nil {
		return js.cluster.qch
	}
	return nil
}

// Mark that the meta layer is recovering.
func (js *jetStream) setMetaRecovering() {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.cluster != nil {
		// metaRecovering
		js.metaRecovering = true
	}
}

// Mark that the meta layer is no longer recovering.
func (js *jetStream) clearMetaRecovering() {
	js.mu.Lock()
	defer js.mu.Unlock()
	js.metaRecovering = false
}

// Return whether the meta layer is recovering.
func (js *jetStream) isMetaRecovering() bool {
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.metaRecovering
}

// During recovery track any stream and consumer delete and update operations.
type recoveryUpdates struct {
	removeStreams   map[string]*streamAssignment
	removeConsumers map[string]*consumerAssignment
	updateStreams   map[string]*streamAssignment
	updateConsumers map[string]*consumerAssignment
}

// Called after recovery of the cluster on startup to check for any orphans.
// Streams and consumers are recovered from disk, and the meta layer's mappings
// should clean them up, but under crash scenarios there could be orphans.
func (js *jetStream) checkForOrphans() {
	consumerName := func(o *consumer) string {
		o.mu.RLock()
		defer o.mu.RUnlock()
		return o.name
	}

	// Can not hold jetstream lock while trying to delete streams or consumers.
	js.mu.Lock()
	s, cc := js.srv, js.cluster
	s.Debugf("JetStream cluster checking for orphans")

	var streams []*stream
	var consumers []*consumer

	for accName, jsa := range js.accounts {
		asa := cc.streams[accName]
		for stream, mset := range jsa.streams {
			if sa := asa[stream]; sa == nil {
				streams = append(streams, mset)
			} else {
				// This one is good, check consumers now.
				for _, o := range mset.getConsumers() {
					consumer := consumerName(o)
					if sa.consumers[consumer] == nil {
						consumers = append(consumers, o)
					}
				}
			}
		}
	}
	js.mu.Unlock()

	for _, mset := range streams {
		mset.mu.RLock()
		accName, stream := mset.acc.Name, mset.cfg.Name
		mset.mu.RUnlock()
		s.Warnf("Detected orphaned stream '%s > %s', will cleanup", accName, stream)
		if err := mset.delete(); err != nil {
			s.Warnf("Deleting stream encountered an error: %v", err)
		}
	}
	for _, o := range consumers {
		o.mu.RLock()
		accName, mset, consumer := o.acc.Name, o.mset, o.name
		o.mu.RUnlock()
		stream := "N/A"
		if mset != nil {
			mset.mu.RLock()
			stream = mset.cfg.Name
			mset.mu.RUnlock()
		}
		s.Warnf("Detected orphaned consumer '%s > %s > %s', will cleanup", accName, stream, consumer)
		if err := o.delete(); err != nil {
			s.Warnf("Deleting consumer encountered an error: %v", err)
		}
	}
}

func (js *jetStream) monitorCluster() {
	s, n := js.server(), js.getMetaGroup()
	qch, rqch, lch, aq := js.clusterQuitC(), n.QuitC(), n.LeadChangeC(), n.ApplyQ()

	defer s.grWG.Done()

	s.Debugf("Starting metadata monitor")
	defer s.Debugf("Exiting metadata monitor")

	// Make sure to stop the raft group on exit to prevent accidental memory bloat.
	defer n.Stop()

	const compactInterval = 2 * time.Minute
	t := time.NewTicker(compactInterval)
	defer t.Stop()

	// Used to check cold boot cluster when possibly in mixed mode.
	const leaderCheckInterval = time.Second
	lt := time.NewTicker(leaderCheckInterval)
	defer lt.Stop()

	var (
		isLeader     bool
		lastSnap     []byte
		lastSnapTime time.Time
		minSnapDelta = 10 * time.Second
	)

	// Highwayhash key for generating hashes.
	key := make([]byte, 32)
	rand.Read(key)

	// Set to true to start.
	js.setMetaRecovering()

	// Snapshotting function.
	doSnapshot := func() {
		// Suppress during recovery.
		if js.isMetaRecovering() {
			return
		}
		snap := js.metaSnapshot()
		if hash := highwayhash.Sum(snap, key); !bytes.Equal(hash[:], lastSnap) {
			if err := n.InstallSnapshot(snap); err == nil {
				lastSnap, lastSnapTime = hash[:], time.Now()
			} else if err != errNoSnapAvailable && err != errNodeClosed {
				s.Warnf("Error snapshotting JetStream cluster state: %v", err)
			}
		}
	}

	ru := &recoveryUpdates{
		removeStreams:   make(map[string]*streamAssignment),
		removeConsumers: make(map[string]*consumerAssignment),
		updateStreams:   make(map[string]*streamAssignment),
		updateConsumers: make(map[string]*consumerAssignment),
	}

	for {
		select {
		case <-s.quitCh:
			return
		case <-rqch:
			return
		case <-qch:
			// Clean signal from shutdown routine so do best effort attempt to snapshot meta layer.
			doSnapshot()
			// Return the signal back since shutdown will be waiting.
			close(qch)
			return
		case <-aq.ch:
			ces := aq.pop()
			for _, ce := range ces {
				if ce == nil {
					// Signals we have replayed all of our metadata.
					js.clearMetaRecovering()
					// Process any removes that are still valid after recovery.
					for _, ca := range ru.removeConsumers {
						js.processConsumerRemoval(ca)
					}
					for _, sa := range ru.removeStreams {
						js.processStreamRemoval(sa)
					}
					// Process pending updates.
					for _, sa := range ru.updateStreams {
						js.processUpdateStreamAssignment(sa)
					}
					// Now consumers.
					for _, ca := range ru.updateConsumers {
						js.processConsumerAssignment(ca)
					}
					// Clear.
					ru = nil
					s.Debugf("Recovered JetStream cluster metadata")
					js.checkForOrphans()
					continue
				}
				// FIXME(dlc) - Deal with errors.
				if didSnap, didStreamRemoval, didConsumerRemoval, err := js.applyMetaEntries(ce.Entries, ru); err == nil {
					_, nb := n.Applied(ce.Index)
					if js.hasPeerEntries(ce.Entries) || didSnap || didStreamRemoval {
						// Since we received one make sure we have our own since we do not store
						// our meta state outside of raft.
						doSnapshot()
					} else if didConsumerRemoval && time.Since(lastSnapTime) > minSnapDelta/2 {
						doSnapshot()
					} else if lls := len(lastSnap); nb > uint64(lls*8) && lls > 0 && time.Since(lastSnapTime) > minSnapDelta {
						doSnapshot()
					}
				}
			}
			aq.recycle(&ces)
		case isLeader = <-lch:
			// For meta layer synchronize everyone to our state on becoming leader.
			if isLeader {
				n.SendSnapshot(js.metaSnapshot())
			}
			// Process the change.
			js.processLeaderChange(isLeader)
			if isLeader {
				s.sendInternalMsgLocked(serverStatsPingReqSubj, _EMPTY_, nil, nil)
				// Install a snapshot as we become leader.
				js.checkClusterSize()
				doSnapshot()
			}

		case <-t.C:
			doSnapshot()
			// Periodically check the cluster size.
			if n.Leader() {
				js.checkClusterSize()
			}
		case <-lt.C:
			s.Debugf("Checking JetStream cluster state")
			// If we have a current leader or had one in the past we can cancel this here since the metaleader
			// will be in charge of all peer state changes.
			// For cold boot only.
			if n.GroupLeader() != _EMPTY_ || n.HadPreviousLeader() {
				lt.Stop()
				continue
			}
			// If we are here we do not have a leader and we did not have a previous one, so cold start.
			// Check to see if we can adjust our cluster size down iff we are in mixed mode and we have
			// seen a total that is what our original estimate was.
			cs := n.ClusterSize()
			if js, total := s.trackedJetStreamServers(); js < total && total >= cs && js != cs {
				s.Noticef("Adjusting JetStream expected peer set size to %d from original %d", js, cs)
				n.AdjustBootClusterSize(js)
			}
		}
	}
}

// This is called on first leader transition to double check the peers and cluster set size.
func (js *jetStream) checkClusterSize() {
	s, n := js.server(), js.getMetaGroup()
	if n == nil {
		return
	}
	// We will check that we have a correct cluster set size by checking for any non-js servers
	// which can happen in mixed mode.
	ps := n.(*raft).currentPeerState()
	if len(ps.knownPeers) >= ps.clusterSize {
		return
	}

	// Grab our active peers.
	peers := s.ActivePeers()

	// If we have not registered all of our peers yet we can't do
	// any adjustments based on a mixed mode. We will periodically check back.
	if len(peers) < ps.clusterSize {
		return
	}

	s.Debugf("Checking JetStream cluster size")

	// If we are here our known set as the leader is not the same as the cluster size.
	// Check to see if we have a mixed mode setup.
	var totalJS int
	for _, p := range peers {
		if si, ok := s.nodeToInfo.Load(p); ok && si != nil {
			if si.(nodeInfo).js {
				totalJS++
			}
		}
	}
	// If we have less then our cluster size adjust that here. Can not do individual peer removals since
	// they will not be in the tracked peers.
	if totalJS < ps.clusterSize {
		s.Debugf("Adjusting JetStream cluster size from %d to %d", ps.clusterSize, totalJS)
		if err := n.AdjustClusterSize(totalJS); err != nil {
			s.Warnf("Error adjusting JetStream cluster size: %v", err)
		}
	}
}

// Represents our stable meta state that we can write out.
type writeableStreamAssignment struct {
	Client    *ClientInfo   `json:"client,omitempty"`
	Created   time.Time     `json:"created"`
	Config    *StreamConfig `json:"stream"`
	Group     *raftGroup    `json:"group"`
	Sync      string        `json:"sync"`
	Consumers []*consumerAssignment
}

func (js *jetStream) clusterStreamConfig(accName, streamName string) (StreamConfig, bool) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	if sa, ok := js.cluster.streams[accName][streamName]; ok {
		return *sa.Config, true
	}
	return StreamConfig{}, false
}

func (js *jetStream) metaSnapshot() []byte {
	var streams []writeableStreamAssignment

	js.mu.RLock()
	cc := js.cluster
	for _, asa := range cc.streams {
		for _, sa := range asa {
			wsa := writeableStreamAssignment{
				Client:  sa.Client,
				Created: sa.Created,
				Config:  sa.Config,
				Group:   sa.Group,
				Sync:    sa.Sync,
			}
			for _, ca := range sa.consumers {
				wsa.Consumers = append(wsa.Consumers, ca)
			}
			streams = append(streams, wsa)
		}
	}

	if len(streams) == 0 {
		js.mu.RUnlock()
		return nil
	}

	b, _ := json.Marshal(streams)
	js.mu.RUnlock()

	return s2.EncodeBetter(nil, b)
}

func (js *jetStream) applyMetaSnapshot(buf []byte, ru *recoveryUpdates, isRecovering bool) error {
	var wsas []writeableStreamAssignment
	if len(buf) > 0 {
		jse, err := s2.Decode(nil, buf)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(jse, &wsas); err != nil {
			return err
		}
	}

	// Build our new version here outside of js.
	streams := make(map[string]map[string]*streamAssignment)
	for _, wsa := range wsas {
		fixCfgMirrorWithDedupWindow(wsa.Config)
		as := streams[wsa.Client.serviceAccount()]
		if as == nil {
			as = make(map[string]*streamAssignment)
			streams[wsa.Client.serviceAccount()] = as
		}
		sa := &streamAssignment{Client: wsa.Client, Created: wsa.Created, Config: wsa.Config, Group: wsa.Group, Sync: wsa.Sync}
		if len(wsa.Consumers) > 0 {
			sa.consumers = make(map[string]*consumerAssignment)
			for _, ca := range wsa.Consumers {
				sa.consumers[ca.Name] = ca
			}
		}
		as[wsa.Config.Name] = sa
	}

	js.mu.Lock()
	cc := js.cluster

	var saAdd, saDel, saChk []*streamAssignment
	// Walk through the old list to generate the delete list.
	for account, asa := range cc.streams {
		nasa := streams[account]
		for sn, sa := range asa {
			if nsa := nasa[sn]; nsa == nil {
				saDel = append(saDel, sa)
			} else {
				saChk = append(saChk, nsa)
			}
		}
	}
	// Walk through the new list to generate the add list.
	for account, nasa := range streams {
		asa := cc.streams[account]
		for sn, sa := range nasa {
			if asa[sn] == nil {
				saAdd = append(saAdd, sa)
			}
		}
	}
	// Now walk the ones to check and process consumers.
	var caAdd, caDel []*consumerAssignment
	for _, sa := range saChk {
		// Make sure to add in all the new ones from sa.
		for _, ca := range sa.consumers {
			caAdd = append(caAdd, ca)
		}
		if osa := js.streamAssignment(sa.Client.serviceAccount(), sa.Config.Name); osa != nil {
			for _, ca := range osa.consumers {
				if sa.consumers[ca.Name] == nil {
					caDel = append(caDel, ca)
				} else {
					caAdd = append(caAdd, ca)
				}
			}
		}
	}
	js.mu.Unlock()

	// Do removals first.
	for _, sa := range saDel {
		js.setStreamAssignmentRecovering(sa)
		if isRecovering {
			key := sa.recoveryKey()
			ru.removeStreams[key] = sa
			delete(ru.updateStreams, key)
		} else {
			js.processStreamRemoval(sa)
		}
	}
	// Now do add for the streams. Also add in all consumers.
	for _, sa := range saAdd {
		js.setStreamAssignmentRecovering(sa)
		js.processStreamAssignment(sa)

		// We can simply process the consumers.
		for _, ca := range sa.consumers {
			js.setConsumerAssignmentRecovering(ca)
			js.processConsumerAssignment(ca)
		}
	}

	// Perform updates on those in saChk. These were existing so make
	// sure to process any changes.
	for _, sa := range saChk {
		js.setStreamAssignmentRecovering(sa)
		if isRecovering {
			key := sa.recoveryKey()
			ru.updateStreams[key] = sa
			delete(ru.removeStreams, key)
		} else {
			js.processUpdateStreamAssignment(sa)
		}
	}

	// Now do the deltas for existing stream's consumers.
	for _, ca := range caDel {
		js.setConsumerAssignmentRecovering(ca)
		if isRecovering {
			key := ca.recoveryKey()
			ru.removeConsumers[key] = ca
			delete(ru.updateConsumers, key)
		} else {
			js.processConsumerRemoval(ca)
		}
	}
	for _, ca := range caAdd {
		js.setConsumerAssignmentRecovering(ca)
		if isRecovering {
			key := ca.recoveryKey()
			delete(ru.removeConsumers, key)
			ru.updateConsumers[key] = ca
		} else {
			js.processConsumerAssignment(ca)
		}
	}

	return nil
}

// Called on recovery to make sure we do not process like original.
func (js *jetStream) setStreamAssignmentRecovering(sa *streamAssignment) {
	js.mu.Lock()
	defer js.mu.Unlock()
	sa.responded = true
	sa.recovering = true
	sa.Restore = nil
	if sa.Group != nil {
		sa.Group.Preferred = _EMPTY_
	}
}

// Called on recovery to make sure we do not process like original.
func (js *jetStream) setConsumerAssignmentRecovering(ca *consumerAssignment) {
	js.mu.Lock()
	defer js.mu.Unlock()
	ca.responded = true
	ca.recovering = true
	if ca.Group != nil {
		ca.Group.Preferred = _EMPTY_
	}
}

// Just copies over and changes out the group so it can be encoded.
// Lock should be held.
func (sa *streamAssignment) copyGroup() *streamAssignment {
	csa, cg := *sa, *sa.Group
	csa.Group = &cg
	csa.Group.Peers = copyStrings(sa.Group.Peers)
	return &csa
}

// Just copies over and changes out the group so it can be encoded.
// Lock should be held.
func (ca *consumerAssignment) copyGroup() *consumerAssignment {
	cca, cg := *ca, *ca.Group
	cca.Group = &cg
	cca.Group.Peers = copyStrings(ca.Group.Peers)
	return &cca
}

// Lock should be held.
func (sa *streamAssignment) missingPeers() bool {
	return len(sa.Group.Peers) < sa.Config.Replicas
}

// Called when we detect a new peer. Only the leader will process checking
// for any streams, and consequently any consumers.
func (js *jetStream) processAddPeer(peer string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	s, cc := js.srv, js.cluster
	if cc == nil || cc.meta == nil {
		return
	}
	isLeader := cc.isLeader()

	// Now check if we are meta-leader. We will check for any re-assignments.
	if !isLeader {
		return
	}

	sir, ok := s.nodeToInfo.Load(peer)
	if !ok || sir == nil {
		return
	}
	si := sir.(nodeInfo)

	for _, asa := range cc.streams {
		for _, sa := range asa {
			if sa.missingPeers() {
				// Make sure the right cluster etc.
				if si.cluster != sa.Client.Cluster {
					continue
				}
				// If we are here we can add in this peer.
				csa := sa.copyGroup()
				csa.Group.Peers = append(csa.Group.Peers, peer)
				// Send our proposal for this csa. Also use same group definition for all the consumers as well.
				cc.meta.Propose(encodeAddStreamAssignment(csa))
				for _, ca := range sa.consumers {
					// Ephemerals are R=1, so only auto-remap durables, or R>1.
					if ca.Config.Durable != _EMPTY_ || len(ca.Group.Peers) > 1 {
						cca := ca.copyGroup()
						cca.Group.Peers = csa.Group.Peers
						cc.meta.Propose(encodeAddConsumerAssignment(cca))
					}
				}
			}
		}
	}
}

func (js *jetStream) processRemovePeer(peer string) {
	js.mu.Lock()
	s, cc := js.srv, js.cluster
	if cc == nil || cc.meta == nil {
		js.mu.Unlock()
		return
	}
	isLeader := cc.isLeader()
	// All nodes will check if this is them.
	isUs := cc.meta.ID() == peer
	disabled := js.disabled
	js.mu.Unlock()

	// We may be already disabled.
	if disabled {
		return
	}

	if isUs {
		s.Errorf("JetStream being DISABLED, our server was removed from the cluster")
		adv := &JSServerRemovedAdvisory{
			TypedEvent: TypedEvent{
				Type: JSServerRemovedAdvisoryType,
				ID:   nuid.Next(),
				Time: time.Now().UTC(),
			},
			Server:   s.Name(),
			ServerID: s.ID(),
			Cluster:  s.cachedClusterName(),
			Domain:   s.getOpts().JetStreamDomain,
		}
		s.publishAdvisory(nil, JSAdvisoryServerRemoved, adv)

		go s.DisableJetStream()
	}

	// Now check if we are meta-leader. We will attempt re-assignment.
	if !isLeader {
		return
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	for _, asa := range cc.streams {
		for _, sa := range asa {
			if rg := sa.Group; rg.isMember(peer) {
				js.removePeerFromStreamLocked(sa, peer)
			}
		}
	}
}

// Assumes all checks have already been done.
func (js *jetStream) removePeerFromStream(sa *streamAssignment, peer string) bool {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.removePeerFromStreamLocked(sa, peer)
}

// Lock should be held.
func (js *jetStream) removePeerFromStreamLocked(sa *streamAssignment, peer string) bool {
	if rg := sa.Group; !rg.isMember(peer) {
		return false
	}

	s, cc, csa := js.srv, js.cluster, sa.copyGroup()
	if cc == nil || cc.meta == nil {
		return false
	}
	replaced := cc.remapStreamAssignment(csa, peer)
	if !replaced {
		s.Warnf("JetStream cluster could not replace peer for stream '%s > %s'", sa.Client.serviceAccount(), sa.Config.Name)
	}

	// Send our proposal for this csa. Also use same group definition for all the consumers as well.
	cc.meta.Propose(encodeAddStreamAssignment(csa))
	rg := csa.Group
	for _, ca := range sa.consumers {
		// Ephemerals are R=1, so only auto-remap durables, or R>1.
		if ca.Config.Durable != _EMPTY_ {
			cca := ca.copyGroup()
			cca.Group.Peers, cca.Group.Preferred = rg.Peers, _EMPTY_
			cc.meta.Propose(encodeAddConsumerAssignment(cca))
		} else if ca.Group.isMember(peer) {
			// These are ephemerals. Check to see if we deleted this peer.
			cc.meta.Propose(encodeDeleteConsumerAssignment(ca))
		}
	}
	return replaced
}

// Check if we have peer related entries.
func (js *jetStream) hasPeerEntries(entries []*Entry) bool {
	for _, e := range entries {
		if e.Type == EntryRemovePeer || e.Type == EntryAddPeer {
			return true
		}
	}
	return false
}

const ksep = ":"

func (sa *streamAssignment) recoveryKey() string {
	if sa == nil {
		return _EMPTY_
	}
	return sa.Client.serviceAccount() + ksep + sa.Config.Name
}

func (ca *consumerAssignment) recoveryKey() string {
	if ca == nil {
		return _EMPTY_
	}
	return ca.Client.serviceAccount() + ksep + ca.Stream + ksep + ca.Name
}

func (js *jetStream) applyMetaEntries(entries []*Entry, ru *recoveryUpdates) (bool, bool, bool, error) {
	var didSnap, didRemoveStream, didRemoveConsumer bool
	isRecovering := js.isMetaRecovering()

	for _, e := range entries {
		if e.Type == EntrySnapshot {
			js.applyMetaSnapshot(e.Data, ru, isRecovering)
			didSnap = true
		} else if e.Type == EntryRemovePeer {
			if !isRecovering {
				js.processRemovePeer(string(e.Data))
			}
		} else if e.Type == EntryAddPeer {
			if !isRecovering {
				js.processAddPeer(string(e.Data))
			}
		} else {
			buf := e.Data
			switch entryOp(buf[0]) {
			case assignStreamOp:
				sa, err := decodeStreamAssignment(buf[1:])
				if err != nil {
					js.srv.Errorf("JetStream cluster failed to decode stream assignment: %q", buf[1:])
					return didSnap, didRemoveStream, didRemoveConsumer, err
				}
				if isRecovering {
					js.setStreamAssignmentRecovering(sa)
					delete(ru.removeStreams, sa.recoveryKey())
				}
				if js.processStreamAssignment(sa) {
					didRemoveStream = true
				}
			case removeStreamOp:
				sa, err := decodeStreamAssignment(buf[1:])
				if err != nil {
					js.srv.Errorf("JetStream cluster failed to decode stream assignment: %q", buf[1:])
					return didSnap, didRemoveStream, didRemoveConsumer, err
				}
				if isRecovering {
					js.setStreamAssignmentRecovering(sa)
					key := sa.recoveryKey()
					ru.removeStreams[key] = sa
					delete(ru.updateStreams, key)
				} else {
					js.processStreamRemoval(sa)
					didRemoveStream = true
				}
			case assignConsumerOp:
				ca, err := decodeConsumerAssignment(buf[1:])
				if err != nil {
					js.srv.Errorf("JetStream cluster failed to decode consumer assignment: %q", buf[1:])
					return didSnap, didRemoveStream, didRemoveConsumer, err
				}
				if isRecovering {
					js.setConsumerAssignmentRecovering(ca)
					key := ca.recoveryKey()
					delete(ru.removeConsumers, key)
					ru.updateConsumers[key] = ca
				} else {
					js.processConsumerAssignment(ca)
				}
			case assignCompressedConsumerOp:
				ca, err := decodeConsumerAssignmentCompressed(buf[1:])
				if err != nil {
					js.srv.Errorf("JetStream cluster failed to decode compressed consumer assignment: %q", buf[1:])
					return didSnap, didRemoveStream, didRemoveConsumer, err
				}
				if isRecovering {
					js.setConsumerAssignmentRecovering(ca)
					key := ca.recoveryKey()
					delete(ru.removeConsumers, key)
					ru.updateConsumers[key] = ca
				} else {
					js.processConsumerAssignment(ca)
				}
			case removeConsumerOp:
				ca, err := decodeConsumerAssignment(buf[1:])
				if err != nil {
					js.srv.Errorf("JetStream cluster failed to decode consumer assignment: %q", buf[1:])
					return didSnap, didRemoveStream, didRemoveConsumer, err
				}
				if isRecovering {
					js.setConsumerAssignmentRecovering(ca)
					key := ca.recoveryKey()
					ru.removeConsumers[key] = ca
					delete(ru.updateConsumers, key)
				} else {
					js.processConsumerRemoval(ca)
					didRemoveConsumer = true
				}
			case updateStreamOp:
				sa, err := decodeStreamAssignment(buf[1:])
				if err != nil {
					js.srv.Errorf("JetStream cluster failed to decode stream assignment: %q", buf[1:])
					return didSnap, didRemoveStream, didRemoveConsumer, err
				}
				if isRecovering {
					js.setStreamAssignmentRecovering(sa)
					key := sa.recoveryKey()
					ru.updateStreams[key] = sa
					delete(ru.removeStreams, key)
				} else {
					js.processUpdateStreamAssignment(sa)
				}
			default:
				panic("JetStream Cluster Unknown meta entry op type")
			}
		}
	}
	return didSnap, didRemoveStream, didRemoveConsumer, nil
}

func (rg *raftGroup) isMember(id string) bool {
	if rg == nil {
		return false
	}
	for _, peer := range rg.Peers {
		if peer == id {
			return true
		}
	}
	return false
}

func (rg *raftGroup) setPreferred() {
	if rg == nil || len(rg.Peers) == 0 {
		return
	}
	if len(rg.Peers) == 1 {
		rg.Preferred = rg.Peers[0]
	} else {
		// For now just randomly select a peer for the preferred.
		pi := rand.Int31n(int32(len(rg.Peers)))
		rg.Preferred = rg.Peers[pi]
	}
}

// createRaftGroup is called to spin up this raft group if needed.
func (js *jetStream) createRaftGroup(accName string, rg *raftGroup, storage StorageType) error {
	js.mu.Lock()
	s, cc := js.srv, js.cluster
	if cc == nil || cc.meta == nil {
		js.mu.Unlock()
		return NewJSClusterNotActiveError()
	}

	// If this is a single peer raft group or we are not a member return.
	if len(rg.Peers) <= 1 || !rg.isMember(cc.meta.ID()) {
		js.mu.Unlock()
		// Nothing to do here.
		return nil
	}

	// Check if we already have this assigned.
	if node := s.lookupRaftNode(rg.Name); node != nil {
		s.Debugf("JetStream cluster already has raft group %q assigned", rg.Name)
		rg.node = node
		js.mu.Unlock()
		return nil
	}
	js.mu.Unlock()

	s.Debugf("JetStream cluster creating raft group:%+v", rg)

	sysAcc := s.SystemAccount()
	if sysAcc == nil {
		s.Debugf("JetStream cluster detected shutdown processing raft group: %+v", rg)
		return errors.New("shutting down")
	}

	// Check here to see if we have a max HA Assets limit set.
	if maxHaAssets := s.getOpts().JetStreamLimits.MaxHAAssets; maxHaAssets > 0 {
		if s.numRaftNodes() > maxHaAssets {
			s.Warnf("Maximum HA Assets limit reached: %d", maxHaAssets)
			// Since the meta leader assigned this, send a statsz update to them to get them up to date.
			go s.sendStatszUpdate()
			return errors.New("system limit reached")
		}
	}

	storeDir := filepath.Join(js.config.StoreDir, sysAcc.Name, defaultStoreDirName, rg.Name)
	var store StreamStore
	if storage == FileStorage {
		fs, err := newFileStoreWithCreated(
			FileStoreConfig{StoreDir: storeDir, BlockSize: defaultMediumBlockSize, AsyncFlush: false, SyncInterval: 5 * time.Minute},
			StreamConfig{Name: rg.Name, Storage: FileStorage},
			time.Now().UTC(),
			s.jsKeyGen(rg.Name),
		)
		if err != nil {
			s.Errorf("Error creating filestore WAL: %v", err)
			return err
		}
		// Register our server.
		fs.registerServer(s)
		store = fs
	} else {
		ms, err := newMemStore(&StreamConfig{Name: rg.Name, Storage: MemoryStorage})
		if err != nil {
			s.Errorf("Error creating memstore WAL: %v", err)
			return err
		}
		store = ms
	}

	cfg := &RaftConfig{Name: rg.Name, Store: storeDir, Log: store, Track: true}

	if _, err := readPeerState(storeDir); err != nil {
		s.bootstrapRaftNode(cfg, rg.Peers, true)
	}

	n, err := s.startRaftNode(accName, cfg)
	if err != nil || n == nil {
		s.Debugf("Error creating raft group: %v", err)
		return err
	}
	// Need locking here for the assignment to avoid data-race reports
	js.mu.Lock()
	rg.node = n
	// See if we are preferred and should start campaign immediately.
	if n.ID() == rg.Preferred && n.Term() == 0 {
		n.Campaign()
	}
	js.mu.Unlock()
	return nil
}

func (mset *stream) raftGroup() *raftGroup {
	if mset == nil {
		return nil
	}
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	if mset.sa == nil {
		return nil
	}
	return mset.sa.Group
}

func (mset *stream) raftNode() RaftNode {
	if mset == nil {
		return nil
	}
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.node
}

func (mset *stream) removeNode() {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	if n := mset.node; n != nil {
		n.Delete()
		mset.node = nil
	}
}

// Helper function to generate peer info.
// lists and sets for old and new.
func genPeerInfo(peers []string, split int) (newPeers, oldPeers []string, newPeerSet, oldPeerSet map[string]bool) {
	newPeers = peers[split:]
	oldPeers = peers[:split]
	newPeerSet = make(map[string]bool, len(newPeers))
	oldPeerSet = make(map[string]bool, len(oldPeers))
	for i, peer := range peers {
		if i < split {
			oldPeerSet[peer] = true
		} else {
			newPeerSet[peer] = true
		}
	}
	return
}

// Monitor our stream node for this stream.
func (js *jetStream) monitorStream(mset *stream, sa *streamAssignment, sendSnapshot bool) {
	s, cc := js.server(), js.cluster
	defer s.grWG.Done()
	if mset != nil {
		defer mset.monitorWg.Done()
	}
	js.mu.RLock()
	n := sa.Group.node
	meta := cc.meta
	js.mu.RUnlock()

	if n == nil || meta == nil {
		s.Warnf("No RAFT group for '%s > %s'", sa.Client.serviceAccount(), sa.Config.Name)
		return
	}

	// Make sure only one is running.
	if mset != nil {
		if mset.checkInMonitor() {
			return
		}
		defer mset.clearMonitorRunning()
	}

	// Make sure to stop the raft group on exit to prevent accidental memory bloat.
	// This should be below the checkInMonitor call though to avoid stopping it out
	// from underneath the one that is running since it will be the same raft node.
	defer n.Stop()

	qch, lch, aq, uch, ourPeerId := n.QuitC(), n.LeadChangeC(), n.ApplyQ(), mset.updateC(), meta.ID()

	s.Debugf("Starting stream monitor for '%s > %s' [%s]", sa.Client.serviceAccount(), sa.Config.Name, n.Group())
	defer s.Debugf("Exiting stream monitor for '%s > %s' [%s]", sa.Client.serviceAccount(), sa.Config.Name, n.Group())

	// Make sure we do not leave the apply channel to fill up and block the raft layer.
	defer func() {
		if n.State() == Closed {
			return
		}
		if n.Leader() {
			n.StepDown()
		}
		// Drain the commit queue...
		aq.drain()
	}()

	const (
		compactInterval = 2 * time.Minute
		compactSizeMin  = 8 * 1024 * 1024
		compactNumMin   = 65536
		minSnapDelta    = 10 * time.Second
	)

	// Spread these out for large numbers on server restart.
	rci := time.Duration(rand.Int63n(int64(time.Minute)))
	t := time.NewTicker(compactInterval + rci)
	defer t.Stop()

	js.mu.RLock()
	isLeader := cc.isStreamLeader(sa.Client.serviceAccount(), sa.Config.Name)
	isRestore := sa.Restore != nil
	js.mu.RUnlock()

	acc, err := s.LookupAccount(sa.Client.serviceAccount())
	if err != nil {
		s.Warnf("Could not retrieve account for stream '%s > %s'", sa.Client.serviceAccount(), sa.Config.Name)
		return
	}
	accName := acc.GetName()

	// Hash of the last snapshot (fixed size in memory).
	var lastSnap []byte
	var lastSnapTime time.Time

	// Highwayhash key for generating hashes.
	key := make([]byte, 32)
	rand.Read(key)

	// Should only to be called from leader.
	doSnapshot := func() {
		if mset == nil || isRestore || time.Since(lastSnapTime) < minSnapDelta {
			return
		}

		snap := mset.stateSnapshot()
		ne, nb := n.Size()
		hash := highwayhash.Sum(snap, key)
		// If the state hasn't changed but the log has gone way over
		// the compaction size then we will want to compact anyway.
		// This shouldn't happen for streams like it can for pull
		// consumers on idle streams but better to be safe than sorry!
		if !bytes.Equal(hash[:], lastSnap) || ne >= compactNumMin || nb >= compactSizeMin {
			if err := n.InstallSnapshot(snap); err == nil {
				lastSnap, lastSnapTime = hash[:], time.Now()
			} else if err != errNoSnapAvailable && err != errNodeClosed {
				s.Warnf("Failed to install snapshot for '%s > %s' [%s]: %v", mset.acc.Name, mset.name(), n.Group(), err)
			}
		}
	}

	// We will establish a restoreDoneCh no matter what. Will never be triggered unless
	// we replace with the restore chan.
	restoreDoneCh := make(<-chan error)
	isRecovering := true

	// For migration tracking.
	var mmt *time.Ticker
	var mmtc <-chan time.Time

	startMigrationMonitoring := func() {
		if mmt == nil {
			mmt = time.NewTicker(500 * time.Millisecond)
			mmtc = mmt.C
		}
	}

	stopMigrationMonitoring := func() {
		if mmt != nil {
			mmt.Stop()
			mmt, mmtc = nil, nil
		}
	}
	defer stopMigrationMonitoring()

	// This is to optionally track when we are ready as a non-leader for direct access participation.
	// Either direct or if we are a direct mirror, or both.
	var dat *time.Ticker
	var datc <-chan time.Time

	startDirectAccessMonitoring := func() {
		if dat == nil {
			dat = time.NewTicker(1 * time.Second)
			datc = dat.C
		}
	}

	stopDirectMonitoring := func() {
		if dat != nil {
			dat.Stop()
			dat, datc = nil, nil
		}
	}
	defer stopDirectMonitoring()

	// Check if we are interest based and if so and we have an active stream wait until we
	// have the consumers attached. This can become important when a server has lots of assets
	// since we process streams first then consumers as an asset class.
	if mset != nil && mset.isInterestRetention() {
		js.mu.RLock()
		numExpectedConsumers := len(sa.consumers)
		js.mu.RUnlock()
		if mset.numConsumers() < numExpectedConsumers {
			s.Debugf("Waiting for consumers for interest based stream '%s > %s'", accName, mset.name())
			// Wait up to 10s
			const maxWaitTime = 10 * time.Second
			const sleepTime = 250 * time.Millisecond
			timeout := time.Now().Add(maxWaitTime)
			for time.Now().Before(timeout) {
				if mset.numConsumers() >= numExpectedConsumers {
					break
				}
				time.Sleep(sleepTime)
			}
			if actual := mset.numConsumers(); actual < numExpectedConsumers {
				s.Warnf("All consumers not online for '%s > %s': expected %d but only have %d", accName, mset.name(), numExpectedConsumers, actual)
			}
		}
	}

	// This is triggered during a scale up from R1 to clustered mode. We need the new followers to catchup,
	// similar to how we trigger the catchup mechanism post a backup/restore.
	// We can arrive here NOT being the leader, so we send the snapshot only if we are, and in this case
	// reset the notion that we need to send the snapshot. If we are not, then the first time the server
	// will switch to leader (in the loop below), we will send the snapshot.
	if sendSnapshot && isLeader && mset != nil && n != nil {
		n.SendSnapshot(mset.stateSnapshot())
		sendSnapshot = false
	}

	for {
		select {
		case <-s.quitCh:
			return
		case <-qch:
			return
		case <-aq.ch:
			var ne, nb uint64
			ces := aq.pop()
			for _, ce := range ces {
				// No special processing needed for when we are caught up on restart.
				if ce == nil {
					isRecovering = false
					// Check on startup if we should snapshot/compact.
					if _, b := n.Size(); b > compactSizeMin || n.NeedSnapshot() {
						doSnapshot()
					}
					continue
				}
				// Apply our entries.
				if err := js.applyStreamEntries(mset, ce, isRecovering); err == nil {
					// Update our applied.
					ne, nb = n.Applied(ce.Index)
				} else {
					s.Warnf("Error applying entries to '%s > %s': %v", accName, sa.Config.Name, err)
					if isClusterResetErr(err) {
						if mset.isMirror() && mset.IsLeader() {
							mset.retryMirrorConsumer()
							continue
						}
						// We will attempt to reset our cluster state.
						if mset.resetClusteredState(err) {
							aq.recycle(&ces)
							return
						}
					} else if isOutOfSpaceErr(err) {
						// If applicable this will tear all of this down, but don't assume so and return.
						s.handleOutOfSpace(mset)
					}
				}
			}
			aq.recycle(&ces)

			// Check about snapshotting
			// If we have at least min entries to compact, go ahead and try to snapshot/compact.
			if ne >= compactNumMin || nb > compactSizeMin {
				doSnapshot()
			}

		case isLeader = <-lch:
			if isLeader {
				if sendSnapshot && mset != nil && n != nil {
					n.SendSnapshot(mset.stateSnapshot())
					sendSnapshot = false
				}
				if isRestore {
					acc, _ := s.LookupAccount(sa.Client.serviceAccount())
					restoreDoneCh = s.processStreamRestore(sa.Client, acc, sa.Config, _EMPTY_, sa.Reply, _EMPTY_)
					continue
				} else if n.NeedSnapshot() {
					doSnapshot()
				}
				// Always cancel if this was running.
				stopDirectMonitoring()

			} else if n.GroupLeader() != noLeader {
				js.setStreamAssignmentRecovering(sa)
			}

			// Process our leader change.
			js.processStreamLeaderChange(mset, isLeader)

			// We may receive a leader change after the stream assignment which would cancel us
			// monitoring for this closely. So re-assess our state here as well.
			// Or the old leader is no longer part of the set and transferred leadership
			// for this leader to resume with removal
			migrating := mset.isMigrating()

			// Check for migrations here. We set the state on the stream assignment update below.
			if isLeader && migrating {
				startMigrationMonitoring()
			}

			// Here we are checking if we are not the leader but we have been asked to allow
			// direct access. We now allow non-leaders to participate in the queue group.
			if !isLeader && mset != nil {
				mset.mu.Lock()
				// Check direct gets first.
				if mset.cfg.AllowDirect {
					if mset.directSub == nil && mset.isCurrent() {
						mset.subscribeToDirect()
					} else {
						startDirectAccessMonitoring()
					}
				}
				// Now check for mirror directs as well.
				if mset.cfg.MirrorDirect {
					if mset.mirror != nil && mset.mirror.dsub == nil && mset.isCurrent() {
						mset.subscribeToMirrorDirect()
					} else {
						startDirectAccessMonitoring()
					}
				}
				mset.mu.Unlock()
			}

		case <-datc:
			mset.mu.Lock()
			ad, md, current := mset.cfg.AllowDirect, mset.cfg.MirrorDirect, mset.isCurrent()
			if !current {
				const syncThreshold = 90.0
				// We are not current, but current means exactly caught up. Under heavy publish
				// loads we may never reach this, so check if we are within 90% caught up.
				_, c, a := mset.node.Progress()
				if p := float64(a) / float64(c) * 100.0; p < syncThreshold {
					mset.mu.Unlock()
					continue
				} else {
					s.Debugf("Stream '%s > %s' enabling direct gets at %.0f%% synchronized",
						sa.Client.serviceAccount(), sa.Config.Name, p)
				}
			}
			// We are current, cancel monitoring and create the direct subs as needed.
			if ad {
				mset.subscribeToDirect()
			}
			if md {
				mset.subscribeToMirrorDirect()
			}
			mset.mu.Unlock()
			// Stop monitoring.
			stopDirectMonitoring()

		case <-t.C:
			doSnapshot()

		case <-uch:
			// keep stream assignment current
			sa = mset.streamAssignment()

			// keep peer list up to date with config
			js.checkPeers(mset.raftGroup())
			// We get this when we have a new stream assignment caused by an update.
			// We want to know if we are migrating.
			if migrating := mset.isMigrating(); migrating {
				if isLeader && mmtc == nil {
					startMigrationMonitoring()
				}
			} else {
				stopMigrationMonitoring()
			}
		case <-mmtc:
			if !isLeader {
				// We are no longer leader, so not our job.
				stopMigrationMonitoring()
				continue
			}

			// Check to see where we are..
			rg := mset.raftGroup()
			ci := js.clusterInfo(rg)
			mset.checkClusterInfo(ci)

			// Track the new peers and check the ones that are current.
			mset.mu.RLock()
			replicas := mset.cfg.Replicas
			mset.mu.RUnlock()
			if len(rg.Peers) <= replicas {
				// Migration no longer happening, so not our job anymore
				stopMigrationMonitoring()
				continue
			}

			newPeers, oldPeers, newPeerSet, oldPeerSet := genPeerInfo(rg.Peers, len(rg.Peers)-replicas)

			// If we are part of the new peerset and we have been passed the baton.
			// We will handle scale down.
			if newPeerSet[ourPeerId] {
				// First need to check on any consumers and make sure they have moved properly before scaling down ourselves.
				js.mu.RLock()
				var needToWait bool
				for name, c := range sa.consumers {
					for _, peer := range c.Group.Peers {
						// If we have peers still in the old set block.
						if oldPeerSet[peer] {
							s.Debugf("Scale down of '%s > %s' blocked by consumer '%s'", accName, sa.Config.Name, name)
							needToWait = true
							break
						}
					}
					if needToWait {
						break
					}
				}
				js.mu.RUnlock()
				if needToWait {
					continue
				}

				// We are good to go, can scale down here.
				for _, p := range oldPeers {
					n.ProposeRemovePeer(p)
				}

				csa := sa.copyGroup()
				csa.Group.Peers = newPeers
				csa.Group.Preferred = ourPeerId
				csa.Group.Cluster = s.cachedClusterName()
				cc.meta.ForwardProposal(encodeUpdateStreamAssignment(csa))
				s.Noticef("Scaling down '%s > %s' to %+v", accName, sa.Config.Name, s.peerSetToNames(newPeers))

			} else {
				// We are the old leader here, from the original peer set.
				// We are simply waiting on the new peerset to be caught up so we can transfer leadership.
				var newLeaderPeer, newLeader string
				neededCurrent, current := replicas/2+1, 0
				for _, r := range ci.Replicas {
					if r.Current && newPeerSet[r.Peer] {
						current++
						if newLeader == _EMPTY_ {
							newLeaderPeer, newLeader = r.Peer, r.Name
						}
					}
				}
				// Check if we have a quorom.
				if current >= neededCurrent {
					s.Noticef("Transfer of stream leader for '%s > %s' to '%s'", accName, sa.Config.Name, newLeader)
					n.StepDown(newLeaderPeer)
				}
			}

		case err := <-restoreDoneCh:
			// We have completed a restore from snapshot on this server. The stream assignment has
			// already been assigned but the replicas will need to catch up out of band. Consumers
			// will need to be assigned by forwarding the proposal and stamping the initial state.
			s.Debugf("Stream restore for '%s > %s' completed", sa.Client.serviceAccount(), sa.Config.Name)
			if err != nil {
				s.Debugf("Stream restore failed: %v", err)
			}
			isRestore = false
			sa.Restore = nil
			// If we were successful lookup up our stream now.
			if err == nil {
				if mset, err = acc.lookupStream(sa.Config.Name); mset != nil {
					mset.monitorWg.Add(1)
					defer mset.monitorWg.Done()
					mset.setStreamAssignment(sa)
					// Make sure to update our updateC which would have been nil.
					uch = mset.updateC()
				}
			}
			if err != nil {
				if mset != nil {
					mset.delete()
				}
				js.mu.Lock()
				sa.err = err
				if n != nil {
					n.Delete()
				}
				result := &streamAssignmentResult{
					Account: sa.Client.serviceAccount(),
					Stream:  sa.Config.Name,
					Restore: &JSApiStreamRestoreResponse{ApiResponse: ApiResponse{Type: JSApiStreamRestoreResponseType}},
				}
				result.Restore.Error = NewJSStreamAssignmentError(err, Unless(err))
				js.mu.Unlock()
				// Send response to the metadata leader. They will forward to the user as needed.
				s.sendInternalMsgLocked(streamAssignmentSubj, _EMPTY_, nil, result)
				return
			}

			if !isLeader {
				panic("Finished restore but not leader")
			}
			// Trigger the stream followers to catchup.
			if n = mset.raftNode(); n != nil {
				n.SendSnapshot(mset.stateSnapshot())
			}
			js.processStreamLeaderChange(mset, isLeader)

			// Check to see if we have restored consumers here.
			// These are not currently assigned so we will need to do so here.
			if consumers := mset.getPublicConsumers(); len(consumers) > 0 {
				for _, o := range consumers {
					name, cfg := o.String(), o.config()
					rg := cc.createGroupForConsumer(&cfg, sa)
					// Pick a preferred leader.
					rg.setPreferred()

					// Place our initial state here as well for assignment distribution.
					state, _ := o.store.State()
					ca := &consumerAssignment{
						Group:   rg,
						Stream:  sa.Config.Name,
						Name:    name,
						Config:  &cfg,
						Client:  sa.Client,
						Created: o.createdTime(),
						State:   state,
					}

					// We make these compressed in case state is complex.
					addEntry := encodeAddConsumerAssignmentCompressed(ca)
					cc.meta.ForwardProposal(addEntry)

					// Check to make sure we see the assignment.
					go func() {
						ticker := time.NewTicker(time.Second)
						defer ticker.Stop()
						for range ticker.C {
							js.mu.RLock()
							ca, meta := js.consumerAssignment(ca.Client.serviceAccount(), sa.Config.Name, name), cc.meta
							js.mu.RUnlock()
							if ca == nil {
								s.Warnf("Consumer assignment has not been assigned, retrying")
								if meta != nil {
									meta.ForwardProposal(addEntry)
								} else {
									return
								}
							} else {
								return
							}
						}
					}()
				}
			}
		}
	}
}

// Determine if we are migrating
func (mset *stream) isMigrating() bool {
	if mset == nil {
		return false
	}

	mset.mu.RLock()
	js, sa := mset.js, mset.sa
	mset.mu.RUnlock()

	js.mu.RLock()
	defer js.mu.RUnlock()

	// During migration we will always be R>1, even when we start R1.
	// So if we do not have a group or node we no we are not migrating.
	if sa == nil || sa.Group == nil || sa.Group.node == nil {
		return false
	}
	// The sign of migration is if our group peer count != configured replica count.
	if sa.Config.Replicas == len(sa.Group.Peers) {
		return false
	}
	return true
}

// resetClusteredState is called when a clustered stream had a sequence mismatch and needs to be reset.
func (mset *stream) resetClusteredState(err error) bool {
	mset.mu.RLock()
	s, js, jsa, sa, acc, node := mset.srv, mset.js, mset.jsa, mset.sa, mset.acc, mset.node
	stype, isLeader, tierName := mset.cfg.Storage, mset.isLeader(), mset.tier
	mset.mu.RUnlock()

	// Stepdown regardless if we are the leader here.
	if isLeader && node != nil {
		node.StepDown()
	}

	// Server
	if js.limitsExceeded(stype) {
		s.Debugf("Will not reset stream, server resources exceeded")
		return false
	}

	// Account
	if exceeded, _ := jsa.limitsExceeded(stype, tierName); exceeded {
		s.Warnf("stream '%s > %s' errored, account resources exceeded", acc, mset.name())
		return false
	}

	// We delete our raft state. Will recreate.
	if node != nil {
		node.Delete()
	}

	// Preserve our current state and messages unless we have a first sequence mismatch.
	shouldDelete := err == errFirstSequenceMismatch

	mset.monitorWg.Wait()
	mset.resetAndWaitOnConsumers()
	// Stop our stream.
	mset.stop(shouldDelete, false)

	if sa != nil {
		js.mu.Lock()
		s.Warnf("Resetting stream cluster state for '%s > %s'", sa.Client.serviceAccount(), sa.Config.Name)
		// Now wipe groups from assignments.
		sa.Group.node = nil
		var consumers []*consumerAssignment
		if cc := js.cluster; cc != nil && cc.meta != nil {
			ourID := cc.meta.ID()
			for _, ca := range sa.consumers {
				if rg := ca.Group; rg != nil && rg.isMember(ourID) {
					rg.node = nil // Erase group raft/node state.
					consumers = append(consumers, ca)
				}
			}
		}
		js.mu.Unlock()

		// restart in a separate Go routine.
		// This will reset the stream and consumers.
		go func() {
			// Reset stream.
			js.processClusterCreateStream(acc, sa)
			// Reset consumers.
			for _, ca := range consumers {
				js.processClusterCreateConsumer(ca, nil, false)
			}
		}()
	}

	return true
}

func isControlHdr(hdr []byte) bool {
	return bytes.HasPrefix(hdr, []byte("NATS/1.0 100 "))
}

// Apply our stream entries.
func (js *jetStream) applyStreamEntries(mset *stream, ce *CommittedEntry, isRecovering bool) error {
	for _, e := range ce.Entries {
		if e.Type == EntryNormal {
			buf, op := e.Data, entryOp(e.Data[0])
			switch op {
			case streamMsgOp, compressedStreamMsgOp:
				if mset == nil {
					continue
				}
				s := js.srv

				mbuf := buf[1:]
				if op == compressedStreamMsgOp {
					var err error
					mbuf, err = s2.Decode(nil, mbuf)
					if err != nil {
						panic(err.Error())
					}
				}

				subject, reply, hdr, msg, lseq, ts, err := decodeStreamMsg(mbuf)
				if err != nil {
					if node := mset.raftNode(); node != nil {
						s.Errorf("JetStream cluster could not decode stream msg for '%s > %s' [%s]",
							mset.account(), mset.name(), node.Group())
					}
					panic(err.Error())
				}

				// Check for flowcontrol here.
				if len(msg) == 0 && len(hdr) > 0 && reply != _EMPTY_ && isControlHdr(hdr) {
					if !isRecovering {
						mset.sendFlowControlReply(reply)
					}
					continue
				}

				// Grab last sequence and CLFS.
				last, clfs := mset.lastSeqAndCLFS()

				// We can skip if we know this is less than what we already have.
				if lseq-clfs < last {
					s.Debugf("Apply stream entries for '%s > %s' skipping message with sequence %d with last of %d",
						mset.account(), mset.name(), lseq+1-clfs, last)
					// Check for any preAcks in case we are interest based.
					mset.mu.Lock()
					seq := lseq + 1 - mset.clfs
					mset.clearAllPreAcks(seq)
					mset.mu.Unlock()
					continue
				}

				// Skip by hand here since first msg special case.
				// Reason is sequence is unsigned and for lseq being 0
				// the lseq under stream would have to be -1.
				if lseq == 0 && last != 0 {
					continue
				}

				// Messages to be skipped have no subject or timestamp or msg or hdr.
				if subject == _EMPTY_ && ts == 0 && len(msg) == 0 && len(hdr) == 0 {
					// Skip and update our lseq.
					last := mset.store.SkipMsg()
					mset.setLastSeq(last)
					mset.clearAllPreAcks(last)
					continue
				}

				// Process the actual message here.
				if err := mset.processJetStreamMsg(subject, reply, hdr, msg, lseq, ts); err != nil {
					// Only return in place if we are going to reset stream or we are out of space.
					if isClusterResetErr(err) || isOutOfSpaceErr(err) {
						return err
					}
					s.Debugf("Apply stream entries for '%s > %s' got error processing message: %v",
						mset.account(), mset.name(), err)
				}
			case deleteMsgOp:
				md, err := decodeMsgDelete(buf[1:])
				if err != nil {
					if node := mset.raftNode(); node != nil {
						s := js.srv
						s.Errorf("JetStream cluster could not decode delete msg for '%s > %s' [%s]",
							mset.account(), mset.name(), node.Group())
					}
					panic(err.Error())
				}
				s, cc := js.server(), js.cluster

				var removed bool
				if md.NoErase {
					removed, err = mset.removeMsg(md.Seq)
				} else {
					removed, err = mset.eraseMsg(md.Seq)
				}

				// Cluster reset error.
				if err == ErrStoreEOF {
					return err
				}

				if err != nil && !isRecovering {
					s.Debugf("JetStream cluster failed to delete stream msg %d from '%s > %s': %v",
						md.Seq, md.Client.serviceAccount(), md.Stream, err)
				}

				js.mu.RLock()
				isLeader := cc.isStreamLeader(md.Client.serviceAccount(), md.Stream)
				js.mu.RUnlock()

				if isLeader && !isRecovering {
					var resp = JSApiMsgDeleteResponse{ApiResponse: ApiResponse{Type: JSApiMsgDeleteResponseType}}
					if err != nil {
						resp.Error = NewJSStreamMsgDeleteFailedError(err, Unless(err))
						s.sendAPIErrResponse(md.Client, mset.account(), md.Subject, md.Reply, _EMPTY_, s.jsonResponse(resp))
					} else if !removed {
						resp.Error = NewJSSequenceNotFoundError(md.Seq)
						s.sendAPIErrResponse(md.Client, mset.account(), md.Subject, md.Reply, _EMPTY_, s.jsonResponse(resp))
					} else {
						resp.Success = true
						s.sendAPIResponse(md.Client, mset.account(), md.Subject, md.Reply, _EMPTY_, s.jsonResponse(resp))
					}
				}
			case purgeStreamOp:
				sp, err := decodeStreamPurge(buf[1:])
				if err != nil {
					if node := mset.raftNode(); node != nil {
						s := js.srv
						s.Errorf("JetStream cluster could not decode purge msg for '%s > %s' [%s]",
							mset.account(), mset.name(), node.Group())
					}
					panic(err.Error())
				}
				// Ignore if we are recovering and we have already processed.
				if isRecovering {
					if mset.state().FirstSeq <= sp.LastSeq {
						// Make sure all messages from the purge are gone.
						mset.store.Compact(sp.LastSeq + 1)
					}
					continue
				}

				s := js.server()
				purged, err := mset.purge(sp.Request)
				if err != nil {
					s.Warnf("JetStream cluster failed to purge stream %q for account %q: %v", sp.Stream, sp.Client.serviceAccount(), err)
				}

				js.mu.RLock()
				isLeader := js.cluster.isStreamLeader(sp.Client.serviceAccount(), sp.Stream)
				js.mu.RUnlock()

				if isLeader && !isRecovering {
					var resp = JSApiStreamPurgeResponse{ApiResponse: ApiResponse{Type: JSApiStreamPurgeResponseType}}
					if err != nil {
						resp.Error = NewJSStreamGeneralError(err, Unless(err))
						s.sendAPIErrResponse(sp.Client, mset.account(), sp.Subject, sp.Reply, _EMPTY_, s.jsonResponse(resp))
					} else {
						resp.Purged = purged
						resp.Success = true
						s.sendAPIResponse(sp.Client, mset.account(), sp.Subject, sp.Reply, _EMPTY_, s.jsonResponse(resp))
					}
				}
			default:
				panic("JetStream Cluster Unknown group entry op type!")
			}
		} else if e.Type == EntrySnapshot {
			if !isRecovering && mset != nil {
				var snap streamSnapshot
				if err := json.Unmarshal(e.Data, &snap); err != nil {
					return err
				}
				if !mset.IsLeader() {
					if err := mset.processSnapshot(&snap); err != nil {
						return err
					}
				}
			} else if isRecovering && mset != nil {
				// On recovery, reset CLFS/FAILED.
				var snap streamSnapshot
				if err := json.Unmarshal(e.Data, &snap); err != nil {
					return err
				}

				mset.mu.Lock()
				mset.clfs = snap.Failed
				mset.mu.Unlock()
			}
		} else if e.Type == EntryRemovePeer {
			js.mu.RLock()
			var ourID string
			if js.cluster != nil && js.cluster.meta != nil {
				ourID = js.cluster.meta.ID()
			}
			js.mu.RUnlock()
			// We only need to do processing if this is us.
			if peer := string(e.Data); peer == ourID && mset != nil {
				// Double check here with the registered stream assignment.
				shouldRemove := true
				if sa := mset.streamAssignment(); sa != nil && sa.Group != nil {
					js.mu.RLock()
					shouldRemove = !sa.Group.isMember(ourID)
					js.mu.RUnlock()
				}
				if shouldRemove {
					mset.stop(true, false)
				}
			}
			return nil
		}
	}
	return nil
}

// Returns the PeerInfo for all replicas of a raft node. This is different than node.Peers()
// and is used for external facing advisories.
func (s *Server) replicas(node RaftNode) []*PeerInfo {
	now := time.Now()
	var replicas []*PeerInfo
	for _, rp := range node.Peers() {
		if sir, ok := s.nodeToInfo.Load(rp.ID); ok && sir != nil {
			si := sir.(nodeInfo)
			pi := &PeerInfo{Peer: rp.ID, Name: si.name, Current: rp.Current, Active: now.Sub(rp.Last), Offline: si.offline, Lag: rp.Lag}
			replicas = append(replicas, pi)
		}
	}
	return replicas
}

// Will check our node peers and see if we should remove a peer.
func (js *jetStream) checkPeers(rg *raftGroup) {
	js.mu.Lock()
	defer js.mu.Unlock()

	// FIXME(dlc) - Single replicas?
	if rg == nil || rg.node == nil {
		return
	}
	for _, peer := range rg.node.Peers() {
		if !rg.isMember(peer.ID) {
			rg.node.ProposeRemovePeer(peer.ID)
		}
	}
}

// Process a leader change for the clustered stream.
func (js *jetStream) processStreamLeaderChange(mset *stream, isLeader bool) {
	if mset == nil {
		return
	}
	sa := mset.streamAssignment()
	if sa == nil {
		return
	}
	js.mu.Lock()
	s, account, err := js.srv, sa.Client.serviceAccount(), sa.err
	client, subject, reply := sa.Client, sa.Subject, sa.Reply
	hasResponded := sa.responded
	sa.responded = true
	peers := copyStrings(sa.Group.Peers)
	js.mu.Unlock()

	streamName := mset.name()

	if isLeader {
		s.Noticef("JetStream cluster new stream leader for '%s > %s'", account, streamName)
		s.sendStreamLeaderElectAdvisory(mset)
		// Check for peer removal and process here if needed.
		js.checkPeers(sa.Group)
		mset.checkAllowMsgCompress(peers)
	} else {
		// We are stepping down.
		// Make sure if we are doing so because we have lost quorum that we send the appropriate advisories.
		if node := mset.raftNode(); node != nil && !node.Quorum() && time.Since(node.Created()) > 5*time.Second {
			s.sendStreamLostQuorumAdvisory(mset)
		}
	}

	// Tell stream to switch leader status.
	mset.setLeader(isLeader)

	if !isLeader || hasResponded {
		return
	}

	acc, _ := s.LookupAccount(account)
	if acc == nil {
		return
	}

	// Send our response.
	var resp = JSApiStreamCreateResponse{ApiResponse: ApiResponse{Type: JSApiStreamCreateResponseType}}
	if err != nil {
		resp.Error = NewJSStreamCreateError(err, Unless(err))
		s.sendAPIErrResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
	} else {
		resp.StreamInfo = &StreamInfo{
			Created: mset.createdTime(),
			State:   mset.state(),
			Config:  mset.config(),
			Cluster: js.clusterInfo(mset.raftGroup()),
			Sources: mset.sourcesInfo(),
			Mirror:  mset.mirrorInfo(),
		}
		resp.DidCreate = true
		s.sendAPIResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
		if node := mset.raftNode(); node != nil {
			mset.sendCreateAdvisory()
		}
	}
}

// Fixed value ok for now.
const lostQuorumAdvInterval = 10 * time.Second

// Determines if we should send lost quorum advisory. We throttle these after first one.
func (mset *stream) shouldSendLostQuorum() bool {
	mset.mu.Lock()
	defer mset.mu.Unlock()
	if time.Since(mset.lqsent) >= lostQuorumAdvInterval {
		mset.lqsent = time.Now()
		return true
	}
	return false
}

func (s *Server) sendStreamLostQuorumAdvisory(mset *stream) {
	if mset == nil {
		return
	}
	node, stream, acc := mset.raftNode(), mset.name(), mset.account()
	if node == nil {
		return
	}
	if !mset.shouldSendLostQuorum() {
		return
	}

	s.Warnf("JetStream cluster stream '%s > %s' has NO quorum, stalled", acc.GetName(), stream)

	subj := JSAdvisoryStreamQuorumLostPre + "." + stream
	adv := &JSStreamQuorumLostAdvisory{
		TypedEvent: TypedEvent{
			Type: JSStreamQuorumLostAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   stream,
		Replicas: s.replicas(node),
		Domain:   s.getOpts().JetStreamDomain,
	}

	// Send to the user's account if not the system account.
	if acc != s.SystemAccount() {
		s.publishAdvisory(acc, subj, adv)
	}
	// Now do system level one. Place account info in adv, and nil account means system.
	adv.Account = acc.GetName()
	s.publishAdvisory(nil, subj, adv)
}

func (s *Server) sendStreamLeaderElectAdvisory(mset *stream) {
	if mset == nil {
		return
	}
	node, stream, acc := mset.raftNode(), mset.name(), mset.account()
	if node == nil {
		return
	}
	subj := JSAdvisoryStreamLeaderElectedPre + "." + stream
	adv := &JSStreamLeaderElectedAdvisory{
		TypedEvent: TypedEvent{
			Type: JSStreamLeaderElectedAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   stream,
		Leader:   s.serverNameForNode(node.GroupLeader()),
		Replicas: s.replicas(node),
		Domain:   s.getOpts().JetStreamDomain,
	}

	// Send to the user's account if not the system account.
	if acc != s.SystemAccount() {
		s.publishAdvisory(acc, subj, adv)
	}
	// Now do system level one. Place account info in adv, and nil account means system.
	adv.Account = acc.GetName()
	s.publishAdvisory(nil, subj, adv)
}

// Will lookup a stream assignment.
// Lock should be held.
func (js *jetStream) streamAssignment(account, stream string) (sa *streamAssignment) {
	cc := js.cluster
	if cc == nil {
		return nil
	}

	if as := cc.streams[account]; as != nil {
		sa = as[stream]
	}
	return sa
}

// processStreamAssignment is called when followers have replicated an assignment.
func (js *jetStream) processStreamAssignment(sa *streamAssignment) bool {
	js.mu.Lock()
	s, cc := js.srv, js.cluster
	accName, stream := sa.Client.serviceAccount(), sa.Config.Name
	noMeta := cc == nil || cc.meta == nil
	var ourID string
	if !noMeta {
		ourID = cc.meta.ID()
	}
	var isMember bool
	if sa.Group != nil && ourID != _EMPTY_ {
		isMember = sa.Group.isMember(ourID)
	}

	// Remove this stream from the inflight proposals
	cc.removeInflightProposal(accName, sa.Config.Name)

	if s == nil || noMeta {
		js.mu.Unlock()
		return false
	}

	accStreams := cc.streams[accName]
	if accStreams == nil {
		accStreams = make(map[string]*streamAssignment)
	} else if osa := accStreams[stream]; osa != nil && osa != sa {
		// Copy over private existing state from former SA.
		sa.Group.node = osa.Group.node
		sa.consumers = osa.consumers
		sa.responded = osa.responded
		sa.err = osa.err
	}

	// Update our state.
	accStreams[stream] = sa
	cc.streams[accName] = accStreams
	hasResponded := sa.responded
	js.mu.Unlock()

	acc, err := s.LookupAccount(accName)
	if err != nil {
		ll := fmt.Sprintf("Account [%s] lookup for stream create failed: %v", accName, err)
		if isMember {
			if !hasResponded {
				// If we can not lookup the account and we are a member, send this result back to the metacontroller leader.
				result := &streamAssignmentResult{
					Account:  accName,
					Stream:   stream,
					Response: &JSApiStreamCreateResponse{ApiResponse: ApiResponse{Type: JSApiStreamCreateResponseType}},
				}
				result.Response.Error = NewJSNoAccountError()
				s.sendInternalMsgLocked(streamAssignmentSubj, _EMPTY_, nil, result)
			}
			s.Warnf(ll)
		} else {
			s.Debugf(ll)
		}
		return false
	}

	var didRemove bool

	// Check if this is for us..
	if isMember {
		js.processClusterCreateStream(acc, sa)
	} else if mset, _ := acc.lookupStream(sa.Config.Name); mset != nil {
		// We have one here even though we are not a member. This can happen on re-assignment.
		s.removeStream(ourID, mset, sa)
	}

	// If this stream assignment does not have a sync subject (bug) set that the meta-leader should check when elected.
	if sa.Sync == _EMPTY_ {
		js.mu.Lock()
		cc.streamsCheck = true
		js.mu.Unlock()
		return false
	}

	return didRemove
}

// processUpdateStreamAssignment is called when followers have replicated an updated assignment.
func (js *jetStream) processUpdateStreamAssignment(sa *streamAssignment) {
	js.mu.RLock()
	s, cc := js.srv, js.cluster
	js.mu.RUnlock()
	if s == nil || cc == nil {
		// TODO(dlc) - debug at least
		return
	}

	accName := sa.Client.serviceAccount()
	stream := sa.Config.Name

	js.mu.Lock()
	if cc.meta == nil {
		js.mu.Unlock()
		return
	}
	ourID := cc.meta.ID()

	var isMember bool
	if sa.Group != nil {
		isMember = sa.Group.isMember(ourID)
	}

	accStreams := cc.streams[accName]
	if accStreams == nil {
		js.mu.Unlock()
		return
	}
	osa := accStreams[stream]
	if osa == nil {
		js.mu.Unlock()
		return
	}

	// Copy over private existing state from former SA.
	sa.Group.node = osa.Group.node
	sa.consumers = osa.consumers
	sa.err = osa.err

	// If we detect we are scaling down to 1, non-clustered, and we had a previous node, clear it here.
	if sa.Config.Replicas == 1 && sa.Group.node != nil {
		sa.Group.node = nil
	}

	// Update our state.
	accStreams[stream] = sa
	cc.streams[accName] = accStreams

	// Make sure we respond if we are a member.
	if isMember {
		sa.responded = false
	} else {
		// Make sure to clean up any old node in case this stream moves back here.
		sa.Group.node = nil
	}
	js.mu.Unlock()

	acc, err := s.LookupAccount(accName)
	if err != nil {
		s.Warnf("Update Stream Account %s, error on lookup: %v", accName, err)
		return
	}

	// Check if this is for us..
	if isMember {
		js.processClusterUpdateStream(acc, osa, sa)
	} else if mset, _ := acc.lookupStream(sa.Config.Name); mset != nil {
		// We have one here even though we are not a member. This can happen on re-assignment.
		s.removeStream(ourID, mset, sa)
	}
}

// Common function to remove ourself from this server.
// This can happen on re-assignment, move, etc
func (s *Server) removeStream(ourID string, mset *stream, nsa *streamAssignment) {
	if mset == nil {
		return
	}
	// Make sure to use the new stream assignment, not our own.
	s.Debugf("JetStream removing stream '%s > %s' from this server", nsa.Client.serviceAccount(), nsa.Config.Name)
	if node := mset.raftNode(); node != nil {
		if node.Leader() {
			node.StepDown(nsa.Group.Preferred)
		}
		node.ProposeRemovePeer(ourID)
		// shut down monitor by shutting down raft
		node.Delete()
	}

	// Make sure this node is no longer attached to our stream assignment.
	if js, _ := s.getJetStreamCluster(); js != nil {
		js.mu.Lock()
		nsa.Group.node = nil
		js.mu.Unlock()
	}

	// wait for monitor to be shut down
	mset.monitorWg.Wait()
	mset.stop(true, false)
}

// processClusterUpdateStream is called when we have a stream assignment that
// has been updated for an existing assignment and we are a member.
func (js *jetStream) processClusterUpdateStream(acc *Account, osa, sa *streamAssignment) {
	if sa == nil {
		return
	}

	js.mu.Lock()
	s, rg := js.srv, sa.Group
	client, subject, reply := sa.Client, sa.Subject, sa.Reply
	alreadyRunning, numReplicas := osa.Group.node != nil, len(rg.Peers)
	needsNode := rg.node == nil
	storage, cfg := sa.Config.Storage, sa.Config
	hasResponded := sa.responded
	sa.responded = true
	recovering := sa.recovering
	js.mu.Unlock()

	mset, err := acc.lookupStream(cfg.Name)
	if err == nil && mset != nil {
		// Make sure we have not had a new group assigned to us.
		if osa.Group.Name != sa.Group.Name {
			s.Warnf("JetStream cluster detected stream remapping for '%s > %s' from %q to %q",
				acc, cfg.Name, osa.Group.Name, sa.Group.Name)
			mset.removeNode()
			alreadyRunning, needsNode = false, true
			// Make sure to clear from original.
			js.mu.Lock()
			osa.Group.node = nil
			js.mu.Unlock()
		}

		var needsSetLeader bool
		if !alreadyRunning && numReplicas > 1 {
			if needsNode {
				mset.setLeader(false)
				js.createRaftGroup(acc.GetName(), rg, storage)
			}
			mset.monitorWg.Add(1)
			// Start monitoring..
			s.startGoRoutine(func() { js.monitorStream(mset, sa, needsNode) })
		} else if numReplicas == 1 && alreadyRunning {
			// We downgraded to R1. Make sure we cleanup the raft node and the stream monitor.
			mset.removeNode()
			// Make sure we are leader now that we are R1.
			needsSetLeader = true
			// In case we need to shutdown the cluster specific subs, etc.
			mset.setLeader(false)
			js.mu.Lock()
			rg.node = nil
			js.mu.Unlock()
		}
		// Call update.
		if err = mset.updateWithAdvisory(cfg, !recovering); err != nil {
			s.Warnf("JetStream cluster error updating stream %q for account %q: %v", cfg.Name, acc.Name, err)
		}
		// Set the new stream assignment.
		mset.setStreamAssignment(sa)
		// Make sure we are the leader now that we are R1.
		if needsSetLeader {
			mset.setLeader(true)
		}
	}

	// If not found we must be expanding into this node since if we are here we know we are a member.
	if err == ErrJetStreamStreamNotFound {
		js.processStreamAssignment(sa)
		return
	}

	if err != nil {
		js.mu.Lock()
		sa.err = err
		result := &streamAssignmentResult{
			Account:  sa.Client.serviceAccount(),
			Stream:   sa.Config.Name,
			Response: &JSApiStreamCreateResponse{ApiResponse: ApiResponse{Type: JSApiStreamCreateResponseType}},
			Update:   true,
		}
		result.Response.Error = NewJSStreamGeneralError(err, Unless(err))
		js.mu.Unlock()

		// Send response to the metadata leader. They will forward to the user as needed.
		s.sendInternalMsgLocked(streamAssignmentSubj, _EMPTY_, nil, result)
		return
	}

	isLeader := mset.IsLeader()

	// Check for missing syncSubject bug.
	if isLeader && osa != nil && osa.Sync == _EMPTY_ {
		if node := mset.raftNode(); node != nil {
			node.StepDown()
		}
		return
	}

	// If we were a single node being promoted assume leadership role for purpose of responding.
	if !hasResponded && !isLeader && !alreadyRunning {
		isLeader = true
	}

	// Check if we should bail.
	if !isLeader || hasResponded || recovering {
		return
	}

	// Send our response.
	var resp = JSApiStreamUpdateResponse{ApiResponse: ApiResponse{Type: JSApiStreamUpdateResponseType}}
	resp.StreamInfo = &StreamInfo{
		Created: mset.createdTime(),
		State:   mset.state(),
		Config:  mset.config(),
		Cluster: js.clusterInfo(mset.raftGroup()),
		Mirror:  mset.mirrorInfo(),
		Sources: mset.sourcesInfo(),
	}

	s.sendAPIResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
}

// processClusterCreateStream is called when we have a stream assignment that
// has been committed and this server is a member of the peer group.
func (js *jetStream) processClusterCreateStream(acc *Account, sa *streamAssignment) {
	if sa == nil {
		return
	}

	js.mu.RLock()
	s, rg := js.srv, sa.Group
	alreadyRunning := rg.node != nil
	storage := sa.Config.Storage
	js.mu.RUnlock()

	// Process the raft group and make sure it's running if needed.
	err := js.createRaftGroup(acc.GetName(), rg, storage)

	// If we are restoring, create the stream if we are R>1 and not the preferred who handles the
	// receipt of the snapshot itself.
	shouldCreate := true
	if sa.Restore != nil {
		if len(rg.Peers) == 1 || rg.node != nil && rg.node.ID() == rg.Preferred {
			shouldCreate = false
		} else {
			sa.Restore = nil
		}
	}

	// Our stream.
	var mset *stream

	// Process here if not restoring or not the leader.
	if shouldCreate && err == nil {
		// Go ahead and create or update the stream.
		mset, err = acc.lookupStream(sa.Config.Name)
		if err == nil && mset != nil {
			osa := mset.streamAssignment()
			// If we already have a stream assignment and they are the same exact config, short circuit here.
			if osa != nil {
				if reflect.DeepEqual(osa.Config, sa.Config) {
					if sa.Group.Name == osa.Group.Name && reflect.DeepEqual(sa.Group.Peers, osa.Group.Peers) {
						// Since this already exists we know it succeeded, just respond to this caller.
						js.mu.RLock()
						client, subject, reply, recovering := sa.Client, sa.Subject, sa.Reply, sa.recovering
						js.mu.RUnlock()

						if !recovering {
							var resp = JSApiStreamCreateResponse{ApiResponse: ApiResponse{Type: JSApiStreamCreateResponseType}}
							resp.StreamInfo = &StreamInfo{
								Created: mset.createdTime(),
								State:   mset.state(),
								Config:  mset.config(),
								Cluster: js.clusterInfo(mset.raftGroup()),
								Sources: mset.sourcesInfo(),
								Mirror:  mset.mirrorInfo(),
							}
							s.sendAPIResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
						}
						return
					} else {
						// We had a bug where we could have multiple assignments for the same
						// stream but with different group assignments, including multiple raft
						// groups. So check for that here. We can only bet on the last one being
						// consistent in the long run, so let it continue if we see this condition.
						s.Warnf("JetStream cluster detected duplicate assignment for stream %q for account %q", sa.Config.Name, acc.Name)
						if osa.Group.node != nil && osa.Group.node != sa.Group.node {
							osa.Group.node.Delete()
							osa.Group.node = nil
						}
					}
				}
			}
			mset.setStreamAssignment(sa)
			if err = mset.updateWithAdvisory(sa.Config, false); err != nil {
				s.Warnf("JetStream cluster error updating stream %q for account %q: %v", sa.Config.Name, acc.Name, err)
				if osa != nil {
					// Process the raft group and make sure it's running if needed.
					js.createRaftGroup(acc.GetName(), osa.Group, storage)
					mset.setStreamAssignment(osa)
				}
				if rg.node != nil {
					rg.node.Delete()
					rg.node = nil
				}
			}
		} else if err == NewJSStreamNotFoundError() {
			// Add in the stream here.
			mset, err = acc.addStreamWithAssignment(sa.Config, nil, sa)
		}
		if mset != nil {
			mset.setCreatedTime(sa.Created)
		}
	}

	// This is an error condition.
	if err != nil {
		if IsNatsErr(err, JSStreamStoreFailedF) {
			s.Warnf("Stream create failed for '%s > %s': %v", sa.Client.serviceAccount(), sa.Config.Name, err)
			err = errStreamStoreFailed
		}
		js.mu.Lock()

		sa.err = err
		hasResponded := sa.responded

		// If out of space do nothing for now.
		if isOutOfSpaceErr(err) {
			hasResponded = true
		}

		if rg.node != nil {
			rg.node.Delete()
		}

		var result *streamAssignmentResult
		if !hasResponded {
			result = &streamAssignmentResult{
				Account:  sa.Client.serviceAccount(),
				Stream:   sa.Config.Name,
				Response: &JSApiStreamCreateResponse{ApiResponse: ApiResponse{Type: JSApiStreamCreateResponseType}},
			}
			result.Response.Error = NewJSStreamCreateError(err, Unless(err))
		}
		js.mu.Unlock()

		// Send response to the metadata leader. They will forward to the user as needed.
		if result != nil {
			s.sendInternalMsgLocked(streamAssignmentSubj, _EMPTY_, nil, result)
		}
		return
	}

	// Start our monitoring routine.
	if rg.node != nil {
		if !alreadyRunning {
			if mset != nil {
				mset.monitorWg.Add(1)
			}
			s.startGoRoutine(func() { js.monitorStream(mset, sa, false) })
		}
	} else {
		// Single replica stream, process manually here.
		// If we are restoring, process that first.
		if sa.Restore != nil {
			// We are restoring a stream here.
			restoreDoneCh := s.processStreamRestore(sa.Client, acc, sa.Config, _EMPTY_, sa.Reply, _EMPTY_)
			s.startGoRoutine(func() {
				defer s.grWG.Done()
				select {
				case err := <-restoreDoneCh:
					if err == nil {
						mset, err = acc.lookupStream(sa.Config.Name)
						if mset != nil {
							mset.setStreamAssignment(sa)
							mset.setCreatedTime(sa.Created)
						}
					}
					if err != nil {
						if mset != nil {
							mset.delete()
						}
						js.mu.Lock()
						sa.err = err
						result := &streamAssignmentResult{
							Account: sa.Client.serviceAccount(),
							Stream:  sa.Config.Name,
							Restore: &JSApiStreamRestoreResponse{ApiResponse: ApiResponse{Type: JSApiStreamRestoreResponseType}},
						}
						result.Restore.Error = NewJSStreamRestoreError(err, Unless(err))
						js.mu.Unlock()
						// Send response to the metadata leader. They will forward to the user as needed.
						b, _ := json.Marshal(result) // Avoids auto-processing and doing fancy json with newlines.
						s.sendInternalMsgLocked(streamAssignmentSubj, _EMPTY_, nil, b)
						return
					}
					js.processStreamLeaderChange(mset, true)

					// Check to see if we have restored consumers here.
					// These are not currently assigned so we will need to do so here.
					if consumers := mset.getPublicConsumers(); len(consumers) > 0 {
						js.mu.RLock()
						cc := js.cluster
						js.mu.RUnlock()

						for _, o := range consumers {
							name, cfg := o.String(), o.config()
							rg := cc.createGroupForConsumer(&cfg, sa)

							// Place our initial state here as well for assignment distribution.
							ca := &consumerAssignment{
								Group:   rg,
								Stream:  sa.Config.Name,
								Name:    name,
								Config:  &cfg,
								Client:  sa.Client,
								Created: o.createdTime(),
							}

							addEntry := encodeAddConsumerAssignment(ca)
							cc.meta.ForwardProposal(addEntry)

							// Check to make sure we see the assignment.
							go func() {
								ticker := time.NewTicker(time.Second)
								defer ticker.Stop()
								for range ticker.C {
									js.mu.RLock()
									ca, meta := js.consumerAssignment(ca.Client.serviceAccount(), sa.Config.Name, name), cc.meta
									js.mu.RUnlock()
									if ca == nil {
										s.Warnf("Consumer assignment has not been assigned, retrying")
										if meta != nil {
											meta.ForwardProposal(addEntry)
										} else {
											return
										}
									} else {
										return
									}
								}
							}()
						}
					}
				case <-s.quitCh:
					return
				}
			})
		} else {
			js.processStreamLeaderChange(mset, true)
		}
	}
}

// processStreamRemoval is called when followers have replicated an assignment.
func (js *jetStream) processStreamRemoval(sa *streamAssignment) {
	js.mu.Lock()
	s, cc := js.srv, js.cluster
	if s == nil || cc == nil || cc.meta == nil {
		// TODO(dlc) - debug at least
		js.mu.Unlock()
		return
	}
	stream := sa.Config.Name
	isMember := sa.Group.isMember(cc.meta.ID())
	wasLeader := cc.isStreamLeader(sa.Client.serviceAccount(), stream)

	// Check if we already have this assigned.
	accStreams := cc.streams[sa.Client.serviceAccount()]
	needDelete := accStreams != nil && accStreams[stream] != nil
	if needDelete {
		delete(accStreams, stream)
		if len(accStreams) == 0 {
			delete(cc.streams, sa.Client.serviceAccount())
		}
	}
	js.mu.Unlock()

	if needDelete {
		js.processClusterDeleteStream(sa, isMember, wasLeader)
	}
}

func (js *jetStream) processClusterDeleteStream(sa *streamAssignment, isMember, wasLeader bool) {
	if sa == nil {
		return
	}
	js.mu.RLock()
	s := js.srv
	node := sa.Group.node
	hadLeader := node == nil || node.GroupLeader() != noLeader
	offline := s.allPeersOffline(sa.Group)
	var isMetaLeader bool
	if cc := js.cluster; cc != nil {
		isMetaLeader = cc.isLeader()
	}
	recovering := sa.recovering
	js.mu.RUnlock()

	stopped := false
	var resp = JSApiStreamDeleteResponse{ApiResponse: ApiResponse{Type: JSApiStreamDeleteResponseType}}
	var err error
	var acc *Account

	// Go ahead and delete the stream if we have it and the account here.
	if acc, _ = s.LookupAccount(sa.Client.serviceAccount()); acc != nil {
		if mset, _ := acc.lookupStream(sa.Config.Name); mset != nil {
			// shut down monitor by shutting down raft
			if n := mset.raftNode(); n != nil {
				n.Delete()
			}
			// wait for monitor to be shut down
			mset.monitorWg.Wait()
			err = mset.stop(true, wasLeader)
			stopped = true
		} else if isMember {
			s.Warnf("JetStream failed to lookup running stream while removing stream '%s > %s' from this server",
				sa.Client.serviceAccount(), sa.Config.Name)
		}
	} else if isMember {
		s.Warnf("JetStream failed to lookup account while removing stream '%s > %s' from this server", sa.Client.serviceAccount(), sa.Config.Name)
	}

	// Always delete the node if present.
	if node != nil {
		node.Delete()
	}

	// This is a stop gap cleanup in case
	// 1) the account does not exist (and mset couldn't be stopped) and/or
	// 2) node was nil (and couldn't be deleted)
	if !stopped || node == nil {
		if sacc := s.SystemAccount(); sacc != nil {
			saccName := sacc.GetName()
			os.RemoveAll(filepath.Join(js.config.StoreDir, saccName, defaultStoreDirName, sa.Group.Name))
			// cleanup dependent consumer groups
			if !stopped {
				for _, ca := range sa.consumers {
					// Make sure we cleanup any possible running nodes for the consumers.
					if isMember && ca.Group != nil && ca.Group.node != nil {
						ca.Group.node.Delete()
					}
					os.RemoveAll(filepath.Join(js.config.StoreDir, saccName, defaultStoreDirName, ca.Group.Name))
				}
			}
		}
	}
	accDir := filepath.Join(js.config.StoreDir, sa.Client.serviceAccount())
	streamDir := filepath.Join(accDir, streamsDir)
	os.RemoveAll(filepath.Join(streamDir, sa.Config.Name))

	// no op if not empty
	os.Remove(streamDir)
	os.Remove(accDir)

	// Normally we want only the leader to respond here, but if we had no leader then all members will respond to make
	// sure we get feedback to the user.
	if !isMember || (hadLeader && !wasLeader) {
		// If all the peers are offline and we are the meta leader we will also respond, so suppress returning here.
		if !(offline && isMetaLeader) {
			return
		}
	}

	// Do not respond if the account does not exist any longer
	if acc == nil || recovering {
		return
	}

	if err != nil {
		resp.Error = NewJSStreamGeneralError(err, Unless(err))
		s.sendAPIErrResponse(sa.Client, acc, sa.Subject, sa.Reply, _EMPTY_, s.jsonResponse(resp))
	} else {
		resp.Success = true
		s.sendAPIResponse(sa.Client, acc, sa.Subject, sa.Reply, _EMPTY_, s.jsonResponse(resp))
	}
}

// processConsumerAssignment is called when followers have replicated an assignment for a consumer.
func (js *jetStream) processConsumerAssignment(ca *consumerAssignment) {
	js.mu.RLock()
	s, cc := js.srv, js.cluster
	accName, stream, consumerName := ca.Client.serviceAccount(), ca.Stream, ca.Name
	noMeta := cc == nil || cc.meta == nil
	var ourID string
	if !noMeta {
		ourID = cc.meta.ID()
	}
	var isMember bool
	if ca.Group != nil && ourID != _EMPTY_ {
		isMember = ca.Group.isMember(ourID)
	}
	js.mu.RUnlock()

	if s == nil || noMeta {
		return
	}

	sa := js.streamAssignment(accName, stream)
	if sa == nil {
		s.Debugf("Consumer create failed, could not locate stream '%s > %s'", accName, stream)
		return
	}

	// Might need this below.
	numReplicas := sa.Config.Replicas

	// Track if this existed already.
	var wasExisting bool

	// Check if we have an existing consumer assignment.
	js.mu.Lock()
	if sa.consumers == nil {
		sa.consumers = make(map[string]*consumerAssignment)
	} else if oca := sa.consumers[ca.Name]; oca != nil {
		wasExisting = true
		// Copy over private existing state from former SA.
		ca.Group.node = oca.Group.node
		ca.responded = oca.responded
		ca.err = oca.err
	}

	// Capture the optional state. We will pass it along if we are a member to apply.
	// This is only applicable when restoring a stream with consumers.
	state := ca.State
	ca.State = nil

	// Place into our internal map under the stream assignment.
	// Ok to replace an existing one, we check on process call below.
	sa.consumers[ca.Name] = ca
	js.mu.Unlock()

	acc, err := s.LookupAccount(accName)
	if err != nil {
		ll := fmt.Sprintf("Account [%s] lookup for consumer create failed: %v", accName, err)
		if isMember {
			if !js.isMetaRecovering() {
				// If we can not lookup the account and we are a member, send this result back to the metacontroller leader.
				result := &consumerAssignmentResult{
					Account:  accName,
					Stream:   stream,
					Consumer: consumerName,
					Response: &JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}},
				}
				result.Response.Error = NewJSNoAccountError()
				s.sendInternalMsgLocked(consumerAssignmentSubj, _EMPTY_, nil, result)
			}
			s.Warnf(ll)
		} else {
			s.Debugf(ll)
		}
		return
	}

	// Check if this is for us..
	if isMember {
		js.processClusterCreateConsumer(ca, state, wasExisting)
	} else {
		// We need to be removed here, we are no longer assigned.
		// Grab consumer if we have it.
		var o *consumer
		if mset, _ := acc.lookupStream(sa.Config.Name); mset != nil {
			o = mset.lookupConsumer(ca.Name)
		}

		// Check if we have a raft node running, meaning we are no longer part of the group but were.
		js.mu.Lock()
		if node := ca.Group.node; node != nil {
			// We have one here even though we are not a member. This can happen on re-assignment.
			s.Debugf("JetStream removing consumer '%s > %s > %s' from this server", sa.Client.serviceAccount(), sa.Config.Name, ca.Name)
			if node.Leader() {
				s.Debugf("JetStream consumer '%s > %s > %s' is being removed and was the leader, will perform stepdown",
					sa.Client.serviceAccount(), sa.Config.Name, ca.Name)

				peers, cn := node.Peers(), s.cachedClusterName()
				migrating := numReplicas != len(peers)

				// Select a new peer to transfer to. If we are a migrating make sure its from the new cluster.
				var npeer string
				for _, r := range peers {
					if !r.Current {
						continue
					}
					if !migrating {
						npeer = r.ID
						break
					} else if sir, ok := s.nodeToInfo.Load(r.ID); ok && sir != nil {
						si := sir.(nodeInfo)
						if si.cluster != cn {
							npeer = r.ID
							break
						}
					}
				}
				// Clear the raftnode from our consumer so that a subsequent o.delete will not also issue a stepdown.
				if o != nil {
					o.clearRaftNode()
				}
				// Manually handle the stepdown and deletion of the node.
				node.UpdateKnownPeers(ca.Group.Peers)
				node.StepDown(npeer)
				node.Delete()
			} else {
				node.UpdateKnownPeers(ca.Group.Peers)
			}
		}
		// Always clear the old node.
		ca.Group.node = nil
		ca.err = nil
		js.mu.Unlock()

		if o != nil {
			o.deleteWithoutAdvisory()
		}
	}
}

func (js *jetStream) processConsumerRemoval(ca *consumerAssignment) {
	js.mu.Lock()
	s, cc := js.srv, js.cluster
	if s == nil || cc == nil || cc.meta == nil {
		// TODO(dlc) - debug at least
		js.mu.Unlock()
		return
	}
	isMember := ca.Group.isMember(cc.meta.ID())
	wasLeader := cc.isConsumerLeader(ca.Client.serviceAccount(), ca.Stream, ca.Name)

	// Delete from our state.
	var needDelete bool
	if accStreams := cc.streams[ca.Client.serviceAccount()]; accStreams != nil {
		if sa := accStreams[ca.Stream]; sa != nil && sa.consumers != nil && sa.consumers[ca.Name] != nil {
			needDelete = true
			delete(sa.consumers, ca.Name)
		}
	}
	js.mu.Unlock()

	if needDelete {
		js.processClusterDeleteConsumer(ca, isMember, wasLeader)
	}
}

type consumerAssignmentResult struct {
	Account  string                       `json:"account"`
	Stream   string                       `json:"stream"`
	Consumer string                       `json:"consumer"`
	Response *JSApiConsumerCreateResponse `json:"response,omitempty"`
}

// processClusterCreateConsumer is when we are a member of the group and need to create the consumer.
func (js *jetStream) processClusterCreateConsumer(ca *consumerAssignment, state *ConsumerState, wasExisting bool) {
	if ca == nil {
		return
	}
	js.mu.RLock()
	s := js.srv
	rg := ca.Group
	alreadyRunning := rg != nil && rg.node != nil
	accName, stream, consumer := ca.Client.serviceAccount(), ca.Stream, ca.Name
	js.mu.RUnlock()

	acc, err := s.LookupAccount(accName)
	if err != nil {
		s.Warnf("JetStream cluster failed to lookup axccount %q: %v", accName, err)
		return
	}

	// Go ahead and create or update the consumer.
	mset, err := acc.lookupStream(stream)
	if err != nil {
		if !js.isMetaRecovering() {
			js.mu.Lock()
			s.Warnf("Consumer create failed, could not locate stream '%s > %s > %s'", ca.Client.serviceAccount(), ca.Stream, ca.Name)
			ca.err = NewJSStreamNotFoundError()
			result := &consumerAssignmentResult{
				Account:  ca.Client.serviceAccount(),
				Stream:   ca.Stream,
				Consumer: ca.Name,
				Response: &JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}},
			}
			result.Response.Error = NewJSStreamNotFoundError()
			s.sendInternalMsgLocked(consumerAssignmentSubj, _EMPTY_, nil, result)
			js.mu.Unlock()
		}
		return
	}

	// Check if we already have this consumer running.
	o := mset.lookupConsumer(consumer)

	if !alreadyRunning {
		// Process the raft group and make sure its running if needed.
		storage := mset.config().Storage
		if ca.Config.MemoryStorage {
			storage = MemoryStorage
		}
		// No-op if R1.
		js.createRaftGroup(accName, rg, storage)
	} else {
		// If we are clustered update the known peers.
		js.mu.RLock()
		if node := rg.node; node != nil {
			node.UpdateKnownPeers(ca.Group.Peers)
		}
		js.mu.RUnlock()
	}

	// Check if we already have this consumer running.
	var didCreate, isConfigUpdate, needsLocalResponse bool
	if o == nil {
		// Add in the consumer if needed.
		if o, err = mset.addConsumerWithAssignment(ca.Config, ca.Name, ca, false); err == nil {
			didCreate = true
		}
	} else {
		// This consumer exists.
		// Only update if config is really different.
		cfg := o.config()
		if isConfigUpdate = !reflect.DeepEqual(&cfg, ca.Config); isConfigUpdate {
			// Call into update, ignore consumer exists error here since this means an old deliver subject is bound
			// which can happen on restart etc.
			if err := o.updateConfig(ca.Config); err != nil && err != NewJSConsumerNameExistError() {
				// This is essentially an update that has failed. Respond back to metaleader if we are not recovering.
				js.mu.RLock()
				if !js.metaRecovering {
					result := &consumerAssignmentResult{
						Account:  accName,
						Stream:   stream,
						Consumer: consumer,
						Response: &JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}},
					}
					result.Response.Error = NewJSConsumerNameExistError()
					s.sendInternalMsgLocked(consumerAssignmentSubj, _EMPTY_, nil, result)
				}
				s.Warnf("Consumer create failed during update for '%s > %s > %s': %v", ca.Client.serviceAccount(), ca.Stream, ca.Name, err)
				js.mu.RUnlock()
				return
			}
		}

		var sendState bool
		js.mu.RLock()
		n := rg.node
		// Check if we already had a consumer assignment and its still pending.
		cca, oca := ca, o.consumerAssignment()
		if oca != nil {
			if !oca.responded {
				// We can't override info for replying here otherwise leader once elected can not respond.
				// So copy over original client and the reply from the old ca.
				cac := *ca
				cac.Client = oca.Client
				cac.Reply = oca.Reply
				cca = &cac
				needsLocalResponse = true
			}
			// If we look like we are scaling up, let's send our current state to the group.
			sendState = len(ca.Group.Peers) > len(oca.Group.Peers) && o.IsLeader() && n != nil
			// Signal that this is an update
			if ca.Reply != _EMPTY_ {
				isConfigUpdate = true
			}
		}
		js.mu.RUnlock()

		if sendState {
			if snap, err := o.store.EncodedState(); err == nil {
				n.SendSnapshot(snap)
			}
		}

		// Set CA for our consumer.
		o.setConsumerAssignment(cca)
		s.Debugf("JetStream cluster, consumer was already running")
	}

	// If we have an initial state set apply that now.
	if state != nil && o != nil {
		o.mu.Lock()
		err = o.setStoreState(state)
		o.mu.Unlock()
	}

	if err != nil {
		if IsNatsErr(err, JSConsumerStoreFailedErrF) {
			s.Warnf("Consumer create failed for '%s > %s > %s': %v", ca.Client.serviceAccount(), ca.Stream, ca.Name, err)
			err = errConsumerStoreFailed
		}

		js.mu.Lock()

		ca.err = err
		hasResponded := ca.responded

		// If out of space do nothing for now.
		if isOutOfSpaceErr(err) {
			hasResponded = true
		}

		if rg.node != nil {
			rg.node.Delete()
		}

		var result *consumerAssignmentResult
		if !hasResponded && !js.metaRecovering {
			result = &consumerAssignmentResult{
				Account:  ca.Client.serviceAccount(),
				Stream:   ca.Stream,
				Consumer: ca.Name,
				Response: &JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}},
			}
			result.Response.Error = NewJSConsumerCreateError(err, Unless(err))
		} else if err == errNoInterest {
			// This is a stranded ephemeral, let's clean this one up.
			subject := fmt.Sprintf(JSApiConsumerDeleteT, ca.Stream, ca.Name)
			mset.outq.send(newJSPubMsg(subject, _EMPTY_, _EMPTY_, nil, nil, nil, 0))
		}
		js.mu.Unlock()

		if result != nil {
			// Send response to the metadata leader. They will forward to the user as needed.
			b, _ := json.Marshal(result) // Avoids auto-processing and doing fancy json with newlines.
			s.sendInternalMsgLocked(consumerAssignmentSubj, _EMPTY_, nil, b)
		}
	} else {
		if didCreate {
			o.setCreatedTime(ca.Created)
		} else {
			// Check for scale down to 1..
			if rg.node != nil && len(rg.Peers) == 1 {
				o.clearNode()
				o.setLeader(true)
				// Need to clear from rg too.
				js.mu.Lock()
				rg.node = nil
				client, subject, reply := ca.Client, ca.Subject, ca.Reply
				js.mu.Unlock()
				var resp = JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}}
				resp.ConsumerInfo = o.info()
				s.sendAPIResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
				return
			}
		}

		if rg.node == nil {
			// Single replica consumer, process manually here.
			js.mu.Lock()
			// Force response in case we think this is an update.
			if !js.metaRecovering && isConfigUpdate {
				ca.responded = false
			}
			js.mu.Unlock()
			js.processConsumerLeaderChange(o, true)
		} else {
			// Clustered consumer.
			// Start our monitoring routine if needed.
			if !alreadyRunning && !o.isMonitorRunning() {
				o.monitorWg.Add(1)
				s.startGoRoutine(func() { js.monitorConsumer(o, ca) })
			}
			// For existing consumer, only send response if not recovering.
			if wasExisting && !js.isMetaRecovering() {
				if o.IsLeader() || (!didCreate && needsLocalResponse) {
					// Process if existing as an update. Double check that this is not recovered.
					js.mu.RLock()
					client, subject, reply, recovering := ca.Client, ca.Subject, ca.Reply, ca.recovering
					js.mu.RUnlock()
					if !recovering {
						var resp = JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}}
						resp.ConsumerInfo = o.info()
						s.sendAPIResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
					}
				}
			}
		}
	}
}

func (js *jetStream) processClusterDeleteConsumer(ca *consumerAssignment, isMember, wasLeader bool) {
	if ca == nil {
		return
	}
	js.mu.RLock()
	s := js.srv
	node := ca.Group.node
	offline := s.allPeersOffline(ca.Group)
	var isMetaLeader bool
	if cc := js.cluster; cc != nil {
		isMetaLeader = cc.isLeader()
	}
	recovering := ca.recovering
	js.mu.RUnlock()

	stopped := false
	var resp = JSApiConsumerDeleteResponse{ApiResponse: ApiResponse{Type: JSApiConsumerDeleteResponseType}}
	var err error
	var acc *Account

	// Go ahead and delete the consumer if we have it and the account.
	if acc, _ = s.LookupAccount(ca.Client.serviceAccount()); acc != nil {
		if mset, _ := acc.lookupStream(ca.Stream); mset != nil {
			if o := mset.lookupConsumer(ca.Name); o != nil {
				err = o.stopWithFlags(true, false, true, wasLeader)
				stopped = true
			}
		}
	}

	// Always delete the node if present.
	if node != nil {
		node.Delete()
	}

	// This is a stop gap cleanup in case
	// 1) the account does not exist (and mset consumer couldn't be stopped) and/or
	// 2) node was nil (and couldn't be deleted)
	if !stopped || node == nil {
		if sacc := s.SystemAccount(); sacc != nil {
			os.RemoveAll(filepath.Join(js.config.StoreDir, sacc.GetName(), defaultStoreDirName, ca.Group.Name))
		}
	}

	if !wasLeader || ca.Reply == _EMPTY_ {
		if !(offline && isMetaLeader) {
			return
		}
	}

	// Do not respond if the account does not exist any longer or this is during recovery.
	if acc == nil || recovering {
		return
	}

	if err != nil {
		resp.Error = NewJSStreamNotFoundError(Unless(err))
		s.sendAPIErrResponse(ca.Client, acc, ca.Subject, ca.Reply, _EMPTY_, s.jsonResponse(resp))
	} else {
		resp.Success = true
		s.sendAPIResponse(ca.Client, acc, ca.Subject, ca.Reply, _EMPTY_, s.jsonResponse(resp))
	}
}

// Returns the consumer assignment, or nil if not present.
// Lock should be held.
func (js *jetStream) consumerAssignment(account, stream, consumer string) *consumerAssignment {
	if sa := js.streamAssignment(account, stream); sa != nil {
		return sa.consumers[consumer]
	}
	return nil
}

// consumerAssigned informs us if this server has this consumer assigned.
func (jsa *jsAccount) consumerAssigned(stream, consumer string) bool {
	jsa.mu.RLock()
	js, acc := jsa.js, jsa.account
	jsa.mu.RUnlock()

	if js == nil {
		return false
	}
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.cluster.isConsumerAssigned(acc, stream, consumer)
}

// Read lock should be held.
func (cc *jetStreamCluster) isConsumerAssigned(a *Account, stream, consumer string) bool {
	// Non-clustered mode always return true.
	if cc == nil {
		return true
	}
	if cc.meta == nil {
		return false
	}
	var sa *streamAssignment
	accStreams := cc.streams[a.Name]
	if accStreams != nil {
		sa = accStreams[stream]
	}
	if sa == nil {
		// TODO(dlc) - This should not happen.
		return false
	}
	ca := sa.consumers[consumer]
	if ca == nil {
		return false
	}
	rg := ca.Group
	// Check if we are the leader of this raftGroup assigned to the stream.
	ourID := cc.meta.ID()
	for _, peer := range rg.Peers {
		if peer == ourID {
			return true
		}
	}
	return false
}

// Returns our stream and underlying raft node.
func (o *consumer) streamAndNode() (*stream, RaftNode) {
	if o == nil {
		return nil, nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.mset, o.node
}

// Return the replica count for this consumer. If the consumer has been
// stopped, this will return an error.
func (o *consumer) replica() (int, error) {
	o.mu.RLock()
	oCfg := o.cfg
	mset := o.mset
	o.mu.RUnlock()
	if mset == nil {
		return 0, errBadConsumer
	}
	sCfg := mset.config()
	return oCfg.replicas(&sCfg), nil
}

func (o *consumer) raftGroup() *raftGroup {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.ca == nil {
		return nil
	}
	return o.ca.Group
}

func (o *consumer) clearRaftNode() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.node = nil
}

func (o *consumer) raftNode() RaftNode {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.node
}

func (js *jetStream) monitorConsumer(o *consumer, ca *consumerAssignment) {
	s, n, cc := js.server(), o.raftNode(), js.cluster
	defer s.grWG.Done()

	defer o.monitorWg.Done()

	if n == nil {
		s.Warnf("No RAFT group for '%s > %s > %s'", o.acc.Name, ca.Stream, ca.Name)
		return
	}

	// Make sure only one is running.
	if o.checkInMonitor() {
		return
	}
	defer o.clearMonitorRunning()

	// Make sure to stop the raft group on exit to prevent accidental memory bloat.
	// This should be below the checkInMonitor call though to avoid stopping it out
	// from underneath the one that is running since it will be the same raft node.
	defer n.Stop()

	qch, lch, aq, uch, ourPeerId := n.QuitC(), n.LeadChangeC(), n.ApplyQ(), o.updateC(), cc.meta.ID()

	s.Debugf("Starting consumer monitor for '%s > %s > %s' [%s]", o.acc.Name, ca.Stream, ca.Name, n.Group())
	defer s.Debugf("Exiting consumer monitor for '%s > %s > %s' [%s]", o.acc.Name, ca.Stream, ca.Name, n.Group())

	const (
		compactInterval = 2 * time.Minute
		compactSizeMin  = 64 * 1024 // What is stored here is always small for consumers.
		compactNumMin   = 1024
		minSnapDelta    = 10 * time.Second
	)

	// Spread these out for large numbers on server restart.
	rci := time.Duration(rand.Int63n(int64(time.Minute)))
	t := time.NewTicker(compactInterval + rci)
	defer t.Stop()

	// Highwayhash key for generating hashes.
	key := make([]byte, 32)
	rand.Read(key)

	// Hash of the last snapshot (fixed size in memory).
	var lastSnap []byte
	var lastSnapTime time.Time

	doSnapshot := func(force bool) {
		// Bail if trying too fast and not in a forced situation.
		if !force && time.Since(lastSnapTime) < minSnapDelta {
			return
		}

		// Check several things to see if we need a snapshot.
		ne, nb := n.Size()
		if !n.NeedSnapshot() {
			// Check if we should compact etc. based on size of log.
			if !force && ne < compactNumMin && nb < compactSizeMin {
				return
			}
		}

		if snap, err := o.store.EncodedState(); err == nil {
			hash := highwayhash.Sum(snap, key)
			// If the state hasn't changed but the log has gone way over
			// the compaction size then we will want to compact anyway.
			// This can happen for example when a pull consumer fetches a
			// lot on an idle stream, log entries get distributed but the
			// state never changes, therefore the log never gets compacted.
			if !bytes.Equal(hash[:], lastSnap) || ne >= compactNumMin || nb >= compactSizeMin {
				if err := n.InstallSnapshot(snap); err == nil {
					lastSnap, lastSnapTime = hash[:], time.Now()
				} else if err != errNoSnapAvailable && err != errNodeClosed {
					s.Warnf("Failed to install snapshot for '%s > %s > %s' [%s]: %v", o.acc.Name, ca.Stream, ca.Name, n.Group(), err)
				}
			}
		}
	}

	// For migration tracking.
	var mmt *time.Ticker
	var mmtc <-chan time.Time

	startMigrationMonitoring := func() {
		if mmt == nil {
			mmt = time.NewTicker(500 * time.Millisecond)
			mmtc = mmt.C
		}
	}

	stopMigrationMonitoring := func() {
		if mmt != nil {
			mmt.Stop()
			mmt, mmtc = nil, nil
		}
	}
	defer stopMigrationMonitoring()

	// Track if we are leader.
	var isLeader bool
	recovering := true

	for {
		select {
		case <-s.quitCh:
			return
		case <-qch:
			return
		case <-aq.ch:
			ces := aq.pop()
			for _, ce := range ces {
				// No special processing needed for when we are caught up on restart.
				if ce == nil {
					recovering = false
					if n.NeedSnapshot() {
						doSnapshot(true)
					}
					// Check our state if we are under an interest based stream.
					o.checkStateForInterestStream()
				} else if !recovering {
					if err := js.applyConsumerEntries(o, ce, isLeader); err == nil {
						ne, nb := n.Applied(ce.Index)
						// If we have at least min entries to compact, go ahead and snapshot/compact.
						if nb > 0 && ne >= compactNumMin || nb > compactSizeMin {
							doSnapshot(false)
						}
					} else {
						s.Warnf("Error applying consumer entries to '%s > %s'", ca.Client.serviceAccount(), ca.Name)
					}
				}
			}
			aq.recycle(&ces)
		case isLeader = <-lch:
			if recovering && !isLeader {
				js.setConsumerAssignmentRecovering(ca)
			}

			// Process the change.
			if err := js.processConsumerLeaderChange(o, isLeader); err == nil && isLeader {
				doSnapshot(true)
			}

			// We may receive a leader change after the consumer assignment which would cancel us
			// monitoring for this closely. So re-assess our state here as well.
			// Or the old leader is no longer part of the set and transferred leadership
			// for this leader to resume with removal
			rg := o.raftGroup()

			// Check for migrations (peer count and replica count differ) here.
			// We set the state on the stream assignment update below.
			replicas, err := o.replica()
			if err != nil {
				continue
			}
			if isLeader && len(rg.Peers) != replicas {
				startMigrationMonitoring()
			} else {
				stopMigrationMonitoring()
			}
		case <-uch:
			// keep consumer assignment current
			ca = o.consumerAssignment()
			// We get this when we have a new consumer assignment caused by an update.
			// We want to know if we are migrating.
			rg := o.raftGroup()
			// keep peer list up to date with config
			js.checkPeers(rg)
			// If we are migrating, monitor for the new peers to be caught up.
			replicas, err := o.replica()
			if err != nil {
				continue
			}
			if isLeader && len(rg.Peers) != replicas {
				startMigrationMonitoring()
			} else {
				stopMigrationMonitoring()
			}
		case <-mmtc:
			if !isLeader {
				// We are no longer leader, so not our job.
				stopMigrationMonitoring()
				continue
			}
			rg := o.raftGroup()
			ci := js.clusterInfo(rg)
			replicas, err := o.replica()
			if err != nil {
				continue
			}
			if len(rg.Peers) <= replicas {
				// Migration no longer happening, so not our job anymore
				stopMigrationMonitoring()
				continue
			}
			newPeers, oldPeers, newPeerSet, _ := genPeerInfo(rg.Peers, len(rg.Peers)-replicas)

			// If we are part of the new peerset and we have been passed the baton.
			// We will handle scale down.
			if newPeerSet[ourPeerId] {
				for _, p := range oldPeers {
					n.ProposeRemovePeer(p)
				}
				cca := ca.copyGroup()
				cca.Group.Peers = newPeers
				cca.Group.Cluster = s.cachedClusterName()
				cc.meta.ForwardProposal(encodeAddConsumerAssignment(cca))
				s.Noticef("Scaling down '%s > %s > %s' to %+v", ca.Client.serviceAccount(), ca.Stream, ca.Name, s.peerSetToNames(newPeers))

			} else {
				var newLeaderPeer, newLeader, newCluster string
				neededCurrent, current := replicas/2+1, 0
				for _, r := range ci.Replicas {
					if r.Current && newPeerSet[r.Peer] {
						current++
						if newCluster == _EMPTY_ {
							newLeaderPeer, newLeader, newCluster = r.Peer, r.Name, r.cluster
						}
					}
				}

				// Check if we have a quorom
				if current >= neededCurrent {
					s.Noticef("Transfer of consumer leader for '%s > %s > %s' to '%s'", ca.Client.serviceAccount(), ca.Stream, ca.Name, newLeader)
					n.StepDown(newLeaderPeer)
				}
			}

		case <-t.C:
			doSnapshot(false)
		}
	}
}

func (js *jetStream) applyConsumerEntries(o *consumer, ce *CommittedEntry, isLeader bool) error {
	for _, e := range ce.Entries {
		if e.Type == EntrySnapshot {
			// No-op needed?
			state, err := decodeConsumerState(e.Data)
			if err != nil {
				if mset, node := o.streamAndNode(); mset != nil && node != nil {
					s := js.srv
					s.Errorf("JetStream cluster could not decode consumer snapshot for '%s > %s > %s' [%s]",
						mset.account(), mset.name(), o, node.Group())
				}
				panic(err.Error())
			}

			if err = o.store.Update(state); err != nil {
				o.mu.RLock()
				s, acc, mset, name := o.srv, o.acc, o.mset, o.name
				o.mu.RUnlock()
				if s != nil && mset != nil {
					s.Warnf("Consumer '%s > %s > %s' error on store update from snapshot entry: %v", acc, mset.name(), name, err)
				}
			} else {
				o.checkStateForInterestStream()
			}

		} else if e.Type == EntryRemovePeer {
			js.mu.RLock()
			var ourID string
			if js.cluster != nil && js.cluster.meta != nil {
				ourID = js.cluster.meta.ID()
			}
			js.mu.RUnlock()
			if peer := string(e.Data); peer == ourID {
				shouldRemove := true
				if mset := o.getStream(); mset != nil {
					if sa := mset.streamAssignment(); sa != nil && sa.Group != nil {
						js.mu.RLock()
						shouldRemove = !sa.Group.isMember(ourID)
						js.mu.RUnlock()
					}
				}
				if shouldRemove {
					o.stopWithFlags(true, false, false, false)
				}
			}
			return nil
		} else if e.Type == EntryAddPeer {
			// Ignore for now.
		} else {
			buf := e.Data
			switch entryOp(buf[0]) {
			case updateDeliveredOp:
				// These are handled in place in leaders.
				if !isLeader {
					dseq, sseq, dc, ts, err := decodeDeliveredUpdate(buf[1:])
					if err != nil {
						if mset, node := o.streamAndNode(); mset != nil && node != nil {
							s := js.srv
							s.Errorf("JetStream cluster could not decode consumer delivered update for '%s > %s > %s' [%s]",
								mset.account(), mset.name(), o, node.Group())
						}
						panic(err.Error())
					}
					// Make sure to update delivered under the lock.
					o.mu.Lock()
					err = o.store.UpdateDelivered(dseq, sseq, dc, ts)
					o.ldt = time.Now()
					o.mu.Unlock()
					if err != nil {
						panic(err.Error())
					}
				}
			case updateAcksOp:
				dseq, sseq, err := decodeAckUpdate(buf[1:])
				if err != nil {
					if mset, node := o.streamAndNode(); mset != nil && node != nil {
						s := js.srv
						s.Errorf("JetStream cluster could not decode consumer ack update for '%s > %s > %s' [%s]",
							mset.account(), mset.name(), o, node.Group())
					}
					panic(err.Error())
				}
				o.processReplicatedAck(dseq, sseq)
			case updateSkipOp:
				o.mu.Lock()
				if !o.isLeader() {
					var le = binary.LittleEndian
					if sseq := le.Uint64(buf[1:]); sseq > o.sseq {
						o.sseq = sseq
					}
				}
				o.mu.Unlock()
			case addPendingRequest:
				o.mu.Lock()
				if !o.isLeader() {
					if o.prm == nil {
						o.prm = make(map[string]struct{})
					}
					o.prm[string(buf[1:])] = struct{}{}
				}
				o.mu.Unlock()
			case removePendingRequest:
				o.mu.Lock()
				if !o.isLeader() {
					if o.prm != nil {
						delete(o.prm, string(buf[1:]))
					}
				}
				o.mu.Unlock()
			default:
				panic(fmt.Sprintf("JetStream Cluster Unknown group entry op type! %v", entryOp(buf[0])))
			}
		}
	}
	return nil
}

func (o *consumer) processReplicatedAck(dseq, sseq uint64) {
	o.mu.Lock()

	mset := o.mset
	if o.closed || mset == nil {
		o.mu.Unlock()
		return
	}

	// Update activity.
	o.lat = time.Now()

	// Do actual ack update to store.
	o.store.UpdateAcks(dseq, sseq)

	if o.retention == LimitsPolicy {
		o.mu.Unlock()
		return
	}

	var sagap uint64
	if o.cfg.AckPolicy == AckAll {
		if o.isLeader() {
			sagap = sseq - o.asflr
		} else {
			// We are a follower so only have the store state, so read that in.
			state, err := o.store.State()
			if err != nil {
				o.mu.Unlock()
				return
			}
			sagap = sseq - state.AckFloor.Stream
		}
	}
	o.mu.Unlock()

	if sagap > 1 {
		// FIXME(dlc) - This is very inefficient, will need to fix.
		for seq := sseq; seq > sseq-sagap; seq-- {
			mset.ackMsg(o, seq)
		}
	} else {
		mset.ackMsg(o, sseq)
	}
}

var errBadAckUpdate = errors.New("jetstream cluster bad replicated ack update")
var errBadDeliveredUpdate = errors.New("jetstream cluster bad replicated delivered update")

func decodeAckUpdate(buf []byte) (dseq, sseq uint64, err error) {
	var bi, n int
	if dseq, n = binary.Uvarint(buf); n < 0 {
		return 0, 0, errBadAckUpdate
	}
	bi += n
	if sseq, n = binary.Uvarint(buf[bi:]); n < 0 {
		return 0, 0, errBadAckUpdate
	}
	return dseq, sseq, nil
}

func decodeDeliveredUpdate(buf []byte) (dseq, sseq, dc uint64, ts int64, err error) {
	var bi, n int
	if dseq, n = binary.Uvarint(buf); n < 0 {
		return 0, 0, 0, 0, errBadDeliveredUpdate
	}
	bi += n
	if sseq, n = binary.Uvarint(buf[bi:]); n < 0 {
		return 0, 0, 0, 0, errBadDeliveredUpdate
	}
	bi += n
	if dc, n = binary.Uvarint(buf[bi:]); n < 0 {
		return 0, 0, 0, 0, errBadDeliveredUpdate
	}
	bi += n
	if ts, n = binary.Varint(buf[bi:]); n < 0 {
		return 0, 0, 0, 0, errBadDeliveredUpdate
	}
	return dseq, sseq, dc, ts, nil
}

func (js *jetStream) processConsumerLeaderChange(o *consumer, isLeader bool) error {
	stepDownIfLeader := func() error {
		if node := o.raftNode(); node != nil && isLeader {
			node.StepDown()
		}
		return errors.New("failed to update consumer leader status")
	}

	ca := o.consumerAssignment()
	if ca == nil {
		return stepDownIfLeader()
	}
	js.mu.Lock()
	s, account, err := js.srv, ca.Client.serviceAccount(), ca.err
	client, subject, reply := ca.Client, ca.Subject, ca.Reply
	hasResponded := ca.responded
	ca.responded = true
	js.mu.Unlock()

	streamName, consumerName := o.streamName(), o.String()
	acc, _ := s.LookupAccount(account)
	if acc == nil {
		return stepDownIfLeader()
	}

	if isLeader {
		s.Noticef("JetStream cluster new consumer leader for '%s > %s > %s'", ca.Client.serviceAccount(), streamName, consumerName)
		s.sendConsumerLeaderElectAdvisory(o)
		// Check for peer removal and process here if needed.
		js.checkPeers(ca.Group)
	} else {
		// We are stepping down.
		// Make sure if we are doing so because we have lost quorum that we send the appropriate advisories.
		if node := o.raftNode(); node != nil && !node.Quorum() && time.Since(node.Created()) > 5*time.Second {
			s.sendConsumerLostQuorumAdvisory(o)
		}
	}

	// Tell consumer to switch leader status.
	o.setLeader(isLeader)

	if !isLeader || hasResponded {
		if isLeader {
			o.clearInitialInfo()
		}
		return nil
	}

	var resp = JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}}
	if err != nil {
		resp.Error = NewJSConsumerCreateError(err, Unless(err))
		s.sendAPIErrResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
	} else {
		resp.ConsumerInfo = o.initialInfo()
		s.sendAPIResponse(client, acc, subject, reply, _EMPTY_, s.jsonResponse(&resp))
		if node := o.raftNode(); node != nil {
			o.sendCreateAdvisory()
		}
	}

	return nil
}

// Determines if we should send lost quorum advisory. We throttle these after first one.
func (o *consumer) shouldSendLostQuorum() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if time.Since(o.lqsent) >= lostQuorumAdvInterval {
		o.lqsent = time.Now()
		return true
	}
	return false
}

func (s *Server) sendConsumerLostQuorumAdvisory(o *consumer) {
	if o == nil {
		return
	}
	node, stream, consumer, acc := o.raftNode(), o.streamName(), o.String(), o.account()
	if node == nil {
		return
	}
	if !o.shouldSendLostQuorum() {
		return
	}

	s.Warnf("JetStream cluster consumer '%s > %s > %s' has NO quorum, stalled.", acc.GetName(), stream, consumer)

	subj := JSAdvisoryConsumerQuorumLostPre + "." + stream + "." + consumer
	adv := &JSConsumerQuorumLostAdvisory{
		TypedEvent: TypedEvent{
			Type: JSConsumerQuorumLostAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   stream,
		Consumer: consumer,
		Replicas: s.replicas(node),
		Domain:   s.getOpts().JetStreamDomain,
	}

	// Send to the user's account if not the system account.
	if acc != s.SystemAccount() {
		s.publishAdvisory(acc, subj, adv)
	}
	// Now do system level one. Place account info in adv, and nil account means system.
	adv.Account = acc.GetName()
	s.publishAdvisory(nil, subj, adv)
}

func (s *Server) sendConsumerLeaderElectAdvisory(o *consumer) {
	if o == nil {
		return
	}
	node, stream, consumer, acc := o.raftNode(), o.streamName(), o.String(), o.account()
	if node == nil {
		return
	}

	subj := JSAdvisoryConsumerLeaderElectedPre + "." + stream + "." + consumer
	adv := &JSConsumerLeaderElectedAdvisory{
		TypedEvent: TypedEvent{
			Type: JSConsumerLeaderElectedAdvisoryType,
			ID:   nuid.Next(),
			Time: time.Now().UTC(),
		},
		Stream:   stream,
		Consumer: consumer,
		Leader:   s.serverNameForNode(node.GroupLeader()),
		Replicas: s.replicas(node),
		Domain:   s.getOpts().JetStreamDomain,
	}

	// Send to the user's account if not the system account.
	if acc != s.SystemAccount() {
		s.publishAdvisory(acc, subj, adv)
	}
	// Now do system level one. Place account info in adv, and nil account means system.
	adv.Account = acc.GetName()
	s.publishAdvisory(nil, subj, adv)
}

type streamAssignmentResult struct {
	Account  string                      `json:"account"`
	Stream   string                      `json:"stream"`
	Response *JSApiStreamCreateResponse  `json:"create_response,omitempty"`
	Restore  *JSApiStreamRestoreResponse `json:"restore_response,omitempty"`
	Update   bool                        `json:"is_update,omitempty"`
}

// Determine if this is an insufficient resources' error type.
func isInsufficientResourcesErr(resp *JSApiStreamCreateResponse) bool {
	return resp != nil && resp.Error != nil && IsNatsErr(resp.Error, JSInsufficientResourcesErr, JSMemoryResourcesExceededErr, JSStorageResourcesExceededErr)
}

// Process error results of stream and consumer assignments.
// Success will be handled by stream leader.
func (js *jetStream) processStreamAssignmentResults(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
	var result streamAssignmentResult
	if err := json.Unmarshal(msg, &result); err != nil {
		// TODO(dlc) - log
		return
	}
	acc, _ := js.srv.LookupAccount(result.Account)
	if acc == nil {
		// TODO(dlc) - log
		return
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	s, cc := js.srv, js.cluster
	if cc == nil || cc.meta == nil {
		return
	}

	// This should have been done already in processStreamAssignment, but in
	// case we have a code path that gets here with no processStreamAssignment,
	// then we will do the proper thing. Otherwise will be a no-op.
	cc.removeInflightProposal(result.Account, result.Stream)

	// FIXME(dlc) - suppress duplicates?
	if sa := js.streamAssignment(result.Account, result.Stream); sa != nil {
		canDelete := !result.Update && time.Since(sa.Created) < 5*time.Second

		// See if we should retry in case this cluster is full but there are others.
		if cfg, ci := sa.Config, sa.Client; cfg != nil && ci != nil && isInsufficientResourcesErr(result.Response) && canDelete {
			// If cluster is defined we can not retry.
			if cfg.Placement == nil || cfg.Placement.Cluster == _EMPTY_ {
				// If we have additional clusters to try we can retry.
				if ci != nil && len(ci.Alternates) > 0 {
					if rg, err := js.createGroupForStream(ci, cfg); err != nil {
						s.Warnf("Retrying cluster placement for stream '%s > %s' failed due to placement error: %+v", result.Account, result.Stream, err)
					} else {
						if org := sa.Group; org != nil && len(org.Peers) > 0 {
							s.Warnf("Retrying cluster placement for stream '%s > %s' due to insufficient resources in cluster %q",
								result.Account, result.Stream, s.clusterNameForNode(org.Peers[0]))
						} else {
							s.Warnf("Retrying cluster placement for stream '%s > %s' due to insufficient resources", result.Account, result.Stream)
						}
						// Pick a new preferred leader.
						rg.setPreferred()
						// Get rid of previous attempt.
						cc.meta.Propose(encodeDeleteStreamAssignment(sa))
						// Propose new.
						sa.Group, sa.err = rg, nil
						cc.meta.Propose(encodeAddStreamAssignment(sa))
						return
					}
				}
			}
		}

		// Respond to the user here.
		var resp string
		if result.Response != nil {
			resp = s.jsonResponse(result.Response)
		} else if result.Restore != nil {
			resp = s.jsonResponse(result.Restore)
		}
		if !sa.responded || result.Update {
			sa.responded = true
			js.srv.sendAPIErrResponse(sa.Client, acc, sa.Subject, sa.Reply, _EMPTY_, resp)
		}
		// Remove this assignment if possible.
		if canDelete {
			sa.err = NewJSClusterNotAssignedError()
			cc.meta.Propose(encodeDeleteStreamAssignment(sa))
		}
	}
}

func (js *jetStream) processConsumerAssignmentResults(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
	var result consumerAssignmentResult
	if err := json.Unmarshal(msg, &result); err != nil {
		// TODO(dlc) - log
		return
	}
	acc, _ := js.srv.LookupAccount(result.Account)
	if acc == nil {
		// TODO(dlc) - log
		return
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	s, cc := js.srv, js.cluster
	if cc == nil || cc.meta == nil {
		return
	}

	if sa := js.streamAssignment(result.Account, result.Stream); sa != nil && sa.consumers != nil {
		if ca := sa.consumers[result.Consumer]; ca != nil && !ca.responded {
			js.srv.sendAPIErrResponse(ca.Client, acc, ca.Subject, ca.Reply, _EMPTY_, s.jsonResponse(result.Response))
			ca.responded = true

			// Check if this failed.
			// TODO(dlc) - Could have mixed results, should track per peer.
			// Make sure this is recent response, do not delete existing consumers.
			if result.Response.Error != nil && result.Response.Error != NewJSConsumerNameExistError() && time.Since(ca.Created) < 2*time.Second {
				// So while we are deleting we will not respond to list/names requests.
				ca.err = NewJSClusterNotAssignedError()
				cc.meta.Propose(encodeDeleteConsumerAssignment(ca))
				s.Warnf("Proposing to delete consumer `%s > %s > %s' due to assignment response error: %v",
					result.Account, result.Stream, result.Consumer, result.Response.Error)
			}
		}
	}
}

const (
	streamAssignmentSubj   = "$SYS.JSC.STREAM.ASSIGNMENT.RESULT"
	consumerAssignmentSubj = "$SYS.JSC.CONSUMER.ASSIGNMENT.RESULT"
)

// Lock should be held.
func (js *jetStream) startUpdatesSub() {
	cc, s, c := js.cluster, js.srv, js.cluster.c
	if cc.streamResults == nil {
		cc.streamResults, _ = s.systemSubscribe(streamAssignmentSubj, _EMPTY_, false, c, js.processStreamAssignmentResults)
	}
	if cc.consumerResults == nil {
		cc.consumerResults, _ = s.systemSubscribe(consumerAssignmentSubj, _EMPTY_, false, c, js.processConsumerAssignmentResults)
	}
	if cc.stepdown == nil {
		cc.stepdown, _ = s.systemSubscribe(JSApiLeaderStepDown, _EMPTY_, false, c, s.jsLeaderStepDownRequest)
	}
	if cc.peerRemove == nil {
		cc.peerRemove, _ = s.systemSubscribe(JSApiRemoveServer, _EMPTY_, false, c, s.jsLeaderServerRemoveRequest)
	}
	if cc.peerStreamMove == nil {
		cc.peerStreamMove, _ = s.systemSubscribe(JSApiServerStreamMove, _EMPTY_, false, c, s.jsLeaderServerStreamMoveRequest)
	}
	if cc.peerStreamCancelMove == nil {
		cc.peerStreamCancelMove, _ = s.systemSubscribe(JSApiServerStreamCancelMove, _EMPTY_, false, c, s.jsLeaderServerStreamCancelMoveRequest)
	}
	if js.accountPurge == nil {
		js.accountPurge, _ = s.systemSubscribe(JSApiAccountPurge, _EMPTY_, false, c, s.jsLeaderAccountPurgeRequest)
	}
}

// Lock should be held.
func (js *jetStream) stopUpdatesSub() {
	cc := js.cluster
	if cc.streamResults != nil {
		cc.s.sysUnsubscribe(cc.streamResults)
		cc.streamResults = nil
	}
	if cc.consumerResults != nil {
		cc.s.sysUnsubscribe(cc.consumerResults)
		cc.consumerResults = nil
	}
	if cc.stepdown != nil {
		cc.s.sysUnsubscribe(cc.stepdown)
		cc.stepdown = nil
	}
	if cc.peerRemove != nil {
		cc.s.sysUnsubscribe(cc.peerRemove)
		cc.peerRemove = nil
	}
	if cc.peerStreamMove != nil {
		cc.s.sysUnsubscribe(cc.peerStreamMove)
		cc.peerStreamMove = nil
	}
	if cc.peerStreamCancelMove != nil {
		cc.s.sysUnsubscribe(cc.peerStreamCancelMove)
		cc.peerStreamCancelMove = nil
	}
	if js.accountPurge != nil {
		cc.s.sysUnsubscribe(js.accountPurge)
		js.accountPurge = nil
	}
}

func (js *jetStream) processLeaderChange(isLeader bool) {
	if isLeader {
		js.srv.Noticef("Self is new JetStream cluster metadata leader")
	} else {
		var node string
		if meta := js.getMetaGroup(); meta != nil {
			node = meta.GroupLeader()
		}
		if node == _EMPTY_ {
			js.srv.Noticef("JetStream cluster no metadata leader")
		} else if srv := js.srv.serverNameForNode(node); srv == _EMPTY_ {
			js.srv.Noticef("JetStream cluster new remote metadata leader")
		} else if clst := js.srv.clusterNameForNode(node); clst == _EMPTY_ {
			js.srv.Noticef("JetStream cluster new metadata leader: %s", srv)
		} else {
			js.srv.Noticef("JetStream cluster new metadata leader: %s/%s", srv, clst)
		}
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	if isLeader {
		js.startUpdatesSub()
	} else {
		js.stopUpdatesSub()
		// TODO(dlc) - stepdown.
	}

	// If we have been signaled to check the streams, this is for a bug that left stream
	// assignments with no sync subject after an update and no way to sync/catchup outside of the RAFT layer.
	if isLeader && js.cluster.streamsCheck {
		cc := js.cluster
		for acc, asa := range cc.streams {
			for _, sa := range asa {
				if sa.Sync == _EMPTY_ {
					js.srv.Warnf("Stream assigment corrupt for stream '%s > %s'", acc, sa.Config.Name)
					nsa := &streamAssignment{Group: sa.Group, Config: sa.Config, Subject: sa.Subject, Reply: sa.Reply, Client: sa.Client}
					nsa.Sync = syncSubjForStream()
					cc.meta.Propose(encodeUpdateStreamAssignment(nsa))
				}
			}
		}
		// Clear check.
		cc.streamsCheck = false
	}
}

// Lock should be held.
func (cc *jetStreamCluster) remapStreamAssignment(sa *streamAssignment, removePeer string) bool {
	// Invoke placement algo passing RG peers that stay (existing) and the peer that is being removed (ignore)
	var retain, ignore []string
	for _, v := range sa.Group.Peers {
		if v == removePeer {
			ignore = append(ignore, v)
		} else {
			retain = append(retain, v)
		}
	}

	newPeers, placementError := cc.selectPeerGroup(len(sa.Group.Peers), sa.Group.Cluster, sa.Config, retain, 0, ignore)

	if placementError == nil {
		sa.Group.Peers = newPeers
		// Don't influence preferred leader.
		sa.Group.Preferred = _EMPTY_
		return true
	}

	// If we are here let's remove the peer at least.
	for i, peer := range sa.Group.Peers {
		if peer == removePeer {
			sa.Group.Peers[i] = sa.Group.Peers[len(sa.Group.Peers)-1]
			sa.Group.Peers = sa.Group.Peers[:len(sa.Group.Peers)-1]
			break
		}
	}
	return false
}

type selectPeerError struct {
	excludeTag  bool
	offline     bool
	noStorage   bool
	uniqueTag   bool
	misc        bool
	noJsClust   bool
	noMatchTags map[string]struct{}
}

func (e *selectPeerError) Error() string {
	b := strings.Builder{}
	writeBoolErrReason := func(hasErr bool, errMsg string) {
		if !hasErr {
			return
		}
		b.WriteString(", ")
		b.WriteString(errMsg)
	}
	b.WriteString("no suitable peers for placement")
	writeBoolErrReason(e.offline, "peer offline")
	writeBoolErrReason(e.excludeTag, "exclude tag set")
	writeBoolErrReason(e.noStorage, "insufficient storage")
	writeBoolErrReason(e.uniqueTag, "server tag not unique")
	writeBoolErrReason(e.misc, "miscellaneous issue")
	writeBoolErrReason(e.noJsClust, "jetstream not enabled in cluster")
	if len(e.noMatchTags) != 0 {
		b.WriteString(", tags not matched [")
		var firstTagWritten bool
		for tag := range e.noMatchTags {
			if firstTagWritten {
				b.WriteString(", ")
			}
			firstTagWritten = true
			b.WriteRune('\'')
			b.WriteString(tag)
			b.WriteRune('\'')
		}
		b.WriteString("]")
	}
	return b.String()
}

func (e *selectPeerError) addMissingTag(t string) {
	if e.noMatchTags == nil {
		e.noMatchTags = map[string]struct{}{}
	}
	e.noMatchTags[t] = struct{}{}
}

func (e *selectPeerError) accumulate(eAdd *selectPeerError) {
	if eAdd == nil {
		return
	}
	acc := func(val *bool, valAdd bool) {
		if valAdd {
			*val = valAdd
		}
	}
	acc(&e.offline, eAdd.offline)
	acc(&e.excludeTag, eAdd.excludeTag)
	acc(&e.noStorage, eAdd.noStorage)
	acc(&e.uniqueTag, eAdd.uniqueTag)
	acc(&e.misc, eAdd.misc)
	acc(&e.noJsClust, eAdd.noJsClust)
	for tag := range eAdd.noMatchTags {
		e.addMissingTag(tag)
	}
}

// selectPeerGroup will select a group of peers to start a raft group.
// when peers exist already the unique tag prefix check for the replaceFirstExisting will be skipped
func (cc *jetStreamCluster) selectPeerGroup(r int, cluster string, cfg *StreamConfig, existing []string, replaceFirstExisting int, ignore []string) ([]string, *selectPeerError) {
	if cluster == _EMPTY_ || cfg == nil {
		return nil, &selectPeerError{misc: true}
	}

	var maxBytes uint64
	if cfg.MaxBytes > 0 {
		maxBytes = uint64(cfg.MaxBytes)
	}

	// Check for tags.
	var tags []string
	if cfg.Placement != nil && len(cfg.Placement.Tags) > 0 {
		tags = cfg.Placement.Tags
	}

	// Used for weighted sorting based on availability.
	type wn struct {
		id    string
		avail uint64
		ha    int
	}

	var nodes []wn
	// peers is a randomized list
	s, peers := cc.s, cc.meta.Peers()

	uniqueTagPrefix := s.getOpts().JetStreamUniqueTag
	if uniqueTagPrefix != _EMPTY_ {
		for _, tag := range tags {
			if strings.HasPrefix(tag, uniqueTagPrefix) {
				// disable uniqueness check if explicitly listed in tags
				uniqueTagPrefix = _EMPTY_
				break
			}
		}
	}
	var uniqueTags = make(map[string]*nodeInfo)

	checkUniqueTag := func(ni *nodeInfo) (bool, *nodeInfo) {
		for _, t := range ni.tags {
			if strings.HasPrefix(t, uniqueTagPrefix) {
				if n, ok := uniqueTags[t]; !ok {
					uniqueTags[t] = ni
					return true, ni
				} else {
					return false, n
				}
			}
		}
		// default requires the unique prefix to be present
		return false, nil
	}

	// Map existing.
	var ep map[string]struct{}
	if le := len(existing); le > 0 {
		if le >= r {
			return existing[:r], nil
		}
		ep = make(map[string]struct{})
		for i, p := range existing {
			ep[p] = struct{}{}
			if uniqueTagPrefix == _EMPTY_ {
				continue
			}
			si, ok := s.nodeToInfo.Load(p)
			if !ok || si == nil || i < replaceFirstExisting {
				continue
			}
			ni := si.(nodeInfo)
			// collect unique tags, but do not require them as this node is already part of the peerset
			checkUniqueTag(&ni)
		}
	}

	// Map ignore
	var ip map[string]struct{}
	if li := len(ignore); li > 0 {
		ip = make(map[string]struct{})
		for _, p := range ignore {
			ip[p] = struct{}{}
		}
	}

	maxHaAssets := s.getOpts().JetStreamLimits.MaxHAAssets

	// An error is a result of multiple individual placement decisions.
	// Which is why we keep taps on how often which one happened.
	err := selectPeerError{}

	// Shuffle them up.
	rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	for _, p := range peers {
		si, ok := s.nodeToInfo.Load(p.ID)
		if !ok || si == nil {
			err.misc = true
			continue
		}
		ni := si.(nodeInfo)
		// Only select from the designated named cluster.
		if ni.cluster != cluster {
			s.Debugf("Peer selection: discard %s@%s reason: not target cluster %s", ni.name, ni.cluster, cluster)
			continue
		}

		// If we know its offline or we do not have config or err don't consider.
		if ni.offline || ni.cfg == nil || ni.stats == nil {
			s.Debugf("Peer selection: discard %s@%s reason: offline", ni.name, ni.cluster)
			err.offline = true
			continue
		}

		// If ignore skip
		if _, ok := ip[p.ID]; ok {
			continue
		}

		// If existing also skip, we will add back in to front of the list when done.
		if _, ok := ep[p.ID]; ok {
			continue
		}

		if ni.tags.Contains(jsExcludePlacement) {
			s.Debugf("Peer selection: discard %s@%s tags: %v reason: %s present",
				ni.name, ni.cluster, ni.tags, jsExcludePlacement)
			err.excludeTag = true
			continue
		}

		if len(tags) > 0 {
			matched := true
			for _, t := range tags {
				if !ni.tags.Contains(t) {
					matched = false
					s.Debugf("Peer selection: discard %s@%s tags: %v reason: mandatory tag %s not present",
						ni.name, ni.cluster, ni.tags, t)
					err.addMissingTag(t)
					break
				}
			}
			if !matched {
				continue
			}
		}

		var available uint64
		var ha int
		if ni.stats != nil {
			switch cfg.Storage {
			case MemoryStorage:
				used := ni.stats.ReservedMemory
				if ni.stats.Memory > used {
					used = ni.stats.Memory
				}
				if ni.cfg.MaxMemory > int64(used) {
					available = uint64(ni.cfg.MaxMemory) - used
				}
			case FileStorage:
				used := ni.stats.ReservedStore
				if ni.stats.Store > used {
					used = ni.stats.Store
				}
				if ni.cfg.MaxStore > int64(used) {
					available = uint64(ni.cfg.MaxStore) - used
				}
			}
			ha = ni.stats.HAAssets
		}

		// Otherwise check if we have enough room if maxBytes set.
		if maxBytes > 0 && maxBytes > available {
			s.Warnf("Peer selection: discard %s@%s (Max Bytes: %d) exceeds available %s storage of %d bytes",
				ni.name, ni.cluster, maxBytes, cfg.Storage.String(), available)
			err.noStorage = true
			continue
		}
		// HAAssets contain _meta_ which we want to ignore, hence > and not >=.
		if maxHaAssets > 0 && ni.stats != nil && ni.stats.HAAssets > maxHaAssets {
			s.Warnf("Peer selection: discard %s@%s (HA Asset Count: %d) exceeds max ha asset limit of %d for stream placement",
				ni.name, ni.cluster, ni.stats.HAAssets, maxHaAssets)
			err.misc = true
			continue
		}

		if uniqueTagPrefix != _EMPTY_ {
			if unique, owner := checkUniqueTag(&ni); !unique {
				if owner != nil {
					s.Debugf("Peer selection: discard %s@%s tags:%v reason: unique prefix %s owned by %s@%s",
						ni.name, ni.cluster, ni.tags, owner.name, owner.cluster)
				} else {
					s.Debugf("Peer selection: discard %s@%s tags:%v reason: unique prefix %s not present",
						ni.name, ni.cluster, ni.tags)
				}
				err.uniqueTag = true
				continue
			}
		}
		// Add to our list of potential nodes.
		nodes = append(nodes, wn{p.ID, available, ha})
	}

	// If we could not select enough peers, fail.
	if len(nodes) < (r - len(existing)) {
		s.Debugf("Peer selection: required %d nodes but found %d (cluster: %s replica: %d existing: %v/%d peers: %d result-peers: %d err: %+v)",
			(r - len(existing)), len(nodes), cluster, r, existing, replaceFirstExisting, len(peers), len(nodes), err)
		if len(peers) == 0 {
			err.noJsClust = true
		}
		return nil, &err
	}
	// Sort based on available from most to least.
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].avail > nodes[j].avail })

	// If we are placing a replicated stream, let's sort based in haAssets, as that is more important to balance.
	if cfg.Replicas > 1 {
		sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ha < nodes[j].ha })
	}

	var results []string
	if len(existing) > 0 {
		results = append(results, existing...)
		r -= len(existing)
	}
	for _, r := range nodes[:r] {
		results = append(results, r.id)
	}
	return results, nil
}

func groupNameForStream(peers []string, storage StorageType) string {
	return groupName("S", peers, storage)
}

func groupNameForConsumer(peers []string, storage StorageType) string {
	return groupName("C", peers, storage)
}

func groupName(prefix string, peers []string, storage StorageType) string {
	gns := getHash(nuid.Next())
	return fmt.Sprintf("%s-R%d%s-%s", prefix, len(peers), storage.String()[:1], gns)
}

// returns stream count for this tier as well as applicable reservation size (not including reservations for cfg)
// jetStream read lock should be held
func tieredStreamAndReservationCount(asa map[string]*streamAssignment, tier string, cfg *StreamConfig) (int, int64) {
	numStreams := len(asa)
	reservation := int64(0)
	if tier == _EMPTY_ {
		for _, sa := range asa {
			if sa.Config.MaxBytes > 0 && sa.Config.Name != cfg.Name {
				if sa.Config.Storage == cfg.Storage {
					reservation += (int64(sa.Config.Replicas) * sa.Config.MaxBytes)
				}
			}
		}
	} else {
		numStreams = 0
		for _, sa := range asa {
			if isSameTier(sa.Config, cfg) {
				numStreams++
				if sa.Config.MaxBytes > 0 {
					if sa.Config.Storage == cfg.Storage && sa.Config.Name != cfg.Name {
						reservation += (int64(sa.Config.Replicas) * sa.Config.MaxBytes)
					}
				}
			}
		}
	}
	return numStreams, reservation
}

// createGroupForStream will create a group for assignment for the stream.
// Lock should be held.
func (js *jetStream) createGroupForStream(ci *ClientInfo, cfg *StreamConfig) (*raftGroup, *selectPeerError) {
	replicas := cfg.Replicas
	if replicas == 0 {
		replicas = 1
	}

	// Default connected cluster from the request origin.
	cc, cluster := js.cluster, ci.Cluster
	// If specified, override the default.
	clusterDefined := cfg.Placement != nil && cfg.Placement.Cluster != _EMPTY_
	if clusterDefined {
		cluster = cfg.Placement.Cluster
	}
	clusters := []string{cluster}
	if !clusterDefined {
		clusters = append(clusters, ci.Alternates...)
	}

	// Need to create a group here.
	errs := &selectPeerError{}
	for _, cn := range clusters {
		peers, err := cc.selectPeerGroup(replicas, cn, cfg, nil, 0, nil)
		if len(peers) < replicas {
			errs.accumulate(err)
			continue
		}
		return &raftGroup{Name: groupNameForStream(peers, cfg.Storage), Storage: cfg.Storage, Peers: peers, Cluster: cn}, nil
	}
	return nil, errs
}

func (acc *Account) selectLimits(cfg *StreamConfig) (*JetStreamAccountLimits, string, *jsAccount, *ApiError) {
	// Grab our jetstream account info.
	acc.mu.RLock()
	jsa := acc.js
	acc.mu.RUnlock()

	if jsa == nil {
		return nil, _EMPTY_, nil, NewJSNotEnabledForAccountError()
	}

	jsa.usageMu.RLock()
	selectedLimits, tierName, ok := jsa.selectLimits(cfg)
	jsa.usageMu.RUnlock()

	if !ok {
		return nil, _EMPTY_, nil, NewJSNoLimitsError()
	}
	return &selectedLimits, tierName, jsa, nil
}

// Read lock needs to be held
func (js *jetStream) jsClusteredStreamLimitsCheck(acc *Account, cfg *StreamConfig) *ApiError {
	selectedLimits, tier, _, apiErr := acc.selectLimits(cfg)
	if apiErr != nil {
		return apiErr
	}

	asa := js.cluster.streams[acc.Name]
	numStreams, reservations := tieredStreamAndReservationCount(asa, tier, cfg)
	// Check for inflight proposals...
	if cc := js.cluster; cc != nil && cc.inflight != nil {
		numStreams += len(cc.inflight[acc.Name])
	}
	if selectedLimits.MaxStreams > 0 && numStreams >= selectedLimits.MaxStreams {
		return NewJSMaximumStreamsLimitError()
	}
	// Check for account limits here before proposing.
	if err := js.checkAccountLimits(selectedLimits, cfg, reservations); err != nil {
		return NewJSStreamLimitsError(err, Unless(err))
	}
	return nil
}

func (s *Server) jsClusteredStreamRequest(ci *ClientInfo, acc *Account, subject, reply string, rmsg []byte, config *StreamConfig) {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	var resp = JSApiStreamCreateResponse{ApiResponse: ApiResponse{Type: JSApiStreamCreateResponseType}}

	ccfg, apiErr := s.checkStreamCfg(config, acc)
	if apiErr != nil {
		resp.Error = apiErr
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	cfg := &ccfg

	// Now process the request and proposal.
	js.mu.Lock()
	defer js.mu.Unlock()

	// Capture if we have existing assignment first.
	osa := js.streamAssignment(acc.Name, cfg.Name)
	var areEqual bool
	if osa != nil {
		areEqual = reflect.DeepEqual(osa.Config, cfg)
	}

	// If this stream already exists, turn this into a stream info call.
	if osa != nil {
		// If they are the same then we will forward on as a stream info request.
		// This now matches single server behavior.
		if areEqual {
			// This works when we have a stream leader. If we have no leader let the dupe
			// go through as normal. We will handle properly on the other end.
			// We must check interest at the $SYS account layer, not user account since import
			// will always show interest.
			sisubj := fmt.Sprintf(clusterStreamInfoT, acc.Name, cfg.Name)
			if s.SystemAccount().Interest(sisubj) > 0 {
				isubj := fmt.Sprintf(JSApiStreamInfoT, cfg.Name)
				// We want to make sure we send along the client info.
				cij, _ := json.Marshal(ci)
				hdr := map[string]string{
					ClientInfoHdr:  string(cij),
					JSResponseType: jsCreateResponse,
				}
				// Send this as system account, but include client info header.
				s.sendInternalAccountMsgWithReply(nil, isubj, reply, hdr, nil, true)
				return
			}
		} else {
			resp.Error = NewJSStreamNameExistError()
			s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
			return
		}
	}

	if cfg.Sealed {
		resp.Error = NewJSStreamInvalidConfigError(fmt.Errorf("stream configuration for create can not be sealed"))
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	var self *streamAssignment
	if osa != nil && areEqual {
		self = osa
	}

	// Check for subject collisions here.
	if cc.subjectsOverlap(acc.Name, cfg.Subjects, self) {
		resp.Error = NewJSStreamSubjectOverlapError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	apiErr = js.jsClusteredStreamLimitsCheck(acc, cfg)
	// Check for stream limits here before proposing. These need to be tracked from meta layer, not jsa.
	if apiErr != nil {
		resp.Error = apiErr
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	// Raft group selection and placement.
	var rg *raftGroup
	if osa != nil && areEqual {
		rg = osa.Group
	} else {
		// Check inflight before proposing in case we have an existing inflight proposal.
		if cc.inflight == nil {
			cc.inflight = make(map[string]map[string]*raftGroup)
		}
		streams, ok := cc.inflight[acc.Name]
		if !ok {
			streams = make(map[string]*raftGroup)
			cc.inflight[acc.Name] = streams
		} else if existing, ok := streams[cfg.Name]; ok {
			// We have existing for same stream. Re-use same group.
			rg = existing
		}
	}
	// Create a new one here.
	if rg == nil {
		nrg, err := js.createGroupForStream(ci, cfg)
		if err != nil {
			resp.Error = NewJSClusterNoPeersError(err)
			s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
			return
		}
		rg = nrg
		// Pick a preferred leader.
		rg.setPreferred()
	}

	// Sync subject for post snapshot sync.
	sa := &streamAssignment{Group: rg, Sync: syncSubjForStream(), Config: cfg, Subject: subject, Reply: reply, Client: ci, Created: time.Now().UTC()}
	if err := cc.meta.Propose(encodeAddStreamAssignment(sa)); err == nil {
		// On success, add this as an inflight proposal so we can apply limits
		// on concurrent create requests while this stream assignment has
		// possibly not been processed yet.
		if streams, ok := cc.inflight[acc.Name]; ok {
			streams[cfg.Name] = rg
		}
	}
}

var (
	errReqTimeout = errors.New("timeout while waiting for response")
	errReqSrvExit = errors.New("server shutdown while waiting for response")
)

// blocking utility call to perform requests on the system account
// returns (synchronized) v or error
func sysRequest[T any](s *Server, subjFormat string, args ...interface{}) (*T, error) {
	isubj := fmt.Sprintf(subjFormat, args...)

	s.mu.Lock()
	inbox := s.newRespInbox()
	results := make(chan *T, 1)
	s.sys.replies[inbox] = func(_ *subscription, _ *client, _ *Account, _, _ string, msg []byte) {
		var v T
		if err := json.Unmarshal(msg, &v); err != nil {
			s.Warnf("Error unmarshalling response for request '%s':%v", isubj, err)
			return
		}
		select {
		case results <- &v:
		default:
			s.Warnf("Failed placing request response on internal channel")
		}
	}
	s.mu.Unlock()

	s.sendInternalMsgLocked(isubj, inbox, nil, nil)

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.sys != nil && s.sys.replies != nil {
			delete(s.sys.replies, inbox)
		}
	}()

	select {
	case <-s.quitCh:
		return nil, errReqSrvExit
	case <-time.After(2 * time.Second):
		return nil, errReqTimeout
	case data := <-results:
		return data, nil
	}
}

func (s *Server) jsClusteredStreamUpdateRequest(ci *ClientInfo, acc *Account, subject, reply string, rmsg []byte, cfg *StreamConfig, peerSet []string) {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	// Now process the request and proposal.
	js.mu.Lock()
	defer js.mu.Unlock()
	meta := cc.meta
	if meta == nil {
		return
	}

	var resp = JSApiStreamUpdateResponse{ApiResponse: ApiResponse{Type: JSApiStreamUpdateResponseType}}

	osa := js.streamAssignment(acc.Name, cfg.Name)

	if osa == nil {
		resp.Error = NewJSStreamNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	var newCfg *StreamConfig
	if jsa := js.accounts[acc.Name]; jsa != nil {
		js.mu.Unlock()
		ncfg, err := jsa.configUpdateCheck(osa.Config, cfg, s)
		js.mu.Lock()
		if err != nil {
			resp.Error = NewJSStreamUpdateError(err, Unless(err))
			s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
			return
		} else {
			newCfg = ncfg
		}
	} else {
		resp.Error = NewJSNotEnabledForAccountError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	// Check for mirror changes which are not allowed.
	if !reflect.DeepEqual(newCfg.Mirror, osa.Config.Mirror) {
		resp.Error = NewJSStreamMirrorNotUpdatableError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	// Check for subject collisions here.
	if cc.subjectsOverlap(acc.Name, cfg.Subjects, osa) {
		resp.Error = NewJSStreamSubjectOverlapError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	// Make copy so to not change original.
	rg := osa.copyGroup().Group

	// Check for a move request.
	var isMoveRequest, isMoveCancel bool
	if lPeerSet := len(peerSet); lPeerSet > 0 {
		isMoveRequest = true
		// check if this is a cancellation
		if lPeerSet == osa.Config.Replicas && lPeerSet <= len(rg.Peers) {
			isMoveCancel = true
			// can only be a cancellation if the peer sets overlap as expected
			for i := 0; i < lPeerSet; i++ {
				if peerSet[i] != rg.Peers[i] {
					isMoveCancel = false
					break
				}
			}
		}
	} else {
		isMoveRequest = newCfg.Placement != nil && !reflect.DeepEqual(osa.Config.Placement, newCfg.Placement)
	}

	// Check for replica changes.
	isReplicaChange := newCfg.Replicas != osa.Config.Replicas

	// We stage consumer updates and do them after the stream update.
	var consumers []*consumerAssignment

	// Check if this is a move request, but no cancellation, and we are already moving this stream.
	if isMoveRequest && !isMoveCancel && osa.Config.Replicas != len(rg.Peers) {
		// obtain stats to include in error message
		msg := _EMPTY_
		if s.allPeersOffline(rg) {
			msg = fmt.Sprintf("all %d peers offline", len(rg.Peers))
		} else {
			// Need to release js lock.
			js.mu.Unlock()
			if si, err := sysRequest[StreamInfo](s, clusterStreamInfoT, ci.serviceAccount(), cfg.Name); err != nil {
				msg = fmt.Sprintf("error retrieving info: %s", err.Error())
			} else if si != nil {
				currentCount := 0
				if si.Cluster.Leader != _EMPTY_ {
					currentCount++
				}
				combinedLag := uint64(0)
				for _, r := range si.Cluster.Replicas {
					if r.Current {
						currentCount++
					}
					combinedLag += r.Lag
				}
				msg = fmt.Sprintf("total peers: %d, current peers: %d, combined lag: %d",
					len(rg.Peers), currentCount, combinedLag)
			}
			// Re-acquire here.
			js.mu.Lock()
		}
		resp.Error = NewJSStreamMoveInProgressError(msg)
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	// Can not move and scale at same time.
	if isMoveRequest && isReplicaChange {
		resp.Error = NewJSStreamMoveAndScaleError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	if isReplicaChange {
		// We are adding new peers here.
		if newCfg.Replicas > len(rg.Peers) {
			peers, err := cc.selectPeerGroup(newCfg.Replicas, rg.Cluster, newCfg, rg.Peers, 0, nil)
			if err != nil {
				resp.Error = NewJSClusterNoPeersError(err)
				s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
				return
			}
			// Single nodes are not recorded by the NRG layer so we can rename.
			if len(peers) == 1 {
				rg.Name = groupNameForStream(peers, rg.Storage)
			} else if len(rg.Peers) == 1 {
				// This is scale up from being a singelton, set preferred to that singelton.
				rg.Preferred = rg.Peers[0]
			}
			rg.Peers = peers
		} else {
			// We are deleting nodes here. We want to do our best to preserve the current leader.
			// We have support now from above that guarantees we are in our own Go routine, so can
			// ask for stream info from the stream leader to make sure we keep the leader in the new list.
			var curLeader string
			if !s.allPeersOffline(rg) {
				// Need to release js lock.
				js.mu.Unlock()
				if si, err := sysRequest[StreamInfo](s, clusterStreamInfoT, ci.serviceAccount(), cfg.Name); err != nil {
					s.Warnf("Did not receive stream info results for '%s > %s' due to: %s", acc, cfg.Name, err)
				} else if si != nil {
					if cl := si.Cluster; cl != nil && cl.Leader != _EMPTY_ {
						curLeader = getHash(cl.Leader)
					}
				}
				// Re-acquire here.
				js.mu.Lock()
			}
			// If we identified a leader make sure its part of the new group.
			selected := make([]string, 0, newCfg.Replicas)

			if curLeader != _EMPTY_ {
				selected = append(selected, curLeader)
			}
			for _, peer := range rg.Peers {
				if len(selected) == newCfg.Replicas {
					break
				}
				if peer == curLeader {
					continue
				}
				if si, ok := s.nodeToInfo.Load(peer); ok && si != nil {
					if si.(nodeInfo).offline {
						continue
					}
					selected = append(selected, peer)
				}
			}
			rg.Peers = selected
		}

		// Need to remap any consumers.
		for _, ca := range osa.consumers {
			// Ephemerals are R=1, so only auto-remap durables, or R>1, unless stream is interest or workqueue policy.
			numPeers := len(ca.Group.Peers)
			if ca.Config.Durable != _EMPTY_ || numPeers > 1 || cfg.Retention != LimitsPolicy {
				cca := ca.copyGroup()
				// Adjust preferred as needed.
				if numPeers == 1 && len(rg.Peers) > 1 {
					cca.Group.Preferred = ca.Group.Peers[0]
				} else {
					cca.Group.Preferred = _EMPTY_
				}
				// Assign new peers.
				cca.Group.Peers = rg.Peers
				// We can not propose here before the stream itself so we collect them.
				consumers = append(consumers, cca)
			}
		}
	} else if isMoveRequest {
		if len(peerSet) == 0 {
			nrg, err := js.createGroupForStream(ci, newCfg)
			if err != nil {
				resp.Error = NewJSClusterNoPeersError(err)
				s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
				return
			}
			// filter peers present in both sets
			for _, peer := range rg.Peers {
				found := false
				for _, newPeer := range nrg.Peers {
					if peer == newPeer {
						found = true
						break
					}
				}
				if !found {
					peerSet = append(peerSet, peer)
				}
			}
			peerSet = append(peerSet, nrg.Peers...)
		}
		if len(rg.Peers) == 1 {
			rg.Preferred = peerSet[0]
		}
		rg.Peers = peerSet

		for _, ca := range osa.consumers {
			cca := ca.copyGroup()
			r := cca.Config.replicas(osa.Config)
			// shuffle part of cluster peer set we will be keeping
			randPeerSet := copyStrings(peerSet[len(peerSet)-newCfg.Replicas:])
			rand.Shuffle(newCfg.Replicas, func(i, j int) { randPeerSet[i], randPeerSet[j] = randPeerSet[j], randPeerSet[i] })
			// move overlapping peers at the end of randPeerSet and keep a tally of non overlapping peers
			dropPeerSet := make([]string, 0, len(cca.Group.Peers))
			for _, p := range cca.Group.Peers {
				found := false
				for i, rp := range randPeerSet {
					if p == rp {
						randPeerSet[i] = randPeerSet[newCfg.Replicas-1]
						randPeerSet[newCfg.Replicas-1] = p
						found = true
						break
					}
				}
				if !found {
					dropPeerSet = append(dropPeerSet, p)
				}
			}
			cPeerSet := randPeerSet[newCfg.Replicas-r:]
			// In case of a set or cancel simply assign
			if len(peerSet) == newCfg.Replicas {
				cca.Group.Peers = cPeerSet
			} else {
				cca.Group.Peers = append(dropPeerSet, cPeerSet...)
			}
			// make sure it overlaps with peers and remove if not
			if cca.Group.Preferred != _EMPTY_ {
				found := false
				for _, p := range cca.Group.Peers {
					if p == cca.Group.Preferred {
						found = true
						break
					}
				}
				if !found {
					cca.Group.Preferred = _EMPTY_
				}
			}
			// We can not propose here before the stream itself so we collect them.
			consumers = append(consumers, cca)
		}
	} else {
		// All other updates make sure no preferred is set.
		rg.Preferred = _EMPTY_
	}

	sa := &streamAssignment{Group: rg, Sync: osa.Sync, Created: osa.Created, Config: newCfg, Subject: subject, Reply: reply, Client: ci}
	meta.Propose(encodeUpdateStreamAssignment(sa))

	// Process any staged consumers.
	for _, ca := range consumers {
		meta.Propose(encodeAddConsumerAssignment(ca))
	}
}

func (s *Server) jsClusteredStreamDeleteRequest(ci *ClientInfo, acc *Account, stream, subject, reply string, rmsg []byte) {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	if cc.meta == nil {
		return
	}

	osa := js.streamAssignment(acc.Name, stream)
	if osa == nil {
		var resp = JSApiStreamDeleteResponse{ApiResponse: ApiResponse{Type: JSApiStreamDeleteResponseType}}
		resp.Error = NewJSStreamNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	sa := &streamAssignment{Group: osa.Group, Config: osa.Config, Subject: subject, Reply: reply, Client: ci}
	cc.meta.Propose(encodeDeleteStreamAssignment(sa))
}

// Process a clustered purge request.
func (s *Server) jsClusteredStreamPurgeRequest(
	ci *ClientInfo,
	acc *Account,
	mset *stream,
	stream, subject, reply string,
	rmsg []byte,
	preq *JSApiStreamPurgeRequest,
) {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	js.mu.Lock()
	sa := js.streamAssignment(acc.Name, stream)
	if sa == nil {
		resp := JSApiStreamPurgeResponse{ApiResponse: ApiResponse{Type: JSApiStreamPurgeResponseType}}
		resp.Error = NewJSStreamNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		js.mu.Unlock()
		return
	}

	if n := sa.Group.node; n != nil {
		sp := &streamPurge{Stream: stream, LastSeq: mset.state().LastSeq, Subject: subject, Reply: reply, Client: ci, Request: preq}
		n.Propose(encodeStreamPurge(sp))
		js.mu.Unlock()
		return
	}
	js.mu.Unlock()

	if mset == nil {
		return
	}

	var resp = JSApiStreamPurgeResponse{ApiResponse: ApiResponse{Type: JSApiStreamPurgeResponseType}}
	purged, err := mset.purge(preq)
	if err != nil {
		resp.Error = NewJSStreamGeneralError(err, Unless(err))
	} else {
		resp.Purged = purged
		resp.Success = true
	}
	s.sendAPIResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(resp))
}

func (s *Server) jsClusteredStreamRestoreRequest(
	ci *ClientInfo,
	acc *Account,
	req *JSApiStreamRestoreRequest,
	stream, subject, reply string, rmsg []byte) {

	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	if cc.meta == nil {
		return
	}

	cfg := &req.Config
	resp := JSApiStreamRestoreResponse{ApiResponse: ApiResponse{Type: JSApiStreamRestoreResponseType}}

	if err := js.jsClusteredStreamLimitsCheck(acc, cfg); err != nil {
		resp.Error = err
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	if sa := js.streamAssignment(ci.serviceAccount(), cfg.Name); sa != nil {
		resp.Error = NewJSStreamNameExistRestoreFailedError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	// Raft group selection and placement.
	rg, err := js.createGroupForStream(ci, cfg)
	if err != nil {
		resp.Error = NewJSClusterNoPeersError(err)
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	// Pick a preferred leader.
	rg.setPreferred()
	sa := &streamAssignment{Group: rg, Sync: syncSubjForStream(), Config: cfg, Subject: subject, Reply: reply, Client: ci, Created: time.Now().UTC()}
	// Now add in our restore state and pre-select a peer to handle the actual receipt of the snapshot.
	sa.Restore = &req.State
	cc.meta.Propose(encodeAddStreamAssignment(sa))
}

// Determine if all peers for this group are offline.
func (s *Server) allPeersOffline(rg *raftGroup) bool {
	if rg == nil {
		return false
	}
	// Check to see if this stream has any servers online to respond.
	for _, peer := range rg.Peers {
		if si, ok := s.nodeToInfo.Load(peer); ok && si != nil {
			if !si.(nodeInfo).offline {
				return false
			}
		}
	}
	return true
}

// This will do a scatter and gather operation for all streams for this account. This is only called from metadata leader.
// This will be running in a separate Go routine.
func (s *Server) jsClusteredStreamListRequest(acc *Account, ci *ClientInfo, filter string, offset int, subject, reply string, rmsg []byte) {
	defer s.grWG.Done()

	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	js.mu.RLock()

	var streams []*streamAssignment
	for _, sa := range cc.streams[acc.Name] {
		if IsNatsErr(sa.err, JSClusterNotAssignedErr) {
			continue
		}

		if filter != _EMPTY_ {
			// These could not have subjects auto-filled in since they are raw and unprocessed.
			if len(sa.Config.Subjects) == 0 {
				if SubjectsCollide(filter, sa.Config.Name) {
					streams = append(streams, sa)
				}
			} else {
				for _, subj := range sa.Config.Subjects {
					if SubjectsCollide(filter, subj) {
						streams = append(streams, sa)
						break
					}
				}
			}
		} else {
			streams = append(streams, sa)
		}
	}

	// Needs to be sorted for offsets etc.
	if len(streams) > 1 {
		sort.Slice(streams, func(i, j int) bool {
			return strings.Compare(streams[i].Config.Name, streams[j].Config.Name) < 0
		})
	}

	scnt := len(streams)
	if offset > scnt {
		offset = scnt
	}
	if offset > 0 {
		streams = streams[offset:]
	}
	if len(streams) > JSApiListLimit {
		streams = streams[:JSApiListLimit]
	}

	var resp = JSApiStreamListResponse{
		ApiResponse: ApiResponse{Type: JSApiStreamListResponseType},
		Streams:     make([]*StreamInfo, 0, len(streams)),
	}

	js.mu.RUnlock()

	if len(streams) == 0 {
		resp.Limit = JSApiListLimit
		resp.Offset = offset
		s.sendAPIResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(resp))
		return
	}

	// Create an inbox for our responses and send out our requests.
	s.mu.Lock()
	inbox := s.newRespInbox()
	rc := make(chan *StreamInfo, len(streams))

	// Store our handler.
	s.sys.replies[inbox] = func(sub *subscription, _ *client, _ *Account, subject, _ string, msg []byte) {
		var si StreamInfo
		if err := json.Unmarshal(msg, &si); err != nil {
			s.Warnf("Error unmarshaling clustered stream info response:%v", err)
			return
		}
		select {
		case rc <- &si:
		default:
			s.Warnf("Failed placing remote stream info result on internal channel")
		}
	}
	s.mu.Unlock()

	// Cleanup after.
	defer func() {
		s.mu.Lock()
		if s.sys != nil && s.sys.replies != nil {
			delete(s.sys.replies, inbox)
		}
		s.mu.Unlock()
	}()

	var missingNames []string
	sent := map[string]int{}

	// Send out our requests here.
	js.mu.RLock()
	for _, sa := range streams {
		if s.allPeersOffline(sa.Group) {
			// Place offline onto our results by hand here.
			si := &StreamInfo{Config: *sa.Config, Created: sa.Created, Cluster: js.offlineClusterInfo(sa.Group)}
			resp.Streams = append(resp.Streams, si)
			missingNames = append(missingNames, sa.Config.Name)
		} else {
			isubj := fmt.Sprintf(clusterStreamInfoT, sa.Client.serviceAccount(), sa.Config.Name)
			s.sendInternalMsgLocked(isubj, inbox, nil, nil)
			sent[sa.Config.Name] = len(sa.consumers)
		}
	}
	// Don't hold lock.
	js.mu.RUnlock()

	const timeout = 4 * time.Second
	notActive := time.NewTimer(timeout)
	defer notActive.Stop()

LOOP:
	for len(sent) > 0 {
		select {
		case <-s.quitCh:
			return
		case <-notActive.C:
			s.Warnf("Did not receive all stream info results for %q", acc)
			for sName := range sent {
				missingNames = append(missingNames, sName)
			}
			break LOOP
		case si := <-rc:
			consCount := sent[si.Config.Name]
			if consCount > 0 {
				si.State.Consumers = consCount
			}
			delete(sent, si.Config.Name)
			resp.Streams = append(resp.Streams, si)
			// Check to see if we are done.
			if len(resp.Streams) == len(streams) {
				break LOOP
			}
		}
	}

	// Needs to be sorted as well.
	if len(resp.Streams) > 1 {
		sort.Slice(resp.Streams, func(i, j int) bool {
			return strings.Compare(resp.Streams[i].Config.Name, resp.Streams[j].Config.Name) < 0
		})
	}

	resp.Total = scnt
	resp.Limit = JSApiListLimit
	resp.Offset = offset
	resp.Missing = missingNames
	s.sendAPIResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(resp))
}

// This will do a scatter and gather operation for all consumers for this stream and account.
// This will be running in a separate Go routine.
func (s *Server) jsClusteredConsumerListRequest(acc *Account, ci *ClientInfo, offset int, stream, subject, reply string, rmsg []byte) {
	defer s.grWG.Done()

	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	js.mu.RLock()

	var consumers []*consumerAssignment
	if sas := cc.streams[acc.Name]; sas != nil {
		if sa := sas[stream]; sa != nil {
			// Copy over since we need to sort etc.
			for _, ca := range sa.consumers {
				consumers = append(consumers, ca)
			}
		}
	}
	// Needs to be sorted.
	if len(consumers) > 1 {
		sort.Slice(consumers, func(i, j int) bool {
			return strings.Compare(consumers[i].Name, consumers[j].Name) < 0
		})
	}

	ocnt := len(consumers)
	if offset > ocnt {
		offset = ocnt
	}
	if offset > 0 {
		consumers = consumers[offset:]
	}
	if len(consumers) > JSApiListLimit {
		consumers = consumers[:JSApiListLimit]
	}

	// Send out our requests here.
	var resp = JSApiConsumerListResponse{
		ApiResponse: ApiResponse{Type: JSApiConsumerListResponseType},
		Consumers:   []*ConsumerInfo{},
	}

	js.mu.RUnlock()

	if len(consumers) == 0 {
		resp.Limit = JSApiListLimit
		resp.Offset = offset
		s.sendAPIResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(resp))
		return
	}

	// Create an inbox for our responses and send out requests.
	s.mu.Lock()
	inbox := s.newRespInbox()
	rc := make(chan *ConsumerInfo, len(consumers))

	// Store our handler.
	s.sys.replies[inbox] = func(sub *subscription, _ *client, _ *Account, subject, _ string, msg []byte) {
		var ci ConsumerInfo
		if err := json.Unmarshal(msg, &ci); err != nil {
			s.Warnf("Error unmarshaling clustered consumer info response:%v", err)
			return
		}
		select {
		case rc <- &ci:
		default:
			s.Warnf("Failed placing consumer info result on internal chan")
		}
	}
	s.mu.Unlock()

	// Cleanup after.
	defer func() {
		s.mu.Lock()
		if s.sys != nil && s.sys.replies != nil {
			delete(s.sys.replies, inbox)
		}
		s.mu.Unlock()
	}()

	var missingNames []string
	sent := map[string]struct{}{}

	// Send out our requests here.
	js.mu.RLock()
	for _, ca := range consumers {
		if s.allPeersOffline(ca.Group) {
			// Place offline onto our results by hand here.
			ci := &ConsumerInfo{Config: ca.Config, Created: ca.Created, Cluster: js.offlineClusterInfo(ca.Group)}
			resp.Consumers = append(resp.Consumers, ci)
			missingNames = append(missingNames, ca.Name)
		} else {
			isubj := fmt.Sprintf(clusterConsumerInfoT, ca.Client.serviceAccount(), stream, ca.Name)
			s.sendInternalMsgLocked(isubj, inbox, nil, nil)
			sent[ca.Name] = struct{}{}
		}
	}
	// Don't hold lock.
	js.mu.RUnlock()

	const timeout = 4 * time.Second
	notActive := time.NewTimer(timeout)
	defer notActive.Stop()

LOOP:
	for len(sent) > 0 {
		select {
		case <-s.quitCh:
			return
		case <-notActive.C:
			s.Warnf("Did not receive all consumer info results for '%s > %s'", acc, stream)
			for cName := range sent {
				missingNames = append(missingNames, cName)
			}
			break LOOP
		case ci := <-rc:
			delete(sent, ci.Name)
			resp.Consumers = append(resp.Consumers, ci)
			// Check to see if we are done.
			if len(resp.Consumers) == len(consumers) {
				break LOOP
			}
		}
	}

	// Needs to be sorted as well.
	if len(resp.Consumers) > 1 {
		sort.Slice(resp.Consumers, func(i, j int) bool {
			return strings.Compare(resp.Consumers[i].Name, resp.Consumers[j].Name) < 0
		})
	}

	resp.Total = len(resp.Consumers)
	resp.Limit = JSApiListLimit
	resp.Offset = offset
	resp.Missing = missingNames
	s.sendAPIResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(resp))
}

func encodeStreamPurge(sp *streamPurge) []byte {
	var bb bytes.Buffer
	bb.WriteByte(byte(purgeStreamOp))
	json.NewEncoder(&bb).Encode(sp)
	return bb.Bytes()
}

func decodeStreamPurge(buf []byte) (*streamPurge, error) {
	var sp streamPurge
	err := json.Unmarshal(buf, &sp)
	return &sp, err
}

func (s *Server) jsClusteredConsumerDeleteRequest(ci *ClientInfo, acc *Account, stream, consumer, subject, reply string, rmsg []byte) {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	if cc.meta == nil {
		return
	}

	var resp = JSApiConsumerDeleteResponse{ApiResponse: ApiResponse{Type: JSApiConsumerDeleteResponseType}}

	sa := js.streamAssignment(acc.Name, stream)
	if sa == nil {
		resp.Error = NewJSStreamNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return

	}
	if sa.consumers == nil {
		resp.Error = NewJSConsumerNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	oca := sa.consumers[consumer]
	if oca == nil {
		resp.Error = NewJSConsumerNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	oca.deleted = true
	ca := &consumerAssignment{Group: oca.Group, Stream: stream, Name: consumer, Config: oca.Config, Subject: subject, Reply: reply, Client: ci}
	cc.meta.Propose(encodeDeleteConsumerAssignment(ca))
}

func encodeMsgDelete(md *streamMsgDelete) []byte {
	var bb bytes.Buffer
	bb.WriteByte(byte(deleteMsgOp))
	json.NewEncoder(&bb).Encode(md)
	return bb.Bytes()
}

func decodeMsgDelete(buf []byte) (*streamMsgDelete, error) {
	var md streamMsgDelete
	err := json.Unmarshal(buf, &md)
	return &md, err
}

func (s *Server) jsClusteredMsgDeleteRequest(ci *ClientInfo, acc *Account, mset *stream, stream, subject, reply string, req *JSApiMsgDeleteRequest, rmsg []byte) {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	js.mu.Lock()
	sa := js.streamAssignment(acc.Name, stream)
	if sa == nil {
		s.Debugf("Message delete failed, could not locate stream '%s > %s'", acc.Name, stream)
		js.mu.Unlock()
		return
	}

	// Check for single replica items.
	if n := sa.Group.node; n != nil {
		md := streamMsgDelete{Seq: req.Seq, NoErase: req.NoErase, Stream: stream, Subject: subject, Reply: reply, Client: ci}
		n.Propose(encodeMsgDelete(&md))
		js.mu.Unlock()
		return
	}
	js.mu.Unlock()

	if mset == nil {
		return
	}

	var err error
	var removed bool
	if req.NoErase {
		removed, err = mset.removeMsg(req.Seq)
	} else {
		removed, err = mset.eraseMsg(req.Seq)
	}
	var resp = JSApiMsgDeleteResponse{ApiResponse: ApiResponse{Type: JSApiMsgDeleteResponseType}}
	if err != nil {
		resp.Error = NewJSStreamMsgDeleteFailedError(err, Unless(err))
	} else if !removed {
		resp.Error = NewJSSequenceNotFoundError(req.Seq)
	} else {
		resp.Success = true
	}
	s.sendAPIResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(resp))
}

func encodeAddStreamAssignment(sa *streamAssignment) []byte {
	var bb bytes.Buffer
	bb.WriteByte(byte(assignStreamOp))
	json.NewEncoder(&bb).Encode(sa)
	return bb.Bytes()
}

func encodeUpdateStreamAssignment(sa *streamAssignment) []byte {
	var bb bytes.Buffer
	bb.WriteByte(byte(updateStreamOp))
	json.NewEncoder(&bb).Encode(sa)
	return bb.Bytes()
}

func encodeDeleteStreamAssignment(sa *streamAssignment) []byte {
	var bb bytes.Buffer
	bb.WriteByte(byte(removeStreamOp))
	json.NewEncoder(&bb).Encode(sa)
	return bb.Bytes()
}

func decodeStreamAssignment(buf []byte) (*streamAssignment, error) {
	var sa streamAssignment
	err := json.Unmarshal(buf, &sa)
	if err != nil {
		return nil, err
	}
	fixCfgMirrorWithDedupWindow(sa.Config)
	return &sa, err
}

// createGroupForConsumer will create a new group from same peer set as the stream.
func (cc *jetStreamCluster) createGroupForConsumer(cfg *ConsumerConfig, sa *streamAssignment) *raftGroup {
	if len(sa.Group.Peers) == 0 || cfg.Replicas > len(sa.Group.Peers) {
		return nil
	}

	peers := copyStrings(sa.Group.Peers)
	var _ss [5]string
	active := _ss[:0]

	// Calculate all active peers.
	for _, peer := range peers {
		if sir, ok := cc.s.nodeToInfo.Load(peer); ok && sir != nil {
			if !sir.(nodeInfo).offline {
				active = append(active, peer)
			}
		}
	}
	if quorum := cfg.Replicas/2 + 1; quorum > len(active) {
		// Not enough active to satisfy the request.
		return nil
	}

	// If we want less then our parent stream, select from active.
	if cfg.Replicas > 0 && cfg.Replicas < len(peers) {
		// Pedantic in case stream is say R5 and consumer is R3 and 3 or more offline, etc.
		if len(active) < cfg.Replicas {
			return nil
		}
		// First shuffle the active peers and then select to account for replica = 1.
		rand.Shuffle(len(active), func(i, j int) { active[i], active[j] = active[j], active[i] })
		peers = active[:cfg.Replicas]
	}
	storage := sa.Config.Storage
	if cfg.MemoryStorage {
		storage = MemoryStorage
	}
	return &raftGroup{Name: groupNameForConsumer(peers, storage), Storage: storage, Peers: peers}
}

// jsClusteredConsumerRequest is first point of entry to create a consumer with R > 1.
func (s *Server) jsClusteredConsumerRequest(ci *ClientInfo, acc *Account, subject, reply string, rmsg []byte, stream string, cfg *ConsumerConfig) {
	js, cc := s.getJetStreamCluster()
	if js == nil || cc == nil {
		return
	}

	var resp = JSApiConsumerCreateResponse{ApiResponse: ApiResponse{Type: JSApiConsumerCreateResponseType}}

	streamCfg, ok := js.clusterStreamConfig(acc.Name, stream)
	if !ok {
		resp.Error = NewJSStreamNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	selectedLimits, _, _, apiErr := acc.selectLimits(&streamCfg)
	if apiErr != nil {
		resp.Error = apiErr
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}
	srvLim := &s.getOpts().JetStreamLimits
	// Make sure we have sane defaults
	setConsumerConfigDefaults(cfg, srvLim, selectedLimits)

	if err := checkConsumerCfg(cfg, srvLim, &streamCfg, acc, selectedLimits, false); err != nil {
		resp.Error = err
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	js.mu.Lock()
	defer js.mu.Unlock()

	if cc.meta == nil {
		return
	}

	// Lookup the stream assignment.
	sa := js.streamAssignment(acc.Name, stream)
	if sa == nil {
		resp.Error = NewJSStreamNotFoundError()
		s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
		return
	}

	// Check for max consumers here to short circuit if possible.
	// Start with limit on a stream, but if one is defined at the level of the account
	// and is lower, use that limit.
	maxc := sa.Config.MaxConsumers
	if maxc <= 0 || (selectedLimits.MaxConsumers > 0 && selectedLimits.MaxConsumers < maxc) {
		maxc = selectedLimits.MaxConsumers
	}
	if maxc > 0 {
		// Don't count DIRECTS.
		total := 0
		for _, ca := range sa.consumers {
			if ca.Config != nil && !ca.Config.Direct {
				total++
			}
		}
		if total >= maxc {
			resp.Error = NewJSMaximumConsumersLimitError()
			s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
			return
		}
	}

	// Also short circuit if DeliverLastPerSubject is set with no FilterSubject.
	if cfg.DeliverPolicy == DeliverLastPerSubject {
		if cfg.FilterSubject == _EMPTY_ {
			resp.Error = NewJSConsumerInvalidPolicyError(fmt.Errorf("consumer delivery policy is deliver last per subject, but FilterSubject is not set"))
			s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
			return
		}
	}

	// Setup proper default for ack wait if we are in explicit ack mode.
	if cfg.AckWait == 0 && (cfg.AckPolicy == AckExplicit || cfg.AckPolicy == AckAll) {
		cfg.AckWait = JsAckWaitDefault
	}
	// Setup default of -1, meaning no limit for MaxDeliver.
	if cfg.MaxDeliver == 0 {
		cfg.MaxDeliver = -1
	}
	// Set proper default for max ack pending if we are ack explicit and none has been set.
	if cfg.AckPolicy == AckExplicit && cfg.MaxAckPending == 0 {
		cfg.MaxAckPending = JsDefaultMaxAckPending
	}

	var ca *consumerAssignment
	var oname string

	// See if we have an existing one already under same durable name or
	// if name was set by the user.
	if isDurableConsumer(cfg) || cfg.Name != _EMPTY_ {
		if cfg.Name != _EMPTY_ {
			oname = cfg.Name
		} else {
			oname = cfg.Durable
		}
		if ca = sa.consumers[oname]; ca != nil && !ca.deleted {
			// Do quick sanity check on new cfg to prevent here if possible.
			if err := acc.checkNewConsumerConfig(ca.Config, cfg); err != nil {
				resp.Error = NewJSConsumerCreateError(err, Unless(err))
				s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
				return
			}
		}
	}

	// If this is new consumer.
	if ca == nil {
		rg := cc.createGroupForConsumer(cfg, sa)
		if rg == nil {
			resp.Error = NewJSInsufficientResourcesError()
			s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
			return
		}
		// Pick a preferred leader.
		rg.setPreferred()

		// Inherit cluster from stream.
		rg.Cluster = sa.Group.Cluster

		// We need to set the ephemeral here before replicating.
		if !isDurableConsumer(cfg) {
			// We chose to have ephemerals be R=1 unless stream is interest or workqueue.
			// Consumer can override.
			if sa.Config.Retention == LimitsPolicy && cfg.Replicas <= 1 {
				rg.Peers = []string{rg.Preferred}
				rg.Name = groupNameForConsumer(rg.Peers, rg.Storage)
			}
			if cfg.Name != _EMPTY_ {
				oname = cfg.Name
			} else {
				// Make sure name is unique.
				for {
					oname = createConsumerName()
					if sa.consumers != nil {
						if sa.consumers[oname] != nil {
							continue
						}
					}
					break
				}
			}
		}
		if len(rg.Peers) > 1 {
			if maxHaAssets := s.getOpts().JetStreamLimits.MaxHAAssets; maxHaAssets != 0 {
				for _, peer := range rg.Peers {
					if ni, ok := s.nodeToInfo.Load(peer); ok {
						ni := ni.(nodeInfo)
						if stats := ni.stats; stats != nil && stats.HAAssets > maxHaAssets {
							resp.Error = NewJSInsufficientResourcesError()
							s.sendAPIErrResponse(ci, acc, subject, reply, string(rmsg), s.jsonResponse(&resp))
							s.Warnf("%s@%s (HA Asset Count: %d) exceeds max ha asset limit of %d"+
								" for (durable) consumer %s placement on stream %s",
								ni.name, ni.cluster, ni.stats.HAAssets, maxHaAssets, oname, stream)
							return
						}
					}
				}
			}
		}
		ca = &consumerAssignment{
			Group:   rg,
			Stream:  stream,
			Name:    oname,
			Config:  cfg,
			Subject: subject,
			Reply:   reply,
			Client:  ci,
			Created: time.Now().UTC(),
		}
	} else {
		nca := ca.copyGroup()

		rBefore := ca.Config.replicas(sa.Config)
		rAfter := cfg.replicas(sa.Config)

		var curLeader string
		if rBefore != rAfter {
			// We are modifying nodes here. We want to do our best to preserve the current leader.
			// We have support now from above that guarantees we are in our own Go routine, so can
			// ask for stream info from the stream leader to make sure we keep the leader in the new list.
			if !s.allPeersOffline(ca.Group) {
				// Need to release js lock.
				js.mu.Unlock()
				if ci, err := sysRequest[ConsumerInfo](s, clusterConsumerInfoT, ci.serviceAccount(), sa.Config.Name, cfg.Durable); err != nil {
					s.Warnf("Did not receive consumer info results for '%s > %s > %s' due to: %s", acc, sa.Config.Name, cfg.Durable, err)
				} else if ci != nil {
					if cl := ci.Cluster; cl != nil {
						curLeader = getHash(cl.Leader)
					}
				}
				// Re-acquire here.
				js.mu.Lock()
			}
		}

		if rBefore < rAfter {
			newPeerSet := nca.Group.Peers
			// scale up by adding new members from the stream peer set that are not yet in the consumer peer set
			streamPeerSet := copyStrings(sa.Group.Peers)
			rand.Shuffle(rAfter, func(i, j int) { streamPeerSet[i], streamPeerSet[j] = streamPeerSet[j], streamPeerSet[i] })
			for _, p := range streamPeerSet {
				found := false
				for _, sp := range newPeerSet {
					if sp == p {
						found = true
						break
					}
				}
				if !found {
					newPeerSet = append(newPeerSet, p)
					if len(newPeerSet) == rAfter {
						break
					}
				}
			}
			nca.Group.Peers = newPeerSet
			nca.Group.Preferred = curLeader
		} else if rBefore > rAfter {
			newPeerSet := nca.Group.Peers
			// mark leader preferred and move it to end
			nca.Group.Preferred = curLeader
			if nca.Group.Preferred != _EMPTY_ {
				for i, p := range newPeerSet {
					if nca.Group.Preferred == p {
						newPeerSet[i] = newPeerSet[len(newPeerSet)-1]
						newPeerSet[len(newPeerSet)-1] = p
					}
				}
			}
			// scale down by removing peers from the end
			newPeerSet = newPeerSet[len(newPeerSet)-rAfter:]
			nca.Group.Peers = newPeerSet
		}

		// Update config and client info on copy of existing.
		nca.Config = cfg
		nca.Client = ci
		nca.Subject = subject
		nca.Reply = reply
		ca = nca
	}

	eca := encodeAddConsumerAssignment(ca)

	// Mark this as pending.
	if sa.consumers == nil {
		sa.consumers = make(map[string]*consumerAssignment)
	}
	sa.consumers[ca.Name] = ca

	// Do formal proposal.
	cc.meta.Propose(eca)
}

func encodeAddConsumerAssignment(ca *consumerAssignment) []byte {
	var bb bytes.Buffer
	bb.WriteByte(byte(assignConsumerOp))
	json.NewEncoder(&bb).Encode(ca)
	return bb.Bytes()
}

func encodeDeleteConsumerAssignment(ca *consumerAssignment) []byte {
	var bb bytes.Buffer
	bb.WriteByte(byte(removeConsumerOp))
	json.NewEncoder(&bb).Encode(ca)
	return bb.Bytes()
}

func decodeConsumerAssignment(buf []byte) (*consumerAssignment, error) {
	var ca consumerAssignment
	err := json.Unmarshal(buf, &ca)
	return &ca, err
}

func encodeAddConsumerAssignmentCompressed(ca *consumerAssignment) []byte {
	b, err := json.Marshal(ca)
	if err != nil {
		return nil
	}
	// TODO(dlc) - Streaming better approach here probably.
	var bb bytes.Buffer
	bb.WriteByte(byte(assignCompressedConsumerOp))
	bb.Write(s2.Encode(nil, b))
	return bb.Bytes()
}

func decodeConsumerAssignmentCompressed(buf []byte) (*consumerAssignment, error) {
	var ca consumerAssignment
	js, err := s2.Decode(nil, buf)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(js, &ca)
	return &ca, err
}

var errBadStreamMsg = errors.New("jetstream cluster bad replicated stream msg")

func decodeStreamMsg(buf []byte) (subject, reply string, hdr, msg []byte, lseq uint64, ts int64, err error) {
	var le = binary.LittleEndian
	if len(buf) < 26 {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	lseq = le.Uint64(buf)
	buf = buf[8:]
	ts = int64(le.Uint64(buf))
	buf = buf[8:]
	sl := int(le.Uint16(buf))
	buf = buf[2:]
	if len(buf) < sl {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	subject = string(buf[:sl])
	buf = buf[sl:]
	if len(buf) < 2 {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	rl := int(le.Uint16(buf))
	buf = buf[2:]
	if len(buf) < rl {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	reply = string(buf[:rl])
	buf = buf[rl:]
	if len(buf) < 2 {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	hl := int(le.Uint16(buf))
	buf = buf[2:]
	if len(buf) < hl {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	if hdr = buf[:hl]; len(hdr) == 0 {
		hdr = nil
	}
	buf = buf[hl:]
	if len(buf) < 4 {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	ml := int(le.Uint32(buf))
	buf = buf[4:]
	if len(buf) < ml {
		return _EMPTY_, _EMPTY_, nil, nil, 0, 0, errBadStreamMsg
	}
	if msg = buf[:ml]; len(msg) == 0 {
		msg = nil
	}
	return subject, reply, hdr, msg, lseq, ts, nil
}

// Helper to return if compression allowed.
func (mset *stream) compressAllowed() bool {
	mset.clMu.Lock()
	defer mset.clMu.Unlock()
	return mset.compressOK
}

func encodeStreamMsg(subject, reply string, hdr, msg []byte, lseq uint64, ts int64) []byte {
	return encodeStreamMsgAllowCompress(subject, reply, hdr, msg, lseq, ts, false)
}

// Threshold for compression.
// TODO(dlc) - Eventually make configurable.
const compressThreshold = 4 * 1024

// If allowed and contents over the threshold we will compress.
func encodeStreamMsgAllowCompress(subject, reply string, hdr, msg []byte, lseq uint64, ts int64, compressOK bool) []byte {
	shouldCompress := compressOK && len(subject)+len(reply)+len(hdr)+len(msg) > compressThreshold

	elen := 1 + 8 + 8 + len(subject) + len(reply) + len(hdr) + len(msg)
	elen += (2 + 2 + 2 + 4) // Encoded lengths, 4bytes
	// TODO(dlc) - check sizes of subject, reply and hdr, make sure uint16 ok.
	buf := make([]byte, elen)
	buf[0] = byte(streamMsgOp)
	var le = binary.LittleEndian
	wi := 1
	le.PutUint64(buf[wi:], lseq)
	wi += 8
	le.PutUint64(buf[wi:], uint64(ts))
	wi += 8
	le.PutUint16(buf[wi:], uint16(len(subject)))
	wi += 2
	copy(buf[wi:], subject)
	wi += len(subject)
	le.PutUint16(buf[wi:], uint16(len(reply)))
	wi += 2
	copy(buf[wi:], reply)
	wi += len(reply)
	le.PutUint16(buf[wi:], uint16(len(hdr)))
	wi += 2
	if len(hdr) > 0 {
		copy(buf[wi:], hdr)
		wi += len(hdr)
	}
	le.PutUint32(buf[wi:], uint32(len(msg)))
	wi += 4
	if len(msg) > 0 {
		copy(buf[wi:], msg)
		wi += len(msg)
	}

	// Check if we should compress.
	if shouldCompress {
		nbuf := make([]byte, s2.MaxEncodedLen(elen))
		nbuf[0] = byte(compressedStreamMsgOp)
		ebuf := s2.Encode(nbuf[1:], buf[1:wi])
		// Only pay cost of decode the other side if we compressed.
		// S2 will allow us to try without major penalty for non-compressable data.
		if len(ebuf) < wi {
			nbuf = nbuf[:len(ebuf)+1]
			buf, wi = nbuf, len(nbuf)
		}
	}

	return buf[:wi]
}

// StreamSnapshot is used for snapshotting and out of band catch up in clustered mode.
type streamSnapshot struct {
	Msgs     uint64   `json:"messages"`
	Bytes    uint64   `json:"bytes"`
	FirstSeq uint64   `json:"first_seq"`
	LastSeq  uint64   `json:"last_seq"`
	Failed   uint64   `json:"clfs"`
	Deleted  []uint64 `json:"deleted,omitempty"`
}

// Grab a snapshot of a stream for clustered mode.
func (mset *stream) stateSnapshot() []byte {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.stateSnapshotLocked()
}

// Grab a snapshot of a stream for clustered mode.
// Lock should be held.
func (mset *stream) stateSnapshotLocked() []byte {
	state := mset.store.State()
	snap := &streamSnapshot{
		Msgs:     state.Msgs,
		Bytes:    state.Bytes,
		FirstSeq: state.FirstSeq,
		LastSeq:  state.LastSeq,
		Failed:   mset.clfs,
		Deleted:  state.Deleted,
	}
	b, _ := json.Marshal(snap)
	return b
}

// Will check if we can do message compression in RAFT and catchup logic.
func (mset *stream) checkAllowMsgCompress(peers []string) {
	allowed := true
	for _, id := range peers {
		sir, ok := mset.srv.nodeToInfo.Load(id)
		if !ok || sir == nil {
			allowed = false
			break
		}
		// Check for capability.
		if si := sir.(nodeInfo); si.cfg == nil || !si.cfg.CompressOK {
			allowed = false
			break
		}
	}
	mset.mu.Lock()
	mset.compressOK = allowed
	mset.mu.Unlock()
}

// To warn when we are getting too far behind from what has been proposed vs what has been committed.
const streamLagWarnThreshold = 10_000

// processClusteredMsg will propose the inbound message to the underlying raft group.
func (mset *stream) processClusteredInboundMsg(subject, reply string, hdr, msg []byte) error {
	// For possible error response.
	var response []byte

	mset.mu.RLock()
	canRespond := !mset.cfg.NoAck && len(reply) > 0
	name, stype, store := mset.cfg.Name, mset.cfg.Storage, mset.store
	s, js, jsa, st, rf, tierName, outq, node := mset.srv, mset.js, mset.jsa, mset.cfg.Storage, mset.cfg.Replicas, mset.tier, mset.outq, mset.node
	maxMsgSize, lseq, clfs := int(mset.cfg.MaxMsgSize), mset.lseq, mset.clfs
	isLeader, isSealed := mset.isLeader(), mset.cfg.Sealed
	mset.mu.RUnlock()

	// This should not happen but possible now that we allow scale up, and scale down where this could trigger.
	if node == nil {
		return mset.processJetStreamMsg(subject, reply, hdr, msg, 0, 0)
	}

	// Check that we are the leader. This can be false if we have scaled up from an R1 that had inbound queued messages.
	if !isLeader {
		return NewJSClusterNotLeaderError()
	}

	// Bail here if sealed.
	if isSealed {
		var resp = JSPubAckResponse{PubAck: &PubAck{Stream: mset.name()}, Error: NewJSStreamSealedError()}
		b, _ := json.Marshal(resp)
		mset.outq.sendMsg(reply, b)
		return NewJSStreamSealedError()
	}

	// Check here pre-emptively if we have exceeded this server limits.
	if js.limitsExceeded(stype) {
		s.resourcesExeededError()
		if canRespond {
			b, _ := json.Marshal(&JSPubAckResponse{PubAck: &PubAck{Stream: name}, Error: NewJSInsufficientResourcesError()})
			outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, nil, b, nil, 0))
		}
		// Stepdown regardless.
		if node := mset.raftNode(); node != nil {
			node.StepDown()
		}
		return NewJSInsufficientResourcesError()
	}

	// Check here pre-emptively if we have exceeded our account limits.
	var exceeded bool
	jsa.usageMu.Lock()
	jsaLimits, ok := jsa.limits[tierName]
	if !ok {
		jsa.usageMu.Unlock()
		err := fmt.Errorf("no JetStream resource limits found account: %q", jsa.acc().Name)
		s.RateLimitWarnf(err.Error())
		if canRespond {
			var resp = &JSPubAckResponse{PubAck: &PubAck{Stream: name}}
			resp.Error = NewJSNoLimitsError()
			response, _ = json.Marshal(resp)
			outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, nil, response, nil, 0))
		}
		return err
	}
	t, ok := jsa.usage[tierName]
	if !ok {
		t = &jsaStorage{}
		jsa.usage[tierName] = t
	}
	if st == MemoryStorage {
		total := t.total.store + int64(memStoreMsgSize(subject, hdr, msg)*uint64(rf))
		if jsaLimits.MaxMemory > 0 && total > jsaLimits.MaxMemory {
			exceeded = true
		}
	} else {
		total := t.total.store + int64(fileStoreMsgSize(subject, hdr, msg)*uint64(rf))
		if jsaLimits.MaxStore > 0 && total > jsaLimits.MaxStore {
			exceeded = true
		}
	}
	jsa.usageMu.Unlock()

	// If we have exceeded our account limits go ahead and return.
	if exceeded {
		err := fmt.Errorf("JetStream resource limits exceeded for account: %q", jsa.acc().Name)
		s.RateLimitWarnf(err.Error())
		if canRespond {
			var resp = &JSPubAckResponse{PubAck: &PubAck{Stream: name}}
			resp.Error = NewJSAccountResourcesExceededError()
			response, _ = json.Marshal(resp)
			outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, nil, response, nil, 0))
		}
		return err
	}

	// Check msgSize if we have a limit set there. Again this works if it goes through but better to be pre-emptive.
	if maxMsgSize >= 0 && (len(hdr)+len(msg)) > maxMsgSize {
		err := fmt.Errorf("JetStream message size exceeds limits for '%s > %s'", jsa.acc().Name, mset.cfg.Name)
		s.RateLimitWarnf(err.Error())
		if canRespond {
			var resp = &JSPubAckResponse{PubAck: &PubAck{Stream: name}}
			resp.Error = NewJSStreamMessageExceedsMaximumError()
			response, _ = json.Marshal(resp)
			outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, nil, response, nil, 0))
		}
		return err
	}

	// Some header checks can be checked pre proposal. Most can not.
	if len(hdr) > 0 {
		// For CAS operations, e.g. ExpectedLastSeqPerSubject, we can also check here and not have to go through.
		// Can only precheck for seq != 0.
		if seq, exists := getExpectedLastSeqPerSubject(hdr); exists && store != nil && seq > 0 {
			var smv StoreMsg
			var fseq uint64
			sm, err := store.LoadLastMsg(subject, &smv)
			if sm != nil {
				fseq = sm.seq
			}
			if err != nil || fseq != seq {
				if canRespond {
					var resp = &JSPubAckResponse{PubAck: &PubAck{Stream: name}}
					resp.PubAck = &PubAck{Stream: name}
					resp.Error = NewJSStreamWrongLastSequenceError(fseq)
					b, _ := json.Marshal(resp)
					outq.sendMsg(reply, b)
				}
				return fmt.Errorf("last sequence by subject mismatch: %d vs %d", seq, fseq)
			}
		}
		// Expected stream name can also be pre-checked.
		if sname := getExpectedStream(hdr); sname != _EMPTY_ && sname != name {
			if canRespond {
				var resp = &JSPubAckResponse{PubAck: &PubAck{Stream: name}}
				resp.PubAck = &PubAck{Stream: name}
				resp.Error = NewJSStreamNotMatchError()
				b, _ := json.Marshal(resp)
				outq.sendMsg(reply, b)
			}
			return errors.New("expected stream does not match")
		}
	}

	// Since we encode header len as u16 make sure we do not exceed.
	// Again this works if it goes through but better to be pre-emptive.
	if len(hdr) > math.MaxUint16 {
		err := fmt.Errorf("JetStream header size exceeds limits for '%s > %s'", jsa.acc().Name, mset.cfg.Name)
		s.RateLimitWarnf(err.Error())
		if canRespond {
			var resp = &JSPubAckResponse{PubAck: &PubAck{Stream: name}}
			resp.Error = NewJSStreamHeaderExceedsMaximumError()
			response, _ = json.Marshal(resp)
			outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, nil, response, nil, 0))
		}
		return err
	}

	// Proceed with proposing this message.

	// We only use mset.clseq for clustering and in case we run ahead of actual commits.
	// Check if we need to set initial value here
	mset.clMu.Lock()
	if mset.clseq == 0 || mset.clseq < lseq {
		// Re-capture
		lseq, clfs = mset.lastSeqAndCLFS()
		mset.clseq = lseq + clfs
	}

	esm := encodeStreamMsgAllowCompress(subject, reply, hdr, msg, mset.clseq, time.Now().UnixNano(), mset.compressOK)
	mset.clseq++

	// Do proposal.
	err := node.Propose(esm)
	if err != nil && mset.clseq > 0 {
		mset.clseq--
	}

	// Check to see if we are being overrun.
	// TODO(dlc) - Make this a limit where we drop messages to protect ourselves, but allow to be configured.
	if mset.clseq-(lseq+clfs) > streamLagWarnThreshold {
		lerr := fmt.Errorf("JetStream stream '%s > %s' has high message lag", jsa.acc().Name, mset.cfg.Name)
		s.RateLimitWarnf(lerr.Error())
	}
	mset.clMu.Unlock()

	if err != nil {
		if canRespond {
			var resp = &JSPubAckResponse{PubAck: &PubAck{Stream: mset.cfg.Name}}
			resp.Error = &ApiError{Code: 503, Description: err.Error()}
			response, _ = json.Marshal(resp)
			// If we errored out respond here.
			outq.send(newJSPubMsg(reply, _EMPTY_, _EMPTY_, nil, response, nil, 0))
		}
	}

	if err != nil && isOutOfSpaceErr(err) {
		s.handleOutOfSpace(mset)
	}

	return err
}

// For requesting messages post raft snapshot to catch up streams post server restart.
// Any deleted msgs etc will be handled inline on catchup.
type streamSyncRequest struct {
	Peer     string `json:"peer,omitempty"`
	FirstSeq uint64 `json:"first_seq"`
	LastSeq  uint64 `json:"last_seq"`
}

// Given a stream state that represents a snapshot, calculate the sync request based on our current state.
func (mset *stream) calculateSyncRequest(state *StreamState, snap *streamSnapshot) *streamSyncRequest {
	// Quick check if we are already caught up.
	if state.LastSeq >= snap.LastSeq {
		return nil
	}
	return &streamSyncRequest{FirstSeq: state.LastSeq + 1, LastSeq: snap.LastSeq, Peer: mset.node.ID()}
}

// processSnapshotDeletes will update our current store based on the snapshot
// but only processing deletes and new FirstSeq / purges.
func (mset *stream) processSnapshotDeletes(snap *streamSnapshot) {
	mset.mu.Lock()
	var state StreamState
	mset.store.FastState(&state)
	// Always adjust if FirstSeq has moved beyond our state.
	if snap.FirstSeq > state.FirstSeq {
		mset.store.Compact(snap.FirstSeq)
		mset.store.FastState(&state)
		mset.lseq = state.LastSeq
		mset.clearAllPreAcksBelowFloor(state.FirstSeq)
	}
	mset.mu.Unlock()

	// Range the deleted and delete if applicable.
	for _, dseq := range snap.Deleted {
		if dseq > state.FirstSeq && dseq <= state.LastSeq {
			mset.store.RemoveMsg(dseq)
		}
	}
}

func (mset *stream) setCatchupPeer(peer string, lag uint64) {
	if peer == _EMPTY_ {
		return
	}
	mset.mu.Lock()
	if mset.catchups == nil {
		mset.catchups = make(map[string]uint64)
	}
	mset.catchups[peer] = lag
	mset.mu.Unlock()
}

// Will decrement by one.
func (mset *stream) updateCatchupPeer(peer string) {
	if peer == _EMPTY_ {
		return
	}
	mset.mu.Lock()
	if lag := mset.catchups[peer]; lag > 0 {
		mset.catchups[peer] = lag - 1
	}
	mset.mu.Unlock()
}

func (mset *stream) clearCatchupPeer(peer string) {
	mset.mu.Lock()
	if mset.catchups != nil {
		delete(mset.catchups, peer)
	}
	mset.mu.Unlock()
}

// Lock should be held.
func (mset *stream) clearAllCatchupPeers() {
	if mset.catchups != nil {
		mset.catchups = nil
	}
}

func (mset *stream) lagForCatchupPeer(peer string) uint64 {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	if mset.catchups == nil {
		return 0
	}
	return mset.catchups[peer]
}

func (mset *stream) hasCatchupPeers() bool {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return len(mset.catchups) > 0
}

func (mset *stream) setCatchingUp() {
	mset.mu.Lock()
	mset.catchup = true
	mset.mu.Unlock()
}

func (mset *stream) clearCatchingUp() {
	mset.mu.Lock()
	mset.catchup = false
	mset.mu.Unlock()
}

func (mset *stream) isCatchingUp() bool {
	mset.mu.RLock()
	defer mset.mu.RUnlock()
	return mset.catchup
}

// Determine if a non-leader is current.
// Lock should be held.
func (mset *stream) isCurrent() bool {
	if mset.node == nil {
		return true
	}
	return mset.node.Current() && !mset.catchup
}

// Maximum requests for the whole server that can be in flight.
const maxConcurrentSyncRequests = 8

var (
	errCatchupCorruptSnapshot = errors.New("corrupt stream snapshot detected")
	errCatchupStalled         = errors.New("catchup stalled")
	errCatchupStreamStopped   = errors.New("stream has been stopped") // when a catchup is terminated due to the stream going away.
	errCatchupBadMsg          = errors.New("bad catchup msg")
	errCatchupWrongSeqForSkip = errors.New("wrong sequence for skipped msg")
)

// Process a stream snapshot.
func (mset *stream) processSnapshot(snap *streamSnapshot) (e error) {
	// Update any deletes, etc.
	mset.processSnapshotDeletes(snap)

	mset.mu.Lock()
	var state StreamState
	mset.clfs = snap.Failed
	mset.store.FastState(&state)
	sreq := mset.calculateSyncRequest(&state, snap)

	s, js, subject, n := mset.srv, mset.js, mset.sa.Sync, mset.node
	qname := fmt.Sprintf("[ACC:%s] stream '%s' snapshot", mset.acc.Name, mset.cfg.Name)
	mset.mu.Unlock()

	// Bug that would cause this to be empty on stream update.
	if subject == _EMPTY_ {
		return errCatchupCorruptSnapshot
	}

	// Just return if up to date or already exceeded limits.
	if sreq == nil || js.limitsExceeded(mset.cfg.Storage) {
		return nil
	}

	// Pause the apply channel for our raft group while we catch up.
	if err := n.PauseApply(); err != nil {
		return err
	}

	defer func() {
		// Don't bother resuming if server or stream is gone.
		if e != errCatchupStreamStopped && e != ErrServerNotRunning {
			n.ResumeApply()
		}
	}()

	// Set our catchup state.
	mset.setCatchingUp()
	defer mset.clearCatchingUp()

	var sub *subscription
	var err error

	const activityInterval = 10 * time.Second
	notActive := time.NewTimer(activityInterval)
	defer notActive.Stop()

	defer func() {
		if sub != nil {
			s.sysUnsubscribe(sub)
		}
		// Make sure any consumers are updated for the pending amounts.
		mset.mu.Lock()
		for _, o := range mset.consumers {
			o.mu.Lock()
			if o.isLeader() {
				o.streamNumPending()
			}
			o.mu.Unlock()
		}
		mset.mu.Unlock()
	}()

	var releaseSem bool
	releaseSyncOutSem := func() {
		if !releaseSem {
			return
		}
		// Need to use select for the server shutdown case.
		select {
		case s.syncOutSem <- struct{}{}:
		default:
		}
		releaseSem = false
	}
	// On exit, we will release our semaphore if we acquired it.
	defer releaseSyncOutSem()

	// Check our final state when we exit cleanly.
	// If this snapshot was for messages no longer held by the leader we want to make sure
	// we are synched for the next message sequence properly.
	lastRequested := sreq.LastSeq
	checkFinalState := func() {
		// Bail if no stream.
		if mset == nil {
			return
		}

		mset.mu.Lock()
		var state StreamState
		mset.store.FastState(&state)
		var didReset bool
		firstExpected := lastRequested + 1
		if state.FirstSeq != firstExpected {
			// Reset our notion of first.
			mset.store.Compact(firstExpected)
			mset.store.FastState(&state)
			// Make sure last is also correct in case this also moved.
			mset.lseq = state.LastSeq
			mset.clearAllPreAcksBelowFloor(state.FirstSeq)
			didReset = true
		}
		mset.mu.Unlock()

		if didReset {
			s.Warnf("Catchup for stream '%s > %s' resetting first sequence: %d on catchup complete",
				mset.account(), mset.name(), firstExpected)
		}

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

RETRY:
	// On retry, we need to release the semaphore we got. Call will be no-op
	// if releaseSem boolean has not been set to true on successfully getting
	// the semaphore.
	releaseSyncOutSem()

	if n.GroupLeader() == _EMPTY_ {
		return fmt.Errorf("catchup for stream '%s > %s' aborted, no leader", mset.account(), mset.name())
	}

	// If we have a sub clear that here.
	if sub != nil {
		s.sysUnsubscribe(sub)
		sub = nil
	}

	// Block here if we have too many requests in flight.
	<-s.syncOutSem
	releaseSem = true
	if !s.isRunning() {
		return ErrServerNotRunning
	}

	// We may have been blocked for a bit, so the reset need to ensure that we
	// consume the already fired timer.
	if !notActive.Stop() {
		select {
		case <-notActive.C:
		default:
		}
	}
	notActive.Reset(activityInterval)

	// Grab sync request again on failures.
	if sreq == nil {
		mset.mu.Lock()
		var state StreamState
		mset.store.FastState(&state)
		sreq = mset.calculateSyncRequest(&state, snap)
		mset.mu.Unlock()
		if sreq == nil {
			return nil
		}
		// Reset notion of lastRequested
		lastRequested = sreq.LastSeq
	}

	// Used to transfer message from the wire to another Go routine internally.
	type im struct {
		msg   []byte
		reply string
	}
	// This is used to notify the leader that it should stop the runCatchup
	// because we are either bailing out or going to retry due to an error.
	notifyLeaderStopCatchup := func(mrec *im, err error) {
		if mrec.reply == _EMPTY_ {
			return
		}
		s.sendInternalMsgLocked(mrec.reply, _EMPTY_, nil, err.Error())
	}

	msgsQ := newIPQueue[*im](s, qname)
	defer msgsQ.unregister()

	// Send our catchup request here.
	reply := syncReplySubject()
	sub, err = s.sysSubscribe(reply, func(_ *subscription, _ *client, _ *Account, _, reply string, msg []byte) {
		// Make copies
		// TODO(dlc) - Since we are using a buffer from the inbound client/route.
		msgsQ.push(&im{copyBytes(msg), reply})
	})
	if err != nil {
		s.Errorf("Could not subscribe to stream catchup: %v", err)
		goto RETRY
	}
	// Send our sync request.
	b, _ := json.Marshal(sreq)
	s.sendInternalMsgLocked(subject, reply, nil, b)
	// Remember when we sent this out to avoimd loop spins on errors below.
	reqSendTime := time.Now()

	// Clear our sync request and capture last.
	last := sreq.LastSeq
	sreq = nil

	// Run our own select loop here.
	for qch, lch := n.QuitC(), n.LeadChangeC(); ; {
		select {
		case <-msgsQ.ch:
			notActive.Reset(activityInterval)

			mrecs := msgsQ.pop()

			for _, mrec := range mrecs {
				msg := mrec.msg

				// Check for eof signaling.
				if len(msg) == 0 {
					msgsQ.recycle(&mrecs)
					checkFinalState()
					return nil
				}
				if lseq, err := mset.processCatchupMsg(msg); err == nil {
					if mrec.reply != _EMPTY_ {
						s.sendInternalMsgLocked(mrec.reply, _EMPTY_, nil, nil)
					}
					if lseq >= last {
						msgsQ.recycle(&mrecs)
						return nil
					}
				} else if isOutOfSpaceErr(err) {
					notifyLeaderStopCatchup(mrec, err)
					return err
				} else if err == NewJSInsufficientResourcesError() {
					notifyLeaderStopCatchup(mrec, err)
					if mset.js.limitsExceeded(mset.cfg.Storage) {
						s.resourcesExeededError()
					} else {
						s.Warnf("Catchup for stream '%s > %s' errored, account resources exceeded: %v", mset.account(), mset.name(), err)
					}
					msgsQ.recycle(&mrecs)
					return err
				} else {
					notifyLeaderStopCatchup(mrec, err)
					s.Warnf("Catchup for stream '%s > %s' errored, will retry: %v", mset.account(), mset.name(), err)
					msgsQ.recycle(&mrecs)

					// Make sure we do not spin and make things worse.
					const minRetryWait = 2 * time.Second
					elapsed := time.Since(reqSendTime)
					if elapsed < minRetryWait {
						select {
						case <-s.quitCh:
							return ErrServerNotRunning
						case <-qch:
							return errCatchupStreamStopped
						case <-time.After(minRetryWait - elapsed):
						}
					}
					goto RETRY
				}
			}
			msgsQ.recycle(&mrecs)
		case <-notActive.C:
			if mrecs := msgsQ.pop(); len(mrecs) > 0 {
				mrec := mrecs[0]
				notifyLeaderStopCatchup(mrec, errCatchupStalled)
				msgsQ.recycle(&mrecs)
			}
			s.Warnf("Catchup for stream '%s > %s' stalled", mset.account(), mset.name())
			goto RETRY
		case <-s.quitCh:
			return ErrServerNotRunning
		case <-qch:
			return errCatchupStreamStopped
		case isLeader := <-lch:
			if isLeader {
				n.StepDown()
				goto RETRY
			}
		}
	}
}

// processCatchupMsg will be called to process out of band catchup msgs from a sync request.
func (mset *stream) processCatchupMsg(msg []byte) (uint64, error) {
	if len(msg) == 0 {
		return 0, errCatchupBadMsg
	}
	op := entryOp(msg[0])
	if op != streamMsgOp && op != compressedStreamMsgOp {
		return 0, errCatchupBadMsg
	}

	mbuf := msg[1:]
	if op == compressedStreamMsgOp {
		var err error
		mbuf, err = s2.Decode(nil, mbuf)
		if err != nil {
			panic(err.Error())
		}
	}

	subj, _, hdr, msg, seq, ts, err := decodeStreamMsg(mbuf)
	if err != nil {
		return 0, errCatchupBadMsg
	}

	mset.mu.Lock()
	st := mset.cfg.Storage
	ddloaded := mset.ddloaded
	tierName := mset.tier

	if mset.hasAllPreAcks(seq, subj) {
		mset.clearAllPreAcks(seq)
		// Mark this to be skipped
		subj, ts = _EMPTY_, 0
	}
	mset.mu.Unlock()

	if mset.js.limitsExceeded(st) {
		return 0, NewJSInsufficientResourcesError()
	} else if exceeded, apiErr := mset.jsa.limitsExceeded(st, tierName); apiErr != nil {
		return 0, apiErr
	} else if exceeded {
		return 0, NewJSInsufficientResourcesError()
	}

	// Put into our store
	// Messages to be skipped have no subject or timestamp.
	// TODO(dlc) - formalize with skipMsgOp
	if subj == _EMPTY_ && ts == 0 {
		if lseq := mset.store.SkipMsg(); lseq != seq {
			return 0, errCatchupWrongSeqForSkip
		}
	} else if err := mset.store.StoreRawMsg(subj, hdr, msg, seq, ts); err != nil {
		return 0, err
	}

	// Update our lseq.
	mset.setLastSeq(seq)

	// Check for MsgId and if we have one here make sure to update our internal map.
	if len(hdr) > 0 {
		if msgId := getMsgId(hdr); msgId != _EMPTY_ {
			if !ddloaded {
				mset.mu.Lock()
				mset.rebuildDedupe()
				mset.mu.Unlock()
			}
			mset.storeMsgId(&ddentry{msgId, seq, ts})
		}
	}

	return seq, nil
}

func (mset *stream) handleClusterSyncRequest(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
	var sreq streamSyncRequest
	if err := json.Unmarshal(msg, &sreq); err != nil {
		// Log error.
		return
	}
	mset.srv.startGoRoutine(func() { mset.runCatchup(reply, &sreq) })
}

// Lock should be held.
func (js *jetStream) offlineClusterInfo(rg *raftGroup) *ClusterInfo {
	s := js.srv

	ci := &ClusterInfo{Name: s.ClusterName()}
	for _, peer := range rg.Peers {
		if sir, ok := s.nodeToInfo.Load(peer); ok && sir != nil {
			si := sir.(nodeInfo)
			pi := &PeerInfo{Peer: peer, Name: si.name, Current: false, Offline: true}
			ci.Replicas = append(ci.Replicas, pi)
		}
	}
	return ci
}

// clusterInfo will report on the status of the raft group.
func (js *jetStream) clusterInfo(rg *raftGroup) *ClusterInfo {
	if js == nil {
		return nil
	}
	js.mu.RLock()
	defer js.mu.RUnlock()

	s := js.srv
	if rg == nil || rg.node == nil {
		return &ClusterInfo{
			Name:   s.ClusterName(),
			Leader: s.Name(),
		}
	}
	n := rg.node

	ci := &ClusterInfo{
		Name:   s.ClusterName(),
		Leader: s.serverNameForNode(n.GroupLeader()),
	}

	now := time.Now()

	id, peers := n.ID(), n.Peers()

	// If we are leaderless, do not suppress putting us in the peer list.
	if ci.Leader == _EMPTY_ {
		id = _EMPTY_
	}

	for _, rp := range peers {
		if rp.ID != id && rg.isMember(rp.ID) {
			var lastSeen time.Duration
			if now.After(rp.Last) && rp.Last.Unix() != 0 {
				lastSeen = now.Sub(rp.Last)
			}
			current := rp.Current
			if current && lastSeen > lostQuorumInterval {
				current = false
			}
			// Create a peer info with common settings if the peer has not been seen
			// yet (which can happen after the whole cluster is stopped and only some
			// of the nodes are restarted).
			pi := &PeerInfo{
				Current: current,
				Offline: true,
				Active:  lastSeen,
				Lag:     rp.Lag,
				Peer:    rp.ID,
			}
			// If node is found, complete/update the settings.
			if sir, ok := s.nodeToInfo.Load(rp.ID); ok && sir != nil {
				si := sir.(nodeInfo)
				pi.Name, pi.Offline, pi.cluster = si.name, si.offline, si.cluster
			} else {
				// If not, then add a name that indicates that the server name
				// is unknown at this time, and clear the lag since it is misleading
				// (the node may not have that much lag).
				// Note: We return now the Peer ID in PeerInfo, so the "(peerID: %s)"
				// would technically not be required, but keeping it for now.
				pi.Name, pi.Lag = fmt.Sprintf("Server name unknown at this time (peerID: %s)", rp.ID), 0
			}
			ci.Replicas = append(ci.Replicas, pi)
		}
	}
	// Order the result based on the name so that we get something consistent
	// when doing repeated stream info in the CLI, etc...
	sort.Slice(ci.Replicas, func(i, j int) bool {
		return ci.Replicas[i].Name < ci.Replicas[j].Name
	})
	return ci
}

func (mset *stream) checkClusterInfo(ci *ClusterInfo) {
	for _, r := range ci.Replicas {
		peer := getHash(r.Name)
		if lag := mset.lagForCatchupPeer(peer); lag > 0 {
			r.Current = false
			r.Lag = lag
		}
	}
}

// Return a list of alternates, ranked by preference order to the request, of stream mirrors.
// This allows clients to select or get more information about read replicas that could be a
// better option to connect to versus the original source.
func (js *jetStream) streamAlternates(ci *ClientInfo, stream string) []StreamAlternate {
	if js == nil {
		return nil
	}

	js.mu.RLock()
	defer js.mu.RUnlock()

	s, cc := js.srv, js.cluster
	// Track our domain.
	domain := s.getOpts().JetStreamDomain

	// No clustering just return nil.
	if cc == nil {
		return nil
	}
	acc, _ := s.LookupAccount(ci.serviceAccount())
	if acc == nil {
		return nil
	}

	// Collect our ordering first for clusters.
	weights := make(map[string]int)
	all := []string{ci.Cluster}
	all = append(all, ci.Alternates...)

	for i := 0; i < len(all); i++ {
		weights[all[i]] = len(all) - i
	}

	var alts []StreamAlternate
	for _, sa := range cc.streams[acc.Name] {
		// Add in ourselves and any mirrors.
		if sa.Config.Name == stream || (sa.Config.Mirror != nil && sa.Config.Mirror.Name == stream) {
			alts = append(alts, StreamAlternate{Name: sa.Config.Name, Domain: domain, Cluster: sa.Group.Cluster})
		}
	}
	// If just us don't fill in.
	if len(alts) == 1 {
		return nil
	}

	// Sort based on our weights that originate from the request itself.
	sort.Slice(alts, func(i, j int) bool {
		return weights[alts[i].Cluster] > weights[alts[j].Cluster]
	})

	return alts
}

// Internal request for stream info, this is coming on the wire so do not block here.
func (mset *stream) handleClusterStreamInfoRequest(_ *subscription, c *client, _ *Account, subject, reply string, _ []byte) {
	go mset.processClusterStreamInfoRequest(reply)
}

func (mset *stream) processClusterStreamInfoRequest(reply string) {
	mset.mu.RLock()
	sysc, js, sa, config := mset.sysc, mset.srv.js, mset.sa, mset.cfg
	isLeader := mset.isLeader()
	mset.mu.RUnlock()

	// By design all members will receive this. Normally we only want the leader answering.
	// But if we have stalled and lost quorom all can respond.
	if sa != nil && !js.isGroupLeaderless(sa.Group) && !isLeader {
		return
	}

	// If we are not the leader let someone else possible respond first.
	if !isLeader {
		time.Sleep(200 * time.Millisecond)
	}

	si := &StreamInfo{
		Created: mset.createdTime(),
		State:   mset.state(),
		Config:  config,
		Cluster: js.clusterInfo(mset.raftGroup()),
		Sources: mset.sourcesInfo(),
		Mirror:  mset.mirrorInfo(),
	}

	// Check for out of band catchups.
	if mset.hasCatchupPeers() {
		mset.checkClusterInfo(si.Cluster)
	}

	sysc.sendInternalMsg(reply, _EMPTY_, nil, si)
}

// 64MB for now, for the total server. This is max we will blast out if asked to
// do so to another server for purposes of catchups.
// This number should be ok on 1Gbit interface.
const defaultMaxTotalCatchupOutBytes = int64(64 * 1024 * 1024)

// Current total outstanding catchup bytes.
func (s *Server) gcbTotal() int64 {
	s.gcbMu.RLock()
	defer s.gcbMu.RUnlock()
	return s.gcbOut
}

// Returns true if Current total outstanding catchup bytes is below
// the maximum configured.
func (s *Server) gcbBelowMax() bool {
	s.gcbMu.RLock()
	defer s.gcbMu.RUnlock()
	return s.gcbOut <= s.gcbOutMax
}

// Adds `sz` to the server's total outstanding catchup bytes and to `localsz`
// under the gcbMu lock. The `localsz` points to the local outstanding catchup
// bytes of the runCatchup go routine of a given stream.
func (s *Server) gcbAdd(localsz *int64, sz int64) {
	s.gcbMu.Lock()
	atomic.AddInt64(localsz, sz)
	s.gcbOut += sz
	if s.gcbOut >= s.gcbOutMax && s.gcbKick == nil {
		s.gcbKick = make(chan struct{})
	}
	s.gcbMu.Unlock()
}

// Removes `sz` from the server's total outstanding catchup bytes and from
// `localsz`, but only if `localsz` is non 0, which would signal that gcSubLast
// has already been invoked. See that function for details.
// Must be invoked under the gcbMu lock.
func (s *Server) gcbSubLocked(localsz *int64, sz int64) {
	if atomic.LoadInt64(localsz) == 0 {
		return
	}
	atomic.AddInt64(localsz, -sz)
	s.gcbOut -= sz
	if s.gcbKick != nil && s.gcbOut < s.gcbOutMax {
		close(s.gcbKick)
		s.gcbKick = nil
	}
}

// Locked version of gcbSubLocked()
func (s *Server) gcbSub(localsz *int64, sz int64) {
	s.gcbMu.Lock()
	s.gcbSubLocked(localsz, sz)
	s.gcbMu.Unlock()
}

// Similar to gcbSub() but reset `localsz` to 0 at the end under the gcbMu lock.
// This will signal further calls to gcbSub() for this `localsz` pointer that
// nothing should be done because runCatchup() has exited and any remaining
// outstanding bytes value has already been decremented.
func (s *Server) gcbSubLast(localsz *int64) {
	s.gcbMu.Lock()
	s.gcbSubLocked(localsz, *localsz)
	*localsz = 0
	s.gcbMu.Unlock()
}

// Returns our kick chan, or nil if it does not exist.
func (s *Server) cbKickChan() <-chan struct{} {
	s.gcbMu.RLock()
	defer s.gcbMu.RUnlock()
	return s.gcbKick
}

func (mset *stream) runCatchup(sendSubject string, sreq *streamSyncRequest) {
	s := mset.srv
	defer s.grWG.Done()

	const maxOutBytes = int64(8 * 1024 * 1024) // 8MB for now, these are all internal, from server to server
	const maxOutMsgs = int32(32 * 1024)
	outb := int64(0)
	outm := int32(0)

	// On abnormal exit make sure to update global total.
	defer s.gcbSubLast(&outb)

	// Flow control processing.
	ackReplySize := func(subj string) int64 {
		if li := strings.LastIndexByte(subj, btsep); li > 0 && li < len(subj) {
			return parseAckReplyNum(subj[li+1:])
		}
		return 0
	}

	nextBatchC := make(chan struct{}, 1)
	nextBatchC <- struct{}{}
	remoteQuitCh := make(chan struct{})

	// Setup ackReply for flow control.
	ackReply := syncAckSubject()
	ackSub, _ := s.sysSubscribe(ackReply, func(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
		if len(msg) > 0 {
			s.Warnf("Catchup for stream '%s > %s' was aborted on the remote due to: %q",
				mset.account(), mset.name(), msg)
			s.sysUnsubscribe(sub)
			close(remoteQuitCh)
			return
		}
		sz := ackReplySize(subject)
		s.gcbSub(&outb, sz)
		atomic.AddInt32(&outm, -1)
		mset.updateCatchupPeer(sreq.Peer)
		// Kick ourselves and anyone else who might have stalled on global state.
		select {
		case nextBatchC <- struct{}{}:
		default:
		}
	})
	defer s.sysUnsubscribe(ackSub)
	ackReplyT := strings.ReplaceAll(ackReply, ".*", ".%d")

	const activityInterval = 5 * time.Second
	notActive := time.NewTimer(activityInterval)
	defer notActive.Stop()

	// Grab our state.
	var state StreamState
	mset.mu.RLock()
	mset.store.FastState(&state)
	mset.mu.RUnlock()

	// Reset notion of first if this request wants sequences before our starting sequence
	// and we would have nothing to send. If we have partial messages still need to send skips for those.
	if sreq.FirstSeq < state.FirstSeq && state.FirstSeq > sreq.LastSeq {
		s.Debugf("Catchup for stream '%s > %s' resetting request first sequence from %d to %d",
			mset.account(), mset.name(), sreq.FirstSeq, state.FirstSeq)
		sreq.FirstSeq = state.FirstSeq
	}

	// Setup sequences to walk through.
	seq, last := sreq.FirstSeq, sreq.LastSeq
	mset.setCatchupPeer(sreq.Peer, last-seq)

	// Check if we can compress during this.
	compressOk := mset.compressAllowed()

	var spb int
	sendNextBatchAndContinue := func(qch chan struct{}) bool {
		// Update our activity timer.
		notActive.Reset(activityInterval)

		// Check if we know we will not enter the loop because we are done.
		if seq > last {
			s.Noticef("Catchup for stream '%s > %s' complete", mset.account(), mset.name())
			// EOF
			s.sendInternalMsgLocked(sendSubject, _EMPTY_, nil, nil)
			return false
		}

		// If we already sent a batch, we will try to make sure we process around
		// half the FC responses - or reach a certain amount of time - before sending
		// the next batch.
		if spb > 0 {
			mw := time.NewTimer(100 * time.Millisecond)
			for done := false; !done; {
				select {
				case <-nextBatchC:
					done = int(atomic.LoadInt32(&outm)) <= spb/2
				case <-mw.C:
					done = true
				case <-s.quitCh:
					return false
				case <-qch:
					return false
				case <-remoteQuitCh:
					return false
				}
			}
			spb = 0
		}

		var smv StoreMsg

		for ; seq <= last && atomic.LoadInt64(&outb) <= maxOutBytes && atomic.LoadInt32(&outm) <= maxOutMsgs && s.gcbBelowMax(); seq++ {
			sm, err := mset.store.LoadMsg(seq, &smv)
			// if this is not a deleted msg, bail out.
			if err != nil && err != ErrStoreMsgNotFound && err != errDeletedMsg {
				if err == ErrStoreEOF {
					var state StreamState
					mset.store.FastState(&state)
					if seq > state.LastSeq {
						// The snapshot has a larger last sequence then we have. This could be due to a truncation
						// when trying to recover after corruption, still not 100% sure. Could be off by 1 too somehow,
						// but tested a ton of those with no success.
						s.Warnf("Catchup for stream '%s > %s' completed, but requested sequence %d was larger then current state: %+v",
							mset.account(), mset.name(), seq, state)
						// Try our best to redo our invalidated snapshot as well.
						if n := mset.raftNode(); n != nil {
							n.InstallSnapshot(mset.stateSnapshot())
						}
						// Signal EOF
						s.sendInternalMsgLocked(sendSubject, _EMPTY_, nil, nil)
						return false
					}
				}
				s.Warnf("Error loading message for catchup '%s > %s': %v", mset.account(), mset.name(), err)
				return false
			}
			var em []byte
			if sm != nil {
				em = encodeStreamMsgAllowCompress(sm.subj, _EMPTY_, sm.hdr, sm.msg, sm.seq, sm.ts, compressOk)
			} else {
				// Skip record for deleted msg.
				em = encodeStreamMsg(_EMPTY_, _EMPTY_, nil, nil, seq, 0)
			}

			// Place size in reply subject for flow control.
			l := int64(len(em))
			reply := fmt.Sprintf(ackReplyT, l)
			s.gcbAdd(&outb, l)
			atomic.AddInt32(&outm, 1)
			s.sendInternalMsgLocked(sendSubject, reply, nil, em)
			spb++
			if seq == last {
				s.Noticef("Catchup for stream '%s > %s' complete", mset.account(), mset.name())
				// EOF
				s.sendInternalMsgLocked(sendSubject, _EMPTY_, nil, nil)
				return false
			}
			select {
			case <-remoteQuitCh:
				return false
			default:
			}
		}
		return true
	}

	// Grab stream quit channel.
	mset.mu.RLock()
	qch := mset.qch
	mset.mu.RUnlock()
	if qch == nil {
		return
	}

	// Run as long as we are still active and need catchup.
	// FIXME(dlc) - Purge event? Stream delete?
	for {
		// Get this each time, will be non-nil if globally blocked and we will close to wake everyone up.
		cbKick := s.cbKickChan()

		select {
		case <-s.quitCh:
			return
		case <-qch:
			return
		case <-remoteQuitCh:
			mset.clearCatchupPeer(sreq.Peer)
			return
		case <-notActive.C:
			s.Warnf("Catchup for stream '%s > %s' stalled", mset.account(), mset.name())
			return
		case <-nextBatchC:
			if !sendNextBatchAndContinue(qch) {
				mset.clearCatchupPeer(sreq.Peer)
				return
			}
		case <-cbKick:
			if !sendNextBatchAndContinue(qch) {
				mset.clearCatchupPeer(sreq.Peer)
				return
			}
		}
	}
}

const jscAllSubj = "$JSC.>"

func syncSubjForStream() string {
	return syncSubject("$JSC.SYNC")
}

func syncReplySubject() string {
	return syncSubject("$JSC.R")
}

func infoReplySubject() string {
	return syncSubject("$JSC.R")
}

func syncAckSubject() string {
	return syncSubject("$JSC.ACK") + ".*"
}

func syncSubject(pre string) string {
	var sb strings.Builder
	sb.WriteString(pre)
	sb.WriteByte(btsep)

	var b [replySuffixLen]byte
	rn := rand.Int63()
	for i, l := 0, rn; i < len(b); i++ {
		b[i] = digits[l%base]
		l /= base
	}

	sb.Write(b[:])
	return sb.String()
}

const (
	clusterStreamInfoT   = "$JSC.SI.%s.%s"
	clusterConsumerInfoT = "$JSC.CI.%s.%s.%s"
	jsaUpdatesSubT       = "$JSC.ARU.%s.*"
	jsaUpdatesPubT       = "$JSC.ARU.%s.%s"
)

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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/highwayhash"
)

type RaftNode interface {
	Propose(entry []byte) error
	ProposeDirect(entries []*Entry) error
	ForwardProposal(entry []byte) error
	InstallSnapshot(snap []byte) error
	SendSnapshot(snap []byte) error
	NeedSnapshot() bool
	Applied(index uint64) (entries uint64, bytes uint64)
	State() RaftState
	Size() (entries, bytes uint64)
	Progress() (index, commit, applied uint64)
	Leader() bool
	Quorum() bool
	Current() bool
	Healthy() bool
	Term() uint64
	GroupLeader() string
	HadPreviousLeader() bool
	StepDown(preferred ...string) error
	SetObserver(isObserver bool)
	IsObserver() bool
	Campaign() error
	ID() string
	Group() string
	Peers() []*Peer
	UpdateKnownPeers(knownPeers []string)
	ProposeAddPeer(peer string) error
	ProposeRemovePeer(peer string) error
	AdjustClusterSize(csz int) error
	AdjustBootClusterSize(csz int) error
	ClusterSize() int
	ApplyQ() *ipQueue[*CommittedEntry]
	PauseApply() error
	ResumeApply()
	LeadChangeC() <-chan bool
	QuitC() <-chan struct{}
	Created() time.Time
	Stop()
	Delete()
	Wipe()
}

type WAL interface {
	Type() StorageType
	StoreMsg(subj string, hdr, msg []byte) (uint64, int64, error)
	LoadMsg(index uint64, sm *StoreMsg) (*StoreMsg, error)
	RemoveMsg(index uint64) (bool, error)
	Compact(index uint64) (uint64, error)
	Purge() (uint64, error)
	Truncate(seq uint64) error
	State() StreamState
	FastState(*StreamState)
	Stop() error
	Delete() error
}

type Peer struct {
	ID      string
	Current bool
	Last    time.Time
	Lag     uint64
}

type RaftState uint8

// Allowable states for a NATS Consensus Group.
const (
	Follower RaftState = iota
	Leader
	Candidate
	Observer
	Closed
)

func (state RaftState) String() string {
	switch state {
	case Follower:
		return "FOLLOWER"
	case Candidate:
		return "CANDIDATE"
	case Leader:
		return "LEADER"
	case Observer:
		return "OBSERVER"
	case Closed:
		return "CLOSED"
	}
	return "UNKNOWN"
}

type raft struct {
	sync.RWMutex
	created  time.Time
	group    string
	sd       string
	id       string
	wal      WAL
	wtype    StorageType
	track    bool
	werr     error
	state    RaftState
	hh       hash.Hash64
	snapfile string
	csz      int
	qn       int
	peers    map[string]*lps
	removed  map[string]struct{}
	acks     map[uint64]map[string]struct{}
	pae      map[uint64]*appendEntry
	elect    *time.Timer
	active   time.Time
	llqrt    time.Time
	lsut     time.Time
	term     uint64 // The current vote term
	pterm    uint64 // Previous term from the last snapshot
	pindex   uint64 // Previous index from the last snapshot
	commit   uint64 // Sequence number of the most recent commit
	applied  uint64 // Sequence number of the most recently applied commit
	leader   string // The ID of the leader
	vote     string
	hash     string
	s        *Server
	c        *client
	js       *jetStream
	dflag    bool
	pleader  bool
	observer bool
	extSt    extensionState

	// Subjects for votes, updates, replays.
	psubj  string
	rpsubj string
	vsubj  string
	vreply string
	asubj  string
	areply string

	sq    *sendq
	aesub *subscription

	// Are we doing a leadership transfer.
	lxfer bool

	// For holding term and vote and peerstate to be written.
	wtv   []byte
	wps   []byte
	wtvch chan struct{}
	wpsch chan struct{}

	// For when we need to catch up as a follower.
	catchup *catchupState

	// For leader or server catching up a follower.
	progress map[string]*ipQueue[uint64]

	// For when we have paused our applyC.
	paused    bool
	hcommit   uint64
	pobserver bool

	// Queues and Channels
	prop     *ipQueue[*Entry]
	entry    *ipQueue[*appendEntry]
	resp     *ipQueue[*appendEntryResponse]
	apply    *ipQueue[*CommittedEntry]
	reqs     *ipQueue[*voteRequest]
	votes    *ipQueue[*voteResponse]
	stepdown *ipQueue[string]
	leadc    chan bool
	quit     chan struct{}

	// Account name of the asset this raft group is for
	accName string

	// Random generator, used to generate inboxes for instance
	prand *rand.Rand
}

// cacthupState structure that holds our subscription, and catchup term and index
// as well as starting term and index and how many updates we have seen.
type catchupState struct {
	sub    *subscription
	cterm  uint64
	cindex uint64
	pterm  uint64
	pindex uint64
	active time.Time
}

// lps holds peer state of last time and last index replicated.
type lps struct {
	ts int64
	li uint64
	kp bool // marks as known peer.
}

const (
	minElectionTimeoutDefault      = 4 * time.Second
	maxElectionTimeoutDefault      = 9 * time.Second
	minCampaignTimeoutDefault      = 100 * time.Millisecond
	maxCampaignTimeoutDefault      = 8 * minCampaignTimeoutDefault
	hbIntervalDefault              = 1 * time.Second
	lostQuorumIntervalDefault      = hbIntervalDefault * 10 // 10 seconds
	lostQuorumCheckIntervalDefault = hbIntervalDefault * 10 // 10 seconds

)

var (
	minElectionTimeout = minElectionTimeoutDefault
	maxElectionTimeout = maxElectionTimeoutDefault
	minCampaignTimeout = minCampaignTimeoutDefault
	maxCampaignTimeout = maxCampaignTimeoutDefault
	hbInterval         = hbIntervalDefault
	lostQuorumInterval = lostQuorumIntervalDefault
	lostQuorumCheck    = lostQuorumCheckIntervalDefault
)

type RaftConfig struct {
	Name     string
	Store    string
	Log      WAL
	Track    bool
	Observer bool
}

var (
	errNotLeader         = errors.New("raft: not leader")
	errAlreadyLeader     = errors.New("raft: already leader")
	errNilCfg            = errors.New("raft: no config given")
	errCorruptPeers      = errors.New("raft: corrupt peer state")
	errEntryLoadFailed   = errors.New("raft: could not load entry from WAL")
	errEntryStoreFailed  = errors.New("raft: could not store entry to WAL")
	errNodeClosed        = errors.New("raft: node is closed")
	errBadSnapName       = errors.New("raft: snapshot name could not be parsed")
	errNoSnapAvailable   = errors.New("raft: no snapshot available")
	errCatchupsRunning   = errors.New("raft: snapshot can not be installed while catchups running")
	errSnapshotCorrupt   = errors.New("raft: snapshot corrupt")
	errTooManyPrefs      = errors.New("raft: stepdown requires at most one preferred new leader")
	errNoPeerState       = errors.New("raft: no peerstate")
	errAdjustBootCluster = errors.New("raft: can not adjust boot peer size on established group")
	errLeaderLen         = fmt.Errorf("raft: leader should be exactly %d bytes", idLen)
	errTooManyEntries    = errors.New("raft: append entry can contain a max of 64k entries")
	errBadAppendEntry    = errors.New("raft: append entry corrupt")
)

// This will bootstrap a raftNode by writing its config into the store directory.
func (s *Server) bootstrapRaftNode(cfg *RaftConfig, knownPeers []string, allPeersKnown bool) error {
	if cfg == nil {
		return errNilCfg
	}
	// Check validity of peers if presented.
	for _, p := range knownPeers {
		if len(p) != idLen {
			return fmt.Errorf("raft: illegal peer: %q", p)
		}
	}
	expected := len(knownPeers)
	// We need to adjust this is all peers are not known.
	if !allPeersKnown {
		s.Debugf("Determining expected peer size for JetStream meta group")
		if expected < 2 {
			expected = 2
		}
		opts := s.getOpts()
		nrs := len(opts.Routes)

		cn := s.ClusterName()
		ngwps := 0
		for _, gw := range opts.Gateway.Gateways {
			// Ignore our own cluster if specified.
			if gw.Name == cn {
				continue
			}
			for _, u := range gw.URLs {
				host := u.Hostname()
				// If this is an IP just add one.
				if net.ParseIP(host) != nil {
					ngwps++
				} else {
					addrs, _ := net.LookupHost(host)
					ngwps += len(addrs)
				}
			}
		}

		if expected < nrs+ngwps {
			expected = nrs + ngwps
			s.Debugf("Adjusting expected peer set size to %d with %d known", expected, len(knownPeers))
		}
	}

	// Check the store directory. If we have a memory based WAL we need to make sure the directory is setup.
	if stat, err := os.Stat(cfg.Store); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.Store, 0750); err != nil {
			return fmt.Errorf("raft: could not create storage directory - %v", err)
		}
	} else if stat == nil || !stat.IsDir() {
		return fmt.Errorf("raft: storage directory is not a directory")
	}
	tmpfile, err := os.CreateTemp(cfg.Store, "_test_")
	if err != nil {
		return fmt.Errorf("raft: storage directory is not writable")
	}
	tmpfile.Close()
	os.Remove(tmpfile.Name())

	return writePeerState(cfg.Store, &peerState{knownPeers, expected, extUndetermined})
}

// startRaftNode will start the raft node.
func (s *Server) startRaftNode(accName string, cfg *RaftConfig) (RaftNode, error) {
	if cfg == nil {
		return nil, errNilCfg
	}
	s.mu.Lock()
	if s.sys == nil {
		s.mu.Unlock()
		return nil, ErrNoSysAccount
	}
	sq := s.sys.sq
	sacc := s.sys.account
	hash := s.sys.shash
	pub := s.info.ID
	s.mu.Unlock()

	ps, err := readPeerState(cfg.Store)
	if err != nil {
		return nil, err
	}
	if ps == nil {
		return nil, errNoPeerState
	}

	qpfx := fmt.Sprintf("[ACC:%s] RAFT '%s' ", accName, cfg.Name)
	rsrc := time.Now().UnixNano()
	if len(pub) >= 32 {
		if h, _ := highwayhash.New64([]byte(pub[:32])); h != nil {
			rsrc += int64(h.Sum64())
		}
	}
	n := &raft{
		created:  time.Now(),
		id:       hash[:idLen],
		group:    cfg.Name,
		sd:       cfg.Store,
		wal:      cfg.Log,
		wtype:    cfg.Log.Type(),
		track:    cfg.Track,
		state:    Follower,
		csz:      ps.clusterSize,
		qn:       ps.clusterSize/2 + 1,
		hash:     hash,
		peers:    make(map[string]*lps),
		acks:     make(map[uint64]map[string]struct{}),
		pae:      make(map[uint64]*appendEntry),
		s:        s,
		c:        s.createInternalSystemClient(),
		js:       s.getJetStream(),
		sq:       sq,
		quit:     make(chan struct{}),
		wtvch:    make(chan struct{}, 1),
		wpsch:    make(chan struct{}, 1),
		reqs:     newIPQueue[*voteRequest](s, qpfx+"vreq"),
		votes:    newIPQueue[*voteResponse](s, qpfx+"vresp"),
		prop:     newIPQueue[*Entry](s, qpfx+"entry"),
		entry:    newIPQueue[*appendEntry](s, qpfx+"appendEntry"),
		resp:     newIPQueue[*appendEntryResponse](s, qpfx+"appendEntryResponse"),
		apply:    newIPQueue[*CommittedEntry](s, qpfx+"committedEntry"),
		stepdown: newIPQueue[string](s, qpfx+"stepdown"),
		accName:  accName,
		leadc:    make(chan bool, 1),
		observer: cfg.Observer,
		extSt:    ps.domainExt,
		prand:    rand.New(rand.NewSource(rsrc)),
	}
	n.c.registerWithAccount(sacc)

	if atomic.LoadInt32(&s.logging.debug) > 0 {
		n.dflag = true
	}

	key := sha256.Sum256([]byte(n.group))
	n.hh, _ = highwayhash.New64(key[:])

	if term, vote, err := n.readTermVote(); err == nil && term > 0 {
		n.term = term
		n.vote = vote
	}

	if err := os.MkdirAll(filepath.Join(n.sd, snapshotsDir), 0750); err != nil {
		return nil, fmt.Errorf("could not create snapshots directory - %v", err)
	}

	// Can't recover snapshots if memory based.
	if _, ok := n.wal.(*memStore); ok {
		os.Remove(filepath.Join(n.sd, snapshotsDir, "*"))
	} else {
		// See if we have any snapshots and if so load and process on startup.
		n.setupLastSnapshot()
	}

	var state StreamState
	n.wal.FastState(&state)
	if state.Msgs > 0 {
		// TODO(dlc) - Recover our state here.
		if first, err := n.loadFirstEntry(); err == nil {
			n.pterm, n.pindex = first.pterm, first.pindex
			if first.commit > 0 && first.commit > n.commit {
				n.commit = first.commit
			}
		}

		for index := state.FirstSeq; index <= state.LastSeq; index++ {
			ae, err := n.loadEntry(index)
			if err != nil {
				n.warn("Could not load %d from WAL [%+v]: %v", index, state, err)
				if err := n.wal.Truncate(index); err != nil {
					n.setWriteErrLocked(err)
				}
				break
			}
			if ae.pindex != index-1 {
				n.warn("Corrupt WAL, will truncate")
				if err := n.wal.Truncate(index); err != nil {
					n.setWriteErrLocked(err)
				}
				break
			}
			n.processAppendEntry(ae, nil)
		}
	}

	// Send nil entry to signal the upper layers we are done doing replay/restore.
	n.apply.push(nil)

	// Make sure to track ourselves.
	n.peers[n.id] = &lps{time.Now().UnixNano(), 0, true}
	// Track known peers
	for _, peer := range ps.knownPeers {
		// Set these to 0 to start but mark as known peer.
		if peer != n.id {
			n.peers[peer] = &lps{0, 0, true}
		}
	}

	// Setup our internal subscriptions.
	if err := n.createInternalSubs(); err != nil {
		n.shutdown(true)
		return nil, err
	}

	n.debug("Started")

	// Check if we need to start in observer mode due to lame duck status.
	if s.isLameDuckMode() {
		n.debug("Will start in observer mode due to lame duck status")
		n.SetObserver(true)
	}

	n.Lock()
	n.resetElectionTimeout()
	n.llqrt = time.Now()
	n.Unlock()

	s.registerRaftNode(n.group, n)
	s.startGoRoutine(n.run)
	s.startGoRoutine(n.fileWriter)

	return n, nil
}

// outOfResources checks to see if we are out of resources.
func (n *raft) outOfResources() bool {
	js := n.js
	if !n.track || js == nil {
		return false
	}
	return js.limitsExceeded(n.wtype)
}

// Maps node names back to server names.
func (s *Server) serverNameForNode(node string) string {
	if si, ok := s.nodeToInfo.Load(node); ok && si != nil {
		return si.(nodeInfo).name
	}
	return _EMPTY_
}

// Maps node names back to cluster names.
func (s *Server) clusterNameForNode(node string) string {
	if si, ok := s.nodeToInfo.Load(node); ok && si != nil {
		return si.(nodeInfo).cluster
	}
	return _EMPTY_
}

// Server will track all raft nodes.
func (s *Server) registerRaftNode(group string, n RaftNode) {
	s.rnMu.Lock()
	defer s.rnMu.Unlock()
	if s.raftNodes == nil {
		s.raftNodes = make(map[string]RaftNode)
	}
	s.raftNodes[group] = n
}

func (s *Server) unregisterRaftNode(group string) {
	s.rnMu.Lock()
	defer s.rnMu.Unlock()
	if s.raftNodes != nil {
		delete(s.raftNodes, group)
	}
}

func (s *Server) numRaftNodes() int {
	s.rnMu.Lock()
	defer s.rnMu.Unlock()
	return len(s.raftNodes)
}

func (s *Server) lookupRaftNode(group string) RaftNode {
	s.rnMu.RLock()
	defer s.rnMu.RUnlock()
	var n RaftNode
	if s.raftNodes != nil {
		n = s.raftNodes[group]
	}
	return n
}

func (s *Server) reloadDebugRaftNodes() {
	if s == nil {
		return
	}
	debug := atomic.LoadInt32(&s.logging.debug) > 0
	s.rnMu.RLock()
	for _, ni := range s.raftNodes {
		n := ni.(*raft)
		n.Lock()
		n.dflag = debug
		n.Unlock()
	}
	s.rnMu.RUnlock()
}

func (s *Server) stepdownRaftNodes() {
	if s == nil {
		return
	}
	var nodes []RaftNode
	s.rnMu.RLock()
	if len(s.raftNodes) > 0 {
		s.Debugf("Stepping down all leader raft nodes")
	}
	for _, n := range s.raftNodes {
		if n.Leader() {
			nodes = append(nodes, n)
		}
	}
	s.rnMu.RUnlock()

	for _, node := range nodes {
		node.StepDown()
	}
}

func (s *Server) shutdownRaftNodes() {
	if s == nil {
		return
	}
	var nodes []RaftNode
	s.rnMu.RLock()
	if len(s.raftNodes) > 0 {
		s.Debugf("Shutting down all raft nodes")
	}
	for _, n := range s.raftNodes {
		nodes = append(nodes, n)
	}
	s.rnMu.RUnlock()

	for _, node := range nodes {
		node.Stop()
	}
}

// Used in lameduck mode to move off the leaders.
// We also put all nodes in observer mode so new leaders
// can not be placed on this server.
func (s *Server) transferRaftLeaders() bool {
	if s == nil {
		return false
	}
	var nodes []RaftNode
	s.rnMu.RLock()
	if len(s.raftNodes) > 0 {
		s.Debugf("Transferring any raft leaders")
	}
	for _, n := range s.raftNodes {
		nodes = append(nodes, n)
	}
	s.rnMu.RUnlock()

	var didTransfer bool
	for _, node := range nodes {
		if node.Leader() {
			node.StepDown()
			didTransfer = true
		}
		node.SetObserver(true)
	}
	return didTransfer
}

// Formal API

// Propose will propose a new entry to the group.
// This should only be called on the leader.
func (n *raft) Propose(data []byte) error {
	n.RLock()
	if n.state != Leader {
		n.RUnlock()
		n.debug("Proposal ignored, not leader")
		return errNotLeader
	}
	// Error if we had a previous write error.
	if werr := n.werr; werr != nil {
		n.RUnlock()
		return werr
	}
	prop := n.prop
	n.RUnlock()

	prop.push(&Entry{EntryNormal, data})
	return nil
}

// ProposeDirect will propose entries directly.
// This should only be called on the leader.
func (n *raft) ProposeDirect(entries []*Entry) error {
	n.RLock()
	if n.state != Leader {
		n.RUnlock()
		n.debug("Proposal ignored, not leader")
		return errNotLeader
	}
	// Error if we had a previous write error.
	if werr := n.werr; werr != nil {
		n.RUnlock()
		return werr
	}
	n.RUnlock()

	n.sendAppendEntry(entries)
	return nil
}

// ForwardProposal will forward the proposal to the leader if known.
// If we are the leader this is the same as calling propose.
// FIXME(dlc) - We could have a reply subject and wait for a response
// for retries, but would need to not block and be in separate Go routine.
func (n *raft) ForwardProposal(entry []byte) error {
	if n.Leader() {
		return n.Propose(entry)
	}

	n.sendRPC(n.psubj, _EMPTY_, entry)
	return nil
}

// ProposeAddPeer is called to add a peer to the group.
func (n *raft) ProposeAddPeer(peer string) error {
	n.RLock()
	if n.state != Leader {
		n.RUnlock()
		return errNotLeader
	}
	// Error if we had a previous write error.
	if werr := n.werr; werr != nil {
		n.RUnlock()
		return werr
	}
	prop := n.prop
	n.RUnlock()

	prop.push(&Entry{EntryAddPeer, []byte(peer)})
	return nil
}

// As a leader if we are proposing to remove a peer assume its already gone.
func (n *raft) doRemovePeerAsLeader(peer string) {
	n.Lock()
	if n.removed == nil {
		n.removed = map[string]struct{}{}
	}
	n.removed[peer] = struct{}{}
	if _, ok := n.peers[peer]; ok {
		delete(n.peers, peer)
		// We should decrease our cluster size since we are tracking this peer and the peer is most likely already gone.
		n.adjustClusterSizeAndQuorum()
	}
	n.Unlock()
}

// ProposeRemovePeer is called to remove a peer from the group.
func (n *raft) ProposeRemovePeer(peer string) error {
	n.RLock()
	prop, subj := n.prop, n.rpsubj
	isLeader := n.state == Leader
	werr := n.werr
	n.RUnlock()

	// Error if we had a previous write error.
	if werr != nil {
		return werr
	}

	if isLeader {
		prop.push(&Entry{EntryRemovePeer, []byte(peer)})
		n.doRemovePeerAsLeader(peer)
		return nil
	}

	// Need to forward.
	n.sendRPC(subj, _EMPTY_, []byte(peer))
	return nil
}

// ClusterSize reports back the total cluster size.
// This effects quorum etc.
func (n *raft) ClusterSize() int {
	n.Lock()
	defer n.Unlock()
	return n.csz
}

// AdjustBootClusterSize can be called to adjust the boot cluster size.
// Will error if called on a group with a leader or a previous leader.
// This can be helpful in mixed mode.
func (n *raft) AdjustBootClusterSize(csz int) error {
	n.Lock()
	defer n.Unlock()

	if n.leader != noLeader || n.pleader {
		return errAdjustBootCluster
	}
	// Same floor as bootstrap.
	if csz < 2 {
		csz = 2
	}
	// Adjust.
	n.csz = csz
	n.qn = n.csz/2 + 1

	return nil
}

// AdjustClusterSize will change the cluster set size.
// Must be the leader.
func (n *raft) AdjustClusterSize(csz int) error {
	n.Lock()
	if n.state != Leader {
		n.Unlock()
		return errNotLeader
	}
	// Same floor as bootstrap.
	if csz < 2 {
		csz = 2
	}

	// Adjust.
	n.csz = csz
	n.qn = n.csz/2 + 1
	n.Unlock()

	n.sendPeerState()
	return nil
}

// PauseApply will allow us to pause processing of append entries onto our
// external apply chan.
func (n *raft) PauseApply() error {
	n.Lock()
	defer n.Unlock()

	if n.state == Leader {
		return errAlreadyLeader
	}
	// If we are currently a candidate make sure we step down.
	if n.state == Candidate {
		n.stepdown.push(noLeader)
	}

	n.debug("Pausing our apply channel")
	n.paused = true
	n.hcommit = n.commit
	// Also prevent us from trying to become a leader while paused and catching up.
	n.pobserver, n.observer = n.observer, true
	n.resetElect(48 * time.Hour)

	return nil
}

func (n *raft) ResumeApply() {
	n.Lock()
	defer n.Unlock()

	if !n.paused {
		return
	}

	n.debug("Resuming our apply channel")
	n.observer, n.pobserver = n.pobserver, false
	n.paused = false
	// Run catchup..
	if n.hcommit > n.commit {
		n.debug("Resuming %d replays", n.hcommit+1-n.commit)
		for index := n.commit + 1; index <= n.hcommit; index++ {
			if err := n.applyCommit(index); err != nil {
				break
			}
		}
	}
	n.hcommit = 0
	n.resetElectionTimeout()
}

// Applied is to be called when the FSM has applied the committed entries.
// Applied will return the number of entries and an estimation of the
// byte size that could be removed with a snapshot/compact.
func (n *raft) Applied(index uint64) (entries uint64, bytes uint64) {
	n.Lock()
	defer n.Unlock()

	// Ignore if not applicable. This can happen during a reset.
	if index > n.commit {
		return 0, 0
	}

	// Ignore if already applied.
	if index > n.applied {
		n.applied = index
	}
	var state StreamState
	n.wal.FastState(&state)
	if n.applied > state.FirstSeq {
		entries = n.applied - state.FirstSeq
	}
	if state.Msgs > 0 {
		bytes = entries * state.Bytes / state.Msgs
	}
	return entries, bytes
}

// For capturing data needed by snapshot.
type snapshot struct {
	lastTerm  uint64
	lastIndex uint64
	peerstate []byte
	data      []byte
}

const minSnapshotLen = 28

// Encodes a snapshot into a buffer for storage.
// Lock should be held.
func (n *raft) encodeSnapshot(snap *snapshot) []byte {
	if snap == nil {
		return nil
	}
	var le = binary.LittleEndian
	buf := make([]byte, minSnapshotLen+len(snap.peerstate)+len(snap.data))
	le.PutUint64(buf[0:], snap.lastTerm)
	le.PutUint64(buf[8:], snap.lastIndex)
	// Peer state
	le.PutUint32(buf[16:], uint32(len(snap.peerstate)))
	wi := 20
	copy(buf[wi:], snap.peerstate)
	wi += len(snap.peerstate)
	// data itself.
	copy(buf[wi:], snap.data)
	wi += len(snap.data)

	// Now do the hash for the end.
	n.hh.Reset()
	n.hh.Write(buf[:wi])
	checksum := n.hh.Sum(nil)
	copy(buf[wi:], checksum)
	wi += len(checksum)
	return buf[:wi]
}

// SendSnapshot will send the latest snapshot as a normal AE.
// Should only be used when the upper layers know this is most recent.
// Used when restoring streams, moving a stream from R1 to R>1, etc.
func (n *raft) SendSnapshot(data []byte) error {
	n.sendAppendEntry([]*Entry{{EntrySnapshot, data}})
	return nil
}

// Used to install a snapshot for the given term and applied index. This will release
// all of the log entries up to and including index. This should not be called with
// entries that have been applied to the FSM but have not been applied to the raft state.
func (n *raft) InstallSnapshot(data []byte) error {
	n.Lock()
	if n.state == Closed {
		n.Unlock()
		return errNodeClosed
	}

	if werr := n.werr; werr != nil {
		n.Unlock()
		return werr
	}

	if len(n.progress) > 0 {
		n.Unlock()
		return errCatchupsRunning
	}

	var state StreamState
	n.wal.FastState(&state)

	if n.applied == 0 {
		n.Unlock()
		return errNoSnapAvailable
	}

	n.debug("Installing snapshot of %d bytes", len(data))

	var term uint64
	if ae, _ := n.loadEntry(n.applied); ae != nil {
		term = ae.term
	} else if ae, _ = n.loadFirstEntry(); ae != nil {
		term = ae.term
	} else {
		term = n.pterm
	}

	snap := &snapshot{
		lastTerm:  term,
		lastIndex: n.applied,
		peerstate: encodePeerState(&peerState{n.peerNames(), n.csz, n.extSt}),
		data:      data,
	}

	snapDir := filepath.Join(n.sd, snapshotsDir)
	sn := fmt.Sprintf(snapFileT, snap.lastTerm, snap.lastIndex)
	sfile := filepath.Join(snapDir, sn)

	if err := os.WriteFile(sfile, n.encodeSnapshot(snap), 0640); err != nil {
		n.Unlock()
		// We could set write err here, but if this is a temporary situation, too many open files etc.
		// we want to retry and snapshots are not fatal.
		return err
	}

	// Remember our latest snapshot file.
	n.snapfile = sfile
	if _, err := n.wal.Compact(snap.lastIndex + 1); err != nil {
		n.setWriteErrLocked(err)
		n.Unlock()
		return err
	}
	n.Unlock()

	psnaps, _ := os.ReadDir(snapDir)
	// Remove any old snapshots.
	for _, fi := range psnaps {
		pn := fi.Name()
		if pn != sn {
			os.Remove(filepath.Join(snapDir, pn))
		}
	}

	return nil
}

func (n *raft) NeedSnapshot() bool {
	n.RLock()
	defer n.RUnlock()
	return n.snapfile == _EMPTY_ && n.applied > 1
}

const (
	snapshotsDir = "snapshots"
	snapFileT    = "snap.%d.%d"
)

func termAndIndexFromSnapFile(sn string) (term, index uint64, err error) {
	if sn == _EMPTY_ {
		return 0, 0, errBadSnapName
	}
	fn := filepath.Base(sn)
	if n, err := fmt.Sscanf(fn, snapFileT, &term, &index); err != nil || n != 2 {
		return 0, 0, errBadSnapName
	}
	return term, index, nil
}

func (n *raft) setupLastSnapshot() {
	snapDir := filepath.Join(n.sd, snapshotsDir)
	psnaps, err := os.ReadDir(snapDir)
	if err != nil {
		return
	}

	var lterm, lindex uint64
	var latest string
	for _, sf := range psnaps {
		sfile := filepath.Join(snapDir, sf.Name())
		var term, index uint64
		term, index, err := termAndIndexFromSnapFile(sf.Name())
		if err == nil {
			if term > lterm {
				lterm, lindex = term, index
				latest = sfile
			} else if term == lterm && index > lindex {
				lindex = index
				latest = sfile
			}
		} else {
			// Clean this up, can't parse the name.
			// TODO(dlc) - We could read in and check actual contents.
			n.debug("Removing snapshot, can't parse name: %q", sf.Name())
			os.Remove(sfile)
		}
	}

	// Now cleanup any old entries
	for _, sf := range psnaps {
		sfile := filepath.Join(snapDir, sf.Name())
		if sfile != latest {
			n.debug("Removing old snapshot: %q", sfile)
			os.Remove(sfile)
		}
	}

	if latest == _EMPTY_ {
		return
	}

	// Set latest snapshot we have.
	n.Lock()
	defer n.Unlock()

	n.snapfile = latest
	snap, err := n.loadLastSnapshot()
	if err != nil {
		if n.snapfile != _EMPTY_ {
			os.Remove(n.snapfile)
			n.snapfile = _EMPTY_
		}
	} else {
		n.pindex = snap.lastIndex
		n.pterm = snap.lastTerm
		n.commit = snap.lastIndex
		n.applied = snap.lastIndex
		n.apply.push(&CommittedEntry{n.commit, []*Entry{{EntrySnapshot, snap.data}}})
		if _, err := n.wal.Compact(snap.lastIndex + 1); err != nil {
			n.setWriteErrLocked(err)
		}
	}
}

// loadLastSnapshot will load and return our last snapshot.
// Lock should be held.
func (n *raft) loadLastSnapshot() (*snapshot, error) {
	if n.snapfile == _EMPTY_ {
		return nil, errNoSnapAvailable
	}
	buf, err := os.ReadFile(n.snapfile)
	if err != nil {
		n.warn("Error reading snapshot: %v", err)
		os.Remove(n.snapfile)
		n.snapfile = _EMPTY_
		return nil, err
	}
	if len(buf) < minSnapshotLen {
		n.warn("Snapshot corrupt, too short")
		os.Remove(n.snapfile)
		n.snapfile = _EMPTY_
		return nil, errSnapshotCorrupt
	}

	// Check to make sure hash is consistent.
	hoff := len(buf) - 8
	lchk := buf[hoff:]
	n.hh.Reset()
	n.hh.Write(buf[:hoff])
	if !bytes.Equal(lchk[:], n.hh.Sum(nil)) {
		n.warn("Snapshot corrupt, checksums did not match")
		os.Remove(n.snapfile)
		n.snapfile = _EMPTY_
		return nil, errSnapshotCorrupt
	}

	var le = binary.LittleEndian
	lps := le.Uint32(buf[16:])
	snap := &snapshot{
		lastTerm:  le.Uint64(buf[0:]),
		lastIndex: le.Uint64(buf[8:]),
		peerstate: buf[20 : 20+lps],
		data:      buf[20+lps : hoff],
	}

	// We had a bug in 2.9.12 that would allow snapshots on last index of 0.
	// Detect that here and return err.
	if snap.lastIndex == 0 {
		n.warn("Snapshot with last index 0 is invalid, cleaning up")
		os.Remove(n.snapfile)
		n.snapfile = _EMPTY_
		return nil, errSnapshotCorrupt
	}

	return snap, nil
}

// Leader returns if we are the leader for our group.
func (n *raft) Leader() bool {
	if n == nil {
		return false
	}
	n.RLock()
	isLeader := n.state == Leader
	n.RUnlock()
	return isLeader
}

func (n *raft) isCatchingUp() bool {
	n.RLock()
	defer n.RUnlock()
	return n.catchup != nil
}

// Lock should be held. This function may block for up to ~5ms to check
// forward progress in some cases.
func (n *raft) isCurrent(includeForwardProgress bool) bool {
	// Check whether we've made progress on any state, 0 is invalid so not healthy.
	if n.commit == 0 {
		n.debug("Not current, no commits")
		return false
	}

	// Make sure we are the leader or we know we have heard from the leader recently.
	if n.state == Leader {
		return true
	}

	// Check here on catchup status.
	if cs := n.catchup; cs != nil && n.pterm >= cs.cterm && n.pindex >= cs.cindex {
		n.cancelCatchup()
	}

	// Check to see that we have heard from the current leader lately.
	if n.leader != noLeader && n.leader != n.id && n.catchup == nil {
		okInterval := int64(hbInterval) * 2
		ts := time.Now().UnixNano()
		if ps := n.peers[n.leader]; ps == nil || ps.ts == 0 && (ts-ps.ts) > okInterval {
			n.debug("Not current, no recent leader contact")
			return false
		}
	}
	if cs := n.catchup; cs != nil {
		n.debug("Not current, still catching up pindex=%d, cindex=%d", n.pindex, cs.cindex)
	}

	if n.commit == n.applied {
		// At this point if we are current, we can return saying so.
		return true
	} else if !includeForwardProgress {
		// Otherwise, if we aren't allowed to include forward progress
		// (i.e. we are checking "current" instead of "healthy") then
		// give up now.
		return false
	}

	// Otherwise, wait for a short period of time and see if we are making any
	// forward progress.
	if startDelta := n.commit - n.applied; startDelta > 0 {
		for i := 0; i < 10; i++ { // 5ms, in 0.5ms increments
			n.Unlock()
			time.Sleep(time.Millisecond / 2)
			n.Lock()
			if n.commit-n.applied < startDelta {
				// The gap is getting smaller, so we're making forward progress.
				return true
			}
		}
	}

	n.warn("Falling behind in health check, commit %d != applied %d", n.commit, n.applied)
	return false
}

// Current returns if we are the leader for our group or an up to date follower.
func (n *raft) Current() bool {
	if n == nil {
		return false
	}
	n.Lock()
	defer n.Unlock()
	return n.isCurrent(false)
}

// Healthy returns if we are the leader for our group and nearly up-to-date.
func (n *raft) Healthy() bool {
	if n == nil {
		return false
	}
	n.Lock()
	defer n.Unlock()
	return n.isCurrent(true)
}

// HadPreviousLeader indicates if this group ever had a leader.
func (n *raft) HadPreviousLeader() bool {
	n.RLock()
	defer n.RUnlock()
	return n.pleader
}

// GroupLeader returns the current leader of the group.
func (n *raft) GroupLeader() string {
	if n == nil {
		return noLeader
	}
	n.RLock()
	defer n.RUnlock()
	return n.leader
}

// Guess the best next leader. Stepdown will check more thoroughly.
// Lock should be held.
func (n *raft) selectNextLeader() string {
	nextLeader, hli := noLeader, uint64(0)
	for peer, ps := range n.peers {
		if peer == n.id || ps.li <= hli {
			continue
		}
		hli = ps.li
		nextLeader = peer
	}
	return nextLeader
}

// StepDown will have a leader stepdown and optionally do a leader transfer.
func (n *raft) StepDown(preferred ...string) error {
	n.Lock()

	if len(preferred) > 1 {
		n.Unlock()
		return errTooManyPrefs
	}

	if n.state != Leader {
		n.Unlock()
		return errNotLeader
	}

	n.debug("Being asked to stepdown")

	// See if we have up to date followers.
	maybeLeader := noLeader
	if len(preferred) > 0 {
		if preferred[0] != _EMPTY_ {
			maybeLeader = preferred[0]
		} else {
			preferred = nil
		}
	}

	// Can't pick ourselves.
	if maybeLeader == n.id {
		maybeLeader = noLeader
		preferred = nil
	}

	nowts := time.Now().UnixNano()

	// If we have a preferred check it first.
	if maybeLeader != noLeader {
		var isHealthy bool
		if ps, ok := n.peers[maybeLeader]; ok {
			si, ok := n.s.nodeToInfo.Load(maybeLeader)
			isHealthy = ok && !si.(nodeInfo).offline && (nowts-ps.ts) < int64(hbInterval*3)
		}
		if !isHealthy {
			maybeLeader = noLeader
		}
	}

	// If we do not have a preferred at this point pick the first healthy one.
	// Make sure not ourselves.
	if maybeLeader == noLeader {
		for peer, ps := range n.peers {
			if peer == n.id {
				continue
			}
			si, ok := n.s.nodeToInfo.Load(peer)
			isHealthy := ok && !si.(nodeInfo).offline && (nowts-ps.ts) < int64(hbInterval*3)
			if isHealthy {
				maybeLeader = peer
				break
			}
		}
	}

	stepdown := n.stepdown
	prop := n.prop
	n.Unlock()

	if len(preferred) > 0 && maybeLeader == noLeader {
		n.debug("Can not transfer to preferred peer %q", preferred[0])
	}

	// If we have a new leader selected, transfer over to them.
	if maybeLeader != noLeader {
		n.debug("Selected %q for new leader", maybeLeader)
		prop.push(&Entry{EntryLeaderTransfer, []byte(maybeLeader)})
		time.AfterFunc(250*time.Millisecond, func() { stepdown.push(noLeader) })
	} else {
		// Force us to stepdown here.
		n.debug("Stepping down")
		stepdown.push(noLeader)
	}

	return nil
}

// Campaign will have our node start a leadership vote.
func (n *raft) Campaign() error {
	n.Lock()
	defer n.Unlock()
	return n.campaign()
}

func randCampaignTimeout() time.Duration {
	delta := rand.Int63n(int64(maxCampaignTimeout - minCampaignTimeout))
	return (minCampaignTimeout + time.Duration(delta))
}

// Campaign will have our node start a leadership vote.
// Lock should be held.
func (n *raft) campaign() error {
	n.debug("Starting campaign")
	if n.state == Leader {
		return errAlreadyLeader
	}
	n.resetElect(randCampaignTimeout())
	return nil
}

// xferCampaign will have our node start an immediate leadership vote.
// Lock should be held.
func (n *raft) xferCampaign() error {
	n.debug("Starting transfer campaign")
	if n.state == Leader {
		return errAlreadyLeader
	}
	n.resetElect(10 * time.Millisecond)
	return nil
}

// State returns the current state for this node.
func (n *raft) State() RaftState {
	n.RLock()
	defer n.RUnlock()
	return n.state
}

// Progress returns the current index, commit and applied values.
func (n *raft) Progress() (index, commit, applied uint64) {
	n.RLock()
	defer n.RUnlock()
	return n.pindex + 1, n.commit, n.applied
}

// Size returns number of entries and total bytes for our WAL.
func (n *raft) Size() (uint64, uint64) {
	n.RLock()
	var state StreamState
	n.wal.FastState(&state)
	n.RUnlock()
	return state.Msgs, state.Bytes
}

func (n *raft) ID() string {
	if n == nil {
		return _EMPTY_
	}
	n.RLock()
	defer n.RUnlock()
	return n.id
}

func (n *raft) Group() string {
	n.RLock()
	defer n.RUnlock()
	return n.group
}

func (n *raft) Peers() []*Peer {
	n.RLock()
	defer n.RUnlock()

	var peers []*Peer
	for id, ps := range n.peers {
		var lag uint64
		if n.commit > ps.li {
			lag = n.commit - ps.li
		}
		p := &Peer{
			ID:      id,
			Current: id == n.leader || ps.li >= n.applied,
			Last:    time.Unix(0, ps.ts),
			Lag:     lag,
		}
		peers = append(peers, p)
	}
	return peers
}

// Update our known set of peers.
func (n *raft) UpdateKnownPeers(knownPeers []string) {
	n.Lock()
	// If this is a scale up, let the normal add peer logic take precedence.
	// Otherwise if the new peers are slow to start we stall ourselves.
	if len(knownPeers) > len(n.peers) {
		n.Unlock()
		return
	}
	// Process like peer state update.
	ps := &peerState{knownPeers, len(knownPeers), n.extSt}
	n.processPeerState(ps)
	isLeader := n.state == Leader
	n.Unlock()

	// If we are the leader send this update out as well.
	if isLeader {
		n.sendPeerState()
	}
}

func (n *raft) ApplyQ() *ipQueue[*CommittedEntry] { return n.apply }
func (n *raft) LeadChangeC() <-chan bool          { return n.leadc }
func (n *raft) QuitC() <-chan struct{}            { return n.quit }

func (n *raft) Created() time.Time {
	n.RLock()
	defer n.RUnlock()
	return n.created
}

func (n *raft) Stop() {
	n.shutdown(false)
}

func (n *raft) Delete() {
	n.shutdown(true)
}

func (n *raft) shutdown(shouldDelete bool) {
	n.Lock()
	if n.state == Closed {
		n.Unlock()
		return
	}

	close(n.quit)
	if c := n.c; c != nil {
		var subs []*subscription
		c.mu.Lock()
		for _, sub := range c.subs {
			subs = append(subs, sub)
		}
		c.mu.Unlock()
		for _, sub := range subs {
			n.unsubscribe(sub)
		}
		c.closeConnection(InternalClient)
	}
	n.state = Closed
	s, g, wal := n.s, n.group, n.wal

	// Delete our peer state and vote state and any snapshots.
	if shouldDelete {
		os.Remove(filepath.Join(n.sd, peerStateFile))
		os.Remove(filepath.Join(n.sd, termVoteFile))
		os.RemoveAll(filepath.Join(n.sd, snapshotsDir))
	}
	// Unregistering ipQueues do not prevent them from push/pop
	// just will remove them from the central monitoring map
	queues := []interface {
		unregister()
	}{n.reqs, n.votes, n.prop, n.entry, n.resp, n.apply, n.stepdown}
	for _, q := range queues {
		q.unregister()
	}
	n.Unlock()

	s.unregisterRaftNode(g)
	if shouldDelete {
		n.debug("Deleted")
	} else {
		n.debug("Shutdown")
	}
	if wal != nil {
		if shouldDelete {
			wal.Delete()
		} else {
			wal.Stop()
		}
	}
}

// Wipe will force an on disk state reset and then call Delete().
// Useful in case we have been stopped before this point.
func (n *raft) Wipe() {
	n.RLock()
	wal := n.wal
	n.RUnlock()
	// Delete our underlying storage.
	if wal != nil {
		wal.Delete()
	}
	// Now call delete.
	n.Delete()
}

const (
	raftAllSubj        = "$NRG.>"
	raftVoteSubj       = "$NRG.V.%s"
	raftAppendSubj     = "$NRG.AE.%s"
	raftPropSubj       = "$NRG.P.%s"
	raftRemovePeerSubj = "$NRG.RP.%s"
	raftReply          = "$NRG.R.%s"
	raftCatchupReply   = "$NRG.CR.%s"
)

// Lock should be held (due to use of random generator)
func (n *raft) newCatchupInbox() string {
	var b [replySuffixLen]byte
	rn := n.prand.Int63()
	for i, l := 0, rn; i < len(b); i++ {
		b[i] = digits[l%base]
		l /= base
	}
	return fmt.Sprintf(raftCatchupReply, b[:])
}

func (n *raft) newInbox() string {
	var b [replySuffixLen]byte
	rn := n.prand.Int63()
	for i, l := 0, rn; i < len(b); i++ {
		b[i] = digits[l%base]
		l /= base
	}
	return fmt.Sprintf(raftReply, b[:])
}

// Our internal subscribe.
// Lock should be held.
func (n *raft) subscribe(subject string, cb msgHandler) (*subscription, error) {
	return n.s.systemSubscribe(subject, _EMPTY_, false, n.c, cb)
}

// Lock should be held.
func (n *raft) unsubscribe(sub *subscription) {
	if sub != nil {
		n.c.processUnsub(sub.sid)
	}
}

func (n *raft) createInternalSubs() error {
	n.Lock()
	defer n.Unlock()
	n.vsubj, n.vreply = fmt.Sprintf(raftVoteSubj, n.group), n.newInbox()
	n.asubj, n.areply = fmt.Sprintf(raftAppendSubj, n.group), n.newInbox()
	n.psubj = fmt.Sprintf(raftPropSubj, n.group)
	n.rpsubj = fmt.Sprintf(raftRemovePeerSubj, n.group)

	// Votes
	if _, err := n.subscribe(n.vreply, n.handleVoteResponse); err != nil {
		return err
	}
	if _, err := n.subscribe(n.vsubj, n.handleVoteRequest); err != nil {
		return err
	}
	// AppendEntry
	if _, err := n.subscribe(n.areply, n.handleAppendEntryResponse); err != nil {
		return err
	}
	if sub, err := n.subscribe(n.asubj, n.handleAppendEntry); err != nil {
		return err
	} else {
		n.aesub = sub
	}

	return nil
}

func randElectionTimeout() time.Duration {
	delta := rand.Int63n(int64(maxElectionTimeout - minElectionTimeout))
	return (minElectionTimeout + time.Duration(delta))
}

// Lock should be held.
func (n *raft) resetElectionTimeout() {
	n.resetElect(randElectionTimeout())
}

func (n *raft) resetElectionTimeoutWithLock() {
	n.resetElectWithLock(randElectionTimeout())
}

// Lock should be held.
func (n *raft) resetElect(et time.Duration) {
	if n.elect == nil {
		n.elect = time.NewTimer(et)
	} else {
		if !n.elect.Stop() {
			select {
			case <-n.elect.C:
			default:
			}
		}
		n.elect.Reset(et)
	}
}

func (n *raft) resetElectWithLock(et time.Duration) {
	n.Lock()
	n.resetElect(et)
	n.Unlock()
}

func (n *raft) run() {
	s := n.s
	defer s.grWG.Done()

	// We want to wait for some routing to be enabled, so we will wait for
	// at least a route, leaf or gateway connection to be established before
	// starting the run loop.
	gw := s.gateway
	for {
		s.mu.Lock()
		ready := len(s.routes)+len(s.leafs) > 0
		if !ready && gw.enabled {
			gw.RLock()
			ready = len(gw.out)+len(gw.in) > 0
			gw.RUnlock()
		}
		s.mu.Unlock()
		if !ready {
			select {
			case <-s.quitCh:
				return
			case <-time.After(100 * time.Millisecond):
				s.RateLimitWarnf("Waiting for routing to be established...")
			}
		} else {
			break
		}
	}

	for s.isRunning() {
		switch n.State() {
		case Follower:
			n.runAsFollower()
		case Candidate:
			n.runAsCandidate()
		case Leader:
			n.runAsLeader()
		case Observer:
			// TODO(dlc) - fix.
			n.runAsFollower()
		case Closed:
			return
		}
	}
}

func (n *raft) debug(format string, args ...interface{}) {
	if n.dflag {
		nf := fmt.Sprintf("RAFT [%s - %s] %s", n.id, n.group, format)
		n.s.Debugf(nf, args...)
	}
}

func (n *raft) warn(format string, args ...interface{}) {
	nf := fmt.Sprintf("RAFT [%s - %s] %s", n.id, n.group, format)
	n.s.RateLimitWarnf(nf, args...)
}

func (n *raft) error(format string, args ...interface{}) {
	nf := fmt.Sprintf("RAFT [%s - %s] %s", n.id, n.group, format)
	n.s.Errorf(nf, args...)
}

func (n *raft) electTimer() *time.Timer {
	n.RLock()
	defer n.RUnlock()
	return n.elect
}

func (n *raft) IsObserver() bool {
	n.RLock()
	defer n.RUnlock()
	return n.observer
}

// Sets the state to observer only.
func (n *raft) SetObserver(isObserver bool) {
	n.setObserver(isObserver, extUndetermined)
}

func (n *raft) setObserver(isObserver bool, extSt extensionState) {
	n.Lock()
	defer n.Unlock()
	n.observer = isObserver
	n.extSt = extSt
}

// Invoked when being notified that there is something in the entryc's queue
func (n *raft) processAppendEntries() {
	aes := n.entry.pop()
	for _, ae := range aes {
		n.processAppendEntry(ae, ae.sub)
	}
	n.entry.recycle(&aes)
}

func (n *raft) runAsFollower() {
	for {
		elect := n.electTimer()

		select {
		case <-n.entry.ch:
			n.processAppendEntries()
		case <-n.s.quitCh:
			n.shutdown(false)
			return
		case <-n.quit:
			return
		case <-elect.C:
			// If we are out of resources we just want to stay in this state for the moment.
			if n.outOfResources() {
				n.resetElectionTimeoutWithLock()
				n.debug("Not switching to candidate, no resources")
			} else if n.IsObserver() {
				n.resetElectWithLock(48 * time.Hour)
				n.debug("Not switching to candidate, observer only")
			} else if n.isCatchingUp() {
				n.debug("Not switching to candidate, catching up")
			} else {
				n.switchToCandidate()
				return
			}
		case <-n.votes.ch:
			n.debug("Ignoring old vote response, we have stepped down")
			n.votes.popOne()
		case <-n.resp.ch:
			// Ignore
			n.resp.popOne()
		case <-n.reqs.ch:
			// Because of drain() it is possible that we get nil from popOne().
			if voteReq, ok := n.reqs.popOne(); ok {
				n.processVoteRequest(voteReq)
			}
		case <-n.stepdown.ch:
			if newLeader, ok := n.stepdown.popOne(); ok {
				n.switchToFollower(newLeader)
				return
			}
		}
	}
}

// CommitEntry is handed back to the user to apply a commit to their FSM.
type CommittedEntry struct {
	Index   uint64
	Entries []*Entry
}

// appendEntry is the main struct that is used to sync raft peers.
type appendEntry struct {
	leader  string
	term    uint64
	commit  uint64
	pterm   uint64
	pindex  uint64
	entries []*Entry
	// internal use only.
	reply string
	sub   *subscription
	buf   []byte
}

type EntryType uint8

const (
	EntryNormal EntryType = iota
	EntryOldSnapshot
	EntryPeerState
	EntryAddPeer
	EntryRemovePeer
	EntryLeaderTransfer
	EntrySnapshot
)

func (t EntryType) String() string {
	switch t {
	case EntryNormal:
		return "Normal"
	case EntryOldSnapshot:
		return "OldSnapshot"
	case EntryPeerState:
		return "PeerState"
	case EntryAddPeer:
		return "AddPeer"
	case EntryRemovePeer:
		return "RemovePeer"
	case EntryLeaderTransfer:
		return "LeaderTransfer"
	case EntrySnapshot:
		return "Snapshot"
	}
	return fmt.Sprintf("Unknown [%d]", uint8(t))
}

type Entry struct {
	Type EntryType
	Data []byte
}

func (ae *appendEntry) String() string {
	return fmt.Sprintf("&{leader:%s term:%d commit:%d pterm:%d pindex:%d entries: %d}",
		ae.leader, ae.term, ae.commit, ae.pterm, ae.pindex, len(ae.entries))
}

const appendEntryBaseLen = idLen + 4*8 + 2

func (ae *appendEntry) encode(b []byte) ([]byte, error) {
	if ll := len(ae.leader); ll != idLen && ll != 0 {
		return nil, errLeaderLen
	}
	if len(ae.entries) > math.MaxUint16 {
		return nil, errTooManyEntries
	}

	var elen int
	for _, e := range ae.entries {
		elen += len(e.Data) + 1 + 4 // 1 is type, 4 is for size.
	}
	tlen := appendEntryBaseLen + elen + 1

	var buf []byte
	if cap(b) >= tlen {
		buf = b[:tlen]
	} else {
		buf = make([]byte, tlen)
	}

	var le = binary.LittleEndian
	copy(buf[:idLen], ae.leader)
	le.PutUint64(buf[8:], ae.term)
	le.PutUint64(buf[16:], ae.commit)
	le.PutUint64(buf[24:], ae.pterm)
	le.PutUint64(buf[32:], ae.pindex)
	le.PutUint16(buf[40:], uint16(len(ae.entries)))
	wi := 42
	for _, e := range ae.entries {
		le.PutUint32(buf[wi:], uint32(len(e.Data)+1))
		wi += 4
		buf[wi] = byte(e.Type)
		wi++
		copy(buf[wi:], e.Data)
		wi += len(e.Data)
	}
	return buf[:wi], nil
}

// This can not be used post the wire level callback since we do not copy.
func (n *raft) decodeAppendEntry(msg []byte, sub *subscription, reply string) (*appendEntry, error) {
	if len(msg) < appendEntryBaseLen {
		return nil, errBadAppendEntry
	}

	var le = binary.LittleEndian
	ae := &appendEntry{
		leader: string(msg[:idLen]),
		term:   le.Uint64(msg[8:]),
		commit: le.Uint64(msg[16:]),
		pterm:  le.Uint64(msg[24:]),
		pindex: le.Uint64(msg[32:]),
		sub:    sub,
		reply:  reply,
	}
	// Decode Entries.
	ne, ri := int(le.Uint16(msg[40:])), 42
	for i, max := 0, len(msg); i < ne; i++ {
		if ri >= max-1 {
			return nil, errBadAppendEntry
		}
		le := int(le.Uint32(msg[ri:]))
		ri += 4
		if le <= 0 || ri+le > max {
			return nil, errBadAppendEntry
		}
		etype := EntryType(msg[ri])
		ae.entries = append(ae.entries, &Entry{etype, msg[ri+1 : ri+le]})
		ri += le
	}
	ae.buf = msg
	return ae, nil
}

// appendEntryResponse is our response to a received appendEntry.
type appendEntryResponse struct {
	term    uint64
	index   uint64
	peer    string
	success bool
	// internal
	reply string
}

// We want to make sure this does not change from system changing length of syshash.
const idLen = 8
const appendEntryResponseLen = 24 + 1

func (ar *appendEntryResponse) encode(b []byte) []byte {
	var buf []byte
	if cap(b) >= appendEntryResponseLen {
		buf = b[:appendEntryResponseLen]
	} else {
		buf = make([]byte, appendEntryResponseLen)
	}
	var le = binary.LittleEndian
	le.PutUint64(buf[0:], ar.term)
	le.PutUint64(buf[8:], ar.index)
	copy(buf[16:16+idLen], ar.peer)
	if ar.success {
		buf[24] = 1
	} else {
		buf[24] = 0
	}
	return buf[:appendEntryResponseLen]
}

func (n *raft) decodeAppendEntryResponse(msg []byte) *appendEntryResponse {
	if len(msg) != appendEntryResponseLen {
		return nil
	}
	var le = binary.LittleEndian
	ar := &appendEntryResponse{
		term:  le.Uint64(msg[0:]),
		index: le.Uint64(msg[8:]),
		peer:  string(msg[16 : 16+idLen]),
	}
	ar.success = msg[24] == 1
	return ar
}

// Called when a remove peer proposal has been forwarded
func (n *raft) handleForwardedRemovePeerProposal(sub *subscription, c *client, _ *Account, _, reply string, msg []byte) {
	n.debug("Received forwarded remove peer proposal: %q", msg)

	if !n.Leader() {
		n.debug("Ignoring forwarded peer removal proposal, not leader")
		return
	}
	if len(msg) != idLen {
		n.warn("Received invalid peer name for remove proposal: %q", msg)
		return
	}
	// Need to copy since this is underlying client/route buffer.
	peer := string(copyBytes(msg))

	n.RLock()
	prop, werr := n.prop, n.werr
	n.RUnlock()

	// Ignore if we have had a write error previous.
	if werr != nil {
		return
	}

	prop.push(&Entry{EntryRemovePeer, []byte(peer)})
}

// Called when a peer has forwarded a proposal.
func (n *raft) handleForwardedProposal(sub *subscription, c *client, _ *Account, _, reply string, msg []byte) {
	if !n.Leader() {
		n.debug("Ignoring forwarded proposal, not leader")
		return
	}
	// Need to copy since this is underlying client/route buffer.
	msg = copyBytes(msg)

	n.RLock()
	prop, werr := n.prop, n.werr
	n.RUnlock()

	// Ignore if we have had a write error previous.
	if werr != nil {
		return
	}

	prop.push(&Entry{EntryNormal, msg})
}

func (n *raft) runAsLeader() {
	n.RLock()
	if n.state == Closed {
		n.RUnlock()
		return
	}
	psubj, rpsubj := n.psubj, n.rpsubj
	n.RUnlock()

	// For forwarded proposals, both normal and remove peer proposals.
	fsub, err := n.subscribe(psubj, n.handleForwardedProposal)
	if err != nil {
		n.debug("Error subscribing to forwarded proposals: %v", err)
		return
	}
	rpsub, err := n.subscribe(rpsubj, n.handleForwardedRemovePeerProposal)
	if err != nil {
		n.debug("Error subscribing to forwarded proposals: %v", err)
		return
	}

	// Cleanup our subscription when we leave.
	defer func() {
		n.Lock()
		n.unsubscribe(fsub)
		n.unsubscribe(rpsub)
		n.Unlock()
	}()

	// To send out our initial peer state.
	n.sendPeerState()

	hb := time.NewTicker(hbInterval)
	defer hb.Stop()

	lq := time.NewTicker(lostQuorumCheck)
	defer lq.Stop()

	for {
		select {
		case <-n.s.quitCh:
			n.shutdown(false)
			return
		case <-n.quit:
			return
		case <-n.resp.ch:
			ars := n.resp.pop()
			for _, ar := range ars {
				n.processAppendEntryResponse(ar)
			}
			n.resp.recycle(&ars)
		case <-n.prop.ch:
			const maxBatch = 256 * 1024
			var entries []*Entry

			es := n.prop.pop()
			sz := 0
			for i, b := range es {
				if b.Type == EntryRemovePeer {
					n.doRemovePeerAsLeader(string(b.Data))
				}
				entries = append(entries, b)
				sz += len(b.Data) + 1
				if i != len(es)-1 && sz < maxBatch && len(entries) < math.MaxUint16 {
					continue
				}
				n.sendAppendEntry(entries)
				// We need to re-create `entries` because there is a reference
				// to it in the node's pae map.
				entries = nil
			}
			n.prop.recycle(&es)
		case <-hb.C:
			if n.notActive() {
				n.sendHeartbeat()
			}
		case <-lq.C:
			if n.lostQuorum() {
				n.switchToFollower(noLeader)
				return
			}
		case <-n.votes.ch:
			// Because of drain() it is possible that we get nil from popOne().
			vresp, ok := n.votes.popOne()
			if !ok {
				continue
			}
			if vresp.term > n.Term() {
				n.switchToFollower(noLeader)
				return
			}
			n.trackPeer(vresp.peer)
		case <-n.reqs.ch:
			// Because of drain() it is possible that we get nil from popOne().
			if voteReq, ok := n.reqs.popOne(); ok {
				n.processVoteRequest(voteReq)
			}
		case <-n.stepdown.ch:
			if newLeader, ok := n.stepdown.popOne(); ok {
				n.switchToFollower(newLeader)
				return
			}
		case <-n.entry.ch:
			n.processAppendEntries()
		}
	}
}

// Quorum reports the quorum status. Will be called on former leaders.
func (n *raft) Quorum() bool {
	n.RLock()
	defer n.RUnlock()

	now, nc := time.Now().UnixNano(), 1
	for _, peer := range n.peers {
		if now-peer.ts < int64(lostQuorumInterval) {
			nc++
			if nc >= n.qn {
				return true
			}
		}
	}
	return false
}

func (n *raft) lostQuorum() bool {
	n.RLock()
	defer n.RUnlock()
	return n.lostQuorumLocked()
}

func (n *raft) lostQuorumLocked() bool {
	// Make sure we let any scale up actions settle before deciding.
	if !n.lsut.IsZero() && time.Since(n.lsut) < lostQuorumInterval {
		return false
	}

	now, nc := time.Now().UnixNano(), 1
	for _, peer := range n.peers {
		if now-peer.ts < int64(lostQuorumInterval) {
			nc++
			if nc >= n.qn {
				return false
			}
		}
	}
	return true
}

// Check for being not active in terms of sending entries.
// Used in determining if we need to send a heartbeat.
func (n *raft) notActive() bool {
	n.RLock()
	defer n.RUnlock()
	return time.Since(n.active) > hbInterval
}

// Return our current term.
func (n *raft) Term() uint64 {
	n.RLock()
	defer n.RUnlock()
	return n.term
}

// Lock should be held.
func (n *raft) loadFirstEntry() (ae *appendEntry, err error) {
	var state StreamState
	n.wal.FastState(&state)
	return n.loadEntry(state.FirstSeq)
}

func (n *raft) runCatchup(ar *appendEntryResponse, indexUpdatesQ *ipQueue[uint64]) {
	n.RLock()
	s, reply := n.s, n.areply
	peer, subj, last := ar.peer, ar.reply, n.pindex
	n.RUnlock()

	defer s.grWG.Done()

	defer func() {
		n.Lock()
		delete(n.progress, peer)
		if len(n.progress) == 0 {
			n.progress = nil
		}
		// Check if this is a new peer and if so go ahead and propose adding them.
		_, exists := n.peers[peer]
		n.Unlock()
		if !exists {
			n.debug("Catchup done for %q, will add into peers", peer)
			n.ProposeAddPeer(peer)
		}
		indexUpdatesQ.unregister()
	}()

	n.debug("Running catchup for %q", peer)

	const maxOutstanding = 2 * 1024 * 1024 // 2MB for now.
	next, total, om := uint64(0), 0, make(map[uint64]int)

	sendNext := func() bool {
		for total <= maxOutstanding {
			next++
			if next > last {
				return true
			}
			ae, err := n.loadEntry(next)
			if err != nil {
				if err != ErrStoreEOF {
					n.warn("Got an error loading %d index: %v", next, err)
				}
				return true
			}
			// Update our tracking total.
			om[next] = len(ae.buf)
			total += len(ae.buf)
			n.sendRPC(subj, reply, ae.buf)
		}
		return false
	}

	const activityInterval = 2 * time.Second
	timeout := time.NewTimer(activityInterval)
	defer timeout.Stop()

	stepCheck := time.NewTicker(100 * time.Millisecond)
	defer stepCheck.Stop()

	// Run as long as we are leader and still not caught up.
	for n.Leader() {
		select {
		case <-n.s.quitCh:
			n.shutdown(false)
			return
		case <-n.quit:
			return
		case <-stepCheck.C:
			if !n.Leader() {
				n.debug("Catching up canceled, no longer leader")
				return
			}
		case <-timeout.C:
			n.debug("Catching up for %q stalled", peer)
			return
		case <-indexUpdatesQ.ch:
			if index, ok := indexUpdatesQ.popOne(); ok {
				// Update our activity timer.
				timeout.Reset(activityInterval)
				// Update outstanding total.
				total -= om[index]
				delete(om, index)
				if next == 0 {
					next = index
				}
				// Check if we are done.
				if index > last || sendNext() {
					n.debug("Finished catching up")
					return
				}
			}
		}
	}
}

// Lock should be held.
func (n *raft) sendSnapshotToFollower(subject string) (uint64, error) {
	snap, err := n.loadLastSnapshot()
	if err != nil {
		// We need to stepdown here when this happens.
		n.stepdown.push(noLeader)
		return 0, err
	}
	// Go ahead and send the snapshot and peerstate here as first append entry to the catchup follower.
	ae := n.buildAppendEntry([]*Entry{{EntrySnapshot, snap.data}, {EntryPeerState, snap.peerstate}})
	ae.pterm, ae.pindex = snap.lastTerm, snap.lastIndex
	var state StreamState
	n.wal.FastState(&state)

	fpIndex := state.FirstSeq - 1
	if snap.lastIndex < fpIndex && state.FirstSeq != 0 {
		snap.lastIndex = fpIndex
		ae.pindex = fpIndex
	}

	encoding, err := ae.encode(nil)
	if err != nil {
		return 0, err
	}
	n.sendRPC(subject, n.areply, encoding)
	return snap.lastIndex, nil
}

func (n *raft) catchupFollower(ar *appendEntryResponse) {
	n.debug("Being asked to catch up follower: %q", ar.peer)
	n.Lock()
	if n.progress == nil {
		n.progress = make(map[string]*ipQueue[uint64])
	} else if q, ok := n.progress[ar.peer]; ok {
		n.debug("Will cancel existing entry for catching up %q", ar.peer)
		delete(n.progress, ar.peer)
		q.push(n.pindex)
	}

	// Check to make sure we have this entry.
	start := ar.index + 1
	var state StreamState
	n.wal.FastState(&state)

	if start < state.FirstSeq || (state.Msgs == 0 && start <= state.LastSeq) {
		n.debug("Need to send snapshot to follower")
		if lastIndex, err := n.sendSnapshotToFollower(ar.reply); err != nil {
			n.error("Error sending snapshot to follower [%s]: %v", ar.peer, err)
			n.Unlock()
			return
		} else {
			start = lastIndex + 1
			// If no other entries, we can just return here.
			if state.Msgs == 0 || start > state.LastSeq {
				n.debug("Finished catching up")
				n.Unlock()
				return
			}
			n.debug("Snapshot sent, reset first catchup entry to %d", lastIndex)
		}
	}

	ae, err := n.loadEntry(start)
	if err != nil {
		n.warn("Request from follower for entry at index [%d] errored for state %+v - %v", start, state, err)
		ae, err = n.loadFirstEntry()
	}
	if err != nil || ae == nil {
		n.warn("Could not find a starting entry for catchup request: %v", err)
		n.Unlock()
		return
	}
	if ae.pindex != ar.index || ae.pterm != ar.term {
		n.debug("Our first entry [%d:%d] does not match request from follower [%d:%d]", ae.pterm, ae.pindex, ar.term, ar.index)
	}
	// Create a queue for delivering updates from responses.
	indexUpdates := newIPQueue[uint64](n.s, fmt.Sprintf("[ACC:%s] RAFT '%s' indexUpdates", n.accName, n.group))
	indexUpdates.push(ae.pindex)
	n.progress[ar.peer] = indexUpdates
	n.Unlock()

	n.s.startGoRoutine(func() { n.runCatchup(ar, indexUpdates) })
}

func (n *raft) loadEntry(index uint64) (*appendEntry, error) {
	var smp StoreMsg
	sm, err := n.wal.LoadMsg(index, &smp)
	if err != nil {
		return nil, err
	}
	return n.decodeAppendEntry(sm.msg, nil, _EMPTY_)
}

// applyCommit will update our commit index and apply the entry to the apply chan.
// lock should be held.
func (n *raft) applyCommit(index uint64) error {
	if n.state == Closed {
		return errNodeClosed
	}
	if index <= n.commit {
		n.debug("Ignoring apply commit for %d, already processed", index)
		return nil
	}
	original := n.commit
	n.commit = index

	if n.state == Leader {
		delete(n.acks, index)
	}

	var fpae bool

	ae := n.pae[index]
	if ae == nil {
		var state StreamState
		n.wal.FastState(&state)
		if index < state.FirstSeq {
			return nil
		}
		var err error
		if ae, err = n.loadEntry(index); err != nil {
			if err != ErrStoreClosed && err != ErrStoreEOF {
				if err == errBadMsg {
					n.warn("Got an error loading %d index: %v - will reset", index, err)
					n.resetWAL()
				} else {
					n.warn("Got an error loading %d index: %v", index, err)
				}
			}
			n.commit = original
			return errEntryLoadFailed
		}
	} else {
		fpae = true
	}

	ae.buf = nil

	var committed []*Entry
	for _, e := range ae.entries {
		switch e.Type {
		case EntryNormal:
			committed = append(committed, e)
		case EntryOldSnapshot:
			// For old snapshots in our WAL.
			committed = append(committed, &Entry{EntrySnapshot, e.Data})
		case EntrySnapshot:
			committed = append(committed, e)
		case EntryPeerState:
			if n.state != Leader {
				if ps, err := decodePeerState(e.Data); err == nil {
					n.processPeerState(ps)
				}
			}
		case EntryAddPeer:
			newPeer := string(e.Data)
			n.debug("Added peer %q", newPeer)

			// If we were on the removed list reverse that here.
			if n.removed != nil {
				delete(n.removed, newPeer)
			}

			if lp, ok := n.peers[newPeer]; !ok {
				// We are not tracking this one automatically so we need to bump cluster size.
				n.peers[newPeer] = &lps{time.Now().UnixNano(), 0, true}
			} else {
				// Mark as added.
				lp.kp = true
			}
			// Adjust cluster size and quorum if needed.
			n.adjustClusterSizeAndQuorum()
			// Write out our new state.
			n.writePeerState(&peerState{n.peerNames(), n.csz, n.extSt})
			// We pass these up as well.
			committed = append(committed, e)

		case EntryRemovePeer:
			peer := string(e.Data)
			n.debug("Removing peer %q", peer)

			// Make sure we have our removed map.
			if n.removed == nil {
				n.removed = make(map[string]struct{})
			}
			n.removed[peer] = struct{}{}

			if _, ok := n.peers[peer]; ok {
				delete(n.peers, peer)
				// We should decrease our cluster size since we are tracking this peer.
				n.adjustClusterSizeAndQuorum()
				// Write out our new state.
				n.writePeerState(&peerState{n.peerNames(), n.csz, n.extSt})
			}

			// If this is us and we are the leader we should attempt to stepdown.
			if peer == n.id && n.state == Leader {
				n.stepdown.push(n.selectNextLeader())
			}

			// We pass these up as well.
			committed = append(committed, e)
		}
	}
	// Pass to the upper layers if we have normal entries.
	if len(committed) > 0 {
		if fpae {
			delete(n.pae, index)
		}
		n.apply.push(&CommittedEntry{index, committed})
	} else {
		// If we processed inline update our applied index.
		n.applied = index
	}
	return nil
}

// Used to track a success response and apply entries.
func (n *raft) trackResponse(ar *appendEntryResponse) {
	n.Lock()
	if n.state == Closed {
		n.Unlock()
		return
	}

	// Update peer's last index.
	if ps := n.peers[ar.peer]; ps != nil && ar.index > ps.li {
		ps.li = ar.index
	}

	// If we are tracking this peer as a catchup follower, update that here.
	if indexUpdateQ := n.progress[ar.peer]; indexUpdateQ != nil {
		indexUpdateQ.push(ar.index)
	}

	// Ignore items already committed.
	if ar.index <= n.commit {
		n.Unlock()
		return
	}

	// See if we have items to apply.
	var sendHB bool

	if results := n.acks[ar.index]; results != nil {
		results[ar.peer] = struct{}{}
		if nr := len(results); nr >= n.qn {
			// We have a quorum.
			for index := n.commit + 1; index <= ar.index; index++ {
				if err := n.applyCommit(index); err != nil && err != errNodeClosed {
					n.error("Got an error applying commit for %d: %v", index, err)
					break
				}
			}
			sendHB = n.prop.len() == 0
		}
	}
	n.Unlock()

	if sendHB {
		n.sendHeartbeat()
	}
}

// Used to adjust cluster size and peer count based on added official peers.
// lock should be held.
func (n *raft) adjustClusterSizeAndQuorum() {
	pcsz, ncsz := n.csz, 0
	for _, peer := range n.peers {
		if peer.kp {
			ncsz++
		}
	}
	n.csz = ncsz
	n.qn = n.csz/2 + 1

	if ncsz > pcsz {
		n.debug("Expanding our clustersize: %d -> %d", pcsz, ncsz)
		n.lsut = time.Now()
	} else if ncsz < pcsz {
		n.debug("Decreasing our clustersize: %d -> %d", pcsz, ncsz)
		if n.state == Leader {
			go n.sendHeartbeat()
		}
	}
}

// Track interactions with this peer.
func (n *raft) trackPeer(peer string) error {
	n.Lock()
	var needPeerAdd, isRemoved bool
	if n.removed != nil {
		_, isRemoved = n.removed[peer]
	}
	if n.state == Leader {
		if lp, ok := n.peers[peer]; !ok || !lp.kp {
			// Check if this peer had been removed previously.
			needPeerAdd = !isRemoved
		}
	}
	if ps := n.peers[peer]; ps != nil {
		ps.ts = time.Now().UnixNano()
	} else if !isRemoved {
		n.peers[peer] = &lps{time.Now().UnixNano(), 0, false}
	}
	n.Unlock()

	if needPeerAdd {
		n.ProposeAddPeer(peer)
	}
	return nil
}

func (n *raft) runAsCandidate() {
	n.Lock()
	// Drain old responses.
	n.votes.drain()
	n.Unlock()

	// Send out our request for votes.
	n.requestVote()

	// We vote for ourselves.
	votes := 1

	for {
		elect := n.electTimer()
		select {
		case <-n.entry.ch:
			n.processAppendEntries()
		case <-n.resp.ch:
			// Ignore
			n.resp.popOne()
		case <-n.s.quitCh:
			n.shutdown(false)
			return
		case <-n.quit:
			return
		case <-elect.C:
			n.switchToCandidate()
			return
		case <-n.votes.ch:
			// Because of drain() it is possible that we get nil from popOne().
			vresp, ok := n.votes.popOne()
			if !ok {
				continue
			}
			n.RLock()
			nterm := n.term
			n.RUnlock()

			if vresp.granted && nterm >= vresp.term {
				// only track peers that would be our followers
				n.trackPeer(vresp.peer)
				votes++
				if n.wonElection(votes) {
					// Become LEADER if we have won and gotten a quorum with everyone we should hear from.
					n.switchToLeader()
					return
				}
			} else if vresp.term > nterm {
				// if we observe a bigger term, we should start over again or risk forming a quorum fully knowing
				// someone with a better term exists. This is even the right thing to do if won == true.
				n.Lock()
				n.debug("Stepping down from candidate, detected higher term: %d vs %d", vresp.term, n.term)
				n.term = vresp.term
				n.vote = noVote
				n.writeTermVote()
				n.stepdown.push(noLeader)
				n.lxfer = false
				n.Unlock()
			}
		case <-n.reqs.ch:
			// Because of drain() it is possible that we get nil from popOne().
			if voteReq, ok := n.reqs.popOne(); ok {
				n.processVoteRequest(voteReq)
			}
		case <-n.stepdown.ch:
			if newLeader, ok := n.stepdown.popOne(); ok {
				n.switchToFollower(newLeader)
				return
			}
		}
	}
}

// handleAppendEntry handles an append entry from the wire.
func (n *raft) handleAppendEntry(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
	if n.outOfResources() {
		n.debug("AppendEntry not processing inbound, no resources")
		return
	}

	msg = copyBytes(msg)
	if ae, err := n.decodeAppendEntry(msg, sub, reply); err == nil {
		n.entry.push(ae)
	} else {
		n.warn("AppendEntry failed to be placed on internal channel: corrupt entry")
	}
}

// Lock should be held.
func (n *raft) cancelCatchup() {
	n.debug("Canceling catchup subscription since we are now up to date")

	if n.catchup != nil && n.catchup.sub != nil {
		n.unsubscribe(n.catchup.sub)
	}
	n.catchup = nil
}

// catchupStalled will try to determine if we are stalled. This is called
// on a new entry from our leader.
// Lock should be held.
func (n *raft) catchupStalled() bool {
	if n.catchup == nil {
		return false
	}
	if n.catchup.pindex == n.pindex {
		return time.Since(n.catchup.active) > 2*time.Second
	}
	n.catchup.pindex = n.pindex
	n.catchup.active = time.Now()
	return false
}

// Lock should be held.
func (n *raft) createCatchup(ae *appendEntry) string {
	// Cleanup any old ones.
	if n.catchup != nil && n.catchup.sub != nil {
		n.unsubscribe(n.catchup.sub)
	}
	// Snapshot term and index.
	n.catchup = &catchupState{
		cterm:  ae.pterm,
		cindex: ae.pindex,
		pterm:  n.pterm,
		pindex: n.pindex,
		active: time.Now(),
	}
	inbox := n.newCatchupInbox()
	sub, _ := n.subscribe(inbox, n.handleAppendEntry)
	n.catchup.sub = sub

	return inbox
}

// Truncate our WAL and reset.
// Lock should be held.
func (n *raft) truncateWAL(term, index uint64) {
	n.debug("Truncating and repairing WAL to Term %d Index %d", term, index)

	if term == 0 && index == 0 {
		n.warn("Resetting WAL state")
	}

	defer func() {
		// Check to see if we invalidated any snapshots that might have held state
		// from the entries we are truncating.
		if snap, _ := n.loadLastSnapshot(); snap != nil && snap.lastIndex >= index {
			os.Remove(n.snapfile)
			n.snapfile = _EMPTY_
		}
		// Make sure to reset commit and applied if above
		if n.commit > n.pindex {
			n.commit = n.pindex
		}
		if n.applied > n.commit {
			n.applied = n.commit
		}
	}()

	if err := n.wal.Truncate(index); err != nil {
		// If we get an invalid sequence, reset our wal all together.
		if err == ErrInvalidSequence {
			n.debug("Resetting WAL")
			n.wal.Truncate(0)
			index, n.pterm, n.pindex = 0, 0, 0
		} else {
			n.warn("Error truncating WAL: %v", err)
			n.setWriteErrLocked(err)
		}
		return
	}

	// Set after we know we have truncated properly.
	n.pterm, n.pindex = term, index
}

// Reset our WAL.
// Lock should be held.
func (n *raft) resetWAL() {
	n.truncateWAL(0, 0)
}

// Lock should be held
func (n *raft) updateLeader(newLeader string) {
	n.leader = newLeader
	if !n.pleader && newLeader != noLeader {
		n.pleader = true
	}
}

// processAppendEntry will process an appendEntry.
func (n *raft) processAppendEntry(ae *appendEntry, sub *subscription) {
	n.Lock()
	// Don't reset here if we have been asked to assume leader position.
	if !n.lxfer {
		n.resetElectionTimeout()
	}

	// Just return if closed or we had previous write error.
	if n.state == Closed || n.werr != nil {
		n.Unlock()
		return
	}

	// Scratch buffer for responses.
	var scratch [appendEntryResponseLen]byte
	arbuf := scratch[:]

	// Are we receiving from another leader.
	if n.state == Leader {
		if ae.term > n.term {
			n.term = ae.term
			n.vote = noVote
			n.writeTermVote()
			n.debug("Received append entry from another leader, stepping down to %q", ae.leader)
			n.stepdown.push(ae.leader)
		} else {
			// Let them know we are the leader.
			ar := &appendEntryResponse{n.term, n.pindex, n.id, false, _EMPTY_}
			n.debug("AppendEntry ignoring old term from another leader")
			n.sendRPC(ae.reply, _EMPTY_, ar.encode(arbuf))
		}
		// Always return here from processing.
		n.Unlock()
		return
	}

	// If we received an append entry as a candidate we should convert to a follower.
	if n.state == Candidate {
		n.debug("Received append entry in candidate state from %q, converting to follower", ae.leader)
		if n.term < ae.term {
			n.term = ae.term
			n.vote = noVote
			n.writeTermVote()
		}
		n.stepdown.push(ae.leader)
	}

	// Catching up state.
	catchingUp := n.catchup != nil
	// Is this a new entry?
	isNew := sub != nil && sub == n.aesub

	// Track leader directly
	if isNew && ae.leader != noLeader {
		if ps := n.peers[ae.leader]; ps != nil {
			ps.ts = time.Now().UnixNano()
		} else {
			n.peers[ae.leader] = &lps{time.Now().UnixNano(), 0, true}
		}
	}

	// If we are catching up ignore old catchup subs.
	// This could happen when we stall or cancel a catchup.
	if !isNew && catchingUp && sub != n.catchup.sub {
		n.Unlock()
		n.debug("AppendEntry ignoring old entry from previous catchup")
		return
	}

	// Check state if we are catching up.
	if catchingUp {
		if cs := n.catchup; cs != nil && n.pterm >= cs.cterm && n.pindex >= cs.cindex {
			// If we are here we are good, so if we have a catchup pending we can cancel.
			n.cancelCatchup()
			// Reset our notion of catching up.
			catchingUp = false
		} else if isNew {
			var ar *appendEntryResponse
			var inbox string
			// Check to see if we are stalled. If so recreate our catchup state and resend response.
			if n.catchupStalled() {
				n.debug("Catchup may be stalled, will request again")
				inbox = n.createCatchup(ae)
				ar = &appendEntryResponse{n.pterm, n.pindex, n.id, false, _EMPTY_}
			}
			n.Unlock()
			if ar != nil {
				n.sendRPC(ae.reply, inbox, ar.encode(arbuf))
			}
			// Ignore new while catching up or replaying.
			return
		}
	}

	// If this term is greater than ours.
	if ae.term > n.term {
		n.pterm = ae.pterm
		n.term = ae.term
		n.vote = noVote
		if isNew {
			n.writeTermVote()
		}
		if n.state != Follower {
			n.debug("Term higher than ours and we are not a follower: %v, stepping down to %q", n.state, ae.leader)
			n.stepdown.push(ae.leader)
		}
	}

	if isNew && n.leader != ae.leader && n.state == Follower {
		n.debug("AppendEntry updating leader to %q", ae.leader)
		n.updateLeader(ae.leader)
		n.writeTermVote()
		n.resetElectionTimeout()
		n.updateLeadChange(false)
	}

	if (isNew && ae.pterm != n.pterm) || ae.pindex != n.pindex {
		// Check if this is a lower or equal index than what we were expecting.
		if ae.pindex <= n.pindex {
			n.debug("AppendEntry detected pindex less than ours: %d:%d vs %d:%d", ae.pterm, ae.pindex, n.pterm, n.pindex)
			var ar *appendEntryResponse

			var success bool
			if eae, _ := n.loadEntry(ae.pindex); eae == nil {
				n.resetWAL()
			} else {
				// If terms mismatched, or we got an error loading, delete that entry and all others past it.
				// Make sure to cancel any catchups in progress.
				// Truncate will reset our pterm and pindex. Only do so if we have an entry.
				n.truncateWAL(ae.pterm, ae.pindex)
			}
			// Cancel regardless.
			n.cancelCatchup()

			// Create response.
			ar = &appendEntryResponse{ae.pterm, ae.pindex, n.id, success, _EMPTY_}
			n.Unlock()
			n.sendRPC(ae.reply, _EMPTY_, ar.encode(arbuf))
			return
		}

		// Check if we are catching up. If we are here we know the leader did not have all of the entries
		// so make sure this is a snapshot entry. If it is not start the catchup process again since it
		// means we may have missed additional messages.
		if catchingUp {
			// Check if only our terms do not match here.
			if ae.pindex == n.pindex {
				// Make sure pterms match and we take on the leader's.
				// This prevents constant spinning.
				n.truncateWAL(ae.pterm, ae.pindex)
				n.cancelCatchup()
				n.Unlock()
				return
			}
			// This means we already entered into a catchup state but what the leader sent us did not match what we expected.
			// Snapshots and peerstate will always be together when a leader is catching us up in this fashion.
			if len(ae.entries) != 2 || ae.entries[0].Type != EntrySnapshot || ae.entries[1].Type != EntryPeerState {
				n.warn("Expected first catchup entry to be a snapshot and peerstate, will retry")
				n.cancelCatchup()
				n.Unlock()
				return
			}

			if ps, err := decodePeerState(ae.entries[1].Data); err == nil {
				n.processPeerState(ps)
				// Also need to copy from client's buffer.
				ae.entries[0].Data = copyBytes(ae.entries[0].Data)
			} else {
				n.warn("Could not parse snapshot peerstate correctly")
				n.cancelCatchup()
				n.Unlock()
				return
			}

			n.pindex = ae.pindex
			n.pterm = ae.pterm
			n.commit = ae.pindex

			if _, err := n.wal.Compact(n.pindex + 1); err != nil {
				n.setWriteErrLocked(err)
				n.Unlock()
				return
			}

			// Now send snapshot to upper levels. Only send the snapshot, not the peerstate entry.
			n.apply.push(&CommittedEntry{n.commit, ae.entries[:1]})
			n.Unlock()
			return

		} else {
			n.debug("AppendEntry did not match %d %d with %d %d", ae.pterm, ae.pindex, n.pterm, n.pindex)
			// Reset our term.
			n.term = n.pterm
			if ae.pindex > n.pindex {
				// Setup our state for catching up.
				inbox := n.createCatchup(ae)
				ar := &appendEntryResponse{n.pterm, n.pindex, n.id, false, _EMPTY_}
				n.Unlock()
				n.sendRPC(ae.reply, inbox, ar.encode(arbuf))
				return
			}
		}
	}

	// Save to our WAL if we have entries.
	if ae.shouldStore() {
		// Only store if an original which will have sub != nil
		if sub != nil {
			if err := n.storeToWAL(ae); err != nil {
				if err != ErrStoreClosed {
					n.warn("Error storing entry to WAL: %v", err)
				}
				n.Unlock()
				return
			}
			// Save in memory for faster processing during applyCommit.
			// Only save so many however to avoid memory bloat.
			if l := len(n.pae); l <= paeDropThreshold {
				n.pae[n.pindex], l = ae, l+1
				if l > paeWarnThreshold && l%paeWarnModulo == 0 {
					n.warn("%d append entries pending", len(n.pae))
				}
			} else {
				n.debug("Not saving to append entries pending")
			}
		} else {
			// This is a replay on startup so just take the appendEntry version.
			n.pterm = ae.term
			n.pindex = ae.pindex + 1
		}
	}

	// Check to see if we have any related entries to process here.
	for _, e := range ae.entries {
		switch e.Type {
		case EntryLeaderTransfer:
			// Only process these if they are new, so no replays or catchups.
			if isNew {
				maybeLeader := string(e.Data)
				if maybeLeader == n.id && !n.observer && !n.paused {
					n.lxfer = true
					n.xferCampaign()
				}
			}
		case EntryAddPeer:
			if newPeer := string(e.Data); len(newPeer) == idLen {
				// Track directly, but wait for commit to be official
				if ps := n.peers[newPeer]; ps != nil {
					ps.ts = time.Now().UnixNano()
				} else {
					n.peers[newPeer] = &lps{time.Now().UnixNano(), 0, false}
				}
			}
		}
	}

	// Apply anything we need here.
	if ae.commit > n.commit {
		if n.paused {
			n.hcommit = ae.commit
			n.debug("Paused, not applying %d", ae.commit)
		} else {
			for index := n.commit + 1; index <= ae.commit; index++ {
				if err := n.applyCommit(index); err != nil {
					break
				}
			}
		}
	}

	var ar *appendEntryResponse
	if sub != nil {
		ar = &appendEntryResponse{n.pterm, n.pindex, n.id, true, _EMPTY_}
	}
	n.Unlock()

	// Success. Send our response.
	if ar != nil {
		n.sendRPC(ae.reply, _EMPTY_, ar.encode(arbuf))
	}
}

// Lock should be held.
func (n *raft) processPeerState(ps *peerState) {
	// Update our version of peers to that of the leader.
	n.csz = ps.clusterSize
	n.qn = n.csz/2 + 1

	old := n.peers
	n.peers = make(map[string]*lps)
	for _, peer := range ps.knownPeers {
		if lp := old[peer]; lp != nil {
			lp.kp = true
			n.peers[peer] = lp
		} else {
			n.peers[peer] = &lps{0, 0, true}
		}
	}
	n.debug("Update peers from leader to %+v", n.peers)
	n.writePeerState(ps)
}

// Process a response.
func (n *raft) processAppendEntryResponse(ar *appendEntryResponse) {
	n.trackPeer(ar.peer)

	if ar.success {
		n.trackResponse(ar)
	} else if ar.term > n.term {
		// False here and they have a higher term.
		n.Lock()
		n.term = ar.term
		n.vote = noVote
		n.writeTermVote()
		n.warn("Detected another leader with higher term, will stepdown and reset")
		n.stepdown.push(noLeader)
		n.resetWAL()
		n.Unlock()
	} else if ar.reply != _EMPTY_ {
		n.catchupFollower(ar)
	}
}

// handleAppendEntryResponse processes responses to append entries.
func (n *raft) handleAppendEntryResponse(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
	msg = copyBytes(msg)
	ar := n.decodeAppendEntryResponse(msg)
	ar.reply = reply
	n.resp.push(ar)
}

func (n *raft) buildAppendEntry(entries []*Entry) *appendEntry {
	return &appendEntry{n.id, n.term, n.commit, n.pterm, n.pindex, entries, _EMPTY_, nil, nil}
}

// Determine if we should store an entry.
func (ae *appendEntry) shouldStore() bool {
	return ae != nil && len(ae.entries) > 0
}

// Store our append entry to our WAL.
// lock should be held.
func (n *raft) storeToWAL(ae *appendEntry) error {
	if ae == nil {
		return fmt.Errorf("raft: Missing append entry for storage")
	}
	if n.werr != nil {
		return n.werr
	}

	seq, _, err := n.wal.StoreMsg(_EMPTY_, nil, ae.buf)
	if err != nil {
		n.setWriteErrLocked(err)
		return err
	}

	// Sanity checking for now.
	if index := ae.pindex + 1; index != seq {
		n.warn("Wrong index, ae is %+v, index stored was %d, n.pindex is %d", ae, seq, n.pindex)
		if index > seq {
			// We are missing store state from our state. We need to stepdown at this point.
			if n.state == Leader {
				n.stepdown.push(n.selectNextLeader())
			}
		} else {
			// Truncate back to our last known.
			n.truncateWAL(n.pterm, n.pindex)
			n.cancelCatchup()
		}
		return errEntryStoreFailed
	}

	n.pterm = ae.term
	n.pindex = seq
	return nil
}

const (
	paeDropThreshold = 20_000
	paeWarnThreshold = 10_000
	paeWarnModulo    = 5_000
)

func (n *raft) sendAppendEntry(entries []*Entry) {
	n.Lock()
	defer n.Unlock()
	ae := n.buildAppendEntry(entries)

	var err error
	var scratch [1024]byte
	ae.buf, err = ae.encode(scratch[:])
	if err != nil {
		return
	}

	// If we have entries store this in our wal.
	if ae.shouldStore() {
		if err := n.storeToWAL(ae); err != nil {
			return
		}
		// We count ourselves.
		n.acks[n.pindex] = map[string]struct{}{n.id: {}}
		n.active = time.Now()

		// Save in memory for faster processing during applyCommit.
		n.pae[n.pindex] = ae
		if l := len(n.pae); l > paeWarnThreshold && l%paeWarnModulo == 0 {
			n.warn("%d append entries pending", len(n.pae))
		}
	}
	n.sendRPC(n.asubj, n.areply, ae.buf)
}

type extensionState uint16

const (
	extUndetermined = extensionState(iota)
	extExtended
	extNotExtended
)

type peerState struct {
	knownPeers  []string
	clusterSize int
	domainExt   extensionState
}

func peerStateBufSize(ps *peerState) int {
	return 4 + 4 + (idLen * len(ps.knownPeers)) + 2
}

func encodePeerState(ps *peerState) []byte {
	var le = binary.LittleEndian
	buf := make([]byte, peerStateBufSize(ps))
	le.PutUint32(buf[0:], uint32(ps.clusterSize))
	le.PutUint32(buf[4:], uint32(len(ps.knownPeers)))
	wi := 8
	for _, peer := range ps.knownPeers {
		copy(buf[wi:], peer)
		wi += idLen
	}
	le.PutUint16(buf[wi:], uint16(ps.domainExt))
	return buf
}

func decodePeerState(buf []byte) (*peerState, error) {
	if len(buf) < 8 {
		return nil, errCorruptPeers
	}
	var le = binary.LittleEndian
	ps := &peerState{clusterSize: int(le.Uint32(buf[0:]))}
	expectedPeers := int(le.Uint32(buf[4:]))
	buf = buf[8:]
	ri := 0
	for i, n := 0, expectedPeers; i < n && ri < len(buf); i++ {
		ps.knownPeers = append(ps.knownPeers, string(buf[ri:ri+idLen]))
		ri += idLen
	}
	if len(ps.knownPeers) != expectedPeers {
		return nil, errCorruptPeers
	}
	if len(buf[ri:]) >= 2 {
		ps.domainExt = extensionState(le.Uint16(buf[ri:]))
	}
	return ps, nil
}

// Lock should be held.
func (n *raft) peerNames() []string {
	var peers []string
	for name, peer := range n.peers {
		if peer.kp {
			peers = append(peers, name)
		}
	}
	return peers
}

func (n *raft) currentPeerState() *peerState {
	n.RLock()
	ps := &peerState{n.peerNames(), n.csz, n.extSt}
	n.RUnlock()
	return ps
}

// sendPeerState will send our current peer state to the cluster.
func (n *raft) sendPeerState() {
	n.sendAppendEntry([]*Entry{{EntryPeerState, encodePeerState(n.currentPeerState())}})
}

// Send a heartbeat.
func (n *raft) sendHeartbeat() {
	n.sendAppendEntry(nil)
}

type voteRequest struct {
	term      uint64
	lastTerm  uint64
	lastIndex uint64
	candidate string
	// internal only.
	reply string
}

const voteRequestLen = 24 + idLen

func (vr *voteRequest) encode() []byte {
	var buf [voteRequestLen]byte
	var le = binary.LittleEndian
	le.PutUint64(buf[0:], vr.term)
	le.PutUint64(buf[8:], vr.lastTerm)
	le.PutUint64(buf[16:], vr.lastIndex)
	copy(buf[24:24+idLen], vr.candidate)

	return buf[:voteRequestLen]
}

func (n *raft) decodeVoteRequest(msg []byte, reply string) *voteRequest {
	if len(msg) != voteRequestLen {
		return nil
	}

	var le = binary.LittleEndian
	return &voteRequest{
		term:      le.Uint64(msg[0:]),
		lastTerm:  le.Uint64(msg[8:]),
		lastIndex: le.Uint64(msg[16:]),
		candidate: string(copyBytes(msg[24 : 24+idLen])),
		reply:     reply,
	}
}

const peerStateFile = "peers.idx"

// Lock should be held.
func (n *raft) writePeerState(ps *peerState) {
	pse := encodePeerState(ps)
	if bytes.Equal(n.wps, pse) {
		return
	}
	// Stamp latest and kick writer.
	n.wps = pse
	select {
	case n.wpsch <- struct{}{}:
	default:
	}
}

// Writes out our peer state outside of a specific raft context.
func writePeerState(sd string, ps *peerState) error {
	psf := filepath.Join(sd, peerStateFile)
	if _, err := os.Stat(psf); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(psf, encodePeerState(ps), 0640); err != nil {
		return err
	}
	return nil
}

func readPeerState(sd string) (ps *peerState, err error) {
	buf, err := os.ReadFile(filepath.Join(sd, peerStateFile))
	if err != nil {
		return nil, err
	}
	return decodePeerState(buf)
}

const termVoteFile = "tav.idx"
const termVoteLen = idLen + 8

// readTermVote will read the largest term and who we voted from to stable storage.
// Lock should be held.
func (n *raft) readTermVote() (term uint64, voted string, err error) {
	buf, err := os.ReadFile(filepath.Join(n.sd, termVoteFile))
	if err != nil {
		return 0, noVote, err
	}
	if len(buf) < termVoteLen {
		return 0, noVote, nil
	}
	var le = binary.LittleEndian
	term = le.Uint64(buf[0:])
	voted = string(buf[8:])
	return term, voted, nil
}

// Lock should be held.
func (n *raft) setWriteErrLocked(err error) {
	// Check if we are closed already.
	if n.state == Closed {
		return
	}
	// Ignore if already set.
	if n.werr == err || err == nil {
		return
	}
	// Ignore non-write errors.
	if err != nil {
		if err == ErrStoreClosed ||
			err == ErrStoreEOF ||
			err == ErrInvalidSequence ||
			err == ErrStoreMsgNotFound ||
			err == errNoPending ||
			err == errPartialCache {
			return
		}
		// If this is a not found report but do not disable.
		if os.IsNotExist(err) {
			n.error("Resource not found: %v", err)
			return
		}
		n.error("Critical write error: %v", err)
	}
	n.werr = err

	if isOutOfSpaceErr(err) {
		// For now since this can be happening all under the covers, we will call up and disable JetStream.
		go n.s.handleOutOfSpace(nil)
	}
}

// Capture our write error if any and hold.
func (n *raft) setWriteErr(err error) {
	n.Lock()
	defer n.Unlock()
	n.setWriteErrLocked(err)
}

func (n *raft) fileWriter() {
	s := n.s
	defer s.grWG.Done()

	n.RLock()
	tvf := filepath.Join(n.sd, termVoteFile)
	psf := filepath.Join(n.sd, peerStateFile)
	n.RUnlock()

	isClosed := func() bool {
		n.RLock()
		defer n.RUnlock()
		return n.state == Closed
	}

	for s.isRunning() {
		select {
		case <-n.quit:
			return
		case <-n.wtvch:
			var buf [termVoteLen]byte
			n.RLock()
			copy(buf[0:], n.wtv)
			n.RUnlock()
			<-dios
			err := os.WriteFile(tvf, buf[:], 0640)
			dios <- struct{}{}
			if err != nil && !isClosed() {
				n.setWriteErr(err)
				n.warn("Error writing term and vote file for %q: %v", n.group, err)
			}
		case <-n.wpsch:
			n.RLock()
			buf := copyBytes(n.wps)
			n.RUnlock()
			<-dios
			err := os.WriteFile(psf, buf, 0640)
			dios <- struct{}{}
			if err != nil && !isClosed() {
				n.setWriteErr(err)
				n.warn("Error writing peer state file for %q: %v", n.group, err)
			}
		}
	}
}

// writeTermVote will record the largest term and who we voted for to stable storage.
// Lock should be held.
func (n *raft) writeTermVote() {
	var buf [termVoteLen]byte
	var le = binary.LittleEndian
	le.PutUint64(buf[0:], n.term)
	copy(buf[8:], n.vote)
	b := buf[:8+len(n.vote)]

	// If same as what we have we can ignore.
	if bytes.Equal(n.wtv, b) {
		return
	}
	// Stamp latest and kick writer.
	n.wtv = b
	select {
	case n.wtvch <- struct{}{}:
	default:
	}
}

// voteResponse is a response to a vote request.
type voteResponse struct {
	term    uint64
	peer    string
	granted bool
}

const voteResponseLen = 8 + 8 + 1

func (vr *voteResponse) encode() []byte {
	var buf [voteResponseLen]byte
	var le = binary.LittleEndian
	le.PutUint64(buf[0:], vr.term)
	copy(buf[8:], vr.peer)
	if vr.granted {
		buf[16] = 1
	} else {
		buf[16] = 0
	}
	return buf[:voteResponseLen]
}

func (n *raft) decodeVoteResponse(msg []byte) *voteResponse {
	if len(msg) != voteResponseLen {
		return nil
	}
	var le = binary.LittleEndian
	vr := &voteResponse{term: le.Uint64(msg[0:]), peer: string(msg[8:16])}
	vr.granted = msg[16] == 1
	return vr
}

func (n *raft) handleVoteResponse(sub *subscription, c *client, _ *Account, _, reply string, msg []byte) {
	vr := n.decodeVoteResponse(msg)
	n.debug("Received a voteResponse %+v", vr)
	if vr == nil {
		n.error("Received malformed vote response for %q", n.group)
		return
	}

	if state := n.State(); state != Candidate && state != Leader {
		n.debug("Ignoring old vote response, we have stepped down")
		return
	}

	n.votes.push(vr)
}

func (n *raft) processVoteRequest(vr *voteRequest) error {
	// To simplify calling code, we can possibly pass `nil` to this function.
	// If that is the case, does not consider it an error.
	if vr == nil {
		return nil
	}
	n.debug("Received a voteRequest %+v", vr)

	if err := n.trackPeer(vr.candidate); err != nil {
		return err
	}

	n.Lock()
	n.resetElectionTimeout()

	vresp := &voteResponse{n.term, n.id, false}
	defer n.debug("Sending a voteResponse %+v -> %q", vresp, vr.reply)

	// Ignore if we are newer.
	if vr.term < n.term {
		n.Unlock()
		n.sendReply(vr.reply, vresp.encode())
		return nil
	}

	// If this is a higher term go ahead and stepdown.
	if vr.term > n.term {
		if n.state != Follower {
			n.debug("Stepping down from %s, detected higher term: %d vs %d", vr.term, n.term, strings.ToLower(n.state.String()))
			n.stepdown.push(noLeader)
			n.term = vr.term
		}
		n.vote = noVote
		n.writeTermVote()
	}

	// Only way we get to yes is through here.
	voteOk := n.vote == noVote || n.vote == vr.candidate
	if voteOk && (vr.lastTerm > n.pterm || vr.lastTerm == n.pterm && vr.lastIndex >= n.pindex) {
		vresp.granted = true
		n.term = vr.term
		n.vote = vr.candidate
		n.writeTermVote()
	} else {
		if vr.term >= n.term && n.vote == noVote {
			n.term = vr.term
			n.resetElect(randCampaignTimeout())
		}
	}
	n.Unlock()

	n.sendReply(vr.reply, vresp.encode())

	return nil
}

func (n *raft) handleVoteRequest(sub *subscription, c *client, _ *Account, subject, reply string, msg []byte) {
	vr := n.decodeVoteRequest(msg, reply)
	if vr == nil {
		n.error("Received malformed vote request for %q", n.group)
		return
	}
	n.reqs.push(vr)
}

func (n *raft) requestVote() {
	n.Lock()
	if n.state != Candidate {
		n.Unlock()
		return
	}
	n.vote = n.id
	n.writeTermVote()
	vr := voteRequest{n.term, n.pterm, n.pindex, n.id, _EMPTY_}
	subj, reply := n.vsubj, n.vreply
	n.Unlock()

	n.debug("Sending out voteRequest %+v", vr)

	// Now send it out.
	n.sendRPC(subj, reply, vr.encode())
}

func (n *raft) sendRPC(subject, reply string, msg []byte) {
	if n.sq != nil {
		n.sq.send(subject, reply, nil, msg)
	}
}

func (n *raft) sendReply(subject string, msg []byte) {
	if n.sq != nil {
		n.sq.send(subject, _EMPTY_, nil, msg)
	}
}

func (n *raft) wonElection(votes int) bool {
	return votes >= n.quorumNeeded()
}

// Return the quorum size for a given cluster config.
func (n *raft) quorumNeeded() int {
	n.RLock()
	qn := n.qn
	n.RUnlock()
	return qn
}

// Lock should be held.
func (n *raft) updateLeadChange(isLeader bool) {
	// We don't care about values that have not been consumed (transitory states),
	// so we dequeue any state that is pending and push the new one.
	for {
		select {
		case n.leadc <- isLeader:
			return
		default:
			select {
			case <-n.leadc:
			default:
				// May have been consumed by the "reader" go routine, so go back
				// to the top of the loop and try to send again.
			}
		}
	}
}

// Lock should be held.
func (n *raft) switchState(state RaftState) {
	if n.state == Closed {
		return
	}

	// Reset the election timer.
	n.resetElectionTimeout()

	if n.state == Leader && state != Leader {
		n.updateLeadChange(false)
		// Drain the response queue.
		n.resp.drain()
	} else if state == Leader && n.state != Leader {
		if len(n.pae) > 0 {
			n.pae = make(map[uint64]*appendEntry)
		}
		n.updateLeadChange(true)
	}

	n.state = state
	n.writeTermVote()
}

const (
	noLeader = _EMPTY_
	noVote   = _EMPTY_
)

func (n *raft) switchToFollower(leader string) {
	n.Lock()
	defer n.Unlock()
	if n.state == Closed {
		return
	}
	n.debug("Switching to follower")

	n.lxfer = false
	n.updateLeader(leader)
	n.switchState(Follower)
}

func (n *raft) switchToCandidate() {
	n.Lock()
	defer n.Unlock()
	if n.state == Closed {
		return
	}
	// If we are catching up or are in observer mode we can not switch.
	if n.observer || n.paused {
		return
	}

	if n.state != Candidate {
		n.debug("Switching to candidate")
	} else {
		if n.lostQuorumLocked() && time.Since(n.llqrt) > 20*time.Second {
			// We signal to the upper layers such that can alert on quorum lost.
			n.updateLeadChange(false)
			n.llqrt = time.Now()
		}
	}
	// Increment the term.
	n.term++
	// Clear current Leader.
	n.updateLeader(noLeader)
	n.switchState(Candidate)
}

func (n *raft) switchToLeader() {
	n.Lock()
	if n.state == Closed {
		n.Unlock()
		return
	}
	n.debug("Switching to leader")

	var state StreamState
	n.wal.FastState(&state)

	// Check if we have items pending as we are taking over.
	sendHB := state.LastSeq > n.commit

	n.lxfer = false
	n.updateLeader(n.id)
	n.switchState(Leader)
	n.Unlock()

	if sendHB {
		n.sendHeartbeat()
	}
}

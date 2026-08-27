package kfake

import (
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Share group state: per-group membership and per-(group,topic,partition) record
// acquisition tracking. Share groups (KIP-932) provide at-least-once delivery
// where multiple consumers in the same group receive records from the same
// partitions concurrently. The broker tracks per-record state: which consumer
// acquired each record, the delivery count, and whether the record has been
// acknowledged (accepted/rejected/released).

type (
	shareGroups struct {
		c *Cluster

		gs map[string]*shareGroup

		// sweepCh receives notifications from manage goroutines when the
		// sweep timer releases records. run() receives from this and fires
		// share watchers (pd.shareWatch is only safe from run()).
		sweepCh chan *shareGroup

		sessions     map[shareSessionKey]*shareSession
		connWatch    map[*clientConn]struct{}
		disconnCh    chan *clientConn
		watchFetchCh chan *watchShareFetch
	}

	shareGroup struct {
		c    *Cluster
		name string

		groupEpoch    int32
		members       map[string]*shareMember
		lastTopicMeta topicMetaSnap // cached snapshot from run(), for recomputation on member removal/fencing

		// Per-(topic,partition) record acquisition state.
		// Accessed from both the run() goroutine (ShareFetch/ShareAcknowledge)
		// and the manage goroutine (sweep timer, member fencing). Must be
		// accessed under mu.
		mu         sync.Mutex
		partitions tps[sharePartition]

		reqCh     chan *clientReq
		controlCh chan func()

		quit   sync.Once
		quitCh chan struct{}
	}

	shareMember struct {
		memberID   string
		clientID   string
		clientHost string
		rackID     *string

		memberEpoch         int32
		previousMemberEpoch int32
		subscribedTopics    []string

		// assignment: topicID -> partitions
		assignment map[uuid][]int32

		t    *time.Timer
		last time.Time
	}

	// shareRecordState tracks the per-record state in a share partition.
	shareRecordState int8

	// shareRecord tracks one record's acquisition state within a share partition.
	shareRecord struct {
		state         shareRecordState
		acquiredBy    string // memberID
		deliveryCount int32
		acquireTime   time.Time
	}

	// sharePartition tracks the SPSO and per-record state for one (group, topic, partition).
	//
	// records is a sparse map from offset to state. Entries are created on
	// first acquisition and deleted when SPSO advances past them. The map
	// may grow large if a consumer acquires many records without acking --
	// acceptable for a test broker.
	sharePartition struct {
		spso       int64 // Share-Partition Start Offset: first unfinalized offset
		acquireEnd int64 // one past highest tracked offset, for in-flight window
		records    map[int64]shareRecord

		// scanOffset tracks the next offset to scan for available records.
		// Advances past acquired/archived records to avoid re-scanning them.
		// Reset back toward SPSO when records are released (ack release,
		// sweep, member fencing).
		scanOffset int64
	}

	// shareSessionKey identifies a share session.
	shareSessionKey struct {
		group    string
		memberID string
		broker   int32
	}

	// shareSession tracks a share fetch session's epoch and the set of
	// partitions currently in the session. On epoch 0 (new session), the
	// request's Topics become the session's partitions. On epoch > 0
	// (incremental), the request's Topics are ADDED to the session and
	// ForgottenTopicsData are REMOVED (matching Kafka's ShareSession.update).
	//
	// partitions maps topicID → partition → requiresUpdate. The bool is
	// true when the partition must appear in the next incremental response
	// even if it has no data (new partition, or previous response had an
	// error). Matching Java's CachedSharePartition.requiresUpdateInResponse.
	shareSession struct {
		epoch      int32
		partitions map[uuid]map[int32]bool // topicID -> partition -> requiresUpdate
		cc         *clientConn             // owning connection, for disconnect cleanup
	}

	// watchShareFetch suspends a ShareFetch request until new records are
	// available or MaxWait expires. Registered on partData.shareWatch;
	// when pushBatch adds records, the watcher fires. The cleanup and
	// re-invocation happen in the cluster run() loop.
	watchShareFetch struct {
		creq    *clientReq
		session *shareSession // session at registration time; stale if overwritten
		in      []*partData
		ackTs   []ackTopic // piggybacked ack topics from the initial request
		cb      func()
		t       *time.Timer

		once    sync.Once
		cleaned bool
	}

	// tpKey identifies a (topicID, partition) pair for response dedup.
	tpKey struct {
		tid uuid
		p   int32
	}
)

const (
	shareRecordAvailable    shareRecordState = iota // can be acquired
	shareRecordAcquired                             // locked by a member
	shareRecordAcknowledged                         // accepted, pending SPSO advance
	shareRecordArchived                             // rejected, pending SPSO advance
)

// Ack types for ShareFetch/ShareAcknowledge acknowledgement batches.
// Matches Kafka's AcknowledgeType enum (KIP-932, KIP-1222).
const (
	shareAckGap     int8 = iota // skip (mapped to ARCHIVED)
	shareAckAccept              // accepted
	shareAckRelease             // release for redelivery
	shareAckReject              // rejected (mapped to ARCHIVED)
	shareAckRenew               // renew acquisition lock (v2+, KIP-1222)
)

const defShareMaxRecords = 500

// release sets the record to available (if under max delivery) or archived.
// Clears acquiredBy. Returns the modified record and true if the record
// became available. Resets sp.scanOffset if the offset is below the current
// scan position.
func (sr shareRecord) release(maxDelivery int32, sp *sharePartition, offset int64) (shareRecord, bool) {
	if sr.deliveryCount >= maxDelivery {
		sr.state = shareRecordArchived
		sr.acquiredBy = ""
		return sr, false
	}
	sr.state = shareRecordAvailable
	sr.acquiredBy = ""
	if offset < sp.scanOffset {
		sp.scanOffset = offset
	}
	return sr, true
}

// releaseAcquiredBy releases all records acquired by memberID on this
// partition. Returns true if any records became available. Advances SPSO
// after releasing.
func (sp *sharePartition) releaseAcquiredBy(memberID string, maxDelivery int32) bool {
	released := false
	for offset, sr := range sp.records {
		if sr.state == shareRecordAcquired && sr.acquiredBy == memberID {
			var ok bool
			sr, ok = sr.release(maxDelivery, sp, offset)
			sp.records[offset] = sr
			if ok {
				released = true
			}
		}
	}
	sp.advanceSPSO()
	return released
}

func (s *shareSession) bumpEpoch() {
	s.epoch++
	if s.epoch < 1 {
		s.epoch = 1
	}
}

func (w *watchShareFetch) fire() {
	w.once.Do(func() {
		go w.cb()
	})
}

func (w *watchShareFetch) cleanup() {
	w.cleaned = true
	for _, pd := range w.in {
		delete(pd.shareWatch, w)
	}
	w.t.Stop()
}

func (sgs *shareGroups) handleHeartbeat(creq *clientReq) {
	req := creq.kreq.(*kmsg.ShareGroupHeartbeatRequest)

	// Group type exclusivity: if this group ID is already a consumer
	// group, reject the share group heartbeat (matching Kafka's
	// GroupCoordinatorService which prevents mixing group types).
	if _, isConsumer := sgs.c.groups.gs[req.GroupID]; isConsumer {
		resp := req.ResponseKind().(*kmsg.ShareGroupHeartbeatResponse)
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		creq.reply(resp)
		return
	}

	// For non-join heartbeats (epoch != 0), the group must exist.
	// Java returns GROUP_ID_NOT_FOUND for heartbeats to unknown groups.
	if req.MemberEpoch != 0 {
		if sgs.gs[req.GroupID] == nil {
			resp := req.ResponseKind().(*kmsg.ShareGroupHeartbeatResponse)
			resp.ErrorCode = kerr.GroupIDNotFound.Code
			creq.reply(resp)
			return
		}
	}

	// Snapshot topic metadata while in run() where c.data is safe to
	// read. manage() will use this snapshot for assignment computation,
	// avoiding a concurrent map read on c.data.tps.
	creq.topicMeta = sgs.c.snapshotTopicMeta()
	g := sgs.getOrCreate(req.GroupID)
	select {
	case g.reqCh <- creq:
	case <-g.quitCh:
		// Group quit -- restart.
		delete(sgs.gs, req.GroupID)
		g = sgs.getOrCreate(req.GroupID)
		select {
		case g.reqCh <- creq:
		case <-g.c.die:
		}
	case <-g.c.die:
	}
}

// get returns the share group for name, or nil if it doesn't exist.
// Cleans up stale entries whose manage goroutine has quit.
func (sgs *shareGroups) get(name string) *shareGroup {
	g := sgs.gs[name]
	if g != nil {
		select {
		case <-g.quitCh:
			delete(sgs.gs, name)
			return nil
		default:
		}
	}
	return g
}

func (sgs *shareGroups) getOrCreate(name string) *shareGroup {
	if g := sgs.get(name); g != nil {
		return g
	}
	g := &shareGroup{
		c:          sgs.c,
		name:       name,
		members:    make(map[string]*shareMember),
		partitions: make(tps[sharePartition]),
		reqCh:      make(chan *clientReq, 16),
		// controlCh is buffered at 1: senders (notably the
		// time.AfterFunc session-timeout callback) select on
		// quitCh/die, so a full buffer blocks the sender until
		// manage() drains it rather than dropping the fn.
		// Concurrent timeouts serialize through the buffer +
		// blocking send; capacity higher than 1 is unnecessary.
		controlCh: make(chan func(), 1),
		quitCh:    make(chan struct{}),
	}
	sgs.gs[name] = g
	go g.manage()
	return g
}

// createSession creates a new share session (epoch 0). Returns the session,
// the share group, and an error code. On success the session is stored and
// a disconnect watcher is registered. Must only be called from run().
func (sgs *shareGroups) createSession(
	key shareSessionKey,
	req *kmsg.ShareFetchRequest,
	cc *clientConn,
) (*shareSession, *shareGroup, int16) {
	sg := sgs.getOrCreate(key.group)

	// Reject piggybacked acks before creating session
	// (matching Kafka: checks before maybeCreateSession).
	for i := range req.Topics {
		for j := range req.Topics[i].Partitions {
			if len(req.Topics[i].Partitions[j].AcknowledgementBatches) > 0 {
				return nil, nil, kerr.InvalidRequest.Code
			}
		}
	}

	// Remove old session before capacity check so re-creating
	// doesn't hit the limit (matching Kafka's cache.remove(key)
	// before maybeCreateSession).
	delete(sgs.sessions, key)
	if int32(len(sgs.sessions)) >= sgs.c.shareMaxSessions() {
		// TODO: use kerr.ShareSessionLimitReached.Code once franz-go
		// 1.21 (or whichever release adds it) is published and
		// pkg/kfake's go.mod is bumped.
		return nil, nil, int16(133) // SHARE_SESSION_LIMIT_REACHED
	}

	session := &shareSession{
		epoch:      0,
		partitions: make(map[uuid]map[int32]bool),
		cc:         cc,
	}
	for i := range req.Topics {
		rt := &req.Topics[i]
		ps := session.partitions[rt.TopicID]
		if ps == nil {
			ps = make(map[int32]bool)
			session.partitions[rt.TopicID] = ps
		}
		for j := range rt.Partitions {
			ps[rt.Partitions[j].Partition] = true
		}
	}
	sgs.sessions[key] = session

	// Watch the connection for disconnect cleanup
	// (matching Kafka's ConnectionDisconnectListener).
	if _, watched := sgs.connWatch[cc]; !watched {
		sgs.connWatch[cc] = struct{}{}
		go func() {
			select {
			case <-cc.done:
				select {
				case sgs.disconnCh <- cc:
				case <-sgs.c.die:
				}
			case <-sgs.c.die:
			}
		}()
	}

	return session, sg, 0
}

// updateSession validates and applies an incremental session update (epoch > 0).
// Returns the session and an error code. Must only be called from run().
func (sgs *shareGroups) updateSession(
	key shareSessionKey,
	epoch int32,
	topics []kmsg.ShareFetchRequestTopic,
	forgotten []kmsg.ShareFetchRequestForgottenTopicsData,
) (*shareSession, int16) {
	session := sgs.sessions[key]
	if session == nil {
		return nil, kerr.ShareSessionNotFound.Code
	}
	if epoch != session.epoch {
		return nil, kerr.InvalidShareSessionEpoch.Code
	}

	// Incremental: merge request Topics (ADD), remove
	// ForgottenTopicsData. New partitions get requiresUpdate=true
	// so they appear in the response even with no data (matching
	// Kafka's CachedSharePartition.requiresUpdateInResponse).
	for i := range topics {
		rt := &topics[i]
		ps := session.partitions[rt.TopicID]
		if ps == nil {
			ps = make(map[int32]bool)
			session.partitions[rt.TopicID] = ps
		}
		for j := range rt.Partitions {
			p := rt.Partitions[j].Partition
			if _, ok := ps[p]; !ok {
				ps[p] = true
			}
		}
	}
	for i := range forgotten {
		ft := &forgotten[i]
		ps := session.partitions[ft.TopicID]
		if ps == nil {
			continue
		}
		for _, p := range ft.Partitions {
			delete(ps, p)
		}
		if len(ps) == 0 {
			delete(session.partitions, ft.TopicID)
		}
	}

	return session, 0
}

// dispatchReq handles a single request from reqCh. Used by manage().
func (g *shareGroup) dispatchReq(creq *clientReq) kmsg.Response {
	if _, ok := creq.kreq.(*kmsg.ShareGroupHeartbeatRequest); ok {
		g.lastTopicMeta = creq.topicMeta
		return g.handleHeartbeat(creq)
	}
	return nil
}

func (g *shareGroup) manage() {
	sweepInterval := time.Duration(g.c.shareLockSweepIntervalMs()) * time.Millisecond
	acqLockTicker := time.NewTicker(sweepInterval)
	defer acqLockTicker.Stop()
	defer func() {
		for _, m := range g.members {
			if m.t != nil {
				m.t.Stop()
			}
		}
	}()
	for {
		select {
		case <-g.quitCh:
			return
		case <-g.c.die:
			return
		case creq := <-g.reqCh:
			if kresp := g.dispatchReq(creq); kresp != nil {
				creq.reply(kresp)
			}
		case fn := <-g.controlCh:
			fn()
		case <-acqLockTicker.C:
			g.sweepExpiredAcquisitions()
		}
	}
}

// sweepExpiredAcquisitions releases records whose acquisition lock has
// expired. If a record has been delivered maxDeliveryCount times, it is
// archived instead of released. After releasing records, notifies run()
// via sweepCh so it can fire share watchers (pd.shareWatch is only
// safe to access from run()).
func (g *shareGroup) sweepExpiredAcquisitions() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	lockDuration := time.Duration(g.c.shareRecordLockDurationMs()) * time.Millisecond
	maxDelivery := g.c.shareMaxDeliveryAttempts()
	released := false
	g.partitions.each(func(_ string, _ int32, sp *sharePartition) {
		for offset, sr := range sp.records {
			if sr.state != shareRecordAcquired {
				continue
			}
			if now.Sub(sr.acquireTime) < lockDuration {
				continue
			}
			var ok bool
			sr, ok = sr.release(maxDelivery, sp, offset)
			sp.records[offset] = sr
			if ok {
				released = true
			}
		}
		sp.advanceSPSO()
	})
	if released {
		select {
		case g.c.shareGroups.sweepCh <- g:
		default:
		}
	}
}

// waitControl sends fn to the manage loop's controlCh and blocks until
// it completes. Used for external callers (persistence, shutdown) that
// need safe access to share group state.
func (g *shareGroup) waitControl(fn func()) bool {
	return waitManageControl(g.controlCh, g.quitCh, g.c, fn)
}

func (g *shareGroup) handleHeartbeat(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.ShareGroupHeartbeatRequest)
	resp := req.ResponseKind().(*kmsg.ShareGroupHeartbeatResponse)
	resp.HeartbeatIntervalMillis = g.c.shareHeartbeatIntervalMs()

	switch req.MemberEpoch {
	case 0:
		return g.handleJoin(creq, req, resp)
	case -1:
		return g.handleLeave(req, resp)
	default:
		return g.handleRegularHeartbeat(creq, req, resp)
	}
}

func (g *shareGroup) handleJoin(creq *clientReq, req *kmsg.ShareGroupHeartbeatRequest, resp *kmsg.ShareGroupHeartbeatResponse) *kmsg.ShareGroupHeartbeatResponse {
	memberID := req.MemberID

	m := g.members[memberID]
	if m != nil {
		// Rejoin: update client info, conditionally rebalance if
		// subscriptions changed (matching Kafka's behavior where
		// groupEpoch only bumps on metadata/subscription changes).
		newSubs := slices.Clone(req.SubscribedTopicNames)
		slices.Sort(newSubs)
		subsChanged := !slices.Equal(m.subscribedTopics, newSubs)
		m.subscribedTopics = newSubs
		m.clientID = creq.cid
		m.clientHost = creq.cc.conn.RemoteAddr().String()
		if req.RackID != nil {
			m.rackID = req.RackID
		}
		m.last = time.Now()
		if subsChanged {
			g.groupEpoch++
			g.recomputeAssignments()
		}
	} else {
		// New join: check capacity, create member.
		if int32(len(g.members)) >= g.c.shareMaxGroupSize() {
			resp.ErrorCode = kerr.GroupMaxSizeReached.Code
			return resp
		}
		m = &shareMember{
			memberID:   memberID,
			clientID:   creq.cid,
			clientHost: creq.cc.conn.RemoteAddr().String(),
			rackID:     req.RackID,
			assignment: make(map[uuid][]int32),
			last:       time.Now(),
		}
		if req.SubscribedTopicNames != nil {
			m.subscribedTopics = slices.Clone(req.SubscribedTopicNames)
			slices.Sort(m.subscribedTopics)
		}
		g.members[memberID] = m
		g.groupEpoch++
		g.recomputeAssignments()
	}

	g.resetSessionTimeout(m)
	g.reconcileMember(m)
	resp.MemberID = &memberID
	resp.MemberEpoch = m.memberEpoch
	resp.Assignment = g.makeAssignment(m)
	return resp
}

func (g *shareGroup) handleLeave(req *kmsg.ShareGroupHeartbeatRequest, resp *kmsg.ShareGroupHeartbeatResponse) *kmsg.ShareGroupHeartbeatResponse {
	m := g.members[req.MemberID]
	if m == nil {
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp
	}
	if m.t != nil {
		m.t.Stop()
	}
	delete(g.members, req.MemberID)
	g.groupEpoch++
	if len(g.members) > 0 {
		g.recomputeAssignments()
	}

	// Release any records acquired by this member.
	g.releaseRecordsForMember(req.MemberID)

	g.maybeQuit()

	resp.MemberID = &req.MemberID
	resp.MemberEpoch = -1
	return resp
}

func (g *shareGroup) handleRegularHeartbeat(creq *clientReq, req *kmsg.ShareGroupHeartbeatRequest, resp *kmsg.ShareGroupHeartbeatResponse) *kmsg.ShareGroupHeartbeatResponse {
	m := g.members[req.MemberID]
	if m == nil {
		resp.ErrorCode = kerr.UnknownMemberID.Code
		return resp
	}

	// Epoch validation matching Kafka's throwIfShareGroupMemberEpochIsInvalid
	// (GroupMetadataManager.java:1605-1625): accept the current epoch or the
	// previous epoch (in case the response that bumped the epoch was lost).
	if req.MemberEpoch != m.memberEpoch && req.MemberEpoch != m.previousMemberEpoch {
		resp.ErrorCode = kerr.FencedMemberEpoch.Code
		return resp
	}

	m.last = time.Now()
	m.clientID = creq.cid
	m.clientHost = creq.cc.conn.RemoteAddr().String()
	if req.RackID != nil {
		m.rackID = req.RackID
	}
	g.resetSessionTimeout(m)

	// Check if subscription changed.
	if req.SubscribedTopicNames != nil {
		sorted := slices.Clone(req.SubscribedTopicNames)
		slices.Sort(sorted)
		if !slices.Equal(m.subscribedTopics, sorted) {
			m.subscribedTopics = sorted
			g.groupEpoch++
			g.recomputeAssignments()
		}
	}

	// Reconcile: bump this member's epoch to the group epoch if behind.
	// Like Kafka's ShareGroupAssignmentBuilder.build(), the epoch is only
	// advanced during the member's own heartbeat, not when other members
	// join/leave.
	g.reconcileMember(m)

	resp.MemberID = &req.MemberID
	resp.MemberEpoch = m.memberEpoch

	// Only send assignment when it may have changed: subscriptions
	// changed or the member epoch was bumped.
	// kgo handles nil Assignment gracefully (skips reconciliation).
	if req.SubscribedTopicNames != nil || m.memberEpoch != m.previousMemberEpoch {
		resp.Assignment = g.makeAssignment(m)
	}
	return resp
}

// recomputeAssignments implements a simple sticky assignor for share groups:
// existing valid assignments are preserved and only the minimum changes are
// made to achieve balance.
//
// Algorithm (per topic):
//  1. Retain assignments for subscribed topics, revoke unsubscribed
//  2. Revoke from overfilled members (too many partitions)
//  3. Revoke overshared partitions (too many members per partition)
//  4. Assign remaining capacity to underfilled members
//
// This updates the assignment but does NOT bump member epochs. Each member's
// epoch is only bumped when it heartbeats and receives the updated assignment
// (via reconcileMember).
//
// Must only be called from the manage() goroutine.
func (g *shareGroup) recomputeAssignments() {
	snap := g.lastTopicMeta

	memberIDs := slices.Sorted(maps.Keys(g.members))

	// Build topic→subscribers index.
	topicSubs := make(map[string][]string)
	for _, id := range memberIDs {
		for _, topic := range g.members[id].subscribedTopics {
			topicSubs[topic] = append(topicSubs[topic], id)
		}
	}

	// Reverse lookup: topicID → topicName.
	topicForID := make(map[uuid]string)
	for topic, si := range snap {
		topicForID[si.id] = topic
	}

	type partKey struct {
		topic string
		part  int32
	}

	// Phase 1: Retain valid assignments, revoke invalid ones.
	partMembers := make(map[partKey]map[string]struct{})

	for _, id := range memberIDs {
		m := g.members[id]
		newAssign := make(map[uuid][]int32)
		for tid, parts := range m.assignment {
			topic := topicForID[tid]
			if topic == "" {
				continue // topic deleted
			}
			si := snap[topic]
			if _, ok := slices.BinarySearch(m.subscribedTopics, topic); !ok {
				continue // unsubscribed
			}
			var kept []int32
			for _, p := range parts {
				if p >= si.partitions {
					continue // partition removed
				}
				pk := partKey{topic, p}
				if partMembers[pk] == nil {
					partMembers[pk] = make(map[string]struct{})
				}
				partMembers[pk][id] = struct{}{}
				kept = append(kept, p)
			}
			if len(kept) > 0 {
				newAssign[tid] = kept
			}
		}
		m.assignment = newAssign
	}

	// Phase 2: Per-topic rebalancing.
	for topic, subs := range topicSubs {
		si, ok := snap[topic]
		if !ok || si.partitions == 0 {
			continue
		}
		nSubs := len(subs)
		nParts := int(si.partitions)
		desiredSharing := (nSubs + nParts - 1) / nParts
		totalSlots := desiredSharing * nParts

		// Compute per-member desired count for this topic (matching
		// Java's cumulative ceiling formula for fair distribution).
		desiredCount := make(map[string]int, nSubs)
		cum := 0
		for i, id := range subs {
			target := ((i+1)*totalSlots + nSubs - 1) / nSubs
			desiredCount[id] = target - cum
			cum = target
		}

		// Current count per member for this topic.
		memberTopicCount := make(map[string]int, nSubs)
		for _, id := range subs {
			memberTopicCount[id] = len(g.members[id].assignment[si.id])
		}

		// Phase 2a: Revoke from overfilled members.
		for _, id := range subs {
			m := g.members[id]
			parts := m.assignment[si.id]
			desired := desiredCount[id]
			if len(parts) <= desired {
				continue
			}
			removed := parts[desired:]
			if desired > 0 {
				m.assignment[si.id] = parts[:desired]
			} else {
				delete(m.assignment, si.id)
			}
			for _, p := range removed {
				pk := partKey{topic, p}
				delete(partMembers[pk], id)
			}
			memberTopicCount[id] = desired
		}

		// Phase 2b: Revoke overshared partitions. Prefer removing
		// from the most-overfilled members for better balance.
		type candidate struct {
			id    string
			extra int // current - desired
		}
		cands := make([]candidate, 0, nSubs)
		for p := int32(0); p < si.partitions; p++ {
			pk := partKey{topic, p}
			members := partMembers[pk]
			excess := len(members) - desiredSharing
			if excess <= 0 {
				continue
			}
			// Collect and sort: most overfilled first, then by ID for determinism.
			cands = cands[:0]
			for id := range members {
				cands = append(cands, candidate{id, memberTopicCount[id] - desiredCount[id]})
			}
			slices.SortFunc(cands, func(a, b candidate) int {
				if a.extra != b.extra {
					return b.extra - a.extra // descending by overfill
				}
				return strings.Compare(a.id, b.id)
			})
			for _, c := range cands {
				if excess <= 0 {
					break
				}
				delete(members, c.id)
				m := g.members[c.id]
				m.assignment[si.id] = slices.DeleteFunc(m.assignment[si.id], func(v int32) bool { return v == p })
				if len(m.assignment[si.id]) == 0 {
					delete(m.assignment, si.id)
				}
				memberTopicCount[c.id]--
				excess--
			}
		}

		// Phase 2c: Assign remaining capacity to underfilled members.
		// Build a list of members needing more partitions.
		type unfilled struct {
			id      string
			desired int
		}
		var uf []unfilled
		for _, id := range subs {
			if memberTopicCount[id] < desiredCount[id] {
				uf = append(uf, unfilled{id, desiredCount[id]})
			}
		}
		if len(uf) == 0 {
			continue
		}

		uidx := 0
		for p := int32(0); p < si.partitions && len(uf) > 0; p++ {
			pk := partKey{topic, p}
			members := partMembers[pk]
			if members == nil {
				members = make(map[string]struct{})
				partMembers[pk] = members
			}
			for len(members) < desiredSharing && len(uf) > 0 {
				start := uidx
				found := false
				for {
					u := &uf[uidx%len(uf)]
					next := (uidx + 1) % len(uf)
					if _, already := members[u.id]; !already {
						members[u.id] = struct{}{}
						m := g.members[u.id]
						m.assignment[si.id] = append(m.assignment[si.id], p)
						memberTopicCount[u.id]++
						if memberTopicCount[u.id] >= u.desired {
							i := uidx % len(uf)
							uf = slices.Delete(uf, i, i+1)
							if len(uf) > 0 {
								uidx = i % len(uf)
							}
						} else {
							uidx = next
						}
						found = true
						break
					}
					uidx = next
					if uidx == start {
						break // all candidates already assigned
					}
				}
				if !found {
					break
				}
			}
		}
	}

	// Sort partition lists for deterministic output.
	for _, m := range g.members {
		for tid := range m.assignment {
			slices.Sort(m.assignment[tid])
		}
	}
}

// reconcileMember bumps the member's epoch to the current group epoch if
// it is behind, mirroring Kafka's ShareGroupAssignmentBuilder.build()
// which only advances a member's epoch during that member's own heartbeat.
func (g *shareGroup) reconcileMember(m *shareMember) {
	if m.memberEpoch != g.groupEpoch {
		m.previousMemberEpoch = m.memberEpoch
		m.memberEpoch = g.groupEpoch
	}
}

func (*shareGroup) makeAssignment(m *shareMember) *kmsg.ShareGroupHeartbeatResponseAssignment {
	a := new(kmsg.ShareGroupHeartbeatResponseAssignment)
	for tid, parts := range m.assignment {
		tp := kmsg.NewShareGroupHeartbeatResponseAssignmentTopicPartition()
		tp.TopicID = tid
		tp.Partitions = parts
		a.TopicPartitions = append(a.TopicPartitions, tp)
	}
	return a
}

func (g *shareGroup) resetSessionTimeout(m *shareMember) {
	if m.t != nil {
		m.t.Stop()
	}
	timeout := time.Duration(g.c.shareSessionTimeoutMs()) * time.Millisecond
	m.t = time.AfterFunc(timeout, func() {
		select {
		case g.controlCh <- func() {
			g.fenceMember(m.memberID)
		}:
		case <-g.quitCh:
		case <-g.c.die:
		}
	})
}

func (g *shareGroup) fenceMember(memberID string) {
	m := g.members[memberID]
	if m == nil {
		return
	}
	if m.t != nil {
		m.t.Stop()
	}
	delete(g.members, memberID)

	g.releaseRecordsForMember(memberID)
	if len(g.members) > 0 {
		g.groupEpoch++
		g.recomputeAssignments()
	}
	g.maybeQuit()
}

func (g *shareGroup) quitOnce() {
	g.quit.Do(func() { close(g.quitCh) })
}

// maybeQuit shuts down the manage goroutine if the group is truly empty:
// no members and no partition state. Only called from manage(), which
// owns g.members. Holds mu while checking partitions AND closing quitCh
// to prevent run() from creating partition state (via getSharePartition)
// between the check and the close.
func (g *shareGroup) maybeQuit() {
	if len(g.members) > 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.partitions) == 0 {
		g.quitOnce()
	}
}

// releaseRecordsForMember releases all records acquired by the given member.
// If a record has hit max delivery count, it is archived instead. If any
// records were released to AVAILABLE, notifies run() via sweepCh so it can
// fire share watchers for waiting consumers.
func (g *shareGroup) releaseRecordsForMember(memberID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	released := g.releaseRecordsForMemberLocked(memberID, g.c.shareMaxDeliveryAttempts())
	if released {
		select {
		case g.c.shareGroups.sweepCh <- g:
		default:
		}
	}
}

// releaseRecordsForMemberLocked releases all records acquired by memberID
// across all partitions. Returns true if any records became available.
// Must be called with g.mu held.
func (g *shareGroup) releaseRecordsForMemberLocked(memberID string, maxDelivery int32) bool {
	released := false
	g.partitions.each(func(_ string, _ int32, sp *sharePartition) {
		if sp.releaseAcquiredBy(memberID, maxDelivery) {
			released = true
		}
	})
	return released
}

// releaseRecordsForSessionLocked releases records acquired by memberID only
// for partitions tracked by the given session. This is used during session
// close (ShareAcknowledge/ShareFetch epoch=-1) to avoid releasing records
// from other sessions on different brokers. Must be called with g.mu held.
func (g *shareGroup) releaseRecordsForSessionLocked(memberID string, session *shareSession, id2t map[uuid]string, maxDelivery int32) bool {
	released := false
	for topicID, parts := range session.partitions {
		topicName := id2t[topicID]
		if topicName == "" {
			continue
		}
		for partition := range parts {
			sp, ok := g.partitions.getp(topicName, partition)
			if !ok {
				continue
			}
			if sp.releaseAcquiredBy(memberID, maxDelivery) {
				released = true
			}
		}
	}
	return released
}

// getSharePartition returns the share partition state, initializing it if needed.
func (g *shareGroup) getSharePartition(topic string, partition int32, pd *partData) *sharePartition {
	sp, ok := g.partitions.getp(topic, partition)
	if ok {
		return sp
	}
	// Initialize SPSO based on group config.
	spso := pd.highWatermark // default: latest
	if g.c.groupConfig(g.name, "share.auto.offset.reset") == "earliest" {
		spso = pd.logStartOffset
	}
	sp = g.partitions.mkp(topic, partition, func() *sharePartition {
		return &sharePartition{
			spso:       spso,
			scanOffset: spso,
			acquireEnd: spso,
			records:    make(map[int64]shareRecord),
		}
	})
	return sp
}

type (
	// ackBatch is a common representation of an acknowledgement batch,
	// abstracting over the different kmsg request types (ShareFetch vs
	// ShareAcknowledge have identical fields but different Go types).
	ackBatch struct {
		first, last int64
		ackTypes    []int8
	}

	// ackTopic groups ack batches by topic, abstracting over ShareFetch and
	// ShareAcknowledge request types.
	ackTopic struct {
		topicID    uuid
		partitions []ackPartition
	}

	ackPartition struct {
		partition int32
		batches   []ackBatch
	}
)

// ackTopicsFromFetch extracts piggybacked ack topics from a ShareFetch
// request, including only partitions with acknowledgement batches.
func ackTopicsFromFetch(topics []kmsg.ShareFetchRequestTopic) []ackTopic {
	var out []ackTopic
	for i := range topics {
		rt := &topics[i]
		at := ackTopic{topicID: rt.TopicID}
		for j := range rt.Partitions {
			rp := &rt.Partitions[j]
			if len(rp.AcknowledgementBatches) == 0 {
				continue
			}
			ap := ackPartition{partition: rp.Partition}
			for _, b := range rp.AcknowledgementBatches {
				ap.batches = append(ap.batches, ackBatch{b.FirstOffset, b.LastOffset, b.AcknowledgeTypes})
			}
			at.partitions = append(at.partitions, ap)
		}
		if len(at.partitions) > 0 {
			out = append(out, at)
		}
	}
	return out
}

// ackTopicsFromAcknowledge extracts ack topics from a ShareAcknowledge request.
func ackTopicsFromAcknowledge(topics []kmsg.ShareAcknowledgeRequestTopic) []ackTopic {
	var out []ackTopic
	for i := range topics {
		rt := &topics[i]
		at := ackTopic{topicID: rt.TopicID}
		for j := range rt.Partitions {
			rp := &rt.Partitions[j]
			if len(rp.AcknowledgementBatches) == 0 {
				continue
			}
			ap := ackPartition{partition: rp.Partition}
			for _, b := range rp.AcknowledgementBatches {
				ap.batches = append(ap.batches, ackBatch{b.FirstOffset, b.LastOffset, b.AcknowledgeTypes})
			}
			at.partitions = append(at.partitions, ap)
		}
		if len(at.partitions) > 0 {
			out = append(out, at)
		}
	}
	return out
}

// validateOneAckBatch validates a single acknowledgement batch. prevEnd
// tracks the previous batch's last offset (pass -1 initially). Updated on
// success.
func validateOneAckBatch(first, last int64, ackTypes []int8, prevEnd *int64, maxAckType int8) int16 {
	if first > last {
		return kerr.InvalidRequest.Code
	}
	if first <= *prevEnd {
		return kerr.InvalidRequest.Code
	}
	if len(ackTypes) == 0 {
		return kerr.InvalidRequest.Code
	}
	if len(ackTypes) > 1 && int64(len(ackTypes)) != last-first+1 {
		return kerr.InvalidRequest.Code
	}
	for _, at := range ackTypes {
		if at < 0 || at > maxAckType {
			return kerr.InvalidRequest.Code
		}
	}
	*prevEnd = last
	return 0
}

// processShareAcks validates and applies acknowledgement batches,
// calling onPartition for each acked partition with its result error
// code (0 on success). If the receiving broker is not the partition's
// current leader, onNotLeader is called instead of onPartition so the
// caller can populate a CurrentLeader hint in its response; if
// onNotLeader is nil, onPartition receives NotLeaderForPartition
// without a hint. Returns partitions with successful acks (for
// watcher firing).
//
// Real Kafka rejects ShareAcknowledge for a partition the broker
// does not lead; clients rely on that per-partition error (plus the
// CurrentLeader hint on standalone ShareAcknowledge responses) to
// migrate cursors promptly. Without enforcing this in kfake, tests
// miss the ack-side leader-move path entirely.
//
// Must be called from run() with g.mu held.
func (g *shareGroup) processShareAcks(
	creq *clientReq,
	memberID string,
	topics []ackTopic,
	maxAckType int8,
	id2t map[uuid]string,
	maxDelivery int32,
	onPartition func(tid uuid, p int32, ec int16),
	onNotLeader func(tid uuid, p int32, pd *partData),
) (toFire []*partData) {
	for _, at := range topics {
		topicName := id2t[at.topicID]
		if topicName == "" {
			for _, ap := range at.partitions {
				onPartition(at.topicID, ap.partition, kerr.UnknownTopicID.Code)
			}
			continue
		}
		if !g.c.allowedACL(creq, topicName, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
			for _, ap := range at.partitions {
				onPartition(at.topicID, ap.partition, kerr.TopicAuthorizationFailed.Code)
			}
			continue
		}
		for _, ap := range at.partitions {
			errCode := int16(0)
			prevEnd := int64(-1)
			for _, batch := range ap.batches {
				if ec := validateOneAckBatch(batch.first, batch.last, batch.ackTypes, &prevEnd, maxAckType); ec != 0 {
					errCode = ec
					break
				}
			}
			if errCode != 0 {
				onPartition(at.topicID, ap.partition, errCode)
				continue
			}
			pd, ok := g.c.data.tps.getp(topicName, ap.partition)
			if !ok {
				onPartition(at.topicID, ap.partition, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			if pd.leader.node != creq.cc.b.node {
				if onNotLeader != nil {
					onNotLeader(at.topicID, ap.partition, pd)
				} else {
					onPartition(at.topicID, ap.partition, kerr.NotLeaderForPartition.Code)
				}
				continue
			}
			shp := g.getSharePartition(topicName, ap.partition, pd)
			if ec := shp.validateAndProcessAcks(memberID, ap.batches, maxDelivery); ec != 0 {
				onPartition(at.topicID, ap.partition, ec)
				continue
			}
			onPartition(at.topicID, ap.partition, 0)
			toFire = append(toFire, pd)
		}
	}
	return toFire
}

// acquireRecords acquires available records from [scanOffset, hwm) for the
// given member, up to maxRecords. Records that have hit maxDeliveryCount are
// archived instead of acquired. Returns the list of acquired offset ranges
// with delivery counts.
//
// pd is used for compaction-aware gap detection: offsets that fall between
// batches (compacted away) are skipped rather than tracked as phantom records.
//
// When readCommitted is true, offsets belonging to aborted transactions are
// archived immediately (matching Java's SharePartition.acquire filtering).
func (sp *sharePartition) acquireRecords(
	pd *partData,
	hwm int64,
	maxRecords int32,
	memberID string,
	maxDeliveryCount int32,
	maxRecordLocks int32,
	readCommitted bool,
) []kmsg.ShareFetchResponseTopicPartitionAcquiredRecord {
	var (
		now      = time.Now()
		count    int32
		acquired []kmsg.ShareFetchResponseTopicPartitionAcquiredRecord
	)

	if sp.scanOffset < sp.spso {
		sp.scanOffset = sp.spso
	}

	// In-flight limit (matching Kafka's lastOffsetAndMaxRecordsToAcquire).
	// The in-flight window is [spso, acquireEnd). When at capacity, records
	// within the existing window can still be re-acquired (e.g., released
	// records near SPSO) because they don't extend the window. Only
	// acquisition of records BEYOND acquireEnd is blocked. Kafka handles
	// this with: if maxRecordsToAcquire <= 0 && fetchOffset <= endOffset,
	// recalculate as min(maxFetch, endOffset - fetchOffset + 1).
	acquireLimit := hwm // default: scan up to HWM
	if maxRecordLocks > 0 && sp.acquireEnd > sp.spso {
		windowSize := int32(sp.acquireEnd - sp.spso)
		if windowSize >= maxRecordLocks {
			// At capacity: only allow re-acquisition within the
			// existing window (up to acquireEnd), not beyond.
			if sp.scanOffset >= sp.acquireEnd {
				return nil
			}
			acquireLimit = sp.acquireEnd
		}
	}
	if acquireLimit > hwm {
		acquireLimit = hwm
	}

	// Find starting batch position for gap detection.
	curSeg, curMeta, hasBatch, atEnd := pd.searchOffset(sp.scanOffset)
	if atEnd {
		hasBatch = false
	}

	lastScanned := sp.scanOffset

	// Walk offsets from scanOffset to HWM looking for available records.
	for offset := sp.scanOffset; offset < acquireLimit && count < maxRecords; offset++ {
		// In-flight limit per iteration: stop if extending beyond acquireEnd
		// while at capacity (records within the window are always ok).
		// This check MUST come before advancing lastScanned, otherwise
		// scanOffset jumps past this offset without creating a record
		// for it. Later, advanceSPSO would skip the nil entry as a
		// "compacted gap", silently losing the record.
		if maxRecordLocks > 0 && offset >= sp.acquireEnd && sp.acquireEnd > sp.spso && sp.acquireEnd-sp.spso >= int64(maxRecordLocks) {
			break
		}
		lastScanned = offset + 1

		// Gap detection: skip offsets between batches (compacted away).
		if hasBatch {
			curBatch := &pd.segments[curSeg].index[curMeta]
			if offset > curBatch.firstOffset+int64(curBatch.lastOffsetDelta) {
				// Past current batch, advance to next.
				curMeta++
				for curMeta >= len(pd.segments[curSeg].index) {
					curSeg++
					curMeta = 0
					if curSeg >= len(pd.segments) {
						hasBatch = false
						break
					}
				}
				if hasBatch {
					curBatch = &pd.segments[curSeg].index[curMeta]
				}
			}
			if hasBatch && offset < curBatch.firstOffset {
				// Offset is in a gap between batches. Jump past it.
				lastScanned = curBatch.firstOffset
				offset = curBatch.firstOffset - 1 // -1 because for loop increments
				continue
			}
		}

		// READ_COMMITTED: check if this offset belongs to an aborted
		// transaction. If so, archive it immediately (matching Java's
		// SharePartition.maybeFilterAbortedTransactionalAcquiredRecords).
		// We piggyback on the batch cursor to get the producerID.
		if readCommitted && hasBatch {
			curBatch := &pd.segments[curSeg].index[curMeta]
			if offset >= curBatch.firstOffset && offset <= curBatch.firstOffset+int64(curBatch.lastOffsetDelta) {
				if curBatch.inTx && pd.isOffsetAborted(curBatch.firstOffset, curBatch.producerID) {
					sr := sp.records[offset]
					sr.state = shareRecordArchived
					sr.acquiredBy = ""
					sp.records[offset] = sr
					continue
				}
			}
		}

		sr, exists := sp.records[offset]
		if !exists {
			// No state yet -- new record, available.
			sr = shareRecord{state: shareRecordAvailable}
		}
		if sr.state != shareRecordAvailable {
			continue
		}

		// If this record has been delivered too many times, archive it
		// rather than delivering again.
		if sr.deliveryCount >= maxDeliveryCount {
			sr.state = shareRecordArchived
			sr.acquiredBy = ""
			sp.records[offset] = sr
			continue
		}

		// Acquire.
		sr.state = shareRecordAcquired
		sr.acquiredBy = memberID
		sr.deliveryCount++
		sr.acquireTime = now
		sp.records[offset] = sr
		count++
		if offset+1 > sp.acquireEnd {
			sp.acquireEnd = offset + 1
		}

		// Try to extend the last AcquiredRecord range.
		if n := len(acquired); n > 0 {
			last := &acquired[n-1]
			if last.LastOffset == offset-1 && int32(last.DeliveryCount) == sr.deliveryCount {
				last.LastOffset = offset
				continue
			}
		}
		ar := kmsg.NewShareFetchResponseTopicPartitionAcquiredRecord()
		ar.FirstOffset = offset
		ar.LastOffset = offset
		ar.DeliveryCount = int16(sr.deliveryCount)
		acquired = append(acquired, ar)
	}

	// Always advance scanOffset to reflect how far we scanned,
	// even when nothing was acquired. This lets advanceSPSO
	// detect compacted gaps (nil entries below scanOffset).
	if lastScanned > sp.scanOffset {
		sp.scanOffset = lastScanned
	}

	// After acquiring, advance SPSO in case we archived some records
	// at the front.
	sp.advanceSPSO()

	return acquired
}

// validateAcks checks that all offsets in [first, last] are valid for acking
// by memberID without applying any state changes. Returns 0 if valid, error
// code otherwise. Used for atomic validate-then-apply across multiple batches:
// validate all, then apply all, rollback everything on any error.
//
// Error codes:
//   - INVALID_REQUEST: batch extends beyond tracked/cached records
//   - INVALID_RECORD_STATE: record exists but wrong state or wrong owner
func (sp *sharePartition) validateAcks(memberID string, first, last int64) int16 {
	// Check if the batch extends beyond tracked records.
	if sp.acquireEnd > sp.spso {
		if first >= sp.acquireEnd || last >= sp.acquireEnd {
			return kerr.InvalidRequest.Code
		}
	} else if last >= sp.spso {
		return kerr.InvalidRequest.Code
	}

	for offset := first; offset <= last; offset++ {
		if offset < sp.spso {
			continue
		}
		sr, ok := sp.records[offset]
		if !ok {
			return kerr.InvalidRecordState.Code
		}
		if sr.state != shareRecordAcquired {
			return kerr.InvalidRecordState.Code
		}
		if sr.acquiredBy != memberID {
			return kerr.InvalidRecordState.Code
		}
	}
	return 0
}

// processAcks applies ack types to the offset range [first, last].
//
// AcknowledgeTypes can be:
//   - empty or single element: uniform type for the whole range
//   - per-offset: one element per offset in the range
//
// Types: 0=Gap (skip), 1=Accept, 2=Release, 3=Reject, 4=Renew.
// Offsets below SPSO are silently skipped (already finalized).
//
// maxDeliveryCount is checked on RELEASE: if the record has already been
// delivered maxDeliveryCount times, it is archived instead of released.
//
// Must be called only after validateAcks succeeds for all batches in the
// request. processAcks assumes records are in valid state (ACQUIRED by
// memberID) and skips any that aren't (defensive, shouldn't happen after
// validation).
func (sp *sharePartition) processAcks(memberID string, first, last int64, ackTypes []int8, maxDeliveryCount int32) {
	perOffset := len(ackTypes) > 1
	uniformType := int8(1) // default: accept
	if len(ackTypes) == 1 {
		uniformType = ackTypes[0]
	}
	now := time.Now()
	for offset := first; offset <= last; offset++ {
		if offset < sp.spso {
			continue // already finalized
		}
		sr, ok := sp.records[offset]
		if !ok {
			continue // compacted or never tracked
		}
		if sr.state != shareRecordAcquired {
			continue // already released/archived/acknowledged
		}
		if sr.acquiredBy != memberID {
			continue // acquired by someone else (e.g., after sweep)
		}
		ackType := uniformType
		if perOffset {
			idx := int(offset - first)
			if idx < len(ackTypes) {
				ackType = ackTypes[idx]
			}
		}
		switch ackType {
		case shareAckGap:
			sr.state = shareRecordArchived
			sr.acquiredBy = ""
		case shareAckAccept:
			sr.state = shareRecordAcknowledged
			sr.acquiredBy = ""
		case shareAckRelease:
			sr, _ = sr.release(maxDeliveryCount, sp, offset)
		case shareAckReject:
			sr.state = shareRecordArchived
			sr.acquiredBy = ""
		case shareAckRenew:
			sr.acquireTime = now
		}
		sp.records[offset] = sr
	}
}

// validateAndProcessAcks atomically validates then applies ack batches
// for one partition. Returns 0 on success, error code on failure.
// Validates all batches first, then applies all -- any validation failure
// rejects the entire request. Must be called with sg.mu held.
func (sp *sharePartition) validateAndProcessAcks(memberID string, batches []ackBatch, maxDelivery int32) int16 {
	for i := range batches {
		if ec := sp.validateAcks(memberID, batches[i].first, batches[i].last); ec != 0 {
			return ec
		}
	}
	for i := range batches {
		sp.processAcks(memberID, batches[i].first, batches[i].last, batches[i].ackTypes, maxDelivery)
	}
	sp.advanceSPSO()
	return 0
}

// advanceSPSO advances the SPSO past contiguous acknowledged/archived
// records, cleaning up their state entries. Also skips compacted gaps:
// if records[spso] is nil but we've already scanned past that offset
// (spso < scanOffset), the nil means the offset was compacted away.
func (sp *sharePartition) advanceSPSO() {
	for {
		sr, ok := sp.records[sp.spso]
		if !ok {
			// If we've scanned past this offset and there's no
			// record, it's a compacted gap - skip it.
			if sp.spso < sp.scanOffset {
				sp.spso++
				continue
			}
			break
		}
		if sr.state != shareRecordAcknowledged && sr.state != shareRecordArchived {
			break
		}
		delete(sp.records, sp.spso)
		sp.spso++
	}
}

// cleanupSessionsForConn removes all share sessions owned by the given
// connection and releases their acquired records. When a TCP connection
// drops, all sessions on that connection are removed and acquired records
// are released. Must only be called from run().
func (sgs *shareGroups) cleanupSessionsForConn(cc *clientConn) {
	maxDelivery := sgs.c.shareMaxDeliveryAttempts()
	id2t := sgs.c.data.id2t
	for key, session := range sgs.sessions {
		if session.cc != cc {
			continue
		}
		delete(sgs.sessions, key)
		sg := sgs.gs[key.group]
		if sg == nil {
			continue
		}
		sg.mu.Lock()
		released := sg.releaseRecordsForSessionLocked(key.memberID, session, id2t, maxDelivery)
		sg.mu.Unlock()
		if released {
			sg.fireAllShareWatchers()
		}
	}
}

// fireAll wakes pending ShareFetch watchers on the given partitions.
// Must only be called from run().
func fireAll(pds []*partData) {
	for _, pd := range pds {
		pd.fireShareWatchers()
	}
}

// fireShareWatchers wakes any pending ShareFetch watchers on this
// partition. Called after acks that may have released records.
// Must only be called from run().
func (pd *partData) fireShareWatchers() {
	for w := range pd.shareWatch {
		w.fire()
	}
}

// fireAllShareWatchers wakes share watchers on all partitions tracked by
// this share group. Called from run() when the manage goroutine's sweep
// timer releases records. Must only be called from run().
func (g *shareGroup) fireAllShareWatchers() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for topic, ps := range g.partitions {
		for p := range ps {
			if pd, ok := g.c.data.tps.getp(topic, p); ok {
				pd.fireShareWatchers()
			}
		}
	}
}

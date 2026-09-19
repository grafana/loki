package kfake

import (
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ShareFetch: v0-2 (KIP-932, KIP-1206, KIP-1222)
//
// Behavior:
// * Acquires unread records from share-group partitions and returns them
// * Share sessions (similar to fetch sessions) track partition sets across requests
// * Piggybacked acknowledgements can be included in any ShareFetch request
// * Record acquisition locks are tracked per-member with configurable timeout
// * MaxWait long-poll: if no records available, waits up to MaxWaitMillis
//
// Session management:
// * epoch 0: Create new session, populate from request Topics
// * epoch >0: Incremental - Topics are ADDED, ForgottenTopicsData REMOVED
// * epoch -1: Close session, process final acks, release acquired records
// * Watcher re-invocation reuses existing session
//
// Version notes:
// * v0: Initial share fetch (KIP-932)
// * v1: BatchSize, ShareAcquireMode for BATCH_OPTIMIZED (KIP-1206)
// * v2: IsRenewAck for lock renewal without fetching (KIP-1222)

func init() { regKey(78, 0, 2) }

func (c *Cluster) handleShareFetch(creq *clientReq, w *watchShareFetch) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.ShareFetchRequest)
		resp = req.ResponseKind().(*kmsg.ShareFetchResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	var groupID, memberID string
	if req.GroupID != nil {
		groupID = *req.GroupID
	}
	if req.MemberID != nil {
		memberID = *req.MemberID
	}

	resp.AcquisitionLockTimeoutMillis = c.shareRecordLockDurationMs()

	// ACL: require GROUP READ.
	if !c.allowedACL(creq, groupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp, nil
	}

	// Validate memberID format (real Kafka does NOT check group membership
	// here, only format: non-empty, <=36 chars).
	if memberID == "" || len(memberID) > 36 {
		resp.ErrorCode = kerr.InvalidRequest.Code
		return resp, nil
	}

	// KIP-1222: when isRenewAck is set, all fetch params must be zero
	// and no fetch data (non-ack partition entries) may be present.
	if req.Version >= 2 && req.IsRenewAck {
		if req.MaxBytes != 0 || req.MinBytes != 0 || req.MaxRecords != 0 || req.MaxWaitMillis != 0 {
			resp.ErrorCode = kerr.InvalidRequest.Code
			return resp, nil
		}
		for i := range req.Topics {
			for j := range req.Topics[i].Partitions {
				if len(req.Topics[i].Partitions[j].AcknowledgementBatches) == 0 {
					resp.ErrorCode = kerr.InvalidRequest.Code
					return resp, nil
				}
			}
		}
	}

	sg := c.shareGroups.get(groupID)
	id2t := c.data.id2t
	maxDelivery := c.shareMaxDeliveryAttempts()

	maxAckType := shareAckReject
	if req.Version >= 2 && req.IsRenewAck {
		maxAckType = shareAckRenew
	}

	// Determine isolation level for this group (default READ_UNCOMMITTED).
	readCommitted := c.groupConfig(groupID, "share.isolation.level") == "read_committed"

	sessionKey := shareSessionKey{
		group:    groupID,
		memberID: memberID,
		broker:   creq.cc.b.node,
	}

	topicIdx := make(map[uuid]int)
	addTopic := func(tid uuid) int {
		i, ok := topicIdx[tid]
		if !ok {
			i = len(resp.Topics)
			topicIdx[tid] = i
			t := kmsg.NewShareFetchResponseTopic()
			t.TopicID = tid
			resp.Topics = append(resp.Topics, t)
		}
		return i
	}
	donep := func(tid uuid, p int32, errCode int16) *kmsg.ShareFetchResponseTopicPartition {
		idx := addTopic(tid)
		sp := kmsg.NewShareFetchResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		resp.Topics[idx].Partitions = append(resp.Topics[idx].Partitions, sp)
		return &resp.Topics[idx].Partitions[len(resp.Topics[idx].Partitions)-1]
	}
	onAck := func(tid uuid, p int32, ec int16) {
		if ec == 0 {
			return // success - fetch phase handles the response entry
		}
		donep(tid, p, 0).AcknowledgeErrorCode = ec
	}
	// onAckNotLeader routes a leader-mismatch on a piggybacked ack to
	// the AcknowledgeErrorCode field. The ShareFetch response has no
	// per-partition CurrentLeader hint specifically for acks (the
	// top-level CurrentLeader is tied to rp.ErrorCode, the fetch-side
	// leader check at the bottom of this file); the client surfaces
	// the non-retryable ack error via its share-ack callback and
	// migrates the cursor via the fetch-side NOT_LEADER path if the
	// same partition was also being fetched.
	onAckNotLeader := func(tid uuid, p int32, _ *partData) {
		donep(tid, p, 0).AcknowledgeErrorCode = kerr.NotLeaderForPartition.Code
	}

	// Session management.
	sgs := &c.shareGroups
	var session *shareSession
	if w != nil {
		// Watcher re-invocation: session was already validated.
		// If the session was overwritten by a new epoch-0 request,
		// this watcher is stale -- return an empty response so the
		// client retries with a fresh epoch-0.
		session = sgs.sessions[sessionKey]
		if session == nil || session != w.session {
			return resp, nil
		}
	} else if req.ShareSessionEpoch == -1 {
		// Session close: process piggybacked acks, release remaining
		// acquired records for this session's partitions only (not all
		// partitions - the member may have other sessions on other brokers).
		session = sgs.sessions[sessionKey]
		if session == nil {
			resp.ErrorCode = kerr.ShareSessionNotFound.Code
			return resp, nil
		}
		if sg != nil {
			ackTs := ackTopicsFromFetch(req.Topics)
			sg.mu.Lock()
			toFire := sg.processShareAcks(creq, memberID, ackTs, maxAckType, id2t, maxDelivery, onAck, onAckNotLeader)
			released := sg.releaseRecordsForSessionLocked(memberID, session, id2t, maxDelivery)
			sg.mu.Unlock()
			ensureAckedParts(resp, ackTs, addTopic)
			// fireAllShareWatchers fires every partition in the
			// group, which is a superset of the partitions in
			// toFire. When released is false, we only wake the
			// specific partitions that were ack-released.
			if released {
				sg.fireAllShareWatchers()
			} else {
				fireAll(toFire)
			}
		}
		delete(sgs.sessions, sessionKey)
		return resp, nil
	} else if req.ShareSessionEpoch == 0 {
		var ec int16
		session, sg, ec = sgs.createSession(sessionKey, req, creq.cc)
		if ec != 0 {
			resp.ErrorCode = ec
			return resp, nil
		}
	} else {
		var ec int16
		session, ec = sgs.updateSession(sessionKey, req.ShareSessionEpoch, req.Topics, req.ForgottenTopicsData)
		if ec != 0 {
			resp.ErrorCode = ec
			return resp, nil
		}
	}

	// Ensure sg is non-nil: epoch 0 creates it via createSession, but
	// epoch > 0 and watcher paths only looked it up via get() at the
	// top of handleShareFetch. That get() returns nil if the manage
	// goroutine quit between session creation and this request (e.g.,
	// DeleteShareGroupOffsets emptied the group). We recreate the group
	// so that ack processing and record acquisition below have a valid
	// shareGroup to operate on.
	if sg == nil {
		sg = c.shareGroups.getOrCreate(groupID)
	}

	// KIP-1222: when isRenewAck is set, skip fetch entirely -- only
	// process acks. Time spent fetching might exceed the renewed lock.
	if req.Version >= 2 && req.IsRenewAck {
		var toFire []*partData
		if w == nil {
			ackTs := ackTopicsFromFetch(req.Topics)
			sg.mu.Lock()
			toFire = sg.processShareAcks(creq, memberID, ackTs, maxAckType, id2t, maxDelivery, onAck, onAckNotLeader)
			sg.mu.Unlock()
			ensureAckedParts(resp, ackTs, addTopic)
		}
		fireAll(toFire)
		session.bumpEpoch()
		return resp, nil
	}

	// Kafka's SharePartition.acquire treats maxFetchRecords <= 0 as
	// "nothing to acquire" (see SharePartition.java). We default to
	// defShareMaxRecords so zero behaves like the client's default.
	maxRecords := req.MaxRecords
	if maxRecords <= 0 {
		maxRecords = defShareMaxRecords
	}

	// BATCH_OPTIMIZED mode (ShareAcquireMode=0, KIP-1206): BatchSize
	// controls response splitting, not acquisition limit.
	batchSize := int32(0)
	if req.ShareAcquireMode == 0 && req.BatchSize > 0 {
		batchSize = req.BatchSize
	}

	type fetchTarget struct {
		topicID   uuid
		topic     string
		partition int32
		pd        *partData
	}
	type acquiredPart struct {
		topicID   uuid
		pd        *partData
		partition int32
		ranges    []kmsg.ShareFetchResponseTopicPartitionAcquiredRecord
	}

	var (
		totalRecords   int32
		targets        []fetchTarget
		acquiredParts  []acquiredPart
		includeBrokers bool
		toFire         []*partData
		maxRecordLocks = c.shareMaxRecordLocks()
	)

	// Parse piggybacked acks before taking the lock (pure request
	// transformation, no shared state).
	var ackTs []ackTopic
	if w == nil {
		ackTs = ackTopicsFromFetch(req.Topics)
	}

	// Lock the share group's partition state for ack processing and
	// record acquisition. Batch I/O happens after unlocking.
	sg.mu.Lock()

	// Process piggybacked acks first (skipped on watcher re-invocation,
	// since acks were already processed in the initial call).
	if len(ackTs) > 0 {
		toFire = sg.processShareAcks(creq, memberID, ackTs, maxAckType, id2t, maxDelivery, onAck, onAckNotLeader)
	}

	// Build target list from session partitions.
	for topicID, parts := range session.partitions {
		topicName := id2t[topicID]
		if topicName == "" {
			for p := range parts {
				donep(topicID, p, kerr.UnknownTopicID.Code)
			}
			continue
		}
		if !c.allowedACL(creq, topicName, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
			for p := range parts {
				donep(topicID, p, kerr.TopicAuthorizationFailed.Code)
			}
			continue
		}
		for p := range parts {
			pd, ok := c.data.tps.getp(topicName, p)
			if !ok {
				donep(topicID, p, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			if pd.leader.node != creq.cc.b.node {
				sp := donep(topicID, p, kerr.NotLeaderForPartition.Code)
				sp.CurrentLeader.LeaderID = pd.leader.node
				sp.CurrentLeader.LeaderEpoch = pd.epoch
				includeBrokers = true
				continue
			}
			targets = append(targets, fetchTarget{topicID, topicName, p, pd})
		}
	}

	// Rotate targets for fairness using session epoch as the
	// round-robin start index, avoiding a copy.
	startIdx := 0
	if n := len(targets); n > 1 && session.epoch > 0 {
		startIdx = int(session.epoch) % n
	}

	// Acquire records from rotated targets.
	for i := range targets {
		tgt := &targets[(startIdx+i)%len(targets)]
		remaining := maxRecords - totalRecords
		if remaining <= 0 {
			continue
		}
		shp := sg.getSharePartition(tgt.topic, tgt.partition, tgt.pd)
		hwm := tgt.pd.highWatermark
		if readCommitted {
			hwm = tgt.pd.lastStableOffset
		}
		acquiredRanges := shp.acquireRecords(
			tgt.pd,
			hwm,
			remaining,
			memberID,
			maxDelivery,
			maxRecordLocks,
			readCommitted,
		)
		if len(acquiredRanges) == 0 {
			continue
		}
		if batchSize > 0 {
			acquiredRanges = splitAcquiredRanges(acquiredRanges, batchSize)
		}
		for _, ar := range acquiredRanges {
			totalRecords += int32(ar.LastOffset - ar.FirstOffset + 1)
		}
		acquiredParts = append(acquiredParts, acquiredPart{
			topicID:   tgt.topicID,
			pd:        tgt.pd,
			partition: tgt.partition,
			ranges:    acquiredRanges,
		})
	}

	sg.mu.Unlock()

	// Fire watchers outside the lock for ack-released records.
	// This cannot move above the unlock: the acquisition loop
	// above requires sg.mu, and firing inside the lock would
	// deadlock when woken watchers try to re-acquire sg.mu.
	fireAll(toFire)

	// Read batch bytes outside the lock -- this may do disk I/O in
	// persistence mode and we don't want to block the sweep timer.
	for _, ap := range acquiredParts {
		firstAcq := ap.ranges[0].FirstOffset
		lastAcq := ap.ranges[len(ap.ranges)-1].LastOffset

		var rawBytes []byte
		segIdx, metaIdx, ok, atEnd := ap.pd.searchOffset(firstAcq)
		if ok && !atEnd {
		readBatches:
			for si := segIdx; si < len(ap.pd.segments); si++ {
				seg := &ap.pd.segments[si]
				start := 0
				if si == segIdx {
					start = metaIdx
				}
				for bi := start; bi < len(seg.index); bi++ {
					m := &seg.index[bi]
					if m.firstOffset > lastAcq {
						break readBatches
					}
					raw, err := c.readBatchRaw(ap.pd, si, m)
					if err != nil {
						break readBatches
					}
					rawBytes = append(rawBytes, raw...)
				}
			}
		}

		sp := donep(ap.topicID, ap.partition, 0)
		sp.Records = rawBytes
		sp.AcquiredRecords = ap.ranges
	}

	// Incremental response filtering: update session requiresUpdate
	// from response entries, then emit empty entries for session
	// partitions that still need update but weren't in the response.
	responded := make(map[tpKey]struct{})
	for _, t := range resp.Topics {
		ps := session.partitions[t.TopicID]
		for _, p := range t.Partitions {
			responded[tpKey{t.TopicID, p.Partition}] = struct{}{}
			if ps != nil {
				if _, ok := ps[p.Partition]; ok {
					ps[p.Partition] = p.ErrorCode != 0
				}
			}
		}
	}
	for tid, parts := range session.partitions {
		for p, needsUpdate := range parts {
			if !needsUpdate {
				continue
			}
			if _, ok := responded[tpKey{tid, p}]; ok {
				continue
			}
			donep(tid, p, 0)
			parts[p] = false
		}
	}

	// Ensure all piggybacked ack partitions appear in the response.
	// The client uses partition presence to confirm ack processing.
	// On watcher re-invocation, ackTs is nil (acks were already
	// processed), so use the watcher's saved ack topics instead.
	ensureAcks := ackTs
	if len(ensureAcks) == 0 && w != nil {
		ensureAcks = w.ackTs
	}
	if len(ensureAcks) > 0 {
		ensureAckedParts(resp, ensureAcks, addTopic)
	}

	// If no records acquired and this is the initial invocation, consider
	// waiting for new data (MinBytes/MaxWait long-poll). Even when
	// piggybacked acks were present, the fetch may delay up to MaxWait
	// (ack results are held until the fetch completes or times out).
	// When blocked by maxRecordLocks, we still create a watcher --
	// fireShareWatchers fires when acks free the window, so the watcher
	// wakes up promptly instead of the client busy-looping with empty
	// fetches.
	if totalRecords == 0 && w == nil {
		wait := time.Duration(req.MaxWaitMillis) * time.Millisecond
		deadline := creq.at.Add(wait)
		remaining := time.Until(deadline)
		if remaining > 0 && len(targets) > 0 {
			wsf := &watchShareFetch{
				creq:    creq,
				session: session,
				ackTs:   ackTs,
			}
			wsf.cb = func() {
				select {
				case c.shareGroups.watchFetchCh <- wsf:
				case <-c.die:
				}
			}
			for _, tgt := range targets {
				tgt.pd.shareWatch[wsf] = struct{}{}
				wsf.in = append(wsf.in, tgt.pd)
			}
			wsf.t = time.AfterFunc(remaining, wsf.cb)
			return nil, nil
		}
	}

	if includeBrokers {
		for _, b := range c.bs {
			ne := kmsg.NewShareFetchResponseNodeEndpoint()
			ne.NodeID = b.node
			ne.Host, ne.Port = b.hostport()
			ne.Rack = &brokerRack
			resp.NodeEndpoints = append(resp.NodeEndpoints, ne)
		}
	}

	session.bumpEpoch()

	return resp, nil
}

// ensureAckedParts ensures every partition in ackTs appears in the response.
// The client uses partition presence to confirm ack processing; missing
// partitions cause "dropping piggybacked acks" warnings.
func ensureAckedParts(resp *kmsg.ShareFetchResponse, ackTs []ackTopic, addTopic func(uuid) int) {
	present := make(map[tpKey]struct{})
	for _, t := range resp.Topics {
		for _, p := range t.Partitions {
			present[tpKey{t.TopicID, p.Partition}] = struct{}{}
		}
	}
	for _, at := range ackTs {
		for _, ap := range at.partitions {
			key := tpKey{at.topicID, ap.partition}
			if _, ok := present[key]; ok {
				continue
			}
			idx := addTopic(at.topicID)
			sp := kmsg.NewShareFetchResponseTopicPartition()
			sp.Partition = ap.partition
			resp.Topics[idx].Partitions = append(resp.Topics[idx].Partitions, sp)
		}
	}
}

// splitAcquiredRanges splits acquired record ranges into sub-batches of at
// most batchSize offsets for BATCH_OPTIMIZED mode (KIP-1206).
func splitAcquiredRanges(ranges []kmsg.ShareFetchResponseTopicPartitionAcquiredRecord, batchSize int32) []kmsg.ShareFetchResponseTopicPartitionAcquiredRecord {
	// Fast path: if no range exceeds batchSize, return as-is.
	needsSplit := false
	for _, r := range ranges {
		if r.LastOffset-r.FirstOffset+1 > int64(batchSize) {
			needsSplit = true
			break
		}
	}
	if !needsSplit {
		return ranges
	}

	out := make([]kmsg.ShareFetchResponseTopicPartitionAcquiredRecord, 0, len(ranges))
	for _, r := range ranges {
		count := int32(r.LastOffset - r.FirstOffset + 1)
		if count <= batchSize {
			out = append(out, r)
			continue
		}
		for off := r.FirstOffset; off <= r.LastOffset; {
			end := min(off+int64(batchSize)-1, r.LastOffset)
			ar := kmsg.NewShareFetchResponseTopicPartitionAcquiredRecord()
			ar.FirstOffset = off
			ar.LastOffset = end
			ar.DeliveryCount = r.DeliveryCount
			out = append(out, ar)
			off = end + 1
		}
	}
	return out
}

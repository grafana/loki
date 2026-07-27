package kfake

import (
	"hash/crc32"
	"hash/fnv"
	"math"
	"math/rand"
	"regexp"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// * Add heap of last use, add index to pidwindow, and remove pidwindow as they exhaust max # of pids configured.

// kfake transaction state simplifications:
//
// EndTxn v0-4 (pre-KIP-890) does not bump the producer epoch; the
// same epoch is reused across transactions. EndTxn v5+ (KIP-890)
// bumps the epoch on each commit/abort and returns the new epoch.
// Produce v12+ implicitly adds partitions to the transaction.
//
// kfake completes transactions synchronously (no log writes), so
// the PREPARE_COMMIT, PREPARE_ABORT, and pendingTransitionInProgress
// states are not needed. Where a real broker returns
// CONCURRENT_TRANSACTIONS for in-progress transitions, kfake simply
// completes the operation immediately.
//
// State tracking: instead of the full transaction state machine
// (Empty, Ongoing, PrepareCommit, PrepareAbort, CompleteCommit,
// CompleteAbort, Dead, PrepareEpochFence), kfake uses:
//   - inTx (bool): true = Ongoing, false = Empty/Complete
//   - lastWasCommit (bool): whether the last completed transaction
//     was commit or abort, for EndTxn retry detection

type (
	pids struct {
		ids     map[int64]*pidinfo
		byTxid  map[string]*pidinfo   // txid -> pidinfo, for expiration
		txs     map[*pidinfo]struct{} // active transactions being tracked for timeout
		txTimer *time.Timer
		c       *Cluster
	}

	// Seq/txn info for a given individual producer ID.
	pidinfo struct {
		pids *pids

		id      int64
		epoch   int16
		windows tps[pidwindow] // topic/partition 5-window pid sequences

		txid         string
		txTimeout    int32                        // millis
		txParts      tps[partData]                // partitions in the transaction, if transactional
		txBatchCount int                          // number of batches in the transaction
		txGroups     []string                     // consumer groups in the transaction
		txOffsets    map[string]tps[offsetCommit] // pending offset commits per group

		// Track per-partition first offset for this transaction.
		// Used for AbortedTransactions in fetch response.
		txPartFirstOffsets tps[int64]

		// Track bytes per partition for this transaction.
		// Used to count committed bytes for readCommitted watchers.
		txPartBytes tps[int]

		txStart    time.Time
		lastActive time.Time // last time this transactional ID was used
		inTx       bool

		// Whether the last completed transaction was a commit (true)
		// or abort (false). Used for EndTxn retry detection: if the
		// client retries an EndTxn after the transaction already
		// completed, we return success only if the retry matches
		// (commit after commit, abort after abort).
		lastWasCommit bool
	}

	// Independent dup-detection entry: stores (firstSeq, nextSeq,
	// offset) for one committed batch. Real Kafka stores 5
	// independent BatchMetadata records; we match that.
	pidEntry struct {
		firstSeq int32
		nextSeq  int32
		offset   int64
	}

	// Sequence ID window for duplicate detection. 5 independent
	// entries (not overlapping pairs), so all 5 in-flight batches
	// allowed by maxInFlight=5 are deduplicatable after a
	// connection death.
	pidwindow struct {
		entries [5]pidEntry
		at      uint8 // next write position (circular)
		count   uint8 // number of valid entries (0-5)
		seen    bool  // true after the first batch has been accepted
		epoch   int16 // last seen epoch; when epoch changes, seq 0 is accepted
		nextSeq int32 // next expected firstSequence (authoritative, survives window rotation)
	}
)

// hasUnstableOffsets returns true if any active transaction has pending
// (uncommitted) offset commits for the given group. Used for KIP-447
// RequireStable support in OffsetFetch.
func (pids *pids) hasUnstableOffsets(group string) bool {
	for pidinf := range pids.txs {
		if _, hasGroup := pidinf.txOffsets[group]; hasGroup {
			return true
		}
	}
	return false
}

// updateTimer reschedules the transaction timeout timer based on the
// next expiring transaction. This also handles any already-expired
// transactions inline: under high request throughput, this function
// runs after every request and drains the timer channel. Without the
// inline check, the timer fires (negative duration for expired txns)
// but gets drained here before mainSelect can read it, creating a
// livelock where expired transactions are never aborted.
func (pids *pids) updateTimer() {
	if !pids.txTimer.Stop() {
		select {
		case <-pids.txTimer.C:
		default:
		}
	}

	// Abort any already-expired transactions inline.
	now := time.Now()
	for {
		var minPid *pidinfo
		var minExpire time.Time
		for pidinf := range pids.txs {
			expire := pidinf.txStart.Add(time.Duration(pidinf.txTimeout) * time.Millisecond)
			if minPid == nil || expire.Before(minExpire) {
				minPid = pidinf
				minExpire = expire
			}
		}
		if minPid == nil || now.Before(minExpire) {
			break
		}
		elapsed := now.Sub(minPid.txStart)
		pids.c.cfg.logger.Logf(LogLevelWarn,
			"txn timeout abort: txn_id=%s producer_id=%d epoch=%d timeout=%dms elapsed=%v",
			minPid.txid, minPid.id, minPid.epoch, minPid.txTimeout, elapsed)
		minPid = pids.bumpEpoch(minPid)
		minPid.endTx(false)
		pids.c.persistPIDEntry(pidLogEntry{Type: "timeout", PID: minPid.id, Epoch: minPid.epoch})
	}

	// Schedule timer for the next future expiry.
	var nextExpire time.Time
	var found bool
	for pidinf := range pids.txs {
		expire := pidinf.txStart.Add(time.Duration(pidinf.txTimeout) * time.Millisecond)
		if !found || expire.Before(nextExpire) {
			nextExpire = expire
			found = true
		}
	}
	// Also schedule for the next transactional ID expiration.
	expirationMs := int64(pids.c.txnIDExpirationMs())
	for _, pidinf := range pids.byTxid {
		if pidinf.inTx {
			continue
		}
		expire := pidinf.lastActive.Add(time.Duration(expirationMs) * time.Millisecond)
		if !found || expire.Before(nextExpire) {
			nextExpire = expire
			found = true
		}
	}
	if found {
		pids.txTimer.Reset(time.Until(nextExpire))
	}
}

// handleTimeout is called when the transaction timer fires in
// mainSelect. It expires transactional IDs and reschedules the timer.
// Transaction abort for expired txns is handled inline by updateTimer,
// which runs after every request - this ensures aborts happen even
// under high throughput when the timer channel gets starved.
func (pids *pids) handleTimeout() {
	pids.expireTransactionalIDs()
	pids.updateTimer()
}

func (pids *pids) doInitProducerID(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.InitProducerIDRequest)
	resp := req.ResponseKind().(*kmsg.InitProducerIDResponse)

	if req.TransactionalID != nil {
		txid := *req.TransactionalID
		if txid == "" {
			resp.ErrorCode = kerr.InvalidRequest.Code
			return resp
		}
		coordinator := pids.c.coordinator(txid)
		if creq.cc.b != coordinator {
			resp.ErrorCode = kerr.NotCoordinator.Code
			return resp
		}
		if req.TransactionTimeoutMillis <= 0 || req.TransactionTimeoutMillis > pids.c.transactionMaxTimeoutMs() {
			resp.ErrorCode = kerr.InvalidTransactionTimeout.Code
			return resp
		}
	}

	// Idempotent-only: always allocate fresh.
	if req.TransactionalID == nil {
		id, epoch := pids.create(nil, 0)
		resp.ProducerID = id
		resp.ProducerEpoch = epoch
		pids.c.persistPIDEntry(pidLogEntry{Type: "init", PID: id, Epoch: epoch})
		return resp
	}

	// KIP-360 (v3+): validate existing producer ID and epoch, bump for recovery.
	if req.ProducerID >= 0 && req.ProducerEpoch >= 0 {
		pidinf := pids.getpid(req.ProducerID)
		if pidinf == nil {
			resp.ErrorCode = kerr.InvalidProducerIDMapping.Code
			return resp
		}
		// Accept current or stale epoch (recovery after timeout
		// bump). Only reject epochs above the server's.
		if req.ProducerEpoch > pidinf.epoch {
			resp.ErrorCode = kerr.ProducerFenced.Code
			return resp
		}
		// Abort any in-flight transaction before bumping. A real
		// broker returns CONCURRENT_TRANSACTIONS and aborts async,
		// but kfake is synchronous so we abort inline.
		if pidinf.inTx {
			pidinf.endTx(false)
		}
		pidinf = pids.bumpEpoch(pidinf)
		pidinf.lastActive = time.Now()
		resp.ProducerID = pidinf.id
		resp.ProducerEpoch = pidinf.epoch
		pids.c.persistPIDEntry(pidLogEntry{Type: "init", PID: pidinf.id, Epoch: pidinf.epoch, TxID: pidinf.txid, Timeout: pidinf.txTimeout})
		return resp
	}

	// New transactional ID or first init. Check if an expired txid is
	// being re-used - the create path handles this via byTxid.
	id, epoch := pids.create(req.TransactionalID, req.TransactionTimeoutMillis)
	resp.ProducerID = id
	resp.ProducerEpoch = epoch
	pids.c.persistPIDEntry(pidLogEntry{Type: "init", PID: id, Epoch: epoch, TxID: *req.TransactionalID, Timeout: req.TransactionTimeoutMillis})
	return resp
}

func (pidinf *pidinfo) maybeStart() {
	if pidinf.inTx {
		return
	}
	pidinf.inTx = true
	pidinf.txStart = time.Now()
	pidinf.pids.txs[pidinf] = struct{}{}
}

func (pids *pids) doAddPartitions(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.AddPartitionsToTxnRequest)
	resp := req.ResponseKind().(*kmsg.AddPartitionsToTxnResponse)

	tidx := make(map[string]int)
	donep := func(t string, p int32, errCode int16) {
		var st *kmsg.AddPartitionsToTxnResponseTopic
		if i, ok := tidx[t]; ok {
			st = &resp.Topics[i]
		} else {
			tidx[t] = len(resp.Topics)
			resp.Topics = append(resp.Topics, kmsg.NewAddPartitionsToTxnResponseTopic())
			st = &resp.Topics[len(resp.Topics)-1]
			st.Topic = t
		}
		sp := kmsg.NewAddPartitionsToTxnResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st.Partitions = append(st.Partitions, sp)
	}
	doneall := func(errCode int16) {
		for _, rt := range req.Topics {
			for _, rp := range rt.Partitions {
				donep(rt.Topic, rp, errCode)
			}
		}
	}

	type tpKey struct {
		t string
		p int32
	}
	pdMap := make(map[tpKey]*partData)
	for _, rt := range req.Topics {
		ps, ok := pids.c.data.tps.gett(rt.Topic)
		if !ok {
			continue
		}
		for _, rp := range rt.Partitions {
			if pd := ps[rp]; pd != nil {
				pdMap[tpKey{rt.Topic, rp}] = pd
			}
		}
	}

	// Check if all topics/partitions exist.
	var noAttempt bool
	for _, rt := range req.Topics {
		for _, rp := range rt.Partitions {
			if pdMap[tpKey{rt.Topic, rp}] == nil {
				noAttempt = true
				break
			}
		}
		if noAttempt {
			break
		}
	}
	if noAttempt {
		for _, rt := range req.Topics {
			for _, rp := range rt.Partitions {
				if pdMap[tpKey{rt.Topic, rp}] == nil {
					donep(rt.Topic, rp, kerr.UnknownTopicOrPartition.Code)
				} else {
					donep(rt.Topic, rp, kerr.OperationNotAttempted.Code)
				}
			}
		}
		return resp
	}

	coordinator := pids.c.coordinator(req.TransactionalID)
	if creq.cc.b != coordinator {
		doneall(kerr.NotCoordinator.Code)
		return resp
	}

	pidinf := pids.getpid(req.ProducerID)
	if pidinf == nil {
		doneall(kerr.InvalidProducerIDMapping.Code)
		return resp
	}
	if pidinf.epoch != req.ProducerEpoch {
		doneall(kerr.ProducerFenced.Code)
		return resp
	}

	for _, rt := range req.Topics {
		for _, partition := range rt.Partitions {
			pd := pdMap[tpKey{rt.Topic, partition}]
			ps := pidinf.txParts.mkt(rt.Topic)
			ps[partition] = pd
			donep(rt.Topic, partition, 0)
		}
	}
	pidinf.maybeStart()
	return resp
}

func (pids *pids) doAddOffsets(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.AddOffsetsToTxnRequest)
	resp := req.ResponseKind().(*kmsg.AddOffsetsToTxnResponse)

	coordinator := pids.c.coordinator(req.TransactionalID)
	if creq.cc.b != coordinator {
		resp.ErrorCode = kerr.NotCoordinator.Code
		return resp
	}

	pidinf := pids.getpid(req.ProducerID)
	if pidinf == nil {
		resp.ErrorCode = kerr.InvalidProducerIDMapping.Code
		return resp
	}
	if pidinf.epoch != req.ProducerEpoch {
		resp.ErrorCode = kerr.ProducerFenced.Code
		return resp
	}

	pidinf.maybeStart()
	pidinf.txGroups = append(pidinf.txGroups, req.Group)
	return resp
}

func (pids *pids) doTxnOffsetCommit(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.TxnOffsetCommitRequest)
	resp := req.ResponseKind().(*kmsg.TxnOffsetCommitResponse)

	doneall := func(errCode int16) {
		for _, rt := range req.Topics {
			st := kmsg.NewTxnOffsetCommitResponseTopic()
			st.Topic = rt.Topic
			for _, rp := range rt.Partitions {
				sp := kmsg.NewTxnOffsetCommitResponseTopicPartition()
				sp.Partition = rp.Partition
				sp.ErrorCode = errCode
				st.Partitions = append(st.Partitions, sp)
			}
			resp.Topics = append(resp.Topics, st)
		}
	}

	coordinator := pids.c.coordinator(req.Group)
	if creq.cc.b != coordinator {
		doneall(kerr.NotCoordinator.Code)
		return resp
	}

	pidinf := pids.getpid(req.ProducerID)
	if pidinf == nil {
		doneall(kerr.InvalidProducerIDMapping.Code)
		return resp
	}
	if pidinf.epoch != req.ProducerEpoch {
		doneall(kerr.ProducerFenced.Code)
		return resp
	}
	if pidinf.txid == "" || pidinf.txid != req.TransactionalID {
		doneall(kerr.InvalidProducerIDMapping.Code)
		return resp
	}

	// KIP-890 Part 2: For v5+ requests, implicitly start the transaction
	// if not already started. For v0-4, require transaction to be active.
	if !pidinf.inTx {
		if req.Version < 5 {
			doneall(kerr.InvalidTxnState.Code)
			return resp
		}
		pidinf.maybeStart()
	}

	// Check if group exists. For generation >= 0, return
	// ILLEGAL_GENERATION if the group doesn't exist. For
	// generation < 0 (admin commits), allow it through.
	var g *group
	if pids.c.groups.gs != nil {
		g = pids.c.groups.gs[req.Group]
	}
	if g != nil {
		if req.Version >= 3 && (req.MemberID != "" || req.Generation != -1) {
			var errCode int16
			if !g.waitControl(func() {
				if err := g.validateInstanceID(req.InstanceID, req.MemberID); err != nil {
					errCode = err.Code
					return
				}
				errCode = g.validateMemberGeneration(req.MemberID, req.Generation)
			}) {
				doneall(kerr.GroupIDNotFound.Code)
				return resp
			}
			if errCode != 0 {
				doneall(errCode)
				return resp
			}
		}
	} else if req.Generation >= 0 {
		doneall(kerr.IllegalGeneration.Code)
		return resp
	}

	groupInTx := slices.Contains(pidinf.txGroups, req.Group)

	// KIP-890: For v5+ requests, implicitly add the group to the transaction
	// if it's not already there. This allows clients to skip AddOffsetsToTxn.
	if !groupInTx {
		if req.Version < 5 {
			doneall(kerr.InvalidTxnState.Code)
			return resp
		}
		pidinf.txGroups = append(pidinf.txGroups, req.Group)
	}

	// Store pending offset commits; will be actually mirrored into
	// the group offsets once the transaction ends with a commit.
	// Validate that each topic/partition exists first.
	if pidinf.txOffsets == nil {
		pidinf.txOffsets = make(map[string]tps[offsetCommit])
	}
	groupOffsets := pidinf.txOffsets[req.Group]
	for _, rt := range req.Topics {
		st := kmsg.NewTxnOffsetCommitResponseTopic()
		st.Topic = rt.Topic
		for _, rp := range rt.Partitions {
			sp := kmsg.NewTxnOffsetCommitResponseTopicPartition()
			sp.Partition = rp.Partition
			if !pids.c.data.tps.checkp(rt.Topic, rp.Partition) {
				sp.ErrorCode = kerr.UnknownTopicOrPartition.Code
			} else {
				groupOffsets.set(rt.Topic, rp.Partition, offsetCommit{
					offset:      rp.Offset,
					leaderEpoch: rp.LeaderEpoch,
					metadata:    rp.Metadata,
				})
			}
			st.Partitions = append(st.Partitions, sp)
		}
		resp.Topics = append(resp.Topics, st)
	}
	pidinf.txOffsets[req.Group] = groupOffsets
	return resp
}

func (pids *pids) doEnd(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.EndTxnRequest)
	resp := req.ResponseKind().(*kmsg.EndTxnResponse)

	coordinator := pids.c.coordinator(req.TransactionalID)
	if creq.cc.b != coordinator {
		resp.ErrorCode = kerr.NotCoordinator.Code
		return resp
	}

	pidinf := pids.getpid(req.ProducerID)
	if pidinf == nil {
		resp.ErrorCode = kerr.InvalidProducerIDMapping.Code
		return resp
	}
	if pidinf.epoch != req.ProducerEpoch {
		// KIP-890 retry detection: if the epoch is exactly one
		// ahead and the producer is not in a transaction, the
		// previous EndTxn already completed and bumped the epoch.
		if req.Version >= 5 && pidinf.epoch == req.ProducerEpoch+1 && !pidinf.inTx {
			if req.Commit != pidinf.lastWasCommit {
				resp.ErrorCode = kerr.InvalidTxnState.Code
				return resp
			}
			resp.ProducerID = pidinf.id
			resp.ProducerEpoch = pidinf.epoch
			return resp
		}
		resp.ErrorCode = kerr.ProducerFenced.Code
		return resp
	}
	if !pidinf.inTx {
		// v5+: allow aborting an empty transaction. Bump epoch
		// so the client uses a fresh epoch for the next transaction.
		if req.Version >= 5 && !req.Commit {
			pidinf = pids.bumpEpoch(pidinf)
			pidinf.lastWasCommit = false
			resp.ProducerID = pidinf.id
			resp.ProducerEpoch = pidinf.epoch
			pids.c.persistPIDEntry(pidLogEntry{Type: "init", PID: pidinf.id, Epoch: pidinf.epoch, TxID: pidinf.txid, Timeout: pidinf.txTimeout})
			return resp
		}
		// Retry detection: return success if the retry matches the
		// completed action (commit retries commit, abort retries abort).
		if req.Commit == pidinf.lastWasCommit {
			return resp
		}
		resp.ErrorCode = kerr.InvalidTxnState.Code
		return resp
	}

	nBatches, nGroups := pidinf.txBatchCount, len(pidinf.txGroups)
	endTxStart := time.Now()
	pidinf.endTx(req.Commit)
	if elapsed := time.Since(endTxStart); elapsed > 5*time.Millisecond {
		pids.c.cfg.logger.Logf(LogLevelWarn, "txn: EndTxn slow pid=%d epoch=%d elapsed=%dms batches=%d groups=%d",
			req.ProducerID, req.ProducerEpoch, elapsed.Milliseconds(), nBatches, nGroups)
	}

	commit := req.Commit
	pids.c.persistPIDEntry(pidLogEntry{Type: "endtx", PID: pidinf.id, Epoch: pidinf.epoch, Commit: &commit})

	// KIP-890: For v5+ clients, bump epoch and return new ID/epoch.
	if req.Version >= 5 {
		pidinf = pids.bumpEpoch(pidinf)
		resp.ProducerID = pidinf.id
		resp.ProducerEpoch = pidinf.epoch
		pids.c.persistPIDEntry(pidLogEntry{Type: "init", PID: pidinf.id, Epoch: pidinf.epoch, TxID: pidinf.txid, Timeout: pidinf.txTimeout})
	}

	return resp
}

func (pids *pids) getpid(id int64) *pidinfo {
	return pids.ids[id]
}

// get returns the pidinfo and idempotent-5 window for a producer on a
// topic-partition. If pd is non-nil and the producer is transactional
// but hasn't registered this partition, the partition is implicitly
// added to the transaction (KIP-890 implicit addition for produce
// v12+). If pd is nil, unregistered transactional partitions cause
// (nil, nil) to be returned.
func (pids *pids) get(id int64, t string, p int32, pd *partData) (*pidinfo, *pidwindow) {
	pidinf := pids.ids[id]
	if pidinf == nil {
		return nil, nil
	}
	if pidinf.txid != "" && !pidinf.txParts.checkp(t, p) {
		if pd == nil {
			return nil, nil
		}
		ps := pidinf.txParts.mkt(t)
		ps[p] = pd
		pidinf.maybeStart()
	}
	return pidinf, pidinf.windows.mkpDefault(t, p)
}

// getOrCreateNonTx returns or creates producer state for a non-transactional
// idempotent produce. Apache Kafka and Redpanda implicitly create producer
// state on demand when the first batch arrives for an unknown (PID, epoch).
// See twmb/franz-go#1281.
func (pids *pids) getOrCreateNonTx(id int64, epoch int16, t string, p int32) (*pidinfo, *pidwindow) {
	pidinf := pids.ids[id]
	if pidinf == nil {
		pidinf = &pidinfo{
			pids:       pids,
			id:         id,
			epoch:      epoch,
			lastActive: time.Now(),
		}
		pids.ids[id] = pidinf
	}
	return pidinf, pidinf.windows.mkpDefault(t, p)
}

func (pids *pids) randomID() int64 {
	for {
		id := int64(rand.Uint64()) & math.MaxInt64
		if _, exists := pids.ids[id]; !exists {
			return id
		}
	}
}

// bumpEpoch increments the epoch. If the epoch reaches the exhaustion
// threshold (math.MaxInt16 - 1), a new producer ID is allocated.
func (pids *pids) bumpEpoch(pidinf *pidinfo) *pidinfo {
	if pidinf.epoch >= math.MaxInt16-1 {
		newID := pids.randomID()
		newPidinf := &pidinfo{
			pids:       pids,
			id:         newID,
			epoch:      0,
			txid:       pidinf.txid,
			txTimeout:  pidinf.txTimeout,
			lastActive: pidinf.lastActive,
		}
		pids.ids[newID] = newPidinf
		delete(pids.ids, pidinf.id)
		if pidinf.txid != "" {
			pids.byTxid[pidinf.txid] = newPidinf
		}
		return newPidinf
	}
	pidinf.epoch++
	return pidinf
}

func (pids *pids) create(txidp *string, txTimeout int32) (int64, int16) {
	// Re-init of an existing transactional ID: look up by txid first
	// to avoid FNV-64 hash collisions between different txids.
	if txidp != nil {
		if pidinf, ok := pids.byTxid[*txidp]; ok {
			pidinf = pids.bumpEpoch(pidinf)
			pidinf.lastActive = time.Now()
			return pidinf.id, pidinf.epoch
		}
	}
	var id int64
	if txidp != nil {
		hasher := fnv.New64()
		hasher.Write([]byte(*txidp))
		id = int64(hasher.Sum64()) & math.MaxInt64
		if _, exists := pids.ids[id]; exists {
			id = pids.randomID() // hash collision, use random
		}
	} else {
		id = pids.randomID()
	}
	pidinf := &pidinfo{
		pids:       pids,
		id:         id,
		txTimeout:  txTimeout,
		lastActive: time.Now(),
	}
	if txidp != nil {
		pidinf.txid = *txidp
		pids.byTxid[*txidp] = pidinf
	}
	pids.ids[id] = pidinf
	return id, 0
}

// expireTransactionalIDs removes idle transactional IDs that haven't been
// used within transactional.id.expiration.ms and are not in an active
// transaction.
func (pids *pids) expireTransactionalIDs() {
	expirationMs := int64(pids.c.txnIDExpirationMs())
	now := time.Now()
	for txid, pidinf := range pids.byTxid {
		if pidinf.inTx {
			continue
		}
		if now.Sub(pidinf.lastActive).Milliseconds() >= expirationMs {
			delete(pids.ids, pidinf.id)
			delete(pids.byTxid, txid)
		}
	}
}

func (pidinf *pidinfo) endTx(commit bool) {
	// Control record key format: version (int16=0) + type (int16: 0=abort, 1=commit)
	var controlType byte // abort = 0
	if commit {
		controlType = 1 // commit
	}
	rec := kmsg.Record{Key: []byte{0, 0, 0, controlType}}
	rec.Length = int32(len(rec.AppendTo(nil)) - 1) // -1 because length itself is encoded as a varint, and varint_length(record_length) == 1 byte
	now := time.Now().UnixMilli()
	b := kmsg.RecordBatch{
		PartitionLeaderEpoch: -1,
		Magic:                2,
		Attributes:           int16(0b00000000_00110000),
		LastOffsetDelta:      0,
		FirstTimestamp:       now,
		MaxTimestamp:         now,
		ProducerID:           pidinf.id,
		ProducerEpoch:        pidinf.epoch,
		FirstSequence:        -1,
		NumRecords:           1,
		Records:              rec.AppendTo(nil),
	}
	benc := b.AppendTo(nil)
	b.Length = int32(len(benc) - 12)
	b.CRC = int32(crc32.Checksum(benc[21:], crc32c))

	pidinf.txParts.each(func(t string, p int32, pd *partData) {
		delete(pd.uncommittedPIDs, pidinf.id)

		c := pidinf.pids.c
		controlOffset := c.pushBatch(pd, len(benc), b, false) // control record is not itself transactional
		if controlOffset < 0 {
			c.cfg.logger.Logf(LogLevelError, "endTx: failed to persist control batch for %s p%d pid %d", t, p, pidinf.id)
			pd.recalculateLSO()
			return
		}
		if !commit {
			firstOffset, ok := pidinf.txPartFirstOffsets.getp(t, p)
			if ok {
				pd.abortedTxns = append(pd.abortedTxns, abortedTxnEntry{
					producerID:  pidinf.id,
					firstOffset: *firstOffset,
					lastOffset:  controlOffset,
				})
			}
		}
		pd.recalculateLSO()
		// Count the now-committed bytes for readCommitted watchers.
		// These bytes were skipped in push() because inTx was true.
		txnBytes, _ := pidinf.txPartBytes.getp(t, p)
		if txnBytes != nil && *txnBytes > 0 {
			for w := range pd.watch {
				if w.readCommitted {
					w.addBytes(pd, *txnBytes)
				}
			}
		}
	})

	// Handle transactional offset commits
	if commit && len(pidinf.txOffsets) > 0 {
		// Apply pending offset commits to groups.
		for _, groupID := range pidinf.txGroups {
			groupOffsets, hasOffsets := pidinf.txOffsets[groupID]
			if !hasOffsets || len(groupOffsets) == 0 {
				continue
			}
			if pidinf.pids.c.groups.gs == nil {
				pidinf.pids.c.groups.gs = make(map[string]*group)
			}
			g := pidinf.pids.c.groups.gs[groupID]
			if g == nil {
				g = pidinf.pids.c.groups.newGroup(groupID)
				pidinf.pids.c.groups.gs[groupID] = g
				go g.manage(func() {})
			}
			g.waitControl(func() {
				groupOffsets.each(func(t string, p int32, oc *offsetCommit) {
					g.commitAndPersist(t, p, *oc)
				})
			})
		}
	}

	pidinf.resetTx(commit)
}

func (pidinf *pidinfo) resetTx(wasCommit bool) {
	pidinf.txParts = nil
	pidinf.txBatchCount = 0
	pidinf.txGroups = nil
	pidinf.txOffsets = nil
	pidinf.txPartFirstOffsets = nil
	pidinf.txPartBytes = nil
	pidinf.txStart = time.Time{}
	pidinf.inTx = false
	pidinf.lastWasCommit = wasCommit
	pidinf.lastActive = time.Now()
	delete(pidinf.pids.txs, pidinf)
}

// pushAndValidate checks the sequence number against the window and
// returns whether the batch is valid and whether it is a duplicate.
// For duplicates, dupOffset is the base offset from the original push.
// For new (non-dup) batches, baseOffset is stored for future dup
// detection.
//
// Sequence validation uses s.nextSeq (the authoritative next-expected
// sequence), not a position in the 5-entry circular buffer. This
// matches the real broker's ProducerStateEntry.lastSeq -- nextSeq
// survives buffer rotation, so retries of committed-but-evicted
// batches are correctly identified as dups or gaps rather than
// false-positive OOOSN.
func (s *pidwindow) pushAndValidate(epoch int16, firstSeq, numRecs int32, baseOffset int64) (ok, dup bool, dupOffset int64) {
	if s == nil {
		return true, false, 0
	}

	// Epoch change or first-ever batch: client has reset sequences.
	if !s.seen || epoch != s.epoch {
		if s.seen && firstSeq != 0 {
			return false, false, 0
		}
		next := int32((int64(firstSeq) + int64(numRecs)) % math.MaxInt32)
		s.seen = true
		s.epoch = epoch
		s.nextSeq = next
		// Reset the window with the first entry.
		s.entries = [5]pidEntry{{firstSeq, next, baseOffset}}
		s.at = 1
		s.count = 1
		return true, false, 0
	}

	// Dup check: scan valid entries for a matching (firstSeq, nextSeq) pair.
	next := int32((int64(firstSeq) + int64(numRecs)) % math.MaxInt32)
	for i := range s.count {
		e := s.entries[i]
		if e.firstSeq == firstSeq && e.nextSeq == next {
			return true, true, e.offset
		}
	}

	// Sequence validation: compare against nextSeq, not the
	// circular buffer position. nextSeq is the authoritative
	// next-expected sequence that survives window rotation.
	if firstSeq != s.nextSeq {
		return false, false, 0
	}

	// Accept: advance nextSeq and store the entry.
	s.nextSeq = next
	s.entries[s.at] = pidEntry{firstSeq, next, baseOffset}
	s.at = (s.at + 1) % 5
	if s.count < 5 {
		s.count++
	}
	return true, false, 0
}

func (pids *pids) doDescribeTransactions(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.DescribeTransactionsRequest)
	resp := req.ResponseKind().(*kmsg.DescribeTransactionsResponse)

	for _, txnID := range req.TransactionalIDs {
		st := kmsg.NewDescribeTransactionsResponseTransactionState()
		st.TransactionalID = txnID

		if !pids.c.allowedACL(creq, txnID, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationDescribe) {
			st.ErrorCode = kerr.TransactionalIDAuthorizationFailed.Code
			resp.TransactionStates = append(resp.TransactionStates, st)
			continue
		}

		coordinator := pids.c.coordinator(txnID)
		if coordinator != creq.cc.b {
			st.ErrorCode = kerr.NotCoordinator.Code
			resp.TransactionStates = append(resp.TransactionStates, st)
			continue
		}

		pidinf := pids.findTxnID(txnID)
		if pidinf == nil {
			st.ErrorCode = kerr.TransactionalIDNotFound.Code
			resp.TransactionStates = append(resp.TransactionStates, st)
			continue
		}

		st.ProducerID = pidinf.id
		st.ProducerEpoch = pidinf.epoch
		st.TimeoutMillis = pidinf.txTimeout

		if pidinf.inTx {
			st.State = "Ongoing"
			st.StartTimestamp = pidinf.txStart.UnixMilli()
			pidinf.txParts.each(func(topic string, partition int32, _ *partData) {
				if !pids.c.allowedACL(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
					return
				}
				var topicEntry *kmsg.DescribeTransactionsResponseTransactionStateTopic
				for i := range st.Topics {
					if st.Topics[i].Topic == topic {
						topicEntry = &st.Topics[i]
						break
					}
				}
				if topicEntry == nil {
					st.Topics = append(st.Topics, kmsg.NewDescribeTransactionsResponseTransactionStateTopic())
					topicEntry = &st.Topics[len(st.Topics)-1]
					topicEntry.Topic = topic
				}
				topicEntry.Partitions = append(topicEntry.Partitions, partition)
			})
		} else {
			st.State = "Empty"
			st.StartTimestamp = -1
		}

		resp.TransactionStates = append(resp.TransactionStates, st)
	}

	return resp
}

func (pids *pids) doListTransactions(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.ListTransactionsRequest)
	resp := req.ResponseKind().(*kmsg.ListTransactionsResponse)

	// Build filter sets.
	stateFilter := make(map[string]struct{})
	for _, s := range req.StateFilters {
		stateFilter[s] = struct{}{}
	}
	pidFilter := make(map[int64]struct{})
	for _, pid := range req.ProducerIDFilters {
		pidFilter[pid] = struct{}{}
	}
	var txnIDRegex *regexp.Regexp
	if req.Version >= 2 && req.TransactionalIDPattern != nil && *req.TransactionalIDPattern != "" {
		var err error
		txnIDRegex, err = regexp.Compile(*req.TransactionalIDPattern)
		if err != nil {
			resp.ErrorCode = kerr.InvalidRegularExpression.Code
			return resp
		}
	}

	for _, pidinf := range pids.ids {
		if pidinf.txid == "" {
			continue
		}
		if !pids.c.allowedACL(creq, pidinf.txid, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationDescribe) {
			continue
		}
		state := "Empty"
		if pidinf.inTx {
			state = "Ongoing"
		}
		if len(stateFilter) > 0 {
			if _, ok := stateFilter[state]; !ok {
				continue
			}
		}
		if len(pidFilter) > 0 {
			if _, ok := pidFilter[pidinf.id]; !ok {
				continue
			}
		}
		if req.Version >= 1 && req.DurationFilterMillis >= 0 && pidinf.inTx {
			if creq.at.Sub(pidinf.txStart).Milliseconds() < req.DurationFilterMillis {
				continue
			}
		}
		if txnIDRegex != nil && !txnIDRegex.MatchString(pidinf.txid) {
			continue
		}
		ts := kmsg.NewListTransactionsResponseTransactionState()
		ts.TransactionalID = pidinf.txid
		ts.ProducerID = pidinf.id
		ts.TransactionState = state
		resp.TransactionStates = append(resp.TransactionStates, ts)
	}

	return resp
}

func (pids *pids) doDescribeProducers(creq *clientReq) kmsg.Response {
	req := creq.kreq.(*kmsg.DescribeProducersRequest)
	resp := req.ResponseKind().(*kmsg.DescribeProducersResponse)

	type partState struct {
		topic     string
		partition int32
		errCode   int16
	}
	var checks []partState
	for _, rt := range req.Topics {
		if !pids.c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
			for _, p := range rt.Partitions {
				checks = append(checks, partState{rt.Topic, p, kerr.TopicAuthorizationFailed.Code})
			}
			continue
		}
		for _, p := range rt.Partitions {
			checks = append(checks, partState{rt.Topic, p, 0})
		}
	}

	for i := range checks {
		if checks[i].errCode != 0 {
			continue
		}
		t, tok := pids.c.data.tps.gett(checks[i].topic)
		if !tok {
			checks[i].errCode = kerr.UnknownTopicOrPartition.Code
			continue
		}
		pd, pok := t[checks[i].partition]
		if !pok {
			checks[i].errCode = kerr.UnknownTopicOrPartition.Code
			continue
		}
		if pd.leader != creq.cc.b {
			checks[i].errCode = kerr.NotLeaderForPartition.Code
		}
	}

	tidx := make(map[string]int)
	for _, pc := range checks {
		var st *kmsg.DescribeProducersResponseTopic
		if i, ok := tidx[pc.topic]; ok {
			st = &resp.Topics[i]
		} else {
			tidx[pc.topic] = len(resp.Topics)
			resp.Topics = append(resp.Topics, kmsg.NewDescribeProducersResponseTopic())
			st = &resp.Topics[len(resp.Topics)-1]
			st.Topic = pc.topic
		}
		sp := kmsg.NewDescribeProducersResponseTopicPartition()
		sp.Partition = pc.partition
		sp.ErrorCode = pc.errCode
		if pc.errCode == 0 {
			sp.ActiveProducers = pids.txnProducers(pc.topic, pc.partition)
		}
		st.Partitions = append(st.Partitions, sp)
	}

	return resp
}

// txnProducers returns active transactional producers for a partition.
func (pids *pids) txnProducers(topic string, partition int32) []kmsg.DescribeProducersResponseTopicPartitionActiveProducer {
	var producers []kmsg.DescribeProducersResponseTopicPartitionActiveProducer
	for _, pidinf := range pids.ids {
		if !pidinf.inTx || !pidinf.txParts.checkp(topic, partition) {
			continue
		}
		ap := kmsg.NewDescribeProducersResponseTopicPartitionActiveProducer()
		ap.ProducerID = pidinf.id
		ap.ProducerEpoch = int32(pidinf.epoch)
		ap.LastTimestamp = pidinf.txStart.UnixMilli()
		ap.CoordinatorEpoch = 0
		ap.CurrentTxnStartOffset = -1
		ap.LastSequence = -1
		if pw, ok := pidinf.windows.getp(topic, partition); ok && pw != nil {
			ap.LastSequence = pw.nextSeq - 1
		}
		if firstOffset, ok := pidinf.txPartFirstOffsets.getp(topic, partition); ok && firstOffset != nil {
			ap.CurrentTxnStartOffset = *firstOffset
		}
		producers = append(producers, ap)
	}
	return producers
}

// findTxnID finds a producer info by transactional ID.
func (pids *pids) findTxnID(txnID string) *pidinfo {
	return pids.byTxid[txnID]
}
